// Ver 2026-07-12 03:10, by Fable 5

// Package report turns audit JSONL files (design doc §9.2) into aggregate
// statistics: a fine-grained JSON data table plus a human-readable Markdown
// rendering. It is coupled to the audit format — change one, change both.
package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"vmr/internal/audit"
)

// Format is bumped whenever the report JSON structure changes.
// 2: rows are grained date × protocol × model (a virtual-model name may
// exist in both protocol groups; merging them mixed two real models).
// 3: tokens_in now includes Anthropic's cache_read/cache_creation tokens
// (previously excluded, since Anthropic reports them as separate counters
// from input_tokens) and gains tokens_in_cached/tokens_in_cache_write.
// 4: rows gain request-shape stats (messages/messages_known/role_chars)
// and first-token latency (ttft_ms_sum/ttft_known, from the audit
// record's ttft_ms field — absent in logs written before it existed).
// 5: adds the session-analysis sections — tools[] (per request-shape tool
// declaration vs. actual per-turn use) and sessions[] (agent-session
// summaries) — attached from AnalyzeSessions by the report command.
// 6: rows gain finish_reasons/truncated/tokens_reasoning and TTFT
// percentiles; adds hours[] (date × hour activity) and workloads[]
// (traffic split by workload class: interactive vs. scheduled scaffolding).
const Format = 6

// Report is the top-level JSON output. Grains: Rows = date × protocol ×
// virtual model (roll up freely to coarser cuts); Endpoints = date × upstream
// endpoint (availability and error view). Finer grains stay in the raw logs.
type Report struct {
	Meta      Meta          `json:"meta"`
	Rows      []Row         `json:"rows"`
	Endpoints []EndpointRow `json:"endpoints"`
	Hours     []HourRow     `json:"hours,omitempty"`
	// Tools, Sessions and Workloads come from the session analysis
	// (session.go) and are attached by the report command after Build;
	// empty when analysis is off.
	Tools     []ToolShapeRow `json:"tools,omitempty"`
	Sessions  []SessionRow   `json:"sessions,omitempty"`
	Workloads []WorkloadRow  `json:"workloads,omitempty"`
}

// HourRow aggregates requests for one (date, local hour) pair — the
// activity profile for spotting load peaks and planning around provider
// rate limits.
type HourRow struct {
	Date     string `json:"date"`
	Hour     int    `json:"hour"` // 0-23, record's local timezone
	Requests int    `json:"requests"`
	OK       int    `json:"ok"`
	Errors   int    `json:"errors"`

	TokensIn       int64 `json:"tokens_in"`
	TokensInCached int64 `json:"tokens_in_cached"`
	TokensOut      int64 `json:"tokens_out"`
	BytesOut       int64 `json:"bytes_out"`
}

type Meta struct {
	Format      int      `json:"format"`
	GeneratedAt string   `json:"generated_at"`
	Inputs      []string `json:"inputs"`
	Records     int      `json:"records"`
	ParseErrors int      `json:"parse_errors"`
	From        string   `json:"from,omitempty"` // earliest ts
	To          string   `json:"to,omitempty"`   // latest ts
}

// Row aggregates requests for one (date, protocol, virtual model) triple.
type Row struct {
	Date     string `json:"date"`
	Model    string `json:"model"`
	Protocol string `json:"protocol"`

	Requests int `json:"requests"`
	OK       int `json:"ok"`
	Errors   int `json:"errors"`
	Canceled int `json:"canceled"`
	Streams  int `json:"streams"`

	Attempts  int `json:"attempts"`  // upstream tries across all requests
	Fallbacks int `json:"fallbacks"` // requests that needed >1 attempt

	TokensIn    int64 `json:"tokens_in"` // summed over records with usage; includes cached tokens
	TokensOut   int64 `json:"tokens_out"`
	TokensKnown int   `json:"tokens_known"` // #records where usage was extractable

	// TokensIn split by cache: TokensInCached is the cache-hit portion (Anthropic
	// cache_read_input_tokens / OpenAI prompt_tokens_details.cached_tokens /
	// DeepSeek prompt_cache_hit_tokens); TokensInCacheWrite is Anthropic-only
	// (cache_creation_input_tokens, billed at a premium, not a hit). Both are
	// subsets already counted in TokensIn — fresh tokens = TokensIn - the two.
	TokensInCached     int64 `json:"tokens_in_cached"`
	TokensInCacheWrite int64 `json:"tokens_in_cache_write"`

	BytesIn  int64 `json:"bytes_in"`  // client request body bytes (as recorded)
	BytesOut int64 `json:"bytes_out"` // client response body bytes (as recorded)

	// Request-shape stats over records whose request body parsed as a chat
	// object (MessagesKnown counts them): total message count and displayed
	// characters per role. Anthropic tool_result parts count as "tool" to
	// stay comparable with openai's dedicated tool role.
	Messages      int64            `json:"messages"`
	MessagesKnown int              `json:"messages_known"`
	RoleChars     map[string]int64 `json:"role_chars,omitempty"`

	// First-token latency (client view: request arrival → first response
	// body byte), summed over the TTFTKnown records that carry ttft_ms —
	// logs written before the field existed contribute nothing.
	TTFTMSSum int64 `json:"ttft_ms_sum"`
	TTFTKnown int   `json:"ttft_known"`
	TTFTMSP50 int64 `json:"ttft_ms_p50,omitempty"`
	TTFTMSP95 int64 `json:"ttft_ms_p95,omitempty"`

	// FinishReasons distributes records by the response's finish_reason /
	// stop_reason ("length" = output truncated by the token cap — a health
	// signal); records without one (errors, canceled) count under "".
	FinishReasons map[string]int `json:"finish_reasons,omitempty"`
	// Truncated counts ok-outcome records whose stream broke mid-flight
	// (attempt error "truncated: …"): the client got a 2xx but incomplete
	// bytes — invisible in OK/Errors, so it gets its own counter.
	Truncated int `json:"truncated,omitempty"`
	// TokensReasoning sums the thinking-token portion of TokensOut where the
	// provider reports it (a subset of TokensOut, not additive).
	TokensReasoning int64 `json:"tokens_reasoning,omitempty"`

	DurMSSum int64 `json:"dur_ms_sum"`
	DurMSP50 int64 `json:"dur_ms_p50"`
	DurMSP95 int64 `json:"dur_ms_p95"`
	DurMSMax int64 `json:"dur_ms_max"`

	// Throughput: tokens_out per second over records with known usage;
	// bytes_out per second over all records with dur>0.
	TokOutPerSec   float64 `json:"tok_out_per_sec"`
	BytesOutPerSec float64 `json:"bytes_out_per_sec"`

	durs       []int64 // working state, not serialized
	ttfts      []int64
	tokDurMS   int64
	bytesDurMS int64
}

// EndpointRow aggregates upstream attempts for one (date, endpoint) pair.
type EndpointRow struct {
	Date     string `json:"date"`
	Endpoint string `json:"endpoint"` // provider/real-model

	Attempts     int            `json:"attempts"`
	OK           int            `json:"ok"`
	Failed       int            `json:"failed"`
	Availability float64        `json:"availability"` // OK/Attempts
	ErrorClasses map[string]int `json:"error_classes,omitempty"`

	DurMSP50 int64 `json:"dur_ms_p50"`
	DurMSP95 int64 `json:"dur_ms_p95"`

	durs []int64
}

// Build reads the given audit JSONL files and aggregates them.
func Build(paths []string, now time.Time) (*Report, error) {
	rep := &Report{Meta: Meta{Format: Format, GeneratedAt: now.Format(time.RFC3339), Inputs: paths}}
	rows := map[string]*Row{}
	eps := map[string]*EndpointRow{}
	hours := map[string]*HourRow{}
	var from, to time.Time

	for _, path := range paths {
		rc, err := openAuditFile(path)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(rc)
		// One line can hold several bodies, each capped at max_body_mb
		// (default 8MiB) — size the scanner generously.
		sc.Buffer(make([]byte, 1<<20), 128<<20)
		for sc.Scan() {
			var rec audit.Record
			if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
				rep.Meta.ParseErrors++
				continue
			}
			rep.Meta.Records++
			if from.IsZero() || rec.TS.Before(from) {
				from = rec.TS
			}
			if rec.TS.After(to) {
				to = rec.TS
			}
			date := rec.TS.Format("2006-01-02")

			model := rec.Model
			if model == "" {
				model = "(rejected)" // failed before model parsing: auth/413/bad json
			}
			// Protocol is part of the grain: the same virtual-model name
			// may exist in both protocol groups and those are two models.
			key := date + "\x00" + rec.Protocol + "\x00" + model
			row, ok := rows[key]
			if !ok {
				row = &Row{Date: date, Model: model, Protocol: rec.Protocol}
				rows[key] = row
			}
			var usage Usage
			usageOK := false
			if rec.Client.Response != nil {
				usage, usageOK = ExtractUsage(rec.Client.Response.Body)
			}
			addRecord(row, &rec, usage, usageOK)

			hk := fmt.Sprintf("%s\x00%02d", date, rec.TS.Hour())
			hr, ok := hours[hk]
			if !ok {
				hr = &HourRow{Date: date, Hour: rec.TS.Hour()}
				hours[hk] = hr
			}
			addHour(hr, &rec, usage, usageOK)

			for _, a := range rec.Attempts {
				k := date + "\x00" + a.Endpoint
				ep, ok := eps[k]
				if !ok {
					ep = &EndpointRow{Date: date, Endpoint: a.Endpoint, ErrorClasses: map[string]int{}}
					eps[k] = ep
				}
				addAttempt(ep, &a)
			}
		}
		rc.Close()
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}

	if !from.IsZero() {
		rep.Meta.From, rep.Meta.To = from.Format(time.RFC3339), to.Format(time.RFC3339)
	}
	for _, r := range rows {
		finishRow(r)
		rep.Rows = append(rep.Rows, *r)
	}
	for _, e := range eps {
		finishEndpoint(e)
		rep.Endpoints = append(rep.Endpoints, *e)
	}
	for _, h := range hours {
		rep.Hours = append(rep.Hours, *h)
	}
	sort.Slice(rep.Hours, func(i, j int) bool {
		if rep.Hours[i].Date != rep.Hours[j].Date {
			return rep.Hours[i].Date < rep.Hours[j].Date
		}
		return rep.Hours[i].Hour < rep.Hours[j].Hour
	})
	sort.Slice(rep.Rows, func(i, j int) bool {
		a, b := rep.Rows[i], rep.Rows[j]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Protocol < b.Protocol
	})
	sort.Slice(rep.Endpoints, func(i, j int) bool {
		if rep.Endpoints[i].Date != rep.Endpoints[j].Date {
			return rep.Endpoints[i].Date < rep.Endpoints[j].Date
		}
		return rep.Endpoints[i].Endpoint < rep.Endpoints[j].Endpoint
	})
	return rep, nil
}

// openAuditFile opens an audit JSONL file, transparently decompressing it if
// the audit package's housekeeping sweep (internal/audit/housekeep.go) has
// since rotated it into a .zst — historical and live files can be mixed in
// the same glob without the caller caring which is which.
func openAuditFile(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(path, ".zst") {
		return f, nil
	}
	dec, err := zstd.NewReader(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return zstdReadCloser{dec, f}, nil
}

// zstdReadCloser adapts *zstd.Decoder — whose Close takes no error and
// doesn't own the underlying reader — to io.ReadCloser over the file it
// reads from.
type zstdReadCloser struct {
	dec *zstd.Decoder
	f   *os.File
}

func (z zstdReadCloser) Read(p []byte) (int, error) { return z.dec.Read(p) }

func (z zstdReadCloser) Close() error {
	z.dec.Close()
	return z.f.Close()
}

func addRecord(row *Row, rec *audit.Record, usage Usage, usageOK bool) {
	row.Requests++
	switch rec.Outcome {
	case "ok":
		row.OK++
	case "canceled":
		row.Canceled++
	default:
		row.Errors++
	}
	if rec.Stream {
		row.Streams++
	}
	row.Attempts += len(rec.Attempts)
	if len(rec.Attempts) > 1 {
		row.Fallbacks++
	}

	row.BytesIn += bodyBytes(rec.Client.Request.Body)
	if rec.Client.Response != nil {
		row.BytesOut += bodyBytes(rec.Client.Response.Body)
	}

	if n, ok := messageCount(rec.Client.Request.Body); ok {
		row.Messages += int64(n)
		row.MessagesKnown++
		for role, c := range roleChars(rec.Client.Request.Body) {
			if row.RoleChars == nil {
				row.RoleChars = map[string]int64{}
			}
			row.RoleChars[role] += c
		}
	}
	if rec.TTFTMS > 0 {
		row.TTFTMSSum += rec.TTFTMS
		row.TTFTKnown++
		row.ttfts = append(row.ttfts, rec.TTFTMS)
	}

	if row.FinishReasons == nil {
		row.FinishReasons = map[string]int{}
	}
	var finish string
	if rec.Client.Response != nil {
		finish = extractFinish(rec.Client.Response.Body)
	}
	row.FinishReasons[finish]++
	if rec.Outcome == "ok" {
		for _, a := range rec.Attempts {
			if strings.HasPrefix(a.Error, "truncated") {
				row.Truncated++
				break
			}
		}
	}

	row.DurMSSum += rec.DurMS
	if rec.DurMS > row.DurMSMax {
		row.DurMSMax = rec.DurMS
	}
	row.durs = append(row.durs, rec.DurMS)
	if rec.DurMS > 0 {
		row.bytesDurMS += rec.DurMS
	}

	if usageOK {
		row.TokensIn += usage.In
		row.TokensOut += usage.Out
		row.TokensInCached += usage.CacheRead
		row.TokensInCacheWrite += usage.CacheWrite
		row.TokensReasoning += usage.Reasoning
		row.TokensKnown++
		if rec.DurMS > 0 {
			row.tokDurMS += rec.DurMS
		}
	}
}

func addHour(h *HourRow, rec *audit.Record, usage Usage, usageOK bool) {
	h.Requests++
	switch rec.Outcome {
	case "ok":
		h.OK++
	case "canceled":
	default:
		h.Errors++
	}
	if rec.Client.Response != nil {
		h.BytesOut += bodyBytes(rec.Client.Response.Body)
	}
	if usageOK {
		h.TokensIn += usage.In
		h.TokensInCached += usage.CacheRead
		h.TokensOut += usage.Out
	}
}

func addAttempt(ep *EndpointRow, a *audit.Attempt) {
	ep.Attempts++
	if a.Error == "" && a.Response != nil && a.Response.Status < 400 {
		ep.OK++
	} else {
		ep.Failed++
		cls := a.Error
		if cls == "" {
			cls = "unknown"
		}
		// Detail-carrying errors ("network: dial tcp …", "truncated: …")
		// are bucketed by their prefix so the class table stays bounded.
		if i := strings.IndexByte(cls, ':'); i > 0 {
			cls = cls[:i]
		}
		ep.ErrorClasses[cls]++
	}
	ep.durs = append(ep.durs, a.DurMS)
}

func finishRow(r *Row) {
	r.DurMSP50, r.DurMSP95 = percentiles(r.durs)
	r.TTFTMSP50, r.TTFTMSP95 = percentiles(r.ttfts)
	if r.tokDurMS > 0 {
		r.TokOutPerSec = round2(float64(r.TokensOut) / (float64(r.tokDurMS) / 1000))
	}
	if r.bytesDurMS > 0 {
		r.BytesOutPerSec = round2(float64(r.BytesOut) / (float64(r.bytesDurMS) / 1000))
	}
	r.durs, r.ttfts = nil, nil
}

func finishEndpoint(e *EndpointRow) {
	e.DurMSP50, e.DurMSP95 = percentiles(e.durs)
	if e.Attempts > 0 {
		e.Availability = round2(float64(e.OK) / float64(e.Attempts))
	}
	if len(e.ErrorClasses) == 0 {
		e.ErrorClasses = nil
	}
	e.durs = nil
}

// bodyBytes sizes a recorded body: JSON bodies by re-serialization, string
// bodies (SSE etc.) by length. Truncated bodies undercount; that matches
// what was recorded.
func bodyBytes(body any) int64 {
	switch b := body.(type) {
	case nil:
		return 0
	case string:
		return int64(len(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return 0
		}
		return int64(len(raw))
	}
}

// percentiles returns nearest-rank p50 and p95.
func percentiles(durs []int64) (p50, p95 int64) {
	if len(durs) == 0 {
		return 0, 0
	}
	s := append([]int64(nil), durs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	rank := func(p float64) int64 {
		i := int(p*float64(len(s))+0.5) - 1
		if i < 0 {
			i = 0
		}
		if i >= len(s) {
			i = len(s) - 1
		}
		return s[i]
	}
	return rank(0.50), rank(0.95)
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}
