// Ver 2026-08-01, by Sonnet 5

package story

import (
	"fmt"
	"strings"
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// RenderComparisonMarkdown renders cmp as a self-contained Markdown
// document in lang: a header identifying both Journeys, a metric-by-metric
// diff table with notable rows starred, and a tool-usage side-by-side.
// Purely a view over already-computed Comparison data — same fact-layer-
// renderer convention as RenderMarkdown (no judgment calls happen here).
// cmp.Rows[].Label is always English (Compare never sees lang — design doc
// §4.4); this function looks up each row's localized label from its stable
// Metric code via i18n.MetricLabel instead of using Label directly.
func RenderComparisonMarkdown(cmp Comparison, lang i18n.Lang) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	t := i18n.Compare(lang)

	w("%s", t.Title)
	w("%s", t.SideBlock("A", cmp.A.ID, cmp.A.Title, cmp.A.From.Format("2006-01-02 15:04:05"), cmp.A.To.Format("15:04:05")))
	w("%s", t.SideBlock("B", cmp.B.ID, cmp.B.Title, cmp.B.From.Format("2006-01-02 15:04:05"), cmp.B.To.Format("15:04:05")))

	w("%s", t.ProfileTitle)
	w("%s", t.ProfileTableHeader)
	for _, r := range cmp.Rows {
		mark := ""
		if r.Notable {
			mark = " ⚠️"
		}
		w("| %s%s | %s | %s | %s |\n", i18n.MetricLabel(lang, string(r.Metric)), mark, formatMetric(r.Kind, r.A), formatMetric(r.Kind, r.B), formatDeltaRel(r.DeltaRel))
	}
	w("%s", t.NotableFootnote(notableRelThreshold*100))

	if cmp.Extras != nil {
		renderDurationAndFinalContext(w, cmp.Extras, t)
	}

	if len(cmp.Tools) > 0 {
		w("%s", t.ToolsTitle)
		w("%s", t.ToolsTableHeader)
		for _, tl := range cmp.Tools {
			w("| %s | %d | %d |\n", tl.Name, tl.ACalls, tl.BCalls)
		}
		w("\n")
	}

	if cmp.Extras != nil {
		renderEndpoints(w, cmp.Extras.Endpoints, t)
		renderCache(w, cmp.Extras.Cache, t)
		renderSysPrompt(w, cmp.Extras.SysPrompt, t)
		renderDeliverable(w, cmp.Extras.Deliverable, t)
		renderSources(w, cmp.Extras.Sources, t)
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
func renderSources(w func(string, ...any), sources []string, t i18n.CompareText) {
	if len(sources) == 0 {
		return
	}
	w("%s", t.SourcesTitle)
	w("%s", t.SourcesIntro)
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
func renderDurationAndFinalContext(w func(string, ...any), ex *ComparisonExtras, t i18n.CompareText) {
	d := ex.Duration
	w("%s", t.WallClockLine(fmtutil.FmtSeconds(d.AWall, 1), fmtutil.FmtSeconds(d.BWall, 1)))
	w("%s", t.TerminationLine(emptyDash(d.ATermination, t), emptyDash(d.BTermination, t)))

	fc := ex.FinalContext
	// Either side being empty (Seq == 0, ComputeComparisonExtras' zero value
	// for a Journey whose Metrics.ContextCurve came back empty) skips the
	// whole table — a real Journey always has at least one Step so this
	// shouldn't fire in practice, but `||` (not `&&`) is what actually
	// guarantees no empty-side row ever renders if it ever does.
	if fc.A.Seq == 0 || fc.B.Seq == 0 {
		return
	}
	w("%s", t.FinalContextTitle)
	w("%s", t.FinalContextHeader(fc.A.Seq, fc.B.Seq))
	rl := t.FinalContextRowLabels
	w("| %s | %s | %s |\n", rl[0], fmtTokens(fc.A.SystemTokens), fmtTokens(fc.B.SystemTokens))
	w("| %s | %s | %s |\n", rl[1], fmtTokens(fc.A.UserTokens), fmtTokens(fc.B.UserTokens))
	w("| %s | %s | %s |\n", rl[2], fmtTokens(fc.A.AssistantTokens), fmtTokens(fc.B.AssistantTokens))
	w("| %s | %s | %s |\n\n", rl[3], fmtTokens(fc.A.ToolTokens), fmtTokens(fc.B.ToolTokens))
}

func emptyDash(s string, t i18n.CompareText) string {
	if s == "" {
		return t.EmptyDash
	}
	return s
}

// renderEndpoints renders the model/endpoint identity check — deepseek
// report §3 generalized to not assume the two sides necessarily match.
func renderEndpoints(w func(string, ...any), ep EndpointsFact, t i18n.CompareText) {
	w("%s", t.EndpointsTitle)
	w("%s", t.EndpointSide("A", endpointList(ep.A, t)))
	w("%s", t.EndpointSide("B", endpointList(ep.B, t)))
	w("\n")
	if ep.Same {
		w("%s", t.EndpointsSame)
	} else {
		w("%s", t.EndpointsDiff)
	}
}

func endpointList(eps []string, t i18n.CompareText) string {
	if len(eps) == 0 {
		return t.NoEndpoints
	}
	return "`" + strings.Join(eps, "`, `") + "`"
}

// renderCache renders the per-step prompt-cache hit-ratio summary — deepseek
// report §5's "18%→97% vs 82%→99%" observation, computed the same way for
// whatever numbers this pair of Journeys actually has.
func renderCache(w func(string, ...any), c CacheFact, t i18n.CompareText) {
	w("%s", t.CacheTitle)
	if len(c.A.Series) == 0 && len(c.B.Series) == 0 {
		w("%s", t.CacheNoData)
		return
	}
	w("%s", t.CacheTableHeader)
	w("| A | %s | %s | %s | %s |\n", pctStr(c.A.FirstRatio), pctStr(c.A.SteadyMean), pctStr(c.A.Min), pctStr(c.A.Max))
	w("| B | %s | %s | %s | %s |\n\n", pctStr(c.B.FirstRatio), pctStr(c.B.SteadyMean), pctStr(c.B.Min), pctStr(c.B.Max))
	w("%s", t.CacheCurveSummary)
	w("A: %s\n\n", cacheCurveLine(c.A.Series, t))
	w("B: %s\n\n", cacheCurveLine(c.B.Series, t))
	w("</details>\n\n")
}

func cacheCurveLine(series []CachePoint, t i18n.CompareText) string {
	if len(series) == 0 {
		return t.CacheCurveNoData
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
func renderSysPrompt(w func(string, ...any), sp SysPromptFact, t i18n.CompareText) {
	w("%s", t.SysPromptTitle)
	w("%s", t.SysPromptTableHeader)
	w("| A | %s | %d |\n", fmtTokens(sp.A.Tokens), sp.A.Changes)
	w("| B | %s | %d |\n\n", fmtTokens(sp.B.Tokens), sp.B.Changes)
	renderExcerpt(w, t.SysPromptExcerptLabel("A"), sp.A.Excerpt, sp.A.Truncated, t)
	renderExcerpt(w, t.SysPromptExcerptLabel("B"), sp.B.Excerpt, sp.B.Truncated, t)
}

// renderDeliverable renders the final-write-shaped tool call each side
// produced, if any — the "result difference" dimension (design doc's four-
// dimension breakdown in the plan doc's review banner), not just process
// metrics.
func renderDeliverable(w func(string, ...any), d DeliverableFact, t i18n.CompareText) {
	w("%s", t.DeliverableTitle)
	renderDeliverableSide(w, "A", d.A, t)
	renderDeliverableSide(w, "B", d.B, t)
}

func renderDeliverableSide(w func(string, ...any), label string, s DeliverableStats, t i18n.CompareText) {
	if !s.Found {
		w("%s", t.DeliverableNotFound(label))
		return
	}
	w("%s", t.DeliverableFound(label, s.StepSeq, s.ToolName))
	renderExcerpt(w, t.DeliverableExcerptLabel(label), s.Excerpt, s.Truncated, t)
}

func renderExcerpt(w func(string, ...any), summary, text string, truncated bool, t i18n.CompareText) {
	if text == "" {
		return
	}
	note := ""
	if truncated {
		note = t.ExcerptTruncatedNote
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
