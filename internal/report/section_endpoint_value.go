// Ver 2026-08-01, by Sonnet 5

// §6.6 端点性价比: not "what did this endpoint cost" (§2 already answers
// that) but "what did it cost per unit of work delivered, and what did its
// failures cost in time". A cheap endpoint that fails a third of the time
// is not cheap — but nothing in a per-endpoint spend column shows that,
// because the spend lands on whichever endpoint eventually succeeded.
//
// Every figure here is derived at render time from fields §2 and §3 already
// carry; the only thing this feature had to collect is EndpointRow.WastedMS.
// That is deliberate — a derived metric that disagrees with the totals it
// was derived from is a reporting bug waiting to happen.
package report

import (
	"fmt"
	"sort"
	"strconv"

	"vmr/internal/i18n"
)

// valueRow is the rendered form of one endpoint's efficiency, computed
// once so the table body stays a formatting exercise.
type valueRow struct {
	endpoint     string
	costPer1MOut float64
	costPerReq   float64
	hasCost      bool
	tokensOut    int64
	requestsOK   int
	failed       int
	availability float64 // 0..1 fraction, NOT 0..100 — EndpointRow.ErrorRate is the other convention
	wastedMS     int64
}

func renderEndpointValue(w func(string, ...any), rep *Report2, lang i18n.Lang) {
	rows := endpointValueRows(rep)
	if len(rows) == 0 {
		return
	}
	t := i18n.EndpointValue(lang)
	w("## %s\n\n", t.Title)

	cur := ""
	priced := rep.Pricing != nil
	if priced {
		cur = " (" + rep.Pricing.Currency + ")"
	}
	if priced {
		w("%s", t.IntroPriced)
	} else {
		w("%s", t.IntroUnpriced)
	}

	headers := append([]string(nil), t.BaseHeaders[:]...)
	if priced {
		ph := t.PricedHeaders(cur)
		headers = append(headers, ph[0], ph[1])
	}
	tail := t.TailHeaders
	headers = append(headers, tail[0], tail[1], tail[2])
	tbl := newTable(w, headers...)

	for _, r := range rows {
		cells := []string{r.endpoint, strconv.Itoa(r.requestsOK), fmtTokens(r.tokensOut)}
		if priced {
			if r.hasCost {
				cells = append(cells, fmt.Sprintf("%.4f", r.costPer1MOut), fmt.Sprintf("%.4f", r.costPerReq))
			} else {
				cells = append(cells, "-", "-")
			}
		}
		cells = append(cells, strconv.Itoa(r.failed), pctStr(r.availability), fmtDurMS(r.wastedMS))
		tbl.row(cells...)
	}
	w("\n")
	w("%s", t.WastedNote)
	w("%s", t.NoMoneyNote1)
	w("%s", t.NoMoneyNote2)
	if priced {
		w("%s", t.PricedCompareNote)
	}
	w("\n")
}

// endpointValueRows builds the sorted body. Sort key: cheapest per unit of
// output first when pricing is available (that is the question the section
// exists to answer), else most wasted time first.
func endpointValueRows(rep *Report2) []valueRow {
	var out []valueRow
	for _, e := range rep.EndpointsAll {
		// An endpoint that never served a request has no unit of work to
		// divide by; its failures still show up in §3 端点健康.
		if e.RequestsOK == 0 && e.WastedMS == 0 {
			continue
		}
		r := valueRow{
			endpoint: e.Endpoint, tokensOut: e.TokensOut, requestsOK: e.RequestsOK,
			failed: e.Failed, availability: e.Availability, wastedMS: e.WastedMS,
		}
		if e.CostEstimate != nil && *e.CostEstimate > 0 {
			if e.TokensOut > 0 {
				r.costPer1MOut = *e.CostEstimate / float64(e.TokensOut) * 1e6
				r.hasCost = true
			}
			if e.RequestsOK > 0 {
				r.costPerReq = *e.CostEstimate / float64(e.RequestsOK)
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.hasCost != b.hasCost {
			return a.hasCost // priced rows first: they carry the comparison
		}
		if a.hasCost && a.costPer1MOut != b.costPer1MOut {
			return a.costPer1MOut < b.costPer1MOut
		}
		if a.wastedMS != b.wastedMS {
			return a.wastedMS > b.wastedMS
		}
		return a.endpoint < b.endpoint
	})
	return out
}
