// Ver 2026-10-13 08:12, by Claude Opus 5.5

package server

import (
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
