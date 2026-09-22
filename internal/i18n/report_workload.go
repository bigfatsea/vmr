// Ver 2026-09-22 02:10, by Sonnet 5

// Pairs with internal/report/viewmodel_workload.go (§5 Workload Distribution).
package i18n

// WorkloadText is viewmodel_workload.go's text, in one language.
type WorkloadText struct {
	Title             string
	ByModelTitle      string
	ByModelHeaders    [6]string // model, protocol, requests, success rate, fresh/cached/out, dur p50/p95
	ByWorkloadTitle   string
	ByWorkloadHeaders [6]string // class, requests, fresh, cache efficiency, tool_call_rate, dur p50/p95
	HourlyTitle       string
	HourlyReqChart    func() (title, axis string)
	HourlyTokChart    string
	DailyTitle        string
	DailyReqChart     func() (title, axis string)
	DailyTokChart     string
	DailyTableOpen    string
	DailyTableHeaders [4]string // date, requests, ok rate, fresh/cached/out
	ByClientTitle     string
	ByClientHeaders   [8]string
	ByEndpointTitle   string
	ByEndpointHeaders [8]string
}

// workloadRow holds report_workload.go's literal templates, one row per
// Lang (Table's own doc comment).
type workloadRow struct {
	title             string
	byModelTitle      string
	byModelHeaders    [6]string
	byWorkloadTitle   string
	byWorkloadHeaders [6]string
	hourlyTitle       string
	hourlyReqChart    string
	hourlyReqAxis     string
	hourlyTokChart    string
	dailyTitle        string
	dailyReqChart     string
	dailyReqAxis      string
	dailyTokChart     string
	dailyTableOpen    string
	dailyTableHeaders [4]string
	byClientTitle     string
	byClientHeaders   [8]string
	byEndpointTitle   string
	byEndpointHeaders [8]string
}

var workloadRows = Table[workloadRow]{
	EN: {
		title:             "§5 Workload Distribution",
		byModelTitle:      "**By Virtual Model**",
		byModelHeaders:    [6]string{"Model", "Protocol", "Requests", "Success Rate", "fresh/cached/out", "dur p50/p95"},
		byWorkloadTitle:   "**By Workload Class**",
		byWorkloadHeaders: [6]string{"Class", "Requests", "fresh", "Cache Efficiency⭐", "tool_call_rate", "dur p50/p95"},
		hourlyTitle:       "**Hourly Activity**",
		hourlyReqChart:    "Requests / hour",
		hourlyReqAxis:     "Requests",
		hourlyTokChart:    "Input Tokens / hour",
		dailyTitle:        "**Daily Activity**",
		dailyReqChart:     "Requests / day",
		dailyReqAxis:      "Requests",
		dailyTokChart:     "Input Tokens / day",
		dailyTableOpen:    "<details><summary>+ Daily Activity Table (%d days)</summary>\n\n",
		dailyTableHeaders: [4]string{"Date", "Requests", "Success Rate", "fresh/cached/out"},
		byClientTitle:     "**By Client** ⭐",
		byClientHeaders:   [8]string{"client_key", "Requests", "Success Rate", "fresh/cached/out(reasoning)", "Cache Eff.", "dur p50/p95", "In(p50/p95)", "Out(p50/p95)"},
		byEndpointTitle:   "**By Endpoint** ⭐ (merged across dates)",
		byEndpointHeaders: [8]string{"Endpoint", "Requests", "Success Rate", "fresh/cached/out(reasoning)", "Cache Eff.", "dur p50/p95", "In(p50/p95)", "Out(p50/p95)"},
	},
	ZH: {
		title:             "§5 负载分布",
		byModelTitle:      "**按虚拟模型**",
		byModelHeaders:    [6]string{"模型", "协议", "请求", "成功率", "fresh/cached/out", "dur p50/p95"},
		byWorkloadTitle:   "**按工作负载类**",
		byWorkloadHeaders: [6]string{"类", "请求", "fresh", "缓存效率⭐", "tool_call_rate", "dur p50/p95"},
		hourlyTitle:       "**每小时活跃度**",
		hourlyReqChart:    "请求量 / 小时",
		hourlyReqAxis:     "请求",
		hourlyTokChart:    "输入Token / 小时",
		dailyTitle:        "**按日期活跃度**",
		dailyReqChart:     "请求量 / 天",
		dailyReqAxis:      "请求",
		dailyTokChart:     "输入Token / 天",
		dailyTableOpen:    "<details><summary>+ 逐日活跃度明细表（共 %d 天）</summary>\n\n",
		dailyTableHeaders: [4]string{"日期", "请求", "成功率", "fresh/cached/out"},
		byClientTitle:     "**按客户端** ⭐",
		byClientHeaders:   [8]string{"client_key", "请求", "成功率", "fresh/cached/out(reasoning)", "缓存效率", "dur p50/p95", "In(p50/p95)", "Out(p50/p95)"},
		byEndpointTitle:   "**按端点** ⭐（跨日合并）",
		byEndpointHeaders: [8]string{"端点", "请求", "成功率", "fresh/cached/out(reasoning)", "缓存效率", "dur p50/p95", "In(p50/p95)", "Out(p50/p95)"},
	},
}

func Workload(lang Lang) WorkloadText {
	r := workloadRows.Row(lang)
	return WorkloadText{
		Title:             r.title,
		ByModelTitle:      r.byModelTitle,
		ByModelHeaders:    r.byModelHeaders,
		ByWorkloadTitle:   r.byWorkloadTitle,
		ByWorkloadHeaders: r.byWorkloadHeaders,
		HourlyTitle:       r.hourlyTitle,
		HourlyReqChart:    func() (string, string) { return r.hourlyReqChart, r.hourlyReqAxis },
		HourlyTokChart:    r.hourlyTokChart,
		DailyTitle:        r.dailyTitle,
		DailyReqChart:     func() (string, string) { return r.dailyReqChart, r.dailyReqAxis },
		DailyTokChart:     r.dailyTokChart,
		DailyTableOpen:    r.dailyTableOpen,
		DailyTableHeaders: r.dailyTableHeaders,
		ByClientTitle:     r.byClientTitle,
		ByClientHeaders:   r.byClientHeaders,
		ByEndpointTitle:   r.byEndpointTitle,
		ByEndpointHeaders: r.byEndpointHeaders,
	}
}
