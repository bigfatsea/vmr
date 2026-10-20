package stats

import (
	"math"
	"slices"
	"strconv"
	"time"
)

// dimDef is one groupable/filterable dimension: a key extractor over a row
// and the key's display value. Keys are cheap integers so grouping never
// builds strings per row; values are resolved only for result groups and
// for filter matching (memoized per key).
type dimDef struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Desc  string `json:"desc"`
	key   func(*row) uint64
	value func(*dict, uint64) string
}

func dictDim(name, label, desc string, field func(*row) uint32) dimDef {
	return dimDef{
		Name: name, Label: label, Desc: desc,
		key:   func(r *row) uint64 { return uint64(field(r)) },
		value: func(d *dict, k uint64) string { return d.str(uint32(k)) },
	}
}

func boolDim(name, label, desc string, flag uint8) dimDef {
	return dimDef{
		Name: name, Label: label, Desc: desc,
		key: func(r *row) uint64 {
			if r.has(flag) {
				return 1
			}
			return 0
		},
		value: func(_ *dict, k uint64) string { return strconv.FormatBool(k == 1) },
	}
}

var weekdayNames = [...]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

// dims is the dimension registry, in the order /stats/schema lists it.
var dims = []dimDef{
	dictDim("vmodel", "Virtual model", "The model name the client asked for (provider:model under direct routing).", func(r *row) uint32 { return r.vmodel }),
	dictDim("protocol", "Protocol", "Ingress protocol.", func(r *row) uint32 { return r.protocol }),
	{
		Name: "endpoint", Label: "Endpoint", Desc: "provider:model of the attempt that served the request (or the last one tried when nothing was forwarded); empty when no attempt was made.",
		key: func(r *row) uint64 { return uint64(r.provider)<<32 | uint64(r.model) },
		value: func(d *dict, k uint64) string {
			p := d.str(uint32(k >> 32))
			if p == "" {
				return ""
			}
			return p + ":" + d.str(uint32(k))
		},
	},
	dictDim("provider", "Provider", "Provider of the serving (or last tried) attempt.", func(r *row) uint32 { return r.provider }),
	dictDim("model", "Upstream model", "Upstream model of the serving (or last tried) attempt.", func(r *row) uint32 { return r.model }),
	dictDim("key_label", "Upstream key", "Label of the upstream API key the attempt used.", func(r *row) uint32 { return r.keyLabel }),
	dictDim("client", "Caller", "Caller tag derived from the client's vmr API key.", func(r *row) uint32 { return r.client }),
	boolDim("stream", "Streaming", "Whether the client asked for a streamed response.", flagStream),
	{
		Name: "outcome", Label: "Outcome", Desc: "ok, error or canceled.",
		key:   func(r *row) uint64 { return uint64(r.outcome) },
		value: func(_ *dict, k uint64) string { return outcomeNames[k] },
	},
	dictDim("error_class", "Error class", "The routing half's error classification, verbatim; empty on success.", func(r *row) uint32 { return r.ec }),
	{
		Name: "status", Label: "HTTP status", Desc: "Final HTTP status; empty when no HTTP exchange happened.",
		key: func(r *row) uint64 { return uint64(r.status) },
		value: func(_ *dict, k uint64) string {
			if k == 0 {
				return ""
			}
			return strconv.FormatUint(k, 10)
		},
	},
	{
		Name: "attempts", Label: "Attempts", Desc: "Upstream attempts made: 0, 1, 2 or 3+ (above 1 means failover).",
		key: func(r *row) uint64 { return uint64(min(r.attempts, 3)) },
		value: func(_ *dict, k uint64) string {
			if k >= 3 {
				return "3+"
			}
			return strconv.FormatUint(k, 10)
		},
	},
	boolDim("forwarded", "Forwarded", "Whether any attempt was actually forwarded to the client.", flagForwarded),
	boolDim("has_tools", "Has tools", "Whether the request declared tools.", flagHasTools),
	boolDim("has_image", "Has image", "Whether the request carried an image.", flagHasImage),
	{
		Name: "hour_of_day", Label: "Hour of day", Desc: "Local hour the request arrived in (00–23).",
		key:   func(r *row) uint64 { return uint64(time.UnixMilli(r.ts).In(time.Local).Hour()) },
		value: func(_ *dict, k uint64) string { return twoDigit(k) },
	},
	{
		Name: "weekday", Label: "Weekday", Desc: "Local weekday the request arrived on.",
		key:   func(r *row) uint64 { return uint64((time.UnixMilli(r.ts).In(time.Local).Weekday() + 6) % 7) },
		value: func(_ *dict, k uint64) string { return weekdayNames[k%7] },
	},
}

func twoDigit(k uint64) string {
	if k < 10 {
		return "0" + strconv.FormatUint(k, 10)
	}
	return strconv.FormatUint(k, 10)
}

var dimIndex = func() map[string]*dimDef {
	m := make(map[string]*dimDef, len(dims))
	for i := range dims {
		m[dims[i].Name] = &dims[i]
	}
	return m
}()

// Sample faces. Request: every booked event. Usage: forwarded events —
// forwarded tokens were really spent whatever the outcome, the same basis
// quota metering charges. Quality: ok AND forwarded — a failed or canceled
// request's duration is meaningless, and mixing it in pollutes percentiles.
const (
	faceRequest = "request"
	faceUsage   = "usage"
	faceQuality = "quality"
)

// Metric kinds tell the front end how to format a value.
const (
	kindCount  = "count"
	kindTokens = "tokens"
	kindRatio  = "ratio"
	kindMS     = "ms"
	kindRate   = "rate"
)

// needs marks which percentile pools a metric requires, so a query that
// asks only for counts never allocates per-row sample slices.
const (
	needDur uint8 = 1 << iota
	needTTFT
	needToks
)

type metricDef struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Face  string `json:"face"`
	Desc  string `json:"desc"`
	need  uint8
	eval  func(a, bucket *acc) any
}

// minToksSpanMS drops a streamed sample whose generation span is shorter
// than this from the throughput pool: a few tokens over a few milliseconds
// (an upstream flushing its tail back-to-back) yields physically
// implausible rates that would dominate the percentiles.
const minToksSpanMS = 50

// acc accumulates one (bucket, group) cell.
type acc struct {
	req, ok, err, canceled, failover int64
	in, out, cacheRead, cacheWrite   int64
	tokUnknown                       int
	qN, durSum                       int64
	ttftN, ttftSum                   int64
	toksN, toksOut, toksSpan         int64
	durs, ttfts                      []int32
	toks                             []float64
	sorted                           bool
}

func (a *acc) add(r *row, need uint8) {
	a.req++
	switch r.outcome {
	case outcomeOK:
		a.ok++
	case outcomeCanceled:
		a.canceled++
	default:
		a.err++
	}
	if r.attempts > 1 {
		a.failover++
	}
	if !r.has(flagForwarded) {
		return
	}
	if r.has(flagNoTokens) {
		a.tokUnknown++
	}
	a.in += r.in
	a.out += r.out
	a.cacheRead += r.cacheRead
	a.cacheWrite += r.cacheWrite
	if r.outcome != outcomeOK {
		return
	}
	a.qN++
	a.durSum += int64(r.dur)
	if need&needDur != 0 {
		a.durs = append(a.durs, r.dur)
	}
	// TTFT and generation throughput only exist for streamed responses: a
	// JSON response arrives in one piece, so it has no first token.
	if !r.has(flagStream) || r.ttft <= 0 {
		return
	}
	a.ttftN++
	a.ttftSum += int64(r.ttft)
	if need&needTTFT != 0 {
		a.ttfts = append(a.ttfts, r.ttft)
	}
	span := int64(r.dur) - int64(r.ttft)
	if r.out <= 0 || span < minToksSpanMS {
		return
	}
	a.toksN++
	a.toksOut += r.out
	a.toksSpan += span
	if need&needToks != 0 {
		a.toks = append(a.toks, float64(r.out)/(float64(span)/1000))
	}
}

func (a *acc) sortPools() {
	if a.sorted {
		return
	}
	slices.Sort(a.durs)
	slices.Sort(a.ttfts)
	slices.Sort(a.toks)
	a.sorted = true
}

// nearestRank is the nearest-rank percentile over a sorted slice:
// element ceil(p·n), clamped to [1, n].
func nearestRank[T int32 | float64](sorted []T, p float64) (T, bool) {
	if len(sorted) == 0 {
		return 0, false
	}
	idx := int(math.Ceil(p * float64(len(sorted))))
	idx = max(1, min(idx, len(sorted)))
	return sorted[idx-1], true
}

func ratio(num, den int64) any {
	if den == 0 {
		return nil
	}
	return round(float64(num)/float64(den), 4)
}

func round(v float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(v*p) / p
}

func meanMS(sum, n int64) any {
	if n == 0 {
		return nil
	}
	return math.Round(float64(sum) / float64(n))
}

func pctMS(pool func(*acc) []int32, p float64) func(a, _ *acc) any {
	return func(a, _ *acc) any {
		a.sortPools()
		if v, ok := nearestRank(pool(a), p); ok {
			return int64(v)
		}
		return nil
	}
}

func pctToks(p float64) func(a, _ *acc) any {
	return func(a, _ *acc) any {
		a.sortPools()
		if v, ok := nearestRank(a.toks, p); ok {
			return round(v, 2)
		}
		return nil
	}
}

func count(f func(*acc) int64) func(a, _ *acc) any { return func(a, _ *acc) any { return f(a) } }

func prompt(a *acc) int64 { return a.in + a.cacheRead + a.cacheWrite }

func durs(a *acc) []int32  { return a.durs }
func ttfts(a *acc) []int32 { return a.ttfts }

// metrics is the metric registry, in the order /stats/schema lists it. It
// is the only place a formula or a sample face is defined.
var metrics = []metricDef{
	{Name: "req", Label: "Requests", Kind: kindCount, Face: faceRequest, Desc: "Booked requests.", eval: count(func(a *acc) int64 { return a.req })},
	{Name: "ok", Label: "OK", Kind: kindCount, Face: faceRequest, Desc: "Requests that completed successfully.", eval: count(func(a *acc) int64 { return a.ok })},
	{Name: "err", Label: "Errors", Kind: kindCount, Face: faceRequest, Desc: "Requests that failed.", eval: count(func(a *acc) int64 { return a.err })},
	{Name: "canceled", Label: "Canceled", Kind: kindCount, Face: faceRequest, Desc: "Requests the client canceled.", eval: count(func(a *acc) int64 { return a.canceled })},
	{Name: "err_rate", Label: "Error rate", Kind: kindRatio, Face: faceRequest, Desc: "Errors / requests.", eval: func(a, _ *acc) any { return ratio(a.err, a.req) }},
	{Name: "cancel_rate", Label: "Cancel rate", Kind: kindRatio, Face: faceRequest, Desc: "Canceled / requests.", eval: func(a, _ *acc) any { return ratio(a.canceled, a.req) }},
	{Name: "failover_rate", Label: "Failover rate", Kind: kindRatio, Face: faceRequest, Desc: "Share of requests that needed more than one upstream attempt.", eval: func(a, _ *acc) any { return ratio(a.failover, a.req) }},
	{Name: "share_req", Label: "Request share", Kind: kindRatio, Face: faceRequest, Desc: "This group's requests / all requests in the same time bucket.", eval: func(a, b *acc) any { return ratio(a.req, b.req) }},
	{Name: "tok_fresh", Label: "Tok fresh", Kind: kindTokens, Face: faceUsage, Desc: "Fresh (uncached) input tokens.", eval: count(func(a *acc) int64 { return a.in })},
	{Name: "tok_cache_write", Label: "Tok cache write", Kind: kindTokens, Face: faceUsage, Desc: "Input tokens written to the prompt cache.", eval: count(func(a *acc) int64 { return a.cacheWrite })},
	{Name: "tok_cache_read", Label: "Tok cache read", Kind: kindTokens, Face: faceUsage, Desc: "Input tokens served from the prompt cache.", eval: count(func(a *acc) int64 { return a.cacheRead })},
	{Name: "tok_out", Label: "Tok out", Kind: kindTokens, Face: faceUsage, Desc: "Output tokens.", eval: count(func(a *acc) int64 { return a.out })},
	{Name: "tok_prompt", Label: "Tok in", Kind: kindTokens, Face: faceUsage, Desc: "All input tokens: fresh + cache write + cache read.", eval: count(prompt)},
	{Name: "tok_total", Label: "Tok total", Kind: kindTokens, Face: faceUsage, Desc: "Input + output tokens.", eval: count(func(a *acc) int64 { return prompt(a) + a.out })},
	{Name: "cache_hit", Label: "Cache hit", Kind: kindRatio, Face: faceUsage, Desc: "Cache-read share of input tokens.", eval: func(a, _ *acc) any { return ratio(a.cacheRead, prompt(a)) }},
	{Name: "share_tok", Label: "Token share", Kind: kindRatio, Face: faceUsage, Desc: "This group's total tokens / all total tokens in the same time bucket (every token weighs the same, regardless of price).", eval: func(a, b *acc) any { return ratio(prompt(a)+a.out, prompt(b)+b.out) }},
	{Name: "dur_avg", Label: "Duration avg", Kind: kindMS, Face: faceQuality, Desc: "Mean total duration of successful requests.", eval: func(a, _ *acc) any { return meanMS(a.durSum, a.qN) }},
	{Name: "dur_p50", Label: "Duration p50", Kind: kindMS, Face: faceQuality, Desc: "Median total duration of successful requests.", need: needDur, eval: pctMS(durs, 0.5)},
	{Name: "dur_p90", Label: "Duration p90", Kind: kindMS, Face: faceQuality, Desc: "90th-percentile total duration of successful requests.", need: needDur, eval: pctMS(durs, 0.9)},
	{Name: "dur_p99", Label: "Duration p99", Kind: kindMS, Face: faceQuality, Desc: "99th-percentile total duration of successful requests.", need: needDur, eval: pctMS(durs, 0.99)},
	{Name: "ttft_avg", Label: "TTFT avg", Kind: kindMS, Face: faceQuality, Desc: "Mean time to first token of successful streamed requests.", eval: func(a, _ *acc) any { return meanMS(a.ttftSum, a.ttftN) }},
	{Name: "ttft_p50", Label: "TTFT p50", Kind: kindMS, Face: faceQuality, Desc: "Median time to first token of successful streamed requests.", need: needTTFT, eval: pctMS(ttfts, 0.5)},
	{Name: "ttft_p90", Label: "TTFT p90", Kind: kindMS, Face: faceQuality, Desc: "90th-percentile time to first token (the slow tail) of successful streamed requests.", need: needTTFT, eval: pctMS(ttfts, 0.9)},
	{Name: "ttft_p99", Label: "TTFT p99", Kind: kindMS, Face: faceQuality, Desc: "99th-percentile time to first token of successful streamed requests.", need: needTTFT, eval: pctMS(ttfts, 0.99)},
	{Name: "toks_avg", Label: "Tok/s avg", Kind: kindRate, Face: faceQuality, Desc: "Output tokens / generation time (after the first token) over successful streamed requests.", eval: func(a, _ *acc) any {
		if a.toksSpan == 0 {
			return nil
		}
		return round(float64(a.toksOut)/(float64(a.toksSpan)/1000), 2)
	}},
	{Name: "toks_p50", Label: "Tok/s p50", Kind: kindRate, Face: faceQuality, Desc: "Median per-request generation throughput of successful streamed requests.", need: needToks, eval: pctToks(0.5)},
	{Name: "toks_p10", Label: "Tok/s p10", Kind: kindRate, Face: faceQuality, Desc: "10th-percentile generation throughput — the slow floor (throughput is a yield metric, so its bad tail is the low end).", need: needToks, eval: pctToks(0.1)},
}

var metricIndex = func() map[string]*metricDef {
	m := make(map[string]*metricDef, len(metrics))
	for i := range metrics {
		m[metrics[i].Name] = &metrics[i]
	}
	return m
}()
