// Ver 2026-08-13, by Opus 5

package quota

import (
	"math"

	"vmr/internal/core"
)

// BaseAmount applies base(metric) to a raw Counters value — requests: the
// count itself; tokens: spec.TokenWeights' four-component weighted sum
// (token_weights all 1.0 — core.DefaultTokenWeight — is the zero-config
// default, so an unconfigured account gets the plain equal-weighted sum
// back exactly); cost: Counters.Cost as-is (already the final $ amount,
// computed once at charge time — see core.Counters' doc comment on why cost
// is the one exception to "store raw, weight on read").
//
// Moved here from internal/router/quota.go (originally baseAmount) so both
// the router's decision path and a read-only offline consumer (vmr report's
// §2.5 quota-vs-consumption table) share exactly one formula — see
// docs/future-strategy/vmr_quota_visibility_devplan_opus-5.md's batch 1.
//
// spec must be non-nil with spec.Limits non-empty; every call site is
// already guarded by that check upstream (P1/P2.1 guarantee exactly one
// Limit per provider — see ChargeResponse's own comment).
func BaseAmount(spec *core.QuotaSpec, c Counters) float64 {
	switch spec.Limits[0].Metric {
	case core.MetricRequests:
		return float64(c.Requests)
	case core.MetricTokens:
		w := spec.TokenWeights
		return float64(c.Fresh)*w.InFresh + float64(c.CacheRead)*w.CacheRead +
			float64(c.CacheWrite)*w.CacheWrite + float64(c.Out)*w.Out
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
// Every component (including Requests) is scaled and rounded UP
// (math.Ceil): a non-integer multiplier (nothing in the design rules that
// out) must round toward "counts as more consumption", the safe direction —
// rounding down could let an already-multiplied-up model's calls be
// under-counted relative to the account's real usage.
func ApplyModelMultiplier(spec *core.QuotaSpec, model string, d Counters, estimated int64) (Counters, int64) {
	mult := modelMultiplier(spec, model)
	if mult == 1.0 {
		return d, estimated
	}
	return Counters{
		Fresh:      ceilScale(d.Fresh, mult),
		CacheRead:  ceilScale(d.CacheRead, mult),
		CacheWrite: ceilScale(d.CacheWrite, mult),
		Out:        ceilScale(d.Out, mult),
		Requests:   ceilScale(d.Requests, mult),
	}, ceilScale(estimated, mult)
}

// ceilScale multiplies v by mult and rounds up — see ApplyModelMultiplier's
// doc comment for why the rounding direction is deliberate, not incidental.
func ceilScale(v int64, mult float64) int64 {
	return int64(math.Ceil(float64(v) * mult))
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
//   - cost: c.Cost is already the final $ amount (Counters' one "store
//     pre-priced" exception — see its doc comment), so estimatedCost/c.Cost
//     is directly comparable.
//   - tokens: estimated is a raw (unweighted) token count, so it's divided
//     by the raw four-component token total, not by BaseAmount's
//     token_weights-weighted sum.
func EstimatedPct(metric core.QuotaMetric, c Counters, estimated int64, estimatedCost float64) float64 {
	switch metric {
	case core.MetricCost:
		if c.Cost > 0 {
			return estimatedCost / c.Cost * 100
		}
	case core.MetricTokens:
		if rawTokens := float64(c.Fresh + c.CacheRead + c.CacheWrite + c.Out); rawTokens > 0 {
			return float64(estimated) / rawTokens * 100
		}
	}
	return 0
}
