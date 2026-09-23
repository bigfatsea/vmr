// Ver 2026-09-22 02:10, by Sonnet 5

// Pairs with internal/report/requests_failed.go (requests/failed.md — the one
// human-readable request document after the per-group index family was retired).
package i18n

import "fmt"

// RequestsText is requests.go's text, in one language.
type RequestsText struct {
	FailedIndexTitle     string
	FailedIndexIntro     func(n int) string
	FailedClusterSummary func(totalRows, clusters, maxClusterRows int, maxClusterSpan, maxClusterClasses string) string
	FailedTableHeader    string
}

// requestsRow holds report_requests.go's literal templates, one row per
// Lang (Table's own doc comment).
type requestsRow struct {
	failedIndexTitle        string
	failedIndexIntroFmt     string
	failedClusterSummaryFmt string
	failedTableHeader       string
}

var requestsRows = Table[requestsRow]{
	EN: {
		failedIndexTitle:        "VMR Failed Request Index",
		failedIndexIntroFmt:     "Dedicated for error analysis: every request with outcome error / canceled, plus outcome=ok but truncated (stream broke mid-way), sorted by time, each linking straight to its `details/*.md`. Full machine-readable per-request detail is in `requests/index.json` (browse it interactively with `request-browser.html`) — failed requests appear there too. %d total.\n\n",
		failedClusterSummaryFmt: "> **Incident Clusters**: %d failures grouped into %d clusters (inter-failure gap ≤ 2m); largest cluster carries %d requests (%s · %s).\n\n",
		failedTableHeader:       "| Time | Session/Task | VM/API | outcome⭐ | dur | File |\n|---|---|---|---|---|---|\n",
	},
	ZH: {
		failedIndexTitle:        "VMR 失败请求索引",
		failedIndexIntroFmt:     "专供错误分析：outcome 为 error / canceled，以及 outcome=ok 但 truncated（流中途断了）的全部请求，按时间排序，每条直链到对应的 `details/*.md`。逐条请求的完整机读明细在 `requests/index.json`（交互浏览用 `request-browser.html`）——失败请求在那里也照常出现。共 %d 条。\n\n",
		failedClusterSummaryFmt: "> **故障聚类**：%d 条失败聚成 %d 簇（时间间隔 ≤ 2 分钟），最大一簇 %d 条（%s · %s）。\n\n",
		failedTableHeader:       "| 时间 | 会话/任务 | VM/API | outcome⭐ | dur | 文件 |\n|---|---|---|---|---|---|\n",
	},
}

func Requests(lang Lang) RequestsText {
	r := requestsRows.Row(lang)
	return RequestsText{
		FailedIndexTitle: r.failedIndexTitle,
		FailedIndexIntro: func(n int) string { return fmt.Sprintf(r.failedIndexIntroFmt, n) },
		FailedClusterSummary: func(totalRows, clusters, maxClusterRows int, maxClusterSpan, maxClusterClasses string) string {
			return fmt.Sprintf(r.failedClusterSummaryFmt, totalRows, clusters, maxClusterRows, maxClusterSpan, maxClusterClasses)
		},
		FailedTableHeader: r.failedTableHeader,
	}
}
