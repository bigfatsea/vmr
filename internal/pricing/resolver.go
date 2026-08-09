// Ver 2026-08-07, by Opus 5

package pricing

import (
	"sync"
	"time"

	"vmr/internal/core"
)

// Resolver memoizes Resolve() across repeated (provider, model) lookups
// against a shared Table and one ResolveOptions-shaped policy per provider
// — the shape `vmr report`'s offline resolution needs (see this package's
// doc comment for the "two consumers, one implementation" split):
// internal/config's metric: cost resolution is one-shot (resolved once per
// provider+model at BuildSnapshot time and cached on core.Endpoint
// directly, no need for this type), but `vmr report` calls RateFor once per
// audit record — tens of thousands of calls easily reusing the same
// provider+model pair — and re-walking the 4-step canonical resolution
// that often would be wasteful. Safe for concurrent use.
type Resolver struct {
	table       *Table
	perProvider map[string]ProviderPolicy
	// displayFactor, when non-zero and not 1, scales every Rate RateFor
	// returns — vmr report's final "resolved in the accounting currency,
	// SHOWN in a different display currency" step (see WithDisplayFactor).
	// A pure linear rescale applied once here, so aggregate.go's per-record
	// Cost() math (internal/report/cost.go's costFor) stays currency-
	// unaware — it just multiplies whatever four-component Rate this
	// Resolver hands it against raw token counts.
	displayFactor float64

	mu    sync.Mutex
	cache map[string]*core.PricingSpec // "provider\x00model" -> resolved spec, or an explicit nil entry caching a miss
}

// ProviderPolicy is one provider's map/overrides — the account-specific
// half of ResolveOptions, without the shared Table (which NewResolver takes
// once, separately, since every provider resolves against the SAME merged
// standard(+supplement) table).
type ProviderPolicy struct {
	Map                  map[string]string
	Overrides            []OverrideRule
	ExchangeRateToTarget float64
	Currency             string
}

// NewResolver builds a Resolver over table, with each named provider's own
// policy (map/overrides/currency) — a provider with no entry in
// perProvider resolves using the zero ProviderPolicy (no map, no
// overrides, factor 1.0, no currency conversion): still useful, since the
// standard table alone can resolve plenty of provider+model pairs on its
// own (see resolveCanonicalKey's steps ②-④, none of which need a policy).
func NewResolver(table *Table, perProvider map[string]ProviderPolicy) *Resolver {
	return &Resolver{table: table, perProvider: perProvider, cache: map[string]*core.PricingSpec{}}
}

// WithDisplayFactor returns a new Resolver that scales every RateFor result
// by f — vmr report's display-currency step (cmd/vmr/cmd_report.go's
// buildPricing): resolution still happens in the accounting currency
// (config.yaml's pricing.currency, or USD with no config.yaml reachable),
// this only rescales the number shown. A genuinely new Resolver (its own
// cache/mutex), not a shallow copy of r, so the two never share a
// sync.Mutex value under two independent lock states — a fresh, initially
// empty cache is cheap here since callers always call this immediately
// after NewResolver, before any RateFor call has populated it.
func (r *Resolver) WithDisplayFactor(f float64) *Resolver {
	nr := NewResolver(r.table, r.perProvider)
	nr.displayFactor = f
	return nr
}

// RateFor resolves provider+model's Rate at ts — Resolve (memoized) then
// RateAt, composed into the single call shape a per-record aggregation
// loop wants. ok=false when nothing resolves at all (no table entry, no
// override) for this provider+model; a resolved-but-incomplete Rate still
// returns ok=true (best-effort — vmr report degrades gracefully on partial
// data the way it always has, unlike metric: cost's load-time hard gate).
func (r *Resolver) RateFor(provider, model string, ts time.Time) (Rate, bool) {
	spec, ok := r.resolve(provider, model)
	if !ok {
		return Rate{}, false
	}
	rate := RateAt(spec, ts)
	if r.displayFactor != 0 && r.displayFactor != 1 {
		rate = rate.Scale(r.displayFactor)
	}
	return rate, true
}

func (r *Resolver) resolve(provider, model string) (*core.PricingSpec, bool) {
	key := provider + "\x00" + model
	r.mu.Lock()
	defer r.mu.Unlock()
	if spec, cached := r.cache[key]; cached {
		return spec, spec != nil
	}
	policy := r.perProvider[provider]
	spec, ok := Resolve(provider, model, ResolveOptions{
		Table: r.table, Map: policy.Map, Overrides: policy.Overrides,
		ExchangeRateToTarget: policy.ExchangeRateToTarget, Currency: policy.Currency,
	})
	if !ok {
		r.cache[key] = nil
		return nil, false
	}
	r.cache[key] = spec
	return spec, true
}
