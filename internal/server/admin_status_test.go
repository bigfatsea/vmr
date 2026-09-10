// Ver 2026-08-24, by ox-alpha

// The /status models array is a cross-surface contract: cmd/vmr's
// statusResponse and status.html's renderModels are both hand-written
// against its shape (JSON is the contract — no shared Go type). This test
// pins it: model entries carry exactly the five keys below; endpoint
// entries draw only from the allowed key set, and the always-present keys
// are there even when zero/empty ([] capabilities, 0 max_context_tokens =
// unconstrained, from_fallback=false). A shape change that would break a
// consumer fails here.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/quota"
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
  - {name: p1, base_url: {openai-completions: http://127.0.0.1:1, anthropic-messages: http://127.0.0.1:1}, api_key: k}
models:
  a:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
      anthropic-messages:
        - {providers: [p1], models: [m2]}
`
	models := adminStatusModels(t, yaml)

	// One entry per (model, protocol) face, sorted for deterministic JSON.
	if len(models) != 2 {
		t.Fatalf("models = %d entries, want 2 (a [anthropic], a [openai])", len(models))
	}
	if id, _ := models[0]["id"].(string); id != "a" {
		t.Errorf("models[0].id = %v, want a", models[0]["id"])
	}
	if proto, _ := models[0]["protocol"].(string); proto != "anthropic-messages" {
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
				// G3/G5: identity split + fallback marker are always present —
				// from_fallback is the bool zero value here and must still emit.
				"provider", "key_label", "model", "from_fallback",
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
					"cooldown_until", "last_error", "probing",
					// G3/G5 identity split + fallback marker; G4 headroom is
					// omitempty and legitimately absent in this fixture (the
					// provider has no quota configured).
					"provider", "key_label", "model", "from_fallback":
				default:
					t.Errorf("models[%d].endpoints[%d] has unexpected key %q (update this contract test together with the CLI/dashboard consumers)", i, j, k)
				}
			}
			// G5: from_fallback is a plain bool, no omitempty — the false zero
			// value must still be present (the console splits the fallback
			// table on it and must be able to tell "false" from "absent").
			if fb, ok := ep["from_fallback"].(bool); !ok {
				t.Errorf("models[%d].endpoints[%d].from_fallback = %#v, want a bool", i, j, ep["from_fallback"])
			} else if fb {
				t.Errorf("models[%d].endpoints[%d].from_fallback = true, want false (non-fallback fixture)", i, j)
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
model_defaults:
  m1:
    capabilities: [text]
    max_context_tokens: 128000
  m2:
    capabilities: [vision]
    max_context_tokens: 200000
providers:
  - {name: p1, base_url: {openai-completions: http://127.0.0.1:1}, api_key: k}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1, m2]}
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

// TestAdminStatus_MetricsCaching verifies that directory size and disk free space
// are cached for 30 seconds rather than rescanned on every request.
func TestAdminStatus_MetricsCaching(t *testing.T) {
	logDir := t.TempDir()
	f1 := filepath.Join(logDir, "audit1.log")
	if err := os.WriteFile(f1, []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}

	size1 := cachedDirTotalSize(logDir)
	if size1 != 5 {
		t.Fatalf("cachedDirTotalSize(logDir) = %d, want 5", size1)
	}

	// Write another file immediately
	f2 := filepath.Join(logDir, "audit2.log")
	if err := os.WriteFile(f2, []byte("67890"), 0600); err != nil {
		t.Fatal(err)
	}

	// Within 30s TTL, it should still return the cached size (5)
	size2 := cachedDirTotalSize(logDir)
	if size2 != 5 {
		t.Errorf("cachedDirTotalSize(logDir) after new file = %d, want cached value 5", size2)
	}

	// Disk free space caching
	free1 := cachedDiskFreeSpace(logDir)
	free2 := cachedDiskFreeSpace(logDir)
	if free1 != free2 {
		t.Errorf("cachedDiskFreeSpace changed within TTL: %d vs %d", free1, free2)
	}
}

// endpointRow is the /status endpoint-row fields G3/G4/G5 add, decoded for
// assertion (decoded from the same response the contract test above pins).
type endpointRow struct {
	Endpoint     string   `json:"endpoint"`
	Provider     string   `json:"provider"`
	KeyLabel     string   `json:"key_label"`
	Model        string   `json:"model"`
	FromFallback bool     `json:"from_fallback"`
	Headroom     *float64 `json:"headroom"`
}

// fetchEndpointRows returns every endpoint row of the /status response
// keyed by the synthetic endpoint name.
func fetchEndpointRows(t *testing.T, rt *router.Router) map[string]endpointRow {
	t.Helper()
	out := fetchStatusRaw(t, rt)
	raw, ok := out["models"]
	if !ok {
		t.Fatalf("response missing \"models\": %v", out)
	}
	var models []struct {
		Endpoints []endpointRow `json:"endpoints"`
	}
	if err := json.Unmarshal(raw, &models); err != nil {
		t.Fatal(err)
	}
	rows := map[string]endpointRow{}
	for _, m := range models {
		for _, ep := range m.Endpoints {
			rows[ep.Endpoint] = ep
		}
	}
	return rows
}

// TestAdminStatus_EndpointIdentitySplit pins G3/G5: each row carries the
// core.Endpoint identity split (provider/key_label/model) alongside the
// unchanged synthetic endpoint name, and from_fallback marks fallback
// endpoints true while staying present (not omitted) for main ones.
func TestAdminStatus_EndpointIdentitySplit(t *testing.T) {
	// api_keys expansion is what gives key_label its label form: each entry
	// becomes its own provider (p1-main / p1-backup) with KeyLabel set to
	// the label verbatim, while the synthetic endpoint name keeps following
	// the (expanded) provider name unchanged.
	const yaml = `
listen: 127.0.0.1:18803
providers:
  - name: p1
    base_url: {openai-completions: http://127.0.0.1:1}
    api_keys: {main: k1secret, backup: k2secret}
models:
  vm:
    endpoints: {openai-completions: [{providers: [p1], models: [m1]}]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)

	out := fetchStatusRaw(t, rt)
	var models []struct {
		Endpoints []endpointRow `json:"endpoints"`
	}
	if err := json.Unmarshal(out["models"], &models); err != nil {
		t.Fatal(err)
	}
	type ident struct {
		provider, key, model string
		fb                   bool
	}
	got := map[ident]int{}
	for _, m := range models {
		for _, ep := range m.Endpoints {
			if ep.Model != "m1" {
				t.Fatalf("model = %q, want m1", ep.Model)
			}
			if ep.Endpoint != "openai-completions/"+ep.Provider+"/m1" {
				t.Fatalf("synthetic endpoint name %q doesn't follow provider %q (name must stay unchanged)", ep.Endpoint, ep.Provider)
			}
			got[ident{ep.Provider, ep.KeyLabel, ep.Model, ep.FromFallback}]++
		}
	}
	want := map[ident]int{
		{"p1-main", "main", "m1", false}:     1,
		{"p1-backup", "backup", "m1", false}: 1,
	}
	for id, n := range want {
		if got[id] != n {
			t.Fatalf("identity %+v count = %d, want %d (all rows: %v)", id, got[id], n, got)
		}
		delete(got, id)
	}
	for id := range got {
		t.Fatalf("unexpected endpoint identity %+v", id)
	}
}

// TestAdminStatus_FromFallbackMarked pins G5's marker on the fallback path:
// an endpoint injected from fallback_endpoints reports from_fallback=true
// while a main endpoint of the same model reports false — the split the
// Overview page needs to build the separate fallback table.
func TestAdminStatus_FromFallbackMarked(t *testing.T) {
	const yaml = `
listen: 127.0.0.1:18804
providers:
  - {name: p1, base_url: {openai-completions: http://127.0.0.1:1}, api_key: k1}
  - {name: p2, base_url: {openai-completions: http://127.0.0.1:2}, api_key: k2}
models:
  vm:
    endpoints: {openai-completions: [{providers: [p1], models: [m1]}]}
fallback_endpoints:
  openai-completions:
    - providers: [p2]
      models: [m1]
      priority: 99
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)

	rows := fetchEndpointRows(t, rt)
	main, ok := rows["openai-completions/p1/m1"]
	if !ok || main.FromFallback {
		t.Fatalf("main endpoint row = %+v, want from_fallback=false", main)
	}
	fb, ok := rows["openai-completions/p2/m1"]
	if !ok || !fb.FromFallback {
		t.Fatalf("fallback endpoint row = %+v, want from_fallback=true", fb)
	}
}

// TestAdminStatus_HeadroomJoinDifferential pins G4's differential discipline:
// the endpoint row's headroom must be numerically IDENTICAL to the headroom
// the quota section shows for the same account (both are rendered from one
// QuotaStatus() read — a reimplementation of the formula would drift the
// moment the score changes).
func TestAdminStatus_HeadroomJoinDifferential(t *testing.T) {
	cfg, err := config.Parse([]byte(quotaStatusYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Quota = quota.NewRegistry("")
	rt.Install(snap)

	// 50 of 500 requests in a monthly window: mid-period, headroom strictly
	// between 0 and the cap (used_frac 0.1 → warning threshold not crossed).
	l := snap.Models["openai-completions"]["vm"].Endpoints[0].Quota.Limits[0]
	rt.Quota.Charge("p1", "requests/1mo", quota.PeriodStart(l, time.Now()), quota.Counters{Requests: 50}, 0)

	// One fetch: adminStatus renders the quota section and the endpoint join
	// from a single QuotaStatus() read, so within one response the two MUST
	// be bit-identical. (Across two fetches the time-left fraction drifts a
	// few ulps — that's expected and is why the contract demands one read.)
	out := fetchStatusRaw(t, rt)
	var models []struct {
		Endpoints []endpointRow `json:"endpoints"`
	}
	if err := json.Unmarshal(out["models"], &models); err != nil {
		t.Fatal(err)
	}
	var ep endpointRow
	for _, m := range models {
		for _, e := range m.Endpoints {
			if e.Endpoint == "openai-completions/p1/m1" {
				ep = e
			}
		}
	}
	if ep.Endpoint == "" {
		t.Fatalf("missing endpoint row in %v", models)
	}
	if ep.Headroom == nil {
		t.Fatalf("headroom missing on a quota-configured endpoint")
	}
	var qrows []struct {
		Headroom float64 `json:"headroom"`
	}
	if err := json.Unmarshal(out["quota"], &qrows); err != nil {
		t.Fatal(err)
	}
	if len(qrows) != 1 {
		t.Fatalf("quota rows = %d, want 1", len(qrows))
	}
	if *ep.Headroom != qrows[0].Headroom {
		t.Fatalf("endpoint headroom %v != quota row headroom %v — the join must be same-source", *ep.Headroom, qrows[0].Headroom)
	}
	if *ep.Headroom <= 0 || *ep.Headroom >= quota.HeadroomCap {
		t.Fatalf("headroom = %v, want strictly inside (0, cap) for a mid-period account", *ep.Headroom)
	}
}

// TestAdminStatus_HeadroomExhaustedIsZeroNotOmitted pins the *float64 choice:
// an exhausted account's headroom IS 0.00 — the one state the console must
// show in red (§8.5 color scale). A plain float64+omitempty would erase it.
func TestAdminStatus_HeadroomExhaustedIsZeroNotOmitted(t *testing.T) {
	cfg, err := config.Parse([]byte(quotaStatusYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Quota = quota.NewRegistry("")
	rt.Install(snap)

	l := snap.Models["openai-completions"]["vm"].Endpoints[0].Quota.Limits[0]
	// Fully exhausted: used == amount → headroom 0.
	rt.Quota.Charge("p1", "requests/1mo", quota.PeriodStart(l, time.Now()), quota.Counters{Requests: 500}, 0)

	rows := fetchEndpointRows(t, rt)
	ep, ok := rows["openai-completions/p1/m1"]
	if !ok {
		t.Fatalf("missing endpoint row: %v", rows)
	}
	if ep.Headroom == nil {
		t.Fatalf("headroom omitted for an exhausted account — 0 must be emitted, not erased")
	}
	if *ep.Headroom != 0 {
		t.Fatalf("headroom = %v, want exactly 0 when the account is exhausted", *ep.Headroom)
	}
}

// TestAdminStatus_HeadroomOmittedWithoutQuota pins G4's omission contract:
// an unmetered provider's endpoint rows carry NO headroom key at all (the
// UI renders —), and a provider whose limits don't cover the endpoint's
// model behaves the same way.
func TestAdminStatus_HeadroomOmittedWithoutQuota(t *testing.T) {
	cfg, err := config.Parse([]byte(noQuotaYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap) // no quota.Registry wired, and none configured anyway

	rows := fetchEndpointRows(t, rt)
	ep, ok := rows["openai-completions/p1/m1"]
	if !ok {
		t.Fatalf("missing endpoint row: %v", rows)
	}
	if ep.Headroom != nil {
		t.Fatalf("headroom = %v, want the key absent for an unmetered account", *ep.Headroom)
	}
}
