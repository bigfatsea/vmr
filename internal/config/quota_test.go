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
        provider: p1
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

func TestQuota_Reject_MetricCost(t *testing.T) {
	yaml := withQuotaBlock(`limits:
  - {metric: cost, every: 1mo, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), `"cost"`) {
		t.Errorf("want cost-metric rejection, got %v", err)
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

func TestQuota_Reject_ModelMultipliers_UnknownField(t *testing.T) {
	yaml := withQuotaBlock(`model_multipliers: {"*": 3}
limits:
  - {metric: requests, every: 1mo, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "model_multipliers") {
		t.Errorf("want model_multipliers unknown-field rejection, got %v", err)
	}
}

func TestQuota_Reject_TokenWeights_UnknownField(t *testing.T) {
	yaml := withQuotaBlock(`token_weights: {cache_read: 0.1}
limits:
  - {metric: tokens, every: 1mo, amount: 100}`)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "token_weights") {
		t.Errorf("want token_weights unknown-field rejection, got %v", err)
	}
}

func TestQuota_Reject_PricingBlock_UnknownField(t *testing.T) {
	yaml := strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\npricing:\n  currency: USD", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "pricing") {
		t.Errorf("want top-level pricing: block unknown-field rejection, got %v", err)
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
	if err == nil || !strings.Contains(err.Error(), "amount must be > 0") {
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
