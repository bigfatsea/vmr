// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/section_sessions.go (§6 Sessions & Tasks).
package i18n

// SessionsText is section_sessions.go's text, in one language.
type SessionsText struct {
	Title               string
	NoInteractive       string
	TableHeaders        [7]string // session, time range, title, turns, tasks, fresh/cached/out, outcome
	TableNote           string
	OutcomeOKErrors     func(errs int) string
	OutcomeFallback     func(n int) string
	CompactionChainNote func(child, parent string) string
	// LongTailOpen opens the <details> that folds a client's short-session
	// tail (n sessions, each at most turnCap turns); LongTailClose ends it.
	LongTailOpen  func(n, turnCap int) string
	LongTailClose string
}

func Sessions(lang Lang) SessionsText {
	if lang == ZH {
		return SessionsText{
			Title:           "§6 会话与任务",
			NoInteractive:   "（无 interactive 会话）\n\n",
			TableHeaders:    [7]string{"会话", "时间范围", "标题", "轮", "任务", "fresh/cached/out", "结果"},
			TableNote:       "> 会话标识形如 s01 (l-...)：sNN 仅为本次报告内行号别名，括号内 l-<hash8> 为稳定内容寻址 ID。\n\n",
			OutcomeOKErrors: func(errs int) string { return "ok (" + itoa64(int64(errs)) + " error)" },
			OutcomeFallback: func(n int) string { return " · " + itoa64(int64(n)) + " fallback" },
			CompactionChainNote: func(child, parent string) string {
				return "> " + child + " ← " + parent + "（单次 compaction）\n\n"
			},
			LongTailOpen: func(n, turnCap int) string {
				return "<details><summary>+ 其余 " + itoa64(int64(n)) + " 个会话（均 ≤ " + itoa64(int64(turnCap)) + " 轮）</summary>\n\n"
			},
			LongTailClose: "\n</details>\n\n",
		}
	}
	return SessionsText{
		Title:           "§6 Sessions & Tasks",
		NoInteractive:   "(no interactive sessions)\n\n",
		TableHeaders:    [7]string{"Session", "Time Range", "Title", "Turns", "Tasks", "fresh/cached/out", "Outcome"},
		TableNote:       "> Session labels like s01 (l-...): sNN is a report-local row alias; l-<hash8> is the stable content-addressed ID.\n\n",
		OutcomeOKErrors: func(errs int) string { return "ok (" + itoa64(int64(errs)) + " error)" },
		OutcomeFallback: func(n int) string { return " · " + itoa64(int64(n)) + " fallback" },
		CompactionChainNote: func(child, parent string) string {
			return "> " + child + " ← " + parent + " (single compaction)\n\n"
		},
		LongTailOpen: func(n, turnCap int) string {
			return "<details><summary>+ " + itoa64(int64(n)) + " more sessions (all ≤ " + itoa64(int64(turnCap)) + " turns)</summary>\n\n"
		},
		LongTailClose: "\n</details>\n\n",
	}
}
