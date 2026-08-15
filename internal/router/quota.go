// Ver 2026-08-07, by Opus 5

// Quota-Aware Routing's router-side glue: metering a successful response
// against its provider's configured Limit (chargeQuota/tokenCharge) and
// reordering same-tier candidates by headroom score (reorderByQuota). Kept
// out of router.go on purpose — see internal/archtest's line-count budget
// for that file — so this feature's decision logic never has to compete
// with the failover loop for that budget. See
// docs/VirtualModelRouter_Design_v4_Quota.md for the full design and its
// "现状与后续计划" section for what's actually shipped.
package router

import (
	"sort"
	"time"

	"vmr/internal/chatmsg"
	"vmr/internal/core"
	"vmr/internal/pricing"
	"vmr/internal/quota"
	"vmr/internal/respnorm"
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
func (rt *Router) chargeQuota(ep *core.Endpoint, rbody respnorm.NormalizerStream, creq *core.CanonicalRequest, now time.Time) {
	if rt.Quota == nil || ep.Quota == nil || len(ep.Quota.Limits) == 0 {
		return
	}
	// Exactly one Limit per provider (internal/config/quota.go's
	// validateQuota enforces this at load time) — no min()-across-Limits
	// merge yet (see the design doc's Core Algorithm section on
	// multi-window merging).
	var raw quota.Counters
	var estimated float64
	if ep.Quota.Limits[0].Metric != core.MetricRequests {
		raw, estimated = tokenCharge(rbody, creq)
	}
	ChargeResponse(rt.Quota, ep, raw, estimated, now)
}

// ChargeResponse charges ep's provider quota for one successful response,
// given its raw four-component token counters (ignored for metric:
// requests) and how much of them came from a degraded estimate (0 = exact).
// This is chargeQuota's metric dispatch + model-multiplier scaling + cost
// pricing tail, factored out so a caller that never streams through
// respnorm.NormalizerStream can drive the exact same pipeline instead of reimplementing
// it — currently `vmr replay` (internal/replay), which extracts usage from
// an already fully-buffered response via chatmsg.MergeUsageBytes; see
// docs/VirtualModelRouter_Design_v4_Quota.md's known-gap entry ② on
// `vmr replay` not charging quota. nil-safe: reg==nil or
// ep.Quota==nil/no Limits is a silent no-op, the same contract chargeQuota
// has always had.
//
// metric: cost prices raw through ep.PricingRate (pricing.EffectiveRate — a
// deterministic function of the resolved override chain, with no time
// dimension) and writes the resulting
// $ amount into Counters.Cost — computed once, here, and never recomputed
// later from raw tokens (the price table itself can still change across a
// config reload, which produces a new ep.PricingRate — recomputing from raw
// tokens later would silently re-price a past charge at today's rate).
// model_multipliers is only ever configured on a requests/tokens account
// (config.validate() rejects it on a cost-only account — see the design
// doc's §4.2④), so applyModelMultiplier never runs on the cost path —
// deliberately: it rebuilds a fresh quota.Counters that would silently zero
// out the Cost field this branch just set.
func ChargeResponse(reg *quota.Registry, ep *core.Endpoint, raw quota.Counters, estimated float64, now time.Time) {
	if reg == nil || ep.Quota == nil || len(ep.Quota.Limits) == 0 {
		return
	}
	l := ep.Quota.Limits[0]
	limitKey := string(l.Metric) + "/" + l.EveryText
	periodStart := quota.PeriodStart(l, now)
	switch l.Metric {
	case core.MetricRequests:
		d, est := quota.ApplyModelMultiplier(ep.Quota, ep.Model, quota.Counters{Requests: 1}, 0)
		reg.Charge(ep.Provider, limitKey, periodStart, d, est)
	case core.MetricCost:
		rate := pricing.EffectiveRate(ep.PricingRate)
		d := raw
		d.Cost = componentCost(d, rate)
		reg.Charge(ep.Provider, limitKey, periodStart, d, estimated)
		if estimated != 0 {
			// The whole charge came from a degraded token estimate (see
			// tokenCharge's doc comment: its degraded path always sets
			// estimated to exactly Fresh+Out, never a partial value), so
			// the $ amount computed from it is entirely an estimate too.
			reg.AddEstimatedCost(ep.Provider, limitKey, periodStart, d.Cost)
		}
	case core.MetricTokens:
		d, est := quota.ApplyModelMultiplier(ep.Quota, ep.Model, raw, estimated)
		reg.Charge(ep.Provider, limitKey, periodStart, d, est)
	}
	// config validation only ever admits requests|tokens|cost; an
	// unreachable metric value here (e.g. a hand-built core.Limit in a
	// test) is a no-op, not a panic.
}

// componentCost prices d's four raw components through rate — see
// pricing.Rate.Cost for the shared formula (also used by
// internal/report/cost.go's costFor) and the nil-component/Complete
// reasoning. d's components are converted back to int64 here: a
// metric: cost account can never have model_multipliers configured
// (config.validate rejects that combination — see ChargeResponse's own
// comment), so d is always the unscaled token counts tokenCharge produced,
// which are exact integers even though quota.Counters stores them as
// float64 to accommodate the requests/tokens accounts that DO scale.
func componentCost(d quota.Counters, rate pricing.Rate) float64 {
	return rate.Cost(int64(d.Fresh), int64(d.CacheRead), int64(d.CacheWrite), int64(d.Out))
}

// tokenCharge computes one response's token consumption: the upstream's own
// reported usage when respnorm.NormalizerStream managed to sniff it (exact),
// degrading to a byte-count estimate when it didn't (opaque response, no
// usage field, or a stream truncated before any usage-bearing block
// arrived) — see the design doc's Metering section. estimated equals the
// full charged total exactly when this is a degraded estimate, 0 when it's
// exact — accumulated by quota.Registry into each account's running
// estimated_pct, the one signal /admin/status gives an operator for how
// much to trust a token-metered account's numbers.
func tokenCharge(rbody respnorm.NormalizerStream, creq *core.CanonicalRequest) (quota.Counters, float64) {
	u, sniffed := rbody.Usage()
	// Request-side degraded estimate reuses the cheap pre-routing number every
	// request already has (creq.Facts.EstimatedTokens, computed once in
	// server/facts.go — zero extra cost here); response-side comes from bytes
	// respnorm classified incrementally as they arrived (respnorm.go's
	// countBytes), through the exact same coefficients core.EstimateTextTokens
	// itself uses. Both are computed unconditionally: they are two field reads
	// and an integer division, cheaper than branching around them, and
	// TokenCounters ignores them entirely when usage was sniffed.
	ascii, wide := rbody.OutBytes()
	return TokenCounters(u, sniffed, creq.Facts.EstimatedTokens, core.EstimateTokensFromCounts(ascii, wide))
}

// TokenCounters turns one response's usage into the raw four-component
// counters ChargeResponse charges, plus how much of that total came from a
// degraded estimate (0 when exact). sniffed reports whether u came from an
// actual upstream usage object; when it didn't, inEst/outEst — token
// estimates the caller derived however it can — are charged instead.
//
// Exported, and factored out of tokenCharge, because this exact-vs-degraded
// decision had grown THREE independent implementations: this one,
// internal/replay's chargeReplay, and internal/report's own reproduction of
// it for `vmr report`'s §2.5 recomputed column. Three copies of "if usage was
// sniffed charge it exactly, otherwise charge a byte estimate and mark the
// whole thing estimated" is the same class of drift risk the audit-log label
// format and quota.BaseAmount were each collapsed to one implementation to
// avoid — and it is specifically what cmd/vmr/quota_parity_test.go exists to
// catch, so that test must drive THIS function rather than re-deriving it.
//
// The degraded branch charges everything to Fresh/Out: it cannot tell cache
// hits apart, and assuming none is the safe direction — it overestimates
// consumption rather than silently crediting a cache discount that may not
// have happened (see the design doc's Metering section).
func TokenCounters(u chatmsg.Usage, sniffed bool, inEst, outEst int64) (quota.Counters, float64) {
	if sniffed {
		return quota.Counters{
			Fresh:      float64(u.Fresh()),
			CacheRead:  float64(u.CacheRead),
			CacheWrite: float64(u.CacheWrite),
			Out:        float64(u.Out),
		}, 0
	}
	return quota.Counters{Fresh: float64(inEst), Out: float64(outEst)}, float64(inEst + outEst)
}

// QuotaProviderStatus is one quota-configured provider's live state, for
// /admin/status's quota section and `vmr status`. Fresh/CacheRead/
// CacheWrite/Out/Requests are the raw stored components (see
// quota.Counters) — exposed alongside the already-weighted Used/Pct so a
// user with a high-cache-hit-rate account can see, on day one, how far
// P1's equal-weighted accounting diverges from what a real Credits-style
// bill would charge (see the design doc's Metering section on why this
// divergence exists and why surfacing the breakdown is the cheap mitigation
// ahead of token_weights/cost metrics).
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
	// Fresh/CacheRead/CacheWrite/Out/Requests mirror quota.Counters' fields
	// exactly, including its float64 type — an account with
	// model_multipliers configured stores the multiplier already folded in
	// (see quota.Counters' doc comment), so these can be fractional even
	// though "requests" reads like it should always be a whole number.
	Fresh      float64 `json:"fresh"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Out        float64 `json:"out"`
	Requests   float64 `json:"requests"`

	// TokenWeights/ModelMultipliers surface the account-level modifiers
	// currently in effect — the design doc's Observability section
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
				used := quota.BaseAmount(ep.Quota, c)
				var pct float64
				if l.Amount > 0 {
					pct = used / l.Amount * 100
				}
				estimatedCost := rt.Quota.EstimatedCostFor(ep.Provider, limitKey, periodStart)
				estPct := quota.EstimatedPct(l.Metric, c, estimated, estimatedCost)
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
// 1. it never reorders ACROSS tiers — priority's ordering intent, or any
// other Dimension a future release adds, is never crossed by a quota
// decision;
// 2. it never touches a candidate with no quota: configured — so
// configuring quota for one provider can never move an unrelated
// provider's position (the "占位重排" placeholder-reorder rule).
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
	return quota.ScoreForLimit(l, quota.BaseAmount(ep.Quota, c), now)
}
