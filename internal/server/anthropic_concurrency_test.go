// Ver 2026-07-30, by Sonnet 5

// Integration tests for two features that shipped in the same batch and
// have shared this file ever since: Anthropic ingress (protocol isolation,
// x-api-key auth, the merged /v1/models shape) and the concurrency gate.
// No relation to this repo's audit-report version numbering (V1.0/2.0/V3/V4)
// — the filename used to say "v2.2", an internal milestone label from
// before that numbering existed, which read as if it did.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/router"

	_ "vmr/internal/adapter/anthropic"
)

// anthUpstream mocks an Anthropic-compatible provider and records auth headers.
type anthUpstream struct {
	srv        *httptest.Server
	hits       atomic.Int32
	status     atomic.Int32
	lastAPIKey atomic.Value
	lastVer    atomic.Value
	lastModel  atomic.Value
}

func newAnthUpstream(t *testing.T) *anthUpstream {
	t.Helper()
	u := &anthUpstream{}
	u.status.Store(200)
	u.srv = newJSONUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			http.NotFound(w, r)
			return
		}
		u.hits.Add(1)
		u.lastAPIKey.Store(r.Header.Get("x-api-key"))
		u.lastVer.Store(r.Header.Get("anthropic-version"))
		body, _ := io.ReadAll(r.Body)
		model := extractModelField(body)
		u.lastModel.Store(model)
		st := int(u.status.Load())
		w.Header().Set("Content-Type", "application/json")
		if st != 200 {
			w.WriteHeader(st)
			fmt.Fprintf(w, `{"type":"error","error":{"type":"api_error","message":"upstream says %d"}}`, st)
			return
		}
		fmt.Fprintf(w, `{"id":"m1","type":"message","role":"assistant","model":%q,"content":[{"type":"text","text":"PONG"}]}`, model)
	})
	return u
}

func messages(t *testing.T, ts *httptest.Server, body string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

const anthReq = `{"model":"vm-anth","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`

// chatQuiet is chat without t.Fatal, safe to call from non-test goroutines.
func chatQuiet(ts *httptest.Server, body string) (int, error) {
	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode, nil
}

func TestAnthropicIngressFailoverAndHeaders(t *testing.T) {
	o, a1, a2 := newUpstream(t), newAnthUpstream(t), newAnthUpstream(t)
	a1.status.Store(500)
	ts := newRouterServer(t, dualProtocolYAML(o.srv.URL, a1.srv.URL, a2.srv.URL, ""))

	resp, body := messages(t, ts, anthReq, map[string]string{"anthropic-version": "2024-10-22"})
	if resp.StatusCode != 200 || !strings.Contains(body, "PONG") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "anthropic-messages/a2/real-b" {
		t.Errorf("endpoint: %s", got)
	}
	if got := resp.Header.Get("X-VMR-Attempts"); got != "2" {
		t.Errorf("attempts: %s", got)
	}
	if got := a2.lastAPIKey.Load(); got != "ka2" {
		t.Errorf("provider key not injected: %v", got)
	}
	if got := a2.lastVer.Load(); got != "2024-10-22" {
		t.Errorf("anthropic-version not forwarded: %v", got)
	}
	if got := a2.lastModel.Load(); got != "real-b" {
		t.Errorf("model rewrite: %v", got)
	}
}

func TestProtocolIsolation(t *testing.T) {
	o, a1, a2 := newUpstream(t), newAnthUpstream(t), newAnthUpstream(t)
	ts := newRouterServer(t, dualProtocolYAML(o.srv.URL, a1.srv.URL, a2.srv.URL, ""))

	// anthropic-messages model via OpenAI ingress → rejected with guidance.
	resp, body := chat(t, ts, `{"model":"vm-anth","messages":[]}`, nil)
	if resp.StatusCode != 404 || !strings.Contains(body, "/v1/messages") {
		t.Errorf("openai-completions ingress must reject anthropic-messages model: status=%d body=%s", resp.StatusCode, body)
	}
	// openai-completions model via Anthropic ingress → rejected with guidance.
	resp, body = messages(t, ts, `{"model":"vm-openai","max_tokens":8,"messages":[]}`, nil)
	if resp.StatusCode != 404 || !strings.Contains(body, "/v1/chat/completions") {
		t.Errorf("anthropic-messages ingress must reject openai-completions model: status=%d body=%s", resp.StatusCode, body)
	}
	if a1.hits.Load() != 0 || a2.hits.Load() != 0 || o.hits.Load() != 0 {
		t.Error("no upstream must be hit on protocol mismatch")
	}
}

// TestCrossProtocolProviderRefRejectedAtLoad locks in the new schema's
// equivalent guard: an endpoint-group's protocol must match one of its
// referenced provider's declared base_url protocols. "anth" only declares
// an anthropic base_url, so an openai-completions entry referencing it is
// rejected — the same "no valid syntax to express this" mismatch the old
// protocol-nested schema caught via "unknown provider", now caught by
// "provider has no base_url for protocol".
func TestCrossProtocolProviderRefRejectedAtLoad(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  - {name: oai, base_url: {openai-completions: https://x.example/v1}, api_key: k}
  - {name: anth, base_url: {anthropic-messages: https://y.example/v1}, api_key: k}
models:
  bad:
    endpoints:
      openai-completions:
        - {providers: [oai], models: [a]}
        - {providers: [anth], models: [b]}
`
	_, err := config.Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "no base_url for protocol") {
		t.Errorf("want a no-base_url-for-protocol error (cross-protocol ref has no valid syntax), got %v", err)
	}
}

// A model name can exist under both protocols at once — one models.<name>
// entry mixes an openai-completions endpoint-group and an anthropic-messages
// one, and BuildSnapshot resolves them independently, no artificial "-a"
// suffix needed to give one virtual model both an OpenAI and an Anthropic
// face.
func TestSameModelNameReachableUnderBothProtocols(t *testing.T) {
	o, a := newUpstream(t), newAnthUpstream(t)
	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: oai, base_url: {openai-completions: %s}, api_key: k0}
  - {name: anth, base_url: {anthropic-messages: %s}, api_key: ka}
models:
  coding:
    endpoints:
      openai-completions:
        - {providers: [oai], models: [model-one]}
      anthropic-messages:
        - {providers: [anth], models: [real-a]}
`, o.srv.URL, a.srv.URL)
	ts := newRouterServer(t, yaml)

	resp, _ := chat(t, ts, `{"model":"coding","messages":[{"role":"user","content":"hi"}]}`, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai-completions/oai/model-one" {
		t.Errorf("openai-completions-face coding: status=%d ep=%s", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}
	resp, body := messages(t, ts, `{"model":"coding","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`, nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "PONG") {
		t.Errorf("anthropic-face coding: status=%d body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "anthropic-messages/anth/real-a" {
		t.Errorf("anthropic-face endpoint: %s", got)
	}
}

func TestXAPIKeyAuth(t *testing.T) {
	o, a1, a2 := newUpstream(t), newAnthUpstream(t), newAnthUpstream(t)
	ts := newRouterServer(t, dualProtocolYAML(o.srv.URL, a1.srv.URL, a2.srv.URL, "api_keys:\n  - sk-vmr-dual-key01"))

	resp, _ := messages(t, ts, anthReq, nil)
	if resp.StatusCode != 401 {
		t.Errorf("no key: %d", resp.StatusCode)
	}
	resp, _ = messages(t, ts, anthReq, map[string]string{"x-api-key": "sk-vmr-dual-key01"})
	if resp.StatusCode != 200 {
		t.Errorf("x-api-key auth failed: %d", resp.StatusCode)
	}
	resp, _ = messages(t, ts, anthReq, map[string]string{"Authorization": "Bearer sk-vmr-dual-key01"})
	if resp.StatusCode != 200 {
		t.Errorf("bearer on anthropic ingress failed: %d", resp.StatusCode)
	}
}

func TestModelsMergedShape(t *testing.T) {
	o, a1, a2 := newUpstream(t), newAnthUpstream(t), newAnthUpstream(t)
	ts := newRouterServer(t, dualProtocolYAML(o.srv.URL, a1.srv.URL, a2.srv.URL, ""))
	resp, err := http.Get(ts.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Object  string `json:"object"`
		HasMore *bool  `json:"has_more"`
		Data    []struct {
			ID, Object, Type, OwnedBy string
			Protocol                  string `json:"vmr_protocol"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Object != "list" || out.HasMore == nil || *out.HasMore {
		t.Errorf("merged shape: %+v", out)
	}
	if len(out.Data) != 2 || out.Data[0].ID != "vm-anth" || out.Data[0].Type != "model" || out.Data[0].Protocol != "anthropic-messages" {
		t.Errorf("data: %+v", out.Data)
	}
}

// slowUpstream parks requests until released, tracking peak parallelism.
func TestConcurrencyGate(t *testing.T) {
	var cur, peak atomic.Int32
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		<-release
		cur.Add(-1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","choices":[]}`)
	}))
	defer slow.Close()

	ts := newRouterServer(t, fmt.Sprintf(`
listen: 127.0.0.1:0
max_concurrency: 2
providers:
  - {name: p, base_url: {openai-completions: %s}, api_key: k}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p], models: [m]}
`, slow.URL))

	var wg sync.WaitGroup
	results := make([]int, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = chatQuiet(ts, simpleReq)
		}(i)
	}
	// Let requests pile up against the gate, then release them all.
	time.Sleep(300 * time.Millisecond)
	if got := cur.Load(); got != 2 {
		t.Errorf("with limit 2, upstream parallelism should be 2 while gated, got %d", got)
	}
	close(release)
	wg.Wait()
	for i, st := range results {
		if st != 200 {
			t.Errorf("request %d: status %d", i, st)
		}
	}
	if p := peak.Load(); p > 2 {
		t.Errorf("peak upstream parallelism %d exceeds limit 2", p)
	}
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, label string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("timed out waiting for %s", label)
}

// TestConcurrencyWaiterCanceled isolates the gate's cancel path: a waiter
// parked in AcquireSlot must leave the queue — and never reach the upstream —
// when its request context is canceled, the same way a client disconnect
// cancels it. Only the FIRST request blocks the upstream, so a gate that
// ignores ctx.Done fails both assertions: the waiter stays queued after the
// cancel (waiting never drops to 0) and is admitted once the slot frees
// (second upstream hit).
func TestConcurrencyWaiterCanceled(t *testing.T) {
	release := make(chan struct{})
	var hits atomic.Int32
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		fmt.Fprint(w, `{"id":"x","choices":[]}`)
	}))
	defer slow.Close()

	// Built inline (not via newRouterServer) because the assertions read the
	// gate's live state from rt.
	cfg, err := config.Parse([]byte(fmt.Sprintf(`
listen: 127.0.0.1:0
max_concurrency: 1
providers:
  - {name: p, base_url: {openai-completions: %s}, api_key: k}
models:
  vm:
    endpoints: {openai-completions: [{providers: [p], models: [m]}]}
`, slow.URL)))
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

	// Occupy the only slot.
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		chatQuiet(ts, simpleReq)
	}()
	waitFor(t, "first request in flight", func() bool {
		_, inFlight, _ := rt.Concurrency()
		return inFlight == 1
	})

	// Second request parks at the gate; cancel it via the request context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/v1/chat/completions", strings.NewReader(simpleReq))
		req.Header.Set("Content-Type", "application/json")
		// The handler returns without writing when the waiter is canceled,
		// so client-side the outcome is an error or an empty 200 — neither
		// is the property under test, so the response is discarded.
		if resp, err := http.DefaultClient.Do(req); err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	waitFor(t, "second request waiting at the gate", func() bool {
		_, _, waiting := rt.Concurrency()
		return waiting == 1
	})

	cancel()
	waitFor(t, "waiter to leave the gate after cancel", func() bool {
		_, _, waiting := rt.Concurrency()
		return waiting == 0
	})

	// Free the slot and let both request goroutines finish. A canceled
	// waiter that still gets admitted (broken cancel path) necessarily hits
	// the upstream before secondDone closes, so asserting after both are
	// done is race-free.
	close(release)
	<-firstDone
	<-secondDone
	if n := hits.Load(); n != 1 {
		t.Errorf("upstream hits after waiter cancel = %d, want 1 (canceled waiter must not reach upstream)", n)
	}
}
