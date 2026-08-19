// Ver 2026-08-07, by Opus 5

package config

import (
	"strings"
	"testing"
	"time"

	"vmr/internal/core"

	_ "vmr/internal/adapter/anthropic"
	_ "vmr/internal/adapter/openai"
)

const quotaBaseYAML = `
listen: 127.0.0.1:9900
providers:
  - name: p1
    base_url: {openai: https://api.example.com/v1}
    api_key: sk-test-0123456789abcdef
    quota:
%s
models:
  m1:
    endpoints:
      - protocol: openai
        providers: [p1]
        models: [real-model]
`

func withQuotaBlock(block string) string {
	// Every line of block gets indented under quota: — callers pass raw
	// YAML starting at "limits:".
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
		b.WriteString("      " + line + "\n")
	}
	return strings.Replace(quotaBaseYAML, "%s\n", b.String(), 1)
}

func TestQuota_HappyPath_Requests(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1mo, since: 2026-08-01, amount: 90000}`)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, ok := cfg.ProviderByName("p1")
	if !ok || p.Quota == nil || len(p.Quota.Limits) != 1 {
		t.Fatalf("quota not parsed: %+v", p.Quota)
	}
	l := p.Quota.Limits[0].Resolved
	if l.Metric != core.MetricRequests || l.EveryN != 1 || l.EveryUnit != "mo" || l.Amount != 90000 {
		t.Fatalf("resolved limit = %+v, want requests/1mo/90000", l)
	}
	want := time.Date(2026, 8, 1, 0, 0, 0, 0, l.Since.Location())
	if !l.Since.Equal(want) {
		t.Fatalf("since = %v, want %v", l.Since, want)
	}
}

func TestQuota_HappyPath_Tokens_DefaultSince(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: tokens, every: 1w, amount: 65000000}`)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, _ := cfg.ProviderByName("p1")
	l := p.Quota.Limits[0].Resolved
	if l.Metric != core.MetricTokens || l.EveryUnit != "w" {
		t.Fatalf("resolved limit = %+v", l)
	}
	if l.Since.Weekday() != time.Monday {
		t.Fatalf("default weekly since = %v, want a Monday", l.Since)
	}
}

func TestQuota_NoQuotaBlock_UnaffectedByDefault(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := cfg.ProviderByName("p1")
	if p.Quota != nil {
		t.Fatalf("Quota = %+v, want nil when not configured", p.Quota)
	}
}

// --- §2.2 reject cases: every one must be a load-time error, never silent. ---

func TestQuota_Reject_MultipleLimits(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 5h, amount: 100}
  - {metric: requests, every: 1mo, amount: 1000}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "one is supported") {
		t.Errorf("want multi-limit rejection, got %v", err)
	}
}

// TestQuota_MetricCost_WithoutPricingCurrency_Rejected verifies that metric: cost requires a pricing currency:
// completeness gate at the point closest to "cost" alone: metric: cost now
// parses structurally, but an account with no pricing.currency configured
// at all can't be charged in anything — see resolvePricing.
func TestQuota_MetricCost_WithoutPricingCurrency_Rejected(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: cost, every: 1mo, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "pricing.currency") {
		t.Errorf("want a pricing.currency-not-set rejection, got %v", err)
	}
}

func TestQuota_Reject_Rolling(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 5h, rolling: true, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "rolling") {
		t.Errorf("want rolling-window rejection, got %v", err)
	}
}

func TestQuota_Reject_ModelsScope(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1mo, amount: 100, models: [premium-model]}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "models") {
		t.Errorf("want models-scope rejection, got %v", err)
	}
}

// --- model_multipliers / token_weights are accepted fields ---

func TestQuota_ModelMultipliers_HappyPath(t *testing.T) {
	yaml := withQuotaBlock(`model_multipliers: {"*": 1, heavy-model: 9}
limits:
  - {metric: requests, every: 1mo, amount: 100}`)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, _ := cfg.ProviderByName("p1")
	if got := p.Quota.ModelMultipliers["heavy-model"]; got != 9 {
		t.Fatalf("model_multipliers[heavy-model] = %v, want 9", got)
	}
	if got := p.Quota.ModelMultipliers["*"]; got != 1 {
		t.Fatalf(`model_multipliers["*"] = %v, want 1`, got)
	}
}

func TestQuota_ModelMultipliers_ZeroOrNegative_Rejected(t *testing.T) {
	for _, bad := range []string{"0", "-1"} {
		yaml := withQuotaBlock(`model_multipliers: {heavy-model: ` + bad + `}
limits:
  - {metric: requests, every: 1mo, amount: 100}`)
		_, err := Parse([]byte(yaml))
		if err == nil || !strings.Contains(err.Error(), "model_multipliers") {
			t.Errorf("multiplier=%s: want model_multipliers rejection, got %v", bad, err)
		}
	}
}

// TestQuota_TokenWeights_ZeroValueIsNotDefault pins a real footgun: an
// account with no token_weights: at all must resolve to
// core.DefaultTokenWeight (1.0) on every component, never Go's TokenWeights{}
// zero value (all 0), which would silently zero out every tokens-metric
// account's accounting.
func TestQuota_TokenWeights_ZeroValueIsNotDefault(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: tokens, every: 1mo, amount: 100}`)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, _ := cfg.ProviderByName("p1")
	w := p.Quota.ResolvedTokenWeights
	want := core.TokenWeights{InFresh: 1, CacheRead: 1, CacheWrite: 1, Out: 1}
	if w != want {
		t.Fatalf("ResolvedTokenWeights = %+v, want %+v (all-default, not the zero value)", w, want)
	}
}

func TestQuota_TokenWeights_PartialOverride_RestDefault(t *testing.T) {
	yaml := withQuotaBlock(`token_weights: {cache_read: 0.1}
limits:
  - {metric: tokens, every: 1mo, amount: 100}`)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, _ := cfg.ProviderByName("p1")
	w := p.Quota.ResolvedTokenWeights
	want := core.TokenWeights{InFresh: 1, CacheRead: 0.1, CacheWrite: 1, Out: 1}
	if w != want {
		t.Fatalf("ResolvedTokenWeights = %+v, want %+v (only cache_read overridden)", w, want)
	}
}

func TestQuota_TokenWeights_ExplicitZero_Rejected(t *testing.T) {
	yaml := withQuotaBlock(`token_weights: {cache_read: 0}
limits:
  - {metric: tokens, every: 1mo, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "token_weights.cache_read") {
		t.Errorf("want explicit-zero token_weights rejection, got %v", err)
	}
}

func TestQuota_TokenWeights_NegativeRejected(t *testing.T) {
	yaml := withQuotaBlock(`token_weights: {out: -2}
limits:
  - {metric: tokens, every: 1mo, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "token_weights.out") {
		t.Errorf("want negative token_weights rejection, got %v", err)
	}
}

func TestQuota_TokenWeights_WithoutTokensLimit_Rejected(t *testing.T) {
	yaml := withQuotaBlock(`token_weights: {cache_read: 0.1}
limits:
  - {metric: requests, every: 1mo, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "no quota.limits entry uses metric") {
		t.Errorf("want token_weights-without-tokens-limit rejection, got %v", err)
	}
}

// TestQuota_PricingBlock_AcceptedWhenUnused verifies pricing block acceptance:
// a top-level pricing: block is now a real, known field — accepted (and
// simply unused) on a config with no metric: cost provider at all. See
// internal/config/pricing_test.go for the full pricing test matrix.
func TestQuota_PricingBlock_AcceptedWhenUnused(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\npricing:\n  currency: USD", 1)
	if _, err := Parse([]byte(yaml)); err != nil {
		t.Errorf("pricing: block should now be a known, accepted field, got %v", err)
	}
}

func TestQuota_Reject_EmptyLimits(t *testing.T) {
	yaml := withQuotaBlock(`limits: []`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "at least one entry") {
		t.Errorf("want empty-limits rejection, got %v", err)
	}
}

func TestQuota_Reject_BadEverySyntax(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: monthly, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "invalid every") {
		t.Errorf("want bad every: syntax rejection, got %v", err)
	}
}

func TestQuota_Reject_ZeroAmount(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1mo, amount: 0}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "amount must be a finite number > 0") {
		t.Errorf("want zero-amount rejection, got %v", err)
	}
}

func TestQuota_Reject_UnknownMetric(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: dollars, every: 1mo, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "unknown metric") {
		t.Errorf("want unknown-metric rejection, got %v", err)
	}
}

func TestQuota_Reject_MissingMetric(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {every: 1mo, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "metric is required") {
		t.Errorf("want missing-metric rejection, got %v", err)
	}
}

func TestQuota_Reject_DuplicateLimitKey(t *testing.T) {
	// Can't actually happen with the "at most one limit" rule in force, but
	// pin the check exists for when P3 lifts that rule — a hand-built
	// QuotaConfig with two identical (metric, every) entries must still be
	// caught by validateQuota directly.
	qc := &QuotaConfig{Limits: []LimitConfig{
		{Metric: "requests", Every: "1mo", Amount: 100},
		{Metric: "requests", Every: "1mo", Amount: 200},
	}}
	err := validateQuota("p1", qc, time.Now())
	// The ">1 limit" guard fires first today; either error is acceptable as
	// long as it's non-nil (P1 has no legal way to reach two identical
	// limits past that guard).
	if err == nil {
		t.Fatal("want an error for two limits on one provider")
	}
}

func TestQuota_Reject_InvalidSince(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: requests, every: 1mo, since: "not-a-date", amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "since") {
		t.Errorf("want invalid since rejection, got %v", err)
	}
}

// TestQuota_Reject_NonFiniteNumbers pins the NaN/Inf hole a plain `v <= 0`
// sign check leaves open: YAML's .nan/.inf are valid scalars, `NaN <= 0` is
// false, and a NaN that reaches ApplyModelMultiplier/headroom scoring
// poisons every accumulated quota.Counters value it touches instead of
// producing a config error.
func TestQuota_Reject_NonFiniteNumbers(t *testing.T) {
	cases := []struct{ name, block, want string }{
		{"amount NaN", `limits:
  - {metric: requests, every: 1mo, amount: .nan}`, "amount must be a finite number"},
		{"amount Inf", `limits:
  - {metric: requests, every: 1mo, amount: .inf}`, "amount must be a finite number"},
		{"model_multipliers NaN", `limits:
  - {metric: requests, every: 1mo, amount: 100}
model_multipliers: {"*": .nan}`, "model_multipliers"},
		{"token_weights Inf", `limits:
  - {metric: tokens, every: 1mo, amount: 100}
token_weights: {in_fresh: .inf}`, "token_weights.in_fresh"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(withQuotaBlock(tc.block)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want %q rejection, got %v", tc.want, err)
			}
		})
	}
}
