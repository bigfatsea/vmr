// Ver 2026-08-07, by Opus 5

// Quota-Aware Routing's router-side glue: metering a successful response
// against its provider's configured Limit (chargeQuota/tokenCharge) and
// reordering same-tier candidates by headroom score (reorderByQuota). Kept
// out of router.go on purpose — see internal/archtest's line-count budget
// for that file — so this feature's decision logic never has to compete
// with the failover loop for that budget. See
// docs/TokenPlan_Quota_Routing_Design_opus-5.md for the full design and
// docs/TokenPlan_Quota_P1_DevPlan_opus-5.md for what P1 actually delivers.
package router

import (
	"math"
	"sort"
	"time"

	"vmr/internal/core"
	"vmr/internal/pricing"
	"vmr/internal/quota"
	"vmr/internal/strategy"
)

// chargeQuota records one successful response's consumption against ep's
// provider quota, if any. Called once from forwardSuccess, after the body
// has finished streaming to the client — never for a failed/canceled/
// content-rejected attempt (see router.go's tryOne/handleErrorResponse:
// only forwardSuccess ever reaches here). A truncated response (the upstream
// died mid-stream but had already committed 2xx) still charges — the
// tokens already sent were genuinely consumed, and forwardSuccess always
// returns success=true for exactly this reason (see its own doc comment).
//
// nil-safe throughout, per the dev plan's §5.4 contract: rt.Quota==nil (no
// quota.Registry wired up — most tests, vmr diagnose), ep.Quota==nil (this
// endpoint's provider has no quota: configured), and an empty Limits slice
// are all silent no-ops, never a panic — a statistics helper must not be
// able to break a response that has already been written to the client.
func (rt *Router) chargeQuota(ep *core.Endpoint, rbody *respStream, creq *core.CanonicalRequest, now time.Time) {
	if rt.Quota == nil || ep.Quota == nil || len(ep.Quota.Limits) == 0 {
		return
	}
	// Exactly one Limit per provider (internal/config/quota.go's
	// validateQuota enforces this at load time) — no min()-across-Limits
	// merge yet; that's P3 (see the design doc's Core Algorithm section on
	// multi-window merging).
	l := ep.Quota.Limits[0]
	limitKey := string(l.Metric) + "/" + l.EveryText
	periodStart := quota.PeriodStart(l, now)

	if l.Metric == core.MetricCost {
		rt.chargeCost(ep, rbody, creq, limitKey, periodStart, now)
		return
	}

	var d quota.Counters
	var estimated int64
	switch l.Metric {
	case core.MetricRequests:
		d = quota.Counters{Requests: 1}
	case core.MetricTokens:
		d, estimated = tokenCharge(rbody, creq)
	default:
		// config validation only ever admits requests|tokens|cost; an
		// unreachable metric value here (e.g. a hand-built core.Limit in a
		// test) is a no-op, not a panic.
		return
	}
	// model_multipliers is only ever configured on a requests/tokens
	// account (config.validate() rejects it on a cost-only account — see
	// docs/TokenPlan_Quota_Routing_Design_opus-5.md's §4.2④), so this never
	// runs for the MetricCost branch above — deliberately: applyModelMultiplier
	// rebuilds a fresh quota.Counters that would silently zero out a Cost
	// field if it ever ran after chargeCost had set one.
	d, estimated = applyModelMultiplier(ep, d, estimated)
	rt.Quota.Charge(ep.Provider, limitKey, periodStart, d, estimated)
}

// chargeCost (P2.2) is chargeQuota's metric: cost path: meters the same raw
// token components tokenCharge always computes, prices them through
// ep.PricingRate at the CHARGE-TIME timestamp (pricing.RateAt — see its doc
// comment for why "the rate right now" and not a pre-resolved constant),
// and writes the resulting $ amount into Counters.Cost — computed once,
// here, and never recomputed later from raw tokens (see
// docs/TokenPlan_Quota_P2_DevPlan_opus-5.md's §1.4/§6.2: a price table that
// changes over time means only charge time can correctly answer "what was
// this worth").
func (rt *Router) chargeCost(ep *core.Endpoint, rbody *respStream, creq *core.CanonicalRequest, limitKey string, periodStart, now time.Time) {
	d, estimatedTokens := tokenCharge(rbody, creq)
	rate := pricing.RateAt(ep.PricingRate, now)
	d.Cost = componentCost(d, rate)
	rt.Quota.Charge(ep.Provider, limitKey, periodStart, d, estimatedTokens)
	if estimatedTokens != 0 {
		// The whole charge came from a degraded token estimate (see
		// tokenCharge's doc comment: its degraded path always sets
		// estimated to exactly Fresh+Out, never a partial value), so the
		// $ amount computed from it is entirely an estimate too.
		rt.Quota.AddEstimatedCost(ep.Provider, limitKey, periodStart, d.Cost)
	}
}

// componentCost prices d's four raw components through rate — see
// pricing.Rate.Cost for the shared formula (also used by
// internal/report/cost.go's costFor) and the nil-component/AllPathsComplete
// reasoning.
func componentCost(d quota.Counters, rate pricing.Rate) float64 {
	return rate.Cost(d.Fresh, d.CacheRead, d.CacheWrite, d.Out)
}

// applyModelMultiplier scales d (and its accompanying degraded-estimate
// marker) by ep's account-level model_multipliers, resolved for ep.Model —
// exact match first, "*" wildcard second, 1.0 (no scaling) otherwise. This
// MUST happen at charge time, not read time — see core.QuotaSpec's doc
// comment on ModelMultipliers for why: quota.Counters aggregates per
// provider, not per model, so once a charge lands there is no way to later
// recover which slice of a read came from which upstream model.
//
// Every component (including Requests) is scaled and rounded UP
// (math.Ceil): a non-integer multiplier (nothing in the design rules that
// out) must round toward "counts as more consumption", the safe direction —
// rounding down could let an already-multiplied-up model's calls be
// under-counted relative to the account's real usage.
func applyModelMultiplier(ep *core.Endpoint, d quota.Counters, estimated int64) (quota.Counters, int64) {
	mult := modelMultiplier(ep)
	if mult == 1.0 {
		return d, estimated
	}
	return quota.Counters{
		Fresh:      ceilScale(d.Fresh, mult),
		CacheRead:  ceilScale(d.CacheRead, mult),
		CacheWrite: ceilScale(d.CacheWrite, mult),
		Out:        ceilScale(d.Out, mult),
		Requests:   ceilScale(d.Requests, mult),
	}, ceilScale(estimated, mult)
}

// modelMultiplier resolves ep's charge-time scaling factor: an exact
// ep.Model match in ep.Quota.ModelMultipliers, else its "*" wildcard entry,
// else 1.0 (no scaling — the zero-config default, and also what an account
// with no model_multipliers configured at all gets, since ep.Quota.
// ModelMultipliers is then a nil map).
func modelMultiplier(ep *core.Endpoint) float64 {
	if ep.Quota == nil || len(ep.Quota.ModelMultipliers) == 0 {
		return 1.0
	}
	if m, ok := ep.Quota.ModelMultipliers[ep.Model]; ok {
		return m
	}
	if m, ok := ep.Quota.ModelMultipliers["*"]; ok {
		return m
	}
	return 1.0
}

// ceilScale multiplies v by mult and rounds up — see applyModelMultiplier's
// doc comment for why the rounding direction is deliberate, not incidental.
func ceilScale(v int64, mult float64) int64 {
	return int64(math.Ceil(float64(v) * mult))
}

// tokenCharge computes one response's token consumption: the upstream's own
// reported usage when respStream managed to sniff it (exact), degrading to
// a byte-count estimate when it didn't (opaque response, no usage field, or
// a stream truncated before any usage-bearing block arrived) — see the
// design doc's Metering section. estimated equals the full charged total
// exactly when this is a degraded estimate, 0 when it's exact — accumulated
// by quota.Registry into each account's running estimated_pct, the one
// signal /admin/status gives an operator for how much to trust a
// token-metered account's numbers.
func tokenCharge(rbody *respStream, creq *core.CanonicalRequest) (quota.Counters, int64) {
	if u, ok := rbody.Usage(); ok {
		return quota.Counters{Fresh: u.Fresh(), CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, Out: u.Out}, 0
	}
	// Degraded: request-side reuses the cheap pre-routing estimate every
	// request already has (creq.Facts.EstimatedTokens, computed once in
	// server/facts.go — zero extra cost here); response-side comes from
	// bytes respStream classified incrementally as they arrived (see
	// response.go's countBytes), through the exact same coefficients
	// core.EstimateTextTokens itself uses. Both are charged entirely to
	// Fresh/Out — the degraded path cannot tell cache hits apart, and
	// assuming none is the safe direction: it overestimates consumption
	// rather than silently crediting a cache discount that may not have
	// happened (see the design doc's Metering section).
	inEst := creq.Facts.EstimatedTokens
	ascii, wide := rbody.OutBytes()
	outEst := core.EstimateTokensFromCounts(ascii, wide)
	return quota.Counters{Fresh: inEst, Out: outEst}, inEst + outEst
}

// baseAmount applies base(metric) to a raw Counters value — requests: the
// count itself; tokens: spec.TokenWeights' four-component weighted sum
// (token_weights all 1.0 — core.DefaultTokenWeight — is the zero-config
// default, so an unconfigured account gets P1's plain equal-weighted sum
// back exactly). Shared by QuotaStatus (below, for /admin/status) and
// reorderByQuota (decision time) so "how much has this account used, in its
// own metric's unit" has exactly one formula.
//
// Reads the metric off spec.Limits[0] rather than taking it as a separate
// parameter: P1/P2.1 both guarantee exactly one Limit per provider (see
// chargeQuota's own comment), so spec already carries it — every call site
// already has spec in hand (ep.Quota) at the point it used to look up l.Metric
// itself. spec must be non-nil; every call site is already guarded by an
// ep.Quota==nil / len(Limits)==0 check upstream (chargeQuota, QuotaStatus,
// scoreForEndpoint).
func baseAmount(spec *core.QuotaSpec, c quota.Counters) float64 {
	switch spec.Limits[0].Metric {
	case core.MetricRequests:
		return float64(c.Requests)
	case core.MetricTokens:
		w := spec.TokenWeights
		return float64(c.Fresh)*w.InFresh + float64(c.CacheRead)*w.CacheRead +
			float64(c.CacheWrite)*w.CacheWrite + float64(c.Out)*w.Out
	case core.MetricCost:
		// Counters.Cost is already the final $ amount, computed once at
		// charge time (see chargeCost) — never re-derived here from raw
		// tokens, which is the whole point of storing it pre-computed (see
		// core.Counters' doc comment on why cost is the one exception to
		// "store raw, weight on read").
		return c.Cost
	default:
		return 0
	}
}

// QuotaProviderStatus is one quota-configured provider's live state, for
// /admin/status's quota section and `vmr status`. Fresh/CacheRead/
// CacheWrite/Out/Requests are the raw stored components (see
// quota.Counters) — exposed alongside the already-weighted Used/Pct so a
// user with a high-cache-hit-rate account can see, on day one, how far
// P1's equal-weighted accounting diverges from what a real Credits-style
// bill would charge (see the design doc's Metering section on why this
// divergence exists and why surfacing the breakdown is the cheap mitigation
// for it in P1, ahead of P2's token_weights/cost metric).
type QuotaProviderStatus struct {
	Provider     string    `json:"provider"`
	Metric       string    `json:"metric"`
	Every        string    `json:"every"`
	Amount       float64   `json:"amount"`
	Used         float64   `json:"used"` // base(metric) already applied — directly comparable to Amount
	Pct          float64   `json:"pct"`  // Used/Amount*100, not clamped — can exceed 100 for an over-quota bucket
	Headroom     float64   `json:"headroom"`
	PeriodStart  time.Time `json:"period_start"`
	PeriodEndsAt time.Time `json:"period_ends_at"`
	// EstimatedPct is 0 for metric=requests (always exact) and for a
	// tokens/cost account whose usage has been fully sniffed from upstream
	// responses — otherwise the percentage of this period's consumption
	// that came from the degraded byte-count fallback instead (see
	// tokenCharge). The one signal an operator has for how much to trust
	// Used/Pct for this account. Computed against the matching unit for
	// each metric (raw tokens for tokens, money for cost — see
	// QuotaStatus), NOT against Used, which has base(metric) applied.
	EstimatedPct float64 `json:"estimated_pct"`
	Fresh        int64   `json:"fresh"`
	CacheRead    int64   `json:"cache_read"`
	CacheWrite   int64   `json:"cache_write"`
	Out          int64   `json:"out"`
	Requests     int64   `json:"requests"`

	// TokenWeights/ModelMultipliers surface the account-level modifiers
	// currently in effect (P2.1) — the design doc's Observability section
	// calls this out explicitly: "a promotional multiplier left configured
	// after it expired is a foreseeable failure mode, and showing the
	// effective value is the cheapest guard against it." TokenWeights is
	// always populated (defaults to all core.DefaultTokenWeight when
	// unconfigured); ModelMultipliers is omitted entirely when the account
	// has none configured.
	TokenWeights     TokenWeightsView   `json:"token_weights"`
	ModelMultipliers map[string]float64 `json:"model_multipliers,omitempty"`
}

// TokenWeightsView is core.TokenWeights with JSON tags — kept separate from
// core.TokenWeights itself so internal/core stays free of any
// presentation/encoding concern (see CLAUDE.md's module map: core is "shared
// types", not a serialization layer).
type TokenWeightsView struct {
	InFresh    float64 `json:"in_fresh"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Out        float64 `json:"out"`
}

// QuotaStatus reports every quota-configured provider's live state, sorted
// by provider name for a stable /admin/status/`vmr status` rendering.
// nil-safe: returns nil when no quota.Registry is wired up (rt.Quota==nil)
// or no Snapshot has been installed yet — same "absent, not a zeroed row"
// contract server/admin.go's reloadBlock already uses for an analogous
// "nothing to report yet" case.
//
// P1 has exactly one Limit per provider, so this reports that Limit
// directly rather than reducing across several — the same simplification
// chargeQuota makes (see its own doc comment).
func (rt *Router) QuotaStatus() []QuotaProviderStatus {
	if rt.Quota == nil {
		return nil
	}
	snap := rt.Snapshot()
	if snap == nil {
		return nil
	}
	now := time.Now()
	seen := map[string]bool{}
	var out []QuotaProviderStatus
	for _, byName := range snap.Models {
		for _, route := range byName {
			for _, ep := range route.Endpoints {
				if ep.Quota == nil || len(ep.Quota.Limits) == 0 || seen[ep.Provider] {
					continue
				}
				seen[ep.Provider] = true
				l := ep.Quota.Limits[0]
				limitKey := string(l.Metric) + "/" + l.EveryText
				periodStart := quota.PeriodStart(l, now)
				periodEnd := quota.PeriodEnd(l, now)
				c, estimated := rt.Quota.Used(ep.Provider, limitKey, periodStart)
				used := baseAmount(ep.Quota, c)
				var pct, estPct float64
				if l.Amount > 0 {
					pct = used / l.Amount * 100
				}
				// Every metric's estimate share is a ratio of two
				// numbers in the SAME unit — which is not `used` for
				// either non-requests metric, since `used` has already
				// had base(metric) applied to it:
				//   cost   — `estimated` is a raw token count; the $
				//            estimate share lives in EstimatedCost.
				//   tokens — `estimated` is the raw (unweighted) token
				//            count charged by tokenCharge's degraded
				//            path, while `used` is the token_weights-
				//            weighted sum. Dividing one by the other
				//            reports a wrong share the moment any weight
				//            isn't 1.0 (a 5x `out` weight would report a
				//            fully-estimated period as 20% estimated).
				//            The raw four-component total is the matching
				//            denominator — both sides are then the same
				//            unweighted (but equally model_multiplier-
				//            scaled) token count.
				switch l.Metric {
				case core.MetricCost:
					if used > 0 {
						estPct = rt.Quota.EstimatedCostFor(ep.Provider, limitKey, periodStart) / used * 100
					}
				case core.MetricTokens:
					if rawTokens := float64(c.Fresh + c.CacheRead + c.CacheWrite + c.Out); rawTokens > 0 {
						estPct = float64(estimated) / rawTokens * 100
					}
				}
				out = append(out, QuotaProviderStatus{
					Provider: ep.Provider, Metric: string(l.Metric), Every: l.EveryText, Amount: l.Amount,
					Used: used, Pct: pct, Headroom: quota.ScoreForLimit(l, used, now),
					PeriodStart: periodStart, PeriodEndsAt: periodEnd, EstimatedPct: estPct,
					Fresh: c.Fresh, CacheRead: c.CacheRead, CacheWrite: c.CacheWrite, Out: c.Out, Requests: c.Requests,
					TokenWeights: TokenWeightsView{
						InFresh: ep.Quota.TokenWeights.InFresh, CacheRead: ep.Quota.TokenWeights.CacheRead,
						CacheWrite: ep.Quota.TokenWeights.CacheWrite, Out: ep.Quota.TokenWeights.Out,
					},
					ModelMultipliers: ep.Quota.ModelMultipliers,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

// reorderByQuota reorders cands in place: within each tier that dims'
// Dimension chain considers equal (a full tie across every Dimension.
// Compare — priority, or any future dimension), quota-bearing endpoints
// move to the front of that tier in descending headroom-score order;
// endpoints with no quota configured keep their exact position. Called from
// Serve right after strategy.Sort and before the Sticky Model block — see
// router.go's Serve. Reports whether the very first candidate actually
// changed, purely for X-VMR-Route-Reason's pick=quota marker (routehdr.go).
//
// Two invariants this function exists to preserve (see the design doc's
// Scheduling Flow section):
//  1. it never reorders ACROSS tiers — priority's ordering intent, or any
//     other Dimension a future release adds, is never crossed by a quota
//     decision;
//  2. it never touches a candidate with no quota: configured — so
//     configuring quota for one provider can never move an unrelated
//     provider's position (the "占位重排" placeholder-reorder rule).
//
// nil-safe: reg==nil (no quota.Registry wired up) or an empty cands slice
// both return false without touching cands at all.
func reorderByQuota(cands []*core.Endpoint, dims []strategy.Dimension, reg *quota.Registry, now time.Time) bool {
	if reg == nil || len(cands) == 0 {
		return false
	}
	front := cands[0]
	for i := 0; i < len(cands); {
		j := i + 1
		for j < len(cands) && sameTier(cands[i], cands[j], dims) {
			j++
		}
		reorderTier(cands[i:j], reg, now)
		i = j
	}
	return cands[0] != front
}

// sameTier reports whether a and b are a full tie across every Dimension in
// dims — dims==nil (only reachable via a hand-built ModelRoute in a test;
// config.applyDefaults always fills an empty strategy with ["priority"])
// ties everything into one tier, matching strategy.Sort's own behavior for
// an empty dims chain.
func sameTier(a, b *core.Endpoint, dims []strategy.Dimension) bool {
	for _, d := range dims {
		if d.Compare(a, b) != 0 {
			return false
		}
	}
	return true
}

// reorderTier reorders one tier in place: the subset of tier that has
// ep.Quota configured is sorted by headroom score descending (stable — a
// score tie keeps config-file relative order, same tie-break Sort itself
// uses) and written back into exactly the slot positions that subset
// already occupied; every other slot is untouched. Fewer than two
// quota-bearing members means there is nothing to reorder — 0 or 1 element
// is trivially already in the only order it can be.
func reorderTier(tier []*core.Endpoint, reg *quota.Registry, now time.Time) {
	var idxs []int
	var eps []*core.Endpoint
	var scores []float64
	for idx, ep := range tier {
		if ep.Quota == nil || len(ep.Quota.Limits) == 0 {
			continue
		}
		idxs = append(idxs, idx)
		eps = append(eps, ep)
		scores = append(scores, scoreForEndpoint(ep, reg, now))
	}
	if len(eps) < 2 {
		return
	}
	order := make([]int, len(eps))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return scores[order[a]] > scores[order[b]] })
	for slot, pos := range order {
		tier[idxs[slot]] = eps[pos]
	}
}

// scoreForEndpoint reads ep's provider's current consumption and computes
// its headroom score — a read, but Registry.Used still performs the same
// lazy period-reset Charge does (see quota.Registry's doc comment), so a
// score computed just after a period boundary reflects a freshly-zeroed
// bucket even if nothing has charged into the new period yet.
func scoreForEndpoint(ep *core.Endpoint, reg *quota.Registry, now time.Time) float64 {
	l := ep.Quota.Limits[0] // P1: exactly one Limit per provider (see chargeQuota's own comment)
	limitKey := string(l.Metric) + "/" + l.EveryText
	c, _ := reg.Used(ep.Provider, limitKey, quota.PeriodStart(l, now))
	return quota.ScoreForLimit(l, baseAmount(ep.Quota, c), now)
}
