// Ver 2026-09-23 02:30, by GPT-5.2

package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vmr/internal/core"
)

// TestServe_ProviderConcurrency_FastSkip verifies that non-sticky requests
// skip a saturated provider immediately without queue delay.
func TestServe_ProviderConcurrency_FastSkip(t *testing.T) {
	var p1Hits, p2Hits atomic.Int64
	p1Block := make(chan struct{})
	p1Entered := make(chan struct{})

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		p1Hits.Add(1)
		if strings.Contains(string(body), "hold") {
			select {
			case <-p1Entered:
			default:
				close(p1Entered)
			}
			<-p1Block // hold slot
		}
		w.WriteHeader(200)
		fmt.Fprint(w, `{"id":"1","model":"m1"}`)
	}))
	defer func() {
		select {
		case <-p1Block:
		default:
			close(p1Block)
		}
		srv1.Close()
	}()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p2Hits.Add(1)
		w.WriteHeader(200)
		fmt.Fprint(w, `{"id":"2","model":"m2"}`)
	}))
	defer srv2.Close()

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1, concurrency: 1}
  - {name: p2, base_url: {openai-completions: %s}, api_key: k2}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1], priority: 1}
        - {providers: [p2], models: [m2], priority: 2}
`, srv1.URL, srv2.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	// Launch in-flight request to occupy p1
	go func() {
		serveReq(rt, "vm", []byte(`{"model":"vm","messages":[{"role":"user","content":"hold"}]}`))
	}()
	select {
	case <-p1Entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for p1 hold request to enter")
	}

	// New request arrives -> p1 is full -> fast skip to p2 without waiting
	start := time.Now()
	w2 := serveReq(rt, "vm", []byte(`{"model":"vm","messages":[{"role":"user","content":"new"}]}`))
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Errorf("fast skip took too long: %v", elapsed)
	}
	if w2.Code != 200 {
		t.Fatalf("request 2 code = %d, want 200", w2.Code)
	}
	if failover := w2.Header().Get("X-VMR-Failover"); !strings.Contains(failover, "p1/m1:busy") {
		t.Errorf("expected p1/m1:busy in X-VMR-Failover, got %q", failover)
	}
	if p2Hits.Load() != 1 {
		t.Errorf("p2Hits = %d, want 1", p2Hits.Load())
	}
}

// TestServe_ProviderConcurrency_StickyQueueWait verifies that a sticky session
// waits in queue and successfully runs on its pinned provider once a slot frees.
func TestServe_ProviderConcurrency_StickyQueueWait(t *testing.T) {
	p1Hold := make(chan struct{})
	p1Entered := make(chan struct{})

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "blocker") {
			select {
			case <-p1Entered:
			default:
				close(p1Entered)
			}
			<-p1Hold
		}
		w.WriteHeader(200)
		fmt.Fprint(w, `{"id":"1","model":"m1"}`)
	}))
	defer func() {
		select {
		case <-p1Hold:
		default:
			close(p1Hold)
		}
		srv1.Close()
	}()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, `{"id":"2","model":"m2"}`)
	}))
	defer srv2.Close()

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1, concurrency: 1, concurrency_queue: 500ms}
  - {name: p2, base_url: {openai-completions: %s}, api_key: k2}
models:
  vm:
    sticky: true
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1], priority: 1}
        - {providers: [p2], models: [m2], priority: 2}
`, srv1.URL, srv2.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	// Step 1: Prime session on p1
	sessionBody := []byte(`{"model":"vm","messages":[{"role":"system","content":"sys-sticky"},{"role":"user","content":"first"}]}`)
	w0 := serveReq(rt, "vm", sessionBody)
	if ep := w0.Header().Get("X-VMR-Endpoint"); !strings.Contains(ep, "p1") {
		t.Fatalf("priming request landed on %s, want p1", ep)
	}

	// Step 2: Another caller occupies p1
	go func() {
		serveReq(rt, "vm", []byte(`{"model":"vm","messages":[{"role":"user","content":"blocker"}]}`))
	}()
	select {
	case <-p1Entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for blocker request to enter p1")
	}

	// Step 3: Same session sends turn 2 -> hits sticky -> queues -> unblocked
	go func() {
		time.Sleep(50 * time.Millisecond)
		select {
		case <-p1Hold:
		default:
			close(p1Hold)
		}
	}()

	sessionTurn2 := []byte(`{"model":"vm","messages":[{"role":"system","content":"sys-sticky"},{"role":"user","content":"first"},{"role":"assistant","content":"hi"},{"role":"user","content":"second"}]}`)
	wTurn2 := serveReq(rt, "vm", sessionTurn2)

	if wTurn2.Code != 200 {
		t.Fatalf("turn 2 code = %d, want 200", wTurn2.Code)
	}
	if ep := wTurn2.Header().Get("X-VMR-Endpoint"); !strings.Contains(ep, "p1") {
		t.Errorf("turn 2 landed on %s, want p1", ep)
	}
	reason := wTurn2.Header().Get("X-VMR-Route-Reason")
	if !strings.Contains(reason, "conc_waited=") {
		t.Errorf("expected conc_waited in reason, got %q", reason)
	}
}

// TestServe_ProviderConcurrency_StickyEscapeNoPollution verifies that when a
// sticky request times out waiting for concurrency, it fails over to the backup
// provider, but DOES NOT overwrite the sticky preference pointer.
func TestServe_ProviderConcurrency_StickyEscapeNoPollution(t *testing.T) {
	p1Block := make(chan struct{})
	p1Entered := make(chan struct{})

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "blocker") {
			select {
			case <-p1Entered:
			default:
				close(p1Entered)
			}
			<-p1Block
		}
		w.WriteHeader(200)
		fmt.Fprint(w, `{"id":"1","model":"m1"}`)
	}))
	defer func() {
		select {
		case <-p1Block:
		default:
			close(p1Block)
		}
		srv1.Close()
	}()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, `{"id":"2","model":"m2"}`)
	}))
	defer srv2.Close()

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1, concurrency: 1, concurrency_queue: 30ms}
  - {name: p2, base_url: {openai-completions: %s}, api_key: k2}
models:
  vm:
    sticky: true
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1], priority: 1}
        - {providers: [p2], models: [m2], priority: 2}
`, srv1.URL, srv2.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	// Step 1: Prime session on p1
	sessionMsg1 := `{"role":"system","content":"sys-escape"},{"role":"user","content":"msg1"}`
	sessionBody1 := []byte(fmt.Sprintf(`{"model":"vm","messages":[%s]}`, sessionMsg1))

	w1 := serveReq(rt, "vm", sessionBody1)
	if ep := w1.Header().Get("X-VMR-Endpoint"); !strings.Contains(ep, "p1") {
		t.Fatalf("step 1 landed on %s, want p1", ep)
	}

	// Step 2: Occupy p1
	go func() {
		serveReq(rt, "vm", []byte(`{"model":"vm","messages":[{"role":"user","content":"blocker"}]}`))
	}()
	select {
	case <-p1Entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for blocker to enter p1")
	}

	// Step 3: Session turn 2 queues for 30ms -> timeouts -> fails over to p2
	sessionBody2 := []byte(fmt.Sprintf(`{"model":"vm","messages":[%s,{"role":"user","content":"msg2"}]}`, sessionMsg1))
	w2 := serveReq(rt, "vm", sessionBody2)

	if w2.Code != 200 {
		t.Fatalf("step 2 code = %d, want 200", w2.Code)
	}
	if ep := w2.Header().Get("X-VMR-Endpoint"); !strings.Contains(ep, "p2") {
		t.Fatalf("step 2 should failover to p2, got %s", ep)
	}
	if failover := w2.Header().Get("X-VMR-Failover"); !strings.Contains(failover, "p1/m1:busy_timeout") {
		t.Errorf("expected busy_timeout in X-VMR-Failover, got %q", failover)
	}

	// Unblock p1
	close(p1Block)
	time.Sleep(30 * time.Millisecond)

	// Step 4: Session turn 3 sends request.
	// Since turn 2 was a temporary concurrency escape, sticky pointer must STILL point to p1!
	sessionBody3 := []byte(fmt.Sprintf(`{"model":"vm","messages":[%s,{"role":"user","content":"msg3"}]}`, sessionMsg1))
	w3 := serveReq(rt, "vm", sessionBody3)

	if w3.Code != 200 {
		t.Fatalf("step 4 code = %d, want 200", w3.Code)
	}
	if ep := w3.Header().Get("X-VMR-Endpoint"); !strings.Contains(ep, "p1") {
		t.Errorf("step 4 should still route back to p1 (sticky preserved), got %s", ep)
	}
}

// TestServe_ProviderConcurrency_ClientCancel verifies that a queued request
// cancels cleanly when client disconnects, decrementing queue count to 0.
func TestServe_ProviderConcurrency_ClientCancel(t *testing.T) {
	p1Block := make(chan struct{})
	p1Entered := make(chan struct{})

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "hold") {
			select {
			case <-p1Entered:
			default:
				close(p1Entered)
			}
			<-p1Block
		}
		w.WriteHeader(200)
	}))
	defer func() {
		select {
		case <-p1Block:
		default:
			close(p1Block)
		}
		srv1.Close()
	}()

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1, concurrency: 1, concurrency_queue: 500ms}
models:
  vm:
    sticky: true
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
`, srv1.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	// Prime session
	sessionBody := []byte(`{"model":"vm","messages":[{"role":"system","content":"cancel-sys"},{"role":"user","content":"u1"}]}`)
	serveReq(rt, "vm", sessionBody)

	// Occupy p1
	go func() {
		serveReq(rt, "vm", []byte(`{"model":"vm","messages":[{"role":"user","content":"hold"}]}`))
	}()
	select {
	case <-p1Entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for hold request to enter")
	}

	// Request with cancelable context
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequestWithContext(ctx, "POST", "/v1/chat/completions", bytes.NewReader(sessionBody))
	w := httptest.NewRecorder()

	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	rt.Serve(w, req, &core.CanonicalRequest{Model: "vm", Raw: sessionBody}, "openai-completions", rt.Snapshot(), nil)

	l := rt.ProviderLimiters.Get("p1")
	if l.waiting.Load() != 0 {
		t.Errorf("waiting count leaked: got %d, want 0", l.waiting.Load())
	}
}

// TestServe_ProviderConcurrency_AllBusyReturnsClearError verifies that when
// all candidate endpoints are busy (at provider concurrency limit), the 503
// error clearly states concurrency limit reached instead of a misleading condition error.
func TestServe_ProviderConcurrency_AllBusyReturnsClearError(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv2.Close()

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1, concurrency: 1}
  - {name: p2, base_url: {openai-completions: %s}, api_key: k2, concurrency: 1}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1], priority: 1}
        - {providers: [p2], models: [m2], priority: 2}
`, srv1.URL, srv2.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	// Occupy both p1 and p2
	l1 := rt.ProviderLimiters.Get("p1")
	rel1, ok1 := l1.TryAcquire()
	if !ok1 {
		t.Fatal("failed to occupy p1")
	}
	defer rel1()

	l2 := rt.ProviderLimiters.Get("p2")
	rel2, ok2 := l2.TryAcquire()
	if !ok2 {
		t.Fatal("failed to occupy p2")
	}
	defer rel2()

	// New request for vm when all candidates are busy
	w := serveReq(rt, "vm", []byte(`{"model":"vm","messages":[{"role":"user","content":"test"}]}`))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	body := w.Body.String()
	wantMsg := `all candidate endpoints for model \"vm\" are busy (provider concurrency limit reached)`
	if !strings.Contains(body, wantMsg) {
		t.Errorf("error body = %q, want it to contain %q", body, wantMsg)
	}

	// Pinned request when pinned endpoint is busy
	reqPinned := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"vm","messages":[{"role":"user","content":"test"}]}`)))
	reqPinned.Header.Set("X-VMR-Provider", "p1")
	wPinned := httptest.NewRecorder()
	rt.Serve(wPinned, reqPinned, &core.CanonicalRequest{Model: "vm", Raw: []byte(`{"model":"vm"}`)}, "openai-completions", rt.Snapshot(), nil)
	if wPinned.Code != http.StatusServiceUnavailable {
		t.Fatalf("pinned: expected 503, got %d", wPinned.Code)
	}
	pinnedBody := wPinned.Body.String()
	wantPinnedMsg := `pinned endpoint (pin=provider=p1) for model \"vm\" is busy (provider concurrency limit reached)`
	if !strings.Contains(pinnedBody, wantPinnedMsg) {
		t.Errorf("pinned error body = %q, want it to contain %q", pinnedBody, wantPinnedMsg)
	}
}

// TestServe_ProviderConcurrency_ClientCancelDoesNotFailoverOrMarkTimeout verifies
// that when a queued sticky request is canceled by the client:
// 1) Failover halts immediately and backup providers are not probed.
// 2) X-VMR-Failover does not misclassify the cancellation as busy_timeout.
// 3) Error response accurately indicates client cancellation.
func TestServe_ProviderConcurrency_ClientCancelDoesNotFailoverOrMarkTimeout(t *testing.T) {
	p1Block := make(chan struct{})
	p1Entered := make(chan struct{})
	var p2Hits atomic.Int64

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "hold") {
			select {
			case <-p1Entered:
			default:
				close(p1Entered)
			}
			<-p1Block
		}
		w.WriteHeader(200)
		fmt.Fprint(w, `{"id":"1","model":"m1"}`)
	}))
	defer func() {
		select {
		case <-p1Block:
		default:
			close(p1Block)
		}
		srv1.Close()
	}()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p2Hits.Add(1)
		w.WriteHeader(200)
		fmt.Fprint(w, `{"id":"2","model":"m2"}`)
	}))
	defer srv2.Close()

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1, concurrency: 1, concurrency_queue: 1s}
  - {name: p2, base_url: {openai-completions: %s}, api_key: k2}
models:
  vm:
    sticky: true
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1], priority: 1}
        - {providers: [p2], models: [m2], priority: 2}
`, srv1.URL, srv2.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	// Step 1: Prime sticky session on p1
	sessionBody := []byte(`{"model":"vm","messages":[{"role":"system","content":"cancel-test"},{"role":"user","content":"u1"}]}`)
	serveReq(rt, "vm", sessionBody)

	// Step 2: Occupy p1
	go func() {
		serveReq(rt, "vm", []byte(`{"model":"vm","messages":[{"role":"user","content":"hold"}]}`))
	}()
	select {
	case <-p1Entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for hold request to enter p1")
	}

	// Step 3: Turn 2 with cancelable context, canceled after 30ms (well before 1s queue timeout)
	ctx, cancel := context.WithCancel(context.Background())
	turn2Body := []byte(`{"model":"vm","messages":[{"role":"system","content":"cancel-test"},{"role":"user","content":"u1"},{"role":"assistant","content":"hi"},{"role":"user","content":"u2"}]}`)
	req := httptest.NewRequestWithContext(ctx, "POST", "/v1/chat/completions", bytes.NewReader(turn2Body))
	w := httptest.NewRecorder()

	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	rt.Serve(w, req, &core.CanonicalRequest{Model: "vm", Raw: turn2Body}, "openai-completions", rt.Snapshot(), nil)

	// Verify p2 was NEVER touched
	if p2Hits.Load() != 0 {
		t.Errorf("p2Hits = %d, want 0 (client cancel must not failover to backup)", p2Hits.Load())
	}

	// Verify X-VMR-Failover does NOT say busy_timeout
	failover := w.Header().Get("X-VMR-Failover")
	if strings.Contains(failover, "busy_timeout") {
		t.Errorf("X-VMR-Failover = %q, must NOT contain busy_timeout when canceled by client", failover)
	}

	// Verify error response message
	body := w.Body.String()
	wantMsg := `request canceled by client for model \"vm\"`
	if !strings.Contains(body, wantMsg) {
		t.Errorf("response body = %q, want it to contain %q", body, wantMsg)
	}

	// Verify queue counter did not leak
	l1 := rt.ProviderLimiters.Get("p1")
	if l1.waiting.Load() != 0 {
		t.Errorf("waiting count leaked: got %d, want 0", l1.waiting.Load())
	}
}
