// Ver 2026-08-07, by Opus 5

package pricing

import (
	"strings"
	"sync"

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
	// tableFactor is the USD -> accounting-currency factor — see NewResolver.
	tableFactor float64
	// currency is the accounting currency this Resolver resolves in
	// (config.yaml's pricing.currency; "" when none was configured,
	// matching internal/config's own Resolve call). Stamped onto every
	// cached spec's Currency — vmr report's per-record RateFor path must
	// carry the same label the config path stamps on its ResolvedPricing
	// specs, or core.PricingSpec.Currency is populated in one consumer and
	// empty in the other (a two-half contract inconsistency). Distinct from
	// displayFactor: resolution happens in the accounting currency, and the
	// display rescale only renumbers the Rate it returns.
	currency string

	mu    sync.Mutex
	cache map[string]*core.PricingSpec // "provider\x00model" -> resolved spec, or an explicit nil entry caching a miss
}

// ProviderPolicy is one provider's map/overrides — the account-specific
// half of ResolveOptions, without the shared Table (which NewResolver takes
// once, separately, since every provider resolves against the SAME merged
// standard(+supplement) table) and without the USD -> accounting-currency
// factor, which is global (config.yaml's pricing.currency/exchange_rate)
// and lives on the Resolver for exactly that reason. Holding that factor
// per provider was a real bug, not just redundancy: an audit log names
// every provider that EVER ran, including ones since renamed or deleted,
// and those resolved through a zero-value policy — factor 1.0 — so the same
// canonical row rendered as USD for one provider and as CNY for another,
// side by side in one table, off by the exchange rate.
type ProviderPolicy struct {
	Map       map[string]string
	Overrides []OverrideRule
}

// NewResolver builds a Resolver over table, with each named provider's own
// policy (map/overrides) — a provider with no entry in perProvider
// resolves using the zero ProviderPolicy (no map, no overrides): still
// useful, since the standard table alone can resolve plenty of
// provider+model pairs on its own (see resolveCanonicalKey's steps ②-④,
// none of which need a policy).
// tableFactor converts one USD unit of table into the accounting currency
// (config.yaml's pricing.currency) — 1, or 0 for "not configured", both
// meaning no conversion. It applies to every provider alike, including one
// that appears only in an audit log and has no entry in perProvider at all.
// currency is that accounting currency's code ("" when none was
// configured, matching internal/config's own Resolve call): stamped onto
// every resolved spec's Currency field, so the offline path labels a
// number it computed the same way the config path does.
func NewResolver(table *Table, perProvider map[string]ProviderPolicy, tableFactor float64, currency string) *Resolver {
	return &Resolver{table: table, perProvider: perProvider, tableFactor: tableFactor, currency: currency, cache: map[string]*core.PricingSpec{}}
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
	nr := NewResolver(r.table, r.perProvider, r.tableFactor, r.currency)
	nr.displayFactor = f
	return nr
}

// RateFor resolves provider+model's Rate — Resolve (memoized) then
// EffectiveRate, composed into the single call shape a per-record
// aggregation loop wants. ok=false when nothing resolves at all (no table
// entry, no override) for this provider+model; a resolved-but-incomplete
// Rate still returns ok=true (best-effort — vmr report degrades gracefully
// on partial data the way it always has, unlike metric: cost's load-time
// hard gate).
func (r *Resolver) RateFor(provider, model string) (Rate, bool) {
	spec, ok := r.resolve(provider, model)
	if !ok {
		return Rate{}, false
	}
	rate := EffectiveRate(spec)
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
		Table: r.table, Map: policy.Map, Overrides: policy.Overrides,
		ExchangeRateToTarget: r.tableFactor, Currency: r.currency,
	})
	if !ok {
		r.cache[key] = nil
		return nil, false
	}
	r.cache[key] = spec
	return spec, true
}
