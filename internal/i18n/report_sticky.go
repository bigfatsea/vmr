// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/section_sticky.go (§6.5 Sticky Effectiveness).
package i18n

// StickyText is section_sticky.go's text, in one language.
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

func Sticky(lang Lang) StickyText {
	if lang == ZH {
		return StickyText{
			Title:        "§6.5 Sticky 有效性 ⭐",
			Intro1:       "同一会话内，落回**上一条请求所用端点**的请求 vs **换了端点**的请求，两组缓存效率对比。\n",
			Intro2:       "Sticky Model（设计文档 §6.5）存在的唯一理由是让上游 prompt cache 保温——这一节是它到底有没有兑现的证据。\n\n",
			TableHeaders: [6]string{"组", "请求", "带 usage", "缓存效率⭐", "cached", "fresh"},
			RowContinued: "落回同一端点",
			RowSwitched:  "换了端点",
			InsufficientData: func(minBasis int) string {
				return "> 样本不足（任一组带 usage 的记录 < " + itoa64(int64(minBasis)) + " 条），本期不下结论。\n"
			},
			Working: func(continuedPct, switchedPct, deltaPct string) string {
				return "> **Sticky 在起作用**：落回同一端点的缓存效率 " + continuedPct + "，换端点后 " + switchedPct + "，相差 " + deltaPct + "。\n"
			},
			Reversed: func(switchedPct, continuedPct string) string {
				return "> ⚠️ **反常**：换端点的缓存效率反而更高（" + switchedPct + " vs " + continuedPct + "）。样本偏斜（例如换端点的多是短会话开头）是最常见的解释，\n"
			},
			ReversedNote2: "> 但如果持续如此，值得检查这些虚拟模型的 sticky_ttl 是否过长、把会话钉在了缓存已经失效的端点上。\n",
			NoDifference:  "> 两组缓存效率相同——这批数据里 sticky 没有产生可观测的差异。\n",
			BasisNote: func(first int, ungroupedSuffix string) string {
				return ">\n> 口径：会话首条请求（" + itoa64(int64(first)) + " 条）没有前一条可比，不计入任何一组" + ungroupedSuffix + "。\n"
			},
			UngroupedSuffix: func(n int) string { return "；未能归入会话的 " + itoa64(int64(n)) + " 条同样不计入" },
			NoExplainNote:   "> **不解释切换原因**：sticky_ttl 到期、端点冷却、条件路由淘汰了 sticky 首选、该模型压根没开 sticky——事后无法区分，本节只陈述发生了什么。\n\n",
			ByModelTitle:    "**按虚拟模型**（sticky 是按虚拟模型配置的，这一层才是可操作的粒度）",
			ByModelHeaders:  [7]string{"模型", "协议", "落回同端点", "缓存效率⭐", "换了端点", "缓存效率", "差值"},
			DeltaEmptyNote:  "\n> 差值为空 = 该模型任一组样本不足，不足以比较。\n\n",
		}
	}
	return StickyText{
		Title:        "§6.5 Sticky Effectiveness ⭐",
		Intro1:       "Within the same session: requests that landed back on the **previous request's endpoint** vs. requests that **switched endpoints** — cache efficiency compared between the two groups.\n",
		Intro2:       "The Sticky Model's only reason to exist is keeping the upstream prompt cache warm — this section is the evidence for whether it actually delivers that.\n\n",
		TableHeaders: [6]string{"Group", "Requests", "With usage", "Cache Efficiency⭐", "cached", "fresh"},
		RowContinued: "Same endpoint",
		RowSwitched:  "Switched endpoint",
		InsufficientData: func(minBasis int) string {
			return "> Not enough samples (either group's usage-bearing record count < " + itoa64(int64(minBasis)) + "); no conclusion drawn this period.\n"
		},
		Working: func(continuedPct, switchedPct, deltaPct string) string {
			return "> **Sticky is working**: cache efficiency is " + continuedPct + " when landing back on the same endpoint vs. " + switchedPct + " after switching — a gap of " + deltaPct + ".\n"
		},
		Reversed: func(switchedPct, continuedPct string) string {
			return "> ⚠️ **Reversed**: cache efficiency is actually higher after switching endpoints (" + switchedPct + " vs " + continuedPct + "). Sample skew (e.g. switches clustering at short-session openings) is the most common explanation,\n"
		},
		ReversedNote2: "> but if this persists, it's worth checking whether these virtual models' sticky_ttl is too long, pinning sessions to an endpoint whose cache has already gone cold.\n",
		NoDifference:  "> Both groups have the same cache efficiency — sticky made no observable difference in this data.\n",
		BasisNote: func(first int, ungroupedSuffix string) string {
			return ">\n> Basis: a session's first request (" + itoa64(int64(first)) + ") has no prior request to compare against and isn't counted in either group" + ungroupedSuffix + ".\n"
		},
		UngroupedSuffix: func(n int) string {
			return "; " + itoa64(int64(n)) + " records that couldn't be grouped into a session are likewise excluded"
		},
		NoExplainNote:  "> **Doesn't explain WHY a switch happened**: sticky_ttl expiry, endpoint cooldown, conditional routing eliminating the sticky pick, or the model simply not having sticky enabled — these can't be told apart after the fact; this section only states what happened.\n\n",
		ByModelTitle:   "**By Virtual Model** (sticky is configured per virtual model — this is the actionable granularity)",
		ByModelHeaders: [7]string{"Model", "Protocol", "Same Endpoint", "Cache Eff.⭐", "Switched", "Cache Eff.", "Delta"},
		DeltaEmptyNote: "\n> Empty delta = either group had too few samples for this model to compare.\n\n",
	}
}
