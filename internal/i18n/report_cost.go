// Ver 2026-09-22 17:35, by coding

// Pairs with internal/report/viewmodel_cost.go (§2 Cost Estimate).
package i18n

import "fmt"

// CostText is viewmodel_cost.go's text, in one language.
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
	// StandardTableSummary / ProviderRulesApplied render the §2 pricing-
	// source traceability line: the standard table's own generation stamp
	// and how many config.yaml rate rules were layered on top.
	StandardTableSummary func(generated string) string
	ProviderRulesApplied func(n int) string
	Disclaimer           func(asOf, currency string) string
	ScopeFootnote        string

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
	// CurrencyFallbackNote is appended to Disclaimer when a display
	// currency was requested but the rate to it was never configured, so
	// the figures stayed in the computation currency.
	CurrencyFallbackNote func(requested, actual string) string
}

// costRow holds this file's CostText literal templates, one row per Lang
// (Table's own doc comment). Every func-typed CostText field wraps a
// plain format/concatenation template here; the interpolating closures
// themselves are written once in Cost below. The two Disclaimer rows
// embed the asOf/currency values mid-sentence rather than at a tail, so
// they carry two %s placeholders consumed in order.
type costRow struct {
	title                 string
	noPricingBody         string
	byDateTitleFmt        string
	byDateHeaders         [4]string
	byDatePartialNote     string
	byModelTitleFmt       string
	byModelHeaders        [5]string
	byEndpointTitleFmt    string
	byEndpointHeaders     [4]string
	byClientTitleFmt      string
	byClientHeaders       [4]string
	noDataBody            string
	frozenSnapshotSummary string
	standardTableSumFmt   string
	providerRulesFmt      string
	disclaimerFmt         string
	scopeFootnote         string
	totalLabel            string
	unpricedNoteFmt       string
	unitDays              string
	unitModels            string
	unitEndpoints         string
	unitClients           string
	degradedNoteFmt       string
	incompleteRateFmt     string
	currencyFallbackFmt   string
}

var costRows = Table[costRow]{
	EN: {
		title:                 "§2 Pay-As-You-Go Equivalent Cost",
		noPricingBody:         "No pricing data available (neither the embedded standard table nor config.yaml's pricing/providers[].pricing resolved anything); this section shows no $ estimate.\n\n",
		byDateTitleFmt:        "**Estimated Cost by Date** (%s)",
		byDateHeaders:         [4]string{"Date", "fresh", "out", "Est. Cost"},
		byDatePartialNote:     "> Some dates' upstream endpoints resolved no applicable rate; their estimated cost renders as `-` (that day's cost is unknown, not zero).",
		byModelTitleFmt:       "**Estimated Cost by Model** (%s)",
		byModelHeaders:        [5]string{"Model", "Protocol", "fresh", "out", "Est. Cost"},
		byEndpointTitleFmt:    "**Estimated Cost by Endpoint** (%s, merged across dates)",
		byEndpointHeaders:     [4]string{"Endpoint", "fresh", "out", "Est. Cost"},
		byClientTitleFmt:      "**Estimated Cost by Client** (%s)",
		byClientHeaders:       [4]string{"client_key", "fresh", "out", "Est. Cost"},
		noDataBody:            "Pricing is configured, but no request matched a configured endpoint — no cost data yet.\n\n",
		frozenSnapshotSummary: "Pricing sources used for this report",
		standardTableSumFmt:   "standard table generated %s",
		providerRulesFmt:      "; %d provider rate rule(s) applied",
		disclaimerFmt:         "The figures in this section are a PAY-AS-YOU-GO EQUIVALENT: what this traffic would cost billed per token at the published prices vmr can resolve for it — the serving platform's own rate where one exists, otherwise the model maker's list price. They are not what you paid — a subscription/plan account's marginal cost is 0, and only you know the real unit price behind a reseller or proxy. They answer \"was this plan/proxy worth it\". Prices come from the standard table (generated %s) plus any config.yaml account overrides (currency %s), and do not represent the prices in effect when these requests historically occurred; configure providers[].pricing.rates on the relevant provider to price at what you actually pay.",
		scopeFootnote:         "> Estimated cost includes requests where usage was not sniffed (priced via fallback estimation); the fresh/out columns only count confirmed token usage. Calculating unit price as \"Est. Cost ÷ Tokens\" may yield an inflated figure.",
		totalLabel:            "**Total**",
		unpricedNoteFmt:       "> The total excludes %d/%d %s with no priced traffic to attribute (the upstream endpoint is not in the price table, or the requests never reached one successfully) — cost unknown, not zero.",
		unitDays:              "days", unitModels: "models", unitEndpoints: "endpoints", unitClients: "clients",
		degradedNoteFmt:     "> About %.4f %s of the total (%.1f%%) came from a degraded estimate: the upstream reported no usage, so tokens were estimated from request/response body size before pricing.",
		incompleteRateFmt:   "> %d endpoint(s) were priced through a rate missing at least one component (a vendor that publishes no cache_read/cache_write price). A missing component prices as 0, so those endpoints' cost is a systematically LOW bound, not an accurate figure.",
		currencyFallbackFmt: " (requested %s display, but no exchange rate configured; defaulted to %s)",
	},
	ZH: {
		title:                 "§2 按量计费等价成本",
		noPricingBody:         "未找到可用的定价数据（内置标准表 + config.yaml 的 pricing/providers[].pricing 均未生效），本章节不显示 $ 估算。\n\n",
		byDateTitleFmt:        "**按日估算成本**（%s）",
		byDateHeaders:         [4]string{"日期", "fresh", "out", "估算成本"},
		byDatePartialNote:     "> 部分日期的上游端点未解析出适用单价，其估算成本渲染为 `-`（当日成本未知，并非 0）。",
		byModelTitleFmt:       "**按模型估算成本**（%s）",
		byModelHeaders:        [5]string{"模型", "协议", "fresh", "out", "估算成本"},
		byEndpointTitleFmt:    "**按端点估算成本**（%s，跨日合并）",
		byEndpointHeaders:     [4]string{"端点", "fresh", "out", "估算成本"},
		byClientTitleFmt:      "**按客户端估算成本**（%s）",
		byClientHeaders:       [4]string{"client_key", "fresh", "out", "估算成本"},
		noDataBody:            "配置了定价，但没有请求命中已配置的端点，暂无成本数据。\n\n",
		frozenSnapshotSummary: "本次使用的定价来源",
		standardTableSumFmt:   "标准价目表生成于 %s",
		providerRulesFmt:      "；已套用 %d 条 provider 费率规则",
		disclaimerFmt:         "本章金额是**按量计费等价成本**：这些流量若按 vmr 能解析到的公开价逐 Token 计费要花多少——渠道有自定价时用渠道价，否则用第一方列表价。它不是实付金额——包月/套餐账号的边际成本是 0，经转售商或代理的实际单价也只有你自己知道。它回答的是「这个套餐/代理买得值不值」。价格取自标准价目表（生成于 %s）与 config.yaml 的账号覆盖（货币 %s），不代表历史请求实际发生时的价格；要用实付价请在对应 provider 的 providers[].pricing.rates 里写明。",
		scopeFootnote:         "> 估算成本包含了未嗅探到 usage 的请求（按降级估算定价计入）；而 fresh/out 列仅统计已确认的 Token 数量。若按「估算成本 ÷ Token」反推单价可能偏高。",
		totalLabel:            "**合计**",
		unpricedNoteFmt:       "> 合计不含 %d/%d %s——没有可归属的已定价流量（上游端点不在定价表内，或其请求未成功送达任何上游端点）。成本未知，不是 0。",
		unitDays:              "天", unitModels: "个模型", unitEndpoints: "个端点", unitClients: "个客户端",
		degradedNoteFmt:     "> 合计中约 %.4f %s（%.1f%%）来自降级估算：上游未返回 usage，按请求/响应体字节数推算 Token 后计价。",
		incompleteRateFmt:   "> 有 %d 个端点的单价缺分量（如厂商未公布 cache_read/cache_write）：缺失分量按 0 计价，这些端点的成本是**系统性偏低**的下界，不是准确值。",
		currencyFallbackFmt: "（请求以 %s 展示，因未配置汇率，降级以 %s 展示）",
	},
}

func Cost(lang Lang) CostText {
	r := costRows.Row(lang)
	return CostText{
		Title:                 r.title,
		NoPricingBody:         r.noPricingBody,
		PricingNote:           func(disclaimer string) string { return "> " + disclaimer + "\n\n" },
		ByDateTitle:           func(cur string) string { return fmt.Sprintf(r.byDateTitleFmt, cur) },
		ByDateHeaders:         r.byDateHeaders,
		ByDatePartialNote:     r.byDatePartialNote,
		ByModelTitle:          func(cur string) string { return fmt.Sprintf(r.byModelTitleFmt, cur) },
		ByModelHeaders:        r.byModelHeaders,
		ByEndpointTitle:       func(cur string) string { return fmt.Sprintf(r.byEndpointTitleFmt, cur) },
		ByEndpointHeaders:     r.byEndpointHeaders,
		ByClientTitle:         func(cur string) string { return fmt.Sprintf(r.byClientTitleFmt, cur) },
		ByClientHeaders:       r.byClientHeaders,
		NoDataBody:            r.noDataBody,
		FrozenSnapshotSummary: r.frozenSnapshotSummary,
		StandardTableSummary:  func(gen string) string { return fmt.Sprintf(r.standardTableSumFmt, gen) },
		ProviderRulesApplied:  func(n int) string { return fmt.Sprintf(r.providerRulesFmt, n) },
		Disclaimer: func(asOf, currency string) string {
			return fmt.Sprintf(r.disclaimerFmt, asOf, currency)
		},
		ScopeFootnote: r.scopeFootnote,
		TotalLabel:    r.totalLabel,
		UnpricedNote: func(unpriced, total int, unit string) string {
			return fmt.Sprintf(r.unpricedNoteFmt, unpriced, total, unit)
		},
		UnitDays: r.unitDays, UnitModels: r.unitModels, UnitEndpoints: r.unitEndpoints, UnitClients: r.unitClients,
		DegradedNote: func(amount, pct float64, cur string) string {
			return fmt.Sprintf(r.degradedNoteFmt, amount, cur, pct)
		},
		IncompleteRateNote: func(n int) string {
			return fmt.Sprintf(r.incompleteRateFmt, n)
		},
		CurrencyFallbackNote: func(requested, actual string) string {
			return fmt.Sprintf(r.currencyFallbackFmt, requested, actual)
		},
	}
}
