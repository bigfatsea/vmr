// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/requests.go (vmr-requests.md and its per-group
// siblings).
package i18n

import "strconv"

// RequestsText is requests.go's text, in one language.
type RequestsText struct {
	ChatUserHeader         func(clientKey string, sessions, tasks, turns int) string
	CronHeader             func(class string, n int) string
	IndexTitle             string
	GroupSummary           func(requests int, successPct, fresh, cached, out, cacheEffPct string) string
	GroupDetailLink        func(file string) string
	ChatUserLegend         string
	ScheduledSummary       func(successPct, fresh, cached, out string) string
	ScheduledTableHeader   string
	FailedIndexTitle       string
	FailedIndexIntro       func(n int) string
	FailedTableHeader      string
	AllRequestsTitle       string
	AllRequestsTableHeader string
	SessionCardHeader      func(label, ts string, tasks, requests int, classNote string) string
	// JourneyLinkLine is the "session row → journey" edge (P6.2c) — path
	// is relative to the vmr-requests-<tag>.md this line is rendered into.
	JourneyLinkLine func(path string) string
	Unrouted        string
	TaskHeader      func(label, ts string, turns int) string
	TurnTableHeader string
}

func Requests(lang Lang) RequestsText {
	if lang == ZH {
		return RequestsText{
			ChatUserHeader: func(ck string, sessions, tasks, turns int) string {
				return "Chat User: " + ck + " · " + strconv.Itoa(sessions) + " 会话 " + strconv.Itoa(tasks) + " 任务 " + strconv.Itoa(turns) + " 轮"
			},
			CronHeader: func(cls string, n int) string {
				return "定时任务 · " + cls + " 单发会话 × " + strconv.Itoa(n)
			},
			IndexTitle: "VMR 请求详单",
			GroupSummary: func(requests int, successPct, fresh, cached, out, cacheEffPct string) string {
				return "> 请求 " + strconv.Itoa(requests) + " · 成功率 " + successPct + " · fresh " + fresh + " / cached " + cached + " / out " + out + " · 缓存效率 " + cacheEffPct + "\n"
			},
			GroupDetailLink: func(file string) string { return "> 详情见 [" + file + "](" + file + ")\n\n" },
			ChatUserLegend:  "层级: Session -> Task -> Turn（时间均为本机系统默认时区）。每轮表列: 轮 | 时间 | msgs | finish | dur | ttft | fresh/cached/out | cache-eff⭐ | 文件\n\n",
			ScheduledSummary: func(successPct, fresh, cached, out string) string {
				return "> 成功率 " + successPct + " · fresh " + fresh + " / cached " + cached + " / out " + out + "\n\n"
			},
			ScheduledTableHeader: "| 时间 | finish | dur | fresh/cached/out | cache-eff⭐ | 文件 |\n|---|---|---|---|---|---|\n",
			FailedIndexTitle:     "VMR 失败请求索引",
			FailedIndexIntro: func(n int) string {
				return "专供错误分析：outcome 为 error / canceled，以及 outcome=ok 但 truncated（流中途断了）的全部请求，按时间排序，每条直链到对应的 details/*.md + *.json。不影响其他报表——这些记录在 vmr-requests.md 及其分组 sibling 文件里照常出现，本文件只是额外的索引。共 " + strconv.Itoa(n) + " 条。\n\n"
			},
			FailedTableHeader:      "| 时间 | 会话/任务 | VM/API | outcome⭐ | dur | 文件 |\n|---|---|---|---|---|---|\n",
			AllRequestsTitle:       "全部请求（时间序）",
			AllRequestsTableHeader: "| 时间 | 会话/任务 | VM/API | outcome⭐ | dur | fresh/cached/out | cache-eff⭐ | 文件 |\n|---|---|---|---|---|---|---|---|\n",
			SessionCardHeader: func(label, ts string, tasks, requests int, classNote string) string {
				return "## " + label + " · " + ts + " · " + strconv.Itoa(tasks) + " 任务 " + strconv.Itoa(requests) + " 轮" + classNote + "\n\n"
			},
			JourneyLinkLine: func(path string) string { return "→ 任务叙事见 [" + path + "](" + path + ")\n\n" },
			Unrouted:        "（未分组）",
			TaskHeader: func(label, ts string, turns int) string {
				return "### " + label + " · " + ts + " · " + strconv.Itoa(turns) + " 轮\n\n"
			},
			TurnTableHeader: "| 轮 | 时间 | msgs | finish | dur | ttft | fresh/cached/out | cache-eff⭐ | 文件 |\n|---|---|---|---|---|---|---|---|---|\n",
		}
	}
	return RequestsText{
		ChatUserHeader: func(ck string, sessions, tasks, turns int) string {
			return "Chat User: " + ck + " · " + strconv.Itoa(sessions) + " sessions " + strconv.Itoa(tasks) + " tasks " + strconv.Itoa(turns) + " turns"
		},
		CronHeader: func(cls string, n int) string {
			return "Scheduled · " + cls + " single-shot sessions × " + strconv.Itoa(n)
		},
		IndexTitle: "VMR Request Index",
		GroupSummary: func(requests int, successPct, fresh, cached, out, cacheEffPct string) string {
			return "> Requests " + strconv.Itoa(requests) + " · Success Rate " + successPct + " · fresh " + fresh + " / cached " + cached + " / out " + out + " · Cache Efficiency " + cacheEffPct + "\n"
		},
		GroupDetailLink: func(file string) string { return "> Details in [" + file + "](" + file + ")\n\n" },
		ChatUserLegend:  "Hierarchy: Session -> Task -> Turn (all times in the local machine's system default timezone). Per-turn columns: Turn | Time | msgs | finish | dur | ttft | fresh/cached/out | cache-eff⭐ | File\n\n",
		ScheduledSummary: func(successPct, fresh, cached, out string) string {
			return "> Success Rate " + successPct + " · fresh " + fresh + " / cached " + cached + " / out " + out + "\n\n"
		},
		ScheduledTableHeader: "| Time | finish | dur | fresh/cached/out | cache-eff⭐ | File |\n|---|---|---|---|---|---|\n",
		FailedIndexTitle:     "VMR Failed Request Index",
		FailedIndexIntro: func(n int) string {
			return "Dedicated for error analysis: every request with outcome error / canceled, plus outcome=ok but truncated (stream broke mid-way), sorted by time, each linking straight to its details/*.md + *.json. Purely additive — these records still appear as usual in vmr-requests.md and its per-group sibling files; this file is just an extra index. " + strconv.Itoa(n) + " total.\n\n"
		},
		FailedTableHeader:      "| Time | Session/Task | VM/API | outcome⭐ | dur | File |\n|---|---|---|---|---|---|\n",
		AllRequestsTitle:       "All Requests (chronological)",
		AllRequestsTableHeader: "| Time | Session/Task | VM/API | outcome⭐ | dur | fresh/cached/out | cache-eff⭐ | File |\n|---|---|---|---|---|---|---|---|\n",
		SessionCardHeader: func(label, ts string, tasks, requests int, classNote string) string {
			return "## " + label + " · " + ts + " · " + strconv.Itoa(tasks) + " tasks " + strconv.Itoa(requests) + " turns" + classNote + "\n\n"
		},
		JourneyLinkLine: func(path string) string { return "→ Task narrative in [" + path + "](" + path + ")\n\n" },
		Unrouted:        "(ungrouped)",
		TaskHeader: func(label, ts string, turns int) string {
			return "### " + label + " · " + ts + " · " + strconv.Itoa(turns) + " turns\n\n"
		},
		TurnTableHeader: "| Turn | Time | msgs | finish | dur | ttft | fresh/cached/out | cache-eff⭐ | File |\n|---|---|---|---|---|---|---|---|---|\n",
	}
}
