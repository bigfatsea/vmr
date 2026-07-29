// Ver 2026-07-30 21:00, by Sonnet 5

// Step 4's 4d module (design doc Appendix C.6/G): diffing two Journeys'
// already-computed Metrics is the whole deliverable — "两份剖面做差就是对比
// 报告的骨架" — so this file adds no new data collection, only a rule-based
// diff over JourneySummary.Metrics. Purely a profile-layer feature (design
// doc §3.3's layering): every number here is derived by a fixed formula from
// two Metrics values, "notable" is a fixed threshold, and nothing here calls
// an LLM or generates free-text commentary — that's Phase 4a's job, if it
// ever happens.
package story

import (
	"encoding/json"
	"strings"
	"time"

	"vmr/internal/chatmsg"
	"vmr/internal/core"
)

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

	// Extras is the step-4a-into-4d slice's rule-derived facts (design doc
	// Appendix G's plan doc, `_tmp/plan_sonnet-5.md`): endpoint/cache/system-
	// prompt/final-context/duration/deliverable — everything Compare's own
	// Metrics diff can't see because it needs Step/Manifest access, not just
	// JourneySummary. Pointer and omitempty on purpose: a caller that only
	// has two JourneySummary values (e.g. the existing unit tests) gets a nil
	// Extras, and RenderComparisonMarkdown skips those sections entirely
	// rather than panicking or rendering an empty table — Extras is always
	// an addition, never a requirement for a valid Comparison.
	Extras *ComparisonExtras `json:"extras,omitempty"`
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

// sysPromptExcerptChars/deliverableExcerptChars bound the two raw-text
// excerpts Extras carries — both feed the LLM evidence pack (`_tmp/plan_
// sonnet-5.md` §3.1) and get rendered as folded <details> blocks in the
// Markdown, but neither may be unbounded: these two Journeys' full bodies
// are 6846/3101 lines, nowhere near what a report row or an LLM prompt
// should carry verbatim.
//
// sysPromptExcerptChars was originally 4000, tuned for "some bounded prefix"
// without checking where the information actually lives in a real system
// prompt. Measured directly against the two validation Journeys' audit
// records (`docs/Step4a_compare_LLM解读层_差距分析与改进建议_2026-07-30_
// sonnet-5.md` §3.2/§6), the "# Project Context" block declaring which
// project files got loaded (deepseek report's top-ranked root cause) sits at
// character 15734/13656 of a 36871/51406-char system prompt — 3.4-3.9x past
// the old 4000 cutoff, so the LLM interpretation layer structurally never saw
// it. 20000 covers both with headroom while staying well inside the "up to a
// few tens of thousands of tokens" evidence-pack budget the plan already
// targeted (two sides' worth of system-prompt excerpt is ~5K tokens each).
// This does NOT reach every possible framework's equivalent block — it's a
// measured fit for this project's validation corpus, not a guarantee.
const (
	sysPromptExcerptChars   = 20000
	deliverableExcerptChars = 6000
)

// ComparisonExtras is the rule-derived evidence beyond the nine Metrics
// rows — endpoint identity, cache behavior, system-prompt scale/stability,
// final-round context shape, wall time/termination, and (new, highest-ROI
// addition from the plan review) the final deliverable itself, when one is
// identifiable. Every field here is computed from Step/Manifest data that
// Metrics doesn't carry (Metrics is JourneySummary-only; this needs the full
// *Journey) — nothing here calls an LLM or invents a number.
type ComparisonExtras struct {
	Endpoints    EndpointsFact    `json:"endpoints"`
	Cache        CacheFact        `json:"cache"`
	SysPrompt    SysPromptFact    `json:"sys_prompt"`
	FinalContext FinalContextFact `json:"final_context"`
	Duration     DurationFact     `json:"duration"`
	Deliverable  DeliverableFact  `json:"deliverable"`

	// Sources is the sorted list of source audit file paths both Journeys
	// were built from (evidence-provenance addition from the plan review,
	// `docs/Step4a_compare_LLM解读层_差距分析与改进建议_2026-07-30_sonnet-5.md`
	// §6.2 item 2) — lets a reader independently re-open the exact records
	// every number above was computed from, without having to reverse-
	// engineer -c's log_dir default. Deliberately NOT computed by
	// ComputeComparisonExtras itself: it has no access to the resolved input
	// paths (those live at the cmd/vmr CLI layer, from resolveInputPaths),
	// so the caller sets this field directly, the same way it already sets
	// Comparison.Extras itself after computing it.
	Sources []string `json:"sources,omitempty"`
}

// EndpointsFact lists each side's distinct "protocol:provider:model" strings
// (ctxgraph.Manifest.Endpoint), first-seen order — deepseek report §3's
// "model and route verification", generalized to not assume the two sides
// necessarily match (design doc: "not everything that differs is an agent
// difference" applies here too — a genuine model/endpoint mismatch is
// itself a finding, not an error).
//
// Per-Step granularity only: Manifest.Endpoint is the FINAL attempt's
// endpoint (manifest.go's own doc comment), so a Step that failed over
// mid-request (provider A errors, VMR retries and succeeds on B) only ever
// contributes B here — the same "who ultimately served this" convention
// internal/report already uses, not a gap specific to this feature. Across
// Steps, a genuine model/provider switch mid-Journey still shows up as a
// second distinct string.
type EndpointsFact struct {
	A    []string `json:"a"`
	B    []string `json:"b"`
	Same bool     `json:"same"` // same set, order-independent
}

// CachePoint is one Step's prompt-cache hit ratio (CacheRead/In); 0 when In
// is 0 or usage wasn't reported (UsageOK false) for that step.
type CachePoint struct {
	Seq   int     `json:"seq"`
	Ratio float64 `json:"ratio"`
}

// CacheStats summarizes one side's per-step cache hit ratio — deepseek
// report §5's "18%→97% vs 82%→99%" observation, generalized to whatever
// numbers this Journey actually has (no assumption that either side
// stabilizes at all).
type CacheStats struct {
	FirstRatio float64      `json:"first_ratio"`
	SteadyMean float64      `json:"steady_mean"` // mean over steps 2..N; 0 if fewer than 2 usable steps
	Min        float64      `json:"min"`
	Max        float64      `json:"max"`
	Series     []CachePoint `json:"series"`
}

type CacheFact struct {
	A CacheStats `json:"a"`
	B CacheStats `json:"b"`
}

// SysPromptStats is one side's "effective" system prompt (the one in force
// as of its last SysChanged step, or the first step if it never changed) —
// size, how many times it changed, and a bounded excerpt of the text itself.
// The excerpt is deliberately NOT parsed for tool names/loaded context
// files here (see the plan doc's rejection of that as a rule-layer job —
// framework-specific regex is fragile and belongs, if anywhere, in the LLM
// interpretation layer that actually reads this excerpt).
type SysPromptStats struct {
	Tokens    int64  `json:"tokens"`
	Changes   int    `json:"changes"`
	Excerpt   string `json:"excerpt"`
	Truncated bool   `json:"truncated"`
}

type SysPromptFact struct {
	A SysPromptStats `json:"a"`
	B SysPromptStats `json:"b"`
}

// FinalContextFact is each side's last Step's token composition by role —
// deepseek report §4.3's "tool tokens differ 2.3x" style observation,
// already free: ContextPoint is Metrics.ContextCurve's own element type.
type FinalContextFact struct {
	A ContextPoint `json:"a"`
	B ContextPoint `json:"b"`
}

// DurationFact pairs each side's wall-clock span with its last Step's
// finish reason. AWall/BWall must always be rendered next to the existing
// NetWorkingMS row (Rows), never alone — design doc F10 already flagged
// "16 minutes vs 7.5 minutes" as a misleading efficiency claim when human
// idle time is folded in; Termination is the closest VMR-visible proxy to
// "did something like loop detection cut this off", since the actual
// mechanism (if any) lives in the agent's own config, not the audit log.
type DurationFact struct {
	AWall        time.Duration `json:"a_wall_ns"` // time.Duration's own JSON encoding: nanoseconds
	BWall        time.Duration `json:"b_wall_ns"`
	ATermination string        `json:"a_termination"` // last Step's Finish; "" if the Journey has no steps
	BTermination string        `json:"b_termination"`
}

// DeliverableStats is the final write-shaped tool call this side's Journey
// made, if any — the "result difference" dimension neither this project's
// first-draft plan nor the reviewed alternative plan originally covered
// (see `_tmp/plan_sonnet-5.md`'s top banner). Found=false is a fact, not a
// failure: plenty of tasks never produce a single-file deliverable, and the
// rendered report says so plainly instead of leaving a blank row.
type DeliverableStats struct {
	Found     bool   `json:"found"`
	ToolName  string `json:"tool_name,omitempty"`
	StepSeq   int    `json:"step_seq,omitempty"`
	Excerpt   string `json:"excerpt,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type DeliverableFact struct {
	A DeliverableStats `json:"a"`
	B DeliverableStats `json:"b"`
}

// ComputeComparisonExtras derives ComparisonExtras for jA/jB — the
// step-4a-into-4d slice's rule layer (`_tmp/plan_sonnet-5.md` §2/§5). ma/mb
// are the same Metrics Compare(Summarize(jA), Summarize(jB)) already
// computed — passed in rather than recomputed so a caller that already has
// both JourneySummary values doesn't pay for ComputeMetrics twice.
func ComputeComparisonExtras(jA, jB *Journey, ma, mb Metrics) ComparisonExtras {
	return ComparisonExtras{
		Endpoints:    endpointsFact(jA, jB),
		Cache:        CacheFact{A: cacheStats(jA), B: cacheStats(jB)},
		SysPrompt:    SysPromptFact{A: sysPromptStats(jA), B: sysPromptStats(jB)},
		FinalContext: finalContextFact(ma, mb),
		Duration:     durationFact(jA, jB),
		Deliverable:  DeliverableFact{A: deliverableStats(jA), B: deliverableStats(jB)},
	}
}

func endpointsFact(jA, jB *Journey) EndpointsFact {
	a, b := distinctEndpoints(jA), distinctEndpoints(jB)
	return EndpointsFact{A: a, B: b, Same: sameSet(a, b)}
}

// distinctEndpoints lists j's steps' Manifest.Endpoint values, first-seen
// order, skipping the "-" sentinel ctxgraph.Manifest.Endpoint uses when a
// request never reached an upstream attempt.
func distinctEndpoints(j *Journey) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range journeySteps(j) {
		ep := s.Manifest.Endpoint
		if ep == "" || ep == "-" || seen[ep] {
			continue
		}
		seen[ep] = true
		out = append(out, ep)
	}
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	bSet := make(map[string]bool, len(b))
	for _, v := range b {
		bSet[v] = true
	}
	for _, v := range a {
		if !bSet[v] {
			return false
		}
	}
	return true
}

func cacheStats(j *Journey) CacheStats {
	var series []CachePoint
	for _, s := range journeySteps(j) {
		u := s.Manifest.Usage
		if !s.Manifest.UsageOK || u.In <= 0 {
			continue
		}
		series = append(series, CachePoint{Seq: s.Seq, Ratio: float64(u.CacheRead) / float64(u.In)})
	}
	if len(series) == 0 {
		return CacheStats{}
	}
	stats := CacheStats{FirstRatio: series[0].Ratio, Min: series[0].Ratio, Max: series[0].Ratio, Series: series}
	var steadySum float64
	for i, p := range series {
		if p.Ratio < stats.Min {
			stats.Min = p.Ratio
		}
		if p.Ratio > stats.Max {
			stats.Max = p.Ratio
		}
		if i > 0 {
			steadySum += p.Ratio
		}
	}
	if len(series) > 1 {
		stats.SteadyMean = steadySum / float64(len(series)-1)
	}
	return stats
}

// sysPromptStats finds j's "effective" system prompt: the last Step whose
// SysChanged is true, or the first Step if it never changed (Step.SysChanged
// is always false for a Journey's very first Step by construction — there is
// no predecessor manifest to diff against — so "never changed" and "first
// step" naturally coincide when there's no SysChanged step at all).
func sysPromptStats(j *Journey) SysPromptStats {
	steps := journeySteps(j)
	if len(steps) == 0 {
		return SysPromptStats{}
	}
	target := steps[0]
	changes := 0
	for _, s := range steps {
		if s.SysChanged {
			changes++
			target = s
		}
	}
	texts := systemMessageTexts(target)
	tokens := systemTokenCount(texts)
	text, truncated := truncateText(strings.Join(texts, "\n\n"), sysPromptExcerptChars)
	return SysPromptStats{Tokens: tokens, Changes: changes, Excerpt: text, Truncated: truncated}
}

// systemMessageTexts parses s's leading system-role message(s) (LeadSys of
// them, mirroring how ctxgraph folds them for SysHash) exactly once — both
// the token count (systemTokenCount) and the excerpt text sysPromptStats
// builds derive from this same slice, instead of each independently
// re-parsing s.Rec.Client.Request.Body (the previous shape of this code: a
// contextTokensAt(j, seq) that re-walked journeySteps AND re-parsed the
// body, plus a separate sysPromptText(s) that parsed the same body again).
func systemMessageTexts(s *Step) []string {
	if !s.Manifest.HasSys {
		return nil
	}
	body, _ := s.Rec.Client.Request.Body.(map[string]any)
	msgs := chatmsg.Messages(body)
	n := s.Manifest.LeadSys
	if n > len(msgs) {
		n = len(msgs)
	}
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		parts = append(parts, msgs[i].Text)
	}
	return parts
}

// systemTokenCount sums core.EstimateTextTokens over texts — the same
// estimator contextCurve() uses for its own SystemTokens field, so the two
// numbers are computed the same way. They aren't guaranteed identical for a
// given Step, though: this sums only the leading system-role block (LeadSys
// messages), while contextCurve sums every role=="system" message anywhere
// in that Step's body. In every corpus seen so far system messages only
// ever appear in that leading block, so the two agree in practice — but
// that's an empirical property of today's traffic, not something either
// type enforces, so a future framework that injects a system message
// mid-conversation would make them diverge silently.
func systemTokenCount(texts []string) int64 {
	var tokens int64
	for _, t := range texts {
		tokens += core.EstimateTextTokens([]byte(t))
	}
	return tokens
}

func finalContextFact(ma, mb Metrics) FinalContextFact {
	var a, b ContextPoint
	if n := len(ma.ContextCurve); n > 0 {
		a = ma.ContextCurve[n-1]
	}
	if n := len(mb.ContextCurve); n > 0 {
		b = mb.ContextCurve[n-1]
	}
	return FinalContextFact{A: a, B: b}
}

func durationFact(jA, jB *Journey) DurationFact {
	return DurationFact{
		AWall: jA.To.Sub(jA.From), BWall: jB.To.Sub(jB.From),
		ATermination: lastFinish(jA), BTermination: lastFinish(jB),
	}
}

func lastFinish(j *Journey) string {
	steps := journeySteps(j)
	if len(steps) == 0 {
		return ""
	}
	return steps[len(steps)-1].Finish
}

// deliverableFileKeys/deliverableContentKeys are the generic argument-shape
// signal deliverableStats uses instead of matching against any specific tool
// name — "参数形状像文件写入" per the plan doc's rejection of framework-
// specific detection: any tool whose JSON arguments carry both a path-like
// and a content-like string field reads as a file-write regardless of what
// the agent framework happens to call that tool (OpenClaw's "write", or any
// other framework's equivalent).
var (
	deliverableFileKeys    = []string{"path", "file_path", "filepath", "filename", "file"}
	deliverableContentKeys = []string{"content", "text", "body", "data"}
)

// deliverableStats scans j's steps in reverse (most recent first) for the
// last tool call whose arguments look like a file write, so a Journey that
// wrote intermediate scratch files and then the real report last still picks
// the real report. Found=false (not an error) when nothing matches.
func deliverableStats(j *Journey) DeliverableStats {
	steps := journeySteps(j)
	for i := len(steps) - 1; i >= 0; i-- {
		s := steps[i]
		for k := len(s.ToolCalls) - 1; k >= 0; k-- {
			tc := s.ToolCalls[k]
			var args map[string]any
			if json.Unmarshal([]byte(tc.Args), &args) != nil {
				continue
			}
			content, ok := firstStringField(args, deliverableContentKeys)
			if !ok {
				continue
			}
			if _, ok := firstStringField(args, deliverableFileKeys); !ok {
				continue
			}
			excerpt, truncated := truncateText(content, deliverableExcerptChars)
			return DeliverableStats{Found: true, ToolName: tc.Name, StepSeq: s.Seq, Excerpt: excerpt, Truncated: truncated}
		}
	}
	return DeliverableStats{}
}

func firstStringField(m map[string]any, keys []string) (string, bool) {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v, true
		}
	}
	return "", false
}

// truncateText bounds s to at most maxChars runes, reporting whether it had
// to cut — shared by both excerpt fields (system prompt, final deliverable)
// so neither ever grows unbounded into the rendered report or the LLM
// prompt.
func truncateText(s string, maxChars int) (string, bool) {
	r := []rune(s)
	if len(r) <= maxChars {
		return s, false
	}
	return string(r[:maxChars]), true
}
