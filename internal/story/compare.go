// Ver 2026-07-30 12:00, by Sonnet 5

// Step 4's 4d module (design doc Appendix C.6/G): diffing two Journeys'
// already-computed Metrics is the whole deliverable — "两份剖面做差就是对比
// 报告的骨架" — so this file adds no new data collection, only a rule-based
// diff over JourneySummary.Metrics. Purely a profile-layer feature (design
// doc §3.3's layering): every number here is derived by a fixed formula from
// two Metrics values, "notable" is a fixed threshold, and nothing here calls
// an LLM or generates free-text commentary — that's Phase 4a's job, if it
// ever happens.
package story

import "time"

// MetricKind tags how a MetricDiff's A/B/values should be rendered — kept as
// a string tag (not a formatting closure) so Comparison stays a plain,
// JSON-serializable value.
type MetricKind string

const (
	KindMillis   MetricKind = "ms"       // milliseconds, rendered via fmtutil.FmtSeconds
	KindTokens   MetricKind = "tokens"   // token count, rendered via fmtTokens
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
	Label string     `json:"label"`
	Kind  MetricKind `json:"kind"`
	A     float64    `json:"a"`
	B     float64    `json:"b"`
	// DeltaRel is (B-A) / max(|A|,|B|) — signed relative change from A to B,
	// 0 when both are exactly 0 (no signal, not a 0% "no change" claim).
	DeltaRel float64 `json:"delta_rel"`
	Notable  bool    `json:"notable"`
}

func metricDiff(label string, kind MetricKind, a, b float64) MetricDiff {
	d := MetricDiff{Label: label, Kind: kind, A: a, B: b}
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

// ToolShareDiff is one tool name's call-count share in each Journey, for the
// side-by-side tool-usage comparison — a qualitative complement to the
// numeric MetricDiff rows (design doc §6.5's "工作方式画像").
type ToolShareDiff struct {
	Name   string `json:"name"`
	ACalls int    `json:"a_calls"`
	BCalls int    `json:"b_calls"`
}

// JourneyRef identifies which Journey is which side of a Comparison —
// enough for a reader (or a consumer of compare-<a>-<b>.json in isolation)
// to know what was compared, without re-embedding that Journey's entire
// Metrics a second time (Rows already carries the numbers that matter).
type JourneyRef struct {
	ID    string    `json:"id"`
	Title string    `json:"title"`
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
}

func journeyRef(s JourneySummary) JourneyRef {
	return JourneyRef{ID: s.ID, Title: s.Title, From: s.From, To: s.To}
}

// Comparison is A-vs-B's full diff — design doc Appendix C.6 4d's entire
// deliverable. Field order in Rows is fixed (not sorted by magnitude) so
// repeated runs against the same two Summaries produce byte-identical
// output.
type Comparison struct {
	A     JourneyRef      `json:"a_journey"`
	B     JourneyRef      `json:"b_journey"`
	Rows  []MetricDiff    `json:"rows"`
	Tools []ToolShareDiff `json:"tools"` // union of both sides' tool names, A's Count-desc order first then B-only names
}

// Compare diffs a and b's Metrics — the whole of Phase 4d (design doc
// Appendix C.6/G.1: "两份剖面做差就是对比报告的骨架"). Order is fixed:
// caller decides which Journey is "A" and which is "B" (e.g. baseline vs
// candidate); Compare doesn't sort or normalize that choice away.
func Compare(a, b JourneySummary) Comparison {
	ma, mb := a.Metrics, b.Metrics
	rows := []MetricDiff{
		metricDiff("模型时间", KindMillis, float64(ma.ModelMS), float64(mb.ModelMS)),
		metricDiff("Agent 侧执行时间", KindMillis, float64(ma.AgentExecMS), float64(mb.AgentExecMS)),
		metricDiff("人类空闲时间", KindMillis, float64(ma.HumanIdleMS), float64(mb.HumanIdleMS)),
		metricDiff("净工作时长", KindMillis, float64(ma.NetWorkingMS), float64(mb.NetWorkingMS)),
		metricDiff("模型/工具时间比", KindMultiple, ma.ModelToToolRatio, mb.ModelToToolRatio),
		metricDiff("工具调用次数", KindCount, float64(ma.ToolCallCount), float64(mb.ToolCallCount)),
		metricDiff("重复动作率", KindRatio, ma.DuplicateActionRate, mb.DuplicateActionRate),
		metricDiff("错误恢复次数", KindCount, float64(ma.ErrorRecoveryCount), float64(mb.ErrorRecoveryCount)),
		metricDiff("计划/执行比", KindRatio, ma.PlanExecRatio, mb.PlanExecRatio),
		metricDiff("上下文有效利用率", KindRatio, ma.ContextUtilization, mb.ContextUtilization),
		metricDiff("Compaction 次数", KindCount, float64(ma.CompactionCount), float64(mb.CompactionCount)),
		metricDiff("Compaction 信息损失", KindTokens, float64(ma.CompactionLossTokens), float64(mb.CompactionLossTokens)),
	}
	return Comparison{A: journeyRef(a), B: journeyRef(b), Rows: rows, Tools: toolShareDiff(ma.ToolCallDist, mb.ToolCallDist)}
}

// toolShareDiff merges two Journeys' tool-call distributions by name: A's
// tools first (in A's own Count-desc order, matching ToolCallDist's own
// sort), then any tool B called that A never did.
func toolShareDiff(a, b []ToolCallStat) []ToolShareDiff {
	bCounts := make(map[string]int, len(b))
	for _, t := range b {
		bCounts[t.Name] = t.Count
	}
	seen := make(map[string]bool, len(a))
	out := make([]ToolShareDiff, 0, len(a)+len(b))
	for _, t := range a {
		out = append(out, ToolShareDiff{Name: t.Name, ACalls: t.Count, BCalls: bCounts[t.Name]})
		seen[t.Name] = true
	}
	for _, t := range b {
		if !seen[t.Name] {
			out = append(out, ToolShareDiff{Name: t.Name, ACalls: 0, BCalls: t.Count})
		}
	}
	return out
}
