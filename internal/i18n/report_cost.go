// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/section_cost.go (§2 Cost Estimate).
package i18n

// CostText is section_cost.go's text, in one language.
type CostText struct {
	Title                 string
	NoPricingBody         string
	PricingNote           func(disclaimer string) string
	ByDateTitle           func(currency string) string
	ByDateHeaders         [4]string // date, fresh, out, estimated cost
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
			NoPricingBody:         "未找到可用的定价数据（内置标准表 + config.yaml 的 pricing/providers[].pricing 均未生效），本章节不显示 $ 估算。\n\n",
			PricingNote:           func(disclaimer string) string { return "> " + disclaimer + "\n\n" },
			ByDateTitle:           func(cur string) string { return "**按日估算成本**（" + cur + "）" },
			ByDateHeaders:         [4]string{"日期", "fresh", "out", "估算成本"},
			ByModelTitle:          func(cur string) string { return "**按模型估算成本**（" + cur + "）" },
			ByModelHeaders:        [5]string{"模型", "协议", "fresh", "out", "估算成本"},
			ByEndpointTitle:       func(cur string) string { return "**按端点估算成本**（" + cur + "，跨日合并）" },
			ByEndpointHeaders:     [4]string{"端点", "fresh", "out", "估算成本"},
			ByClientTitle:         func(cur string) string { return "**按客户端估算成本**（" + cur + "）" },
			ByClientHeaders:       [4]string{"client_key", "fresh", "out", "估算成本"},
			NoDataBody:            "配置了定价，但没有请求命中已配置的端点，暂无成本数据。\n\n",
			FrozenSnapshotSummary: "本次使用的定价来源",
			Disclaimer: func(asOf, currency string) string {
				return "成本估算基于标准价目表（生成于 " + asOf + "）与 config.yaml 的账号覆盖（货币 " + currency + "），不代表报告所涵盖历史请求实际发生时的价格。"
			},
		}
	}
	return CostText{
		Title:                 "§2 Cost Estimate",
		NoPricingBody:         "No pricing data available (neither the embedded standard table nor config.yaml's pricing/providers[].pricing resolved anything); this section shows no $ estimate.\n\n",
		PricingNote:           func(disclaimer string) string { return "> " + disclaimer + "\n\n" },
		ByDateTitle:           func(cur string) string { return "**Estimated Cost by Date** (" + cur + ")" },
		ByDateHeaders:         [4]string{"Date", "fresh", "out", "Est. Cost"},
		ByModelTitle:          func(cur string) string { return "**Estimated Cost by Model** (" + cur + ")" },
		ByModelHeaders:        [5]string{"Model", "Protocol", "fresh", "out", "Est. Cost"},
		ByEndpointTitle:       func(cur string) string { return "**Estimated Cost by Endpoint** (" + cur + ", merged across dates)" },
		ByEndpointHeaders:     [4]string{"Endpoint", "fresh", "out", "Est. Cost"},
		ByClientTitle:         func(cur string) string { return "**Estimated Cost by Client** (" + cur + ")" },
		ByClientHeaders:       [4]string{"client_key", "fresh", "out", "Est. Cost"},
		NoDataBody:            "Pricing is configured, but no request matched a configured endpoint — no cost data yet.\n\n",
		FrozenSnapshotSummary: "Pricing sources used for this report",
		Disclaimer: func(asOf, currency string) string {
			return "Cost estimates are based on the standard price table (generated " + asOf + ") plus any config.yaml account overrides (currency " + currency + "); they do not represent the actual prices in effect when the requests in this report historically occurred."
		},
	}
}
