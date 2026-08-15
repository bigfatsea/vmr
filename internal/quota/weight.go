// Ver 2026-08-13, by Opus 5

package quota

import (
	"vmr/internal/core"
)

// BaseAmount applies base(metric) to a raw Counters value — requests: the
// count itself; tokens: spec.TokenWeights' four-component weighted sum
// (token_weights all 1.0 — core.DefaultTokenWeight — is the zero-config
// default, so an unconfigured account gets the plain equal-weighted sum
// back exactly); cost: Counters.Cost as-is (already the final $ amount,
// computed once at charge time — see quota.Counters' doc comment on why cost
// is the one exception to "store raw, weight on read").
//
// Moved here from internal/router/quota.go (originally baseAmount) so both
// the router's decision path and a read-only offline consumer (vmr report's
// §2.5 quota-vs-consumption table) share exactly one formula — see
// the quota design specification.
//
// spec must be non-nil with spec.Limits non-empty; every call site is
// already guarded by that check upstream (configuration guarantees exactly one
// Limit per provider — see ChargeResponse's own comment).
func BaseAmount(spec *core.QuotaSpec, c Counters) float64 {
	switch spec.Limits[0].Metric {
	case core.MetricRequests:
		return c.Requests
	case core.MetricTokens:
		w := spec.TokenWeights
		return c.Fresh*w.InFresh + c.CacheRead*w.CacheRead +
			c.CacheWrite*w.CacheWrite + c.Out*w.Out
	case core.MetricCost:
		return c.Cost
	default:
		return 0
	}
}

// modelMultiplier resolves spec's charge-time scaling factor for model: an
// exact match in spec.ModelMultipliers, else its "*" wildcard entry, else
// 1.0 (no scaling — the zero-config default, and also what an account with
// no model_multipliers configured at all gets, since spec.ModelMultipliers
// is then a nil map). spec may be nil (no quota: configured for this
// endpoint's provider), in which case this returns 1.0.
func modelMultiplier(spec *core.QuotaSpec, model string) float64 {
	if spec == nil || len(spec.ModelMultipliers) == 0 {
		return 1.0
	}
	if m, ok := spec.ModelMultipliers[model]; ok {
		return m
	}
	if m, ok := spec.ModelMultipliers["*"]; ok {
		return m
	}
	return 1.0
}

// ApplyModelMultiplier scales d (and its accompanying degraded-estimate
// marker) by spec's account-level model_multipliers, resolved for model —
// see modelMultiplier. This MUST happen at charge time, not read time — see
// core.QuotaSpec's doc comment on ModelMultipliers for why: Counters
// aggregates per provider, not per model, so once a charge lands there is
// no way to later recover which slice of a read came from which upstream
// model.
//
// Every component (including Requests) is scaled by exact multiplication —
// no rounding. A non-integer multiplier (e.g. 4.5) is deliberately not
// forced toward an integer: which direction an upstream provider's own
// billing rounds a fractional multiplier, if at all, isn't observable from
// here, so picking one (this package rounded up through 2026-08-13) is a
// guess dressed as a safety margin — and a bad one, since the "safe"
// direction compounds into a systematic overcharge with no fixed relation
// to the configured multiplier (2.5 → +20% per charge, 4.5 → +11.1%, while
// 2.9 → +3.4%, so nearby multiplier values produce wildly different bias).
// Counters is float64 for exactly this reason (see its doc comment) — with
// nowhere left that needs an integer, there is nothing to round.
func ApplyModelMultiplier(spec *core.QuotaSpec, model string, d Counters, estimated float64) (Counters, float64) {
	mult := modelMultiplier(spec, model)
	if mult == 1.0 {
		return d, estimated
	}
	return Counters{
		Fresh:      d.Fresh * mult,
		CacheRead:  d.CacheRead * mult,
		CacheWrite: d.CacheWrite * mult,
		Out:        d.Out * mult,
		Requests:   d.Requests * mult,
	}, estimated * mult
}

// EstimatedPct returns the percentage of this period's consumption that
// came from a degraded (non-usage-sniffed) estimate rather than real
// upstream usage — 0 for metric: requests (always exact) and for a
// tokens/cost account whose usage has been fully sniffed. Moved here from
// internal/router/quota.go's QuotaStatus (the only prior computation of
// this ratio) for the same "one formula, two independent consumers" reason
// BaseAmount was moved: a read-only offline consumer (vmr report's §2.5
// live-quota column) needs the exact same share router.QuotaStatus reports
// for /admin/status, not a re-derivation of it — see
// docs/VirtualModelRouter_Design_v4_Quota.md's "额度公式的唯一实现"
// decision row.
//
// estimated/estimatedCost and c must come from the SAME bucket read (quota.
// Registry.Used or a persisted quota.Bucket) — this deliberately does not
// take a pre-weighted "used" value as the denominator, because dividing an
// unweighted estimate by a base(metric)-weighted total reports the wrong
// share the instant any weight isn't 1.0 (see the two metric branches below
// for the matching-unit reasoning):
// - cost: c.Cost is already the final $ amount (Counters' one "store
// pre-priced" exception — see its doc comment), so estimatedCost/c.Cost
// is directly comparable.
// - tokens: estimated is a raw (unweighted) token count, so it's divided
// by the raw four-component token total, not by BaseAmount's
// token_weights-weighted sum.
func EstimatedPct(metric core.QuotaMetric, c Counters, estimated float64, estimatedCost float64) float64 {
	switch metric {
	case core.MetricCost:
		if c.Cost > 0 {
			return estimatedCost / c.Cost * 100
		}
	case core.MetricTokens:
		if rawTokens := c.Fresh + c.CacheRead + c.CacheWrite + c.Out; rawTokens > 0 {
			return estimated / rawTokens * 100
		}
	}
	return 0
}
