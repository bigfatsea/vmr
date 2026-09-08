// Ver 2026-09-15, by Opus 5

// §2 成本估算 view model: per-model / per-endpoint / per-client $
// estimates, rendered only when pricing resolved, plus the closing note
// naming the pricing sources. Pairs with internal/i18n/report_cost.go.
package report

import (
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/reqdetail"
)

func vmCostSection(rep *Report2, lang i18n.Lang) SectionVM {
	t := i18n.Cost(lang)
	sec := SectionVM{ID: "cost", Title: t.Title}
	if rep.Pricing == nil {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.NoPricingBody})
		return sec
	}
	sec.Blocks = append(sec.Blocks, ParaVM{Text: t.PricingNote(rep.Pricing.Disclaimer(lang))})
	cur := rep.Pricing.Currency

	dateTot := vmCostByDate(&sec, rep, t, cur)
	modelTot := vmCostByModel(&sec, rep, t, cur)
	epTot := vmCostByEndpoint(&sec, rep, t, cur)
	clientTot := vmCostByClient(&sec, rep, t, cur)

	if dateTot.priced == 0 && modelTot.priced == 0 && epTot.priced == 0 && clientTot.priced == 0 {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.NoDataBody})
	} else {
		sec.Blocks = append(sec.Blocks, ParaVM{Text: t.ScopeFootnote + "\n\n"})
	}

	// Pricing is composed from two layers (embedded standard table +
	// per-provider config.yaml pricing.rates), so there's no longer a
	// single file's bytes to freeze verbatim — this summary line is the
	// traceability mechanism.
	summary := t.StandardTableSummary(orDash2(rep.Pricing.StandardGeneratedAt == "", "(unknown)", rep.Pricing.StandardGeneratedAt))
	if rep.Pricing.ProviderOverrides > 0 {
		summary += t.ProviderRulesApplied(rep.Pricing.ProviderOverrides)
	}
	sec.Blocks = append(sec.Blocks, ParaVM{Text: reqdetail.Details(t.FrozenSnapshotSummary, summary) + "\n\n"})
	return sec
}

// vmCostByDate renders §2's per-day table and returns its totals.
func vmCostByDate(sec *SectionVM, rep *Report2, t i18n.CostText, cur string) costTotal {
	dateTot := costTotalOf(len(rep.ByDate), func(i int) *float64 { return rep.ByDate[i].CostEstimate })
	if dateTot.priced > 0 {
		tbl := &TableVM{Title: t.ByDateTitle(cur), Headers: t.ByDateHeaders[:]}
		for _, d := range rep.ByDate {
			// CostEstimate == nil: none of that day's records resolved a
			// rate. Render "-" ("unknown ≠ zero"), not a dropped row.
			cost := "-"
			if d.CostEstimate != nil {
				cost = fmtutil.FmtCurrency(*d.CostEstimate, cur)
			}
			tbl.row(d.Date, fmtutil.FmtTokens(d.TokensInFresh), fmtutil.FmtTokens(d.TokensOut), cost)
		}
		tbl.row(t.TotalLabel, "", "", fmtutil.FmtCurrency(dateTot.sum, cur))
		if dateTot.unpriced > 0 {
			tbl.note(t.ByDatePartialNote + "\n\n")
			tbl.note(t.UnpricedNote(dateTot.unpriced, dateTot.priced+dateTot.unpriced, t.UnitDays) + "\n\n")
		}
		sec.Blocks = append(sec.Blocks, tbl)
	}
	return dateTot
}

// vmCostByModel renders §2's per-model table and returns its totals.
func vmCostByModel(sec *SectionVM, rep *Report2, t i18n.CostText, cur string) costTotal {
	modelTot := costTotalOf(len(rep.ByModel), func(i int) *float64 { return rep.ByModel[i].CostEstimate })
	if modelTot.priced > 0 {
		tbl := &TableVM{Title: t.ByModelTitle(cur), Headers: t.ByModelHeaders[:]}
		for _, m := range rep.ByModel {
			if m.CostEstimate != nil {
				tbl.row(m.Model, m.Protocol, fmtutil.FmtTokens(m.TokensInFresh), fmtutil.FmtTokens(m.TokensOut),
					fmtutil.FmtCurrency(*m.CostEstimate, cur))
			}
		}
		tbl.row(t.TotalLabel, "", "", "", fmtutil.FmtCurrency(modelTot.sum, cur))
		if modelTot.unpriced > 0 {
			tbl.note(t.UnpricedNote(modelTot.unpriced, modelTot.priced+modelTot.unpriced, t.UnitModels) + "\n\n")
		}
		sec.Blocks = append(sec.Blocks, tbl)
	}
	return modelTot
}

// vmCostByEndpoint renders §2's per-endpoint table, plus the two caveats
// only EndpointRow carries the data for (degraded-estimate share,
// incomplete-rate endpoints) — both stated once, after this table.
func vmCostByEndpoint(sec *SectionVM, rep *Report2, t i18n.CostText, cur string) costTotal {
	// Forwarded == 0: this endpoint never served a request (every attempt
	// failed), so it has no cost to attribute and its absence from the
	// total is not a pricing gap.
	epTot := costTotalOf(len(rep.EndpointsAll), func(i int) *float64 {
		if rep.EndpointsAll[i].CostEstimate == nil && rep.EndpointsAll[i].Forwarded == 0 {
			return skipRow
		}
		return rep.EndpointsAll[i].CostEstimate
	})
	if epTot.priced > 0 {
		tbl := &TableVM{Title: t.ByEndpointTitle(cur), Headers: t.ByEndpointHeaders[:]}
		for _, e := range rep.EndpointsAll {
			if e.CostEstimate != nil {
				tbl.row(e.Endpoint, fmtutil.FmtTokens(e.TokensInFresh), fmtutil.FmtTokens(e.TokensOut),
					fmtutil.FmtCurrency(*e.CostEstimate, cur))
			}
		}
		tbl.row(t.TotalLabel, "", "", fmtutil.FmtCurrency(epTot.sum, cur))
		if epTot.unpriced > 0 {
			tbl.note(t.UnpricedNote(epTot.unpriced, epTot.priced+epTot.unpriced, t.UnitEndpoints) + "\n\n")
		}
		// The degraded-estimate and incomplete-rate shares are only
		// recoverable per endpoint, so both caveats are stated once, here,
		// and apply to every table above and below.
		var degraded float64
		incomplete := 0
		for _, e := range rep.EndpointsAll {
			degraded += e.CostEstimateEst
			if e.CostRateIncomplete {
				incomplete++
			}
		}
		if degraded > 0 && epTot.sum > 0 {
			tbl.note(t.DegradedNote(degraded, degraded/epTot.sum*100, cur) + "\n\n")
		}
		if incomplete > 0 {
			tbl.note(t.IncompleteRateNote(incomplete) + "\n\n")
		}
		sec.Blocks = append(sec.Blocks, tbl)
	}
	return epTot
}

// vmCostByClient renders §2's per-client table and returns its totals.
func vmCostByClient(sec *SectionVM, rep *Report2, t i18n.CostText, cur string) costTotal {
	clientTot := costTotalOf(len(rep.ByClient), func(i int) *float64 { return rep.ByClient[i].CostEstimate })
	if clientTot.priced > 0 {
		tbl := &TableVM{Title: t.ByClientTitle(cur), Headers: t.ByClientHeaders[:]}
		for _, c := range rep.ByClient {
			if c.CostEstimate != nil {
				tbl.row(c.ClientKey, fmtutil.FmtTokens(c.TokensInFresh), fmtutil.FmtTokens(c.TokensOut),
					fmtutil.FmtCurrency(*c.CostEstimate, cur))
			}
		}
		tbl.row(t.TotalLabel, "", "", fmtutil.FmtCurrency(clientTot.sum, cur))
		if clientTot.unpriced > 0 {
			tbl.note(t.UnpricedNote(clientTot.unpriced, clientTot.priced+clientTot.unpriced, t.UnitClients) + "\n\n")
		}
		sec.Blocks = append(sec.Blocks, tbl)
	}
	return clientTot
}

// costTotal is one §2 table's totals-row inputs: the sum over rows that
// actually resolved a rate, and how many rows did and didn't.
type costTotal struct {
	sum              float64
	priced, unpriced int
}

// skipRow is costTotalOf's "this row belongs in neither count"
// sentinel — distinct from nil, which means "counted, and it has no
// price". Compared by pointer identity, so no real rate can ever collide
// with it.
var skipRow = new(float64)

// costTotalOf walks n rows through get: a rate pointer counts toward the
// total, nil counts as unpriced, skipRow counts as neither.
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
