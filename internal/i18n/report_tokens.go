// Ver 2026-09-22 02:10, by Sonnet 5

// Pairs with internal/report/viewmodel_tokens.go (§1 Cost & Token Economy).
package i18n

import "fmt"

// TokensText is viewmodel_tokens.go's text, in one language.
type TokensText struct {
	Title               string
	ClassBreakdownFmt   func(known int) string
	ClassHeaders        [3]string // category, amount, share
	RowInputCached      string
	OfInSuffix          func(pct string) string
	RowInputFresh       string
	RowInputCacheWrite  string
	CacheWriteNote      string
	RowOutput           string
	RowReasoning        string
	OfOutSuffix         func(pct string) string
	BillingNote         string
	NoPricingNote       string
	PricingNote         func(disclaimer string) string
	ByModelCacheTitle   string
	ByModelHeaders      [7]string // model, protocol, requests, cache efficiency, fresh, cached, out
	RoleCharsTitle      string
	RoleHeaders         [4]string // role, chars, estimated tokens, share
	EstimatedTokensNote string
	TakeawayNote        string
}

// tokensRow holds report_tokens.go's literal templates, one row per Lang
// (Table's own doc comment). ofInSuffixFmt/ofOutSuffixFmt are identical
// between EN and ZH in the pre-existing text (the "of in"/"of out" suffix
// was never translated) — preserved as-is, not a bug this refactor fixes.
type tokensRow struct {
	title               string
	classBreakdownFmt   string
	classHeaders        [3]string
	rowInputCached      string
	ofInSuffixFmt       string
	rowInputFresh       string
	rowInputCacheWrite  string
	cacheWriteNote      string
	rowOutput           string
	rowReasoning        string
	ofOutSuffixFmt      string
	billingNote         string
	noPricingNote       string
	pricingNoteFmt      string
	byModelCacheTitle   string
	byModelHeaders      [7]string
	roleCharsTitle      string
	roleHeaders         [4]string
	estimatedTokensNote string
	takeawayNote        string
}

var tokensRows = Table[tokensRow]{
	EN: {
		title:               "§1 Cost & Token Economy",
		classBreakdownFmt:   "**Token Class Breakdown** (basis: %d records with usage)",
		classHeaders:        [3]string{"Category", "Amount", "Share"},
		rowInputCached:      "Input - cache hit",
		ofInSuffixFmt:       "%s of in",
		rowInputFresh:       "Input - fresh ⭐",
		rowInputCacheWrite:  "Input - cache_write",
		cacheWriteNote:      "(Anthropic cache creation, billed at a premium)",
		rowOutput:           "Output",
		rowReasoning:        "└ of which reasoning",
		ofOutSuffixFmt:      "%s of out",
		billingNote:         "\n> Billing basis: fresh + cache_write(×premium) + out. Cache hits are billed free/near-free by most providers.\n",
		noPricingNote:       "> No pricing configured -> no $ estimate shown; configure it to see §2 Cost Estimate.\n",
		pricingNoteFmt:      "> %s See §2 Cost Estimate for details.\n",
		byModelCacheTitle:   "**Cache Efficiency by Model** ⭐",
		byModelHeaders:      [7]string{"Model", "Protocol", "Requests", "Cache Efficiency⭐", "fresh", "cached", "out"},
		roleCharsTitle:      "**Request Message Characters, Estimated Tokens & Share**",
		roleHeaders:         [4]string{"Role", "Chars", "Est. Tokens⭐", "Share⭐"},
		estimatedTokensNote: "\n> Est. Tokens⭐: upstream usage isn't broken down by role, so this is an estimate; share is computed on estimated tokens.\n",
		takeawayNote:        "> takeaway: when tool results dominate, the first lever for context optimization is compressing tool output, not the system prompt.\n\n",
	},
	ZH: {
		title:               "§1 成本与 Token 经济",
		classBreakdownFmt:   "**Token 类别分解**（basis: %d 条带 usage 的记录）",
		classHeaders:        [3]string{"类别", "数量", "占比"},
		rowInputCached:      "输入-缓存命中",
		ofInSuffixFmt:       "%s of in",
		rowInputFresh:       "输入-fresh ⭐",
		rowInputCacheWrite:  "输入-cache_write",
		cacheWriteNote:      "（Anthropic 缓存创建，溢价计费）",
		rowOutput:           "输出",
		rowReasoning:        "└ 其中 reasoning",
		ofOutSuffixFmt:      "%s of out",
		billingNote:         "\n> 计费口径：fresh + cache_write(×溢价) + out。缓存命中按各厂免费/极低价计。\n",
		noPricingNote:       "> 未配置定价 -> 不显示 $ 估算；配置后见 §2 成本估算。\n",
		pricingNoteFmt:      "> %s 详见 §2 成本估算。\n",
		byModelCacheTitle:   "**按模型缓存效率** ⭐",
		byModelHeaders:      [7]string{"模型", "协议", "请求", "缓存效率⭐", "fresh", "cached", "out"},
		roleCharsTitle:      "**请求消息字符、预估Token及占比**",
		roleHeaders:         [4]string{"角色", "字符", "预估Token⭐", "占比⭐"},
		estimatedTokensNote: "\n> 预估Token⭐：上游 usage 不按角色拆分，无法拿到真实值，这里使用估算公式计算；占比按预估Token 计算。\n",
		takeawayNote:        "> takeaway: tool 结果占比最大时，上下文优化的首要杠杆是压缩 tool 返回，而非 system prompt。\n\n",
	},
}

func Tokens(lang Lang) TokensText {
	r := tokensRows.Row(lang)
	return TokensText{
		Title:               r.title,
		ClassBreakdownFmt:   func(known int) string { return fmt.Sprintf(r.classBreakdownFmt, known) },
		ClassHeaders:        r.classHeaders,
		RowInputCached:      r.rowInputCached,
		OfInSuffix:          func(pct string) string { return fmt.Sprintf(r.ofInSuffixFmt, pct) },
		RowInputFresh:       r.rowInputFresh,
		RowInputCacheWrite:  r.rowInputCacheWrite,
		CacheWriteNote:      r.cacheWriteNote,
		RowOutput:           r.rowOutput,
		RowReasoning:        r.rowReasoning,
		OfOutSuffix:         func(pct string) string { return fmt.Sprintf(r.ofOutSuffixFmt, pct) },
		BillingNote:         r.billingNote,
		NoPricingNote:       r.noPricingNote,
		PricingNote:         func(disclaimer string) string { return fmt.Sprintf(r.pricingNoteFmt, disclaimer) },
		ByModelCacheTitle:   r.byModelCacheTitle,
		ByModelHeaders:      r.byModelHeaders,
		RoleCharsTitle:      r.roleCharsTitle,
		RoleHeaders:         r.roleHeaders,
		EstimatedTokensNote: r.estimatedTokensNote,
		TakeawayNote:        r.takeawayNote,
	}
}
