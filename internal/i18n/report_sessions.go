// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/section_sessions.go (§6 Sessions & Tasks).
package i18n

// SessionsText is section_sessions.go's text, in one language.
type SessionsText struct {
	Title               string
	NoInteractive       string
	TableHeaders        [6]string // session, title, turns, tasks, fresh/cached/out, outcome
	OutcomeOKErrors     func(errs int) string
	OutcomeFallback     func(n int) string
	CompactionChainNote func(child, parent string) string
}

func Sessions(lang Lang) SessionsText {
	if lang == ZH {
		return SessionsText{
			Title:           "§6 会话与任务",
			NoInteractive:   "（无 interactive 会话）\n\n",
			TableHeaders:    [6]string{"会话", "标题", "轮", "任务", "fresh/cached/out", "结果"},
			OutcomeOKErrors: func(errs int) string { return "ok (" + itoa64(int64(errs)) + " error)" },
			OutcomeFallback: func(n int) string { return " · " + itoa64(int64(n)) + " fallback" },
			CompactionChainNote: func(child, parent string) string {
				return "> " + child + " ← " + parent + "（单次 compaction）\n\n"
			},
		}
	}
	return SessionsText{
		Title:           "§6 Sessions & Tasks",
		NoInteractive:   "(no interactive sessions)\n\n",
		TableHeaders:    [6]string{"Session", "Title", "Turns", "Tasks", "fresh/cached/out", "Outcome"},
		OutcomeOKErrors: func(errs int) string { return "ok (" + itoa64(int64(errs)) + " error)" },
		OutcomeFallback: func(n int) string { return " · " + itoa64(int64(n)) + " fallback" },
		CompactionChainNote: func(child, parent string) string {
			return "> " + child + " ← " + parent + " (single compaction)\n\n"
		},
	}
}
