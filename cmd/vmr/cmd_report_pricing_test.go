// Ver 2026-08-09, by Sonnet 5
package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestBuildPricing_NoDisplayCurrency_Unaffected pins the pre-existing
// behavior (config.yaml's pricing.currency, no -currency involved) so a
// regression in buildPricing's new display-currency branch can't silently
// change the common case.
func TestBuildPricing_NoDisplayCurrency_Unaffected(t *testing.T) {
	configPath := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
pricing:
  currency: CNY
  exchange_rate: {CNY: 7.1}
providers:
  - name: anthropic
    base_url: {anthropic: https://api.anthropic.com}
    api_key: test-key
models:
  m1:
    endpoints:
      - protocol: anthropic
        provider: anthropic
        models: [claude-3-7-sonnet-20250219]
`)
	var tw bytes.Buffer
	resolver, summary := buildPricing(configPath, &tw, "", nil)
	if summary.Currency != "CNY" {
		t.Fatalf("summary.Currency = %q, want CNY", summary.Currency)
	}
	rate, ok := resolver.RateFor("anthropic", "claude-3-7-sonnet-20250219", time.Now())
	if !ok || rate.InFresh == nil {
		t.Fatal("RateFor: no rate resolved")
	}
	// Standard table lists this model at 3.0 USD/1M in_fresh -> 3*7.1 CNY.
	if got, want := *rate.InFresh, 3*7.1; got < want-1e-6 || got > want+1e-6 {
		t.Errorf("InFresh = %v, want %v (unconverted CNY accounting figure)", got, want)
	}
}

// TestBuildPricing_DisplayCurrency_ConvertsFromComputeCurrency covers the
// new feature: compute in the config's accounting currency (CNY), DISPLAY
// in a different currency (JPY) via extraRates (report.yaml's own map) —
// the resolver must hand back JPY-scaled numbers and summary.Currency must
// relabel to JPY.
func TestBuildPricing_DisplayCurrency_ConvertsFromComputeCurrency(t *testing.T) {
	configPath := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
pricing:
  currency: CNY
  exchange_rate: {CNY: 7.1}
providers:
  - name: anthropic
    base_url: {anthropic: https://api.anthropic.com}
    api_key: test-key
models:
  m1:
    endpoints:
      - protocol: anthropic
        provider: anthropic
        models: [claude-3-7-sonnet-20250219]
`)
	var tw bytes.Buffer
	extraRates := map[string]float64{"JPY": 155}
	resolver, summary := buildPricing(configPath, &tw, "JPY", extraRates)
	if summary.Currency != "JPY" {
		t.Fatalf("summary.Currency = %q, want JPY", summary.Currency)
	}
	rate, ok := resolver.RateFor("anthropic", "claude-3-7-sonnet-20250219", time.Now())
	if !ok || rate.InFresh == nil {
		t.Fatal("RateFor: no rate resolved")
	}
	// 3.0 USD -> CNY (accounting, x7.1) -> JPY (display, /7.1 x155): the
	// USD list price converted straight to JPY via the USD pivot.
	want := 3.0 * 155
	if got := *rate.InFresh; got < want-1e-6 || got > want+1e-6 {
		t.Errorf("InFresh = %v, want %v (3.0 USD standard price -> JPY via USD pivot)", got, want)
	}
	if tw.String() != "" {
		t.Errorf("expected no warning when the conversion succeeds, got %q", tw.String())
	}
}

// TestBuildPricing_DisplayCurrency_ConfigRateWins covers cfg.Pricing.
// ExchangeRate supplying the needed rate with no report.yaml/-currency
// extraRates at all.
func TestBuildPricing_DisplayCurrency_ConfigRateWins(t *testing.T) {
	configPath := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
pricing:
  currency: USD
  exchange_rate: {CNY: 7.1}
providers:
  - name: anthropic
    base_url: {anthropic: https://api.anthropic.com}
    api_key: test-key
models:
  m1:
    endpoints:
      - protocol: anthropic
        provider: anthropic
        models: [claude-3-7-sonnet-20250219]
`)
	var tw bytes.Buffer
	resolver, summary := buildPricing(configPath, &tw, "CNY", nil)
	if summary.Currency != "CNY" {
		t.Fatalf("summary.Currency = %q, want CNY", summary.Currency)
	}
	rate, ok := resolver.RateFor("anthropic", "claude-3-7-sonnet-20250219", time.Now())
	if !ok || rate.InFresh == nil {
		t.Fatal("RateFor: no rate resolved")
	}
	if got, want := *rate.InFresh, 3*7.1; got < want-1e-6 || got > want+1e-6 {
		t.Errorf("InFresh = %v, want %v", got, want)
	}
}

// TestBuildPricing_DisplayCurrency_MissingRate_DegradesWithWarning: no rate
// anywhere to convert USD -> CNY — must warn and keep the original
// (compute) currency, never error out (vmr report's "a pricing problem
// costs $ accuracy, never the whole report" philosophy).
func TestBuildPricing_DisplayCurrency_MissingRate_DegradesWithWarning(t *testing.T) {
	configPath := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
providers:
  - name: anthropic
    base_url: {anthropic: https://api.anthropic.com}
    api_key: test-key
models:
  m1:
    endpoints:
      - protocol: anthropic
        provider: anthropic
        models: [claude-3-7-sonnet-20250219]
`)
	var tw bytes.Buffer
	resolver, summary := buildPricing(configPath, &tw, "CNY", nil)
	if summary.Currency != "USD" {
		t.Fatalf("summary.Currency = %q, want USD (degrade keeps the compute currency)", summary.Currency)
	}
	if !strings.Contains(tw.String(), "CNY") {
		t.Errorf("expected a warning mentioning the unresolved target currency, got %q", tw.String())
	}
	rate, ok := resolver.RateFor("anthropic", "claude-3-7-sonnet-20250219", time.Now())
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
	resolver, summary := buildPricing("/nonexistent/config.yaml", &tw, "JPY", extraRates)
	if summary.Currency != "JPY" {
		t.Fatalf("summary.Currency = %q, want JPY", summary.Currency)
	}
	rate, ok := resolver.RateFor("anthropic", "claude-3-7-sonnet-20250219", time.Now())
	if !ok || rate.InFresh == nil {
		t.Fatal("RateFor: no rate resolved (standard table alone should still cover this model)")
	}
	if got, want := *rate.InFresh, 3.0*155; got < want-1e-6 || got > want+1e-6 {
		t.Errorf("InFresh = %v, want %v", got, want)
	}
}
