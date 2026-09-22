// Ver 2026-09-22 02:10, by Sonnet 5

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

// indicatorsRow holds journey_indicators.go's literal templates, one row
// per Lang (Table's own doc comment).
type indicatorsRow struct {
	title               string
	tableHeader         string
	sparklineTitle      string
	sparklineCaptionFmt string
}

var indicatorsRows = Table[indicatorsRow]{
	EN: {
		title:               "## Behavior Indicators\n\n",
		tableHeader:         "| Metric | Value |\n|---|---|\n",
		sparklineTitle:      "**Context Token Trajectory**",
		sparklineCaptionFmt: "(start %d tok → end %d tok)\n\n",
	},
	ZH: {
		title:               "## 行为指标\n\n",
		tableHeader:         "| 指标 | 值 |\n|---|---|\n",
		sparklineTitle:      "**上下文构成演化趋势**",
		sparklineCaptionFmt: "（起始 %d tok → 结束 %d tok）\n\n",
	},
}

func Indicators(lang Lang) IndicatorsText {
	r := indicatorsRows.Row(lang)
	return IndicatorsText{
		Title:          r.title,
		TableHeader:    r.tableHeader,
		SparklineTitle: r.sparklineTitle,
		SparklineCaption: func(start, end int64) string {
			return fmt.Sprintf(r.sparklineCaptionFmt, start, end)
		},
	}
}
