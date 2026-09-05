// Ver 2026-08-07, by Opus 5

package pricing

import (
	"strings"
	"sync"

	"vmr/internal/core"
)

// Resolver memoizes Resolve() across repeated (provider, model) lookups
// against a shared Table and one ResolveOptions-shaped policy per provider.
// `vmr report` calls RateFor once per audit record — tens of thousands of
// calls easily reusing the same provider+model pair — and re-walking the
// 4-step canonical resolution that often would be wasteful. Safe for
// concurrent use.
type Resolver struct {
	table       *Table
	perProvider map[string]ProviderPolicy
	// displayFactor, when non-zero and not 1, scales every Rate RateFor
	// returns — vmr report's final "resolved in USD, SHOWN in a different
	// display currency" step (see WithDisplayFactor). A pure linear rescale
	// applied once here, so aggregate.go's per-record Cost() math
	// (internal/report/cost.go's costFor) stays currency-unaware — it just
	// multiplies whatever four-component Rate this Resolver hands it
	// against raw token counts.
	displayFactor float64

	mu    sync.Mutex
	cache map[string]*core.PricingSpec // "provider\x00model" -> resolved spec, or an explicit nil entry caching a miss
}

// ProviderPolicy is one provider's aliases/rates — the account-specific
// half of ResolveOptions, without the shared Table (which NewResolver takes
// once, separately, since every provider resolves against the SAME merged
// standard table). No currency/exchange-rate factor here, nor on Resolver
// itself: every Rate this package resolves is already USD (see
// ResolveOptions' doc comment) — an audit log naming a provider since
// renamed or deleted resolves through a zero-value policy exactly like any
// other unconfigured provider, with no currency question to get wrong (the
// bug class the old per-provider USD->accounting-currency factor caused no
// longer has a place to occur).
type ProviderPolicy struct {
	Aliases   map[string]string
	Overrides []OverrideRule
}

// NewResolver builds a Resolver over table, with each named provider's own
// policy (aliases/rates) — a provider with no entry in perProvider
// resolves using the zero ProviderPolicy (no aliases, no rates): still
// useful, since the standard table alone can resolve plenty of
// provider+model pairs on its own (see resolveCanonicalKey's steps ②-④,
// none of which need a policy).
func NewResolver(table *Table, perProvider map[string]ProviderPolicy) *Resolver {
	return &Resolver{table: table, perProvider: perProvider, cache: map[string]*core.PricingSpec{}}
}

// WithDisplayFactor returns a new Resolver that scales every RateFor result
// by f — vmr report's display-currency step (cmd/vmr/cmd_report.go's
// buildPricing): resolution still happens in USD, this only rescales the
// number shown. A genuinely new Resolver (its own cache/mutex), not a
// shallow copy of r, so the two never share a sync.Mutex value under two
// independent lock states — a fresh, initially empty cache is cheap here
// since callers always call this immediately after NewResolver, before any
// RateFor call has populated it.
func (r *Resolver) WithDisplayFactor(f float64) *Resolver {
	nr := NewResolver(r.table, r.perProvider)
	nr.displayFactor = f
	return nr
}

// RateFor resolves provider+model's Rate — Resolve (memoized) then
// EffectiveRate, composed into the single call shape a per-record
// aggregation loop wants. ok=false when nothing resolves at all — no table
// entry, no override, or a dangling discount over an empty Base (an
// all-nil Rate is "unpriced", not "free": best-effort reports drop the $
// column rather than report $0.00). A resolved-but-incomplete Rate still
// returns ok=true (best-effort — vmr report degrades gracefully on partial
// data).
func (r *Resolver) RateFor(provider, model string) (Rate, bool) {
	spec, ok := r.resolve(provider, model)
	if !ok {
		return Rate{}, false
	}
	rate := EffectiveRate(spec)
	if rate.IsEmpty() {
		return Rate{}, false
	}
	if r.displayFactor != 0 && r.displayFactor != 1 {
		rate = rate.Scale(r.displayFactor)
	}
	return rate, true
}

// RateForEndpoint resolves the Rate for a "protocol:provider:model" audit-log
// endpoint label (core.EndpointLabel's format) — the shape both
// internal/report (per audit record) and internal/story (per journey step)
// hold, so neither has to carry its own label split alongside a RateFor
// call. Strict ":"-delimited SplitN(…, 3): the model segment may itself
// contain ":" or "/" (e.g. "z-ai/glm-5.2") and is passed through whole.
// ok=false for a malformed label (< 3 segments), the "-" no-endpoint
// sentinel, or an unresolvable provider+model — same best-effort contract
// as RateFor. Deliberately NOT core.SplitEndpointLabel, which also accepts
// the legacy "/"-joined form: widening this would change the $ numbers
// historical reports produce for old-format logs.
func (r *Resolver) RateForEndpoint(label string) (Rate, bool) {
	parts := strings.SplitN(label, ":", 3)
	if len(parts) < 3 {
		return Rate{}, false
	}
	return r.RateFor(parts[1], parts[2])
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
		Table: r.table, Aliases: policy.Aliases, Overrides: policy.Overrides,
	})
	if !ok {
		r.cache[key] = nil
		return nil, false
	}
	r.cache[key] = spec
	return spec, true
}
