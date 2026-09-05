// Ver 2026-09-06, by Sonnet 5
package main

import (
	"bytes"
	"strings"
	"testing"

	"vmr/internal/config"
)

// TestBuildPricing_NoDisplayCurrency_ComputeCurrencyIsUSD pins the new
// architecture's baseline: resolution is always USD (see
// internal/pricing.Table's doc comment — a provider's own pricing.currency
// is converted once at config-validate time, well before this ever runs).
// With no -currency requested, summary.Currency stays USD and RateFor
// returns the raw, unconverted standard-table price.
func TestBuildPricing_NoDisplayCurrency_ComputeCurrencyIsUSD(t *testing.T) {
	configPath := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
exchange_rate: {CNY: 7.1}
providers:
  - name: anthropic
    base_url: {anthropic-messages: https://api.anthropic.com}
    api_key: test-key
models:
  m1:
    endpoints:
      anthropic-messages:
        - providers: [anthropic]
          models: [claude-3-7-sonnet-20250219]
`)
	var tw bytes.Buffer
	cfg, cfgErr := config.Load(configPath)
	if cfgErr != nil {
		t.Fatalf("config.Load: %v", cfgErr)
	}
	resolver, summary := buildPricing(cfg, cfgErr, configPath, &tw, "", nil)
	if summary.Currency != "USD" {
		t.Fatalf("summary.Currency = %q, want USD", summary.Currency)
	}
	rate, ok := resolver.RateFor("anthropic", "claude-3-7-sonnet-20250219")
	if !ok || rate.InFresh == nil {
		t.Fatal("RateFor: no rate resolved")
	}
	// Standard table lists this model at 3.0 USD/1M in_fresh — no
	// conversion at all when no -currency is requested.
	if got, want := *rate.InFresh, 3.0; got < want-1e-6 || got > want+1e-6 {
		t.Errorf("InFresh = %v, want %v (unconverted USD standard price)", got, want)
	}
}

// TestBuildPricing_DisplayCurrency_ConvertsFromUSD covers -currency: compute
// stays USD, DISPLAY converts via extraRates (report.yaml's own rates map)
// — the resolver must hand back the display-scaled numbers and
// summary.Currency must relabel accordingly.
func TestBuildPricing_DisplayCurrency_ConvertsFromUSD(t *testing.T) {
	configPath := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
providers:
  - name: anthropic
    base_url: {anthropic-messages: https://api.anthropic.com}
    api_key: test-key
models:
  m1:
    endpoints:
      anthropic-messages:
        - providers: [anthropic]
          models: [claude-3-7-sonnet-20250219]
`)
	var tw bytes.Buffer
	extraRates := map[string]float64{"JPY": 155}
	cfg, cfgErr := config.Load(configPath)
	if cfgErr != nil {
		t.Fatalf("config.Load: %v", cfgErr)
	}
	resolver, summary := buildPricing(cfg, cfgErr, configPath, &tw, "JPY", extraRates)
	if summary.Currency != "JPY" {
		t.Fatalf("summary.Currency = %q, want JPY", summary.Currency)
	}
	rate, ok := resolver.RateFor("anthropic", "claude-3-7-sonnet-20250219")
	if !ok || rate.InFresh == nil {
		t.Fatal("RateFor: no rate resolved")
	}
	want := 3.0 * 155
	if got := *rate.InFresh; got < want-1e-6 || got > want+1e-6 {
		t.Errorf("InFresh = %v, want %v (3.0 USD standard price -> JPY)", got, want)
	}
	if tw.String() != "" {
		t.Errorf("expected no warning when the conversion succeeds, got %q", tw.String())
	}
}

// TestBuildPricing_DisplayCurrency_ConfigExchangeRateWins covers config.yaml's
// top-level exchange_rate: supplying the needed rate with no report.yaml/
// -currency extraRates at all — and that a user-declared rate wins over the
// built-in default table (CNY: 7.1 there — see
// internal/pricing/standard_exchange_rate.yaml) rather than merely
// coinciding with it.
func TestBuildPricing_DisplayCurrency_ConfigExchangeRateWins(t *testing.T) {
	configPath := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
exchange_rate: {CNY: 8.0}
providers:
  - name: anthropic
    base_url: {anthropic-messages: https://api.anthropic.com}
    api_key: test-key
models:
  m1:
    endpoints:
      anthropic-messages:
        - providers: [anthropic]
          models: [claude-3-7-sonnet-20250219]
`)
	var tw bytes.Buffer
	cfg, cfgErr := config.Load(configPath)
	if cfgErr != nil {
		t.Fatalf("config.Load: %v", cfgErr)
	}
	resolver, summary := buildPricing(cfg, cfgErr, configPath, &tw, "CNY", nil)
	if summary.Currency != "CNY" {
		t.Fatalf("summary.Currency = %q, want CNY", summary.Currency)
	}
	rate, ok := resolver.RateFor("anthropic", "claude-3-7-sonnet-20250219")
	if !ok || rate.InFresh == nil {
		t.Fatal("RateFor: no rate resolved")
	}
	if got, want := *rate.InFresh, 3.0*8.0; got < want-1e-6 || got > want+1e-6 {
		t.Errorf("InFresh = %v, want %v (config's own exchange_rate, not the built-in default 7.1)", got, want)
	}
}

// TestBuildPricing_DisplayCurrency_MissingRate_DegradesWithWarning: no rate
// anywhere (not the config, not the built-in default table) to convert USD
// -> a made-up currency — must warn and keep USD, never error out (vmr
// report's "a pricing problem costs $ accuracy, never the whole report"
// philosophy).
func TestBuildPricing_DisplayCurrency_MissingRate_DegradesWithWarning(t *testing.T) {
	configPath := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
providers:
  - name: anthropic
    base_url: {anthropic-messages: https://api.anthropic.com}
    api_key: test-key
models:
  m1:
    endpoints:
      anthropic-messages:
        - providers: [anthropic]
          models: [claude-3-7-sonnet-20250219]
`)
	var tw bytes.Buffer
	cfg, cfgErr := config.Load(configPath)
	if cfgErr != nil {
		t.Fatalf("config.Load: %v", cfgErr)
	}
	resolver, summary := buildPricing(cfg, cfgErr, configPath, &tw, "ZZZ", nil)
	if summary.Currency != "USD" {
		t.Fatalf("summary.Currency = %q, want USD (degrade keeps the compute currency)", summary.Currency)
	}
	if !strings.Contains(tw.String(), "ZZZ") {
		t.Errorf("expected a warning mentioning the unresolved target currency, got %q", tw.String())
	}
	rate, ok := resolver.RateFor("anthropic", "claude-3-7-sonnet-20250219")
	if !ok || rate.InFresh == nil {
		t.Fatal("RateFor: no rate resolved")
	}
	if got := *rate.InFresh; got != 3.0 {
		t.Errorf("InFresh = %v, want 3.0 (unconverted — the display factor must not apply on a failed conversion)", got)
	}
}

// TestBuildPricing_DisplayCurrency_NoConfigReachable proves -currency
// works even with no config.yaml at all — vmr report's documented degrade
// path (see buildPricing's own doc comment).
func TestBuildPricing_DisplayCurrency_NoConfigReachable(t *testing.T) {
	var tw bytes.Buffer
	extraRates := map[string]float64{"JPY": 155}
	cfg, cfgErr := config.Load("/nonexistent/config.yaml")
	resolver, summary := buildPricing(cfg, cfgErr, "/nonexistent/config.yaml", &tw, "JPY", extraRates)
	if summary.Currency != "JPY" {
		t.Fatalf("summary.Currency = %q, want JPY", summary.Currency)
	}
	rate, ok := resolver.RateFor("anthropic", "claude-3-7-sonnet-20250219")
	if !ok || rate.InFresh == nil {
		t.Fatal("RateFor: no rate resolved (standard table alone should still cover this model)")
	}
	if got, want := *rate.InFresh, 3.0*155; got < want-1e-6 || got > want+1e-6 {
		t.Errorf("InFresh = %v, want %v", got, want)
	}
}

// TestBuildPricing_LoadErrDoesNotWarnItself is the unit-level lock-in:
// buildPricing must NOT print its own cfgErr warning anymore — cmdReport
// prints the one unified warning now, so a duplicate here would resurrect
// the "same file, two warnings" noise the fix removed.
func TestBuildPricing_LoadErrDoesNotWarnItself(t *testing.T) {
	var tw bytes.Buffer
	cfg, cfgErr := config.Load("/nonexistent/config.yaml")
	buildPricing(cfg, cfgErr, "/nonexistent/config.yaml", &tw, "", nil)
	if tw.String() != "" {
		t.Errorf("buildPricing must not print its own cfgErr warning, got: %q", tw.String())
	}
}
