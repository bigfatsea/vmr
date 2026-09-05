// Ver 2026-07-30, by Sonnet 5
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
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
models:
  plain:
    endpoints: {openai-completions: [{providers: [p1], models: [m]}]}
  overridden:
    image_downscale: 256
    endpoints: {openai-completions: [{providers: [p1], models: [m]}]}
  disabled:
    image_downscale: 0
    endpoints: {openai-completions: [{providers: [p1], models: [m]}]}
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
		route := snap.Models["openai-completions"][c.model]
		if got := route.EffectiveImageDownscaleMaxPx(cfg.ImageDownscaleMaxPx); got != c.want {
			t.Errorf("model %q: effective image_downscale = %d, want %d", c.model, got, c.want)
		}
	}
}

// TestBuildSnapshotCarriesEndpointRoleMap documents that a provider's
// role_map (e.g. remapping "developer" to "system" for providers that reject
// the former) reaches the endpoint BuildRequest actually sees — closing the
// gap between internal/jsonscan's coverage of the RewriteRoles byte-splice
// itself and the config->snapshot->endpoint wiring around it. role_map lives
// per provider: the role rejection it repairs is a property of the
// provider's API implementation, not of any one virtual model.
func TestBuildSnapshotCarriesEndpointRoleMap(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  - {name: mapped, base_url: {openai-completions: https://example.com}, api_key: k1, role_map: {developer: system}}
  - {name: plain, base_url: {openai-completions: https://example.com}, api_key: k2}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [mapped], models: [m1]}
        - {providers: [plain], models: [m2]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}

	eps := snap.Models["openai-completions"]["vm"].Endpoints
	mapped, plain := eps[0], eps[1]
	if got := mapped.RoleMap["developer"]; got != "system" {
		t.Errorf("mapped endpoint: RoleMap[developer] = %q, want %q", got, "system")
	}
	if plain.RoleMap != nil {
		t.Errorf("plain endpoint: RoleMap should be nil (its provider has no role_map), got %v", plain.RoleMap)
	}
}

// TestBuildSnapshotCarriesConditionRoutingFields checks that
// capabilities/max_context_tokens from model_defaults reach core.Endpoint,
// and that an endpoint with no matching declaration ends up unconstrained
// (nil Capabilities, 0 MaxContextTokens) — see
// docs/VirtualModelRouter_Design_v4_Core.md's Condition-based Routing section.
func TestBuildSnapshotCarriesConditionRoutingFields(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
model_defaults:
  m1:
    capabilities: [text, image, tools]
    max_context_tokens: 200000
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
models:
  vm:
    endpoints:
      openai-completions:
        - providers: [p1]
          models: [m1]
        - providers: [p1]
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
	eps := snap.Models["openai-completions"]["vm"].Endpoints
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

// TestBuildSnapshotResolvesModelDefaultsAndOverrides tests the three-tier
// priority (virtual model override > exact model default > wildcard "*")
// and per-field independent fallback.
func TestBuildSnapshotResolvesModelDefaultsAndOverrides(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
model_defaults:
  "*":
    capabilities: [text, tools]
    max_context_tokens: 256000
  MiniMax-M3:
    capabilities: [text, tools, image, audio, video, thinking]
    max_context_tokens: 512000
    providers: [p1]
  partial-model:
    max_context_tokens: 100000
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
  - {name: p2, base_url: {openai-completions: https://example.com}, api_key: k2}
models:
  agent:
    endpoints:
      openai-completions:
        - providers: [p1]
          models: [MiniMax-M3]
        - providers: [p2]
          models: [MiniMax-M3]
        - providers: [p1]
          models: [partial-model]
        - providers: [p1]
          models: [unknown-model]
  cheap:
    max_context_tokens: 128000
    endpoints:
      openai-completions:
        - providers: [p1]
          models: [MiniMax-M3]
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	agentEps := snap.Models["openai-completions"]["agent"].Endpoints
	// ep 0: p1 / MiniMax-M3 -> exact match (providers contains p1)
	ep0 := agentEps[0]
	if ep0.MaxContextTokens != 512000 {
		t.Errorf("ep0 MaxContextTokens = %d, want 512000 (exact match)", ep0.MaxContextTokens)
	}
	for _, cap := range []string{"text", "tools", "image", "audio", "video", "thinking"} {
		if !ep0.HasCapability(cap) {
			t.Errorf("ep0 missing capability %s", cap)
		}
	}

	// ep 1: p2 / MiniMax-M3 -> exact match providers=[p1] does not match p2, falls back to "*"
	ep1 := agentEps[1]
	if ep1.MaxContextTokens != 256000 {
		t.Errorf("ep1 MaxContextTokens = %d, want 256000 (fallback to wildcard)", ep1.MaxContextTokens)
	}
	if ep1.HasCapability("image") {
		t.Error("ep1 should not have image capability (wildcard only has text, tools)")
	}
	if !ep1.HasCapability("text") || !ep1.HasCapability("tools") {
		t.Error("ep1 should have text and tools from wildcard")
	}

	// ep 2: p1 / partial-model -> exact has max_context_tokens: 100000, capabilities falls back to "*"
	ep2 := agentEps[2]
	if ep2.MaxContextTokens != 100000 {
		t.Errorf("ep2 MaxContextTokens = %d, want 100000 (per-field exact)", ep2.MaxContextTokens)
	}
	if !ep2.HasCapability("text") || !ep2.HasCapability("tools") {
		t.Error("ep2 should inherit text, tools capabilities from wildcard")
	}

	// ep 3: p1 / unknown-model -> both fields fall back to "*"
	ep3 := agentEps[3]
	if ep3.MaxContextTokens != 256000 {
		t.Errorf("ep3 MaxContextTokens = %d, want 256000 (wildcard)", ep3.MaxContextTokens)
	}
	if !ep3.HasCapability("text") || !ep3.HasCapability("tools") {
		t.Error("ep3 should have text, tools from wildcard")
	}

	// cheap model overrides max_context_tokens to 128000
	cheapEps := snap.Models["openai-completions"]["cheap"].Endpoints
	cheapEp := cheapEps[0]
	if cheapEp.MaxContextTokens != 128000 {
		t.Errorf("cheapEp MaxContextTokens = %d, want 128000 (virtual model override)", cheapEp.MaxContextTokens)
	}
	// cheapEp capabilities still come from MiniMax-M3 exact match
	if !cheapEp.HasCapability("image") {
		t.Error("cheapEp should still inherit capabilities from model_defaults exact match")
	}
}

// TestBuildSnapshotResolvesStickyDefaultAndOverride locks the *bool ->
// bool resolution (nil = true) plus the provider-level StickyTTL
// inherit/override split — see
// docs/VirtualModelRouter_Design_v4_Core.md's Sticky Model section.
func TestBuildSnapshotResolvesStickyDefaultAndOverride(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
ttl:
  sticky: 10m
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
  - {name: p2, base_url: {openai-completions: https://example.com}, api_key: k2, sticky_ttl: 2h}
models:
  defaulted:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
  disabled:
    sticky: false
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
  overridden:
    endpoints:
      openai-completions:
        - {providers: [p2], models: [m1]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !snap.Models["openai-completions"]["defaulted"].Sticky {
		t.Error("Sticky should default to true when unset")
	}
	if snap.Models["openai-completions"]["disabled"].Sticky {
		t.Error("explicit sticky: false must resolve to false")
	}
	if got := snap.Models["openai-completions"]["defaulted"].Endpoints[0].StickyTTL; got != 10*time.Minute {
		t.Errorf("endpoint with no override: StickyTTL = %v, want the global 10m default", got)
	}
	if got := snap.Models["openai-completions"]["overridden"].Endpoints[0].StickyTTL; got != 2*time.Hour {
		t.Errorf("endpoint with an override: StickyTTL = %v, want 2h", got)
	}
}

// TestBuildSnapshotSplitsVirtualModelByProtocol pins the new schema's
// cross-protocol reuse: one virtual model name with endpoint-groups on both
// protocols must resolve into two independent routes (Snapshot.Models
// ["openai-completions"]["vm"] and ["anthropic-messages"]["vm"]), each carrying only its own
// protocol's endpoints, sharing the model-level Sticky/Strategy/
// ImageDownscaleMaxPx settings.
func TestBuildSnapshotSplitsVirtualModelByProtocol(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://o.example, anthropic-messages: https://a.example}, api_key: k1}
models:
  vm:
    sticky: false
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m-openai]}
      anthropic-messages:
        - {providers: [p1], models: [m-anthropic]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	openaiRoute := snap.Models["openai-completions"]["vm"]
	anthropicRoute := snap.Models["anthropic-messages"]["vm"]
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
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k1}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [model-a, model-b, model-c]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eps := snap.Models["openai-completions"]["vm"].Endpoints
	want := []string{"model-a", "model-b", "model-c"}
	if len(eps) != len(want) {
		t.Fatalf("got %d endpoints, want %d", len(eps), len(want))
	}
	for i, w := range want {
		if eps[i].Model != w {
			t.Errorf("endpoint[%d].Model = %q, want %q", i, eps[i].Model, w)
		}
		if eps[i].Provider != "p1" || eps[i].AdapterType != "openai-completions" {
			t.Errorf("endpoint[%d]: provider=%q adapterType=%q", i, eps[i].Provider, eps[i].AdapterType)
		}
	}
}
