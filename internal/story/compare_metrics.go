// Ver 2026-08-20, by Sonnet 5

package story

import (
	"fmt"
	"strconv"
	"time"

	"vmr/internal/fmtutil"
)

// MetricKind tags how a MetricDiff's A/B/values should be rendered — kept as
// a string tag (not a formatting closure) so Comparison stays a plain,
// JSON-serializable value.
type MetricKind string

const (
	KindMillis   MetricKind = "ms"       // milliseconds, rendered via fmtutil.FmtSeconds
	KindTokens   MetricKind = "tokens"   // token count, rendered via fmtutil.FmtTokens
	KindCount    MetricKind = "count"    // plain integer
	KindRatio    MetricKind = "ratio"    // 0..1 fraction, rendered as a percentage
	KindMultiple MetricKind = "multiple" // an unbounded ratio (e.g. model/tool time), rendered as "x.xx×"
)

// notableFloor is the minimum |A-B| a Kind needs to clear before a large
// RELATIVE difference is worth flagging — otherwise a tiny Journey (e.g. one
// tool call vs zero) reports a meaningless "∞% different" on pure noise.
// Values are deliberately generous rather than precise: this is a triage
// flag ("worth a second look"), not a statistical test.
var notableFloor = map[MetricKind]float64{
	KindMillis:   2000, // 2s
	KindTokens:   200,
	KindCount:    2,
	KindRatio:    0.05, // 5 percentage points
	KindMultiple: 0.3,
}

// notableRelThreshold is the relative-difference bar (see MetricDiff.DeltaRel)
// a row must also clear, in addition to notableFloor, to be flagged.
const notableRelThreshold = 0.30

// MetricDiff is one behavior-profile metric's value in both Journeys, plus a
// rule-derived "notable" flag — no judgment about WHY they differ, only
// THAT they differ by more than a fixed bar (design doc's "宁可粗糙也不猜语义").
type MetricDiff struct {
	// Metric is a stable, non-localized identifier for programmatic consumption.
	Metric MetricCode `json:"metric"`
	Label  string     `json:"label"`
	Kind   MetricKind `json:"kind"`
	A      float64    `json:"a"`
	B      float64    `json:"b"`
	// DeltaRel is (B-A) / max(|A|,|B|) — signed relative change from A to B,
	// 0 when both are exactly 0 (no signal, not a 0% "no change" claim).
	DeltaRel float64 `json:"delta_rel"`
	Notable  bool    `json:"notable"`
}

// MetricCode identifies which behavior-profile metric a MetricDiff row is,
// independent of its (localized) display Label. See MetricDiff.Metric.
type MetricCode string

const (
	MetricModelMS              MetricCode = "model_ms"
	MetricAgentExecMS          MetricCode = "agent_exec_ms"
	MetricHumanIdleMS          MetricCode = "human_idle_ms"
	MetricNetWorkingMS         MetricCode = "net_working_ms"
	MetricModelToolRatio       MetricCode = "model_tool_ratio"
	MetricToolCallCount        MetricCode = "tool_call_count"
	MetricDuplicateActionRate  MetricCode = "duplicate_action_rate"
	MetricErrorRecoveryCount   MetricCode = "error_recovery_count"
	MetricPlanExecRatio        MetricCode = "plan_exec_ratio"
	MetricContextUtilization   MetricCode = "context_utilization"
	MetricCompactionCount      MetricCode = "compaction_count"
	MetricCompactionLossTokens MetricCode = "compaction_loss_tokens"
	// MetricModelSwitchCount  is len(Metrics.ModelSwitches) — a
	// ROUTING-ENVIRONMENT variable, not an agent-behavior one: failover,
	// sticky-TTL expiry, and routing-policy changes all produce a switch
	// with no change in what the agent itself did. In corpus.go's
	// correlation matrix this reads as "did these two groups' routing
	// environment differ", never as "did the agent behave differently".
	MetricModelSwitchCount     MetricCode = "model_switch_count"
	MetricOutputRepetitionRate MetricCode = "output_repetition_rate"
)

// metricSpec is one behavior-profile metric's full definition: its stable
// Code, how to render it (Kind), and how to pull its value out of a Metrics
// value. Its display label is NOT stored here — see i18n.MetricLabel(lang,
// string(Code)), the single lookup Compare and render_corpus.go both use;
// keeping the label out of this struct means there is exactly one place a
// metric's display text can come from, not two (this struct plus the i18n
// table) that could drift apart. metricSpecs below is the ONE authoritative
// list of the behavior-profile metrics vmr tracks — both Compare's per-row
// diff and corpus.go's per-metric distribution/correlation/Markdown-
// rendering range over this same slice, instead of each independently
// declaring which metrics exist (corpus.go used to hand-maintain its own
// copy of this exact code/kind/extractor mapping across three separate
// declarations, kept in sync with Compare only by a comment asserting they
// matched).
type metricSpec struct {
	Code  MetricCode
	Kind  MetricKind
	Value func(Metrics) float64
}

// metricSpecs' order is Comparison.Rows' order — fixed, not sorted by
// magnitude, so repeated runs against the same two Summaries produce
// byte-identical output (see Comparison's own doc comment).
var metricSpecs = []metricSpec{
	{MetricModelMS, KindMillis, func(m Metrics) float64 { return float64(m.ModelMS) }},
	{MetricAgentExecMS, KindMillis, func(m Metrics) float64 { return float64(m.AgentExecMS) }},
	{MetricHumanIdleMS, KindMillis, func(m Metrics) float64 { return float64(m.HumanIdleMS) }},
	{MetricNetWorkingMS, KindMillis, func(m Metrics) float64 { return float64(m.NetWorkingMS) }},
	{MetricModelToolRatio, KindMultiple, func(m Metrics) float64 { return m.ModelToToolRatio }},
	{MetricToolCallCount, KindCount, func(m Metrics) float64 { return float64(m.ToolCallCount) }},
	{MetricDuplicateActionRate, KindRatio, func(m Metrics) float64 { return m.DuplicateActionRate }},
	{MetricErrorRecoveryCount, KindCount, func(m Metrics) float64 { return float64(m.ErrorRecoveryCount) }},
	{MetricPlanExecRatio, KindRatio, func(m Metrics) float64 { return m.PlanExecRatio }},
	{MetricContextUtilization, KindRatio, func(m Metrics) float64 { return m.ContextUtilization }},
	{MetricCompactionCount, KindCount, func(m Metrics) float64 { return float64(m.CompactionCount) }},
	{MetricCompactionLossTokens, KindTokens, func(m Metrics) float64 { return float64(m.CompactionLossTokens) }},
	{MetricModelSwitchCount, KindCount, func(m Metrics) float64 { return float64(len(m.ModelSwitches)) }},
	{MetricOutputRepetitionRate, KindRatio, func(m Metrics) float64 { return m.OutputRepetitionRate }},
}

func metricDiff(code MetricCode, label string, kind MetricKind, a, b float64) MetricDiff {
	d := MetricDiff{Metric: code, Label: label, Kind: kind, A: a, B: b}
	denom := a
	if abs(b) > abs(a) {
		denom = b
	}
	if denom != 0 {
		d.DeltaRel = (b - a) / abs(denom)
	}
	floor := notableFloor[kind]
	d.Notable = abs(b-a) >= floor && abs(d.DeltaRel) >= notableRelThreshold
	return d
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// journeyMetric is one single-journey behavior-indicator row — the same
// metric universe as metricSpecs above, plus the journey-view formatting
// rule. renderBehaviorIndicators (Markdown) and htmlMetrics (HTML) both
// iterate this one slice, so the two formats can never again disagree about
// WHICH metrics a journey view shows (§2.81): the difference between them
// is only how one row is rendered, never which rows exist. Label is NOT
// stored here — i18n.MetricLabel resolves it, the same single source
// Compare uses.
type journeyMetric struct {
	Code  MetricCode
	Value func(Metrics) float64
	// Format renders one row's value. It receives the whole Metrics rather
	// than just the extracted scalar because two rows' display rules depend
	// on context the scalar doesn't carry: ModelToolRatio renders "—" when
	// AgentExecMS is 0 (no agent-side gap was observed anywhere, so 0.00×
	// would read as a measurement rather than an absence), and
	// ErrorRecoveryCount renders "n/a" for a non-Anthropic journey with
	// zero recoveries (the is_error marker it counts has no OpenAI
	// equivalent — see Metrics.ErrorRecoveryCount's doc comment).
	Format func(m Metrics, v float64, nonAnthropic bool) string
}

// journeyMetrics' order is both renderers' row order — fixed, not sorted by
// magnitude, so the same Journey renders byte-identically run to run.
var journeyMetrics = []journeyMetric{
	{MetricNetWorkingMS, func(m Metrics) float64 { return float64(m.NetWorkingMS) }, fmtJourneyMillis},
	{MetricModelMS, func(m Metrics) float64 { return float64(m.ModelMS) }, fmtJourneyMillis},
	{MetricAgentExecMS, func(m Metrics) float64 { return float64(m.AgentExecMS) }, fmtJourneyMillis},
	{MetricHumanIdleMS, func(m Metrics) float64 { return float64(m.HumanIdleMS) }, fmtJourneyMillis},
	{MetricModelToolRatio, func(m Metrics) float64 { return m.ModelToToolRatio }, fmtJourneyModelToolRatio},
	{MetricToolCallCount, func(m Metrics) float64 { return float64(m.ToolCallCount) }, fmtJourneyCount},
	{MetricDuplicateActionRate, func(m Metrics) float64 { return m.DuplicateActionRate }, fmtJourneyPct},
	{MetricOutputRepetitionRate, func(m Metrics) float64 { return m.OutputRepetitionRate }, fmtJourneyPct},
	{MetricErrorRecoveryCount, func(m Metrics) float64 { return float64(m.ErrorRecoveryCount) }, fmtJourneyErrorRecovery},
	{MetricPlanExecRatio, func(m Metrics) float64 { return m.PlanExecRatio }, fmtJourneyPct},
	{MetricContextUtilization, func(m Metrics) float64 { return m.ContextUtilization }, fmtJourneyPct},
	{MetricCompactionCount, func(m Metrics) float64 { return float64(m.CompactionCount) }, fmtJourneyCount},
	{MetricCompactionLossTokens, func(m Metrics) float64 { return float64(m.CompactionLossTokens) }, fmtJourneyTokens},
	{MetricModelSwitchCount, func(m Metrics) float64 { return float64(len(m.ModelSwitches)) }, fmtJourneyCount},
}

func fmtJourneyMillis(_ Metrics, v float64, _ bool) string {
	return fmtutil.FmtSeconds(time.Duration(int64(v))*time.Millisecond, 1)
}

func fmtJourneyCount(_ Metrics, v float64, _ bool) string {
	return strconv.Itoa(int(v))
}

func fmtJourneyPct(_ Metrics, v float64, _ bool) string {
	return pctStr(v)
}

func fmtJourneyTokens(_ Metrics, v float64, _ bool) string {
	return fmtutil.FmtTokens(int64(v))
}

func fmtJourneyModelToolRatio(m Metrics, v float64, _ bool) string {
	if m.AgentExecMS == 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f×", v)
}

func fmtJourneyErrorRecovery(_ Metrics, v float64, nonAnthropic bool) string {
	if nonAnthropic && int(v) == 0 {
		return "n/a"
	}
	return strconv.Itoa(int(v))
}
