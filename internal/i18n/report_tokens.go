// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/section_tokens.go (§1 Cost & Token Economy).
package i18n

import "strconv"

// TokensText is section_tokens.go's text, in one language.
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

func Tokens(lang Lang) TokensText {
	if lang == ZH {
		return TokensText{
			Title: "§1 成本与 Token 经济",
			ClassBreakdownFmt: func(known int) string {
				return "**Token 类别分解**（basis: " + strconv.Itoa(known) + " 条带 usage 的记录）"
			},
			ClassHeaders:        [3]string{"类别", "数量", "占比"},
			RowInputCached:      "输入-缓存命中",
			OfInSuffix:          func(pct string) string { return pct + " of in" },
			RowInputFresh:       "输入-fresh ⭐",
			RowInputCacheWrite:  "输入-cache_write",
			CacheWriteNote:      "（Anthropic 缓存创建，溢价计费）",
			RowOutput:           "输出",
			RowReasoning:        "└ 其中 reasoning",
			OfOutSuffix:         func(pct string) string { return pct + " of out" },
			BillingNote:         "\n> 计费口径：fresh + cache_write(×溢价) + out。缓存命中按各厂免费/极低价计。\n",
			NoPricingNote:       "> 未配置定价 -> 不显示 $ 估算；配置后见 §2 成本估算。\n",
			PricingNote:         func(disclaimer string) string { return "> " + disclaimer + " 详见 §2 成本估算。\n" },
			ByModelCacheTitle:   "**按模型缓存效率** ⭐",
			ByModelHeaders:      [7]string{"模型", "协议", "请求", "缓存效率⭐", "fresh", "cached", "out"},
			RoleCharsTitle:      "**请求消息字符、预估Token及占比**",
			RoleHeaders:         [4]string{"角色", "字符", "预估Token⭐", "占比⭐"},
			EstimatedTokensNote: "\n> 预估Token⭐：上游 usage 不按角色拆分，无法拿到真实值，这里使用估算公式计算；占比按预估Token 计算。\n",
			TakeawayNote:        "> takeaway: tool 结果占比最大时，上下文优化的首要杠杆是压缩 tool 返回，而非 system prompt。\n\n",
		}
	}
	return TokensText{
		Title: "§1 Cost & Token Economy",
		ClassBreakdownFmt: func(known int) string {
			return "**Token Class Breakdown** (basis: " + strconv.Itoa(known) + " records with usage)"
		},
		ClassHeaders:        [3]string{"Category", "Amount", "Share"},
		RowInputCached:      "Input - cache hit",
		OfInSuffix:          func(pct string) string { return pct + " of in" },
		RowInputFresh:       "Input - fresh ⭐",
		RowInputCacheWrite:  "Input - cache_write",
		CacheWriteNote:      "(Anthropic cache creation, billed at a premium)",
		RowOutput:           "Output",
		RowReasoning:        "└ of which reasoning",
		OfOutSuffix:         func(pct string) string { return pct + " of out" },
		BillingNote:         "\n> Billing basis: fresh + cache_write(×premium) + out. Cache hits are billed free/near-free by most providers.\n",
		NoPricingNote:       "> No pricing configured -> no $ estimate shown; configure it to see §2 Cost Estimate.\n",
		PricingNote:         func(disclaimer string) string { return "> " + disclaimer + " See §2 Cost Estimate for details.\n" },
		ByModelCacheTitle:   "**Cache Efficiency by Model** ⭐",
		ByModelHeaders:      [7]string{"Model", "Protocol", "Requests", "Cache Efficiency⭐", "fresh", "cached", "out"},
		RoleCharsTitle:      "**Request Message Characters, Estimated Tokens & Share**",
		RoleHeaders:         [4]string{"Role", "Chars", "Est. Tokens⭐", "Share⭐"},
		EstimatedTokensNote: "\n> Est. Tokens⭐: upstream usage isn't broken down by role, so this is an estimate; share is computed on estimated tokens.\n",
		TakeawayNote:        "> takeaway: when tool results dominate, the first lever for context optimization is compressing tool output, not the system prompt.\n\n",
	}
}
