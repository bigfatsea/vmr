// Ver 2026-09-22 02:10, by Sonnet 5

// Pairs with internal/report/viewmodel_sticky.go (§6.5 Sticky Effectiveness).
package i18n

import "fmt"

// StickyText is viewmodel_sticky.go's text, in one language.
type StickyText struct {
	Title            string
	Intro1           string
	Intro2           string
	TableHeaders     [6]string // group, requests, with usage, cache efficiency, cached, fresh
	RowContinued     string
	RowSwitched      string
	InsufficientData func(minBasis int) string
	Working          func(continuedPct, switchedPct, deltaPct string) string
	Reversed         func(switchedPct, continuedPct string) string
	ReversedNote2    string
	NoDifference     string
	BasisNote        func(first int, ungroupedSuffix string) string
	UngroupedSuffix  func(n int) string
	NoExplainNote    string
	ByModelTitle     string
	ByModelHeaders   [7]string // model, protocol, continued, cache eff, switched, cache eff, delta
	DeltaEmptyNote   string
}

// stickyRow holds report_sticky.go's literal templates, one row per Lang
// (Table's own doc comment).
type stickyRow struct {
	title               string
	intro1              string
	intro2              string
	tableHeaders        [6]string
	rowContinued        string
	rowSwitched         string
	insufficientDataFmt string
	workingFmt          string
	reversedFmt         string
	reversedNote2       string
	noDifference        string
	basisNoteFmt        string
	ungroupedSuffixFmt  string
	noExplainNote       string
	byModelTitle        string
	byModelHeaders      [7]string
	deltaEmptyNote      string
}

var stickyRows = Table[stickyRow]{
	EN: {
		title:               "§6.5 Sticky Effectiveness ⭐",
		intro1:              "Within the same session: requests that landed back on the **previous request's endpoint** vs. requests that **switched endpoints** — cache efficiency compared between the two groups.\n",
		intro2:              "The Sticky Model's only reason to exist is keeping the upstream prompt cache warm — this section is the evidence for whether it actually delivers that.\n\n",
		tableHeaders:        [6]string{"Group", "Requests", "With usage", "Cache Efficiency⭐", "cached", "fresh"},
		rowContinued:        "Same endpoint",
		rowSwitched:         "Switched endpoint",
		insufficientDataFmt: "> Not enough samples (either group's usage-bearing record count < %d); no conclusion drawn this period.\n",
		workingFmt:          "> **Sticky is working**: cache efficiency is %s when landing back on the same endpoint vs. %s after switching — a gap of %s.\n",
		reversedFmt:         "> ⚠️ **Reversed**: cache efficiency is actually higher after switching endpoints (%s vs %s). Sample skew (e.g. switches clustering at short-session openings) is the most common explanation,\n",
		reversedNote2:       "> but if this persists, it's worth checking whether these virtual models' sticky_ttl is too long, pinning sessions to an endpoint whose cache has already gone cold.\n",
		noDifference:        "> Both groups have the same cache efficiency — sticky made no observable difference in this data.\n",
		basisNoteFmt:        ">\n> Basis: a session's first request (%d) has no prior request to compare against and isn't counted in either group%s.\n",
		ungroupedSuffixFmt:  "; %d records that couldn't be grouped into a session are likewise excluded",
		noExplainNote:       "> **Doesn't explain WHY a switch happened**: sticky_ttl expiry, endpoint cooldown, conditional routing eliminating the sticky pick, or the model simply not having sticky enabled — these can't be told apart after the fact; this section only states what happened.\n\n",
		byModelTitle:        "**By Virtual Model** (sticky is configured per virtual model — this is the actionable granularity)",
		byModelHeaders:      [7]string{"Model", "Protocol", "Same Endpoint", "Cache Eff.⭐", "Switched", "Cache Eff.", "Delta"},
		deltaEmptyNote:      "\n> Empty delta = either group had too few samples for this model to compare.\n\n",
	},
	ZH: {
		title:               "§6.5 Sticky 有效性 ⭐",
		intro1:              "同一会话内，落回**上一条请求所用端点**的请求 vs **换了端点**的请求，两组缓存效率对比。\n",
		intro2:              "Sticky Model（设计文档 §6.5）存在的唯一理由是让上游 prompt cache 保温——这一节是它到底有没有兑现的证据。\n\n",
		tableHeaders:        [6]string{"组", "请求", "带 usage", "缓存效率⭐", "cached", "fresh"},
		rowContinued:        "落回同一端点",
		rowSwitched:         "换了端点",
		insufficientDataFmt: "> 样本不足（任一组带 usage 的记录 < %d 条），本期不下结论。\n",
		workingFmt:          "> **Sticky 在起作用**：落回同一端点的缓存效率 %s，换端点后 %s，相差 %s。\n",
		reversedFmt:         "> ⚠️ **反常**：换端点的缓存效率反而更高（%s vs %s）。样本偏斜（例如换端点的多是短会话开头）是最常见的解释，\n",
		reversedNote2:       "> 但如果持续如此，值得检查这些虚拟模型的 sticky_ttl 是否过长、把会话钉在了缓存已经失效的端点上。\n",
		noDifference:        "> 两组缓存效率相同——这批数据里 sticky 没有产生可观测的差异。\n",
		basisNoteFmt:        ">\n> 口径：会话首条请求（%d 条）没有前一条可比，不计入任何一组%s。\n",
		ungroupedSuffixFmt:  "；未能归入会话的 %d 条同样不计入",
		noExplainNote:       "> **不解释切换原因**：sticky_ttl 到期、端点冷却、条件路由淘汰了 sticky 首选、该模型压根没开 sticky——事后无法区分，本节只陈述发生了什么。\n\n",
		byModelTitle:        "**按虚拟模型**（sticky 是按虚拟模型配置的，这一层才是可操作的粒度）",
		byModelHeaders:      [7]string{"模型", "协议", "落回同端点", "缓存效率⭐", "换了端点", "缓存效率", "差值"},
		deltaEmptyNote:      "\n> 差值为空 = 该模型任一组样本不足，不足以比较。\n\n",
	},
}

func Sticky(lang Lang) StickyText {
	r := stickyRows.Row(lang)
	return StickyText{
		Title:        r.title,
		Intro1:       r.intro1,
		Intro2:       r.intro2,
		TableHeaders: r.tableHeaders,
		RowContinued: r.rowContinued,
		RowSwitched:  r.rowSwitched,
		InsufficientData: func(minBasis int) string {
			return fmt.Sprintf(r.insufficientDataFmt, minBasis)
		},
		Working: func(continuedPct, switchedPct, deltaPct string) string {
			return fmt.Sprintf(r.workingFmt, continuedPct, switchedPct, deltaPct)
		},
		Reversed: func(switchedPct, continuedPct string) string {
			return fmt.Sprintf(r.reversedFmt, switchedPct, continuedPct)
		},
		ReversedNote2: r.reversedNote2,
		NoDifference:  r.noDifference,
		BasisNote: func(first int, ungroupedSuffix string) string {
			return fmt.Sprintf(r.basisNoteFmt, first, ungroupedSuffix)
		},
		UngroupedSuffix: func(n int) string { return fmt.Sprintf(r.ungroupedSuffixFmt, n) },
		NoExplainNote:   r.noExplainNote,
		ByModelTitle:    r.byModelTitle,
		ByModelHeaders:  r.byModelHeaders,
		DeltaEmptyNote:  r.deltaEmptyNote,
	}
}
