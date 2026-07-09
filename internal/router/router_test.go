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
