// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/requests.go (vmr-requests.md and its per-group
// siblings).
package i18n

import "strconv"

// RequestsText is requests.go's text, in one language.
type RequestsText struct {
	FailedIndexTitle     string
	FailedIndexIntro     func(n int) string
	FailedClusterSummary func(totalRows, clusters, maxClusterRows int, maxClusterSpan, maxClusterClasses string) string
	FailedTableHeader    string
}

func Requests(lang Lang) RequestsText {
	if lang == ZH {
		return RequestsText{
			FailedIndexTitle: "VMR 失败请求索引",
			FailedIndexIntro: func(n int) string {
				return "专供错误分析：outcome 为 error / canceled，以及 outcome=ok 但 truncated（流中途断了）的全部请求，按时间排序，每条直链到对应的 details/*.md。不影响其他报表——这些记录在各分组明细文件（vmr-requests-<tag>.md / -unresolved.md）里照常出现，本文件只是额外的索引。共 " + strconv.Itoa(n) + " 条。\n\n"
			},
			FailedClusterSummary: func(totalRows, clusters, maxClusterRows int, maxClusterSpan, maxClusterClasses string) string {
				return "> **故障聚类**：" + strconv.Itoa(totalRows) + " 条失败聚成 " + strconv.Itoa(clusters) + " 簇（时间间隔 ≤ 2 分钟），最大一簇 " + strconv.Itoa(maxClusterRows) + " 条（" + maxClusterSpan + " · " + maxClusterClasses + "）。\n\n"
			},
			FailedTableHeader: "| 时间 | 会话/任务 | VM/API | outcome⭐ | dur | 文件 |\n|---|---|---|---|---|---|\n",
		}
	}
	return RequestsText{
		FailedIndexTitle: "VMR Failed Request Index",
		FailedIndexIntro: func(n int) string {
			return "Dedicated for error analysis: every request with outcome error / canceled, plus outcome=ok but truncated (stream broke mid-way), sorted by time, each linking straight to its details/*.md. Purely additive — these records still appear as usual in the per-group detail files (vmr-requests-<tag>.md / -unresolved.md); this file is just an extra index. " + strconv.Itoa(n) + " total.\n\n"
		},
		FailedClusterSummary: func(totalRows, clusters, maxClusterRows int, maxClusterSpan, maxClusterClasses string) string {
			return "> **Incident Clusters**: " + strconv.Itoa(totalRows) + " failures grouped into " + strconv.Itoa(clusters) + " clusters (inter-failure gap ≤ 2m); largest cluster carries " + strconv.Itoa(maxClusterRows) + " requests (" + maxClusterSpan + " · " + maxClusterClasses + ").\n\n"
		},
		FailedTableHeader: "| Time | Session/Task | VM/API | outcome⭐ | dur | File |\n|---|---|---|---|---|---|\n",
	}
}
