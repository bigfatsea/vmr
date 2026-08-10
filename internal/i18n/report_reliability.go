// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/section_reliability.go (§3 Reliability).
package i18n

// ReliabilityText is section_reliability.go's text, in one language.
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
}

func Reliability(lang Lang) ReliabilityText {
	if lang == ZH {
		return ReliabilityText{
			Title:                  "§3 可靠性",
			OutcomeTitle:           "**结果分布**",
			OutcomeHeaders:         [5]string{"ok", "error", "canceled", "truncated", "fallback(恢复/失败)⭐"},
			EndpointHealthTitle:    "**端点健康**（跨日合并）",
			EndpointHeaders:        [6]string{"端点", "尝试", "成功", "可用度", "错误率⭐", "首要错误"},
			ErrorByEndpointTitle:   "**错误类别 × 端点**（仅非零）",
			ErrorByEndpointHeaders: [3]string{"端点", "类别", "计数"},
			QuirkByEndpointTitle:   "**Quirk 修复 × 端点**（仅非零，占该端点成功尝试的比例；详见每条请求的详情页）",
			QuirkByEndpointHeaders: [3]string{"端点", "标记", "计数"},
			ErrorTimelineTitle:     "**错误时间线**（错误数 / 小时）",
			ErrorTimelineChart:     func() (string, string) { return "错误数 / 小时", "错误数" },
			PeakHourNote: func(hour int, count int64) string {
				return "> 错误集中在 " + pad2(hour) + ":00（共 " + itoa64(count) + " 条）。\n\n"
			},
		}
	}
	return ReliabilityText{
		Title:                  "§3 Reliability",
		OutcomeTitle:           "**Outcome Distribution**",
		OutcomeHeaders:         [5]string{"ok", "error", "canceled", "truncated", "fallback(recovered/failed)⭐"},
		EndpointHealthTitle:    "**Endpoint Health** (merged across dates)",
		EndpointHeaders:        [6]string{"Endpoint", "Attempts", "OK", "Availability", "Error Rate⭐", "Top Error"},
		ErrorByEndpointTitle:   "**Error Class × Endpoint** (non-zero only)",
		ErrorByEndpointHeaders: [3]string{"Endpoint", "Class", "Count"},
		QuirkByEndpointTitle:   "**Quirk Fix × Endpoint** (non-zero only, % of this endpoint's successful attempts; see each request's detail page for the full narration)",
		QuirkByEndpointHeaders: [3]string{"Endpoint", "Marker", "Count"},
		ErrorTimelineTitle:     "**Error Timeline** (errors / hour)",
		ErrorTimelineChart:     func() (string, string) { return "Errors / hour", "Errors" },
		PeakHourNote: func(hour int, count int64) string {
			return "> Errors peak at " + pad2(hour) + ":00 (" + itoa64(count) + " total).\n\n"
		},
	}
}
