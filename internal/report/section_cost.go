// Ver 2026-07-28 19:20, by Opus 5

// §2 成本估算: per-model / per-endpoint / per-client $ estimates, rendered
// only when a pricing sidecar was loaded, plus the frozen snapshot of the
// pricing.yaml those figures came from.
package report

import (
	"fmt"
)

// ---- §2 成本估算 ----
func renderCostEstimate(w func(string, ...any), rep *Report2) {
	w("## §2 成本估算\n\n")
	if rep.Pricing == nil {
		w("未配置定价（`-pricing pricing.yaml`），本章节不显示 $ 估算。\n\n")
		return
	}
	w("> %s\n\n", rep.Pricing.Disclaimer())
	cur := rep.Pricing.Currency

	hasModel := false
	for _, m := range rep.ByModel {
		if m.CostEstimate != nil {
			hasModel = true
			break
		}
	}
	if hasModel {
		w("**按模型估算成本**（%s）\n\n", cur)
		tbl := newTable(w, "模型", "协议", "fresh", "out", "估算成本")
		for _, m := range rep.ByModel {
			if m.CostEstimate != nil {
				tbl.row(m.Model, m.Protocol, fmtTokens(m.TokensInFresh), fmtTokens(m.TokensOut),
					fmt.Sprintf("%.4f %s", *m.CostEstimate, cur))
			}
		}
		w("\n")
	}

	hasEndpoint := false
	for _, e := range rep.EndpointsAll {
		if e.CostEstimate != nil {
			hasEndpoint = true
			break
		}
	}
	if hasEndpoint {
		w("**按端点估算成本**（%s，跨日合并）\n\n", cur)
		tbl := newTable(w, "端点", "fresh", "out", "估算成本")
		for _, e := range rep.EndpointsAll {
			if e.CostEstimate != nil {
				tbl.row(e.Endpoint, fmtTokens(e.TokensInFresh), fmtTokens(e.TokensOut),
					fmt.Sprintf("%.4f %s", *e.CostEstimate, cur))
			}
		}
		w("\n")
	}

	hasClient := false
	for _, c := range rep.ByClient {
		if c.CostEstimate != nil {
			hasClient = true
			break
		}
	}
	if hasClient {
		w("**按客户端估算成本**（%s）\n\n", cur)
		tbl := newTable(w, "client_key", "fresh", "out", "估算成本")
		for _, c := range rep.ByClient {
			if c.CostEstimate != nil {
				tbl.row(c.ClientKey, fmtTokens(c.TokensInFresh), fmtTokens(c.TokensOut),
					fmt.Sprintf("%.4f %s", *c.CostEstimate, cur))
			}
		}
		w("\n")
	}

	if !hasModel && !hasEndpoint && !hasClient {
		w("配置了定价，但没有请求命中已配置的端点，暂无成本数据。\n\n")
	}

	// Freeze the exact pricing.yaml used for this report, collapsed by
	// default — pricing.yaml can keep changing after the fact; embedding it
	// verbatim means a later reader of this report is never left guessing
	// which price snapshot the $ figures above actually came from.
	if len(rep.Pricing.Raw) > 0 {
		w("%s\n", details("本次使用的定价配置（冻结快照，点击展开）", codeFence(string(rep.Pricing.Raw))))
	}
}
