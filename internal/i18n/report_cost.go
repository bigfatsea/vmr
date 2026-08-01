// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/section_cost.go (§2 Cost Estimate).
package i18n

// CostText is section_cost.go's text, in one language.
type CostText struct {
	Title                 string
	NoPricingBody         string
	PricingNote           func(disclaimer string) string
	ByModelTitle          func(currency string) string
	ByModelHeaders        [5]string // model, protocol, fresh, out, estimated cost
	ByEndpointTitle       func(currency string) string
	ByEndpointHeaders     [4]string // endpoint, fresh, out, estimated cost
	ByClientTitle         func(currency string) string
	ByClientHeaders       [4]string // client_key, fresh, out, estimated cost
	NoDataBody            string
	FrozenSnapshotSummary string
	Disclaimer            func(asOf, currency string) string
}

func Cost(lang Lang) CostText {
	if lang == ZH {
		return CostText{
			Title:                 "§2 成本估算",
			NoPricingBody:         "未配置定价（`-pricing pricing.yaml`），本章节不显示 $ 估算。\n\n",
			PricingNote:           func(disclaimer string) string { return "> " + disclaimer + "\n\n" },
			ByModelTitle:          func(cur string) string { return "**按模型估算成本**（" + cur + "）" },
			ByModelHeaders:        [5]string{"模型", "协议", "fresh", "out", "估算成本"},
			ByEndpointTitle:       func(cur string) string { return "**按端点估算成本**（" + cur + "，跨日合并）" },
			ByEndpointHeaders:     [4]string{"端点", "fresh", "out", "估算成本"},
			ByClientTitle:         func(cur string) string { return "**按客户端估算成本**（" + cur + "）" },
			ByClientHeaders:       [4]string{"client_key", "fresh", "out", "估算成本"},
			NoDataBody:            "配置了定价，但没有请求命中已配置的端点，暂无成本数据。\n\n",
			FrozenSnapshotSummary: "本次使用的定价配置（冻结快照，点击展开）",
			Disclaimer: func(asOf, currency string) string {
				return "成本估算基于 " + asOf + " 的价格配置（货币 " + currency + "），不代表报告所涵盖历史请求实际发生时的价格。"
			},
		}
	}
	return CostText{
		Title:                 "§2 Cost Estimate",
		NoPricingBody:         "No pricing configured (`-pricing pricing.yaml`); this section shows no $ estimate.\n\n",
		PricingNote:           func(disclaimer string) string { return "> " + disclaimer + "\n\n" },
		ByModelTitle:          func(cur string) string { return "**Estimated Cost by Model** (" + cur + ")" },
		ByModelHeaders:        [5]string{"Model", "Protocol", "fresh", "out", "Est. Cost"},
		ByEndpointTitle:       func(cur string) string { return "**Estimated Cost by Endpoint** (" + cur + ", merged across dates)" },
		ByEndpointHeaders:     [4]string{"Endpoint", "fresh", "out", "Est. Cost"},
		ByClientTitle:         func(cur string) string { return "**Estimated Cost by Client** (" + cur + ")" },
		ByClientHeaders:       [4]string{"client_key", "fresh", "out", "Est. Cost"},
		NoDataBody:            "Pricing is configured, but no request matched a configured endpoint — no cost data yet.\n\n",
		FrozenSnapshotSummary: "Pricing config used for this report (frozen snapshot, click to expand)",
		Disclaimer: func(asOf, currency string) string {
			return "Cost estimates are based on the " + asOf + " pricing config (currency " + currency + "); they do not represent the actual prices in effect when the requests in this report historically occurred."
		},
	}
}
