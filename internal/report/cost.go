// Ver 2026-08-07, by Opus 5

// Package report provides cost calculation formulas and pricing resolvers for offline reports.
package report

import (
	"vmr/internal/pricing"
)

// costFor computes an estimated cost for a record using pricing.Rate.Cost.
// Falls back to estimating based on input/output token byte counts when exact usage was not reported.
func costFor(pr pricing.Rate, rc *rec2) (c float64, estimated bool) {
	if rc.usageOK {
		return pr.Cost(rc.usage.Fresh(), rc.usage.CacheRead, rc.usage.CacheWrite, rc.usage.Out), false
	}
	return pr.Cost(rc.estInFresh, 0, 0, rc.estOut), true
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
	c, estimated := costFor(pr, rc)
	addCost(&rep.Overall.CostEstimate, c)
	addCost(&mr.CostEstimate, c)
	addCost(&dr.CostEstimate, c)
	if ea := epsAll[rc.endpoint]; ea != nil {
		addCost(&ea.CostEstimate, c)
		if estimated {
			ea.CostEstimateEst += c
		}
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
