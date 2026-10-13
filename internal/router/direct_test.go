// Ver 2026-10-13 08:12, by Claude Opus 5.5

package router

import (
	"fmt"
	"strings"
	"testing"
)

const directShapeYAML = `
listen: 127.0.0.1:0
direct_routing: %v
providers:
  - {name: plain, base_url: {openai-completions: https://plain.example.com, anthropic-messages: https://plain.example.com}, api_key: k-plain}
  - {name: pool, base_url: {openai-completions: https://pool.example.com}, api_keys: {a: k-a, b: k-b}}
  - {name: off, base_url: {openai-completions: https://off.example.com}, api_key: k-off, disabled: true}
  - {name: fb, base_url: {openai-completions: https://fb.example.com}, api_key: k-fb}
fallback_endpoints:
  openai-completions:
    - {providers: [fb], models: [z], priority: 9}
models:
  vm1:
    endpoints:
      openai-completions:
        - {providers: [plain, pool, off], models: [x], priority: 2}
  vm2:
    endpoints:
      openai-completions:
        - {providers: [plain], models: [x], priority: 5}
      anthropic-messages:
        - {providers: [plain], models: [y]}
`

// TestBuildDirectRoutes_Shape pins what the direct table contains: every
// declared (protocol, provider, model) triple — fallback included, disabled
// excluded — under its group name and, for api_keys entries, its own name;
// one endpoint per HealthKey however many groups declare it; no fallback
// injection; single-tier priority; and nothing on a protocol the pair was
// never declared under.
func TestBuildDirectRoutes_Shape(t *testing.T) {
	snap := mustSnapshot(t, mustConfig(t, fmt.Sprintf(directShapeYAML, true)))
	oc := snap.Direct["openai-completions"]
	wantCounts := map[string]int{"plain:x": 1, "pool:x": 2, "pool-a:x": 1, "pool-b:x": 1, "fb:z": 1}
	if len(oc) != len(wantCounts) {
		t.Errorf("openai-completions direct names = %v, want %v", snap.DirectNames(), wantCounts)
	}
	for name, n := range wantCounts {
		r, ok := oc[name]
		if !ok {
			t.Errorf("missing direct route %q", name)
			continue
		}
		if len(r.Endpoints) != n {
			t.Errorf("%q: %d endpoints, want %d", name, len(r.Endpoints), n)
		}
		if !r.Sticky {
			t.Errorf("%q: Sticky = false, want true", name)
		}
		for _, ep := range r.Endpoints {
			if ep.Priority != 0 || ep.FromFallback {
				t.Errorf("%q: endpoint %s Priority=%d FromFallback=%v, want 0/false", name, ep.Name(), ep.Priority, ep.FromFallback)
			}
		}
	}
	if _, ok := oc["off:x"]; ok {
		t.Error("disabled provider must not get a direct route")
	}
	if got := snap.Direct["anthropic-messages"]; len(got) != 1 || got["plain:y"] == nil {
		t.Errorf("anthropic-messages direct = %v, want only plain:y", got)
	}
	if _, ok := snap.Route("anthropic-messages", "plain:x"); ok {
		t.Error("plain:x was never declared under anthropic-messages; direct routing must not widen protocols")
	}

	// Shared identity with the virtual-model endpoints: health, sticky and
	// quota state must be one and the same.
	orig := map[string]bool{}
	for _, ep := range snap.Models["openai-completions"]["vm1"].Endpoints {
		orig[ep.HealthKey()] = true
	}
	for _, ep := range oc["pool:x"].Endpoints {
		if !orig[ep.HealthKey()] {
			t.Errorf("direct endpoint %s has a HealthKey no virtual-model endpoint has", ep.Name())
		}
	}
	if eps := oc["pool:x"].Endpoints; eps[0].Provider != "pool-a" || eps[1].Provider != "pool-b" {
		t.Errorf("pool:x order = %s, %s; want provider-sorted pool-a, pool-b", eps[0].Provider, eps[1].Provider)
	}
	if hk := snap.HealthKeys(); len(hk) == 0 {
		t.Fatal("HealthKeys empty")
	} else {
		for _, ep := range oc["fb:z"].Endpoints {
			if !hk[ep.HealthKey()] {
				t.Error("direct endpoint must not introduce a HealthKey outside the virtual-model set")
			}
		}
	}

	if got := snap.DirectNames(); strings.Join(got, ",") != "fb:z,plain:x,plain:y,pool-a:x,pool-b:x,pool:x" {
		t.Errorf("DirectNames = %v", got)
	}
}

// TestBuildDirectRoutes_GuardExemptionPerRoute: Agent Guard's
// all-trusted exemption is computed per direct route, so naming a trusted
// provider directly is exempt even when the virtual model (which can also
// land on an untrusted one) is not.
func TestBuildDirectRoutes_GuardExemptionPerRoute(t *testing.T) {
	snap := mustSnapshot(t, mustConfig(t, `
listen: 127.0.0.1:0
direct_routing: true
guard: {trusted_providers: [p1]}
providers:
  - {name: p1, base_url: {openai-completions: https://p1.example.com}, api_key: k1}
  - {name: p2, base_url: {openai-completions: https://p2.example.com}, api_key: k2}
models:
  vm: {endpoints: {openai-completions: [{providers: [p1, p2], models: [m]}]}}
`))
	if r, _ := snap.Route("openai-completions", "vm"); r.GuardAllTrusted {
		t.Error("vm reaches untrusted p2; must not be exempt")
	}
	if r, _ := snap.Route("openai-completions", "p1:m"); !r.GuardAllTrusted {
		t.Error("p1:m reaches only trusted p1; must be exempt")
	}
	if r, _ := snap.Route("openai-completions", "p2:m"); r.GuardAllTrusted {
		t.Error("p2:m reaches untrusted p2; must not be exempt")
	}
}

func TestBuildDirectRoutes_OffByDefault(t *testing.T) {
	snap := mustSnapshot(t, mustConfig(t, fmt.Sprintf(directShapeYAML, false)))
	if snap.Direct != nil {
		t.Fatalf("Direct = %v, want nil with direct_routing off", snap.Direct)
	}
	if _, ok := snap.Route("openai-completions", "plain:x"); ok {
		t.Error("Route must not resolve a direct name with direct_routing off")
	}
	if r, ok := snap.Route("openai-completions", "vm1"); !ok || r == nil {
		t.Error("Route must still resolve virtual models")
	}
	if _, ok := (*Snapshot)(nil).Route("openai-completions", "vm1"); ok {
		t.Error("nil Snapshot Route must report not found")
	}
}

// TestServe_DirectRouting walks the request path: a direct name reaches only
// its own upstream (no failover onto the virtual model's other endpoints or
// fallback), the response's model field carries the name the client sent,
// and the three 404 shapes explain themselves.
func TestServe_DirectRouting(t *testing.T) {
	u1 := newMockUpstream(t, 200, `{"id":"a","model":"m1"}`)
	u2 := newMockUpstream(t, 500, `{"error":"down"}`)
	fb := newMockUpstream(t, 200, `{"id":"f","model":"mf"}`)
	yaml := `
listen: 127.0.0.1:0
direct_routing: %v
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1}
  - {name: p2, base_url: {openai-completions: %s}, api_key: k2}
  - {name: pf, base_url: {openai-completions: %s}, api_key: kf}
  - {name: pa, base_url: {anthropic-messages: https://example.com/v1}, api_key: ka}
fallback_endpoints:
  openai-completions:
    - {providers: [pf], models: [mf], priority: 9}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1], priority: 1}
        - {providers: [p2], models: [m2], priority: 2}
  claude:
    endpoints:
      anthropic-messages:
        - {providers: [pa], models: [ma]}
`
	rt := New(nil)
	rt.Install(mustSnapshot(t, mustConfig(t, fmt.Sprintf(yaml, true, u1.srv.URL, u2.srv.URL, fb.srv.URL))))

	w := serveReq(rt, "p1:m1", []byte(`{"model":"p1:m1"}`))
	if w.Code != 200 || u1.hits != 1 {
		t.Fatalf("p1:m1: status=%d hits=%d body=%s", w.Code, u1.hits, w.Body)
	}
	if got := w.Header().Get("X-VMR-Endpoint"); got != "openai-completions:p1:m1" {
		t.Errorf("X-VMR-Endpoint = %q", got)
	}
	if !strings.Contains(w.Body.String(), `"model":"p1:m1"`) {
		t.Errorf("response model should be rewritten to the requested direct name: %s", w.Body)
	}

	// p2 fails: the direct route must not fail over to p1 or the fallback.
	w = serveReq(rt, "p2:m2", []byte(`{"model":"p2:m2"}`))
	if w.Code != 500 || u2.hits != 1 || u1.hits != 1 || fb.hits != 0 {
		t.Fatalf("p2:m2: status=%d hits u1=%d u2=%d fb=%d, want 500 and no failover", w.Code, u1.hits, u2.hits, fb.hits)
	}

	// Fallback pairs are addressable in their own right.
	if w = serveReq(rt, "pf:mf", []byte(`{"model":"pf:mf"}`)); w.Code != 200 || fb.hits != 1 {
		t.Fatalf("pf:mf: status=%d fb hits=%d", w.Code, fb.hits)
	}

	for _, tc := range []struct{ model, want string }{
		{"pa:ma", "/v1/messages"},                      // declared under the other ingress
		{"p1:m2", "only reaches provider:model pairs"}, // provider known, pair undeclared
		{"nobody:m1", "models on this endpoint"},       // not a provider: plain not-found
	} {
		w = serveReq(rt, tc.model, []byte(`{"model":"`+tc.model+`"}`))
		if w.Code != 404 || !strings.Contains(w.Body.String(), tc.want) {
			t.Errorf("%s: status=%d body=%s, want 404 containing %q", tc.model, w.Code, w.Body, tc.want)
		}
	}

	rtOff := New(nil)
	rtOff.Install(mustSnapshot(t, mustConfig(t, fmt.Sprintf(yaml, false, u1.srv.URL, u2.srv.URL, fb.srv.URL))))
	w = serveReq(rtOff, "p1:m1", []byte(`{"model":"p1:m1"}`))
	if w.Code != 404 || !strings.Contains(w.Body.String(), "direct_routing: true") {
		t.Errorf("direct off: status=%d body=%s, want 404 naming direct_routing", w.Code, w.Body)
	}
}
