// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/section_workload.go (§5 Workload Distribution).
package i18n

// WorkloadText is section_workload.go's text, in one language.
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
	DailyTableClose   string
	DailyTableHeaders [4]string // date, requests, ok rate, fresh/cached/out
	ByClientTitle     string
	ByClientHeaders   [8]string
	ByEndpointTitle   string
	ByEndpointHeaders [8]string
}

func Workload(lang Lang) WorkloadText {
	if lang == ZH {
		return WorkloadText{
			Title:             "§5 负载分布",
			ByModelTitle:      "**按虚拟模型**",
			ByModelHeaders:    [6]string{"模型", "协议", "请求", "成功率", "fresh/cached/out", "dur p50/p95"},
			ByWorkloadTitle:   "**按工作负载类**",
			ByWorkloadHeaders: [6]string{"类", "请求", "fresh", "缓存效率⭐", "tool_call_rate", "dur p50/p95"},
			HourlyTitle:       "**每小时活跃度**",
			HourlyReqChart:    func() (string, string) { return "请求量 / 小时", "请求" },
			HourlyTokChart:    "输入Token / 小时",
			DailyTitle:        "**按日期活跃度**",
			DailyReqChart:     func() (string, string) { return "请求量 / 天", "请求" },
			DailyTokChart:     "输入Token / 天",
			DailyTableOpen:    "<details><summary>+ 逐日活跃度明细表（共 %d 天）</summary>\n\n",
			DailyTableClose:   "\n</details>\n\n",
			DailyTableHeaders: [4]string{"日期", "请求", "成功率", "fresh/cached/out"},
			ByClientTitle:     "**按客户端** ⭐",
			ByClientHeaders:   [8]string{"client_key", "请求", "成功率", "fresh/cached/out(reasoning)", "缓存效率", "dur p50/p95", "In(p50/p95)", "Out(p50/p95)"},
			ByEndpointTitle:   "**按端点** ⭐（跨日合并）",
			ByEndpointHeaders: [8]string{"端点", "请求", "成功率", "fresh/cached/out(reasoning)", "缓存效率", "dur p50/p95", "In(p50/p95)", "Out(p50/p95)"},
		}
	}
	return WorkloadText{
		Title:             "§5 Workload Distribution",
		ByModelTitle:      "**By Virtual Model**",
		ByModelHeaders:    [6]string{"Model", "Protocol", "Requests", "Success Rate", "fresh/cached/out", "dur p50/p95"},
		ByWorkloadTitle:   "**By Workload Class**",
		ByWorkloadHeaders: [6]string{"Class", "Requests", "fresh", "Cache Efficiency⭐", "tool_call_rate", "dur p50/p95"},
		HourlyTitle:       "**Hourly Activity**",
		HourlyReqChart:    func() (string, string) { return "Requests / hour", "Requests" },
		HourlyTokChart:    "Input Tokens / hour",
		DailyTitle:        "**Daily Activity**",
		DailyReqChart:     func() (string, string) { return "Requests / day", "Requests" },
		DailyTokChart:     "Input Tokens / day",
		DailyTableOpen:    "<details><summary>+ Daily Activity Table (%d days)</summary>\n\n",
		DailyTableClose:   "\n</details>\n\n",
		DailyTableHeaders: [4]string{"Date", "Requests", "Success Rate", "fresh/cached/out"},
		ByClientTitle:     "**By Client** ⭐",
		ByClientHeaders:   [8]string{"client_key", "Requests", "Success Rate", "fresh/cached/out(reasoning)", "Cache Eff.", "dur p50/p95", "In(p50/p95)", "Out(p50/p95)"},
		ByEndpointTitle:   "**By Endpoint** ⭐ (merged across dates)",
		ByEndpointHeaders: [8]string{"Endpoint", "Requests", "Success Rate", "fresh/cached/out(reasoning)", "Cache Eff.", "dur p50/p95", "In(p50/p95)", "Out(p50/p95)"},
	}
}
