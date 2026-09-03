// Ver 2026-08-22, by Sonnet 5
package config

import (
	"strings"
	"testing"

	_ "vmr/internal/adapter/openai"
)

const apiKeysYAML = `
listen: 127.0.0.1:9910
providers:
  - name: openrouter
    base_url: {openai-completions: https://openrouter.ai/api/v1}
    api_keys:
      team_a: sk-aaaaaaaaaaaaaaaa
      team_b: sk-bbbbbbbbbbbbbbbb
models:
  m1:
    endpoints:
      - protocol: openai-completions
        providers: [openrouter]
        models: [real-model]
fallback_endpoints:
  - protocol: openai-completions
    providers: [openrouter]
    models: [fallback-model]
    priority: 90
`

// byName finds an expanded provider by name — expansion order follows Go's
// randomized map iteration (see expandProviderAPIKeys's doc comment), so
// tests assert by name/set, never by position.
func byName(providers []Provider, name string) (Provider, bool) {
	for _, p := range providers {
		if p.Name == name {
			return p, true
		}
	}
	return Provider{}, false
}

func TestProviderAPIKeys_ExpandsIntoNamedProviders(t *testing.T) {
	cfg, err := Parse([]byte(apiKeysYAML))
	if err != nil {
		t.Fatalf("api_keys should expand and validate: %v", err)
	}
	if len(cfg.Providers) != 2 {
		t.Fatalf("Providers = %v, want 2 expanded entries", cfg.Providers)
	}
	a, ok := byName(cfg.Providers, "openrouter-team_a")
	if !ok || a.APIKey != "sk-aaaaaaaaaaaaaaaa" {
		t.Errorf("openrouter-team_a = %+v (found=%v), want key sk-aaaaaaaaaaaaaaaa", a, ok)
	}
	b, ok := byName(cfg.Providers, "openrouter-team_b")
	if !ok || b.APIKey != "sk-bbbbbbbbbbbbbbbb" {
		t.Errorf("openrouter-team_b = %+v (found=%v), want key sk-bbbbbbbbbbbbbbbb", b, ok)
	}
	if a.APIKeys != nil || b.APIKeys != nil {
		t.Errorf("expanded providers should not carry APIKeys forward: %+v / %+v", a, b)
	}
}

func TestProviderAPIKeys_RewritesEndpointAndFallbackReferences(t *testing.T) {
	cfg, err := Parse([]byte(apiKeysYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	wantSet := map[string]bool{"openrouter-team_a": true, "openrouter-team_b": true}
	for _, got := range [][]string{cfg.Models["m1"].Endpoints[0].Providers, cfg.FallbackEndpoints[0].Providers} {
		if len(got) != 2 || !wantSet[got[0]] || !wantSet[got[1]] || got[0] == got[1] {
			t.Errorf("providers = %v, want both expanded names, each once", got)
		}
	}
	// The rewritten reference order must match cfg.Providers' own order for
	// these two entries — self-consistent, even though which one comes
	// first isn't pinned.
	got := cfg.Models["m1"].Endpoints[0].Providers
	a, _ := byName(cfg.Providers, got[0])
	b, _ := byName(cfg.Providers, got[1])
	if a.Name != got[0] || b.Name != got[1] {
		t.Errorf("reference order %v doesn't match resolved provider order", got)
	}
}

func TestProviderAPIKeys_BothAPIKeyAndAPIKeysRejected(t *testing.T) {
	yaml := strings.Replace(apiKeysYAML, "base_url: {openai-completions: https://openrouter.ai/api/v1}",
		"base_url: {openai-completions: https://openrouter.ai/api/v1}\n    api_key: sk-should-not-coexist", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "set either api_key or api_keys, not both") {
		t.Errorf("want a mutual-exclusivity error, got %v", err)
	}
}

// TestProviderAPIKeys_DuplicateLabelRejected relies on yaml.v3's own
// mapping decode — a duplicate key in api_keys: is rejected before
// expandProviderAPIKeys ever sees it, so there's no separate dup-check to
// maintain here.
func TestProviderAPIKeys_DuplicateLabelRejected(t *testing.T) {
	yaml := strings.Replace(apiKeysYAML, "team_b: sk-bbbbbbbbbbbbbbbb", "team_a: sk-bbbbbbbbbbbbbbbb", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), `mapping key "team_a" already defined`) {
		t.Errorf("want yaml's own duplicate-key error, got %v", err)
	}
}

// TestProviderAPIKeys_ListFormRejected relies on yaml.v3's own type
// mismatch error (APIKeys is map[string]string) — same rationale as above.
func TestProviderAPIKeys_ListFormRejected(t *testing.T) {
	yaml := strings.Replace(apiKeysYAML, "api_keys:\n      team_a: sk-aaaaaaaaaaaaaaaa\n      team_b: sk-bbbbbbbbbbbbbbbb",
		"api_keys: [sk-aaaaaaaaaaaaaaaa, sk-bbbbbbbbbbbbbbbb]", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "cannot unmarshal") {
		t.Errorf("want yaml's own type-mismatch error, got %v", err)
	}
}

// TestProviderAPIKeysLabelWithColonRejected: the second half of VE2. A
// label with ':' would flow straight into the expanded provider name
// (parent_name + "-" + label), reintroducing the same audit-label
// breakage the provider-name check guards against. Caught by
// expandProviderAPIKeys (which runs before validate()), so the error
// message names the parent provider, not an index.
func TestProviderAPIKeysLabelWithColonRejected(t *testing.T) {
	yaml := strings.Replace(apiKeysYAML, "team_a:", `"x:y":`, 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "':'") || !strings.Contains(err.Error(), `provider "openrouter"`) {
		t.Fatalf("want error naming the parent provider and ':', got %v", err)
	}
}

// TestProviderAPIKeysLabelWithSlashRejected: same shape as the colon
// test, for the '/' half of the rule.
func TestProviderAPIKeysLabelWithSlashRejected(t *testing.T) {
	yaml := strings.Replace(apiKeysYAML, "team_a:", `"x/y":`, 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "'/'") {
		t.Fatalf("want error naming '/', got %v", err)
	}
}

// TestProviderAPIKeysLegalLabelAccepted: a label with - _ . still
// expands fine.
func TestProviderAPIKeysLegalLabelAccepted(t *testing.T) {
	for _, label := range []string{"team-a", "team_b", "v1.0"} {
		// Replace BOTH api_keys entries in one go, not just the first
		// (yaml.v3 rejects duplicate keys, and apiKeysYAML already has
		// team_a + team_b).
		yaml := strings.Replace(apiKeysYAML, "      team_a: sk-aaaaaaaaaaaaaaaa\n      team_b: sk-bbbbbbbbbbbbbbbb",
			"      "+label+": sk-aaaaaaaaaaaaaaaa\n      other-key: sk-bbbbbbbbbbbbbbbb", 1)
		if _, err := Parse([]byte(yaml)); err != nil {
			t.Errorf("api_keys label %q should still validate: %v", label, err)
		}
	}
}

func TestProviderAPIKeys_ExpandedNameCollidesWithHandWrittenProvider(t *testing.T) {
	yaml := strings.Replace(apiKeysYAML,
		"      team_b: sk-bbbbbbbbbbbbbbbb\n",
		"      team_b: sk-bbbbbbbbbbbbbbbb\n  - name: openrouter-team_a\n    base_url: {openai-completions: https://example.com}\n    api_key: k\n", 1)
	_, err := Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), `duplicate provider name "openrouter-team_a"`) {
		t.Errorf("want a duplicate provider name error, got %v", err)
	}
}

func TestProviderAPIKeys_DirectReferenceToExpandedNameStillWorks(t *testing.T) {
	yaml := strings.Replace(apiKeysYAML, "providers: [openrouter]\n        models: [real-model]",
		"providers: [openrouter-team_a]\n        models: [real-model]", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("a direct reference to an expanded sub-provider name should still validate: %v", err)
	}
	got := cfg.Models["m1"].Endpoints[0].Providers
	if len(got) != 1 || got[0] != "openrouter-team_a" {
		t.Errorf("endpoint providers = %v, want [openrouter-team_a] unchanged", got)
	}
}
