// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/section_cost.go (§2 Cost Estimate).
package i18n

import "fmt"

// CostText is section_cost.go's text, in one language.
type CostText struct {
	Title                 string
	NoPricingBody         string
	PricingNote           func(disclaimer string) string
	ByDateTitle           func(currency string) string
	ByDateHeaders         [4]string // date, fresh, out, estimated cost
	ByDatePartialNote     string    // rendered when some dates carried traffic but resolved no rate
	ByModelTitle          func(currency string) string
	ByModelHeaders        [5]string // model, protocol, fresh, out, estimated cost
	ByEndpointTitle       func(currency string) string
	ByEndpointHeaders     [4]string // endpoint, fresh, out, estimated cost
	ByClientTitle         func(currency string) string
	ByClientHeaders       [4]string // client_key, fresh, out, estimated cost
	NoDataBody            string
	FrozenSnapshotSummary string
	Disclaimer            func(asOf, currency string) string
	ScopeFootnote         string

	// TotalLabel names the totals row every §2 table now carries.
	TotalLabel string
	// UnpricedNote is rendered under a table that had rows it could not
	// price — n of total, in that table's own unit ("days", "endpoints").
	UnpricedNote func(unpriced, total int, unit string) string
	// UnitDays/UnitModels/UnitEndpoints/UnitClients are UnpricedNote's unit
	// nouns, so the four call sites don't each carry their own wording.
	UnitDays, UnitModels, UnitEndpoints, UnitClients string
	// DegradedNote states how much of the total came from a byte-count
	// estimate rather than usage the upstream actually reported.
	DegradedNote func(amount float64, pct float64, currency string) string
	// IncompleteRateNote states how many endpoints were priced through a
	// rate missing at least one component — their $ figure is low, not wrong.
	IncompleteRateNote func(n int) string
}

func Cost(lang Lang) CostText {
	if lang == ZH {
		return CostText{
			Title:                 "§2 按量计费等价成本",
			NoPricingBody:         "未找到可用的定价数据（内置标准表 + config.yaml 的 pricing/providers[].pricing 均未生效），本章节不显示 $ 估算。\n\n",
			PricingNote:           func(disclaimer string) string { return "> " + disclaimer + "\n\n" },
			ByDateTitle:           func(cur string) string { return "**按日估算成本**（" + cur + "）" },
			ByDateHeaders:         [4]string{"日期", "fresh", "out", "估算成本"},
			ByDatePartialNote:     "> 部分日期的上游端点未解析出适用单价，其估算成本渲染为 `-`（当日成本未知，并非 0）。",
			ByModelTitle:          func(cur string) string { return "**按模型估算成本**（" + cur + "）" },
			ByModelHeaders:        [5]string{"模型", "协议", "fresh", "out", "估算成本"},
			ByEndpointTitle:       func(cur string) string { return "**按端点估算成本**（" + cur + "，跨日合并）" },
			ByEndpointHeaders:     [4]string{"端点", "fresh", "out", "估算成本"},
			ByClientTitle:         func(cur string) string { return "**按客户端估算成本**（" + cur + "）" },
			ByClientHeaders:       [4]string{"client_key", "fresh", "out", "估算成本"},
			NoDataBody:            "配置了定价，但没有请求命中已配置的端点，暂无成本数据。\n\n",
			FrozenSnapshotSummary: "本次使用的定价来源",
			Disclaimer: func(asOf, currency string) string {
				return "本章金额是**按量计费等价成本**：这些流量若按 vmr 能解析到的公开价逐 Token 计费要花多少——渠道有自定价时用渠道价，否则用第一方列表价。它不是实付金额——包月/套餐账号的边际成本是 0，经转售商或代理的实际单价也只有你自己知道。它回答的是「这个套餐/代理买得值不值」。价格取自标准价目表（生成于 " + asOf + "）与 config.yaml 的账号覆盖（货币 " + currency + "），不代表历史请求实际发生时的价格；要用实付价请配 providers[].pricing.overrides 或 pricing.supplement。"
			},
			ScopeFootnote: "> 估算成本包含了未嗅探到 usage 的请求（按降级估算定价计入）；而 fresh/out 列仅统计已确认的 Token 数量。若按「估算成本 ÷ Token」反推单价可能偏高。",
			TotalLabel:    "**合计**",
			UnpricedNote: func(unpriced, total int, unit string) string {
				return fmt.Sprintf("> 合计不含 %d/%d %s——没有可归属的已定价流量（上游端点不在定价表内，或其请求未成功送达任何上游端点）。成本未知，不是 0。", unpriced, total, unit)
			},
			UnitDays: "天", UnitModels: "个模型", UnitEndpoints: "个端点", UnitClients: "个客户端",
			DegradedNote: func(amount, pct float64, cur string) string {
				return fmt.Sprintf("> 合计中约 %.4f %s（%.1f%%）来自降级估算：上游未返回 usage，按请求/响应体字节数推算 Token 后计价。", amount, cur, pct)
			},
			IncompleteRateNote: func(n int) string {
				return fmt.Sprintf("> 有 %d 个端点的单价缺分量（如厂商未公布 cache_read/cache_write）：缺失分量按 0 计价，这些端点的成本是**系统性偏低**的下界，不是准确值。", n)
			},
		}
	}
	return CostText{
		Title:                 "§2 Pay-As-You-Go Equivalent Cost",
		NoPricingBody:         "No pricing data available (neither the embedded standard table nor config.yaml's pricing/providers[].pricing resolved anything); this section shows no $ estimate.\n\n",
		PricingNote:           func(disclaimer string) string { return "> " + disclaimer + "\n\n" },
		ByDateTitle:           func(cur string) string { return "**Estimated Cost by Date** (" + cur + ")" },
		ByDateHeaders:         [4]string{"Date", "fresh", "out", "Est. Cost"},
		ByDatePartialNote:     "> Some dates' upstream endpoints resolved no applicable rate; their estimated cost renders as `-` (that day's cost is unknown, not zero).",
		ByModelTitle:          func(cur string) string { return "**Estimated Cost by Model** (" + cur + ")" },
		ByModelHeaders:        [5]string{"Model", "Protocol", "fresh", "out", "Est. Cost"},
		ByEndpointTitle:       func(cur string) string { return "**Estimated Cost by Endpoint** (" + cur + ", merged across dates)" },
		ByEndpointHeaders:     [4]string{"Endpoint", "fresh", "out", "Est. Cost"},
		ByClientTitle:         func(cur string) string { return "**Estimated Cost by Client** (" + cur + ")" },
		ByClientHeaders:       [4]string{"client_key", "fresh", "out", "Est. Cost"},
		NoDataBody:            "Pricing is configured, but no request matched a configured endpoint — no cost data yet.\n\n",
		FrozenSnapshotSummary: "Pricing sources used for this report",
		Disclaimer: func(asOf, currency string) string {
			return "The figures in this section are a PAY-AS-YOU-GO EQUIVALENT: what this traffic would cost billed per token at the published prices vmr can resolve for it — the serving platform's own rate where one exists, otherwise the model maker's list price. They are not what you paid — a subscription/plan account's marginal cost is 0, and only you know the real unit price behind a reseller or proxy. They answer \"was this plan/proxy worth it\". Prices come from the standard table (generated " + asOf + ") plus any config.yaml account overrides (currency " + currency + "), and do not represent the prices in effect when these requests historically occurred; configure providers[].pricing.overrides or pricing.supplement to price at what you actually pay."
		},
		ScopeFootnote: "> Estimated cost includes requests where usage was not sniffed (priced via fallback estimation); the fresh/out columns only count confirmed token usage. Calculating unit price as \"Est. Cost ÷ Tokens\" may yield an inflated figure.",
		TotalLabel:    "**Total**",
		UnpricedNote: func(unpriced, total int, unit string) string {
			return fmt.Sprintf("> The total excludes %d/%d %s with no priced traffic to attribute (the upstream endpoint is not in the price table, or the requests never reached one successfully) — cost unknown, not zero.", unpriced, total, unit)
		},
		UnitDays: "days", UnitModels: "models", UnitEndpoints: "endpoints", UnitClients: "clients",
		DegradedNote: func(amount, pct float64, cur string) string {
			return fmt.Sprintf("> About %.4f %s of the total (%.1f%%) came from a degraded estimate: the upstream reported no usage, so tokens were estimated from request/response body size before pricing.", amount, cur, pct)
		},
		IncompleteRateNote: func(n int) string {
			return fmt.Sprintf("> %d endpoint(s) were priced through a rate missing at least one component (a vendor that publishes no cache_read/cache_write price). A missing component prices as 0, so those endpoints' cost is a systematically LOW bound, not an accurate figure.", n)
		},
	}
}
