// Ver 2026-09-01, by Sonnet 5

// RenderBehaviorIndicators renders a Journey's 17 behavior profile indicators
// and context composition curve sparkline into human-readable Markdown (问题 9).
package story

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

func renderBehaviorIndicators(w func(string, ...any), m Metrics, isNonAnthropic bool, lang i18n.Lang) {
	t := i18n.Indicators(lang)
	w("%s", t.Title)
	w("%s", t.TableHeader)

	row := func(label, val string) {
		w("| %s | %s |\n", label, val)
	}

	row(t.NetWorkingTime, fmtutil.FmtSeconds(time.Duration(m.NetWorkingMS)*time.Millisecond, 1))
	row(t.ModelTime, fmtutil.FmtSeconds(time.Duration(m.ModelMS)*time.Millisecond, 1))
	row(t.AgentExecTime, fmtutil.FmtSeconds(time.Duration(m.AgentExecMS)*time.Millisecond, 1))
	row(t.HumanIdleTime, fmtutil.FmtSeconds(time.Duration(m.HumanIdleMS)*time.Millisecond, 1))
	if m.AgentExecMS > 0 {
		row(t.ModelToolRatio, fmt.Sprintf("%.2f×", m.ModelToToolRatio))
	} else {
		row(t.ModelToolRatio, "—")
	}
	row(t.ToolCallCount, strconv.Itoa(m.ToolCallCount))
	row(t.DupActionRate, pctStr(m.DuplicateActionRate))
	row(t.OutputRepeatRate, pctStr(m.OutputRepetitionRate))
	errRecVal := strconv.Itoa(m.ErrorRecoveryCount)
	if isNonAnthropic && m.ErrorRecoveryCount == 0 {
		errRecVal = "n/a"
	}
	row(t.ErrorRecovery, errRecVal)
	row(t.PlanExecRatio, pctStr(m.PlanExecRatio))
	row(t.ContextUtil, pctStr(m.ContextUtilization))
	row(t.CompactionCount, strconv.Itoa(m.CompactionCount))
	row(t.CompactionLoss, fmtutil.FmtTokens(m.CompactionLossTokens))
	row(t.ModelSwitches, strconv.Itoa(len(m.ModelSwitches)))
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
