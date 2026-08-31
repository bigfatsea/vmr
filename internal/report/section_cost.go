// Ver 2026-08-01, by Sonnet 5

// §2 成本估算: per-model / per-endpoint / per-client $ estimates, rendered
// only when pricing resolved (see pricing.go's Pricing summary), plus a
// closing note naming the pricing sources those figures came from.
package report

import (
	"fmt"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/reqdetail"
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

	hasDate := false
	for _, d := range rep.ByDate {
		if d.CostEstimate != nil {
			hasDate = true
			break
		}
	}
	if hasDate {
		w("%s\n\n", t.ByDateTitle(cur))
		h := t.ByDateHeaders
		tbl := newTable(w, h[0], h[1], h[2], h[3])
		unpricedDate := false
		for _, d := range rep.ByDate {
			// CostEstimate == nil: none of that day's records resolved a
			// rate. Render "-" (§2.5's "unknown ≠ zero"), not a dropped row
			// — a missing row reads as "no traffic" to anyone cross-checking
			// §5's daily activity.
			cost := "-"
			if d.CostEstimate != nil {
				cost = fmt.Sprintf("%.4f %s", *d.CostEstimate, cur)
			} else {
				unpricedDate = true
			}
			tbl.row(d.Date, fmtutil.FmtTokens(d.TokensInFresh), fmtutil.FmtTokens(d.TokensOut), cost)
		}
		w("\n")
		if unpricedDate {
			w("%s\n\n", t.ByDatePartialNote)
		}
	}

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
				tbl.row(m.Model, m.Protocol, fmtutil.FmtTokens(m.TokensInFresh), fmtutil.FmtTokens(m.TokensOut),
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
				tbl.row(e.Endpoint, fmtutil.FmtTokens(e.TokensInFresh), fmtutil.FmtTokens(e.TokensOut),
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
				tbl.row(c.ClientKey, fmtutil.FmtTokens(c.TokensInFresh), fmtutil.FmtTokens(c.TokensOut),
					fmt.Sprintf("%.4f %s", *c.CostEstimate, cur))
			}
		}
		w("\n")
	}

	if !hasDate && !hasModel && !hasEndpoint && !hasClient {
		w("%s", t.NoDataBody)
	} else {
		w("%s\n\n", t.ScopeFootnote)
	}

	// Pricing is composed from several layers (embedded standard
	// table + optional supplement + per-provider config.yaml overrides),
	// so there's no longer a single file's bytes to freeze verbatim the
	// way an earlier sidecar did — this summary line is the replacement
	// traceability mechanism (see pricing.go's package doc comment).
	summary := fmt.Sprintf("standard table generated %s", orDash2(rep.Pricing.StandardGeneratedAt == "", "(unknown)", rep.Pricing.StandardGeneratedAt))
	if rep.Pricing.Supplement != "" {
		summary += fmt.Sprintf("; supplement: %s", rep.Pricing.Supplement)
	}
	if rep.Pricing.ProviderOverrides > 0 {
		summary += fmt.Sprintf("; %d provider override rule(s) applied", rep.Pricing.ProviderOverrides)
	}
	w("%s\n\n", reqdetail.Details(t.FrozenSnapshotSummary, summary))
}
