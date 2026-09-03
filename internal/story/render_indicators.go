// Ver 2026-09-01, by Sonnet 5

// RenderBehaviorIndicators renders a Journey's behavior profile indicators
// and context composition curve sparkline into human-readable Markdown (问题 9).
package story

import (
	"strings"

	"vmr/internal/i18n"
)

// renderBehaviorIndicators renders a Journey's behavior profile into
// human-readable Markdown (问题 9). The row set comes from journeyMetrics
// (compare_metrics.go) — the same slice htmlMetrics iterates — so the two
// formats can never drift apart about which metrics a journey view shows;
// this function only decides how one Markdown row is rendered (§2.81).
func renderBehaviorIndicators(w func(string, ...any), m Metrics, isNonAnthropic bool, lang i18n.Lang) {
	t := i18n.Indicators(lang)
	w("%s", t.Title)
	w("%s", t.TableHeader)

	for _, jm := range journeyMetrics {
		v := jm.Value(m)
		w("| %s | %s |\n", i18n.MetricLabel(lang, string(jm.Code)), jm.Format(m, v, isNonAnthropic))
	}
	w("\n")

	renderContextAsciiSparkline(w, m.ContextCurve, t)
}

// asciiSparklineChars provides 8 vertical bar levels for text sparklines.
var asciiSparklineChars = []rune{' ', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

func renderContextAsciiSparkline(w func(string, ...any), curve []ContextPoint, t i18n.IndicatorsText) {
	if len(curve) < 2 {
		return
	}
	totals := make([]int64, len(curve))
	var maxT int64
	for i, p := range curve {
		totals[i] = p.SystemTokens + p.UserTokens + p.AssistantTokens + p.ToolTokens
		if totals[i] > maxT {
			maxT = totals[i]
		}
	}
	if maxT == 0 {
		return
	}
	var b strings.Builder
	for _, v := range totals {
		idx := int(float64(v) / float64(maxT) * float64(len(asciiSparklineChars)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(asciiSparklineChars) {
			idx = len(asciiSparklineChars) - 1
		}
		b.WriteRune(asciiSparklineChars[idx])
	}
	w("%s: `%s` %s", t.SparklineTitle, b.String(), t.SparklineCaption(totals[0], totals[len(totals)-1]))
}
