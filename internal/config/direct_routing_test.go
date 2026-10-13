// Ver 2026-10-13 08:12, by Claude Opus 5.5

package config

import (
	"strings"
	"testing"
)

const directRoutingYAML = `
listen: 127.0.0.1:0
direct_routing: %s
providers:
  - name: plain
    base_url: {openai-completions: https://plain.example.com}
    api_key: k-plain
  - name: pool
    base_url: {openai-completions: https://pool.example.com}
    api_keys: {a: k-a, b: k-b}
models:
  %s: {endpoints: {openai-completions: [{providers: [plain, pool], models: [x]}]}}
`

func parseDirect(enabled, modelName string) (*Config, error) {
	y := strings.Replace(directRoutingYAML, "%s", enabled, 1)
	y = strings.Replace(y, "%s", modelName, 1)
	return Parse([]byte(y))
}

// TestProviderGroup_SetByExpansion pins the pre-expansion name every
// Provider carries: itself for a plain api_key, the written name for each
// expanded api_keys child — what direct routing's "<group>:model" resolves.
func TestProviderGroup_SetByExpansion(t *testing.T) {
	cfg, err := parseDirect("false", "m")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, p := range cfg.Providers {
		got[p.Name] = p.GroupName()
	}
	want := map[string]string{"plain": "plain", "pool-a": "pool", "pool-b": "pool"}
	for name, g := range want {
		if got[name] != g {
			t.Errorf("provider %q group = %q, want %q (all: %v)", name, got[name], g, got)
		}
	}
	for _, name := range []string{"plain", "pool", "pool-a"} {
		if !cfg.IsProviderName(name) {
			t.Errorf("IsProviderName(%q) = false", name)
		}
	}
	if cfg.IsProviderName("nope") {
		t.Error("IsProviderName(nope) = true")
	}
	if (Provider{Name: "lit"}).GroupName() != "lit" {
		t.Error("GroupName must fall back to Name for a Provider built without Parse")
	}
}

// TestDirectRouting_ModelNameCollision: with the switch on, a virtual model
// named "<provider>:..." would shadow (or be shadowed by) that provider's
// direct name — a load error. Off, or with a non-provider prefix, the same
// name is an ordinary virtual model.
func TestDirectRouting_ModelNameCollision(t *testing.T) {
	cases := []struct {
		enabled, model string
		wantErr        bool
	}{
		{"true", `"plain:x"`, true},
		{"true", `"pool:x"`, true},
		{"true", `"pool-a:x"`, true},
		{"false", `"plain:x"`, false},
		{"true", `"qwen:7b"`, false},
		{"true", "m", false},
	}
	for _, c := range cases {
		_, err := parseDirect(c.enabled, c.model)
		if (err != nil) != c.wantErr {
			t.Errorf("direct_routing=%s model=%s: err=%v, wantErr=%v", c.enabled, c.model, err, c.wantErr)
		}
		if err != nil && !strings.Contains(err.Error(), "direct_routing") {
			t.Errorf("error should name direct_routing: %v", err)
		}
	}
}
