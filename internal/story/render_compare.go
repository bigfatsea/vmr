// Ver 2026-07-30 12:00, by Sonnet 5

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

	if len(cmp.Tools) > 0 {
		w("## 工具调用对比\n\n")
		w("| 工具 | A 次数 | B 次数 |\n|---|---|---|\n")
		for _, t := range cmp.Tools {
			w("| %s | %d | %d |\n", t.Name, t.ACalls, t.BCalls)
		}
		w("\n")
	}

	return b.String()
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
