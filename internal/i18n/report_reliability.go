// Ver 2026-09-22 02:10, by Sonnet 5

// Pairs with internal/report/viewmodel_reliability.go (Reliability).
package i18n

import "fmt"

// ReliabilityText is viewmodel_reliability.go's text, in one language.
type ReliabilityText struct {
	Title                  string
	OutcomeTitle           string
	OutcomeHeaders         [5]string // ok, error, canceled, truncated, fallback(recovered/failed)
	EndpointHealthTitle    string
	EndpointHeaders        [6]string // endpoint, attempts, ok, availability, error rate, top error
	ErrorByEndpointTitle   string
	ErrorByEndpointHeaders [3]string // endpoint, class, count
	QuirkByEndpointTitle   string
	QuirkByEndpointHeaders [3]string // endpoint, marker, count
	ErrorTimelineTitle     string
	ErrorTimelineChart     func() (title, axis string)
	PeakHourNote           func(hour int, count int64) string
	LowSampleOpen          func(n int) string
}

// reliabilityRow holds report_reliability.go's literal templates, one row
// per Lang (Table's own doc comment).
type reliabilityRow struct {
	title                  string
	outcomeTitle           string
	outcomeHeaders         [5]string
	endpointHealthTitle    string
	endpointHeaders        [6]string
	errorByEndpointTitle   string
	errorByEndpointHeaders [3]string
	quirkByEndpointTitle   string
	quirkByEndpointHeaders [3]string
	errorTimelineTitle     string
	errorTimelineChartName string
	errorTimelineAxisName  string
	peakHourNoteFmt        string
	lowSampleOpenFmt       string
}

var reliabilityRows = Table[reliabilityRow]{
	EN: {
		title:                  "§3 Reliability",
		outcomeTitle:           "**Outcome Distribution**",
		outcomeHeaders:         [5]string{"ok", "error", "canceled", "truncated", "fallback(recovered/failed)⭐"},
		endpointHealthTitle:    "**Endpoint Health** (merged across dates)",
		endpointHeaders:        [6]string{"Endpoint", "Attempts", "OK", "Availability", "Error Rate⭐", "Top Error"},
		errorByEndpointTitle:   "**Error Class × Endpoint** (non-zero only)",
		errorByEndpointHeaders: [3]string{"Endpoint", "Class", "Count"},
		quirkByEndpointTitle:   "**Quirk Fix × Endpoint** (non-zero only, % of this endpoint's successful attempts; see each request's detail page for the full narration)",
		quirkByEndpointHeaders: [3]string{"Endpoint", "Marker", "Count"},
		errorTimelineTitle:     "**Error Timeline** (errors / hour)",
		errorTimelineChartName: "Errors / hour",
		errorTimelineAxisName:  "Errors",
		peakHourNoteFmt:        "> Errors peak at %s:00 (%s total).\n\n",
		lowSampleOpenFmt:       "<details><summary>+ %s more low-sample endpoints (attempts < 20)</summary>\n\n",
	},
	ZH: {
		title:                  "§3 可靠性",
		outcomeTitle:           "**结果分布**",
		outcomeHeaders:         [5]string{"ok", "error", "canceled", "truncated", "fallback(恢复/失败)⭐"},
		endpointHealthTitle:    "**端点健康**（跨日合并）",
		endpointHeaders:        [6]string{"端点", "尝试", "成功", "可用度", "错误率⭐", "首要错误"},
		errorByEndpointTitle:   "**错误类别 × 端点**（仅非零）",
		errorByEndpointHeaders: [3]string{"端点", "类别", "计数"},
		quirkByEndpointTitle:   "**Quirk 修复 × 端点**（仅非零，占该端点成功尝试的比例；详见每条请求的详情页）",
		quirkByEndpointHeaders: [3]string{"端点", "标记", "计数"},
		errorTimelineTitle:     "**错误时间线**（错误数 / 小时）",
		errorTimelineChartName: "错误数 / 小时",
		errorTimelineAxisName:  "错误数",
		peakHourNoteFmt:        "> 错误集中在 %s:00（共 %s 条）。\n\n",
		lowSampleOpenFmt:       "<details><summary>+ 另有 %s 个低样本端点（尝试 < 20）</summary>\n\n",
	},
}

func Reliability(lang Lang) ReliabilityText {
	r := reliabilityRows.Row(lang)
	return ReliabilityText{
		Title:                  r.title,
		OutcomeTitle:           r.outcomeTitle,
		OutcomeHeaders:         r.outcomeHeaders,
		EndpointHealthTitle:    r.endpointHealthTitle,
		EndpointHeaders:        r.endpointHeaders,
		ErrorByEndpointTitle:   r.errorByEndpointTitle,
		ErrorByEndpointHeaders: r.errorByEndpointHeaders,
		QuirkByEndpointTitle:   r.quirkByEndpointTitle,
		QuirkByEndpointHeaders: r.quirkByEndpointHeaders,
		ErrorTimelineTitle:     r.errorTimelineTitle,
		ErrorTimelineChart: func() (string, string) {
			return r.errorTimelineChartName, r.errorTimelineAxisName
		},
		PeakHourNote: func(hour int, count int64) string {
			return fmt.Sprintf(r.peakHourNoteFmt, pad2(hour), itoa64(count))
		},
		LowSampleOpen: func(n int) string {
			return fmt.Sprintf(r.lowSampleOpenFmt, itoa64(int64(n)))
		},
	}
}
