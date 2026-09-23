// Ver 2026-09-22 02:10, by Sonnet 5

// Pairs with internal/report/viewmodel_sessions.go (Sessions & Tasks).
package i18n

import "fmt"

// SessionsText is viewmodel_sessions.go's text, in one language.
type SessionsText struct {
	Title               string
	NoInteractive       string
	TableHeaders        [7]string // session, time range, title, turns, tasks, fresh/cached/out, outcome
	TableNote           string
	OutcomeOKErrors     func(errs int) string
	OutcomeFallback     func(n int) string
	CompactionChainNote func(child, parent string) string
	// LongTailOpen opens the <details> that folds a client's short-session
	// tail (n sessions, each at most turnCap turns); the serializer closes it.
	LongTailOpen func(n, turnCap int) string
}

// sessionsRow holds report_sessions.go's literal templates, one row per
// Lang (Table's own doc comment).
type sessionsRow struct {
	title                  string
	noInteractive          string
	tableHeaders           [7]string
	tableNote              string
	outcomeOKErrorsFmt     string
	outcomeFallbackFmt     string
	compactionChainNoteFmt string
	longTailOpenFmt        string
}

var sessionsRows = Table[sessionsRow]{
	EN: {
		title:                  "§6 Sessions & Tasks",
		noInteractive:          "(no interactive sessions)\n\n",
		tableHeaders:           [7]string{"Session", "Time Range", "Title", "Turns", "Tasks", "fresh/cached/out", "Outcome"},
		tableNote:              "> Session labels like s01 (l-...): sNN is a report-local row alias; l-<hash8> is the stable content-addressed ID.\n\n",
		outcomeOKErrorsFmt:     "ok (%d error)",
		outcomeFallbackFmt:     " · %d fallback",
		compactionChainNoteFmt: "> %s ← %s (single compaction)\n\n",
		longTailOpenFmt:        "<details><summary>+ %d more sessions (all ≤ %d turns)</summary>\n\n",
	},
	ZH: {
		title:                  "§6 会话与任务",
		noInteractive:          "（无 interactive 会话）\n\n",
		tableHeaders:           [7]string{"会话", "时间范围", "标题", "轮", "任务", "fresh/cached/out", "结果"},
		tableNote:              "> 会话标识形如 s01 (l-...)：sNN 仅为本次报告内行号别名，括号内 l-<hash8> 为稳定内容寻址 ID。\n\n",
		outcomeOKErrorsFmt:     "ok (%d error)",
		outcomeFallbackFmt:     " · %d fallback",
		compactionChainNoteFmt: "> %s ← %s（单次 compaction）\n\n",
		longTailOpenFmt:        "<details><summary>+ 其余 %d 个会话（均 ≤ %d 轮）</summary>\n\n",
	},
}

func Sessions(lang Lang) SessionsText {
	r := sessionsRows.Row(lang)
	return SessionsText{
		Title:           r.title,
		NoInteractive:   r.noInteractive,
		TableHeaders:    r.tableHeaders,
		TableNote:       r.tableNote,
		OutcomeOKErrors: func(errs int) string { return fmt.Sprintf(r.outcomeOKErrorsFmt, errs) },
		OutcomeFallback: func(n int) string { return fmt.Sprintf(r.outcomeFallbackFmt, n) },
		CompactionChainNote: func(child, parent string) string {
			return fmt.Sprintf(r.compactionChainNoteFmt, child, parent)
		},
		LongTailOpen: func(n, turnCap int) string { return fmt.Sprintf(r.longTailOpenFmt, n, turnCap) },
	}
}
