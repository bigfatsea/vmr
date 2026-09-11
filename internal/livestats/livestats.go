package livestats

import (
	"strings"
	"time"
)

// Outcome values, verbatim what the server maps onto a Sample. The strings
// match the audit record's outcome without this package importing audit.
const (
	OutcomeOK       = "ok"
	OutcomeError    = "error"
	OutcomeCanceled = "canceled"
)

// ringCap is the per-(provider, key_label, model) latency-window
// capacity: last_100 with no room to spare (design §3.4). Stream and
// non-stream samples share this one ring.
const ringCap = 100

// rollupRetentionDays is the in-memory rollup window (design §8): the last
// this-many whole calendar days plus the current partial one. Startup only
// loads rollup rows inside it; each day roll evicts anything older. The
// rollup FILE still keeps every row — only the memory image, and therefore
// the read-time fold behind every /stats call, is bounded to a constant
// instead of growing with deployment age. 7 days == the widest /stats view
// (?range=7d), so memory holds exactly what the console can show; deeper
// history is `vmr analyze`'s job (it reads the audit log, not this file).
const rollupRetentionDays = 7

// HourlyTailDefault is the /stats hourly[] tail when no ?range= narrows it
// (design §8): the last 48 distinct hours with data. dailyTail bounds
// daily[] — the window spans rollupRetentionDays whole days plus today, and
// nothing older is held anyway, so +1 keeps every day that is in memory.
const (
	HourlyTailDefault = 48
	dailyTail         = rollupRetentionDays + 1
)

// recentErrCap bounds the in-memory recent_errors ring (design §8.1).
// Keeps at most 100 errors within the last 24 hours.
const recentErrCap = 100

// snapCacheTTL bounds how stale CachedSnapshot may be. The console Overview
// poller drives /stats every ~2s while requests are in flight (plus the
// whole-page 5-minute refresh, plus any external monitor); without a cache
// each poll would hold the aggregator mutex through a full O(rollup) fold,
// contending with the completion hook. The TTL sits above the poller's
// cadence on purpose — a poll then usually reuses the last fold, so the
// steady-state cost during active traffic is one fold every few seconds,
// not one per poll. Bounded staleness is harmless for a monitor, and it
// only touches the historical aggregate: /stats computes in-flight and
// concurrency fresh on every read, outside this cache.
const snapCacheTTL = 3 * time.Second

// TokenCounts is the raw four-way per-request token tally. Same field names
// as the audit record's token stamp, re-declared here: the slim/rollup key
// space quotes audit's names verbatim (design §3.1) but the import stays
// severed — the record's shape can change without touching this contract.
type TokenCounts struct {
	In         int64 `json:"in"`
	Out        int64 `json:"out"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

func (t *TokenCounts) add(o TokenCounts) {
	t.In += o.In
	t.Out += o.Out
	t.CacheRead += o.CacheRead
	t.CacheWrite += o.CacheWrite
}

// SumCount carries a sum with its sample count wherever a mean is wanted
// later (dur_ms/ttft_ms in rollup rows).
type SumCount struct {
	Sum int64 `json:"sum"`
	N   int64 `json:"n"`
}

// WindowBlock is the read-time portrait of one recent-sample window
// (contracts §1.2): the window's actual sample count, its four-way token
// totals, and nearest-rank ttft/toks percentiles. The only rate is toks —
// tps was revoked (design §8).
type WindowBlock struct {
	N       int64       `json:"n"`
	Tokens  TokenCounts `json:"tokens"`
	TTFTP50 int64       `json:"ttft_p50_ms"`
	TTFTP90 int64       `json:"ttft_p90_ms"`
	ToksP50 float64     `json:"toks_p50"`
	ToksP90 float64     `json:"toks_p90"`
}

// Sample is one completed request as the completion hook sees it. TS is the
// arrival time and decides the hour bucket (design §3.2). Provider, Model
// and KeyLabel are the winning attempt's service identity — all empty when
// the request never forwarded, in which case only the request-face outcome
// count is booked (design §4.2). TTFTMS 0 means unmeasured and is excluded
// from ttft sums and the ring. ErrorClass/Status/Attempt feed only the
// recent_errors ring (contracts §1.6) — memory-only, slim/rollup never
// carry them; ErrorClass/Status quote the terminal attempt verbatim.
type Sample struct {
	TS           time.Time
	VModel       string
	Protocol     string
	Stream       bool
	Outcome      string
	ClientKeyTag string
	Provider     string
	Model        string
	KeyLabel     string
	DurMS        int64
	TTFTMS       int64
	Tokens       TokenCounts
	ErrorClass   string
	Status       int
	Attempt      int
}

// Dims mirrors the rollup row's dims object — same key names, same order as
// the design's example (§3.3). vmodel is the one deliberately renamed field:
// the flat row carries both the virtual name and the upstream name, so the
// virtual one gets a distinct key (design §3.2).
type Dims struct {
	VModel       string `json:"vmodel"`
	Protocol     string `json:"protocol"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	KeyLabel     string `json:"key_label"`
	ClientKeyTag string `json:"client_key_tag"`
	Stream       bool   `json:"stream"`
}

// dimsKey is the memory-side grouping key (§3.3's dims tuple). Components
// are config-sourced names or fixed enums, so "\x1f" joining in id() is
// collision-proof in practice.
type dimsKey struct {
	vmodel, protocol, clientKeyTag, provider, model, keyLabel string
	stream                                                    bool
}

func (d Dims) key() dimsKey {
	return dimsKey{d.VModel, d.Protocol, d.ClientKeyTag, d.Provider, d.Model, d.KeyLabel, d.Stream}
}

func (k dimsKey) dims() Dims {
	return Dims{k.vmodel, k.protocol, k.provider, k.model, k.keyLabel, k.clientKeyTag, k.stream}
}

// id is a total order over dims for deterministic row output.
func (k dimsKey) id() string {
	return strings.Join([]string{
		k.vmodel, k.protocol, k.clientKeyTag, k.provider, k.model, k.keyLabel, streamBit(k.stream),
	}, "\x1f")
}

func (s Sample) key() dimsKey {
	return dimsKey{s.VModel, s.Protocol, s.ClientKeyTag, s.Provider, s.Model, s.KeyLabel, s.Stream}
}

func streamBit(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// Counters is the counting payload of one (hour × dims) group — the same
// shape a rollup row carries. Outcome counting is request-face (every
// sample); tokens/dur/ttft are service-face and only move for forwarded
// samples (design §4.2).
type Counters struct {
	OK       int64       `json:"ok"`
	Error    int64       `json:"error"`
	Canceled int64       `json:"canceled"`
	Tokens   TokenCounts `json:"tokens"`
	DurMS    SumCount    `json:"dur_ms"`
	TTFTMS   SumCount    `json:"ttft_ms"`
}

func (c *Counters) addSample(s Sample) {
	switch s.Outcome {
	case OutcomeCanceled:
		c.Canceled++
	case OutcomeOK:
		c.OK++
	default:
		c.Error++
	}
	if s.Provider == "" {
		return
	}
	c.Tokens.add(s.Tokens)
	c.DurMS.Sum += s.DurMS
	c.DurMS.N++
	if s.TTFTMS != 0 {
		c.TTFTMS.Sum += s.TTFTMS
		c.TTFTMS.N++
	}
}

func (c *Counters) add(o Counters) {
	c.OK += o.OK
	c.Error += o.Error
	c.Canceled += o.Canceled
	c.Tokens.add(o.Tokens)
	c.DurMS.Sum += o.DurMS.Sum
	c.DurMS.N += o.DurMS.N
	c.TTFTMS.Sum += o.TTFTMS.Sum
	c.TTFTMS.N += o.TTFTMS.N
}
