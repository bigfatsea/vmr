// Ver 2026-10-13 08:12, by Claude Opus 5.5

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func listModels(t *testing.T, url string) map[string]string {
	t.Helper()
	resp, err := http.Get(url + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range out.Data {
		got[m.ID] = m.OwnedBy
	}
	return got
}

// TestDirectRouting_ModelsListing: /v1/models adds the provider:model names
// (owned_by = the provider part) only when direct_routing is on.
func TestDirectRouting_ModelsListing(t *testing.T) {
	u := newUpstream(t)
	on := listModels(t, newRouterServer(t, twoEndpointYAML(u.srv.URL, u.srv.URL, "direct_routing: true")).URL)
	want := map[string]string{"vm": "vmr", "p1:model-one": "p1", "p2:model-two": "p2"}
	if len(on) != len(want) {
		t.Errorf("models = %v, want %v", on, want)
	}
	for id, owner := range want {
		if on[id] != owner {
			t.Errorf("model %q owned_by = %q, want %q (all: %v)", id, on[id], owner, on)
		}
	}
	if off := listModels(t, newRouterServer(t, twoEndpointYAML(u.srv.URL, u.srv.URL, "")).URL); len(off) != 1 {
		t.Errorf("direct_routing off: models = %v, want only vm", off)
	}
}

// TestDirectRouting_ChatThroughServer drives the whole HTTP stack: the
// server layer's own route lookups (image downscale, guard exemption) and
// the router must all resolve the direct name, the upstream must receive
// its real model name, and the client must see the name it sent.
func TestDirectRouting_ChatThroughServer(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	ts := newRouterServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, "direct_routing: true"))
	resp, body := chat(t, ts, `{"model":"p2:model-two","messages":[{"role":"user","content":"hi"}]}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if u1.hits.Load() != 0 || u2.hits.Load() != 1 {
		t.Errorf("hits u1=%d u2=%d, want 0/1 (p1 has the better priority; the name alone must pick p2)", u1.hits.Load(), u2.hits.Load())
	}
	if got, _ := u2.lastModel.Load().(string); got != "model-two" {
		t.Errorf("upstream saw model %q, want the real model-two", got)
	}
	if got := extractModelField([]byte(body)); got != "p2:model-two" {
		t.Errorf("client saw model %q, want p2:model-two", got)
	}
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai-completions:p2:model-two" {
		t.Errorf("X-VMR-Endpoint = %q", got)
	}
}

// TestDirectRouting_StatusListsRoutes: /status carries the direct names in a
// top-level direct_routes block (help.html reads it) — never inside the
// "models" topology — and omits the block while direct_routing is off.
func TestDirectRouting_StatusListsRoutes(t *testing.T) {
	u := newUpstream(t)
	status := func(extra string) map[string]json.RawMessage {
		ts := newRouterServer(t, twoEndpointYAML(u.srv.URL, u.srv.URL, extra))
		resp, err := http.Get(ts.URL + "/status")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	on := status("direct_routing: true")
	var routes []struct {
		Name      string   `json:"name"`
		Provider  string   `json:"provider"`
		Protocols []string `json:"protocols"`
	}
	if err := json.Unmarshal(on["direct_routes"], &routes); err != nil {
		t.Fatalf("direct_routes: %v (raw %s)", err, on["direct_routes"])
	}
	if len(routes) != 2 || routes[0].Name != "p1:model-one" || routes[0].Provider != "p1" ||
		routes[1].Name != "p2:model-two" || len(routes[0].Protocols) != 1 || routes[0].Protocols[0] != "openai-completions" {
		t.Errorf("direct_routes = %+v, want p1:model-one then p2:model-two under openai-completions", routes)
	}
	if bytes.Contains(on["models"], []byte(`"id":"p1:model-one"`)) {
		t.Errorf("direct names leaked into the models topology: %s", on["models"])
	}
	if _, ok := status("")["direct_routes"]; ok {
		t.Error("direct_routing off: /status must omit direct_routes")
	}
}

// An api_keys group name nests its per-key names as members in /status.
func TestDirectRouting_StatusNestsPerKeyNames(t *testing.T) {
	u := newUpstream(t)
	yaml := `
listen: 127.0.0.1:0
direct_routing: true
providers:
  - {name: pool, base_url: {openai-completions: ` + u.srv.URL + `}, api_keys: {a: ka, b: kb}}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [pool], models: [m]}
`
	ts := newRouterServer(t, yaml)
	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		DirectRoutes []struct {
			Name    string `json:"name"`
			Keys    int    `json:"keys"`
			Members []struct {
				Name string `json:"name"`
			} `json:"members"`
		} `json:"direct_routes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.DirectRoutes) != 1 || out.DirectRoutes[0].Name != "pool:m" || out.DirectRoutes[0].Keys != 2 ||
		len(out.DirectRoutes[0].Members) != 2 || out.DirectRoutes[0].Members[0].Name != "pool-a:m" || out.DirectRoutes[0].Members[1].Name != "pool-b:m" {
		t.Errorf("direct_routes = %+v, want one root pool:m (2 keys) with members pool-a:m, pool-b:m", out.DirectRoutes)
	}
}
