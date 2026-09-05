// Ver 2026-09-06, by Sonnet 5

// Pricing — YAML-shape config types and their validation/resolution.
// providers[].pricing is the ONLY place pricing is configured: no top-level
// pricing.rates/aliases, no external pricing.yaml supplement file. See
// docs/future-strategy/pricing_architecture_simplification_plan.md for the
// full rationale (four overlapping config-time entry points collapsed to
// one, and why the top-level pricing: block existed at all before this).
// Split from config.go per that file's own line-count budget.
package config

import (
	"fmt"
	"strings"

	"vmr/internal/fmtutil"
	"vmr/internal/pricing"
)

// ProviderPricingConfig is one provider's `pricing:` block: what's
// different about THIS account's prices versus the standard list price.
type ProviderPricingConfig struct {
	// Currency is a load-time annotation: the currency Rates' explicit
	// components below are written in (default USD). Converted to USD once
	// here, at validate time, via the top-level config.ExchangeRate table
	// (built-in default table as fallback — see
	// pricing.EffectiveExchangeRate). NOT a runtime quantity: it never
	// reaches core.Endpoint, the audit log, or report labels — see
	// core.PricingSpec's doc comment for why nothing downstream of
	// validate() ever needs to ask "which currency was this written in".
	Currency string `yaml:"currency"`
	// Aliases maps a local upstream model name to the standard table's
	// canonical key, for the cases the automatic 4-step resolution (see
	// internal/pricing.resolveCanonicalKey) can't or shouldn't guess.
	Aliases map[string]string `yaml:"aliases"`
	// Rates is a first-match-wins rule list — see PricingOverrideConfig.
	Rates []PricingOverrideConfig `yaml:"rates"`
}

// PricingOverrideConfig is one providers[].pricing.rates entry, as written
// in YAML. Model supports a "*" wildcard. Exactly one of Discount or the
// four explicit rate components must be given — see validate() below for
// why an explicit form must supply all four or none at all (partial
// explicit rates are rejected, not silently treated as "the other
// components are free"). No per-row currency: every row in one provider's
// Rates list is written in that provider's single pricing.currency (see
// ProviderPricingConfig.Currency) — a provider whose different rates are
// genuinely quoted in different currencies is rare enough that adding a
// second currency dimension here isn't worth the config surface.
type PricingOverrideConfig struct {
	Model      string   `yaml:"model"`
	Discount   *float64 `yaml:"discount"`
	InFresh    *float64 `yaml:"in_fresh"`
	CacheRead  *float64 `yaml:"cache_read"`
	CacheWrite *float64 `yaml:"cache_write"`
	Out        *float64 `yaml:"out"`
}

// explicitFieldsSet counts how many of the four rate components are set —
// used by validate() to enforce "all four or none".
func (o PricingOverrideConfig) explicitFieldsSet() int {
	n := 0
	for _, v := range []*float64{o.InFresh, o.CacheRead, o.CacheWrite, o.Out} {
		if v != nil {
			n++
		}
	}
	return n
}

// validate checks one rates[] rule's STRUCTURE only (model present,
// discount XOR a complete explicit rate, every number finite and in
// range) and returns its resolved pricing.OverrideRule form, still
// denominated in the provider's own pricing.currency — currency conversion
// to USD happens once per provider, over every rule at once, after they're
// all collected (see resolvePricing), not per row here.
func (o PricingOverrideConfig) validate(providerName string, idx int) (pricing.OverrideRule, error) {
	model := strings.TrimSpace(o.Model)
	if model == "" {
		return pricing.OverrideRule{}, fmt.Errorf("provider %q: pricing.rates[%d]: model is required (a name, or \"*\" for a wildcard)", providerName, idx)
	}
	explicitN := o.explicitFieldsSet()
	switch {
	case o.Discount != nil && explicitN > 0:
		return pricing.OverrideRule{}, fmt.Errorf("provider %q: pricing.rates[%d]: discount and an explicit rate are mutually exclusive — use one or the other, not both", providerName, idx)
	case o.Discount == nil && explicitN == 0:
		return pricing.OverrideRule{}, fmt.Errorf("provider %q: pricing.rates[%d]: either discount or all four explicit rate components (in_fresh/cache_read/cache_write/out) are required", providerName, idx)
	case o.Discount == nil && explicitN != 4:
		return pricing.OverrideRule{}, fmt.Errorf("provider %q: pricing.rates[%d]: an explicit rate must supply all four components (in_fresh/cache_read/cache_write/out) — a partial one is ambiguous about whether the rest are free or simply unspecified, see internal/pricing.Rate's doc comment", providerName, idx)
	}
	if o.Discount != nil && !positiveFinite(*o.Discount) {
		return pricing.OverrideRule{}, fmt.Errorf("provider %q: pricing.rates[%d]: discount must be a finite number > 0 (got %v)", providerName, idx, *o.Discount)
	}
	// An explicit component may legitimately be 0.0 ("this provider really
	// doesn't charge for cache reads") but never negative and never
	// non-finite.
	for _, f := range []struct {
		name string
		val  *float64
	}{{"in_fresh", o.InFresh}, {"cache_read", o.CacheRead}, {"cache_write", o.CacheWrite}, {"out", o.Out}} {
		if f.val != nil && !nonNegativeFinite(*f.val) {
			return pricing.OverrideRule{}, fmt.Errorf("provider %q: pricing.rates[%d]: %s must be a finite number >= 0 (got %v)", providerName, idx, f.name, *f.val)
		}
	}
	rule := pricing.OverrideRule{Model: model, Discount: o.Discount}
	if o.Discount == nil {
		rule.Explicit = pricing.Rate{InFresh: o.InFresh, CacheRead: o.CacheRead, CacheWrite: o.CacheWrite, Out: o.Out}
	}
	return rule, nil
}

// firstDeadOverride returns the index of the first rule in rules (already
// validated, in written order) that first-match-wins can never reach. Only
// an EXPLICIT rule (a rate, not a discount) terminates matching —
// resolveChain (internal/pricing) drills through a Discount to whatever
// resolves below it — so only an Explicit rule shadows later rules in its
// match domain: an earlier Explicit "*" wildcard makes every later rule
// unreachable, and an earlier Explicit rule for a model makes a later rule
// for that same model unreachable. A Discount-form rule (wildcard or not)
// shadows nothing: it composes multiplicatively with everything below it,
// so "[wildcard discount, specific explicit rate]" and stacked discounts on
// one model are both live, legal configs. Returns -1 when every rule is
// reachable.
func firstDeadOverride(rules []pricing.OverrideRule) int {
	seenExplicitWildcard := false
	seenExplicitModel := map[string]bool{}
	for i, r := range rules {
		if seenExplicitWildcard {
			return i
		}
		key := strings.ToLower(r.Model)
		if key != "*" && seenExplicitModel[key] {
			return i
		}
		if r.Discount == nil { // only an Explicit rule terminates matching
			if key == "*" {
				seenExplicitWildcard = true
			} else {
				seenExplicitModel[key] = true
			}
		}
	}
	return -1
}

// resolvePricing is config.validate()'s pricing pass, run after the
// provider loop. For every provider that declares a pricing: block, it
// validates the block structurally (aliases resolve against the standard
// table, rates are well-formed, no dead overrides) and converts that
// provider's rates to USD once via its own pricing.currency — building
// ProviderPricingPolicies for `vmr report`'s offline resolution
// (internal/pricing.Resolver). Nothing here touches routing or quota:
// pricing never reaches the request path (see core.PricingSpec's doc
// comment) — this whole pass exists solely for report's $ estimates and
// `vmr check`'s display.
func (c *Config) resolvePricing() error {
	if len(c.LegacyPricing) > 0 {
		return fmt.Errorf("top-level pricing: block is no longer supported — exchange_rate moved to the top level (exchange_rate: {...}), and currency/aliases/rates moved under each provider (providers[].pricing.{currency,aliases,rates}); see docs/UserGuide.md's Pricing section")
	}

	standard, err := pricing.LoadStandard()
	if err != nil {
		return fmt.Errorf("embedded standard pricing table: %w", err)
	}
	c.pricingTableCache = standard

	effectiveRates, err := pricing.EffectiveExchangeRate(c.ExchangeRate)
	if err != nil {
		return fmt.Errorf("exchange_rate: %w", err)
	}

	c.ProviderPricingPolicies = map[string]pricing.ProviderPolicy{}
	for _, p := range c.Providers {
		if p.Pricing == nil {
			continue
		}
		for _, local := range fmtutil.SortedKeys(p.Pricing.Aliases) {
			// An explicit alias entry naming a canonical key or alias the
			// standard table doesn't contain is always a mistake (a typo,
			// or a model this table doesn't carry) — and a silent one,
			// because resolution would just fall through to the automatic
			// steps and possibly land on some OTHER model's price. Failing
			// at load time is how "有歧义不猜" is honored for a key the
			// user wrote out by hand.
			if _, ok := standard.LookupRateOrAlias(p.Pricing.Aliases[local]); !ok {
				return fmt.Errorf("provider %q: pricing.aliases[%q]: %q is not a key or alias in the standard price table — fix the model name, or drop the alias entry and let automatic resolution try (see internal/pricing.resolveCanonicalKey)", p.Name, local, p.Pricing.Aliases[local])
			}
		}
		var overrides []pricing.OverrideRule
		for i, oc := range p.Pricing.Rates {
			rule, err := oc.validate(p.Name, i)
			if err != nil {
				return err
			}
			overrides = append(overrides, rule)
		}
		if idx := firstDeadOverride(overrides); idx >= 0 {
			return fmt.Errorf("provider %q: pricing.rates[%d]: model %q can never activate — an earlier Explicit rule (a rate, not a discount) in this list already matches every request this one would (either the exact same model, or an earlier \"*\" wildcard) and an Explicit rule terminates first-match-wins; a discount composes down the chain instead, so only an Explicit rule can shadow — drop this rule or reorder the list", p.Name, idx, overrides[idx].Model)
		}
		currency := strings.ToUpper(strings.TrimSpace(p.Pricing.Currency))
		if currency != "" && currency != "USD" {
			factor, ok := pricing.FactorBetween(currency, "USD", effectiveRates)
			if !ok {
				return fmt.Errorf("provider %q: pricing.currency %q has no matching exchange_rate entry to convert into USD (write exchange_rate: {%s: <rate>} at the top level, \"1 USD = <rate> %s\")", p.Name, currency, currency, currency)
			}
			for i := range overrides {
				// A Discount is a dimensionless multiplier — nothing to
				// convert. Only an Explicit rate is denominated in a
				// currency at all.
				if overrides[i].Discount == nil {
					overrides[i].Explicit = overrides[i].Explicit.Scale(factor)
				}
			}
		}
		c.ProviderPricingPolicies[p.Name] = pricing.ProviderPolicy{Aliases: p.Pricing.Aliases, Overrides: overrides}
	}
	return nil
}

// PricingTable returns the merged generated+curated standard table — the
// same table resolvePricing() builds internally, exposed here for
// `vmr report`'s composition root (cmd/vmr/cmd_report.go), which pairs it
// with ProviderPricingPolicies to build a pricing.Resolver. Safe to call
// even when validate() hasn't run (returns a freshly loaded table);
// returns the cached value once validate() has (the common case, once per
// config load/reload).
func (c *Config) PricingTable() (*pricing.Table, error) {
	if c.pricingTableCache != nil {
		return c.pricingTableCache, nil
	}
	standard, err := pricing.LoadStandard()
	if err != nil {
		return nil, err
	}
	c.pricingTableCache = standard
	return standard, nil
}
