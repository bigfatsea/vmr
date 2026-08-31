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

	dateTot := renderCostByDate(w, rep, t, cur)
	modelTot := renderCostByModel(w, rep, t, cur)
	epTot := renderCostByEndpoint(w, rep, t, cur)
	clientTot := renderCostByClient(w, rep, t, cur)

	if dateTot.priced == 0 && modelTot.priced == 0 && epTot.priced == 0 && clientTot.priced == 0 {
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

// renderCostByDate renders §2's per-day table and returns its totals.
func renderCostByDate(w func(string, ...any), rep *Report2, t i18n.CostText, cur string) costTotal {
	dateTot := costTotalOf(len(rep.ByDate), func(i int) *float64 { return rep.ByDate[i].CostEstimate })
	if dateTot.priced > 0 {
		w("%s\n\n", t.ByDateTitle(cur))
		h := t.ByDateHeaders
		tbl := newTable(w, h[0], h[1], h[2], h[3])
		for _, d := range rep.ByDate {
			// CostEstimate == nil: none of that day's records resolved a
			// rate. Render "-" ("unknown ≠ zero"), not a dropped row — a
			// missing row reads as "no traffic" to anyone cross-checking
			// §5's daily activity.
			cost := "-"
			if d.CostEstimate != nil {
				cost = money(*d.CostEstimate, cur)
			}
			tbl.row(d.Date, fmtutil.FmtTokens(d.TokensInFresh), fmtutil.FmtTokens(d.TokensOut), cost)
		}
		tbl.row(t.TotalLabel, "", "", money(dateTot.sum, cur))
		w("\n")
		if dateTot.unpriced > 0 {
			w("%s\n\n", t.ByDatePartialNote)
			w("%s\n\n", t.UnpricedNote(dateTot.unpriced, dateTot.priced+dateTot.unpriced, t.UnitDays))
		}
	}
	return dateTot
}

// renderCostByModel renders §2's per-model table and returns its totals.
func renderCostByModel(w func(string, ...any), rep *Report2, t i18n.CostText, cur string) costTotal {
	modelTot := costTotalOf(len(rep.ByModel), func(i int) *float64 { return rep.ByModel[i].CostEstimate })
	if modelTot.priced > 0 {
		w("%s\n\n", t.ByModelTitle(cur))
		h := t.ByModelHeaders
		tbl := newTable(w, h[0], h[1], h[2], h[3], h[4])
		for _, m := range rep.ByModel {
			if m.CostEstimate != nil {
				tbl.row(m.Model, m.Protocol, fmtutil.FmtTokens(m.TokensInFresh), fmtutil.FmtTokens(m.TokensOut),
					money(*m.CostEstimate, cur))
			}
		}
		tbl.row(t.TotalLabel, "", "", "", money(modelTot.sum, cur))
		w("\n")
		if modelTot.unpriced > 0 {
			w("%s\n\n", t.UnpricedNote(modelTot.unpriced, modelTot.priced+modelTot.unpriced, t.UnitModels))
		}
	}
	return modelTot
}

// renderCostByEndpoint renders §2's per-endpoint table, plus the two caveats
// only EndpointRow carries the data for (degraded-estimate share,
// incomplete-rate endpoints) — both apply to every table in §2, and are
// stated once here rather than repeated under each.
func renderCostByEndpoint(w func(string, ...any), rep *Report2, t i18n.CostText, cur string) costTotal {
	// Forwarded == 0: this endpoint never served a request (every attempt
	// failed), so it has no cost to attribute and its absence from the total
	// is not a pricing gap. Counting it as "unpriced" overstated the gap and
	// pointed the reader at the price table for something the price table
	// has nothing to do with.
	epTot := costTotalOf(len(rep.EndpointsAll), func(i int) *float64 {
		if rep.EndpointsAll[i].CostEstimate == nil && rep.EndpointsAll[i].Forwarded == 0 {
			return skipRow
		}
		return rep.EndpointsAll[i].CostEstimate
	})
	if epTot.priced > 0 {
		w("%s\n\n", t.ByEndpointTitle(cur))
		h := t.ByEndpointHeaders
		tbl := newTable(w, h[0], h[1], h[2], h[3])
		for _, e := range rep.EndpointsAll {
			if e.CostEstimate != nil {
				tbl.row(e.Endpoint, fmtutil.FmtTokens(e.TokensInFresh), fmtutil.FmtTokens(e.TokensOut),
					money(*e.CostEstimate, cur))
			}
		}
		tbl.row(t.TotalLabel, "", "", money(epTot.sum, cur))
		w("\n")
		if epTot.unpriced > 0 {
			w("%s\n\n", t.UnpricedNote(epTot.unpriced, epTot.priced+epTot.unpriced, t.UnitEndpoints))
		}
		// The degraded-estimate and incomplete-rate shares are only
		// recoverable per endpoint (EndpointRow carries CostEstimateEst and
		// CostRateIncomplete; no other bucket does), so both caveats are
		// stated once, here, and apply to every table above and below.
		var degraded float64
		incomplete := 0
		for _, e := range rep.EndpointsAll {
			degraded += e.CostEstimateEst
			if e.CostRateIncomplete {
				incomplete++
			}
		}
		if degraded > 0 && epTot.sum > 0 {
			w("%s\n\n", t.DegradedNote(degraded, degraded/epTot.sum*100, cur))
		}
		if incomplete > 0 {
			w("%s\n\n", t.IncompleteRateNote(incomplete))
		}
	}
	return epTot
}

// renderCostByClient renders §2's per-client table and returns its totals.
func renderCostByClient(w func(string, ...any), rep *Report2, t i18n.CostText, cur string) costTotal {
	clientTot := costTotalOf(len(rep.ByClient), func(i int) *float64 { return rep.ByClient[i].CostEstimate })
	if clientTot.priced > 0 {
		w("%s\n\n", t.ByClientTitle(cur))
		h := t.ByClientHeaders
		tbl := newTable(w, h[0], h[1], h[2], h[3])
		for _, c := range rep.ByClient {
			if c.CostEstimate != nil {
				tbl.row(c.ClientKey, fmtutil.FmtTokens(c.TokensInFresh), fmtutil.FmtTokens(c.TokensOut),
					money(*c.CostEstimate, cur))
			}
		}
		tbl.row(t.TotalLabel, "", "", money(clientTot.sum, cur))
		w("\n")
		if clientTot.unpriced > 0 {
			w("%s\n\n", t.UnpricedNote(clientTot.unpriced, clientTot.priced+clientTot.unpriced, t.UnitClients))
		}
	}
	return clientTot
}

// costTotal is one §2 table's totals-row inputs: the sum over rows that
// actually resolved a rate, and how many rows did and didn't. A total that
// silently omitted the unpriced rows without saying how many there were
// would be the same "precise, systematically low, reads like a real number"
// failure §2.5's WindowUnpricedPct exists to prevent.
type costTotal struct {
	sum              float64
	priced, unpriced int
}

// skipRow is costTotalOf's "this row belongs in neither count" sentinel —
// distinct from nil, which means "counted, and it has no price". Compared by
// pointer identity, so no real rate can ever collide with it.
var skipRow = new(float64)

// costTotalOf walks n rows through get: a rate pointer counts toward the
// total, nil counts as unpriced, skipRow counts as neither. Index-and-accessor
// rather than a slice: §2's four tables hold four different row types, and Go
// has no covariance to unify them with.
func costTotalOf(n int, get func(i int) *float64) costTotal {
	var ct costTotal
	for i := 0; i < n; i++ {
		switch c := get(i); {
		case c == skipRow:
		case c != nil:
			ct.sum += *c
			ct.priced++
		default:
			ct.unpriced++
		}
	}
	return ct
}

// money renders one $ cell — one place, so the totals row and the detail
// rows can never drift in precision or currency placement.
func money(v float64, currency string) string {
	return fmt.Sprintf("%.4f %s", v, currency)
}
