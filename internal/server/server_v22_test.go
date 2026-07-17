// Ver 2026-07-13 04:00, by Sonnet 5

// V2.2 integration tests: Anthropic ingress, protocol isolation, concurrency gate.
package server

import (
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
	u := &anthUpstream{}
	u.status.Store(200)
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			http.NotFound(w, r)
			return
		}
		u.hits.Add(1)
		u.lastAPIKey.Store(r.Header.Get("x-api-key"))
		u.lastVer.Store(r.Header.Get("anthropic-version"))
		body, _ := io.ReadAll(r.Body)
		var m struct {
			Model string `json:"model"`
		}
		json.Unmarshal(body, &m)
		u.lastModel.Store(m.Model)
		st := int(u.status.Load())
		w.Header().Set("Content-Type", "application/json")
		if st != 200 {
			w.WriteHeader(st)
			fmt.Fprintf(w, `{"type":"error","error":{"type":"api_error","message":"upstream says %d"}}`, st)
			return
		}
		fmt.Fprintf(w, `{"id":"m1","type":"message","role":"assistant","model":%q,"content":[{"type":"text","text":"PONG"}]}`, m.Model)
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func dualProtocolYAML(oai, anth1, anth2 string, extra string) string {
	return fmt.Sprintf(`
listen: 127.0.0.1:0
%s
providers:
  openai:
    oai: {base_url: %s, api_key: k0}
  anthropic:
    a1: {base_url: %s, api_key: ka1}
    a2: {base_url: %s, api_key: ka2}
models:
  openai:
    vm-openai:
      endpoints:
        - {provider: oai, model: model-one, priority: 1}
  anthropic:
    vm-anth:
      endpoints:
        - {provider: a1, model: real-a, priority: 1}
        - {provider: a2, model: real-b, priority: 2}
`, extra, oai, anth1, anth2)
}

func messages(t *testing.T, ts *httptest.Server, body string, hdr map[string]string) (*http.Response, string) {
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
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "anthropic/a2/real-b" {
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

	// Anthropic-protocol model via OpenAI ingress → rejected with guidance.
	resp, body := chat(t, ts, `{"model":"vm-anth","messages":[]}`, nil)
	if resp.StatusCode != 404 || !strings.Contains(body, "/v1/messages") {
		t.Errorf("openai ingress must reject anthropic model: status=%d body=%s", resp.StatusCode, body)
	}
	// OpenAI-protocol model via Anthropic ingress → rejected with guidance.
	resp, body = messages(t, ts, `{"model":"vm-openai","max_tokens":8,"messages":[]}`, nil)
	if resp.StatusCode != 404 || !strings.Contains(body, "/v1/chat/completions") {
		t.Errorf("anthropic ingress must reject openai model: status=%d body=%s", resp.StatusCode, body)
	}
	if a1.hits.Load() != 0 || a2.hits.Load() != 0 || o.hits.Load() != 0 {
		t.Error("no upstream must be hit on protocol mismatch")
	}
}

// Nesting providers/models by protocol removes the syntax for mixing
// protocols within one model entirely: an endpoint can only ever name a
// provider from its own model's protocol group, so referencing the other
// group's provider is just "unknown provider" — the same error class as any
// other typo, not a special case.
func TestCrossProtocolProviderRefRejectedAtLoad(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  openai:
    oai: {base_url: https://x.example/v1, api_key: k}
  anthropic:
    anth: {base_url: https://y.example/v1, api_key: k}
models:
  openai:
    bad:
      endpoints:
        - {provider: oai, model: a}
        - {provider: anth, model: b}
`
	_, err := config.Parse([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("want unknown-provider error (cross-protocol ref has no valid syntax), got %v", err)
	}
}

// A model name can now exist under both protocol groups at once — nesting by
// protocol means the two entries are looked up independently, no artificial
// "-a" suffix needed to give one virtual model both an OpenAI and an
// Anthropic face.
func TestSameModelNameReachableUnderBothProtocols(t *testing.T) {
	o, a := newUpstream(t), newAnthUpstream(t)
	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai:
    oai: {base_url: %s, api_key: k0}
  anthropic:
    anth: {base_url: %s, api_key: ka}
models:
  openai:
    coding:
      endpoints: [{provider: oai, model: model-one}]
  anthropic:
    coding:
      endpoints: [{provider: anth, model: real-a}]
`, o.srv.URL, a.srv.URL)
	ts := newRouterServer(t, yaml)

	resp, _ := chat(t, ts, `{"model":"coding","messages":[{"role":"user","content":"hi"}]}`, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai/oai/model-one" {
		t.Errorf("openai-face coding: status=%d ep=%s", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}
	resp, body := messages(t, ts, `{"model":"coding","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`, nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "PONG") {
		t.Errorf("anthropic-face coding: status=%d body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "anthropic/anth/real-a" {
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
	if len(out.Data) != 2 || out.Data[0].ID != "vm-anth" || out.Data[0].Type != "model" || out.Data[0].Protocol != "anthropic" {
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
  openai:
    p: {base_url: %s, api_key: k}
models:
  openai:
    vm:
      endpoints:
        - {provider: p, model: m}
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

func TestConcurrencyWaiterCanceled(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		fmt.Fprint(w, `{"id":"x","choices":[]}`)
	}))
	defer slow.Close()
	ts := newRouterServer(t, fmt.Sprintf(`
listen: 127.0.0.1:0
max_concurrency: 1
providers:
  openai:
    p: {base_url: %s, api_key: k}
models:
  openai:
    vm:
      endpoints: [{provider: p, model: m}]
`, slow.URL))

	// Occupy the only slot.
	go chatQuiet(ts, simpleReq)
	time.Sleep(150 * time.Millisecond)

	// Second request waits at the gate; give it a short client timeout.
	client := &http.Client{Timeout: 300 * time.Millisecond}
	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions", strings.NewReader(simpleReq))
	req.Header.Set("Content-Type", "application/json")
	if _, err := client.Do(req); err == nil {
		t.Error("waiter should have timed out client-side")
	}
	close(release) // unblock the first request; test must not deadlock
}
