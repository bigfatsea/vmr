// Ver 2026-07-29 23:55, by Sonnet 5

// The report's data shape: every struct that Build fills in and that both
// renderers (vmr-report.md and vmr-report.json) read back out. Split out of
// aggregate.go so that file is about the aggregation pass itself — a new
// metric adds a field here and an accumulator there, without either concern
// having to be read through the other.
//
// These types ARE the vmr-report.json schema: the JSON tags below are the
// public contract, not an implementation detail.
package report

// Format is the aggregate report's JSON structure version. 10 continues the legacy
// sequence (9 = legacy report) and marks the redesigned layout.
const Format = 10

// SlowThresholdMS is the default "unbearably slow" cutoff for slow_requests
// (V2 C-family / F-family). 30s matches the V2 spec.
const SlowThresholdMS = 30_000

// Report2 is the top-level JSON output, mirroring the Markdown structure.
// Every aggregating bucket carries the derived fields that are cheap to
// compute during finish* (fresh tokens, cache_efficiency, slow_requests,
// true stream_ms percentiles) since the raw sums already exist then.
type Report2 struct {
	Meta         Meta            `json:"meta"`
	Overall      Row             `json:"overall"`
	ByModel      []Row           `json:"by_model"`
	ByDate       []Row           `json:"by_date"`
	Hours        []HourRow       `json:"hours,omitempty"`
	HoursOfDay   []HourRow       `json:"hours_of_day,omitempty"`
	Endpoints    []EndpointRow   `json:"endpoints"`
	EndpointsAll []EndpointRow   `json:"endpoints_all,omitempty"`
	ByClient     []ClientRow     `json:"by_client,omitempty"`
	Workloads    []WorkloadRow   `json:"workloads,omitempty"`
	Sessions     []SessionRow    `json:"sessions,omitempty"`
	Compactions  []CompactionRow `json:"compactions,omitempty"`
	Tools        []ToolShapeRow  `json:"tools,omitempty"`
	Efficiency   []Finding       `json:"efficiency,omitempty"`
	Sticky       *StickyEffect   `json:"sticky,omitempty"`
	Pricing      *Pricing        `json:"pricing,omitempty"`

	// requests is the per-request export (vmr-requests.jsonl). Unexported so
	// it stays OUT of vmr-report.json (which is aggregate-only); exposed via
	// RequestRows() for the jsonl writer + index renderer.
	requests []RequestRow
}

// Meta carries provenance + method notes consumed by the appendix.
type Meta struct {
	Format           int      `json:"format"`
	GeneratedAt      string   `json:"generated_at"`
	Inputs           []string `json:"inputs"`
	Records          int      `json:"records"`
	ParseErrors      int      `json:"parse_errors"`
	From             string   `json:"from,omitempty"`
	To               string   `json:"to,omitempty"`
	SlowThreshold    int      `json:"slow_threshold_ms"`
	PercentileMethod string   `json:"percentile_method"` // documented in appendix
}

// Row is the full-metric bucket: Overall, ByModel, ByDate. It carries
// families A (volume/outcome), B (token economics), C (latency), D (wire).
type Row struct {
	// identity
	Date     string `json:"date,omitempty"`
	Model    string `json:"model,omitempty"`
	Protocol string `json:"protocol,omitempty"`

	// A - volume & outcome
	Requests          int     `json:"requests"`
	OK                int     `json:"ok"`
	Errors            int     `json:"errors"`
	Canceled          int     `json:"canceled"`
	Fallbacks         int     `json:"fallbacks,omitempty"`          // requests needing >1 attempt
	FallbackRecovered int     `json:"fallback_recovered,omitempty"` // subset that ended ok
	FallbackFailed    int     `json:"fallback_failed,omitempty"`    // subset that ended error
	Truncated         int     `json:"truncated,omitempty"`          // ok but stream broke
	Streams           int     `json:"streams,omitempty"`            // stream:true count
	SuccessRate       float64 `json:"success_rate"`

	// B - token economics
	TokensIn           int64   `json:"tokens_in"`
	TokensInCached     int64   `json:"tokens_in_cached"`
	TokensInCacheWrite int64   `json:"tokens_in_cache_write,omitempty"`
	TokensInFresh      int64   `json:"tokens_in_fresh"` // derived: in - cached - cache_write
	TokensOut          int64   `json:"tokens_out"`
	TokensReasoning    int64   `json:"tokens_reasoning,omitempty"`
	TokensKnown        int     `json:"tokens_known,omitempty"`     // basis for B ratios
	CacheHitRate       float64 `json:"cache_hit_rate,omitempty"`   // cached/in
	CacheEfficiency    float64 `json:"cache_efficiency,omitempty"` // cached/(cached+fresh) - the lever
	ReasoningShare     float64 `json:"reasoning_share,omitempty"`

	// C - latency & speed (true per-bucket percentiles)
	TTFTKnown       int     `json:"ttft_known,omitempty"`
	TTFTMSSum       int64   `json:"ttft_ms_sum,omitempty"`
	TTFTMSP50       int64   `json:"ttft_ms_p50,omitempty"`
	TTFTMSP95       int64   `json:"ttft_ms_p95,omitempty"`
	RequestsWithDur int     `json:"requests_with_dur,omitempty"`
	DurMSSum        int64   `json:"dur_ms_sum,omitempty"`
	DurMSP50        int64   `json:"dur_ms_p50,omitempty"`
	DurMSP95        int64   `json:"dur_ms_p95,omitempty"`
	DurMSMax        int64   `json:"dur_ms_max,omitempty"`
	StreamKnown     int     `json:"stream_known,omitempty"`  // basis = records with BOTH dur>0 and ttft>0
	StreamMSP50     int64   `json:"stream_ms_p50,omitempty"` // true p50 of (dur-ttft)
	StreamMSP95     int64   `json:"stream_ms_p95,omitempty"`
	SlowRequests    int     `json:"slow_requests,omitempty"` // count(dur > SlowThresholdMS)
	TokOutPerSec    float64 `json:"tok_out_per_sec,omitempty"`

	// D - wire & payload
	BytesIn          int64            `json:"bytes_in,omitempty"`
	BytesOut         int64            `json:"bytes_out,omitempty"`
	Messages         int64            `json:"messages,omitempty"`
	MessagesKnown    int              `json:"messages_known,omitempty"`
	RoleChars        map[string]int64 `json:"role_chars,omitempty"`
	RoleTokens       map[string]int64 `json:"role_tokens,omitempty"`
	Images           int              `json:"images,omitempty"`
	ImagesCompressed int              `json:"images_compressed,omitempty"`

	// H - cost (only when pricing configured)
	CostEstimate *float64 `json:"cost_estimate,omitempty"`

	// working state (not serialized)
	durs, ttfts, streamMS []int64
	tokDurMS              int64
}

// HourRow is the (date, local-hour) and hour-of-day bucket: A+B+C (+D in JSON).
type HourRow struct {
	Date string `json:"date,omitempty"`
	Hour int    `json:"hour"`

	Requests  int `json:"requests"`
	OK        int `json:"ok"`
	Errors    int `json:"errors"`
	Fallbacks int `json:"fallbacks,omitempty"`
	Truncated int `json:"truncated,omitempty"`

	TokensIn           int64   `json:"tokens_in"`
	TokensInCached     int64   `json:"tokens_in_cached"`
	TokensInCacheWrite int64   `json:"tokens_in_cache_write,omitempty"`
	TokensInFresh      int64   `json:"tokens_in_fresh"`
	TokensOut          int64   `json:"tokens_out"`
	TokensKnown        int     `json:"tokens_known,omitempty"`
	CacheEfficiency    float64 `json:"cache_efficiency,omitempty"`

	TTFTKnown       int   `json:"ttft_known,omitempty"`
	TTFTMSP50       int64 `json:"ttft_ms_p50,omitempty"`
	TTFTMSP95       int64 `json:"ttft_ms_p95,omitempty"`
	RequestsWithDur int   `json:"requests_with_dur,omitempty"`
	DurMSP50        int64 `json:"dur_ms_p50,omitempty"`
	DurMSP95        int64 `json:"dur_ms_p95,omitempty"`
	DurMSMax        int64 `json:"dur_ms_max,omitempty"`
	StreamKnown     int   `json:"stream_known,omitempty"`
	StreamMSP95     int64 `json:"stream_ms_p95,omitempty"`
	SlowRequests    int   `json:"slow_requests,omitempty"`

	BytesIn  int64 `json:"bytes_in,omitempty"`
	BytesOut int64 `json:"bytes_out,omitempty"`
	Images   int   `json:"images,omitempty"`

	durs, ttfts, streamMS []int64
}

// EndpointRow is the (date, endpoint) and endpoint-only bucket: G + B + C.
type EndpointRow struct {
	Date     string `json:"date,omitempty"`
	Endpoint string `json:"endpoint"` // provider/real-model (or provider:real-model label)

	// G - endpoint health
	Attempts     int            `json:"attempts"`
	OK           int            `json:"ok"`
	Failed       int            `json:"failed"`
	Availability float64        `json:"availability"`
	ErrorRate    float64        `json:"error_rate,omitempty"` // failed/attempts × 100
	ErrorClasses map[string]int `json:"error_classes,omitempty"`

	// B/C - only for the requests this endpoint actually served
	Requests           int     `json:"requests,omitempty"`     // requests this endpoint actually served (request-level, ≠ Attempts)
	RequestsOK         int     `json:"requests_ok,omitempty"`  // subset with overall outcome "ok"
	SuccessRate        float64 `json:"success_rate,omitempty"` // RequestsOK/Requests - request-level, distinct from Availability (attempt-level)
	TokensIn           int64   `json:"tokens_in,omitempty"`
	TokensInCached     int64   `json:"tokens_in_cached,omitempty"`
	TokensInCacheWrite int64   `json:"tokens_in_cache_write,omitempty"`
	TokensInFresh      int64   `json:"tokens_in_fresh,omitempty"`
	TokensOut          int64   `json:"tokens_out,omitempty"`
	TokensReasoning    int64   `json:"tokens_reasoning,omitempty"`
	TokensKnown        int     `json:"tokens_known,omitempty"`
	CacheEfficiency    float64 `json:"cache_efficiency,omitempty"`
	TTFTKnown          int     `json:"ttft_known,omitempty"`
	TTFTMSP50          int64   `json:"ttft_ms_p50,omitempty"`
	TTFTMSP95          int64   `json:"ttft_ms_p95,omitempty"`
	RequestsWithDur    int     `json:"requests_with_dur,omitempty"`
	DurMSP50           int64   `json:"dur_ms_p50,omitempty"`
	DurMSP95           int64   `json:"dur_ms_p95,omitempty"`
	DurMSMax           int64   `json:"dur_ms_max,omitempty"`
	StreamKnown        int     `json:"stream_known,omitempty"`
	StreamMSP95        int64   `json:"stream_ms_p95,omitempty"`
	SlowRequests       int     `json:"slow_requests,omitempty"`
	TokOutPerSec       float64 `json:"tok_out_per_sec,omitempty"`
	DurMSSum           int64   `json:"dur_ms_sum,omitempty"`
	// WastedMS is wall-clock spent on this endpoint's FAILED attempts —
	// tries that produced nothing and forced the request onward to another
	// endpoint. Attempt-level (not request-level) like the other G-family
	// fields above it.
	//
	// Time, deliberately not money: vmr extracts token usage only from the
	// response the client actually received, and a failed attempt has none
	// (nor do providers generally bill for one). Reporting a currency
	// figure here would be an invention; the wall-clock is measured.
	WastedMS int64 `json:"wasted_ms,omitempty"`

	// per-request input/output token percentiles (⭐ derived, from Usage.In/Out)
	InTokP50  int64 `json:"in_tok_p50,omitempty"`
	InTokP95  int64 `json:"in_tok_p95,omitempty"`
	OutTokP50 int64 `json:"out_tok_p50,omitempty"`
	OutTokP95 int64 `json:"out_tok_p95,omitempty"`

	// H - cost (only when pricing configured)
	CostEstimate *float64 `json:"cost_estimate,omitempty"`

	durs, ttfts, streamMS, inToks, outToks []int64
}

// ClientRow is the by-client_key_tag bucket (new in the summary): A+B+C.
type ClientRow struct {
	ClientKey   string  `json:"client_key"`
	Requests    int     `json:"requests"`
	OK          int     `json:"ok"`
	Errors      int     `json:"errors"`
	SuccessRate float64 `json:"success_rate"`

	TokensIn           int64   `json:"tokens_in"`
	TokensInCached     int64   `json:"tokens_in_cached"`
	TokensInCacheWrite int64   `json:"tokens_in_cache_write,omitempty"`
	TokensInFresh      int64   `json:"tokens_in_fresh"`
	TokensOut          int64   `json:"tokens_out"`
	TokensReasoning    int64   `json:"tokens_reasoning,omitempty"`
	TokensKnown        int     `json:"tokens_known,omitempty"`
	CacheEfficiency    float64 `json:"cache_efficiency"`

	RequestsWithDur int   `json:"requests_with_dur,omitempty"`
	DurMSP50        int64 `json:"dur_ms_p50,omitempty"`
	DurMSP95        int64 `json:"dur_ms_p95,omitempty"`
	SlowRequests    int   `json:"slow_requests,omitempty"`

	// per-request input/output token percentiles (⭐ derived, from Usage.In/Out)
	InTokP50  int64 `json:"in_tok_p50,omitempty"`
	InTokP95  int64 `json:"in_tok_p95,omitempty"`
	OutTokP50 int64 `json:"out_tok_p50,omitempty"`
	OutTokP95 int64 `json:"out_tok_p95,omitempty"`

	// H - cost (only when pricing configured)
	CostEstimate *float64 `json:"cost_estimate,omitempty"`

	durs, ttfts, streamMS, inToks, outToks []int64
}

// WorkloadRow splits traffic by workload class: A+B+C+E(tool_call_rate).
type WorkloadRow struct {
	Class    string `json:"class"`
	Requests int    `json:"requests"`

	TokensIn           int64   `json:"tokens_in"`
	TokensInCached     int64   `json:"tokens_in_cached"`
	TokensInCacheWrite int64   `json:"tokens_in_cache_write,omitempty"`
	TokensInFresh      int64   `json:"tokens_in_fresh"`
	TokensOut          int64   `json:"tokens_out"`
	TokensKnown        int     `json:"tokens_known,omitempty"`
	CacheEfficiency    float64 `json:"cache_efficiency"`

	RequestsWithDur int   `json:"requests_with_dur,omitempty"`
	DurMSP50        int64 `json:"dur_ms_p50,omitempty"`
	DurMSP95        int64 `json:"dur_ms_p95,omitempty"`
	SlowRequests    int   `json:"slow_requests,omitempty"`

	ToolCalls             int     `json:"tool_calls,omitempty"`
	RequestsWithToolCalls int     `json:"requests_with_tool_calls,omitempty"`
	ToolCallRate          float64 `json:"tool_call_rate,omitempty"`

	durs, streamMS []int64
}

// SessionRow is the per-session drill-down (§6 Sessions & Tasks): no latency columns in
// Markdown, but the data stays in JSON (P6). context_growth = last/first turn.
type SessionRow struct {
	ID            string `json:"id"`
	Title         string `json:"title,omitempty"`
	Class         string `json:"class,omitempty"`
	ClientKey     string `json:"client_key,omitempty"`
	ContinuedFrom string `json:"continued_from,omitempty"`
	Requests      int    `json:"requests"`
	Tasks         int    `json:"tasks"`
	OK            int    `json:"ok,omitempty"`
	Errors        int    `json:"errors,omitempty"`
	Fallbacks     int    `json:"fallbacks,omitempty"`
	Truncated     int    `json:"truncated,omitempty"`
	From          string `json:"from"`
	To            string `json:"to"`

	TokensIn           int64   `json:"tokens_in"`
	TokensInCached     int64   `json:"tokens_in_cached,omitempty"`
	TokensInCacheWrite int64   `json:"tokens_in_cache_write,omitempty"`
	TokensInFresh      int64   `json:"tokens_in_fresh"`
	TokensOut          int64   `json:"tokens_out"`
	TokensKnown        int     `json:"tokens_known,omitempty"`
	CacheEfficiency    float64 `json:"cache_efficiency,omitempty"`

	// latency kept in JSON for P6 completeness, not shown in MD
	TTFTKnown       int   `json:"ttft_known,omitempty"`
	TTFTMSP95       int64 `json:"ttft_ms_p95,omitempty"`
	RequestsWithDur int   `json:"requests_with_dur,omitempty"`
	DurMSP95        int64 `json:"dur_ms_p95,omitempty"`
	DurMSMax        int64 `json:"dur_ms_max,omitempty"`

	RoleChars map[string]int64 `json:"role_chars,omitempty"`
	Images    int              `json:"images,omitempty"`

	// E - workload shape
	ContextGrowth   float64  `json:"context_growth,omitempty"`   // last_in / first_in
	CompactionChain []string `json:"compaction_chain,omitempty"` // session ids, head->current

	durs, ttfts []int64
}

// CompactionRow is one standalone compaction LLM call (CCR N-4): a
// report-only, body-sniffed concept (ReqInfo.Compaction) distinct from the
// anchor-gluing-style structural session splits SessionRow.ContinuedFrom
// already covers — this row is specifically about
// an OBSERVABLE call that consumed tokens to produce a summary, not an
// in-place history rebuild with no separate request. "Before/after" here is
// the compaction call's OWN input/output (how much history it was asked to
// compress vs how big the resulting summary is), not either neighboring
// session's own token counts.
type CompactionRow struct {
	TS          string `json:"ts"`
	Summarizes  string `json:"summarizes,omitempty"`   // predecessor session id
	ContinuesTo string `json:"continues_to,omitempty"` // successor session id

	TokensIn  int64 `json:"tokens_in"`  // history fed into the compaction call
	TokensOut int64 `json:"tokens_out"` // the resulting summary

	// Entities found in the compaction call's own input (the conversation
	// being condensed) via chatmsg.ExtractEntities, split by whether they're
	// still mentioned in its output (the summary) — a rule-based, "宁可粗糙也
	// 不猜语义" proxy for information loss.
	SwallowedEntities []string `json:"swallowed_entities,omitempty"`
	SurvivedEntities  []string `json:"survived_entities,omitempty"`
}

// ToolShapeRow is per declared-tool-set waste (§7 Efficiency & Waste's Top-N tool shapes): F-family.
type ToolShapeRow struct {
	Shape         string         `json:"shape"`
	Requests      int            `json:"requests"`
	Declared      []string       `json:"declared"`
	DeclaredBytes int64          `json:"declared_bytes"` // per-request schema bytes
	Calls         map[string]int `json:"calls,omitempty"`
	NeverCalled   []string       `json:"never_called,omitempty"`
	// derived (F-family)
	SchemaBytesShipped int64   `json:"schema_bytes_shipped"` // declared_bytes × requests
	DistinctCalled     int     `json:"distinct_called"`
	DeclareUtilization float64 `json:"declare_utilization"` // distinct_called / declared
	SchemaWasteBytes   int64   `json:"schema_waste_bytes"`  // shipped × (1 - utilization)
}

// Finding is one row of the §7 efficiency/waste table.
type Finding struct {
	// Code is a stable, English, never-localized identifier — the only
	// thing tests or any programmatic consumer of vmr-report.json should
	// key off (never Finding below, which is display text).
	Code FindingCode `json:"code"`
	// Finding/Value/Implicated/Action are narrative text. They are always
	// English in this persisted struct, regardless of report language —
	// buildFindings is always called with i18n.EN to populate Report2.
	// A localized copy for Markdown rendering is produced separately, by
	// calling buildFindings again with the target language; it is never
	// derived from this struct after the fact.
	Finding    string `json:"finding"`
	Metric     string `json:"metric"`
	Value      string `json:"value"`
	Implicated string `json:"implicated,omitempty"`
	Action     string `json:"action,omitempty"`
}

// FindingCode identifies which §7 finding a row is, independent of its
// (localized) display text. See Finding.Code.
type FindingCode string

const (
	FindingToolSchemaWaste  FindingCode = "tool_schema_waste"
	FindingCacheMiss        FindingCode = "cache_miss"
	FindingCronRedundancy   FindingCode = "cron_redundancy"
	FindingOutputTruncation FindingCode = "output_truncation"
	FindingSlowRequests     FindingCode = "slow_requests"
	FindingContextGrowth    FindingCode = "context_growth"
)

// RequestRow is one line of vmr-requests.jsonl: the per-request drill-down
// backing the redesigned index (§8 Request Detail Index). Every field is rule-extracted;
// unavailable signals are omitted rather than fabricated.
type RequestRow struct {
	TS             string  `json:"ts"`
	Session        string  `json:"session,omitempty"`
	Task           string  `json:"task,omitempty"`
	Turn           int     `json:"turn,omitempty"`         // within task
	SessTurn       int     `json:"session_turn,omitempty"` // within session
	Model          string  `json:"model,omitempty"`
	Protocol       string  `json:"protocol,omitempty"`
	Outcome        string  `json:"outcome"`
	ClientKey      string  `json:"client_key,omitempty"`
	Endpoint       string  `json:"endpoint,omitempty"`
	Finish         string  `json:"finish,omitempty"`
	DurMS          int64   `json:"dur_ms,omitempty"`
	TTFTMS         int64   `json:"ttft_ms,omitempty"`
	Msgs           int     `json:"msgs,omitempty"`
	TokensIn       int64   `json:"tokens_in,omitempty"`
	TokensInCached int64   `json:"tokens_in_cached,omitempty"`
	TokensInFresh  int64   `json:"tokens_in_fresh,omitempty"`
	TokensOut      int64   `json:"tokens_out,omitempty"`
	CacheEff       float64 `json:"cache_eff,omitempty"`
	Fallbacks      int     `json:"fallbacks,omitempty"`
	Truncated      bool    `json:"truncated,omitempty"`
	ErrorClass     string  `json:"error_class,omitempty"`
	DetailFile     string  `json:"detail_file,omitempty"`
	Title          string  `json:"title,omitempty"` // session/task title (index-only)

	Path string `json:"-"`
	Line int    `json:"-"`
}

// RequestRows returns the per-request export rows (populated by Build).
// StickyEffect quantifies what Sticky Model is actually
// buying. Sticky exists to keep an upstream prompt cache warm by sending a
// conversation back to the endpoint that last served it — but until now
// nothing proved it worked, and prompt cache is the single largest cost
// lever in agent workloads (a hit is free or near-free at every provider).
//
// Measured by OUTCOME, not by mechanism: a request is compared against the
// previous request of the same session, and classified by whether it landed
// on the same endpoint. That needs no new audit field, and — more
// importantly — it measures the thing that actually matters (endpoint
// continuity) rather than whether a particular code path ran. A sticky
// pointer that fired but landed somewhere cold would still count as a
// switch here, which is the honest answer.
//
// What this deliberately does NOT claim: WHY a request switched endpoints.
// Sticky TTL expiry, a health cooldown, a condition eliminating the sticky
// pick, or sticky simply being off for that model are indistinguishable
// after the fact. The report says what happened, not why.
type StickyEffect struct {
	// Continued: same endpoint as the session's previous request.
	// Switched: a different one. First: the session's opening request,
	// counted but excluded from both — it has no predecessor, so its cache
	// state says nothing about continuity either way.
	Continued StickyGroup `json:"continued"`
	Switched  StickyGroup `json:"switched"`
	First     int         `json:"first_in_session"`
	// Ungrouped counts records that never got a session id (see session.go):
	// no predecessor is knowable for them at all.
	Ungrouped int              `json:"ungrouped,omitempty"`
	ByModel   []StickyModelRow `json:"by_model,omitempty"`
}

// StickyGroup is one side of the comparison.
type StickyGroup struct {
	Requests           int     `json:"requests"`
	TokensKnown        int     `json:"tokens_known"`
	TokensInCached     int64   `json:"tokens_in_cached"`
	TokensInFresh      int64   `json:"tokens_in_fresh"`
	TokensInCacheWrite int64   `json:"tokens_in_cache_write,omitempty"`
	CacheEfficiency    float64 `json:"cache_efficiency"`
}

// StickyModelRow is the same comparison per virtual model — the actionable
// cut, because `sticky` is configured per virtual model: a model whose two
// groups show no gap is one whose stickiness is not paying for itself.
type StickyModelRow struct {
	Model     string      `json:"model"`
	Protocol  string      `json:"protocol"`
	Continued StickyGroup `json:"continued"`
	Switched  StickyGroup `json:"switched"`
}

func (r *Report2) RequestRows() []RequestRow { return r.requests }
