// Ver 2026-08-07, by Opus 5

package pricing

import (
	"strings"

	"vmr/internal/core"
)

// OverrideRule is one providers[].pricing.overrides entry, as resolved by
// internal/config from YAML — the input shape Resolve consumes. Model
// supports a "*" wildcard (matches any upstream model); Discount and
// Explicit are mutually exclusive (internal/config's validation enforces
// this before Resolve ever sees a rule). No time dimension (date/hour
// window) by design — see EffectiveRate's doc comment for why that
// functionality was dropped rather than kept: this package used to carry
// date_from/date_to/hour_from/hour_to promotional windows, but they priced
// vmr's core routing decision (which is a dimensionless ratio, unaffected
// by absolute $ precision — see the design doc's §14.2① math) at a
// complexity cost (a full time-reachability analysis in what's now
// Complete/resolveChain) the feature's actual value never justified. A
// static, per-model price differentiation covers the overwhelming majority
// of real-world "this account's price differs by model" needs.
type OverrideRule struct {
	Model    string // exact upstream model name, or "*"
	Discount *float64
	Explicit Rate
}

// matchesModel reports whether o applies to model — exact match (case-
// insensitive, upstream model names aren't case-normalized by any adapter)
// or o's own "*" wildcard.
func (o OverrideRule) matchesModel(model string) bool {
	return o.Model == "*" || strings.EqualFold(o.Model, model)
}

func (o OverrideRule) toCoreOverride() core.PricingOverride {
	return core.PricingOverride{Discount: o.Discount, Explicit: o.Explicit.toCore()}
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

func lookupMapping(mapping map[string]string, model string) (string, bool) {
	if len(mapping) == 0 {
		return "", false
	}
	trimmed := strings.TrimSpace(model)
	if ck, ok := mapping[trimmed]; ok {
		return strings.TrimSpace(ck), true
	}
	lower := strings.ToLower(trimmed)
	for k, v := range mapping {
		if strings.ToLower(strings.TrimSpace(k)) == lower {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}

// resolveCanonicalKey implements the design doc's four-step automatic
// resolution: ① opts.Map's explicit entry for model (matched case-insensitively),
// ② "<provider>/<model>" (provider here is vmr's OWN providers[].name — see the
// design doc's literal wording; this only helps when a provider happens to be
// named after its upstream vendor, e.g. "anthropic", "deepseek"), ③ the bare
// model name — first as a canonical key on its own (the common shape for
// directly-configured Western vendors in the standard table), then through
// the table's alias map (one hop, see Table.aliases), ④ a "*/model" suffix
// match resolved by vendor precedence (see Table.LookupPreferredSuffix).
// Any step that doesn't resolve falls through to the next; if none do,
// ok=false — "no rate", not a guess.
//
// When the request name carries a "/" and all four raw steps missed, one
// basename retry follows: the whole resolution re-runs on
// ModelBasename(model). The table's keys are org-stripped (see
// ModelBasename), so an aggregator's forced org prefix
// ("google/gemma-4-31b-it" on together) would otherwise never reach any
// row. Re-running every step — not just the bare-name and suffix ones — is
// what makes the retry byte-identical to what the bare name itself resolves
// to: a provider named after its vendor answers its own
// "<provider>/<basename>" row at step ② for the bare name today, and the
// org-prefixed request must land on that same row (the serving vendor's own
// price), not diverge to a first-party list price via the suffix scan alone.
// The retry only ever WIDENS what resolves — every name a raw step answered
// keeps that answer, including map entries pinned on the raw name. No new
// pricing decision is introduced; this is not the retired generateAliases
// mechanic (it pins nothing — it lowers an input form into the space the
// existing four steps and vendor precedence already decide).
//
// The alias hop sits inside step ③ rather than ahead of step ②
// deliberately: a provider that IS the vendor (deepseek/deepseek-v4-flash)
// or that carries an exact supplement row of its own
// (sub2api/gemini-3.6-flash-high) already has the more specific answer, and
// a global alias must never override a per-provider one.
func resolveCanonicalKey(provider, model string, table *Table, mapping map[string]string) (Rate, bool) {
	if ck, ok := lookupMapping(mapping, model); ok {
		if r, ok := table.LookupRateOrAlias(ck); ok {
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
	if ck, ok := table.LookupAlias(model); ok {
		if r, ok := table.Lookup(ck); ok {
			return r, true
		}
		// An alias naming a key the merged table doesn't have is a load-time
		// error (Table.ValidateAliases, called by LoadStandard and config's
		// buildPricingContext), so reaching here means a caller built a
		// Table by hand without validating; falling through to step ④ is
		// best-effort, never a silent mis-price.
	}
	if r, ok := table.LookupPreferredSuffix(model); ok {
		return r, true
	}
	// Org-prefix fallback: the request name carries a path the generator
	// stripped from every table key (see ModelBasename) — reduce it to the
	// same space and re-run the whole resolution on the bare name. Recursing
	// (rather than re-running only the bare/suffix steps) is what makes the
	// result byte-identical to the bare name's own resolution. Terminates by
	// construction: ModelBasename never contains a "/", so the recursive
	// call's own fallback is unreachable. Runs only after all four raw steps
	// missed, so nothing that resolved before changes.
	if b := ModelBasename(model); b != model {
		return resolveCanonicalKey(provider, b, table, mapping)
	}
	return Rate{}, false
}

// Resolve computes provider+model's PricingSpec: Base is the RAW
// canonical-key lookup (opts.Map / the 4-step auto-resolution), converted
// to opts.Currency via opts.ExchangeRateToTarget — deliberately with NO
// override folded in (see EffectiveRate for why: folding an override into
// Base here and ALSO keeping it in Overrides for EffectiveRate to apply
// would double-apply it — a discount composing against itself). Overrides
// carries every opts.Overrides entry whose model pattern matches model, in
// written order, unmodified; EffectiveRate is the only place that ever
// combines Base with an Override.
//
// ok=false means the chain has no anchor: neither the table nor any
// Explicit override supplies so much as a partial rate for this
// provider+model. A matching set of ONLY discount-form overrides with no
// table hit also resolves ok=false — scaling an all-nil Base yields an
// all-nil Rate, which downstream consumers would read as a priced $0.00
// rather than "unpriced" (see Resolver.RateFor's gate). A
// partial/incomplete Base (some components nil) still resolves with
// ok=true; whether that's fatal is the CALLER's decision — see Complete,
// which config.validate() uses to decide.
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

	if !tableHit {
		// Without a table hit the chain's floor is spec.Base — empty. If no
		// matching override is Explicit either, resolveChain has no rate to
		// stop at and the whole chain resolves all-nil: "unpriced", not a
		// discount over nothing.
		hasExplicit := false
		for _, o := range matching {
			if o.Discount == nil {
				hasExplicit = true
				break
			}
		}
		if !hasExplicit {
			return nil, false
		}
	}

	spec := &core.PricingSpec{Base: base.toCore(), Currency: opts.Currency}
	for _, o := range matching {
		spec.Overrides = append(spec.Overrides, o.toCoreOverride())
	}
	return spec, true
}

// EffectiveRate resolves spec's Rate: the first Override (in written/config
// order) whose model pattern matched at Resolve time wins — Resolve already
// filtered spec.Overrides to model, so every entry here is a live candidate
// (no time-window eligibility check: P0-A dropped that dimension, see
// OverrideRule's doc comment). An explicit form is used as-is; a discount
// form scales "the rate that resolves below it in the chain" — the design
// doc's §4.2① wording, implemented as literally as its name suggests:
// everything BELOW this rule (later Overrides, then spec.Base), resolved
// recursively — NOT always spec.Base directly. This matters whenever a
// discount rule is layered above another, more specific rule (a wildcard
// catch-all discount above a model-specific explicit override): the
// catch-all must discount THAT rate, not fall straight through to Base
// (which, for a model with no standard-table entry at all and only account
// overrides, may not even be complete). No override at all (the common
// case) resolves to spec.Base itself. Deterministic — the same spec always
// resolves to the same Rate, which is why this is a pure function of spec
// alone, not "at" any particular moment. nil-safe: a nil spec returns the
// zero Rate.
func EffectiveRate(spec *core.PricingSpec) Rate {
	if spec == nil {
		return Rate{}
	}
	r, _ := resolveChain(spec, 0)
	return r
}

// Complete reports whether EffectiveRate(spec) is a Complete() rate — the
// gate config.validate() applies to any provider+model a metric: cost Limit
// will actually charge. Since EffectiveRate has exactly one resolution path
// (no time dimension to range over), this is a single walk, not a
// reachability search: badIndex is -1 when spec.Base itself supplied the
// (possibly incomplete) rate, else the index of the Override whose Explicit
// rate did (a Discount form can never itself introduce an incompleteness —
// Rate.Scale only narrows an already-resolved rate, never widens it).
// nil-safe: a nil spec is never complete.
func Complete(spec *core.PricingSpec) (ok bool, bad Rate, badIndex int) {
	if spec == nil {
		return false, Rate{}, -1
	}
	r, idx := resolveChain(spec, 0)
	return r.Complete(), r, idx
}

// resolveChain walks spec.Overrides starting at index from — the first
// entry wins: an explicit-form one is returned directly (idx = its own
// index), a discount-form one scales whatever resolveChain would have
// returned starting one entry later (a genuine recursive descent, not
// always spec.Base — see EffectiveRate's doc comment for why that
// distinction is load-bearing), propagating that deeper call's idx
// unchanged (a discount never becomes the "source" of an incompleteness).
// No entry at all falls through to spec.Base (idx = -1).
func resolveChain(spec *core.PricingSpec, from int) (Rate, int) {
	if from < len(spec.Overrides) {
		o := spec.Overrides[from]
		if o.Discount != nil {
			r, idx := resolveChain(spec, from+1)
			return r.Scale(*o.Discount), idx
		}
		return fromCoreRate(o.Explicit), from
	}
	return fromCoreRate(spec.Base), -1
}
