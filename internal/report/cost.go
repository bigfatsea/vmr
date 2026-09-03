// Ver 2026-08-07, by Opus 5

// Package report provides cost calculation formulas and pricing resolvers for offline reports.
package report

import (
	"vmr/internal/pricing"
)

// costFor computes an estimated cost for a record using pricing.Rate.Cost.
// Per-side basis, mirroring router.TokenCountersSides / router.ChargeResponse:
// a side the upstream reported is priced from its real value; a missing side
// falls back to the degraded estimate (In charged entirely to Fresh, Out
// max'd with the placeholder the usage object may still carry). estCost is
// the portion of c attributable to a degraded side only — 0 when both sides
// were sniffed, the full c when both degraded, and just the un-sniffed
// side's price when one side is real. It feeds EndpointRow.CostEstimateEst /
// WindowEstimatedPct, the operator's calibration signal: a request with real
// input usage and a degraded output side is ~1% estimated, not 100%.
func costFor(pr pricing.Rate, rc *rec2) (c, estCost float64) {
	var fresh, cacheRead, cacheWrite, out int64
	var eFresh, eOut int64 // components attributable to a degraded side
	if rc.usageInOK {
		fresh, cacheRead, cacheWrite = rc.usage.Fresh(), rc.usage.CacheRead, rc.usage.CacheWrite
	} else {
		fresh = rc.estInFresh
		eFresh = fresh
	}
	if rc.usageOutOK {
		out = rc.usage.Out
	} else {
		out = max(rc.usage.Out, rc.estOut)
		eOut = out
	}
	return pr.Cost(fresh, cacheRead, cacheWrite, out), pr.Cost(eFresh, 0, 0, eOut)
}

// accumulateCost prices rc against every CostEstimate bucket it applies to
// (Overall/ByModel/ByDate/EndpointsAll/ByClient) when pricingSrc resolves a
// rate for its endpoint — kept separate from aggState.ingestRecord
// (aggregate.go) so that method stays focused on fan-out, not per-bucket
// pricing detail.
func accumulateCost(rep *Report2, mr, dr *Row, epsAll map[string]*EndpointRow, byClient map[string]*ClientRow, pricingSrc *pricing.Resolver, rc *rec2) {
	if pricingSrc == nil || rc.endpoint == "" {
		return
	}
	// RateForEndpoint splits the label itself (strict ":", the same
	// split core.SplitEndpointLabel accepts plus the legacy "/" form
	// deliberately excluded here) — the one shared entry point story's
	// ComputeJourneyCost also prices through, so the two halves can't
	// drift on how a label becomes a provider+model. A legacy "/"-joined
	// label still resolves to nothing and is silently skipped, exactly as
	// before (see KNOWN_ISSUES: the strict split is a recorded non-fix).
	pr, ok := pricingSrc.RateForEndpoint(rc.endpoint)
	if !ok {
		return
	}
	c, estCost := costFor(pr, rc)
	addCost(&rep.Overall.CostEstimate, c)
	addCost(&mr.CostEstimate, c)
	addCost(&dr.CostEstimate, c)
	if ea := epsAll[rc.endpoint]; ea != nil {
		addCost(&ea.CostEstimate, c)
		ea.CostEstimateEst += estCost
		if !pr.Complete() {
			ea.CostRateIncomplete = true
		}
	}
	if rc.clientKey != "" {
		if cl := byClient[rc.clientKey]; cl != nil {
			addCost(&cl.CostEstimate, c)
		}
	}
}

// addCost adds c to *p, lazily allocating the pointer on first use — the
// nil-check-then-allocate-then-add pattern every CostEstimate bucket
// (Overall/ByModel/ByDate/EndpointsAll/ByClient) needs, factored out after
// a missing ByDate accumulation (a hand-written copy of this block that
// simply wasn't added) went unnoticed until a 2026-08-12 review caught it —
// a shared call site is harder to omit by accident than a fifth copy-paste.
func addCost(p **float64, c float64) {
	if *p == nil {
		*p = new(float64)
	}
	**p += c
}
