// Ver 2026-08-19, by Sonnet 5

// BuildSnapshot coverage for multi-provider endpoint groups
// (endpoints[].providers) and global fallback endpoints
// (Config.FallbackEndpoints). Deferred tiers (multi-key pooling, graduated
// failover) and why: docs/KNOWN_ISSUES_sonnet-5.md's "配置与协议" section.
package router

import (
	"fmt"
	"testing"

	"vmr/internal/config"
	"vmr/internal/core"

	_ "vmr/internal/adapter/openai"
)

const multiProviderYAML = `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai-completions: https://p1.example.com}
    api_key: k1
    quota:
      limits: [{metric: requests, every: 1mo, since: 2026-08-01, amount: 1000}]
  - name: p2
    base_url: {openai-completions: https://p2.example.com}
    api_key: k2
models:
  vm:
    endpoints:
      - protocol: openai-completions
        providers: [p1, p2]
        models: [model-a, model-b]
        priority: 1
`

// TestBuildSnapshot_MultiProvider_ExpandsModelMajor pins the documented
// expansion order — outer loop over Models, inner loop over
// Providers — so every provider is tried for the preferred model
// before falling through to the next model: p1/model-a, p2/model-a,
// p1/model-b, p2/model-b, in that order.
func TestBuildSnapshot_MultiProvider_ExpandsModelMajor(t *testing.T) {
	cfg, err := config.Parse([]byte(multiProviderYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eps := snap.Models["openai-completions"]["vm"].Endpoints
	type pm struct{ provider, model string }
	want := []pm{{"p1", "model-a"}, {"p2", "model-a"}, {"p1", "model-b"}, {"p2", "model-b"}}
	if len(eps) != len(want) {
		t.Fatalf("got %d endpoints, want %d", len(eps), len(want))
	}
	for i, w := range want {
		if eps[i].Provider != w.provider || eps[i].Model != w.model {
			t.Errorf("endpoint[%d] = %s/%s, want %s/%s", i, eps[i].Provider, eps[i].Model, w.provider, w.model)
		}
		if eps[i].AdapterType != "openai-completions" {
			t.Errorf("endpoint[%d].AdapterType = %q, want openai", i, eps[i].AdapterType)
		}
		if eps[i].Priority != 1 {
			t.Errorf("endpoint[%d].Priority = %d, want 1 (shared by the whole entry)", i, eps[i].Priority)
		}
	}
}

// TestBuildSnapshot_MultiProvider_BaseURLAndAPIKeyPerProvider pins that
// each expanded endpoint resolves ITS OWN named provider's base_url/api_key
// — not some shared/first-provider value, which was the pre-existing bug
// class this feature's implementation had to avoid.
func TestBuildSnapshot_MultiProvider_BaseURLAndAPIKeyPerProvider(t *testing.T) {
	cfg, err := config.Parse([]byte(multiProviderYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range snap.Models["openai-completions"]["vm"].Endpoints {
		switch ep.Provider {
		case "p1":
			if ep.BaseURL != "https://p1.example.com" || ep.APIKey != "k1" {
				t.Errorf("p1 endpoint %s: BaseURL=%q APIKey=%q, want p1's own values", ep.Model, ep.BaseURL, ep.APIKey)
			}
		case "p2":
			if ep.BaseURL != "https://p2.example.com" || ep.APIKey != "k2" {
				t.Errorf("p2 endpoint %s: BaseURL=%q APIKey=%q, want p2's own values", ep.Model, ep.BaseURL, ep.APIKey)
			}
		}
	}
}

// TestBuildSnapshot_MultiProvider_QuotaPerProvider pins that quota
// resolution keys off each expanded endpoint's OWN provider name — p1's
// endpoints share p1's QuotaSpec pointer, p2's endpoints get nil (p2 has no
// quota: configured), matching how a single-provider entry already behaves
// (TestBuildSnapshot_QuotaSpecAttachedAndSharedPerProvider).
func TestBuildSnapshot_MultiProvider_QuotaPerProvider(t *testing.T) {
	cfg, err := config.Parse([]byte(multiProviderYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var p1Quota *core.QuotaSpec
	for _, ep := range snap.Models["openai-completions"]["vm"].Endpoints {
		switch ep.Provider {
		case "p1":
			if ep.Quota == nil {
				t.Fatalf("p1 endpoint %s: Quota is nil, want the configured spec", ep.Model)
			}
			if p1Quota == nil {
				p1Quota = ep.Quota
			} else if p1Quota != ep.Quota {
				t.Errorf("p1's two endpoints have different *core.QuotaSpec pointers")
			}
		case "p2":
			if ep.Quota != nil {
				t.Errorf("p2 endpoint %s: Quota = %+v, want nil", ep.Model, ep.Quota)
			}
		}
	}
}

// TestBuildSnapshot_MultiProvider_HealthKeysAllDistinct pins that the four
// expanded endpoints never collide in the health registry — each carries a
// distinct (protocol, provider, model, api-key-hash) tuple.
func TestBuildSnapshot_MultiProvider_HealthKeysAllDistinct(t *testing.T) {
	cfg, err := config.Parse([]byte(multiProviderYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, ep := range snap.Models["openai-completions"]["vm"].Endpoints {
		key := ep.HealthKey()
		if seen[key] {
			t.Errorf("duplicate HealthKey %q for endpoint %s/%s", key, ep.Provider, ep.Model)
		}
		seen[key] = true
	}
	if len(seen) != 4 {
		t.Fatalf("got %d distinct health keys, want 4", len(seen))
	}
}

const fallbackSnapYAML = `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://p1.example.com, anthropic-messages: https://p1.example.com}, api_key: k1}
  - {name: fb, base_url: {openai-completions: https://fb.example.com}, api_key: kfb}
fallback_endpoints:
  - protocol: openai-completions
    providers: [fb]
    models: [fallback-model]
    priority: 90
models:
  openai_only:
    capabilities: [text, tools]
    max_context_tokens: 128000
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [own-model]}
  anthropic_only:
    endpoints:
      - {protocol: anthropic-messages, providers: [p1], models: [own-model]}
  opted_out:
    fallback: false
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [own-model]}
`

func TestBuildSnapshot_Fallback_InjectedWhenProtocolMatches(t *testing.T) {
	cfg, err := config.Parse([]byte(fallbackSnapYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eps := snap.Models["openai-completions"]["openai_only"].Endpoints
	if len(eps) != 2 {
		t.Fatalf("got %d endpoints, want 2 (own + injected fallback)", len(eps))
	}
	if eps[0].Provider != "p1" || eps[0].Model != "own-model" || eps[0].FromFallback {
		t.Errorf("endpoint[0] = %+v, want the model's own endpoint with FromFallback=false", eps[0])
	}
	if eps[1].Provider != "fb" || eps[1].Model != "fallback-model" || !eps[1].FromFallback {
		t.Errorf("endpoint[1] = %+v, want the injected fallback with FromFallback=true", eps[1])
	}
	if eps[1].Priority != 90 {
		t.Errorf("injected fallback Priority = %d, want 90", eps[1].Priority)
	}
}

// TestBuildSnapshot_Fallback_NotInjectedOnMismatchedProtocol pins the
// "augments, never opens a new ingress" rule: a model with only an
// anthropic-messages entry gets no openai-completions route at all, so the
// openai-completions fallback has nothing to attach to.
func TestBuildSnapshot_Fallback_NotInjectedOnMismatchedProtocol(t *testing.T) {
	cfg, err := config.Parse([]byte(fallbackSnapYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap.Models["openai-completions"]["anthropic_only"]; ok {
		t.Fatalf("anthropic_only model must have no openai-completions route at all")
	}
	eps := snap.Models["anthropic-messages"]["anthropic_only"].Endpoints
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1 (own endpoint only, no fallback — fallback is openai-completions)", len(eps))
	}
}

func TestBuildSnapshot_Fallback_OptOut(t *testing.T) {
	cfg, err := config.Parse([]byte(fallbackSnapYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eps := snap.Models["openai-completions"]["opted_out"].Endpoints
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1 (fallback: false must suppress injection)", len(eps))
	}
	if eps[0].FromFallback {
		t.Errorf("opted_out's only endpoint must not be FromFallback")
	}
}

// TestBuildSnapshot_Fallback_InheritsModelCapabilitiesBase pins that a
// fallback entry merges against the SAME virtual model's own base
// capabilities/max_context_tokens as an ordinary endpoint-group would —
// there's no separate "fallback base", it's whichever model it attached to.
func TestBuildSnapshot_Fallback_InheritsModelCapabilitiesBase(t *testing.T) {
	cfg, err := config.Parse([]byte(fallbackSnapYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eps := snap.Models["openai-completions"]["openai_only"].Endpoints
	fb := eps[1]
	if fb.MaxContextTokens != 128000 {
		t.Errorf("fallback endpoint MaxContextTokens = %d, want 128000 (inherited from openai_only's base)", fb.MaxContextTokens)
	}
	if len(fb.Capabilities) != 2 {
		t.Errorf("fallback endpoint Capabilities = %v, want the model's [text, tools] base", fb.Capabilities)
	}
}

// TestServe_FailoverIntoInjectedFallback is the end-to-end payoff: a
// model's own endpoint fails, and Serve's ordinary failover loop reaches a
// global-fallbacks-injected endpoint exactly like it would any other
// candidate — no special-cased routing path for fallback-origin endpoints.
func TestServe_FailoverIntoInjectedFallback(t *testing.T) {
	own := newMockUpstream(t, 500, `{"error":"e1"}`)
	fb := newMockUpstream(t, 200, `{"id":"ok","model":"fallback-model"}`)

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1}
  - {name: fb, base_url: {openai-completions: %s}, api_key: kfb}
fallback_endpoints:
  - {protocol: openai-completions, providers: [fb], models: [fallback-model], priority: 90}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [own-model]}
`, own.srv.URL, fb.srv.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if got := w.Header().Get("X-VMR-Endpoint"); got != "openai-completions/fb/fallback-model" {
		t.Errorf("endpoint=%s, want openai-completions/fb/fallback-model", got)
	}
	if own.hits != 1 || fb.hits != 1 {
		t.Errorf("hits: own=%d fb=%d, want 1/1", own.hits, fb.hits)
	}
}
