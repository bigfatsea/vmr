// Ver 2026-07-25, report2

// Package report2 is the aggregator behind the `vmr report` command: it
// turns audit JSONL into a Markdown + JSON pair organized around nine
// numbered sections (§0-§8, see docs/VirtualModelRouter_System_Design_v3.md
// §9.4; the original design rationale is in REPORT_REDESIGN_V2.zh.md, kept
// as a historical record — its "eight sections" and "format 9" numbering
// predate the pricing/cost-estimate section added since). The package name
// predates a CLI rename (it shipped as `vmr report2` alongside a since-
// deleted `vmr report`; both the command and this package are now simply
// "report") and is kept as-is — renaming it would touch every import for no
// functional benefit.
//
// Reused from internal/report (read-only foundation, not superseded):
//   - report.AnalyzeSessions  -> session/task grouping, compaction chains,
//     workload tags, tool signatures (the 791-line heuristic core this
//     package does not reimplement).
//   - report.ExtractUsage     -> the four provider usage shapes.
//   - report.WriteDetails     -> per-request detail files under details/.
//   - report.ReqInfo / SessionAnalysis / SessionInfo / Usage / ToolShapeRow
//     -> reading the analysis output.
//
// internal/report's own former standalone aggregator/renderer (report.Build,
// report.Markdown, report.WriteRequests, and WriteDetails' legacy index
// side effect) has been deleted; this package is the only aggregator left.
//
// Everything else - bucket aggregation, derived metrics (fresh tokens, cache
// efficiency, true stream_ms percentiles, slow-request counts, tool-schema
// waste, context growth, by-client, cost estimate), Markdown rendering, the
// requests index, and the pricing sidecar - lives here.
//
// Format 10 (Meta.Format / const Format below) is the only format; the
// invariant it encodes: every bucket keeps its own raw dur_ms / ttft_ms /
// stream_ms slices and computes true p50/p95 directly - no cross-bucket
// roll-up, no percentile-of-percentiles. stream_ms (dur-ttft) is collected
// as its own per-request slice for the same reason:
// P95(dur)-P95(ttft) != P95(dur-ttft).
package report2

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"vmr/internal/audit"
	"vmr/internal/report"
)

// Format is the report2 JSON structure version. 10 continues the legacy
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
	Meta         Meta           `json:"meta"`
	Overall      Row            `json:"overall"`
	ByModel      []Row          `json:"by_model"`
	ByDate       []Row          `json:"by_date"`
	Hours        []HourRow      `json:"hours,omitempty"`
	HoursOfDay   []HourRow      `json:"hours_of_day,omitempty"`
	Endpoints    []EndpointRow  `json:"endpoints"`
	EndpointsAll []EndpointRow  `json:"endpoints_all,omitempty"`
	ByClient     []ClientRow    `json:"by_client,omitempty"`
	Workloads    []WorkloadRow  `json:"workloads,omitempty"`
	Sessions     []SessionRow   `json:"sessions,omitempty"`
	Tools        []ToolShapeRow `json:"tools,omitempty"`
	Efficiency   []Finding      `json:"efficiency,omitempty"`
	Pricing      *Pricing       `json:"pricing,omitempty"`

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
	TokensIn           int64   `json:"tokens_in,omitempty"`
	TokensInCached     int64   `json:"tokens_in_cached,omitempty"`
	TokensInCacheWrite int64   `json:"tokens_in_cache_write,omitempty"`
	TokensInFresh      int64   `json:"tokens_in_fresh,omitempty"`
	TokensOut          int64   `json:"tokens_out,omitempty"`
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

	// H - cost (only when pricing configured)
	CostEstimate *float64 `json:"cost_estimate,omitempty"`

	durs, ttfts, streamMS []int64
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
	TokensKnown        int     `json:"tokens_known,omitempty"`
	CacheEfficiency    float64 `json:"cache_efficiency"`

	RequestsWithDur int   `json:"requests_with_dur,omitempty"`
	DurMSP50        int64 `json:"dur_ms_p50,omitempty"`
	DurMSP95        int64 `json:"dur_ms_p95,omitempty"`
	SlowRequests    int   `json:"slow_requests,omitempty"`

	// H - cost (only when pricing configured)
	CostEstimate *float64 `json:"cost_estimate,omitempty"`

	durs, ttfts, streamMS []int64
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

// SessionRow is the per-session drill-down (V2 §5): no latency columns in
// Markdown, but the data stays in JSON (P6). context_growth = last/first turn.
type SessionRow struct {
	ID            string `json:"id"`
	Title         string `json:"title,omitempty"`
	Class         string `json:"class,omitempty"`
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

// ToolShapeRow is per declared-tool-set waste (V2 §6 Top-N): F-family.
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
	Finding    string `json:"finding"`
	Metric     string `json:"metric"`
	Value      string `json:"value"`
	Implicated string `json:"implicated,omitempty"`
	Action     string `json:"action,omitempty"`
}

// RequestRow is one line of vmr-requests.jsonl: the per-request drill-down
// backing the redesigned index (V2 §7). Every field is rule-extracted;
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
func (r *Report2) RequestRows() []RequestRow { return r.requests }

// Pricing is the optional sidecar config (V2 §4). Nil/absent => no $ anywhere.
type Pricing struct {
	Currency   string                 `json:"currency,omitempty"`
	UpdatedAt  string                 `json:"updated_at,omitempty"`
	Rates      []PricingRate          `json:"rates"`
	byEndpoint map[string]PricingRate `json:"-"`
}

// PricingRate is one endpoint's unit prices (per 1M tokens).
type PricingRate struct {
	Endpoint        string  `json:"endpoint" yaml:"endpoint"`
	InFreshPer1M    float64 `json:"in_fresh_per_1m" yaml:"in_fresh_per_1m"`
	CacheReadPer1M  float64 `json:"cache_read_per_1m" yaml:"cache_read_per_1m"`
	CacheWritePer1M float64 `json:"cache_write_per_1m" yaml:"cache_write_per_1m"`
	OutPer1M        float64 `json:"out_per_1m" yaml:"out_per_1m"`
}

// rec2 is report2's per-record working struct: raw fields from audit.Record
// joined to ReqInfo's grouping/features. Built once per record, shared
// read-only by every bucket.
type rec2 struct {
	ts                       time.Time
	date                     string
	hour                     int
	model, protocol, outcome string
	stream                   bool
	durMS, ttftMS            int64
	streamMS                 int64
	streamOK                 bool
	usage                    report.Usage
	usageOK                  bool
	msgs                     int
	bytesIn, bytesOut        int64
	finish                   string
	truncated                bool
	fallbacks                int
	images, imagesCompressed int
	clientKey                string
	endpoint                 string // last successful attempt's endpoint
	errClass                 string // last attempt's error class (index display)
	toolDeclBytes            int64
	toolDeclCount            int
	toolCalls                []string
	roleChars                map[string]int64
	// from ReqInfo
	sessionID, taskID       string
	taskSeq, sessSeq        int
	tags                    []string
	workloadClass           string
	compaction              bool
	summarizes, continuesTo string
	detailFile              string
	newInstruction          string
	path                    string
	line                    int
}

// Build reads audit JSONL files and aggregates them into a Report2. It calls
// report.AnalyzeSessions for grouping (one read), then does its own pass
// (second read) joining each record to its ReqInfo via sess.Lookup.
//
// Unlike the old (now removed) `vmr report` aggregator — which ran its
// deterministic aggregation first and only attempted session analysis
// afterward, so a session-analysis failure degraded to a warning instead of
// losing the whole report — this two-pass design needs AnalyzeSessions to
// succeed before the second pass can even start (every record's usage/
// tokens now comes from its ReqInfo, not a second independent extraction).
// A failure here is fatal to the whole command. In practice the only way
// AnalyzeSessions fails is a per-file I/O error (bad open, or a read error
// mid-scan — malformed JSON lines are just skipped, not fatal), and this
// pass reads the exact same files the same way a moment later, so the
// error surface is the same either way. The most likely real-world trigger
// is a race with `internal/audit/housekeep.go`'s rotation sweep — a
// long-running `vmr start` compressing/deleting a log file out from under
// a concurrently running `vmr report` — not a code bug, so the message
// below names that possibility explicitly.
func Build(paths []string, now time.Time, progress io.Writer, pricing *Pricing) (*Report2, *report.SessionAnalysis, error) {
	sess, err := report.AnalyzeSessions(paths)
	if err != nil {
		return nil, nil, fmt.Errorf("session analysis failed (%w) — no report was written. "+
			"This step reads every input file a second time; the most common real-world cause "+
			"is one of them being rotated/compressed by the audit housekeeping sweep (a running "+
			"`vmr start` instance) while this scan was in progress. Rerun; if it persists, check "+
			"whether any input path still exists under its original name (housekeeping renames "+
			"rotated files to .zst) and that it isn't corrupt", err)
	}
	// valid session IDs (for SessionRow accumulation) + lookup by id
	sessionInfo := map[string]*report.SessionInfo{}
	for _, s := range sess.Sessions {
		sessionInfo[s.ID] = s
	}

	rep := &Report2{Meta: Meta{
		Format: Format, GeneratedAt: now.Format(time.RFC3339), Inputs: paths,
		SlowThreshold:    SlowThresholdMS,
		PercentileMethod: "true per-bucket from raw dur_ms/ttft_ms/stream_ms; cross-day merges use pre-aggregated *_all/hours_of_day siblings",
	}}
	var from, to time.Time

	// bucket maps
	byModel := map[string]*Row{}
	byDate := map[string]*Row{}
	hours := map[string]*HourRow{}
	hoursOfDay := map[int]*HourRow{}
	eps := map[string]*EndpointRow{}
	epsAll := map[string]*EndpointRow{}
	byClient := map[string]*ClientRow{}
	workloads := map[string]*WorkloadRow{}
	sessions := map[string]*SessionRow{}

	addA := func(r *Row, rc *rec2) {
		r.Requests++
		switch rc.outcome {
		case "ok":
			r.OK++
		case "canceled":
			r.Canceled++
		default:
			r.Errors++
		}
		if rc.stream {
			r.Streams++
		}
		if rc.fallbacks > 0 {
			r.Fallbacks++
			if rc.outcome == "ok" {
				r.FallbackRecovered++
			} else {
				r.FallbackFailed++
			}
		}
		if rc.truncated {
			r.Truncated++
		}
		r.BytesIn += rc.bytesIn
		r.BytesOut += rc.bytesOut
		if rc.usageOK {
			r.TokensIn += rc.usage.In
			r.TokensInCached += rc.usage.CacheRead
			r.TokensInCacheWrite += rc.usage.CacheWrite
			r.TokensOut += rc.usage.Out
			r.TokensReasoning += rc.usage.Reasoning
			r.TokensKnown++
		}
		if rc.durMS > 0 {
			r.RequestsWithDur++
			r.durs = append(r.durs, rc.durMS)
			r.DurMSSum += rc.durMS
			if rc.durMS > r.DurMSMax {
				r.DurMSMax = rc.durMS
			}
			if rc.durMS > SlowThresholdMS {
				r.SlowRequests++
			}
			if rc.usageOK {
				r.tokDurMS += rc.durMS
			}
		}
		if rc.ttftMS > 0 {
			r.TTFTKnown++
			r.TTFTMSSum += rc.ttftMS
			r.ttfts = append(r.ttfts, rc.ttftMS)
		}
		if rc.streamOK {
			r.StreamKnown++
			r.streamMS = append(r.streamMS, rc.streamMS)
		}
		r.Images += rc.images
		r.ImagesCompressed += rc.imagesCompressed
		if len(rc.roleChars) > 0 {
			if r.RoleChars == nil {
				r.RoleChars = map[string]int64{}
			}
			for role, c := range rc.roleChars {
				r.RoleChars[role] += c
			}
		}
	}

	addHour := func(h *HourRow, rc *rec2) {
		h.Requests++
		switch rc.outcome {
		case "ok":
			h.OK++
		case "canceled":
		default:
			h.Errors++
		}
		if rc.fallbacks > 0 {
			h.Fallbacks++
		}
		if rc.truncated {
			h.Truncated++
		}
		h.BytesIn += rc.bytesIn
		h.BytesOut += rc.bytesOut
		if rc.usageOK {
			h.TokensIn += rc.usage.In
			h.TokensInCached += rc.usage.CacheRead
			h.TokensInCacheWrite += rc.usage.CacheWrite
			h.TokensOut += rc.usage.Out
			h.TokensKnown++
		}
		if rc.durMS > 0 {
			h.RequestsWithDur++
			h.durs = append(h.durs, rc.durMS)
			if rc.durMS > h.DurMSMax {
				h.DurMSMax = rc.durMS
			}
			if rc.durMS > SlowThresholdMS {
				h.SlowRequests++
			}
		}
		if rc.ttftMS > 0 {
			h.TTFTKnown++
			h.ttfts = append(h.ttfts, rc.ttftMS)
		}
		if rc.streamOK {
			h.StreamKnown++
			h.streamMS = append(h.streamMS, rc.streamMS)
		}
		h.Images += rc.images
	}

	// Endpoint attempt-level accounting (G-family).
	addAttempt := func(e *EndpointRow, a audit.Attempt) {
		e.Attempts++
		if a.Error == "" && a.Response != nil && a.Response.Status < 400 {
			e.OK++
		} else {
			e.Failed++
			cls := attemptErrClass(a)
			if cls == "" {
				cls = "unknown"
			}
			if e.ErrorClasses == nil {
				e.ErrorClasses = map[string]int{}
			}
			e.ErrorClasses[cls]++
		}
	}
	// Request-level metrics attach to the endpoint that served the client.
	addEndpointReq := func(e *EndpointRow, rc *rec2) {
		if rc.usageOK {
			e.TokensIn += rc.usage.In
			e.TokensInCached += rc.usage.CacheRead
			e.TokensInCacheWrite += rc.usage.CacheWrite
			e.TokensOut += rc.usage.Out
			e.TokensKnown++
		}
		if rc.ttftMS > 0 {
			e.TTFTKnown++
			e.ttfts = append(e.ttfts, rc.ttftMS)
		}
		if rc.durMS > 0 {
			e.RequestsWithDur++
			e.durs = append(e.durs, rc.durMS)
			e.DurMSSum += rc.durMS
			if rc.durMS > e.DurMSMax {
				e.DurMSMax = rc.durMS
			}
			if rc.durMS > SlowThresholdMS {
				e.SlowRequests++
			}
		}
		if rc.streamOK {
			e.StreamKnown++
			e.streamMS = append(e.streamMS, rc.streamMS)
		}
	}

	addClient := func(c *ClientRow, rc *rec2) {
		c.Requests++
		switch rc.outcome {
		case "ok":
			c.OK++
		case "canceled":
		default:
			c.Errors++
		}
		if rc.usageOK {
			c.TokensIn += rc.usage.In
			c.TokensInCached += rc.usage.CacheRead
			c.TokensInCacheWrite += rc.usage.CacheWrite
			c.TokensOut += rc.usage.Out
			c.TokensKnown++
		}
		if rc.durMS > 0 {
			c.RequestsWithDur++
			c.durs = append(c.durs, rc.durMS)
			if rc.durMS > SlowThresholdMS {
				c.SlowRequests++
			}
		}
		if rc.ttftMS > 0 {
			c.ttfts = append(c.ttfts, rc.ttftMS)
		}
		if rc.streamOK {
			c.streamMS = append(c.streamMS, rc.streamMS)
		}
	}

	addWorkload := func(w *WorkloadRow, rc *rec2) {
		w.Requests++
		if rc.usageOK {
			w.TokensIn += rc.usage.In
			w.TokensInCached += rc.usage.CacheRead
			w.TokensInCacheWrite += rc.usage.CacheWrite
			w.TokensOut += rc.usage.Out
			w.TokensKnown++
		}
		if rc.durMS > 0 {
			w.RequestsWithDur++
			w.durs = append(w.durs, rc.durMS)
			if rc.durMS > SlowThresholdMS {
				w.SlowRequests++
			}
		}
		if rc.streamOK {
			w.streamMS = append(w.streamMS, rc.streamMS)
		}
		w.ToolCalls += len(rc.toolCalls)
		if len(rc.toolCalls) > 0 {
			w.RequestsWithToolCalls++
		}
	}

	addSession := func(s *SessionRow, rc *rec2) {
		s.Requests++
		switch rc.outcome {
		case "ok":
			s.OK++
		case "canceled":
		default:
			s.Errors++
		}
		if rc.fallbacks > 0 {
			s.Fallbacks++
		}
		if rc.truncated {
			s.Truncated++
		}
		if rc.usageOK {
			s.TokensIn += rc.usage.In
			s.TokensInCached += rc.usage.CacheRead
			s.TokensInCacheWrite += rc.usage.CacheWrite
			s.TokensOut += rc.usage.Out
			s.TokensKnown++
		}
		if rc.durMS > 0 {
			s.RequestsWithDur++
			s.durs = append(s.durs, rc.durMS)
			if rc.durMS > s.DurMSMax {
				s.DurMSMax = rc.durMS
			}
		}
		if rc.ttftMS > 0 {
			s.TTFTKnown++
			s.ttfts = append(s.ttfts, rc.ttftMS)
		}
		s.Images += rc.images
		if len(rc.roleChars) > 0 {
			if s.RoleChars == nil {
				s.RoleChars = map[string]int64{}
			}
			for role, c := range rc.roleChars {
				s.RoleChars[role] += c
			}
		}
	}

	// ---- single pass over files, joined to ReqInfo ----
	for fileIdx, path := range paths {
		fileStart := time.Now()
		var fileRecords int
		rc, err := audit.OpenLogFile(path)
		if err != nil {
			return nil, nil, err
		}
		line := 0
		scanErr := audit.ForEachLine(rc, audit.MaxLogLine, func(lineBytes []byte) {
			line++
			var arec audit.Record
			if err := json.Unmarshal(lineBytes, &arec); err != nil {
				rep.Meta.ParseErrors++
				return
			}
			rep.Meta.Records++
			fileRecords++
			if from.IsZero() || arec.TS.Before(from) {
				from = arec.TS
			}
			if arec.TS.After(to) {
				to = arec.TS
			}
			ri := sess.Lookup(path, line)
			rc := buildRec2(&arec, ri, path, line)

			date := rc.date
			hour := rc.hour
			model := rc.model
			if model == "" {
				model = "(rejected)"
			}

			// 1. Overall
			addA(&rep.Overall, rc)
			// 2. ByModel
			mk := model + "\x00" + rc.protocol
			mr := byModel[mk]
			if mr == nil {
				mr = &Row{Model: model, Protocol: rc.protocol}
				byModel[mk] = mr
			}
			addA(mr, rc)
			// 3. ByDate
			dr := byDate[date]
			if dr == nil {
				dr = &Row{Date: date}
				byDate[date] = dr
			}
			addA(dr, rc)
			// 4. Hours + HoursOfDay
			hk := fmt.Sprintf("%s\x00%02d", date, hour)
			hr := hours[hk]
			if hr == nil {
				hr = &HourRow{Date: date, Hour: hour}
				hours[hk] = hr
			}
			addHour(hr, rc)
			hod := hoursOfDay[hour]
			if hod == nil {
				hod = &HourRow{Hour: hour}
				hoursOfDay[hour] = hod
			}
			addHour(hod, rc)
			// 5. Endpoints + EndpointsAll (attempts), request-level on success ep
			for _, a := range arec.Attempts {
				k := date + "\x00" + a.Endpoint
				e := eps[k]
				if e == nil {
					e = &EndpointRow{Date: date, Endpoint: a.Endpoint}
					eps[k] = e
				}
				addAttempt(e, a)
				ea := epsAll[a.Endpoint]
				if ea == nil {
					ea = &EndpointRow{Endpoint: a.Endpoint}
					epsAll[a.Endpoint] = ea
				}
				addAttempt(ea, a)
				if a.Endpoint == rc.endpoint {
					addEndpointReq(e, rc)
					addEndpointReq(ea, rc)
				}
			}
			// 6. ByClient (skip empty tag - auth disabled / no match)
			if rc.clientKey != "" {
				c := byClient[rc.clientKey]
				if c == nil {
					c = &ClientRow{ClientKey: rc.clientKey}
					byClient[rc.clientKey] = c
				}
				addClient(c, rc)
			}
			// 7. Workloads
			wc := rc.workloadClass
			if wc == "" {
				wc = "interactive"
			}
			w := workloads[wc]
			if w == nil {
				w = &WorkloadRow{Class: wc}
				workloads[wc] = w
			}
			addWorkload(w, rc)
			// 8. Sessions (only grouped)
			if rc.sessionID != "" && sessionInfo[rc.sessionID] != nil {
				s := sessions[rc.sessionID]
				if s == nil {
					info := sessionInfo[rc.sessionID]
					s = &SessionRow{ID: info.ID, Title: info.Title, Tasks: len(info.Tasks),
						ContinuedFrom: info.ContinuedFrom, Class: wc}
					if len(info.Recs) > 0 {
						s.From = info.Recs[0].TS.Format(time.RFC3339)
						s.To = info.Recs[len(info.Recs)-1].TS.Format(time.RFC3339)
					}
					sessions[rc.sessionID] = s
				}
				addSession(s, rc)
			}
			// cost (if pricing): overall + by-model (existing) plus
			// by-endpoint (epsAll, cross-date — matches §3 端点健康's basis)
			// and by-client, when either bucket applies to this record.
			if pricing != nil && rc.endpoint != "" {
				pr, ok := pricing.byEndpoint[rc.endpoint]
				if ok {
					c := costFor(pr, rc)
					if rep.Overall.CostEstimate == nil {
						rep.Overall.CostEstimate = new(float64)
					}
					*rep.Overall.CostEstimate += c
					if mr.CostEstimate == nil {
						mr.CostEstimate = new(float64)
					}
					*mr.CostEstimate += c
					if ea := epsAll[rc.endpoint]; ea != nil {
						if ea.CostEstimate == nil {
							ea.CostEstimate = new(float64)
						}
						*ea.CostEstimate += c
					}
					if rc.clientKey != "" {
						if cl := byClient[rc.clientKey]; cl != nil {
							if cl.CostEstimate == nil {
								cl.CostEstimate = new(float64)
							}
							*cl.CostEstimate += c
						}
					}
				}
			}
			// per-request export row
			rep.requests = append(rep.requests, buildRequestRow(rc))
		}, func() {
			rep.Meta.ParseErrors++
		})
		rc.Close()
		if scanErr != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, scanErr)
		}
		if progress != nil {
			fmt.Fprintf(progress, "[%d/%d] %s  done: %d records (%s)\n",
				fileIdx+1, len(paths), path, fileRecords, time.Since(fileStart).Round(time.Millisecond))
		}
	}

	if !from.IsZero() {
		rep.Meta.From = from.Format(time.RFC3339)
		rep.Meta.To = to.Format(time.RFC3339)
	}

	// ---- finish all buckets ----
	finishRow(&rep.Overall)
	for _, r := range byModel {
		finishRow(r)
		rep.ByModel = append(rep.ByModel, *r)
	}
	for _, r := range byDate {
		finishRow(r)
		rep.ByDate = append(rep.ByDate, *r)
	}
	for _, h := range hours {
		finishHour(h)
		rep.Hours = append(rep.Hours, *h)
	}
	for _, h := range hoursOfDay {
		finishHour(h)
		rep.HoursOfDay = append(rep.HoursOfDay, *h)
	}
	for _, e := range eps {
		finishEndpoint(e)
		rep.Endpoints = append(rep.Endpoints, *e)
	}
	for _, e := range epsAll {
		finishEndpoint(e)
		rep.EndpointsAll = append(rep.EndpointsAll, *e)
	}
	for _, c := range byClient {
		finishClient(c)
		rep.ByClient = append(rep.ByClient, *c)
	}
	for _, w := range workloads {
		finishWorkload(w)
		rep.Workloads = append(rep.Workloads, *w)
	}
	for _, s := range sessions {
		finishSession(s, sessionInfo[s.ID])
		rep.Sessions = append(rep.Sessions, *s)
	}
	rep.Tools = buildTools(sess)
	rep.Efficiency = buildFindings(rep)
	rep.Pricing = pricing

	// ---- sort all slices ----
	sortRows(rep.ByModel, "model")
	sortRows(rep.ByDate, "date")
	sort.Slice(rep.Hours, func(i, j int) bool {
		if rep.Hours[i].Date != rep.Hours[j].Date {
			return rep.Hours[i].Date < rep.Hours[j].Date
		}
		return rep.Hours[i].Hour < rep.Hours[j].Hour
	})
	sort.Slice(rep.HoursOfDay, func(i, j int) bool { return rep.HoursOfDay[i].Hour < rep.HoursOfDay[j].Hour })
	sort.Slice(rep.Endpoints, func(i, j int) bool {
		if rep.Endpoints[i].Date != rep.Endpoints[j].Date {
			return rep.Endpoints[i].Date < rep.Endpoints[j].Date
		}
		return rep.Endpoints[i].Endpoint < rep.Endpoints[j].Endpoint
	})
	sort.Slice(rep.EndpointsAll, func(i, j int) bool { return rep.EndpointsAll[i].Attempts > rep.EndpointsAll[j].Attempts })
	sort.Slice(rep.ByClient, func(i, j int) bool { return rep.ByClient[i].Requests > rep.ByClient[j].Requests })
	sort.Slice(rep.Workloads, func(i, j int) bool { return rep.Workloads[i].TokensIn > rep.Workloads[j].TokensIn })
	sort.Slice(rep.Sessions, func(i, j int) bool {
		// interactive first (by requests), then scheduled; stable within
		a, b := rep.Sessions[i], rep.Sessions[j]
		if (a.Class == "interactive") != (b.Class == "interactive") {
			return a.Class == "interactive"
		}
		return a.Requests > b.Requests
	})
	sort.Slice(rep.Tools, func(i, j int) bool { return rep.Tools[i].SchemaWasteBytes > rep.Tools[j].SchemaWasteBytes })
	return rep, sess, nil
}

// buildRequestRow maps a rec2 to its per-request export row.
func buildRequestRow(rc *rec2) RequestRow {
	rr := RequestRow{
		TS:         rc.ts.Format("2006-01-02T15:04:05Z07:00"),
		Session:    rc.sessionID,
		Task:       rc.taskID,
		Turn:       rc.taskSeq,
		SessTurn:   rc.sessSeq,
		Model:      rc.model,
		Protocol:   rc.protocol,
		Outcome:    rc.outcome,
		ClientKey:  rc.clientKey,
		Endpoint:   rc.endpoint,
		Finish:     rc.finish,
		DurMS:      rc.durMS,
		TTFTMS:     rc.ttftMS,
		Msgs:       rc.msgs,
		Fallbacks:  rc.fallbacks,
		Truncated:  rc.truncated,
		ErrorClass: rc.errClass,
		DetailFile: rc.detailFile,
		Path:       rc.path,
		Line:       rc.line,
	}
	if rc.usageOK {
		rr.TokensIn = rc.usage.In
		rr.TokensInCached = rc.usage.CacheRead
		rr.TokensInFresh = freshTokens(rc.usage.In, rc.usage.CacheRead, rc.usage.CacheWrite)
		rr.TokensOut = rc.usage.Out
		rr.CacheEff = cacheEff(rc.usage.CacheRead, rr.TokensInFresh)
	}
	return rr
}

// WriteJSON writes the aggregate report JSON (vmr-report.json). Per-request
// rows are NOT included (they live in vmr-requests.jsonl).
func WriteJSON(rep *Report2, path string) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// sortRows sorts Row slices by the given key ("model" or "date").
func sortRows(rows []Row, key string) {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if key == "date" {
			return a.Date < b.Date
		}
		if a.Requests != b.Requests {
			return a.Requests > b.Requests
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Protocol < b.Protocol
	})
}

// buildRec2 extracts report2's per-record fields from an audit.Record joined
// to its ReqInfo (which may be nil for records the analyzer skipped).
func buildRec2(arec *audit.Record, ri *report.ReqInfo, path string, line int) *rec2 {
	r := &rec2{
		ts:       arec.TS,
		date:     arec.TS.Format("2006-01-02"),
		hour:     arec.TS.Hour(),
		model:    arec.Model,
		protocol: arec.Protocol,
		outcome:  arec.Outcome,
		stream:   arec.Stream,
		durMS:    arec.DurMS,
		ttftMS:   arec.TTFTMS,
		path:     path,
		line:     line,
	}
	if arec.DurMS > 0 && arec.TTFTMS > 0 {
		r.streamMS = arec.DurMS - arec.TTFTMS
		if r.streamMS < 0 {
			r.streamMS = 0
		} else {
			r.streamOK = true
		}
	}
	// bytes (recompute; ReqInfo keeps these unexported)
	r.bytesIn = bodyBytes(arec.Client.Request.Body)
	if arec.Client.Response != nil {
		r.bytesOut = bodyBytes(arec.Client.Response.Body)
	}
	// tool declaration bytes (recompute; ReqInfo.declBytes unexported)
	r.toolDeclCount, r.toolDeclBytes = toolDeclInfo(arec.Client.Request.Body)
	// endpoint served + last error class (recompute; ReqInfo unexported)
	r.endpoint, r.errClass = endpointInfo(arec)
	r.clientKey = arec.ClientKeyTag
	// images
	for _, img := range arec.Images {
		r.images++
		if img.Downscaled {
			r.imagesCompressed++
		}
	}
	// fallbacks
	if len(arec.Attempts) > 1 {
		r.fallbacks = 1
	}
	// truncated: ok outcome with a truncated attempt error
	if arec.Outcome == "ok" {
		for _, a := range arec.Attempts {
			if attemptErrClass(a) == "truncated" {
				r.truncated = true
				break
			}
		}
	}
	// join ReqInfo (grouping + expensive features it already computed)
	if ri != nil {
		r.usage = ri.Usage
		r.usageOK = ri.UsageOK
		r.finish = ri.Finish
		r.truncated = r.truncated || ri.Truncated
		r.fallbacks = ri.Fallbacks
		r.images = ri.Images
		r.imagesCompressed = ri.ImagesCompressed
		r.toolCalls = ri.ToolCalls
		r.roleChars = ri.RoleChars
		r.msgs = ri.Msgs
		r.sessionID = ri.SessionID
		r.taskID = ri.TaskID
		r.taskSeq = ri.TaskSeq
		r.sessSeq = ri.SessSeq
		r.tags = ri.Tags
		r.compaction = ri.Compaction
		r.summarizes = ri.Summarizes
		r.continuesTo = ri.ContinuesTo
		r.detailFile = ri.DetailFile
		r.newInstruction = ri.NewInstruction
		r.workloadClass = workloadClassOf(ri)
	}
	return r
}

// endpointInfo returns the last successful attempt's endpoint (the one that
// served the client) and the last attempt's error class (for index display).
func endpointInfo(arec *audit.Record) (endpoint, errClass string) {
	var successEp string
	for _, a := range arec.Attempts {
		if a.Error == "" && a.Response != nil && a.Response.Status < 400 {
			successEp = a.Endpoint
		}
	}
	if len(arec.Attempts) > 0 {
		errClass = attemptErrClass(arec.Attempts[len(arec.Attempts)-1])
	}
	return successEp, errClass
}

// workloadClassOf replicates report.workloadClass (unexported) using the
// exported ReqInfo fields Compaction + Tags.
func workloadClassOf(ri *report.ReqInfo) string {
	if ri == nil {
		return "interactive"
	}
	if ri.Compaction {
		return "compaction"
	}
	for _, t := range ri.Tags {
		if t == "heartbeat" {
			return "heartbeat"
		}
		if t == "dream_diary" {
			return "dream_diary"
		}
	}
	return "interactive"
}

// costFor computes one record's estimated cost from its endpoint's rate.
func costFor(pr PricingRate, rc *rec2) float64 {
	if !rc.usageOK {
		return 0
	}
	fresh := rc.usage.In - rc.usage.CacheRead - rc.usage.CacheWrite
	if fresh < 0 {
		fresh = 0
	}
	return pr.InFreshPer1M/1e6*float64(fresh) +
		pr.CacheWritePer1M/1e6*float64(rc.usage.CacheWrite) +
		pr.OutPer1M/1e6*float64(rc.usage.Out)
}

// buildTools derives the tool-waste fields from the analysis's ToolShapes.
func buildTools(sess *report.SessionAnalysis) []ToolShapeRow {
	shapes := sess.ToolShapes()
	out := make([]ToolShapeRow, 0, len(shapes))
	for _, t := range shapes {
		row := ToolShapeRow{
			Shape:         t.Shape,
			Requests:      t.Requests,
			Declared:      t.Declared,
			DeclaredBytes: t.DeclaredBytes,
			Calls:         t.Calls,
			NeverCalled:   t.NeverCalled,
		}
		row.SchemaBytesShipped = t.DeclaredBytes * int64(t.Requests)
		row.DistinctCalled = len(t.Calls)
		if len(t.Declared) > 0 {
			row.DeclareUtilization = round2(float64(row.DistinctCalled) / float64(len(t.Declared)))
		}
		row.SchemaWasteBytes = int64(float64(row.SchemaBytesShipped) * (1 - row.DeclareUtilization))
		out = append(out, row)
	}
	return out
}
