// Ver 2026-09-05, by Pkg-D
package router

import (
	"testing"

	"gopkg.in/yaml.v3"

	"vmr/internal/config"

	_ "vmr/internal/adapter/anthropic"
	_ "vmr/internal/adapter/openai"
)

// Differential equivalence test for the D.1/D.2 config-shape change: the
// endpoints/fallback_endpoints sections moved from a flat list with a
// per-entry `protocol:` field to protocol-keyed maps
// (map[string][]config.EndpointGroup). The shape change must be exactly a
// shape change — the routing table a config expands to may not depend on
// which form it was written in. This file pins that with a differential
// test rather than an argument: the same logical config is expanded twice,
// once through the legacy shape (parsed here with a local struct carrying
// the old `protocol` field, then converted) and once through the new shape
// (parsed by config.Parse itself), and the resulting
// (protocol, provider, model, priority) sequences must be identical.

// legacyEndpointGroup mirrors the pre-map EndpointGroup: a flat list entry
// carrying its own protocol.
type legacyEndpointGroup struct {
	Protocol          string            `yaml:"protocol"`
	Providers         []string          `yaml:"providers"`
	Models            []string          `yaml:"models"`
	Priority          int               `yaml:"priority"`
	RoleMap           map[string]string `yaml:"role_map"`
	StickyTTL         *config.Duration  `yaml:"sticky_ttl"`
	SoftBlockFailover *bool             `yaml:"soft_block_failover"`
}

type legacyVirtualModel struct {
	Strategy            []string              `yaml:"strategy"`
	Endpoints           []legacyEndpointGroup `yaml:"endpoints"`
	Capabilities        []string              `yaml:"capabilities"`
	MaxContextTokens    int64                 `yaml:"max_context_tokens"`
	Sticky              *bool                 `yaml:"sticky"`
	Fallback            *bool                 `yaml:"fallback"`
	SoftBlockFailover   *bool                 `yaml:"soft_block_failover"`
	ImageDownscaleMaxPx *int                  `yaml:"image_downscale"`
}

type legacyConfig struct {
	Providers         []config.Provider             `yaml:"providers"`
	Models            map[string]legacyVirtualModel `yaml:"models"`
	FallbackEndpoints []legacyEndpointGroup         `yaml:"fallback_endpoints"`
}

// toNew converts the legacy shape into the current config.Config shape:
// list entries are bucketed by their own protocol, order within a bucket
// preserved. This mirrors what the old BuildSnapshot did at expansion time
// (routes[eg.Protocol]) — the difference is that it now happens once here,
// in the shape migration, instead of on every snapshot build.
func (l *legacyConfig) toNew(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{Providers: l.Providers, FallbackEndpoints: map[string][]config.EndpointGroup{}}
	for protocol, groups := range bucketByProtocol(t, l.FallbackEndpoints) {
		cfg.FallbackEndpoints[protocol] = groups
	}
	cfg.Models = map[string]config.VirtualModel{}
	for name, lm := range l.Models {
		m := config.VirtualModel{
			Strategy:            lm.Strategy,
			Capabilities:        lm.Capabilities,
			MaxContextTokens:    lm.MaxContextTokens,
			Sticky:              lm.Sticky,
			Fallback:            lm.Fallback,
			SoftBlockFailover:   lm.SoftBlockFailover,
			ImageDownscaleMaxPx: lm.ImageDownscaleMaxPx,
			Endpoints:           map[string][]config.EndpointGroup{},
		}
		for protocol, groups := range bucketByProtocol(t, lm.Endpoints) {
			m.Endpoints[protocol] = groups
		}
		cfg.Models[name] = m
	}
	return cfg
}

func bucketByProtocol(t *testing.T, legacy []legacyEndpointGroup) map[string][]config.EndpointGroup {
	t.Helper()
	out := map[string][]config.EndpointGroup{}
	for _, le := range legacy {
		if le.Protocol == "" {
			t.Fatal("legacy endpoint group without protocol")
		}
		ne := config.EndpointGroup{
			Providers:         le.Providers,
			Models:            le.Models,
			Priority:          le.Priority,
			RoleMap:           le.RoleMap,
			StickyTTL:         le.StickyTTL,
			SoftBlockFailover: le.SoftBlockFailover,
		}
		out[le.Protocol] = append(out[le.Protocol], ne)
	}
	return out
}

const equivLegacyYAML = `
providers:
  - name: p1
    base_url: {openai-completions: https://p1.example.com/v1, anthropic-messages: https://p1.example.com/anthropic}
    api_key: k1
  - name: p2
    base_url: {openai-completions: https://p2.example.com/v1, anthropic-messages: https://p2.example.com/anthropic}
    api_key: k2
  - name: p3
    base_url: {openai-responses: https://p3.example.com/v1}
    api_key: k3
models:
  agent:
    capabilities: [text, tools]
    max_context_tokens: 256000
    endpoints:
      - protocol: openai-completions
        providers: [p1, p2]
        models: [m1, m2]
      - protocol: openai-completions
        providers: [p2]
        models: [m3]
        priority: 5
        sticky_ttl: 2h
        role_map: {developer: system}
      - protocol: anthropic-messages
        providers: [p1]
        models: [m4]
        soft_block_failover: true
      - protocol: openai-responses
        providers: [p3]
        models: [m5]
  claude:
    sticky: false
    fallback: false
    endpoints:
      - protocol: anthropic-messages
        providers: [p2]
        models: [m6]
fallback_endpoints:
  - protocol: openai-completions
    providers: [p2]
    models: [fb1]
    priority: 90
  - protocol: openai-completions
    providers: [p1]
    models: [fb2, fb3]
    priority: 95
  - protocol: anthropic-messages
    providers: [p2]
    models: [fb4]
    priority: 90
`

const equivNewYAML = `
providers:
  - name: p1
    base_url: {openai-completions: https://p1.example.com/v1, anthropic-messages: https://p1.example.com/anthropic}
    api_key: k1
  - name: p2
    base_url: {openai-completions: https://p2.example.com/v1, anthropic-messages: https://p2.example.com/anthropic}
    api_key: k2
  - name: p3
    base_url: {openai-responses: https://p3.example.com/v1}
    api_key: k3
models:
  agent:
    capabilities: [text, tools]
    max_context_tokens: 256000
    endpoints:
      openai-completions:
        - providers: [p1, p2]
          models: [m1, m2]
        - providers: [p2]
          models: [m3]
          priority: 5
          sticky_ttl: 2h
          role_map: {developer: system}
      anthropic-messages:
        - providers: [p1]
          models: [m4]
          soft_block_failover: true
      openai-responses:
        - providers: [p3]
          models: [m5]
  claude:
    sticky: false
    fallback: false
    endpoints:
      anthropic-messages:
        - providers: [p2]
          models: [m6]
fallback_endpoints:
  openai-completions:
    - providers: [p2]
      models: [fb1]
      priority: 90
    - providers: [p1]
      models: [fb2, fb3]
      priority: 95
  anthropic-messages:
    - providers: [p2]
      models: [fb4]
      priority: 90
`

// expansion is one (protocol, provider, model, priority) tuple in
// route-try order: per (protocol, virtual model), endpoints in the order
// BuildSnapshot appends them (own endpoints in config order, then the
// fallback tier).
type expansion struct {
	protocol, model, provider, upstream string
	priority                            int
}

func expansionSequence(t *testing.T, snap *Snapshot) []expansion {
	t.Helper()
	var out []expansion
	for _, protocol := range sortedProtocolKeys(snap) {
		for _, name := range sortedModelKeys(snap.Models[protocol]) {
			for _, ep := range snap.Models[protocol][name].Endpoints {
				out = append(out, expansion{protocol, name, ep.Provider, ep.Model, ep.Priority})
			}
		}
	}
	return out
}

func sortedProtocolKeys(snap *Snapshot) []string {
	keys := make([]string, 0, len(snap.Models))
	for k := range snap.Models {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func sortedModelKeys(byName map[string]*ModelRoute) []string {
	keys := make([]string, 0, len(byName))
	for k := range byName {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// TestSnapshotExpansionEquivalence_LegacyVsMapShape pins the D.1/D.2 shape
// migration: legacy list-with-protocol config and the map-keyed config must
// expand to the identical (protocol, provider, model, priority) sequence.
func TestSnapshotExpansionEquivalence_LegacyVsMapShape(t *testing.T) {
	var legacy legacyConfig
	if err := yaml.Unmarshal([]byte(equivLegacyYAML), &legacy); err != nil {
		t.Fatalf("legacy yaml: %v", err)
	}
	legacyCfg := legacy.toNew(t)
	legacySnap, err := BuildSnapshot(legacyCfg)
	if err != nil {
		t.Fatalf("legacy BuildSnapshot: %v", err)
	}

	newCfg, err := config.Parse([]byte(equivNewYAML))
	if err != nil {
		t.Fatalf("new-shape yaml: %v", err)
	}
	newSnap, err := BuildSnapshot(newCfg)
	if err != nil {
		t.Fatalf("new BuildSnapshot: %v", err)
	}

	got, want := expansionSequence(t, legacySnap), expansionSequence(t, newSnap)
	if len(got) != len(want) {
		t.Fatalf("expansion lengths differ: legacy %d, new %d\nlegacy: %+v\nnew: %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expansion[%d] differs:\nlegacy: %+v\nnew:    %+v\nfull legacy: %+v\nfull new: %+v", i, got[i], want[i], got, want)
		}
	}
	// Sanity: the fixture must actually exercise fallback injection,
	// multi-protocol models, and per-entry overrides — otherwise the
	// equivalence above is vacuous.
	if len(want) < 12 {
		t.Fatalf("fixture too small to be meaningful: %d expansions", len(want))
	}
	sawFallback := false
	for _, e := range want {
		if e.priority > 0 {
			sawFallback = true
		}
	}
	if !sawFallback {
		t.Fatal("fixture never exercised the fallback tier")
	}
}
