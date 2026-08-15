// Ver 2026-08-07, by Opus 5

// §2 成本估算's per-record cost formula and endpoint-label parsing — split
// out of aggregate.go (P2.2) purely to keep that file under
// internal/archtest's line budget; aggState.ingestRecord (aggregate.go) is
// the only caller.
package report

import (
	"strings"

	"vmr/internal/pricing"
)

// costFor computes one record's estimated cost from its endpoint's
// resolved rate via pricing.Rate.Cost (shared with
// internal/router/quota.go's componentCost) — all four components,
// INCLUDING cache_read (P2.2 fix: the pre-P2.2 sidecar deliberately left
// cache_read out of this formula because the four providers in the repo's
// own sample pricing.yaml all happened to price it at 0; that was a fact
// about those four rows, not a general truth — the design doc's own market
// survey found cache-read pricing 5-120x cheaper than fresh input, never
// actually free, across providers broadly. Excluding it systematically
// UNDERSTATES cost for any provider/model priced above 0, the dangerous
// direction (see docs/VirtualModelRouter_Design_v4_Quota.md's market-data
// section for the full argument).
//
// When usage wasn't sniffed (!rc.usageOK), this prices the SAME degraded
// byte-count estimate (rc.estInFresh/rc.estOut) internal/router/quota.go's
// tokenCharge degraded branch charges — Fresh/Out only, no cache
// components, for the same reason the router's degraded branch has none:
// it cannot tell cache hits apart from an unparseable response. Returning a
// real (if approximate) amount here instead of a hardcoded 0 is what fixes
// the "false zero": before this, a provider whose entire window had
// unsniffable usage rendered a misleadingly precise $0.0000 instead of
// either a real number or an honest "-". estimated reports whether c came
// from this degraded path, the same signal TokensEstimated/
// TokensInFreshEst/TokensOutEst already carry for the tokens metric.
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
	provider, model := splitEndpointProviderModel(rc.endpoint)
	pr, ok := pricingSrc.RateFor(provider, model)
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

// splitEndpointProviderModel splits a "protocol:provider:model" endpoint
// label into its provider and model segments — pricing is keyed by
// provider+model only, protocol-agnostic (see internal/pricing's package
// doc comment). SplitN(…, 3) rather than a plain Split: a real-world model
// name can itself contain ":" or "/" (e.g. OpenRouter's "z-ai/glm-5.2"), so
// this only ever isolates the first two colon-separated segments and
// leaves the third — the model — exactly as-is, whatever it contains.
func splitEndpointProviderModel(endpoint string) (provider, model string) {
	parts := strings.SplitN(endpoint, ":", 3)
	if len(parts) < 3 {
		return "", ""
	}
	return parts[1], parts[2]
}
