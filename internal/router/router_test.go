// Ver 2026-07-24 12:00, by Sonnet 5
package router

import (
	"testing"
	"time"

	"vmr/internal/config"

	_ "vmr/internal/adapter/openai"
)

func intPtr(n int) *int { return &n }

func TestEffectiveImageDownscaleMaxPx(t *testing.T) {
	cases := []struct {
		name      string
		route     *ModelRoute
		globalMax int
		want      int
	}{
		{"nil route falls back to global", nil, 1024, 1024},
		{"route with no override falls back to global", &ModelRoute{}, 1024, 1024},
		{"route override wins over global", &ModelRoute{ImageDownscaleMaxPx: intPtr(256)}, 1024, 256},
		{"explicit zero override force-disables regardless of global", &ModelRoute{ImageDownscaleMaxPx: intPtr(0)}, 1024, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.route.EffectiveImageDownscaleMaxPx(c.globalMax); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestBuildSnapshotCarriesModelImageDownscaleOverride(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
image_downscale: 1024
providers:
  openai:
    p1: {base_url: https://example.com, api_key: k1}
models:
  openai:
    plain:
      endpoints: [{provider: p1, model: m}]
    overridden:
      image_downscale: 256
      endpoints: [{provider: p1, model: m}]
    disabled:
      image_downscale: 0
      endpoints: [{provider: p1, model: m}]
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		model string
		want  int
	}{
		{"plain", 1024},     // no override: inherits global
		{"overridden", 256}, // explicit override wins
		{"disabled", 0},     // explicit zero force-disables
	}
	for _, c := range cases {
		route := snap.Models["openai"][c.model]
		if got := route.EffectiveImageDownscaleMaxPx(cfg.ImageDownscaleMaxPx); got != c.want {
			t.Errorf("model %q: effective image_downscale = %d, want %d", c.model, got, c.want)
		}
	}
}

// TestBuildSnapshotCarriesProviderRoleMap documents that a provider's
// role_map (e.g. remapping "developer" to "system" for providers that
// reject the former) reaches the endpoint BuildRequest actually sees —
// closing the gap between classify_test.go's coverage of the RewriteRoles
// byte-splice itself and the config->snapshot->endpoint wiring around it.
func TestBuildSnapshotCarriesProviderRoleMap(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  openai:
    mapped: {base_url: https://example.com, api_key: k1, role_map: {developer: system}}
    plain: {base_url: https://example.com, api_key: k2}
models:
  openai:
    vm:
      endpoints:
        - {provider: mapped, model: m1}
        - {provider: plain, model: m2}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}

	eps := snap.Models["openai"]["vm"].Endpoints
	mapped, plain := eps[0], eps[1]
	if got := mapped.RoleMap["developer"]; got != "system" {
		t.Errorf("mapped endpoint: RoleMap[developer] = %q, want %q", got, "system")
	}
	if plain.RoleMap != nil {
		t.Errorf("plain endpoint: RoleMap should be nil (provider has no role_map), got %v", plain.RoleMap)
	}
}

// TestBuildSnapshotCarriesConditionRoutingFields checks that
// capabilities/max_context_tokens reach core.Endpoint unchanged from
// config, and that an endpoint not declaring them ends up unconstrained
// (nil Capabilities, 0 MaxContextTokens) — see
// docs/VirtualModelRouter_Design_v4_Core.md §6.4.
func TestBuildSnapshotCarriesConditionRoutingFields(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  openai:
    p1: {base_url: https://example.com, api_key: k1}
models:
  openai:
    vm:
      endpoints:
        - provider: p1
          model: m1
          capabilities: [text, image, tools]
          max_context_tokens: 200000
        - provider: p1
          model: m2
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eps := snap.Models["openai"]["vm"].Endpoints
	declared, undeclared := eps[0], eps[1]

	if !declared.HasCapability("image") {
		t.Error("declared endpoint should report HasCapability(\"image\") = true")
	}
	if declared.MaxContextTokens != 200000 {
		t.Errorf("declared endpoint: MaxContextTokens = %d, want 200000", declared.MaxContextTokens)
	}
	if !undeclared.HasCapability("image") {
		t.Error("an endpoint with no declared capabilities must be unconstrained (HasCapability true for anything)")
	}
	if undeclared.MaxContextTokens != 0 {
		t.Errorf("undeclared endpoint: MaxContextTokens = %d, want 0 (unconstrained)", undeclared.MaxContextTokens)
	}
}

// TestBuildSnapshotResolvesStickyDefaultAndOverride locks the *bool ->
// bool resolution (nil = true) plus the endpoint-level StickyTTL
// inherit/override split — see
// docs/VirtualModelRouter_Design_v4_Core.md §6.5.
func TestBuildSnapshotResolvesStickyDefaultAndOverride(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
sticky_ttl: 10m
providers:
  openai:
    p1: {base_url: https://example.com, api_key: k1}
models:
  openai:
    defaulted:
      endpoints:
        - provider: p1
          model: m1
    disabled:
      sticky: false
      endpoints:
        - provider: p1
          model: m1
    overridden:
      endpoints:
        - provider: p1
          model: m1
          sticky_ttl: 2h
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !snap.Models["openai"]["defaulted"].Sticky {
		t.Error("Sticky should default to true when unset")
	}
	if snap.Models["openai"]["disabled"].Sticky {
		t.Error("explicit sticky: false must resolve to false")
	}
	if got := snap.Models["openai"]["defaulted"].Endpoints[0].StickyTTL; got != 10*time.Minute {
		t.Errorf("endpoint with no override: StickyTTL = %v, want the global 10m default", got)
	}
	if got := snap.Models["openai"]["overridden"].Endpoints[0].StickyTTL; got != 2*time.Hour {
		t.Errorf("endpoint with an override: StickyTTL = %v, want 2h", got)
	}
}
