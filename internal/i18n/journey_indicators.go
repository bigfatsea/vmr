// Ver 2026-09-01, by Sonnet 5

// Chrome for one Journey's behavior-indicators section — consumed by
// internal/journey/viewmodel_build.go's buildVMIndicators. The per-metric
// row labels come from MetricLabel, not from here.
package i18n

import "fmt"

// IndicatorsText is the behavior-indicators section's chrome, in one language.
type IndicatorsText struct {
	Title            string
	TableHeader      string // header+separator row
	SparklineTitle   string
	SparklineCaption func(start, end int64) string
}

func Indicators(lang Lang) IndicatorsText {
	if lang == ZH {
		return IndicatorsText{
			Title:          "## 行为指标\n\n",
			TableHeader:    "| 指标 | 值 |\n|---|---|\n",
			SparklineTitle: "**上下文构成演化趋势**",
			SparklineCaption: func(start, end int64) string {
				return fmt.Sprintf("（起始 %d tok → 结束 %d tok）\n\n", start, end)
			},
		}
	}
	return IndicatorsText{
		Title:          "## Behavior Indicators\n\n",
		TableHeader:    "| Metric | Value |\n|---|---|\n",
		SparklineTitle: "**Context Token Trajectory**",
		SparklineCaption: func(start, end int64) string {
			return fmt.Sprintf("(start %d tok → end %d tok)\n\n", start, end)
		},
	}
}
