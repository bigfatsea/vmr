// Ver 2026-08-07, by Opus 5

// §2 成本估算's per-record cost formula and endpoint-label parsing — split
// out of aggregate.go (P2.2) purely to keep that file under
// internal/archtest's line budget; buildInternal's aggregation loop is the
// only caller.
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
// direction (see docs/TokenPlan_Quota_Routing_Design_opus-5.md's market-data
// section for the full argument).
func costFor(pr pricing.Rate, rc *rec2) float64 {
	if !rc.usageOK {
		return 0
	}
	return pr.Cost(rc.usage.Fresh(), rc.usage.CacheRead, rc.usage.CacheWrite, rc.usage.Out)
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
