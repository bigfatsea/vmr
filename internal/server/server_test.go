// Ver 2026-08-02, by Sonnet 5

// Integration tests: full handler + mock upstreams over real HTTP. Shared
// mock/fixture/client scaffolding lives in testhelpers_test.go and
// fixtures_test.go (docs/test_review_action_plan_sonnet-5.md Batch 3) — this
// file holds only the tests themselves.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFailoverOn5xxAndModelRewrite(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	u1.status.Store(500)
	ts := newRouterServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, ""))

	resp, body := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai/p2/model-two" {
		t.Errorf("endpoint: %s", got)
	}
	if got := resp.Header.Get("X-VMR-Attempts"); got != "2" {
		t.Errorf("attempts: %s", got)
	}
	if got := u2.lastModel.Load(); got != "model-two" {
		t.Errorf("model rewrite: upstream saw %v", got)
	}
	if u1.hits.Load() != 1 || u2.hits.Load() != 1 {
		t.Errorf("hits: u1=%d u2=%d", u1.hits.Load(), u2.hits.Load())
	}
}

func TestClientErrorDoesNotFailover(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	u1.status.Store(400)
	ts := newRouterServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, ""))

	resp, body := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 400 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if !strings.Contains(body, "upstream says 400") {
		t.Errorf("must return upstream error verbatim, got %s", body)
	}
	if u2.hits.Load() != 0 {
		t.Error("second endpoint must not be tried on ErrClient")
	}
}

// TestUpstreamGatewayFailureContinuesFailover reproduces reports/incident-
// 20260718-console-go-400-failover_Sonnet5.md end to end through the real
// Serve()/tryOne() path: an upstream relay reports its own forwarding
// failure (the literal body vmr actually saw, byte for byte), and that must
// classify as ErrEndpoint — continue failing over instead of stopping dead
// the way an ErrClient (genuine bad request) correctly does — AND the failed
// endpoint must get ErrEndpoint's long cooldown (health.longBase, minutes),
// not ErrTransient's short one, since a relay failure that returns
// immediately (not a timeout) still means "don't retry this soon". This is
// the test that would have caught internal/adapter/classify.go's
// misclassification bug directly, wired through the whole stack rather than
// just the classifier in isolation (see classify_test.go for that).
func TestUpstreamGatewayFailureContinuesFailover(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	u1.status.Store(400)
	u1.errBody.Store(`{"message":"Error from provider (Console Go): Upstream request failed","type":"invalid_request_error","param":null,"code":"invalid_request_error"}`)
	ts := newRouterServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, ""))

	resp, body := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s (must fail over past the relay failure, not return it to the client)", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai/p2/model-two" {
		t.Errorf("endpoint=%s, want p2 (p1's relay failure must not stop the failover walk)", got)
	}
	if got := resp.Header.Get("X-VMR-Attempts"); got != "2" {
		t.Errorf("attempts=%s, want 2", got)
	}
	if u2.hits.Load() != 1 {
		t.Errorf("p2 hits=%d, want 1 (failover must have reached it)", u2.hits.Load())
	}

	// The classification must also carry a real health consequence:
	// ErrEndpoint's long cooldown, not ErrClient's "say nothing about
	// health" or ErrTransient's short one.
	statusResp, err := http.Get(ts.URL + "/admin/status")
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()
	var out struct {
		Models map[string][]struct {
			Endpoint      string    `json:"endpoint"`
			Available     bool      `json:"available"`
			Fails         int       `json:"consecutive_failures"`
			LastError     string    `json:"last_error"`
			CooldownUntil time.Time `json:"cooldown_until"`
		} `json:"models"`
	}
	if err := json.NewDecoder(statusResp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	var p1 *struct {
		Endpoint      string    `json:"endpoint"`
		Available     bool      `json:"available"`
		Fails         int       `json:"consecutive_failures"`
		LastError     string    `json:"last_error"`
		CooldownUntil time.Time `json:"cooldown_until"`
	}
	for i, ep := range out.Models["vm [openai]"] {
		if ep.Endpoint == "openai/p1/model-one" {
			p1 = &out.Models["vm [openai]"][i]
		}
	}
	if p1 == nil {
		t.Fatal("p1 missing from /admin/status")
	}
	if p1.Available || p1.Fails != 1 || p1.LastError != "endpoint" {
		t.Errorf("p1 health = %+v, want cooling down, fails=1, last_error=endpoint", p1)
	}
	if wantMin := time.Now().Add(5 * time.Minute); p1.CooldownUntil.Before(wantMin) {
		t.Errorf("p1 cooldown_until=%s, want at least 5min out (ErrEndpoint's long cooldown, not a short transient one)", p1.CooldownUntil)
	}
}

// TestCooldownFiltersEndpointUntilRetryAfterExpires checks that a
// rate-limited endpoint is filtered out for the duration of Retry-After —
// recovery itself (the half-open endpoint getting probed and rejoining
// rotation) is covered by TestActiveProbe_RecoversInBackgroundThenServesRealTraffic
// in server_active_probe_test.go.
func TestCooldownFiltersEndpointUntilRetryAfterExpires(t *testing.T) {
	t.Parallel()
	u1, u2 := newUpstream(t), newUpstream(t)
	u1.status.Store(429)
	u1.retryAfter = "1"
	ts := newRouterServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, ""))

	// 1st request: 429 on p1 → served by p2; p1 enters cooldown.
	resp, _ := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai/p2/model-two" {
		t.Fatalf("first request: status=%d ep=%s", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}
	// 2nd request immediately: p1 filtered by cooldown, p2 hit directly.
	chat(t, ts, simpleReq, nil)
	if u1.hits.Load() != 1 {
		t.Errorf("p1 must not be hit during cooldown, hits=%d", u1.hits.Load())
	}
}

func TestAllFailReturnsLastUpstreamError(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	u1.status.Store(503)
	u2.status.Store(503)
	ts := newRouterServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, ""))

	resp, body := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 503 || !strings.Contains(body, "upstream says 503") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-VMR-Attempts"); got != "2" {
		t.Errorf("attempts: %s", got)
	}
	// Both now cooling down → no candidates at all.
	resp, body = chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 503 || !strings.Contains(body, "vmr_no_candidates") {
		t.Errorf("no-candidate case: status=%d body=%s", resp.StatusCode, body)
	}
}

func TestStreamingPassthrough(t *testing.T) {
	sse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "data: {\"chunk\":%d}\n\n", i)
			fl.Flush()
			time.Sleep(20 * time.Millisecond)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer sse.Close()
	dead := newUpstream(t)
	dead.status.Store(500)
	ts := newRouterServer(t, twoEndpointYAML(dead.srv.URL, sse.URL, ""))

	resp, body := chat(t, ts, `{"model":"vm","stream":true,"messages":[]}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type: %s", ct)
	}
	for i := 0; i < 3; i++ {
		if !strings.Contains(body, fmt.Sprintf(`{"chunk":%d}`, i)) {
			t.Errorf("missing chunk %d in %s", i, body)
		}
	}
	if !strings.Contains(body, "[DONE]") {
		t.Error("missing DONE marker")
	}
}

func TestRouterAuth(t *testing.T) {
	u := newUpstream(t)
	ts := newRouterServer(t, twoEndpointYAML(u.srv.URL, u.srv.URL, "api_keys:\n  - sk-vmr-secret-key01"))

	resp, _ := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 401 {
		t.Errorf("no key: status=%d", resp.StatusCode)
	}
	resp, _ = chat(t, ts, simpleReq, map[string]string{"Authorization": "Bearer wrong"})
	if resp.StatusCode != 401 {
		t.Errorf("wrong key: status=%d", resp.StatusCode)
	}
	resp, _ = chat(t, ts, simpleReq, map[string]string{"Authorization": "Bearer sk-vmr-secret-key01"})
	if resp.StatusCode != 200 {
		t.Errorf("right key: status=%d", resp.StatusCode)
	}
}

// TestRouterAuthBearerCaseInsensitive locks RFC 7235's requirement that the
// auth-scheme token is case-insensitive — a client sending "bearer" or
// "BEARER" must authenticate exactly like "Bearer" does.
func TestRouterAuthBearerCaseInsensitive(t *testing.T) {
	u := newUpstream(t)
	ts := newRouterServer(t, twoEndpointYAML(u.srv.URL, u.srv.URL, "api_keys:\n  - sk-vmr-secret-key01"))

	for _, scheme := range []string{"bearer", "BEARER", "BeArEr"} {
		resp, _ := chat(t, ts, simpleReq, map[string]string{"Authorization": scheme + " sk-vmr-secret-key01"})
		if resp.StatusCode != 200 {
			t.Errorf("scheme %q: status=%d, want 200", scheme, resp.StatusCode)
		}
	}
}

// TestRouterAuthMultiKeyTagsRequests confirms that (a) each api_keys entry
// authenticates independently and tags the audit record with its own KeyTag
// (api_keys is the only auth surface — the untagged api_key catch-all was
// removed), and (b) a request that matches nothing is still audited,
// untagged, with a 401.
func TestRouterAuthMultiKeyTagsRequests(t *testing.T) {
	u := newUpstream(t)
	extra := "api_keys:\n  - sk-vmr-team-alice\n  - sk-vmr-team-bobby"
	ts, al := newAuditedServer(t, twoEndpointYAML(u.srv.URL, u.srv.URL, extra))

	cases := []struct {
		name    string
		key     string
		wantTag string
	}{
		{"tagged alice", "sk-vmr-team-alice", "alice"},
		{"tagged bobby", "sk-vmr-team-bobby", "bobby"},
	}
	for _, c := range cases {
		resp, _ := chat(t, ts, simpleReq, map[string]string{"Authorization": "Bearer " + c.key})
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status=%d", c.name, resp.StatusCode)
		}
	}
	resp, _ := chat(t, ts, simpleReq, map[string]string{"Authorization": "Bearer wrong"})
	if resp.StatusCode != 401 {
		t.Fatalf("unmatched key: status=%d", resp.StatusCode)
	}

	recs := readRecords(t, al)
	if len(recs) != len(cases)+1 {
		t.Fatalf("records: %d", len(recs))
	}
	for i, c := range cases {
		if got := recs[i].ClientKeyTag; got != c.wantTag {
			t.Errorf("%s: client_key_tag = %q, want %q", c.name, got, c.wantTag)
		}
	}
	if last := recs[len(recs)-1]; last.ClientKeyTag != "" || last.Outcome != "error" {
		t.Errorf("unmatched key record: tag=%q outcome=%q, want empty tag + error", last.ClientKeyTag, last.Outcome)
	}
}

// TestNoAuthConfiguredSelfDeclaredTag covers the private-network shortcut:
// with neither api_key nor api_keys configured, the door stays fully open
// (no config change from today), but a client that voluntarily sends an
// Authorization value still gets it KeyTag-derived and recorded, letting it
// self-identify to `vmr report` with zero vmr-side config. A client sending
// nothing must still get an untagged, unaffected record (backward compat).
func TestNoAuthConfiguredSelfDeclaredTag(t *testing.T) {
	u := newUpstream(t)
	ts, al := newAuditedServer(t, twoEndpointYAML(u.srv.URL, u.srv.URL, "")) // no api_key/api_keys at all

	resp, _ := chat(t, ts, simpleReq, map[string]string{"Authorization": "Bearer anything-alice"})
	if resp.StatusCode != 200 {
		t.Fatalf("self-declared tag: status=%d", resp.StatusCode)
	}
	resp, _ = chat(t, ts, simpleReq, nil) // no credential sent at all
	if resp.StatusCode != 200 {
		t.Fatalf("no credential: status=%d", resp.StatusCode)
	}

	recs := readRecords(t, al)
	if len(recs) != 2 {
		t.Fatalf("records: %d", len(recs))
	}
	if got := recs[0].ClientKeyTag; got != "alice" {
		t.Errorf("self-declared: client_key_tag = %q, want %q", got, "alice")
	}
	if got := recs[1].ClientKeyTag; got != "" {
		t.Errorf("no credential: client_key_tag = %q, want empty", got)
	}
}

func TestModelsEndpoint(t *testing.T) {
	u := newUpstream(t)
	ts := newRouterServer(t, twoEndpointYAML(u.srv.URL, u.srv.URL, ""))
	resp, err := http.Get(ts.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Object string `json:"object"`
		Data   []struct {
			ID, Object, OwnedBy string
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Object != "list" || len(out.Data) != 1 || out.Data[0].ID != "vm" {
		t.Errorf("models: %+v", out)
	}
}

// TestModelsEndpointDedupesNameAcrossProtocols locks in a fix: a virtual
// model name registered under more than one ingress protocol at once
// (config's VirtualModel doc comment documents this as supported — one
// openai-protocol endpoint group and one anthropic-protocol one sharing the
// same name) used to produce a duplicate "id" in /v1/models' data array,
// one row per protocol. From a client's perspective it's still one
// addressable model — the name is what it calls, not the protocol behind
// it — so the id must appear exactly once.
func TestModelsEndpointDedupesNameAcrossProtocols(t *testing.T) {
	o, a := newUpstream(t), newAnthUpstream(t)
	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: oai, base_url: {openai: %s}, api_key: k0}
  - {name: anth, base_url: {anthropic: %s}, api_key: k1}
models:
  shared:
    endpoints:
      - {protocol: openai, provider: oai, models: [model-one]}
      - {protocol: anthropic, provider: anth, models: [model-two]}
`, o.srv.URL, a.srv.URL)
	ts := newRouterServer(t, yaml)

	resp, err := http.Get(ts.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			ID       string `json:"id"`
			Protocol string `json:"vmr_protocol"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 1 {
		t.Fatalf("expected exactly one deduped entry for %q, got %d: %+v", "shared", len(out.Data), out.Data)
	}
	if out.Data[0].ID != "shared" || out.Data[0].Protocol != "anthropic" {
		t.Errorf("data[0] = %+v, want id=shared vmr_protocol=anthropic (sorted-protocol-order tiebreak)", out.Data[0])
	}
}

func TestUnknownModelAndBadRequests(t *testing.T) {
	u := newUpstream(t)
	ts := newRouterServer(t, twoEndpointYAML(u.srv.URL, u.srv.URL, ""))

	resp, body := chat(t, ts, `{"model":"ghost","messages":[]}`, nil)
	if resp.StatusCode != 404 || !strings.Contains(body, "not found") {
		t.Errorf("unknown model: status=%d body=%s", resp.StatusCode, body)
	}
	resp, _ = chat(t, ts, `{"messages":[]}`, nil)
	if resp.StatusCode != 400 {
		t.Errorf("missing model field: status=%d", resp.StatusCode)
	}
	resp, _ = chat(t, ts, `not json`, nil)
	if resp.StatusCode != 400 {
		t.Errorf("bad json: status=%d", resp.StatusCode)
	}
}

func TestBodyTooLarge(t *testing.T) {
	u := newUpstream(t)
	ts := newRouterServer(t, twoEndpointYAML(u.srv.URL, u.srv.URL, "max_request_body_mb: 1"))
	big := fmt.Sprintf(`{"model":"vm","messages":[{"role":"user","content":"%s"}]}`, strings.Repeat("x", 2<<20))
	resp, _ := chat(t, ts, big, nil)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status=%d", resp.StatusCode)
	}
}

func TestAdminStatus(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	u1.status.Store(500)
	ts := newRouterServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, ""))
	chat(t, ts, simpleReq, nil) // u1 fails once → cooldown

	resp, err := http.Get(ts.URL + "/admin/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Models map[string][]struct {
			Endpoint  string `json:"endpoint"`
			Available bool   `json:"available"`
			Fails     int    `json:"consecutive_failures"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	eps := out.Models["vm [openai]"]
	if len(eps) != 2 {
		t.Fatalf("endpoints: %+v", eps)
	}
	for _, ep := range eps {
		switch ep.Endpoint {
		case "openai/p1/model-one":
			if ep.Available || ep.Fails != 1 {
				t.Errorf("p1 should be cooling down: %+v", ep)
			}
		case "openai/p2/model-two":
			if !ep.Available {
				t.Errorf("p2 should be available: %+v", ep)
			}
		}
	}
}
