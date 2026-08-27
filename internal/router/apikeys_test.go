// Ver 2026-08-22, by Sonnet 5

// BuildSnapshot coverage for the provider-level api_keys: config sugar
// (internal/config/apikeys.go). The desugaring happens entirely inside
// config.Parse, before BuildSnapshot ever runs — these tests exist to pin
// that BuildSnapshot/quota/health genuinely need no awareness of it: an
// api_keys-expanded provider must behave exactly like the hand-written
// multi-provider config in providergroup_test.go, key for key.
package router

import (
	"testing"

	"vmr/internal/config"

	_ "vmr/internal/adapter/openai"
)

const apiKeysSnapYAML = `
listen: 127.0.0.1:0
providers:
  - name: openrouter
    base_url: {openai-completions: https://openrouter.example.com}
    api_keys:
      team_a: sk-aaaaaaaaaaaaaaaa
      team_b: sk-bbbbbbbbbbbbbbbb
    quota:
      limits: [{metric: requests, every: 1mo, since: 2026-08-01, amount: 1000}]
models:
  vm:
    endpoints:
      - protocol: openai-completions
        providers: [openrouter]
        models: [model-a]
        priority: 1
`

// TestBuildSnapshot_APIKeys_ExpandsIntoDistinctEndpointsWithOwnCredentials
// pins that api_keys: yields the same per-key BaseURL/APIKey isolation as a
// hand-written providers: [p1, p2] list.
func TestBuildSnapshot_APIKeys_ExpandsIntoDistinctEndpointsWithOwnCredentials(t *testing.T) {
	cfg, err := config.Parse([]byte(apiKeysSnapYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eps := snap.Models["openai-completions"]["vm"].Endpoints
	if len(eps) != 2 {
		t.Fatalf("got %d endpoints, want 2 (one per api_keys entry)", len(eps))
	}
	// Expansion order follows Go's randomized map iteration (see
	// config.expandProviderAPIKeys's doc comment) — assert by provider name,
	// not position.
	for _, ep := range eps {
		switch ep.Provider {
		case "openrouter-team_a":
			if ep.APIKey != "sk-aaaaaaaaaaaaaaaa" {
				t.Errorf("openrouter-team_a: APIKey = %q, want sk-aaaaaaaaaaaaaaaa", ep.APIKey)
			}
		case "openrouter-team_b":
			if ep.APIKey != "sk-bbbbbbbbbbbbbbbb" {
				t.Errorf("openrouter-team_b: APIKey = %q, want sk-bbbbbbbbbbbbbbbb", ep.APIKey)
			}
		default:
			t.Errorf("unexpected provider %q", ep.Provider)
		}
	}
}

// TestBuildSnapshot_APIKeys_QuotaAndHealthAreIndependentPerKey pins the
// core claim behind treating api_keys: as full independent accounts: each
// expanded key gets its own *core.QuotaSpec (not a shared ledger) and its
// own distinct HealthKey, even though both came from one quota: block on
// the parent provider entry.
func TestBuildSnapshot_APIKeys_QuotaAndHealthAreIndependentPerKey(t *testing.T) {
	cfg, err := config.Parse([]byte(apiKeysSnapYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eps := snap.Models["openai-completions"]["vm"].Endpoints
	if eps[0].Quota == nil || eps[1].Quota == nil {
		t.Fatalf("both keys declared quota:, want both endpoints' Quota non-nil: %+v / %+v", eps[0].Quota, eps[1].Quota)
	}
	if eps[0].Quota == eps[1].Quota {
		t.Errorf("both expanded keys share the same *core.QuotaSpec pointer, want independent buckets")
	}
	if eps[0].HealthKey() == eps[1].HealthKey() {
		t.Errorf("both expanded keys share HealthKey %q, want distinct", eps[0].HealthKey())
	}
}
