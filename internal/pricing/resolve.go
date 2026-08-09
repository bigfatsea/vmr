// Ver 2026-08-07, by Opus 5

package pricing

import (
	"strings"
	"time"

	"vmr/internal/core"
	"vmr/internal/fmtutil"
)

// OverrideRule is one providers[].pricing.overrides entry, as resolved by
// internal/config from YAML — the input shape Resolve consumes. Model
// supports a "*" wildcard (matches any upstream model); Discount and
// Explicit are mutually exclusive (internal/config's validation enforces
// this before Resolve ever sees a rule).
type OverrideRule struct {
	Model            string // exact upstream model name, or "*"
	Discount         *float64
	Explicit         Rate
	DateFrom, DateTo string
	HourFrom, HourTo string
}

// matchesModel reports whether o applies to model — exact match (case-
// insensitive, upstream model names aren't case-normalized by any adapter)
// or o's own "*" wildcard.
func (o OverrideRule) matchesModel(model string) bool {
	return o.Model == "*" || strings.EqualFold(o.Model, model)
}

// matchesTime reports whether ts falls inside o's optional date/hour
// window — ported byte-for-byte from the old internal/report/pricing.go's
// PricingRate.matches (already exercised in production; see
// docs/TokenPlan_Quota_P2_DevPlan_opus-5.md's S5 "migrate, don't rewrite").
// ts is converted to fmtutil.DisplayZone up front so a record's own
// embedded offset never silently shifts which window a request falls into
// — the same reasoning core.Endpoint.HealthKey-adjacent code already
// documents for every other DisplayZone consumer.
func (o OverrideRule) matchesTime(ts time.Time) bool {
	ts = ts.In(fmtutil.DisplayZone)
	if o.DateFrom != "" && ts.Format("2006-01-02") < o.DateFrom {
		return false
	}
	if o.DateTo != "" && ts.Format("2006-01-02") > o.DateTo {
		return false
	}
	switch {
	case o.HourFrom != "" && o.HourTo != "":
		h := ts.Format("15:04")
		if o.HourFrom <= o.HourTo {
			if h < o.HourFrom || h > o.HourTo {
				return false
			}
		} else if h > o.HourTo && h < o.HourFrom { // wraps midnight
			return false
		}
	case o.HourFrom != "":
		if ts.Format("15:04") < o.HourFrom {
			return false
		}
	case o.HourTo != "":
		if ts.Format("15:04") > o.HourTo {
			return false
		}
	}
	return true
}

// unconditional reports whether o has no date/hour window at all — the
// "always active" rules Resolve uses to compute PricingSpec.Base (see
// ResolveOptions' doc comment for why only these count toward the
// completeness check a metric: cost account must pass).
func (o OverrideRule) unconditional() bool {
	return o.DateFrom == "" && o.DateTo == "" && o.HourFrom == "" && o.HourTo == ""
}

func (o OverrideRule) toCoreOverride() core.PricingOverride {
	return core.PricingOverride{
		Discount: o.Discount, Explicit: o.Explicit.toCore(),
		DateFrom: o.DateFrom, DateTo: o.DateTo, HourFrom: o.HourFrom, HourTo: o.HourTo,
	}
}

func (r Rate) toCore() core.Rate {
	return core.Rate{InFresh: r.InFresh, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite, Out: r.Out}
}

func fromCoreRate(r core.Rate) Rate {
	return Rate{InFresh: r.InFresh, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite, Out: r.Out}
}

// ResolveOptions bundles one vmr provider's pricing configuration inputs —
// everything Resolve needs beyond the provider/model name pair themselves.
type ResolveOptions struct {
	// Table is the merged supplement-over-standard table (see embed.go's
	// LoadStandard / Merge) — canonical-key -> Rate.
	Table *Table
	// Map is providers[].pricing.map: a local upstream model name -> the
	// canonical key it should resolve to, when the automatic 4-step
	// resolution (see resolveCanonicalKey) would get it wrong or fail.
	Map map[string]string
	// Overrides is providers[].pricing.overrides, UNFILTERED (every model
	// pattern, not just ones matching a specific endpoint) — Resolve
	// filters to the ones matching model itself.
	Overrides []OverrideRule
	// ExchangeRateToTarget converts one USD unit into the account's target
	// pricing.currency (1.0 when the target currency IS USD, or when no
	// exchange_rate applies because there's nothing from Table to convert —
	// account overrides are already in the target currency, never
	// converted). 0 is treated as "not configured" and only matters if
	// Table actually contributes a rate.
	ExchangeRateToTarget float64
	Currency             string
}

// resolveCanonicalKey implements the design doc's §9.1 four-step automatic
// resolution: ① opts.Map's explicit entry for model, ② "<provider>/<model>"
// (provider here is vmr's OWN providers[].name — see the design doc's
// literal wording; this only helps when a provider happens to be named
// after its upstream vendor, e.g. "anthropic", "deepseek"), ③ the bare
// model name as a canonical key on its own (the common shape for
// directly-configured Western vendors in the standard table), ④ a UNIQUE
// "*/model" suffix match across the whole table. Any step that doesn't
// resolve falls through to the next; if none do, ok=false — "no rate", not
// a guess (see Table.LookupUniqueSuffix's own doc comment on ambiguity).
func resolveCanonicalKey(provider, model string, table *Table, mapping map[string]string) (Rate, bool) {
	if ck, ok := mapping[model]; ok {
		if r, ok := table.Lookup(ck); ok {
			return r, true
		}
		// An explicit map entry naming a key the table doesn't have is a
		// config mistake, and config.validate() rejects it at load time
		// (see resolvePricing's pricing.map check) precisely so this
		// function never has to decide what a broken mapping means. Falling
		// through to the next step here is therefore only reachable for a
		// caller that skipped that validation — vmr report against a
		// config.yaml it couldn't load — where best-effort resolution is
		// the documented behavior, not a silent mis-price.
	}
	if r, ok := table.Lookup(provider + "/" + model); ok {
		return r, true
	}
	if r, ok := table.Lookup(model); ok {
		return r, true
	}
	return table.LookupUniqueSuffix(model)
}

// Resolve computes provider+model's PricingSpec: Base is the RAW
// canonical-key lookup (opts.Map / the 4-step auto-resolution), converted
// to opts.Currency via opts.ExchangeRateToTarget — deliberately with NO
// override folded in (see RateAt for why: folding an unconditional override
// into Base here and ALSO keeping it in Overrides for RateAt to apply would
// double-apply it — a discount composing against itself). Overrides carries
// every opts.Overrides entry whose model pattern matches model, in written
// order, unmodified; RateAt is the only place that ever combines Base with
// an Override.
//
// ok=false means neither the table nor any override supplies so much as a
// partial rate for this provider+model — genuinely nothing to go on. A
// partial/incomplete Base (some components nil) still resolves with
// ok=true; whether that's fatal is the CALLER's decision — see
// GuaranteedRate, which config.validate() uses to decide.
func Resolve(provider, model string, opts ResolveOptions) (*core.PricingSpec, bool) {
	base, tableHit := resolveCanonicalKey(provider, model, opts.Table, opts.Map)
	if tableHit {
		factor := opts.ExchangeRateToTarget
		if factor == 0 {
			factor = 1
		}
		base = base.Scale(factor)
	}

	var matching []OverrideRule
	for _, o := range opts.Overrides {
		if o.matchesModel(model) {
			matching = append(matching, o)
		}
	}

	if !tableHit && len(matching) == 0 {
		return nil, false
	}

	spec := &core.PricingSpec{Base: base.toCore(), Currency: opts.Currency}
	for _, o := range matching {
		spec.Overrides = append(spec.Overrides, o.toCoreOverride())
	}
	return spec, true
}

// RateAt resolves spec's effective Rate at ts: the first Override (in
// written/config order) whose time window contains ts wins. An explicit
// form is used as-is; a discount form scales "the rate that would have
// resolved had this rule not existed" — the design doc's §4.2① wording,
// implemented as literally as its name suggests: everything BELOW this
// rule in the chain (later Overrides, then spec.Base), resolved
// recursively — NOT always spec.Base directly. This matters whenever a
// discount rule is layered above another, more specific rule (a promo
// window above an always-on explicit override, e.g. the design doc's
// plan-e example): the promo must discount THAT rate, not fall straight
// through to Base (which, for a model with no standard-table entry at all
// and only account overrides, may not even be complete). No override
// matches at all (including the common case of none configured) resolves
// to spec.Base itself. nil-safe: a nil spec returns the zero Rate.
func RateAt(spec *core.PricingSpec, ts time.Time) Rate {
	if spec == nil {
		return Rate{}
	}
	return resolveChain(spec, func(o core.PricingOverride) bool {
		rule := OverrideRule{DateFrom: o.DateFrom, DateTo: o.DateTo, HourFrom: o.HourFrom, HourTo: o.HourTo}
		return rule.matchesTime(ts)
	}, 0)
}

// GuaranteedRate is the Rate RateAt would return at any moment NO
// time-scoped override is active — the same resolveChain walk as RateAt,
// but only ever descending through Overrides that have no date/hour window
// at all (since a windowed one might not be active at an arbitrary future
// charge time). This is what config.validate() checks Complete() against
// for a metric: cost account: a temporary promotional override is a bonus
// layered on top of real coverage, never a substitute for it (see
// docs/TokenPlan_Quota_P2_DevPlan_opus-5.md's §6.3). nil-safe: a nil spec
// returns the zero Rate.
func GuaranteedRate(spec *core.PricingSpec) Rate {
	if spec == nil {
		return Rate{}
	}
	return resolveChain(spec, func(o core.PricingOverride) bool {
		rule := OverrideRule{DateFrom: o.DateFrom, DateTo: o.DateTo, HourFrom: o.HourFrom, HourTo: o.HourTo}
		return rule.unconditional()
	}, 0)
}

// AllPathsComplete reports whether RateAt(spec, ts) is guaranteed Complete()
// for EVERY possible ts — not just "no override active" (that's
// GuaranteedRate's question). This closes a real gap GuaranteedRate alone
// leaves open: a conditional (time-scoped) discount override composing
// against an incomplete spec.Base would only surface as a silently wrong
// (under-priced) charge on the live request path at the moment that
// override's window is active, never as a load-time error — exactly the
// dangerous failure direction docs/TokenPlan_Quota_Routing_Design_opus-5.md's
// §9.1 validation checklist exists to rule out. config.validate() uses this
// (not GuaranteedRate) as the actual completeness gate for a metric: cost
// account. badIndex is -1 when spec.Base itself (the "no override active"
// case) is the incomplete one, else the index of the Override whose
// activation would produce badRate.
func AllPathsComplete(spec *core.PricingSpec) (ok bool, badRate Rate, badIndex int) {
	if spec == nil {
		return false, Rate{}, -1
	}
	hasUnconditional := false
	for i, o := range spec.Overrides {
		r := resolveChain(spec, func(core.PricingOverride) bool { return true }, i)
		if !r.Complete() {
			return false, r, i
		}
		rule := OverrideRule{DateFrom: o.DateFrom, DateTo: o.DateTo, HourFrom: o.HourFrom, HourTo: o.HourTo}
		if rule.unconditional() {
			// First-match-wins: an unconditional rule is eligible at every
			// ts, so RateAt can never walk past it — every LATER override is
			// dead config that no timestamp can activate, and checking it
			// would reject a config whose live behavior is always complete.
			// (Rules BEFORE this one are still reachable and were already
			// checked by earlier iterations; a discount among them descends
			// into this one, never past it.)
			hasUnconditional = true
			break
		}
	}
	if hasUnconditional {
		// spec.Base is unreachable at ANY ts: walking the Overrides list in
		// order, an unconditional entry matches every ts, so RateAt (and
		// this function's own per-override loop above) always resolves
		// through an Override before ever falling through to Base — Base's
		// own completeness is moot and must NOT be checked here (it would
		// reject configs like the design doc's plan-e, whose account
		// overrides fully cover a model the standard table doesn't even
		// list).
		return true, Rate{}, -1
	}
	base := fromCoreRate(spec.Base)
	if !base.Complete() {
		return false, base, -1
	}
	return true, Rate{}, -1
}

// resolveChain walks spec.Overrides starting at index from, using eligible
// to decide which entries are even candidates (RateAt: matches ts;
// GuaranteedRate: has no time window at all) — the first eligible entry
// wins: an explicit-form one is returned directly, a discount-form one
// scales whatever resolveChain would have returned starting one entry
// later (a genuine recursive descent, not always spec.Base — see RateAt's
// doc comment for why that distinction is load-bearing). No eligible entry
// falls through to spec.Base.
func resolveChain(spec *core.PricingSpec, eligible func(core.PricingOverride) bool, from int) Rate {
	for i := from; i < len(spec.Overrides); i++ {
		o := spec.Overrides[i]
		if !eligible(o) {
			continue
		}
		if o.Discount != nil {
			return resolveChain(spec, eligible, i+1).Scale(*o.Discount)
		}
		return fromCoreRate(o.Explicit)
	}
	return fromCoreRate(spec.Base)
}
