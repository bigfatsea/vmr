// Ver 2026-08-07, by Opus 5

// Pricing (P2.2) YAML-shape config types and their validation/resolution —
// see docs/VirtualModelRouter_Design_v4_Quota.md's pricing sections
// (config shape, three-layer resolution, "现状与后续计划" for what's
// actually shipped) for the full design. Split from config.go per that
// file's own line-count budget (see quota.go's identical rationale).
package config

import (
	"fmt"
	"os"
	"strings"

	"vmr/internal/core"
	"vmr/internal/pricing"
)

// PricingConfig is the global `pricing:` block: the account-independent
// settings needed to interpret every provider's pricing (see
// ProviderPricingConfig for the per-account half).
type PricingConfig struct {
	// Currency is the currency `cost`-metric amounts and account overrides
	// are denominated in. Required only when at least one provider actually
	// has a metric: cost Limit — a provider whose pricing: block exists
	// purely to sharpen `vmr report`'s $ estimates for a requests/tokens
	// account doesn't need one (cmd_report.go's independent pricing
	// resolution degrades to USD list price with no conversion, same as
	// today's behavior with no pricing.yaml at all).
	Currency string `yaml:"currency"`
	// ExchangeRate is a general "1 USD = X <code>" map (USD itself is
	// always implicit 1.0, never needs an entry here) — see
	// internal/pricing.FactorBetween. Every currency this deployment
	// touches needs an entry: Currency itself (to convert the USD standard
	// table into it), a providers[].pricing.overrides row's own currency:
	// (to convert it into Currency — overrides live in this same file, no
	// fallback question there), and a pricing.supplement/pricing.standard
	// row's own currency: that file DOESN'T resolve on its own — a
	// supplement file's own fileTable.ExchangeRate block, if it has one,
	// wins on a matching key (see internal/pricing.ParseTableWithRates'
	// doc comment for why: it keeps that file portable/self-contained
	// rather than silently depending on whichever config.yaml happens to
	// be merging it in); this map is only the fallback for what it doesn't
	// cover. Ignored entirely when nothing above needs a non-USD
	// conversion.
	ExchangeRate map[string]float64 `yaml:"exchange_rate"`
	// Supplement is an optional path to a user-maintained pricing table
	// (same shape as internal/pricing's embedded standard.*.yaml — see
	// that package's fileTable), merged over the embedded standard table
	// (supplement wins on a matching canonical key). A path that doesn't
	// exist is a load-time error, never a silent skip.
	Supplement string `yaml:"supplement"`
	// Standard optionally replaces the embedded standard table wholesale
	// (for a deployment that maintains its own complete price list) —
	// still merged with Supplement on top. Rare; most configs leave this
	// unset and get the embedded table.
	Standard string `yaml:"standard"`
}

// ProviderPricingConfig is one provider's `pricing:` block: what's
// different about THIS account's prices versus the standard list price —
// see docs/VirtualModelRouter_Design_v4_Quota.md's §4.2① for why this is
// two layers (account overrides on top of a shared table) rather than one.
type ProviderPricingConfig struct {
	// Map resolves a local upstream model name to the standard table's
	// canonical key, for the cases the automatic 4-step resolution (see
	// internal/pricing.resolveCanonicalKey) can't or shouldn't guess.
	Map map[string]string `yaml:"map"`
	// Overrides is a first-match-wins rule list — see PricingOverrideConfig.
	Overrides []PricingOverrideConfig `yaml:"overrides"`
}

// PricingOverrideConfig is one providers[].pricing.overrides entry, as
// written in YAML. Model supports a "*" wildcard. Exactly one of Discount
// or the four explicit rate components must be given — see validate()
// below for why an explicit form must supply all four or none at all
// (partial explicit rates are rejected, not silently treated as "the other
// components are free").
type PricingOverrideConfig struct {
	Model    string   `yaml:"model"`
	Discount *float64 `yaml:"discount"`
	// Currency optionally names the currency the four explicit components
	// below are written in — e.g. a domestic account's negotiated rate
	// entered straight from its CNY invoice while pricing.currency stays
	// USD. Converted to pricing.currency (or USD if that's unset) once at
	// load time via pricing.FactorBetween, same "normalize at the earliest
	// point" treatment pricing.supplement rows get. Empty means "already in
	// pricing.currency" (the pre-existing behavior, unchanged). Only valid
	// alongside an explicit rate — a discount is a dimensionless multiplier,
	// no currency applies.
	Currency   string   `yaml:"currency"`
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

// validate checks one override rule and, on success, returns its resolved
// pricing.OverrideRule form. rates/targetCurrency are only consulted when
// o.Currency names a conversion (see PricingOverrideConfig.Currency's doc
// comment); targetCurrency "" is treated as USD, matching
// buildPricingContext's own "no pricing.currency set = USD" default.
func (o PricingOverrideConfig) validate(providerName string, idx int, rates map[string]float64, targetCurrency string) (pricing.OverrideRule, error) {
	if o.Model == "" {
		return pricing.OverrideRule{}, fmt.Errorf("provider %q: pricing.overrides[%d]: model is required (a name, or \"*\" for a wildcard)", providerName, idx)
	}
	explicitN := o.explicitFieldsSet()
	switch {
	case o.Discount != nil && explicitN > 0:
		return pricing.OverrideRule{}, fmt.Errorf("provider %q: pricing.overrides[%d]: discount and an explicit rate are mutually exclusive — use one or the other, not both", providerName, idx)
	case o.Discount == nil && explicitN == 0:
		return pricing.OverrideRule{}, fmt.Errorf("provider %q: pricing.overrides[%d]: either discount or all four explicit rate components (in_fresh/cache_read/cache_write/out) are required", providerName, idx)
	case o.Discount == nil && explicitN != 4:
		return pricing.OverrideRule{}, fmt.Errorf("provider %q: pricing.overrides[%d]: an explicit rate must supply all four components (in_fresh/cache_read/cache_write/out) — a partial one is ambiguous about whether the rest are free or simply unspecified, see internal/pricing.Rate's doc comment", providerName, idx)
	}
	if o.Discount != nil && !positiveFinite(*o.Discount) {
		return pricing.OverrideRule{}, fmt.Errorf("provider %q: pricing.overrides[%d]: discount must be a finite number > 0 (got %v)", providerName, idx, *o.Discount)
	}
	if o.Currency != "" && o.Discount != nil {
		return pricing.OverrideRule{}, fmt.Errorf("provider %q: pricing.overrides[%d]: currency only applies to an explicit rate, not a discount multiplier", providerName, idx)
	}
	// An explicit component may legitimately be 0.0 ("this provider really
	// doesn't charge for cache reads") but never negative and never
	// non-finite — see nonNegativeFinite's doc comment for what a negative
	// rate would do to a metric: cost account's running total.
	for _, f := range []struct {
		name string
		val  *float64
	}{{"in_fresh", o.InFresh}, {"cache_read", o.CacheRead}, {"cache_write", o.CacheWrite}, {"out", o.Out}} {
		if f.val != nil && !nonNegativeFinite(*f.val) {
			return pricing.OverrideRule{}, fmt.Errorf("provider %q: pricing.overrides[%d]: %s must be a finite number >= 0 (got %v)", providerName, idx, f.name, *f.val)
		}
	}
	rule := pricing.OverrideRule{Model: o.Model, Discount: o.Discount}
	if o.Discount == nil {
		rate := pricing.Rate{InFresh: o.InFresh, CacheRead: o.CacheRead, CacheWrite: o.CacheWrite, Out: o.Out}
		if o.Currency != "" {
			target := targetCurrency
			if target == "" {
				target = "USD"
			}
			factor, ok := pricing.FactorBetween(o.Currency, target, rates)
			if !ok {
				return pricing.OverrideRule{}, fmt.Errorf("provider %q: pricing.overrides[%d]: currency %q has no matching pricing.exchange_rate entry to convert into %s", providerName, idx, o.Currency, target)
			}
			rate = rate.Scale(factor)
		}
		rule.Explicit = rate
	}
	return rule, nil
}

// firstDeadOverride returns the index of the first rule in rules (already
// validated, in written order) that first-match-wins can never reach: an
// earlier "*" wildcard matches every model including this rule's own, or an
// earlier rule already named this exact model. Returns -1 when every rule
// is reachable. This is only a meaningful, unconditional mistake now that
// P0-A dropped the date/hour time dimension — two rules sharing a model
// pattern used to legitimately differ by active time window (a promo
// stacked over a standing rate); with no time axis left, a repeated model
// pattern has no way to ever differ in outcome, so it is always dead
// config, not a deliberate pairing.
func firstDeadOverride(rules []pricing.OverrideRule) int {
	seenWildcard := false
	seenModel := map[string]bool{}
	for i, r := range rules {
		if seenWildcard {
			return i
		}
		key := strings.ToLower(r.Model)
		if seenModel[key] {
			return i
		}
		seenModel[key] = true
		if r.Model == "*" {
			seenWildcard = true
		}
	}
	return -1
}

// pricingContext bundles what resolvePricing needs across every provider —
// built once per config load/reload rather than reloading the embedded
// standard table and reparsing every provider's overrides once per
// provider+model pair.
type pricingContext struct {
	table                *pricing.Table
	exchangeRateToTarget float64
	currency             string
	rates                map[string]float64 // = gc.ExchangeRate, nil-safe — threaded into override validation for their own currency: conversion
}

// buildPricingContext loads the merged standard(+supplement) table and
// resolves the global exchange-rate factor. Only called when at least one
// provider actually needs it (a metric: cost Limit, or a pricing: block for
// vmr report's benefit) — see resolvePricing.
//
// A non-USD pricing.currency with no exchange_rate[currency] entry is an
// unconditional load-time error, not a factor-defaults-to-1 degrade: the
// standard/supplement table is USD-denominated and reaches BOTH consumers
// (metric: cost charging and vmr report's $ column), so silently skipping
// the conversion produces numbers labelled in the target currency but
// computed in USD — a ~7x error that looks completely normal on screen.
// "Every price this account uses is an explicit override, so no conversion
// is needed" is expressible without lying about the currency: leave
// pricing.currency unset, or give exchange_rate a 1.0 entry deliberately.
func buildPricingContext(gc *PricingConfig) (*pricingContext, error) {
	standard, err := pricing.LoadStandard()
	if err != nil {
		return nil, fmt.Errorf("embedded standard pricing table: %w", err)
	}
	if gc == nil {
		return &pricingContext{table: standard, exchangeRateToTarget: 1, currency: ""}, nil
	}
	table := standard
	if gc.Standard != "" {
		data, err := os.ReadFile(gc.Standard)
		if err != nil {
			return nil, fmt.Errorf("pricing.standard %s: %w", gc.Standard, err)
		}
		override, err := pricing.ParseTableWithRates(data, gc.ExchangeRate)
		if err != nil {
			return nil, fmt.Errorf("pricing.standard %s: %w", gc.Standard, err)
		}
		table = override
	}
	if gc.Supplement != "" {
		data, err := os.ReadFile(gc.Supplement)
		if err != nil {
			return nil, fmt.Errorf("pricing.supplement %s: %w", gc.Supplement, err)
		}
		supp, err := pricing.ParseTableWithRates(data, gc.ExchangeRate)
		if err != nil {
			return nil, fmt.Errorf("pricing.supplement %s: %w", gc.Supplement, err)
		}
		table = pricing.Merge(table, supp)
	}
	currency := gc.Currency
	factor := 1.0
	if currency != "" && currency != "USD" {
		f, ok := gc.ExchangeRate[currency]
		if !ok {
			return nil, fmt.Errorf("pricing.currency is %q but pricing.exchange_rate has no %q entry — the standard/supplement price table is USD-denominated and needs a rate to convert into %s (write exchange_rate: {%s: 1.0} to state deliberately that no conversion applies)", currency, currency, currency, currency)
		}
		if !positiveFinite(f) {
			return nil, fmt.Errorf("pricing.exchange_rate[%q]: must be a finite number > 0 (got %v)", currency, f)
		}
		factor = f
	}
	return &pricingContext{table: table, exchangeRateToTarget: factor, currency: currency, rates: gc.ExchangeRate}, nil
}

// resolvePricing is config.validate()'s pricing pass, run after both the
// provider loop and the models loop (providerModels: provider name -> set
// of upstream model names that provider is actually asked to serve,
// collected while validating models[].endpoints[].models). It validates
// every provider's pricing: block structurally (regardless of that
// provider's metric — a requests/tokens account's pricing: block only
// sharpens vmr report's $ estimates, but still must be well-formed), and
// additionally requires full, resolved pricing for every model a metric:
// cost provider is configured to serve — storing the result in
// c.ResolvedPricing for router.BuildSnapshot to attach to core.Endpoint,
// so this expensive-ish resolution happens exactly once per config load,
// not once per request.
func (c *Config) resolvePricing(providerModels map[string]map[string]bool) error {
	needsTable := c.Pricing != nil
	costProviders := map[string]bool{}
	for _, p := range c.Providers {
		if p.Pricing != nil {
			needsTable = true
		}
		if p.Quota != nil && len(p.Quota.Limits) == 1 && p.Quota.Limits[0].Resolved.Metric == core.MetricCost {
			costProviders[p.Name] = true
			needsTable = true
		}
	}
	if !needsTable {
		return nil
	}
	pctx, err := buildPricingContext(c.Pricing)
	if err != nil {
		return err
	}
	// Cached so PricingTable() (vmr report's broader, best-effort
	// resolution — see that method's doc comment) doesn't have to re-parse
	// the embedded standard table plus any supplement/standard override a
	// second time.
	c.pricingTableCache = pctx.table

	c.ResolvedPricing = map[string]*core.PricingSpec{}
	c.ProviderPricingPolicies = map[string]pricing.ProviderPolicy{}
	for _, p := range c.Providers {
		var mapping map[string]string
		var overrides []pricing.OverrideRule
		if p.Pricing != nil {
			mapping = p.Pricing.Map
			for _, local := range core.SortedKeys(p.Pricing.Map) {
				// An explicit map entry naming a canonical key the merged
				// table doesn't contain is always a mistake (a typo, or a
				// key that only exists in a supplement that isn't loaded) —
				// and a silent one, because resolution would just fall
				// through to the automatic steps and possibly land on some
				// OTHER model's price. The design doc's own rule for that
				// situation is "猜错一个费率比没有费率危险得多"; failing at
				// load time is how that rule is honored for a key the user
				// wrote out by hand.
				if _, ok := pctx.table.Lookup(p.Pricing.Map[local]); !ok {
					return fmt.Errorf("provider %q: pricing.map[%q]: %q is not a key in the standard/supplement price table — fix the canonical key, add it via pricing.supplement, or drop the map entry and let automatic resolution try (see internal/pricing.resolveCanonicalKey)", p.Name, local, p.Pricing.Map[local])
				}
			}
			for i, oc := range p.Pricing.Overrides {
				rule, err := oc.validate(p.Name, i, pctx.rates, pctx.currency)
				if err != nil {
					return err
				}
				overrides = append(overrides, rule)
			}
			if idx := firstDeadOverride(overrides); idx >= 0 {
				return fmt.Errorf("provider %q: pricing.overrides[%d]: model %q can never activate — an earlier rule in this list already matches every request this one would (either the exact same model, or an earlier \"*\" wildcard) and first-match-wins always picks that one first; drop this rule or reorder the list", p.Name, idx, overrides[idx].Model)
			}
		}
		// Stored for EVERY provider, not just ones with a pricing: block or
		// a metric: cost Limit — the policy also carries the global
		// currency/exchange-rate factor, and `vmr report` resolves rates
		// for every provider that appears in an audit log, most of which
		// have no pricing: block at all. Omitting them here would leave
		// their standard-table (USD) prices unconverted while the report
		// still labelled every number with pricing.currency: right label,
		// wrong number. Only the completeness gate below is specific to
		// metric: cost.
		c.ProviderPricingPolicies[p.Name] = pricing.ProviderPolicy{
			Map: mapping, Overrides: overrides,
			ExchangeRateToTarget: pctx.exchangeRateToTarget, Currency: pctx.currency,
		}
		if !costProviders[p.Name] {
			// A pricing: block on a non-cost provider is purely for vmr
			// report's benefit — validated above for shape, but nothing
			// needs to be resolved against router.Snapshot for it (report
			// resolves independently — see internal/pricing's package doc
			// comment on the two consumers).
			continue
		}
		if pctx.currency == "" {
			return fmt.Errorf("provider %q: has a metric: cost quota limit but pricing.currency is not set — cost accounting needs a currency to charge in", p.Name)
		}
		factor := pctx.exchangeRateToTarget
		models := core.SortedKeys(providerModels[p.Name])
		for _, model := range models {
			spec, ok := pricing.Resolve(p.Name, model, pricing.ResolveOptions{
				Table: pctx.table, Map: mapping, Overrides: overrides,
				ExchangeRateToTarget: factor, Currency: pctx.currency,
			})
			if !ok {
				return fmt.Errorf("provider %q: metric: cost: no price found for model %q — checked pricing.overrides, then providers[].pricing.map / the standard price table; add an override or a pricing.map entry (see providers[].pricing's doc comment)", p.Name, model)
			}
			// The resolved chain (Overrides first-match-wins, then Base) must
			// be fully priced, or a charge on the live request path would
			// silently under-price (a nil component priced as 0 — see
			// pricing.Rate's doc comment) with no load-time warning.
			if ok, bad, badIdx := pricing.Complete(spec); !ok {
				via := "the standard/supplement/account base rate (no override matched)"
				if badIdx >= 0 {
					via = fmt.Sprintf("pricing.overrides[%d]", badIdx)
				}
				return fmt.Errorf("provider %q: metric: cost: model %q resolves an incomplete rate via %s (missing %v) — every one of in_fresh/cache_read/cache_write/out must be priced (explicitly 0.0 if genuinely free)",
					p.Name, model, via, bad.MissingComponents())
			}
			c.ResolvedPricing[p.Name+"\x00"+model] = spec
		}
	}
	return nil
}

// PricingTable returns the merged standard(+supplement, +standard-override)
// table config.yaml's global pricing: block describes — the same table
// resolvePricing() builds internally for metric: cost accounts, exposed
// here for callers needing BROADER, best-effort pricing coverage than
// ResolvedPricing provides (see that field's doc comment: it only covers
// the specific provider+model pairs a metric: cost Limit actually needs,
// strictly validated). `vmr report`'s composition root (cmd/vmr/cmd_report.go)
// is the intended caller, pairing this with ProviderPricingPolicies to
// build a pricing.Resolver — see internal/pricing's package doc comment for
// why report can't just reuse ResolvedPricing directly (it only knows about
// models config.yaml's own models: block references, not whatever a raw
// audit log's endpoint labels happen to contain). Safe to call even when
// c.Pricing is nil and no provider declares a pricing: block (returns just
// the embedded standard table); returns a cached value when validate()
// already built one (the common case, once per config load/reload).
func (c *Config) PricingTable() (*pricing.Table, error) {
	if c.pricingTableCache != nil {
		return c.pricingTableCache, nil
	}
	pctx, err := buildPricingContext(c.Pricing)
	if err != nil {
		return nil, err
	}
	return pctx.table, nil
}
