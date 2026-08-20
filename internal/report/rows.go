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

import (
	"time"

	"vmr/internal/core"
)

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
	Meta            Meta                `json:"meta"`
	Overall         Row                 `json:"overall"`
	ByModel         []Row               `json:"by_model"`
	ByDate          []Row               `json:"by_date"`
	Hours           []HourRow           `json:"hours,omitempty"`
	HoursOfDay      []HourRow           `json:"hours_of_day,omitempty"`
	Endpoints       []EndpointRow       `json:"endpoints"`
	EndpointsAll    []EndpointRow       `json:"endpoints_all,omitempty"`
	ByClient        []ClientRow         `json:"by_client,omitempty"`
	Workloads       []WorkloadRow       `json:"workloads,omitempty"`
	Sessions        []SessionRow        `json:"sessions,omitempty"`
	Compactions     []CompactionRow     `json:"compactions,omitempty"`
	Tools           []ToolShapeRow      `json:"tools,omitempty"`
	Efficiency      []Finding           `json:"efficiency,omitempty"`
	Sticky          *StickyEffect       `json:"sticky,omitempty"`
	Providers       []ProviderRow       `json:"providers,omitempty"`
	ProviderQuotas  []ProviderQuotaRow  `json:"provider_quotas,omitempty"`
	ClientEndpoints []ClientEndpointRow `json:"client_endpoints,omitempty"`
	Pricing         *Pricing            `json:"pricing,omitempty"`

	// requests is the per-request export (vmr-requests.json). Unexported so
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
	// DetailsEnabled records whether this run wrote details/*.md (the
	// -details flag) — §8's link line (P6.2b) reads it to decide between
	// "here are the links" and "here's how to fetch one on demand"
	// (`vmr replay -print -req <coord>`), since the default run no longer
	// produces a details/ to link to (P3.3).
	DetailsEnabled bool `json:"details_enabled,omitempty"`
	// SelfTrafficExcluded is how many records this run's self-traffic
	// exclusion (P6.4) skipped from every aggregation bucket — vmr
	// story's own -llm-addr calls routed back through this instance, not
	// the workload actually being analyzed. Counted, never silently
	// dropped; 0 when no exclusion tags were configured or none matched.
	SelfTrafficExcluded int `json:"self_traffic_excluded,omitempty"`

	// QuotaJSONPath/QuotaInputOutsideLogDir are cmd_report.go's
	// composition-root facts about §2.5's live-quota counter, set on rep
	// AFTER Build/BuildCached returns (report itself never imports config,
	// so it can't compute either of these) — see cmdReport's own comment
	// at the call site. Both zero-valued when the sub-table has nothing to
	// render (no point naming a source for a table that isn't shown).
	// QuotaJSONPath is the <log_dir>/vmr-quota.json path the live column's
	// numbers actually came from — named explicitly so a reader can judge
	// whether it's plausibly the same instance that produced the audit
	// logs Inputs lists, not just implicitly "some file somewhere".
	QuotaJSONPath string `json:"quota_json_path,omitempty"`
	// QuotaInputOutsideLogDir is true when NONE of Inputs resolves under
	// the directory QuotaJSONPath sits in — the cheap single-machine
	// signal for "these audit logs and this live quota counter probably
	// belong to two different vmr instances" (e.g. analyzing a colleague's
	// copied-over logs against this machine's own healthy counter).
	QuotaInputOutsideLogDir bool `json:"quota_input_outside_log_dir,omitempty"`
}

// TrafficStats is the token/latency/volume core Row, HourRow, ClientRow,
// WorkloadRow, and SessionRow all accumulate the same way — one shared type
// instead of 5 near-identical accumulation closures.
// EndpointRow deliberately does NOT embed this: its own Requests/TokensIn/
// TokensOut/TokensInFresh are `omitempty` where these 5 types are not — an
// endpoint can have attempts with zero SERVED requests, a distinction this
// core has no room for. See EndpointRow's own comment and IngestAttempt/
// IngestRequest in ingest.go.
//
// Every embedding type's Ingest wraps this one (see ingest.go): call
// TrafficStats.Ingest first, then accumulate whatever fields are still its
// own (Fallbacks, TTFT/Stream percentiles, Bytes, Images, RoleChars, ...).
// TTFT/Stream stay OUTSIDE this core on purpose — the 5 types disagree too
// much on which of TTFTKnown/StreamKnown/their percentiles they expose
// (WorkloadRow has none, SessionRow has TTFT but no Stream, ClientRow
// collects raw ttfts but never surfaces a percentile from them) for a shared
// field set to be honest.
//
// Note for anyone changing a tag here: encoding/json flattens an embedded
// struct using ITS OWN tags, so every omitempty decision below applies to all
// 5 embedding types at once and changes their emitted JSON shape together.
type TrafficStats struct {
	Requests int `json:"requests"`
	OK       int `json:"ok"`
	Errors   int `json:"errors"`

	TokensIn           int64   `json:"tokens_in"`
	TokensInCached     int64   `json:"tokens_in_cached"`
	TokensInCacheWrite int64   `json:"tokens_in_cache_write,omitempty"`
	TokensInFresh      int64   `json:"tokens_in_fresh"` // derived: in - cached - cache_write
	TokensOut          int64   `json:"tokens_out"`
	TokensReasoning    int64   `json:"tokens_reasoning,omitempty"`
	TokensKnown        int     `json:"tokens_known,omitempty"`     // basis for B ratios
	CacheEfficiency    float64 `json:"cache_efficiency,omitempty"` // cached/(cached+fresh) - the lever

	RequestsWithDur int   `json:"requests_with_dur,omitempty"`
	DurMSP50        int64 `json:"dur_ms_p50,omitempty"`
	DurMSP95        int64 `json:"dur_ms_p95,omitempty"`
	SlowRequests    int   `json:"slow_requests,omitempty"` // count(dur > SlowThresholdMS)

	// working state (not serialized)
	durs []int64
}

// Row is the full-metric bucket: Overall, ByModel, ByDate. It carries
// families A (volume/outcome), B (token economics), C (latency), D (wire).
type Row struct {
	// identity
	Date     string `json:"date,omitempty"`
	Model    string `json:"model,omitempty"`
	Protocol string `json:"protocol,omitempty"`

	TrafficStats

	// A - volume & outcome (beyond the shared core)
	Canceled          int     `json:"canceled"`
	Fallbacks         int     `json:"fallbacks,omitempty"`          // requests needing >1 attempt
	FallbackRecovered int     `json:"fallback_recovered,omitempty"` // subset that ended ok
	FallbackFailed    int     `json:"fallback_failed,omitempty"`    // subset that ended error
	Truncated         int     `json:"truncated,omitempty"`          // ok but stream broke
	Streams           int     `json:"streams,omitempty"`            // stream:true count
	SuccessRate       float64 `json:"success_rate"`

	// B - token economics (beyond the shared core)
	CacheHitRate   float64 `json:"cache_hit_rate,omitempty"` // cached/in
	ReasoningShare float64 `json:"reasoning_share,omitempty"`

	// C - latency & speed (true per-bucket percentiles; beyond the shared core)
	TTFTKnown    int     `json:"ttft_known,omitempty"`
	TTFTMSSum    int64   `json:"ttft_ms_sum,omitempty"`
	TTFTMSP50    int64   `json:"ttft_ms_p50,omitempty"`
	TTFTMSP95    int64   `json:"ttft_ms_p95,omitempty"`
	DurMSSum     int64   `json:"dur_ms_sum,omitempty"`
	DurMSMax     int64   `json:"dur_ms_max,omitempty"`
	StreamKnown  int     `json:"stream_known,omitempty"`  // basis = records with BOTH dur>0 and ttft>0
	StreamMSP50  int64   `json:"stream_ms_p50,omitempty"` // true p50 of (dur-ttft)
	StreamMSP95  int64   `json:"stream_ms_p95,omitempty"`
	TokOutPerSec float64 `json:"tok_out_per_sec,omitempty"`

	// D - wire & payload
	//
	// No message-count field here on purpose: "messages"/"messages_known"
	// were declared with JSON tags from this file's first version and never
	// once written, so they could only ever render as absent (both were
	// omitempty). Per-request message counts do exist and are reported —
	// RequestRow.Msgs, from rec2.msgs — the bucket-level roll-up simply
	// never had an accumulator or a reader. A field in this file is a public
	// contract; one that cannot carry a value is a promise the report can't
	// keep.
	BytesIn          int64            `json:"bytes_in,omitempty"`
	BytesOut         int64            `json:"bytes_out,omitempty"`
	RoleChars        map[string]int64 `json:"role_chars,omitempty"`
	RoleTokens       map[string]int64 `json:"role_tokens,omitempty"`
	Images           int              `json:"images,omitempty"`
	ImagesCompressed int              `json:"images_compressed,omitempty"`

	// H - cost (only when pricing configured)
	CostEstimate *float64 `json:"cost_estimate,omitempty"`

	// working state (not serialized)
	ttfts, streamMS []int64
	tokDurMS        int64
}

// HourRow is the (date, local-hour) and hour-of-day bucket: A+B+C (+D in JSON).
type HourRow struct {
	Date string `json:"date,omitempty"`
	Hour int    `json:"hour"`

	TrafficStats

	Fallbacks int `json:"fallbacks,omitempty"`
	Truncated int `json:"truncated,omitempty"`

	TTFTKnown   int   `json:"ttft_known,omitempty"`
	TTFTMSP50   int64 `json:"ttft_ms_p50,omitempty"`
	TTFTMSP95   int64 `json:"ttft_ms_p95,omitempty"`
	DurMSMax    int64 `json:"dur_ms_max,omitempty"`
	StreamKnown int   `json:"stream_known,omitempty"`
	StreamMSP95 int64 `json:"stream_ms_p95,omitempty"`

	BytesIn  int64 `json:"bytes_in,omitempty"`
	BytesOut int64 `json:"bytes_out,omitempty"`
	Images   int   `json:"images,omitempty"`

	ttfts, streamMS []int64
}

// EndpointRow is the (date, endpoint) and endpoint-only bucket: G + B + C.
type EndpointRow struct {
	Date     string `json:"date,omitempty"`
	Endpoint string `json:"endpoint"` // provider/real-model (or provider:real-model label)

	// G - endpoint health
	Attempts int `json:"attempts"`
	OK       int `json:"ok"`
	// Forwarded counts attempts whose response was actually forwarded to the
	// client — response present and status < 400, WITHOUT the Error == ""
	// condition OK additionally requires. A superset of OK on purpose: a 2xx
	// that broke mid-copy gets Error = "truncated: …" and drops out of OK, yet
	// the router still charged quota for it (router.go's forwardSuccess:
	// "Charged here regardless of copyErr").
	//
	// That makes it the exact offline basis for the router's requests-metric
	// charging — chargeQuota fires once per forwarded attempt, so this
	// endpoint's real charged total is identically Forwarded x multiplier
	// (providerquota.go's MetricRequests branch, pinned by
	// cmd/vmr/quota_parity_test.go). Neither OK nor the request-level
	// Requests/RequestsOK is that identity.
	Forwarded    int            `json:"forwarded,omitempty"`
	Failed       int            `json:"failed"`
	Availability float64        `json:"availability"`
	ErrorRate    float64        `json:"error_rate,omitempty"` // failed/attempts × 100
	ErrorClasses map[string]int `json:"error_classes,omitempty"`
	// NormCounts tallies this endpoint's SUCCESSFUL attempts by which
	// response-normalization quirk-repair step fired — think_strip/
	// thinking_process_strip/soft_block_detected/thinking_process_pattern_detected
	// only (see aggregate.go's diagnosticNormMarker): routine steps every
	// successful response carries regardless of vendor behavior
	// (model_rewrite, buffered, opaque, ...) are deliberately excluded, or
	// this would be dominated by near-100%-hit-rate noise instead of
	// surfacing the vendor quirks it exists to catch. Per-request detail
	// pages already narrate each step for one request at a time (see
	// internal/i18n.Detail's NormDescriptions); this is the cross-request
	// frequency view that was previously uncollected.
	NormCounts map[string]int `json:"norm_counts,omitempty"`

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
	// TokensInFreshEst/TokensOutEst/TokensEstimated cover the requests this
	// endpoint served whose usage was NOT sniffable (the complement of
	// TokensKnown): the degraded byte-count estimate the routing half charged
	// for them, reproduced here by tokenest.go's estimateDegradedTokens.
	// TokensEstimated counts requests that CONTRIBUTED a non-zero estimate,
	// not every request that missed a usage object — so TokensKnown +
	// TokensEstimated is deliberately not expected to equal Requests. A
	// request whose every attempt failed forwards nothing and is charged
	// nothing (see estimateDegradedTokens' doc comment), so counting it here
	// would attribute a degraded token charge to a request the router never
	// charged at all.
	// Kept in SEPARATE fields rather than folded into TokensInFresh/TokensOut
	// on purpose — every existing consumer of those two (cache efficiency,
	// $ estimates, per-endpoint token tables) is asking about measured usage
	// and must not silently start averaging an estimate into it. §2.5's quota
	// column is the one consumer that deliberately adds them, because the
	// router charged both (see providerquota.go's MetricTokens branch).
	TokensInFreshEst int64   `json:"tokens_in_fresh_est,omitempty"`
	TokensOutEst     int64   `json:"tokens_out_est,omitempty"`
	TokensEstimated  int     `json:"tokens_estimated,omitempty"`
	CacheEfficiency  float64 `json:"cache_efficiency,omitempty"`
	TTFTKnown        int     `json:"ttft_known,omitempty"`
	TTFTMSP50        int64   `json:"ttft_ms_p50,omitempty"`
	TTFTMSP95        int64   `json:"ttft_ms_p95,omitempty"`
	RequestsWithDur  int     `json:"requests_with_dur,omitempty"`
	DurMSP50         int64   `json:"dur_ms_p50,omitempty"`
	DurMSP95         int64   `json:"dur_ms_p95,omitempty"`
	DurMSMax         int64   `json:"dur_ms_max,omitempty"`
	StreamKnown      int     `json:"stream_known,omitempty"`
	StreamMSP95      int64   `json:"stream_ms_p95,omitempty"`
	SlowRequests     int     `json:"slow_requests,omitempty"`
	TokOutPerSec     float64 `json:"tok_out_per_sec,omitempty"`
	DurMSSum         int64   `json:"dur_ms_sum,omitempty"`
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
	// CostEstimateEst is the portion of CostEstimate priced from the same
	// degraded byte-count estimate TokensInFreshEst/TokensOutEst record
	// (rather than sniffed usage) — mirrors TokensEstimated's role for the
	// tokens metric, and is what lets providerquota.go's cost branch report
	// a real WindowEstimatedPct instead of always 0. Meaningful only when
	// CostEstimate is non-nil; 0 there means every priced request had
	// sniffed usage.
	CostEstimateEst float64 `json:"cost_estimate_est,omitempty"`

	durs, ttfts, streamMS, inToks, outToks []int64
}

// ClientRow is the by-client_key_tag bucket (new in the summary): A+B+C.
type ClientRow struct {
	ClientKey string `json:"client_key"`

	TrafficStats

	SuccessRate float64 `json:"success_rate"`

	// per-request input/output token percentiles (⭐ derived, from Usage.In/Out)
	InTokP50  int64 `json:"in_tok_p50,omitempty"`
	InTokP95  int64 `json:"in_tok_p95,omitempty"`
	OutTokP50 int64 `json:"out_tok_p50,omitempty"`
	OutTokP95 int64 `json:"out_tok_p95,omitempty"`

	// H - cost (only when pricing configured)
	CostEstimate *float64 `json:"cost_estimate,omitempty"`

	inToks, outToks []int64
}

// WorkloadRow splits traffic by workload class: A+B+C+E(tool_call_rate).
type WorkloadRow struct {
	Class string `json:"class"`

	TrafficStats

	ToolCalls             int     `json:"tool_calls,omitempty"`
	RequestsWithToolCalls int     `json:"requests_with_tool_calls,omitempty"`
	ToolCallRate          float64 `json:"tool_call_rate,omitempty"`
}

// SessionRow is the per-session drill-down (§6 Sessions & Tasks): no latency columns in
// Markdown, but the data stays in JSON (P6). context_growth = last/first turn.
type SessionRow struct {
	// ID is the underlying Lineage's content-addressed identity
	// ("l-<hash8>", see SessionInfo.ID's doc comment) — stable across
	// independent runs/subsets, joinable against story's
	// JourneyIndexRow.Lineages by set membership. Alias is the old
	// run-scoped s%02d label, kept only for human scannability within
	// this one report; never use it as a lookup key (P6.1).
	ID            string `json:"id"`
	Alias         string `json:"alias,omitempty"`
	Title         string `json:"title,omitempty"`
	Class         string `json:"class,omitempty"`
	ClientKey     string `json:"client_key,omitempty"`
	ContinuedFrom string `json:"continued_from,omitempty"`
	Tasks         int    `json:"tasks"`
	From          string `json:"from"`
	To            string `json:"to"`

	TrafficStats

	Fallbacks int `json:"fallbacks,omitempty"`
	Truncated int `json:"truncated,omitempty"`

	// latency kept in JSON for P6 completeness, not shown in MD
	TTFTKnown int   `json:"ttft_known,omitempty"`
	TTFTMSP95 int64 `json:"ttft_ms_p95,omitempty"`
	DurMSMax  int64 `json:"dur_ms_max,omitempty"`

	RoleChars map[string]int64 `json:"role_chars,omitempty"`
	Images    int              `json:"images,omitempty"`

	// E - workload shape
	//
	// No compaction_chain field: it was declared here (omitempty []string)
	// and never written by anything, so it could only render as absent. The
	// chain itself IS reported — section_sessions.go's renderCompactionChains
	// walks ContinuedFrom across rows at render time rather than
	// materializing a per-row copy, which is why no accumulator for it was
	// ever missed. Same reasoning as the messages fields on Row above.
	ContextGrowth float64 `json:"context_growth,omitempty"` // last_in / first_in

	ttfts []int64
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
	// Code is a stable, non-localized identifier for programmatic consumption.
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
	// FindingProviderQuotaExhaustion  fires from the router's own
	// real-time counter — see findings_quota.go's quotaExhaustionFinding.
	FindingProviderQuotaExhaustion FindingCode = "provider_quota_exhaustion"
)

// RequestRow is one row of vmr-requests.json's "requests" field: the per-request drill-down
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

	// Req is this record's stable cross-command coordinate
	// (ctxgraph.ReqCoord: CanonicalPath(Path) + ":" + Line) — the join key
	// external tooling (or a future vmr-stories.json cross-reference) uses
	// to identify "the same audit record" regardless of which command
	// produced this row or how its input paths were spelled.
	Req string `json:"req,omitempty"`

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

// ProviderRow is one upstream account's (config.yaml's providers[].name)
// cross-model summary — §2.5 账户消耗与额度. Rolled up post-hoc from the
// already-finished EndpointsAll rows rather than accumulated independently
// (see provider.go's buildProviders): every field here is additive, so no
// new streaming state is needed. Deliberately carries no P50/P95 —
// percentiles aren't additive, and giving this bucket real ones would mean
// buffering a whole extra per-request slice during aggregation just for a
// question ("is this account under pressure") that doesn't need them; each
// endpoint's own percentiles are already in §5. DurMSMean is the honest
// substitute.
type ProviderRow struct {
	Provider     string         `json:"provider"`
	Models       []string       `json:"models"` // upstream models with traffic under this account, sorted
	Requests     int            `json:"requests"`
	RequestsOK   int            `json:"requests_ok"`
	Attempts     int            `json:"attempts"`
	Failed       int            `json:"failed"`
	ErrorRate    float64        `json:"error_rate,omitempty"` // failed/attempts × 100
	ErrorClasses map[string]int `json:"error_classes,omitempty"`
	WastedMS     int64          `json:"wasted_ms,omitempty"`

	TokensIn           int64   `json:"tokens_in"`
	TokensInCached     int64   `json:"tokens_in_cached"`
	TokensInCacheWrite int64   `json:"tokens_in_cache_write,omitempty"`
	TokensInFresh      int64   `json:"tokens_in_fresh"`
	TokensOut          int64   `json:"tokens_out"`
	TokensKnown        int     `json:"tokens_known,omitempty"`
	CacheEfficiency    float64 `json:"cache_efficiency,omitempty"`
	DurMSMean          int64   `json:"dur_ms_mean,omitempty"` // mean, NOT a percentile

	// H - cost (only when pricing configured)
	CostEstimate *float64 `json:"cost_estimate,omitempty"`

	// Quota is a read-only snapshot of this account's config.yaml quota
	// declaration, purely as a reference point — see ProviderQuotaRef's own
	// doc comment for what it deliberately is NOT.
	Quota *ProviderQuotaRef `json:"quota,omitempty"`
}

// ProviderQuotaRef is a config.yaml provider's declared quota limit, plus
// the computation inputs and Live state buildProviderQuotaRows
// needs to build §2.5's "额度与消耗对照" sub-table row for the same
// provider — the same map (cmd_report.go's buildProviderQuotas) feeds both
// ProviderRow.Quota (below) and that sub-table, so the two never disagree
// on what an account's declared quota is.
//
// ProviderRow.Quota itself only ever round-trips into vmr-report.json now
// — the Markdown main table renders no quota column at all, to
// avoid the exact duplication this type used to produce: the same Amount
// shown twice, through two different formatters, sometimes as two
// different strings for the same fractional value. The sub-table
// (renderProviderQuotaTable) is the one Markdown surface for this data,
// and carries its own "unweighted / window doesn't align with billing
// cycle" qualification in its footnotes.
type ProviderQuotaRef struct {
	Metric string  `json:"metric"` // requests | tokens | cost
	Every  string  `json:"every"`  // 1mo / 1w / 5h ...
	Amount float64 `json:"amount"`

	// Limit and Spec are input configs excluded from serialized JSON output.
	Limit *core.Limit     `json:"-"`
	Spec  *core.QuotaSpec `json:"-"`

	// Live holds real-time quota state read from vmr-quota.json.
	Live *LiveQuota `json:"live,omitempty"`

	// LiveConfigChanged indicates Live is nil because quota config changed since state was stored.
	LiveConfigChanged bool `json:"live_config_changed,omitempty"`
}

// LiveQuota snapshots real-time quota registry state for a provider at report generation time.
type LiveQuota struct {
	Used         float64   `json:"used"` // base(metric) already applied — directly comparable to Amount
	Pct          float64   `json:"pct"`  // Used/Amount*100, not clamped
	PeriodStart  time.Time `json:"period_start"`
	PeriodEndsAt time.Time `json:"period_ends_at"`
	// EstimatedPct is quota.EstimatedPct's result for this account's
	// bucket: the percentage of Used that came from a degraded (non-usage-
	// sniffed) estimate rather than real upstream usage — always 0 for
	// metric: requests and for a tokens/cost account whose usage was fully
	// sniffed. Without this, an account whose entire period is a downgraded
	// estimate renders identically to one with authoritative usage — the
	// same estimated_pct/⭐ honesty discipline the rest of this package
	// applies everywhere else (see report_tokens.go's EstimatedTokensNote).
	EstimatedPct float64 `json:"estimated_pct,omitempty"`
}

// ProviderQuotaRow is one row of §2.5's "额度与消耗对照" sub-table
// (providerquota.go's buildProviderQuotaRows) — every config.yaml account
// that declares a quota:, with two independently-windowed consumption
// figures placed side by side ON PURPOSE, never subtracted or ratioed
// against each other (see the quota design specification):
//
// - WindowConsumed is THIS REPORT RUN's own audit-log window, recomputed
// post-hoc through the same base(metric)/model_multiplier formula the
// router charges with (quota.BaseAmount/ApplyModelMultiplier) — but NOT
// a replay of the router's actual charge history. Known small sources
// of drift from the router's real number: failed attempts never
// charged, config weights/multipliers having changed mid-window, and
// (metric: cost only) this report's own pricing resolution possibly
// differing from the price in effect at charge time. requests-metric
// rounding is NOT a drift source — quota.ApplyModelMultiplier applies
// an exact multiplier with no rounding, so per-charge and
// aggregate-then-multiply agree exactly (see quota.Counters' doc
// comment).
// - Live is the router's own real-time counter (see LiveQuota) — the
// account's ACTUAL billing period, almost always a different window
// than the report's own input files cover.
//
// The renderer must keep both windows labeled, not just this comment.
type ProviderQuotaRow struct {
	Provider string  `json:"provider"`
	Metric   string  `json:"metric"`
	Every    string  `json:"every"`
	Amount   float64 `json:"amount"`

	// WindowConsumed is a pointer so a metric: cost account with NO
	// resolvable pricing for any of its endpoints (CostEstimate nil
	// everywhere in this window) can render nil → "-", the same "missing
	// data, not a real zero" convention section_provider.go's own $ Estimate
	// column already uses — a plain 0 here would be indistinguishable
	// from "genuinely spent nothing this window" and read as false
	// reassurance for exactly the AFP-pricing-gap scenario this report
	// exists to surface. requests/tokens accounts are never nil: 0 there is
	// a real zero (no traffic), not a missing-data case.
	WindowConsumed *float64 `json:"window_consumed"`

	// WindowEstimatedPct is the share of WindowConsumed that came from the
	// degraded byte-count estimate rather than usage the routing half actually
	// sniffed off the response — quota.EstimatedPct over this window's own
	// recomputation, the exact counterpart of LiveQuota.EstimatedPct on the
	// column to its right, so the two are read in the same unit.
	//
	// Always 0 for metric: requests (always exact). For metric: tokens it's
	// the degraded share of the raw token total; for metric: cost it's the
	// degraded share of the $ total (EndpointRow.CostEstimateEst summed
	// across this window, priced from the same degraded token estimate
	// rather than sniffed usage) — both read from a real per-record source,
	// not derived from one another.
	//
	// This field is what replaced an all-or-nothing bail-out: before it,
	// a window where NO record had parseable usage rendered "-" while a
	// window where only SOME did rendered a precise, systematically-low
	// number with no signal whatsoever. The second case is the dangerous one
	// — it is indistinguishable from genuinely lower consumption — and it was
	// the common one.
	WindowEstimatedPct float64 `json:"window_estimated_pct,omitempty"`

	// WindowUnpricedPct is the share of this account's requests that
	// WindowConsumed leaves out entirely because no rate resolved for their
	// endpoint (metric: cost only). WindowEstimatedPct's sibling one step
	// further out: that one is "priced, but from a degraded estimate", this
	// one is "not priced at all, so not in the number". Both exist because
	// WindowConsumed's own guard is all-or-nothing — one unpriced endpoint
	// among several priced ones rendered a precise, systematically-low figure
	// that reads like genuinely lower spend.
	//
	// In REQUESTS, not currency, deliberately: no rate existing is precisely
	// why these rows are missing, so a dollar figure would be invented.
	//
	// Normally 0 — config.validate already requires a metric: cost account's
	// configured models to price completely. It goes non-zero when the audit
	// log outruns the config (a model since renamed or dropped from models:)
	// or on a legacy "/"-joined label splitEndpointProviderModel won't parse.
	WindowUnpricedPct float64 `json:"window_unpriced_pct,omitempty"`

	// WindowNoOverlap is true when this report run's audit-log
	// coverage ([Meta.From, Meta.To]) and this account's current billing
	// period ([PeriodStart, PeriodEndsAt]) share NO time in common at all —
	// e.g. analyzing three-month-old archived logs against today's live
	// period. Partial misalignment is the documented normal case (the
	// whole reason these two columns are never subtracted); this flags the
	// more extreme, more easily misread case where they don't overlap even
	// a little. Always false when this report processed zero records (no
	// window to compare against in the first place).
	WindowNoOverlap bool `json:"window_no_overlap,omitempty"`

	// Live/LiveConfigChanged are copied as-is from the ProviderQuotaRef this
	// row was built from — see that type's own doc comments.
	Live              *LiveQuota `json:"live,omitempty"`
	LiveConfigChanged bool       `json:"live_config_changed,omitempty"`

	PeriodStart  time.Time `json:"period_start"`
	PeriodEndsAt time.Time `json:"period_ends_at"`
	// PeriodElapsedPct is 1 - quota.TimeLeftFrac(now, PeriodStart,
	// PeriodEndsAt), as a percentage — "周期已过%", read side by side with
	// Live.Pct ("已用%") to judge burn rate without any extrapolation (see
	// the dev plan's period-progress rewrite: quota.Headroom<1 is exactly
	// equivalent to Live.Pct > PeriodElapsedPct).
	PeriodElapsedPct float64 `json:"period_elapsed_pct"`
}

// ClientEndpointRow is one (client_key_tag, upstream endpoint) pair's token
// consumption — §5.5 按客户端的上游归属. Rendered grouped by ClientKey
// (section_client_endpoint.go), not as a client×endpoint matrix — see this
// file's package doc comment / the dev doc's §3.2 for why. Streaming-
// collected (clientendpoint.go) since no existing bucket is keyed this way.
// Deliberately token/request-only: no $ (already answered by §2's by-client
// table) and no percentiles (this row's whole point is the endpoint-level
// split, not a new latency view).
type ClientEndpointRow struct {
	ClientKey string `json:"client_key"`
	Endpoint  string `json:"endpoint"` // protocol:provider:model (or the "/"-joined legacy label)
	Requests  int    `json:"requests"`

	TokensIn       int64 `json:"tokens_in"`
	TokensInCached int64 `json:"tokens_in_cached"`
	TokensInFresh  int64 `json:"tokens_in_fresh"`
	TokensOut      int64 `json:"tokens_out"`
}

func (r *Report2) RequestRows() []RequestRow { return r.requests }
