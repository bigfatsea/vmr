// Ver 2026-08-24, by ox-alpha

// The /status models array is a cross-surface contract: cmd/vmr's
// statusResponse and status.html's renderModels are both hand-written
// against its shape (JSON is the contract — no shared Go type). This test
// pins it: model entries carry exactly the five keys below; endpoint
// entries draw only from the allowed key set, and the always-present keys
// are there even when zero/empty ([] capabilities, 0 max_context_tokens =
// unconstrained). A shape change that would break a consumer fails here.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"vmr/internal/config"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

func adminStatusModels(t *testing.T, yaml string) []map[string]any {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	ts := httptest.NewServer(New(rt, nil).Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Models
}

// TestAdminStatusModelsShapeContract locks the models-array shape the CLI
// and dashboard parse by hand.
func TestAdminStatusModelsShapeContract(t *testing.T) {
	const yaml = `
listen: 127.0.0.1:18800
providers:
  - {name: p1, base_url: {openai: http://127.0.0.1:1, anthropic: http://127.0.0.1:1}, api_key: k}
models:
  a:
    endpoints:
      - {protocol: openai, providers: [p1], models: [m1]}
      - {protocol: anthropic, providers: [p1], models: [m2]}
`
	models := adminStatusModels(t, yaml)

	// One entry per (model, protocol) face, sorted for deterministic JSON.
	if len(models) != 2 {
		t.Fatalf("models = %d entries, want 2 (a [anthropic], a [openai])", len(models))
	}
	if id, _ := models[0]["id"].(string); id != "a" {
		t.Errorf("models[0].id = %v, want a", models[0]["id"])
	}
	if proto, _ := models[0]["protocol"].(string); proto != "anthropic" {
		t.Errorf("models[0].protocol = %v, want anthropic (protocols sorted)", models[0]["protocol"])
	}

	wantModelKeys := "capabilities,endpoints,id,max_context_tokens,protocol"
	for i, m := range models {
		got := make([]string, 0, len(m))
		for k := range m {
			got = append(got, k)
		}
		sort.Strings(got)
		if gotJoined := strings.Join(got, ","); gotJoined != wantModelKeys {
			t.Errorf("models[%d] keys = %q, want exactly %q", i, gotJoined, wantModelKeys)
		}

		// Unconstrained must still be present — as [], never null/absent.
		caps, ok := m["capabilities"].([]any)
		if !ok || caps == nil {
			t.Errorf("models[%d].capabilities = %#v, want an (empty) array", i, m["capabilities"])
		}
		if _, ok := m["max_context_tokens"].(float64); !ok {
			t.Errorf("models[%d].max_context_tokens = %#v, want a number", i, m["max_context_tokens"])
		}

		endpoints, ok := m["endpoints"].([]any)
		if !ok || len(endpoints) == 0 {
			t.Fatalf("models[%d].endpoints = %#v, want a non-empty array", i, m["endpoints"])
		}
		for j, e := range endpoints {
			ep, ok := e.(map[string]any)
			if !ok {
				t.Fatalf("models[%d].endpoints[%d] = %#v, want an object", i, j, e)
			}
			required := []string{
				"endpoint", "protocol", "priority", "consecutive_failures",
				"available", "serving", "capabilities", "max_context_tokens",
			}
			for _, k := range required {
				if _, ok := ep[k]; !ok {
					t.Errorf("models[%d].endpoints[%d] missing always-present key %q", i, j, k)
				}
			}
			// This fixture declares no capabilities on either endpoint, so
			// this also pins the nil -> [] normalization: a bare presence
			// check above would not catch capabilities silently marshaling
			// as JSON null instead of [].
			if caps, ok := ep["capabilities"].([]any); !ok || caps == nil {
				t.Errorf("models[%d].endpoints[%d].capabilities = %#v, want an (empty) array, not null", i, j, ep["capabilities"])
			}
			for k := range ep {
				switch k {
				case "endpoint", "protocol", "priority", "consecutive_failures",
					"available", "serving", "capabilities", "max_context_tokens",
					"cooldown_until", "last_error", "probing":
				default:
					t.Errorf("models[%d].endpoints[%d] has unexpected key %q (update this contract test together with the CLI/dashboard consumers)", i, j, k)
				}
			}
		}
	}
}

// TestAdminStatusModelsAggregateValues checks the model-level aggregation
// semantics on a model whose endpoints disagree: capabilities = union,
// max_context_tokens = max.
func TestAdminStatusModelsAggregateValues(t *testing.T) {
	const yaml = `
listen: 127.0.0.1:18800
providers:
  - {name: p1, base_url: {openai: http://127.0.0.1:1}, api_key: k}
models:
  vm:
    capabilities: [text]
    max_context_tokens: 128000
    endpoints:
      - {protocol: openai, providers: [p1], models: [m1], capabilities: [vision], max_context_tokens: 200000}
`
	models := adminStatusModels(t, yaml)
	if len(models) != 1 {
		t.Fatalf("models = %d entries, want 1", len(models))
	}
	m := models[0]
	caps, _ := m["capabilities"].([]any)
	var capStrs []string
	for _, c := range caps {
		capStrs = append(capStrs, c.(string))
	}
	if strings.Join(capStrs, ",") != "text,vision" {
		t.Errorf("capabilities = %v, want union [text vision] sorted", capStrs)
	}
	if ctx, _ := m["max_context_tokens"].(float64); ctx != 200000 {
		t.Errorf("max_context_tokens = %v, want 200000 (max across endpoints)", ctx)
	}
}
