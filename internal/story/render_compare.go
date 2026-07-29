// Ver 2026-07-30 21:00, by Sonnet 5

package story

import (
	"fmt"
	"strings"
	"time"

	"vmr/internal/fmtutil"
)

// RenderComparisonMarkdown renders cmp as a self-contained Markdown
// document: a header identifying both Journeys, a metric-by-metric diff
// table with notable rows starred, and a tool-usage side-by-side. Purely a
// view over already-computed Comparison data — same fact-layer-renderer
// convention as RenderMarkdown (no judgment calls happen here).
func RenderComparisonMarkdown(cmp Comparison) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("# Journey 对比：A vs B\n\n")
	w("**A** %s\n> %s\n> %s → %s\n\n", cmp.A.ID, cmp.A.Title, cmp.A.From.Format("2006-01-02 15:04:05"), cmp.A.To.Format("15:04:05"))
	w("**B** %s\n> %s\n> %s → %s\n\n", cmp.B.ID, cmp.B.Title, cmp.B.From.Format("2006-01-02 15:04:05"), cmp.B.To.Format("15:04:05"))

	w("## 行为剖面对比\n\n")
	w("| 指标 | A | B | 相对变化 |\n|---|---|---|---|\n")
	for _, r := range cmp.Rows {
		mark := ""
		if r.Notable {
			mark = " ⚠️"
		}
		w("| %s%s | %s | %s | %s |\n", r.Label, mark, formatMetric(r.Kind, r.A), formatMetric(r.Kind, r.B), formatDeltaRel(r.DeltaRel))
	}
	w("\n> ⚠️ = 相对变化 ≥ %.0f%% 且绝对差值超过噪声阈值——一个规则性的\"值得看一眼\"标记，不代表已判断出原因。\n\n", notableRelThreshold*100)

	if cmp.Extras != nil {
		renderDurationAndFinalContext(w, cmp.Extras)
	}

	if len(cmp.Tools) > 0 {
		w("## 工具调用对比\n\n")
		w("| 工具 | A 次数 | B 次数 |\n|---|---|---|\n")
		for _, t := range cmp.Tools {
			w("| %s | %d | %d |\n", t.Name, t.ACalls, t.BCalls)
		}
		w("\n")
	}

	if cmp.Extras != nil {
		renderEndpoints(w, cmp.Extras.Endpoints)
		renderCache(w, cmp.Extras.Cache)
		renderSysPrompt(w, cmp.Extras.SysPrompt)
		renderDeliverable(w, cmp.Extras.Deliverable)
		renderSources(w, cmp.Extras.Sources)
	}

	return b.String()
}

// renderSources renders the evidence-provenance section (plan review
// `docs/Step4a_compare_LLM解读层_差距分析与改进建议_2026-07-30_sonnet-5.md`
// §6.2 item 2) — the source audit file paths both Journeys were built from,
// so a reader can independently re-open the exact records every number above
// came from. Placed last, after every fact-layer section: this is a
// verification aid, not something that should compete for attention with the
// report's actual findings. Empty (no Sources set, e.g. a caller that built
// Extras without plumbing the resolved input paths through) renders nothing.
func renderSources(w func(string, ...any), sources []string) {
	if len(sources) == 0 {
		return
	}
	w("## 证据溯源\n\n")
	w("本报告所有数字均计算自以下源审计文件：\n\n")
	for _, s := range sources {
		w("- `%s`\n", s)
	}
	w("\n")
}

// renderDurationAndFinalContext renders the three free facts absorbed from
// the plan review (`_tmp/plan_sonnet-5.md` §2 point 2): wall-clock duration
// (captioned per design doc F10 — never presented as an efficiency number on
// its own), termination mode (the closest VMR-visible proxy to "did
// something like loop detection cut this off"), and each side's final-round
// context composition (free: ContextPoint is Metrics.ContextCurve's own
// element type, just its last entry).
func renderDurationAndFinalContext(w func(string, ...any), ex *ComparisonExtras) {
	d := ex.Duration
	w("总耗时（墙钟）：A %s · B %s —— 含人类空闲时间，不是效率指标，效率请看上表的\"净工作时长\"（设计文档 F10）。\n\n",
		fmtutil.FmtSeconds(d.AWall, 1), fmtutil.FmtSeconds(d.BWall, 1))
	w("终止方式：A `finish=%s` · B `finish=%s`——VMR 只能看到这一步的结果，看不到 Agent 自身是否配置了类似 loop detection 的机制。\n\n",
		emptyDash(d.ATermination), emptyDash(d.BTermination))

	fc := ex.FinalContext
	// Either side being empty (Seq == 0, ComputeComparisonExtras' zero value
	// for a Journey whose Metrics.ContextCurve came back empty) skips the
	// whole table — a real Journey always has at least one Step so this
	// shouldn't fire in practice, but `||` (not `&&`) is what actually
	// guarantees no "B（第 0 轮）" row ever renders if it ever does.
	if fc.A.Seq == 0 || fc.B.Seq == 0 {
		return
	}
	w("**末轮上下文构成**\n\n")
	w("| | A（第 %d 轮） | B（第 %d 轮） |\n|---|---|---|\n", fc.A.Seq, fc.B.Seq)
	w("| system | %s | %s |\n", fmtTokens(fc.A.SystemTokens), fmtTokens(fc.B.SystemTokens))
	w("| user | %s | %s |\n", fmtTokens(fc.A.UserTokens), fmtTokens(fc.B.UserTokens))
	w("| assistant | %s | %s |\n", fmtTokens(fc.A.AssistantTokens), fmtTokens(fc.B.AssistantTokens))
	w("| tool | %s | %s |\n\n", fmtTokens(fc.A.ToolTokens), fmtTokens(fc.B.ToolTokens))
}

func emptyDash(s string) string {
	if s == "" {
		return "(无)"
	}
	return s
}

// renderEndpoints renders the model/endpoint identity check — deepseek
// report §3 generalized to not assume the two sides necessarily match.
func renderEndpoints(w func(string, ...any), ep EndpointsFact) {
	w("## 模型与端点核查\n\n")
	w("- A: %s\n", endpointList(ep.A))
	w("- B: %s\n\n", endpointList(ep.B))
	if ep.Same {
		w("两侧模型/端点完全相同。\n\n")
	} else {
		w("两侧模型/端点**不同**——这本身可能是效果差异的一个直接原因，不要默认排除。\n\n")
	}
}

func endpointList(eps []string) string {
	if len(eps) == 0 {
		return "(未识别到任何端点)"
	}
	return "`" + strings.Join(eps, "`, `") + "`"
}

// renderCache renders the per-step prompt-cache hit-ratio summary — deepseek
// report §5's "18%→97% vs 82%→99%" observation, computed the same way for
// whatever numbers this pair of Journeys actually has.
func renderCache(w func(string, ...any), c CacheFact) {
	w("## Prompt 缓存命中率\n\n")
	if len(c.A.Series) == 0 && len(c.B.Series) == 0 {
		w("两侧均未取得可用的 usage/缓存数据。\n\n")
		return
	}
	w("| | 首轮 | 稳态均值（除首轮） | 最小 | 最大 |\n|---|---|---|---|---|\n")
	w("| A | %s | %s | %s | %s |\n", pctStr(c.A.FirstRatio), pctStr(c.A.SteadyMean), pctStr(c.A.Min), pctStr(c.A.Max))
	w("| B | %s | %s | %s | %s |\n\n", pctStr(c.B.FirstRatio), pctStr(c.B.SteadyMean), pctStr(c.B.Min), pctStr(c.B.Max))
	w("<details><summary>逐轮曲线</summary>\n\n")
	w("A: %s\n\n", cacheCurveLine(c.A.Series))
	w("B: %s\n\n", cacheCurveLine(c.B.Series))
	w("</details>\n\n")
}

func cacheCurveLine(series []CachePoint) string {
	if len(series) == 0 {
		return "(无数据)"
	}
	parts := make([]string, len(series))
	for i, p := range series {
		parts[i] = fmt.Sprintf("R%d %s", p.Seq, pctStr(p.Ratio))
	}
	return strings.Join(parts, " → ")
}

// renderSysPrompt renders each side's effective system-prompt size/stability
// plus a bounded, explicitly-labeled excerpt — the excerpt is raw evidence
// for a human (or, if -llm-addr is given, the LLM interpretation layer) to
// read; this renderer does not attempt to parse it for tool names or loaded
// context files (see the plan doc's rejection of that as a rule-layer job).
func renderSysPrompt(w func(string, ...any), sp SysPromptFact) {
	w("## System Prompt 规模与稳定性\n\n")
	w("| | tokens | 变更次数 |\n|---|---|---|\n")
	w("| A | %s | %d |\n", fmtTokens(sp.A.Tokens), sp.A.Changes)
	w("| B | %s | %d |\n\n", fmtTokens(sp.B.Tokens), sp.B.Changes)
	renderExcerpt(w, "A 的 system prompt 节选", sp.A.Excerpt, sp.A.Truncated)
	renderExcerpt(w, "B 的 system prompt 节选", sp.B.Excerpt, sp.B.Truncated)
}

// renderDeliverable renders the final-write-shaped tool call each side
// produced, if any — the "result difference" dimension (design doc's four-
// dimension breakdown in the plan doc's review banner), not just process
// metrics.
func renderDeliverable(w func(string, ...any), d DeliverableFact) {
	w("## 最终交付物对比\n\n")
	renderDeliverableSide(w, "A", d.A)
	renderDeliverableSide(w, "B", d.B)
}

func renderDeliverableSide(w func(string, ...any), label string, s DeliverableStats) {
	if !s.Found {
		w("**%s**：未识别到可比较的最终交付物（没有找到参数形状像文件写入的工具调用）。\n\n", label)
		return
	}
	w("**%s**：在第 %d 轮通过 `%s` 识别到疑似最终交付物。\n\n", label, s.StepSeq, s.ToolName)
	renderExcerpt(w, label+" 的交付物节选", s.Excerpt, s.Truncated)
}

func renderExcerpt(w func(string, ...any), summary, text string, truncated bool) {
	if text == "" {
		return
	}
	note := ""
	if truncated {
		note = "（已截断）"
	}
	w("<details><summary>%s%s</summary>\n\n````\n%s\n````\n</details>\n\n", summary, note, text)
}

// formatMetric renders one MetricDiff side's raw float64 per its Kind.
func formatMetric(kind MetricKind, v float64) string {
	switch kind {
	case KindMillis:
		return fmtutil.FmtSeconds(time.Duration(v)*time.Millisecond, 1)
	case KindTokens:
		return fmtTokens(int64(v))
	case KindRatio:
		return pctStr(v)
	case KindMultiple:
		return fmt.Sprintf("%.2f×", v)
	default: // KindCount
		return fmt.Sprintf("%d", int64(v))
	}
}

// formatDeltaRel renders a signed relative change as a percentage with an
// explicit sign — "+42%"/"-15%"/"0%" — 0% both when nothing changed and when
// both sides were 0 (MetricDiff.DeltaRel doesn't distinguish those; the
// side-by-side A/B columns already show the absolute values either way).
func formatDeltaRel(rel float64) string {
	sign := "+"
	if rel < 0 {
		sign = "-"
		rel = -rel
	}
	return fmt.Sprintf("%s%.0f%%", sign, rel*100)
}
