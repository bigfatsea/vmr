// Ver 2026-07-30, by Sonnet 5
package router

import (
	"reflect"
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
  - {name: p1, base_url: {openai: https://example.com}, api_key: k1}
models:
  plain:
    endpoints: [{protocol: openai, provider: p1, models: [m]}]
  overridden:
    image_downscale: 256
    endpoints: [{protocol: openai, provider: p1, models: [m]}]
  disabled:
    image_downscale: 0
    endpoints: [{protocol: openai, provider: p1, models: [m]}]
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

// TestBuildSnapshotCarriesEndpointRoleMap documents that an endpoint-group's
// role_map (e.g. remapping "developer" to "system" for providers that reject
// the former) reaches the endpoint BuildRequest actually sees — closing the
// gap between internal/jsonscan's coverage of the RewriteRoles byte-splice
// itself and the config->snapshot->endpoint wiring around it. role_map lives
// per endpoint-group (not per provider): the same account can back several
// endpoint-groups with different upstream model families.
func TestBuildSnapshotCarriesEndpointRoleMap(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  - {name: mapped, base_url: {openai: https://example.com}, api_key: k1}
  - {name: plain, base_url: {openai: https://example.com}, api_key: k2}
models:
  vm:
    endpoints:
      - {protocol: openai, provider: mapped, models: [m1], role_map: {developer: system}}
      - {protocol: openai, provider: plain, models: [m2]}
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
		t.Errorf("plain endpoint: RoleMap should be nil (its endpoint-group has no role_map), got %v", plain.RoleMap)
	}
}

// TestBuildSnapshotCarriesConditionRoutingFields checks that
// capabilities/max_context_tokens reach core.Endpoint unchanged from
// config, and that an endpoint not declaring them ends up unconstrained
// (nil Capabilities, 0 MaxContextTokens) — see
// docs/VirtualModelRouter_Design_v4_Core.md's Condition-based Routing section.
func TestBuildSnapshotCarriesConditionRoutingFields(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: https://example.com}, api_key: k1}
models:
  vm:
    endpoints:
      - protocol: openai
        provider: p1
        models: [m1]
        capabilities: [text, image, tools]
        max_context_tokens: 200000
      - protocol: openai
        provider: p1
        models: [m2]
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

// TestBuildSnapshotMergesModelBaseWithEndpointExtra locks the virtual-
// model-level Capabilities/MaxContextTokens base: an endpoint's own
// Capabilities is unioned on top of it (additive), its own
// MaxContextTokens overrides it when set (else inherits it as-is) — see
// config.VirtualModel/config.EndpointGroup's doc comments.
func TestBuildSnapshotMergesModelBaseWithEndpointExtra(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: https://example.com}, api_key: k1}
models:
  vm:
    capabilities: [text, tools]
    max_context_tokens: 128000
    endpoints:
      - protocol: openai
        provider: p1
        models: [extra]
        capabilities: [image]
        max_context_tokens: 512000
      - protocol: openai
        provider: p1
        models: [plain]
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
	extra, plain := eps[0], eps[1]

	for _, cap := range []string{"text", "tools", "image"} {
		if !extra.HasCapability(cap) {
			t.Errorf("extra endpoint: HasCapability(%q) = false, want true (base ∪ own)", cap)
		}
	}
	if got := extra.ExtraCapabilities; len(got) != 1 || got[0] != "image" {
		t.Errorf("extra endpoint: ExtraCapabilities = %v, want [image] (its own declaration, pre-merge)", got)
	}
	if extra.MaxContextTokens != 512000 {
		t.Errorf("extra endpoint: MaxContextTokens = %d, want 512000 (its own override)", extra.MaxContextTokens)
	}
	if extra.OwnMaxContextTokens != 512000 {
		t.Errorf("extra endpoint: OwnMaxContextTokens = %d, want 512000", extra.OwnMaxContextTokens)
	}

	for _, cap := range []string{"text", "tools"} {
		if !plain.HasCapability(cap) {
			t.Errorf("plain endpoint: HasCapability(%q) = false, want true (inherits base)", cap)
		}
	}
	if plain.HasCapability("image") {
		t.Error("plain endpoint: HasCapability(\"image\") = true, want false (base doesn't include it, endpoint added nothing)")
	}
	if len(plain.ExtraCapabilities) != 0 {
		t.Errorf("plain endpoint: ExtraCapabilities = %v, want empty (declared nothing of its own)", plain.ExtraCapabilities)
	}
	if plain.MaxContextTokens != 128000 {
		t.Errorf("plain endpoint: MaxContextTokens = %d, want 128000 (inherits model base)", plain.MaxContextTokens)
	}
	if plain.OwnMaxContextTokens != 0 {
		t.Errorf("plain endpoint: OwnMaxContextTokens = %d, want 0 (no override of its own)", plain.OwnMaxContextTokens)
	}
}

// TestMergeCapabilitiesDedup locks mergeCapabilities's exact contract:
// union of base+extra, base entries first, duplicates collapsed — an
// endpoint listing a capability its model's base already declares must not
// end up with it twice in the effective set.
func TestMergeCapabilitiesDedup(t *testing.T) {
	tests := []struct {
		name        string
		base, extra []string
		want        []string
	}{
		{"both empty", nil, nil, nil},
		{"base only", []string{"text", "tools"}, nil, []string{"text", "tools"}},
		{"extra only", nil, []string{"image"}, []string{"image"}},
		{"no overlap", []string{"text", "tools"}, []string{"image"}, []string{"text", "tools", "image"}},
		{"overlap deduped", []string{"text", "tools"}, []string{"tools", "image"}, []string{"text", "tools", "image"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeCapabilities(tt.base, tt.extra); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mergeCapabilities(%v, %v) = %v, want %v", tt.base, tt.extra, got, tt.want)
			}
		})
	}
}

// TestBuildSnapshotResolvesStickyDefaultAndOverride locks the *bool ->
// bool resolution (nil = true) plus the endpoint-level StickyTTL
// inherit/override split — see
// docs/VirtualModelRouter_Design_v4_Core.md's Sticky Model section.
func TestBuildSnapshotResolvesStickyDefaultAndOverride(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
sticky_ttl: 10m
providers:
  - {name: p1, base_url: {openai: https://example.com}, api_key: k1}
models:
  defaulted:
    endpoints:
      - {protocol: openai, provider: p1, models: [m1]}
  disabled:
    sticky: false
    endpoints:
      - {protocol: openai, provider: p1, models: [m1]}
  overridden:
    endpoints:
      - protocol: openai
        provider: p1
        models: [m1]
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

// TestBuildSnapshotSplitsVirtualModelByProtocol pins the new schema's
// cross-protocol reuse: one virtual model name with endpoint-groups on both
// protocols must resolve into two independent routes (Snapshot.Models
// ["openai"]["vm"] and ["anthropic"]["vm"]), each carrying only its own
// protocol's endpoints, sharing the model-level Sticky/Strategy/
// ImageDownscaleMaxPx settings.
func TestBuildSnapshotSplitsVirtualModelByProtocol(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: https://o.example, anthropic: https://a.example}, api_key: k1}
models:
  vm:
    sticky: false
    endpoints:
      - {protocol: openai, provider: p1, models: [m-openai]}
      - {protocol: anthropic, provider: p1, models: [m-anthropic]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	openaiRoute := snap.Models["openai"]["vm"]
	anthropicRoute := snap.Models["anthropic"]["vm"]
	if openaiRoute == nil || anthropicRoute == nil {
		t.Fatal("expected a route on both protocols")
	}
	if len(openaiRoute.Endpoints) != 1 || openaiRoute.Endpoints[0].Model != "m-openai" {
		t.Errorf("openai route endpoints: %+v", openaiRoute.Endpoints)
	}
	if len(anthropicRoute.Endpoints) != 1 || anthropicRoute.Endpoints[0].Model != "m-anthropic" {
		t.Errorf("anthropic route endpoints: %+v", anthropicRoute.Endpoints)
	}
	if openaiRoute.Sticky || anthropicRoute.Sticky {
		t.Error("sticky: false on the virtual model must apply to both protocol splits")
	}
}

// TestBuildSnapshotExpandsModelsList pins the new schema's headline feature:
// one endpoint-group's `models:` list expands into that many independent
// *core.Endpoints, in list order, sharing the group's provider/protocol.
func TestBuildSnapshotExpandsModelsList(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: https://example.com}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai, provider: p1, models: [model-a, model-b, model-c]}
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
	want := []string{"model-a", "model-b", "model-c"}
	if len(eps) != len(want) {
		t.Fatalf("got %d endpoints, want %d", len(eps), len(want))
	}
	for i, w := range want {
		if eps[i].Model != w {
			t.Errorf("endpoint[%d].Model = %q, want %q", i, eps[i].Model, w)
		}
		if eps[i].Provider != "p1" || eps[i].AdapterType != "openai" {
			t.Errorf("endpoint[%d]: provider=%q adapterType=%q", i, eps[i].Provider, eps[i].AdapterType)
		}
	}
}
