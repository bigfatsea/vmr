// Ver 2026-07-09 00:00, by Sonnet 5
package router

import (
	"testing"

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
