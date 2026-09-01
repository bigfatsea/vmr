// Ver 2026-08-22, by Sonnet 5

// Quota-Aware Routing's router-side glue: metering a successful response
// against its provider's configured Limit(s) (chargeQuota/tokenCharge) and
// reordering same-tier candidates by headroom score (reorderByQuota). Kept
// out of router.go on purpose — see internal/archtest's line-count budget
// for that file — so this feature's decision logic never has to compete
// with the failover loop for that budget. See
// docs/VirtualModelRouter_Design_v4_Quota.md for the full design and its
// "现状与后续计划" section for what's actually shipped.
package router

import (
	"sort"
	"strings"
	"time"

	"vmr/internal/chatmsg"
	"vmr/internal/core"
	"vmr/internal/pricing"
	"vmr/internal/quota"
	"vmr/internal/respnorm"
	"vmr/internal/strategy"
)

// applicableLimits filters limits down to the ones whose Scope covers
// model (quota.AppliesToModel) — the subset that actually constrains (and
// gets charged for) one specific endpoint. A provider's quota: block can
// carry Limits scoped to different models; an endpoint only ever interacts
// with the ones that include its own model.
func applicableLimits(limits []core.Limit, model string) []core.Limit {
	out := make([]core.Limit, 0, len(limits))
	for _, l := range limits {
		if quota.AppliesToModel(l, model) {
			out = append(out, l)
		}
	}
	return out
}

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
	var raw quota.Counters
	var estimated float64
	if needsTokenCharge(ep.Quota.Limits, ep.Model) {
		raw, estimated = tokenCharge(rbody, creq)
	}
	ChargeResponse(rt.Quota, ep, raw, estimated, now)
}

// needsTokenCharge reports whether any of limits that actually apply to
// model (see applicableLimits) needs the token/usage extraction tokenCharge
// performs — i.e. is metric: tokens or metric: cost. A provider whose only
// applicable Limit is metric: requests never needs it (zero extra cost —
// see tokenCharge's own doc comment on why that matters for the hot path).
func needsTokenCharge(limits []core.Limit, model string) bool {
	for _, l := range limits {
		if l.Metric != core.MetricRequests && quota.AppliesToModel(l, model) {
			return true
		}
	}
	return false
}

// ChargeResponse charges every one of ep's provider Limits that apply to
// ep.Model (see applicableLimits) for one successful response, given its
// raw four-component token counters (ignored for metric: requests) and how
// much of them came from a degraded estimate (0 = exact). This is
// chargeQuota's metric dispatch + model-multiplier scaling + cost pricing
// tail, factored out so a caller that never streams through
// respnorm.NormalizerStream can drive the exact same pipeline instead of
// reimplementing it — currently `vmr replay` (internal/replay), which
// extracts usage from an already fully-buffered response via
// chatmsg.MergeUsageBytes; see docs/VirtualModelRouter_Design_v4_Quota.md's
// known-gap entry ② on `vmr replay` not charging quota. nil-safe: reg==nil
// or ep.Quota==nil/no Limits is a silent no-op, the same contract
// chargeQuota has always had.
//
// metric: cost prices raw through ep.PricingRate (pricing.EffectiveRate — a
// deterministic function of the resolved override chain, with no time
// dimension) and writes the resulting $ amount into Counters.Cost —
// computed once per applicable Limit (never recomputed later from raw
// tokens: the price table itself can still change across a config reload,
// which produces a new ep.PricingRate — recomputing from raw tokens later
// would silently re-price a past charge at today's rate).
// model_multipliers is only ever configured on a requests/tokens Limit
// (config.validate() rejects it on a cost Limit — see LimitConfig.validate),
// so applyModelMultiplier never runs on the cost path — deliberately: it
// rebuilds a fresh quota.Counters that would silently zero out the Cost
// field this branch just set.
func ChargeResponse(reg *quota.Registry, ep *core.Endpoint, raw quota.Counters, estimated float64, now time.Time) {
	if reg == nil || ep.Quota == nil {
		return
	}
	for _, l := range applicableLimits(ep.Quota.Limits, ep.Model) {
		limitKey := quota.LimitKey(l, ep.Model)
		periodStart := quota.PeriodStart(l, now)
		switch l.Metric {
		case core.MetricRequests:
			d, est := quota.ApplyModelMultiplier(l, ep.Model, quota.Counters{Requests: 1}, 0)
			reg.Charge(ep.Provider, limitKey, periodStart, d, est)
		case core.MetricCost:
			rate := pricing.EffectiveRate(ep.PricingRate)
			d := raw
			d.Cost = componentCost(d, rate)
			// estimated is token-denominated; on a cost Limit the estimate
			// signal is money and is tracked via AddEstimatedCost below.
			// Passing the token figure into Charge's `estimated` param would
			// pollute bucket.Estimated (a requests/tokens-only accumulator)
			// with a meaningless number (B6).
			reg.Charge(ep.Provider, limitKey, periodStart, d, 0)
			if estimated != 0 {
				// The whole charge came from a degraded token estimate (see
				// tokenCharge's doc comment: its degraded path always sets
				// estimated to exactly Fresh+Out, never a partial value), so
				// the $ amount computed from it is entirely an estimate too.
				reg.AddEstimatedCost(ep.Provider, limitKey, periodStart, d.Cost)
			}
		case core.MetricTokens:
			d, est := quota.ApplyModelMultiplier(l, ep.Model, raw, estimated)
			reg.Charge(ep.Provider, limitKey, periodStart, d, est)
		}
		// config validation only ever admits requests|tokens|cost; an
		// unreachable metric value here (e.g. a hand-built core.Limit in a
		// test) is a no-op, not a panic.
	}
}

// componentCost prices d's four raw components through rate — see
// pricing.Rate.Cost for the shared formula (also used by
// internal/report/cost.go's costFor) and the nil-component/Complete
// reasoning. d's components are converted back to int64 here: a
// metric: cost Limit can never have model_multipliers configured
// (config.validate rejects that combination — see LimitConfig.validate's
// own comment), so d is always the unscaled token counts tokenCharge
// produced, which are exact integers even though quota.Counters stores
// them as float64 to accommodate the requests/tokens Limits that DO scale.
func componentCost(d quota.Counters, rate pricing.Rate) float64 {
	return rate.Cost(int64(d.Fresh), int64(d.CacheRead), int64(d.CacheWrite), int64(d.Out))
}

// tokenCharge computes one response's token consumption: the upstream's own
// reported usage when respnorm.NormalizerStream managed to sniff it (exact),
// degrading to a token estimate when it didn't (opaque response, no
// usage field, or a stream truncated before any usage-bearing block
// arrived) — see the design doc's Metering section. For opaque responses,
// rbody.OutTokens() returns 0 (compressed bytes cannot be estimated).
// estimated equals the full charged total exactly when this is a degraded estimate,
// 0 when it's exact — accumulated by quota.Registry into each account's running
// estimated_pct, the one signal /status gives an operator for how
// much to trust a token-metered account's numbers.
func tokenCharge(rbody respnorm.NormalizerStream, creq *core.CanonicalRequest) (quota.Counters, float64) {
	// Usage() ok alone is not the exact-vs-degraded signal: a stream
	// truncated after Anthropic's message_start has real INPUT usage but
	// only the ~1 placeholder output — billing that as exact would write
	// out≈1 into the ledger with estimated=0, poisoning estimated_pct, the
	// operator's only trust signal. The per-side flags decide (R46).
	u, _ := rbody.Usage()
	inSniffed, outSniffed := rbody.UsageSides()
	// Request-side degraded estimate reuses the cheap pre-routing number every
	// request already has (creq.Facts.EstimatedTokens, computed once in
	// server/facts.go — zero extra cost here); response-side comes from
	// rbody.OutTokens() (respnorm.go), which returns 0 for opaque responses.
	return TokenCountersSides(u, inSniffed, outSniffed, creq.Facts.EstimatedTokens, rbody.OutTokens())
}

// TokenCounters turns one response's COMPLETE usage into the raw four-
// component counters ChargeResponse charges, plus how much of that total
// came from a degraded estimate (0 when exact). sniffed reports whether u is
// a complete upstream usage object — BOTH sides of the ledger present; a
// caller that can only say "some usage was seen" must use TokenCountersSides
// instead, because partial usage (real input, placeholder output) billed as
// exact is precisely the failure TokenCountersSides exists to prevent.
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
func TokenCounters(u chatmsg.Usage, sniffed bool, inEst, outEst int64) (quota.Counters, float64) {
	return TokenCountersSides(u, sniffed, sniffed, inEst, outEst)
}

// TokenCountersSides is TokenCounters' side-aware form — the canonical
// implementation of the exact-vs-degraded rule. inSniffed/outSniffed report
// whether each side of the usage ledger was actually parsed; a missing side
// falls back to the caller's estimate for that side (max'd with whatever the
// usage object did claim — a placeholder must never beat real emitted-text
// evidence), and the estimated share is reported honestly as the portion of
// the charge that came from an estimate, never 0 just because SOME usage was
// seen. The degraded In side charges everything to Fresh: it cannot tell
// cache hits apart, and assuming none is the safe direction — it
// overestimates consumption rather than silently crediting a cache discount
// that may not have happened (see the design doc's Metering section).
func TokenCountersSides(u chatmsg.Usage, inSniffed, outSniffed bool, inEst, outEst int64) (quota.Counters, float64) {
	if inSniffed && outSniffed {
		return quota.Counters{
			Fresh:      float64(u.Fresh()),
			CacheRead:  float64(u.CacheRead),
			CacheWrite: float64(u.CacheWrite),
			Out:        float64(u.Out),
		}, 0
	}
	var c quota.Counters
	var est float64
	if inSniffed {
		c.Fresh = float64(u.Fresh())
		c.CacheRead = float64(u.CacheRead)
		c.CacheWrite = float64(u.CacheWrite)
	} else {
		c.Fresh = float64(inEst)
		est += float64(inEst)
	}
	if outSniffed {
		c.Out = float64(u.Out)
	} else {
		out := max(u.Out, outEst)
		c.Out = float64(out)
		est += float64(out)
	}
	return c, est
}

// QuotaProviderStatus is one (provider, Limit) pair's live state, for
// /status's quota section and `vmr status` — P3: one row per Limit,
// not per provider, now that a provider can carry more than one window
// (see docs/VirtualModelRouter_Design_v4_Quota.md's §5.2). Fresh/CacheRead/
// CacheWrite/Out/Requests are the raw stored components (see
// quota.Counters) — exposed alongside the already-weighted Used/Pct so a
// user with a high-cache-hit-rate account can see, on day one, how far
// equal-weighted accounting diverges from what a real Credits-style bill
// would charge.
type QuotaProviderStatus struct {
	Provider string `json:"provider"`
	Metric   string `json:"metric"`
	Every    string `json:"every"`
	// Models is this Limit's Scope — omitted when the Limit applies to
	// every model on the provider (the zero-config default).
	Models []string `json:"models,omitempty"`
	// Role is "bucket" or "gate" — see quota.BucketIndex/ScoreForLimits'
	// doc comments for the rule (longest tumbling period on the provider
	// wins bucket; every other Limit is a gate). Always "bucket" for a
	// provider with exactly one Limit — the P1/P2 shape.
	Role         string    `json:"role"`
	Amount       float64   `json:"amount"`
	Used         float64   `json:"used"`     // base(metric) already applied — directly comparable to Amount
	Pct          float64   `json:"pct"`      // Used/Amount*100, not clamped — can exceed 100 for an over-quota bucket
	Headroom     float64   `json:"headroom"` // this Limit's own raw headroom (§5.1) — NOT the provider's merged routing score, see quota.ScoreForLimits
	PeriodStart  time.Time `json:"period_start"`
	PeriodEndsAt time.Time `json:"period_ends_at"`
	// EstimatedPct is 0 for metric=requests (always exact) and for a
	// tokens/cost account whose usage has been fully sniffed from upstream
	// responses — otherwise the percentage of this period's consumption
	// that came from the degraded byte-count fallback instead (see
	// tokenCharge). The one signal an operator has for how much to trust
	// Used/Pct for this Limit. Computed against the matching unit for each
	// metric (raw tokens for tokens, money for cost — see QuotaStatus), NOT
	// against Used, which has base(metric) applied.
	EstimatedPct float64 `json:"estimated_pct"`
	// Fresh/CacheRead/CacheWrite/Out/Requests mirror quota.Counters' fields
	// exactly, including its float64 type — a Limit with model_multipliers
	// configured stores the multiplier already folded in (see
	// quota.Counters' doc comment), so these can be fractional even though
	// "requests" reads like it should always be a whole number.
	Fresh      float64 `json:"fresh"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Out        float64 `json:"out"`
	Requests   float64 `json:"requests"`

	// TokenWeights/ModelMultipliers surface this Limit's own modifiers
	// currently in effect — the design doc's Observability section calls
	// this out explicitly: "a promotional multiplier left configured after
	// it expired is a foreseeable failure mode, and showing the effective
	// value is the cheapest guard against it." TokenWeights is always
	// populated (defaults to all core.DefaultTokenWeight when
	// unconfigured); ModelMultipliers is omitted entirely when this Limit
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
// by provider name then window for a stable /status/`vmr status`
// rendering. nil-safe: returns nil when no quota.Registry is wired up
// (rt.Quota==nil) or no Snapshot has been installed yet — same "absent, not
// a zeroed row" contract server/admin.go's reloadBlock already uses for an
// analogous "nothing to report yet" case.
//
// One row per Limit for a shared Limit; one row per actually-charged model
// for a per-model Limit (quota.PerModel) — see quotaStatusRowsForProvider's
// doc comment for why the latter can't be enumerated from config alone.
func (rt *Router) QuotaStatus() []QuotaProviderStatus {
	if rt.Quota == nil {
		return nil
	}
	snap := rt.Snapshot()
	if snap == nil {
		return nil
	}
	now := time.Now()
	// ep.Quota is the SAME *core.QuotaSpec pointer shared by every endpoint
	// of a given provider (BuildQuotaSpecs builds exactly one per provider
	// name) — deduping at the provider level, rather than per (provider,
	// Limit) as before, is both simpler and still correct: there is only
	// ever one Limits slice per provider to walk.
	seenProvider := map[string]bool{}
	var out []QuotaProviderStatus
	for _, byName := range snap.Models {
		for _, route := range byName {
			for _, ep := range route.Endpoints {
				if ep.Quota == nil || len(ep.Quota.Limits) == 0 || seenProvider[ep.Provider] {
					continue
				}
				seenProvider[ep.Provider] = true
				out = append(out, quotaStatusRowsForProvider(rt.Quota, ep.Provider, ep.Quota.Limits, now)...)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if a.Every != b.Every {
			return a.Every < b.Every
		}
		if a.Metric != b.Metric {
			return a.Metric < b.Metric
		}
		// Several rows can now share (Provider, Every, Metric) — every
		// per-model Limit's live buckets do — so the specific model each
		// row is about is the final tie-break, for a deterministic order
		// across runs.
		return strings.Join(a.Models, ",") < strings.Join(b.Models, ",")
	})
	return out
}

// quotaStatusRowsForProvider renders every QuotaProviderStatus row for one
// provider's Limits. A shared Limit (quota.PerModel false) always produces
// exactly one row, same as P1/P2. A per-model Limit's row count can't be
// derived from its declared Scope — a wildcard's membership is open-ended,
// and even a restricted list only says which models COULD have a bucket,
// not which ones actually do — so this walks the Registry's actual live
// keys (reg.Keys) and renders one row per key that quota.ExtractModel
// recognizes as belonging to l.
func quotaStatusRowsForProvider(reg *quota.Registry, provider string, limits []core.Limit, now time.Time) []QuotaProviderStatus {
	var liveKeys []string
	for _, l := range limits {
		if quota.PerModel(l) {
			liveKeys = reg.Keys(provider)
			break
		}
	}
	var out []QuotaProviderStatus
	for _, l := range limits {
		if !quota.PerModel(l) {
			role := limitRoleForModel(limits, l, "")
			out = append(out, quotaStatusRow(reg, provider, l, "", role, now))
			continue
		}
		for _, key := range liveKeys {
			model, ok := quota.ExtractModel(l, key)
			if !ok {
				continue
			}
			role := limitRoleForModel(limits, l, model)
			out = append(out, quotaStatusRow(reg, provider, l, model, role, now))
		}
	}
	return out
}

// limitRoleForModel determines whether l acts as the bucket or a gate for
// model, mirroring the exact applicableLimits + BucketIndex logic
// scoreForEndpoint uses during routing. model is "" for a shared Limit.
func limitRoleForModel(limits []core.Limit, l core.Limit, model string) string {
	app := limits
	if model != "" {
		app = applicableLimits(limits, model)
	}
	bi := quota.BucketIndex(app)
	if bi < 0 || bi >= len(app) {
		return "gate"
	}
	bucket := app[bi]
	if bucket.Metric == l.Metric && bucket.EveryText == l.EveryText && bucket.Amount == l.Amount && bucket.Since.Equal(l.Since) {
		return "bucket"
	}
	return "gate"
}

// quotaStatusRow builds one QuotaProviderStatus row for l. model is ""
// for a shared Limit (quota.LimitKey ignores it) or the specific model a
// per-model row is about — which also becomes the row's Models field, in
// place of l's own declared Scope: "which model is THIS row" is the useful
// answer here, not "which models could this Limit ever cover".
func quotaStatusRow(reg *quota.Registry, provider string, l core.Limit, model, role string, now time.Time) QuotaProviderStatus {
	limitKey := quota.LimitKey(l, model)
	periodStart := quota.PeriodStart(l, now)
	periodEnd := quota.PeriodEnd(l, now)
	c, estimated := reg.Used(provider, limitKey, periodStart)
	used := quota.BaseAmount(l, c)
	var pct float64
	if l.Amount > 0 {
		pct = used / l.Amount * 100
	}
	estimatedCost := reg.EstimatedCostFor(provider, limitKey, periodStart)
	estPct := quota.EstimatedPct(l.Metric, c, estimated, estimatedCost)
	models := l.Models
	if model != "" {
		models = []string{model}
	}
	return QuotaProviderStatus{
		Provider: provider, Metric: string(l.Metric), Every: l.EveryText, Models: models, Role: role,
		Amount: l.Amount, Used: used, Pct: pct, Headroom: quota.ScoreForLimit(l, used, now),
		PeriodStart: periodStart, PeriodEndsAt: periodEnd, EstimatedPct: estPct,
		Fresh: c.Fresh, CacheRead: c.CacheRead, CacheWrite: c.CacheWrite, Out: c.Out, Requests: c.Requests,
		TokenWeights: TokenWeightsView{
			InFresh: l.TokenWeights.InFresh, CacheRead: l.TokenWeights.CacheRead,
			CacheWrite: l.TokenWeights.CacheWrite, Out: l.TokenWeights.Out,
		},
		ModelMultipliers: l.ModelMultipliers,
	}
}

// reorderByQuota reorders cands in place: within each tier that dims'
// Dimension chain considers equal (a full tie across every Dimension.
// Compare — priority, or any future dimension), quota-bearing endpoints
// move to the front of that tier in descending headroom-score order;
// endpoints with no quota configured (or whose provider's quota has no
// Limit applicable to that endpoint's own model — see applicableLimits)
// keep their exact position. Called from Serve right after strategy.Sort
// and before the Sticky Model block — see router.go's Serve. Reports
// whether the very first candidate actually changed, purely for
// X-VMR-Route-Reason's pick=quota marker (routehdr.go).
//
// Two invariants this function exists to preserve (see the design doc's
// Scheduling Flow section):
// 1. it never reorders ACROSS tiers — priority's ordering intent, or any
// other Dimension a future release adds, is never crossed by a quota
// decision;
// 2. it never touches a candidate with no applicable quota Limit — so
// configuring quota for one provider (or scoping a Limit to specific
// models) can never move an unrelated candidate's position (the "占位
// 重排" placeholder-reorder rule).
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

// reorderTier reorders one tier in place: the subset of tier that has an
// applicable quota Limit (see applicableLimits) is sorted by headroom score
// descending (stable — a score tie keeps config-file relative order, same
// tie-break Sort itself uses) and written back into exactly the slot
// positions that subset already occupied; every other slot is untouched.
// Fewer than two quota-bearing members means there is nothing to reorder —
// 0 or 1 element is trivially already in the only order it can be.
func reorderTier(tier []*core.Endpoint, reg *quota.Registry, now time.Time) {
	var idxs []int
	var eps []*core.Endpoint
	var scores []float64
	for idx, ep := range tier {
		if ep.Quota == nil {
			continue
		}
		limits := applicableLimits(ep.Quota.Limits, ep.Model)
		if len(limits) == 0 {
			continue
		}
		idxs = append(idxs, idx)
		eps = append(eps, ep)
		scores = append(scores, scoreForEndpoint(ep.Provider, ep.Model, limits, reg, now))
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

// scoreForEndpoint reads provider's current consumption against every one
// of limits (already Scope-filtered to model — see applicableLimits) and
// merges them into one headroom score via the bucket-vs-gate rule
// (quota.ScoreForLimits). model is threaded into quota.LimitKey for any
// per-model Limit in the slice — a shared Limit ignores it. A read, but
// Registry.Used still performs the same lazy period-reset Charge does (see
// quota.Registry's doc comment), so a score computed just after a period
// boundary reflects a freshly-zeroed bucket even if nothing has charged
// into the new period yet.
func scoreForEndpoint(provider, model string, limits []core.Limit, reg *quota.Registry, now time.Time) float64 {
	used := make([]float64, len(limits))
	for i, l := range limits {
		c, _ := reg.Used(provider, quota.LimitKey(l, model), quota.PeriodStart(l, now))
		used[i] = quota.BaseAmount(l, c)
	}
	return quota.ScoreForLimits(limits, used, now)
}
