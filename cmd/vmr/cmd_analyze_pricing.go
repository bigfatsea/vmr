// Ver 2026-09-23 04:16, by Claude Opus 5.5

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"vmr/internal/config"
	"vmr/internal/pricing"
	"vmr/internal/report"
)

// buildPricing resolves standard pricing tables and optional provider/global overrides from config.
// Degrades gracefully to embedded standard pricing if config is missing or invalid.
func buildPricing(cfg *config.Config, loadErr error, configPath string, tw io.Writer, displayCCY string, extraRates map[string]float64) (*pricing.Resolver, *report.Pricing) {
	standard, err := pricing.LoadStandard()
	if err != nil {
		fmt.Fprintf(tw, "pricing: embedded standard table failed to load (%v) — no $ estimates\n", err)
		return nil, nil
	}
	summary := &report.Pricing{Currency: "USD", StandardGeneratedAt: standard.GeneratedAt}

	var resolver *pricing.Resolver
	var configRates map[string]float64
	if loadErr != nil {
		resolver = pricing.NewResolver(standard, nil)
	} else {
		table := standard
		if t, err := cfg.PricingTable(); err == nil && t != nil {
			table = t
		}
		perProvider := map[string]pricing.ProviderPolicy{}
		overrideCount := 0
		for name, policy := range cfg.ProviderPricingPolicies {
			perProvider[name] = policy
			overrideCount += len(policy.Overrides)
		}
		configRates = cfg.ExchangeRate
		summary.ProviderOverrides = overrideCount
		if overrideCount > 0 {
			fmt.Fprintf(tw, "pricing: %d provider rate rule(s) loaded from %s\n", overrideCount, configPath)
		}
		resolver = pricing.NewResolver(table, perProvider)
	}

	if displayCCY != "" && !strings.EqualFold(displayCCY, summary.Currency) {
		rates, effErr := pricing.EffectiveExchangeRate(configRates)
		if effErr != nil {
			rates = map[string]float64{}
		}
		for k, v := range extraRates {
			rates[k] = v
		}
		if factor, ok := pricing.FactorBetween(summary.Currency, displayCCY, rates); ok {
			resolver = resolver.WithDisplayFactor(factor)
			summary.Currency = displayCCY
		} else {
			summary.RequestedCurrency = displayCCY
			fmt.Fprintf(tw, "pricing: no exchange rate to convert %s -> %s for -currency, showing %s instead (add exchange_rate: {%s: <rate>} to config.yaml's top level or report.yaml)\n", summary.Currency, displayCCY, summary.Currency, displayCCY)
		}
	}
	return resolver, summary
}

// resolvePricingForAnalyze builds the pricing resolver needed by analyze runs.
func resolvePricingForAnalyze(cfg *config.Config, cfgErr error, configPath, displayCCY string, exchangeRate map[string]float64) (*pricing.Resolver, *report.Pricing, string) {
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "config: %s not usable (%v) — $ estimates use the standard price table only (no account overrides)\n", configPath, cfgErr)
	}
	resolver, info := buildPricing(cfg, cfgErr, configPath, os.Stderr, displayCCY, exchangeRate)
	ccy := "USD"
	if info != nil && info.Currency != "" {
		ccy = info.Currency
	}
	return resolver, info, ccy
}

// resolvePricingFingerprint resolves standard pricing and override policies to produce the configuration fingerprint that keys the product cache.
func resolvePricingFingerprint(cfg *config.Config, extraRates map[string]float64) []byte {
	standardGen := ""
	if standard, err := pricing.LoadStandard(); err == nil && standard != nil {
		standardGen = standard.GeneratedAt
	}
	rates := map[string]float64{}
	var policies map[string]pricing.ProviderPolicy
	if cfg != nil {
		if t, err := cfg.PricingTable(); err == nil && t != nil && t.GeneratedAt != "" {
			standardGen = t.GeneratedAt
		}
		if cfg.ExchangeRate != nil {
			for k, v := range cfg.ExchangeRate {
				rates[k] = v
			}
		}
		policies = cfg.ProviderPricingPolicies
	}
	for k, v := range extraRates {
		rates[k] = v
	}
	return report.ComputePricingFingerprint(standardGen, rates, policies)
}

// allPathsOutsideDir reports whether EVERY entry in paths resolves outside dir.
func allPathsOutsideDir(paths []string, dir string) bool {
	if dir == "" || len(paths) == 0 {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for _, p := range paths {
		absP, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absDir, absP)
		if err != nil {
			continue
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false
		}
	}
	return true
}

// configHasQuotaLimits reports whether any provider declares a quota limit.
func configHasQuotaLimits(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	for _, p := range cfg.Providers {
		if p.Quota != nil && len(p.Quota.Limits) > 0 {
			return true
		}
	}
	return false
}
