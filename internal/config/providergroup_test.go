// Ver 2026-08-19, by Sonnet 5
package config

import (
	"strings"
	"testing"

	_ "vmr/internal/adapter/openai"
)

const providerGroupYAML = `
listen: 127.0.0.1:9902
providers:
  - name: p1
    base_url: {openai: https://api.example.com/v1}
    api_key: k1
  - name: p2
    base_url: {openai: https://api.example.com/v1}
    api_key: k2
models:
  m1:
    endpoints:
      - protocol: openai
        providers: [p1, p2]
        models: [real-model]
`

func TestEndpointGroup_ProvidersField_Parses(t *testing.T) {
	cfg, err := Parse([]byte(providerGroupYAML))
	if err != nil {
		t.Fatalf("providers: [...] should validate: %v", err)
	}
	eg := cfg.Models["m1"].Endpoints[0]
	got := eg.Providers
	want := []string{"p1", "p2"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Providers = %v, want %v", got, want)
	}
}

func TestEndpointGroup_EmptyProviders_Rejected(t *testing.T) {
	yaml := strings.Replace(providerGroupYAML, "providers: [p1, p2]", "", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "providers: at least one required") {
		t.Errorf("want a providers-required error, got %v", err)
	}
}

func TestEndpointGroup_ProvidersList_UnknownNameRejected(t *testing.T) {
	yaml := strings.Replace(providerGroupYAML, "providers: [p1, p2]", "providers: [p1, ghost]", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), `unknown provider "ghost"`) {
		t.Errorf("want unknown provider error naming ghost, got %v", err)
	}
}

func TestEndpointGroup_ProvidersList_MissingBaseURLForProtocolRejected(t *testing.T) {
	yaml := strings.Replace(providerGroupYAML,
		"  - name: p2\n    base_url: {openai: https://api.example.com/v1}\n    api_key: k2",
		"  - name: p2\n    base_url: {anthropic: https://api.example.com/v1}\n    api_key: k2", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), `provider "p2" has no base_url for protocol "openai"`) {
		t.Errorf("want a no-base_url error naming p2/openai, got %v", err)
	}
}

const fallbackYAML = `
listen: 127.0.0.1:9903
providers:
  - name: p1
    base_url: {openai: https://api.example.com/v1}
    api_key: k1
  - name: p2
    base_url: {openai: https://api.example.com/v1}
    api_key: k2
fallback_endpoints:
  - protocol: openai
    providers: [p2]
    models: [fallback-model]
    priority: 90
models:
  m1:
    endpoints:
      - protocol: openai
        providers: [p1]
        models: [real-model]
`

func TestFallbackEndpoints_Parses(t *testing.T) {
	cfg, err := Parse([]byte(fallbackYAML))
	if err != nil {
		t.Fatalf("fallback_endpoints: should validate: %v", err)
	}
	if len(cfg.FallbackEndpoints) != 1 {
		t.Fatalf("got %d fallback endpoints, want 1", len(cfg.FallbackEndpoints))
	}
	fb := cfg.FallbackEndpoints[0]
	if fb.Protocol != "openai" || len(fb.Providers) != 1 || fb.Providers[0] != "p2" || fb.Priority != 90 {
		t.Errorf("fallback endpoint = %+v, unexpected", fb)
	}
}

func TestFallbackEndpoints_PriorityOmitted_Rejected(t *testing.T) {
	yaml := strings.Replace(fallbackYAML, "    priority: 90\n", "", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "priority must be set and > 0") {
		t.Errorf("want a priority-required error, got %v", err)
	}
}

func TestFallbackEndpoints_PriorityZero_Rejected(t *testing.T) {
	yaml := strings.Replace(fallbackYAML, "priority: 90", "priority: 0", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "priority must be set and > 0") {
		t.Errorf("want a priority-required error, got %v", err)
	}
}

func TestFallbackEndpoints_PriorityNegative_Rejected(t *testing.T) {
	yaml := strings.Replace(fallbackYAML, "priority: 90", "priority: -1", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "priority must be set and > 0") {
		t.Errorf("want a priority-required error, got %v", err)
	}
}

// TestFallbackEndpoints_ReuseEndpointGroupValidation pins that a
// FallbackEndpoints entry is validated through the exact same path as an
// ordinary endpoint-group entry (validateEndpointGroup) — an unknown
// provider reference here must fail the same way it would inside
// models.<name>.endpoints[].
func TestFallbackEndpoints_ReuseEndpointGroupValidation(t *testing.T) {
	yaml := strings.Replace(fallbackYAML, "providers: [p2]", "providers: [ghost]", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), `unknown provider "ghost"`) {
		t.Errorf("want unknown provider error naming ghost, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "fallback_endpoints[0]") {
		t.Errorf("error should be attributed to fallback_endpoints[0], got %v", err)
	}
}

func TestFallbackEndpoints_UnknownProtocolRejected(t *testing.T) {
	yaml := strings.Replace(fallbackYAML, "protocol: openai\n    providers: [p2]", "protocol: nosuch\n    providers: [p2]", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "unknown protocol") {
		t.Errorf("want unknown protocol error, got %v", err)
	}
}

func TestFallbackEndpoints_EmptyModelsRejected(t *testing.T) {
	yaml := strings.Replace(fallbackYAML, "models: [fallback-model]", "models: []", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "at least one required") {
		t.Errorf("want models-required error, got %v", err)
	}
}

func TestVirtualModel_FallbackField_Parses(t *testing.T) {
	yaml := strings.Replace(fallbackYAML, "  m1:\n    endpoints:", "  m1:\n    fallback: false\n    endpoints:", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("fallback: false should validate: %v", err)
	}
	m := cfg.Models["m1"]
	if m.Fallback == nil || *m.Fallback != false {
		t.Errorf("Fallback = %v, want explicit false", m.Fallback)
	}
}

func TestVirtualModel_FallbackField_DefaultsToNilMeansTrue(t *testing.T) {
	cfg, err := Parse([]byte(fallbackYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models["m1"].Fallback != nil {
		t.Errorf("Fallback should be nil (unset) when not written, got %v", cfg.Models["m1"].Fallback)
	}
}

// TestFallbackEndpoints_FeedProviderModelsForPricing pins the fix noted in
// CHANGELOG.md: a (provider, model) pair reachable only through a
// fallback_endpoints: entry must still be registered into resolvePricing's
// completeness set — a metric: cost provider used only as a fallback must
// not be able to reach the request path with an unresolved rate. Exercised
// indirectly: a cost-metric provider with no matching pricing for a
// fallback-only model must fail validate(), not silently pass.
func TestFallbackEndpoints_FeedProviderModelsForPricing(t *testing.T) {
	yaml := `
listen: 127.0.0.1:9904
pricing: {currency: USD}
providers:
  - name: p1
    base_url: {openai: https://api.example.com/v1}
    api_key: k1
  - name: costy
    base_url: {openai: https://api.example.com/v1}
    api_key: k2
    quota:
      limits: [{metric: cost, every: 1mo, since: 2026-08-01, amount: 100}]
fallback_endpoints:
  - protocol: openai
    providers: [costy]
    models: [totally-unpriceable-model-xyz]
    priority: 90
models:
  m1:
    endpoints:
      - protocol: openai
        providers: [p1]
        models: [real-model]
`
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "totally-unpriceable-model-xyz") {
		t.Fatalf("want a pricing-resolution error naming the fallback-only model, got %v", err)
	}
}
