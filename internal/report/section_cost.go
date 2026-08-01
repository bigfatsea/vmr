// Ver 2026-08-01, by Sonnet 5

// §2 成本估算: per-model / per-endpoint / per-client $ estimates, rendered
// only when a pricing sidecar was loaded, plus the frozen snapshot of the
// pricing.yaml those figures came from.
package report

import (
	"fmt"

	"vmr/internal/i18n"
)

// ---- §2 成本估算 ----
func renderCostEstimate(w func(string, ...any), rep *Report2, lang i18n.Lang) {
	t := i18n.Cost(lang)
	w("## %s\n\n", t.Title)
	if rep.Pricing == nil {
		w("%s", t.NoPricingBody)
		return
	}
	w("%s", t.PricingNote(rep.Pricing.Disclaimer(lang)))
	cur := rep.Pricing.Currency

	hasModel := false
	for _, m := range rep.ByModel {
		if m.CostEstimate != nil {
			hasModel = true
			break
		}
	}
	if hasModel {
		w("%s\n\n", t.ByModelTitle(cur))
		h := t.ByModelHeaders
		tbl := newTable(w, h[0], h[1], h[2], h[3], h[4])
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
		w("%s\n\n", t.ByEndpointTitle(cur))
		h := t.ByEndpointHeaders
		tbl := newTable(w, h[0], h[1], h[2], h[3])
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
		w("%s\n\n", t.ByClientTitle(cur))
		h := t.ByClientHeaders
		tbl := newTable(w, h[0], h[1], h[2], h[3])
		for _, c := range rep.ByClient {
			if c.CostEstimate != nil {
				tbl.row(c.ClientKey, fmtTokens(c.TokensInFresh), fmtTokens(c.TokensOut),
					fmt.Sprintf("%.4f %s", *c.CostEstimate, cur))
			}
		}
		w("\n")
	}

	if !hasModel && !hasEndpoint && !hasClient {
		w("%s", t.NoDataBody)
	}

	// Freeze the exact pricing.yaml used for this report, collapsed by
	// default — pricing.yaml can keep changing after the fact; embedding it
	// verbatim means a later reader of this report is never left guessing
	// which price snapshot the $ figures above actually came from.
	if len(rep.Pricing.Raw) > 0 {
		w("%s\n", details(t.FrozenSnapshotSummary, codeFence(string(rep.Pricing.Raw))))
	}
}
