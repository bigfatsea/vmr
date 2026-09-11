// Ver 2026-09-03, by pi-agent

package router

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"vmr/internal/core"
)

func TestInflightRegistry_BasicLifecycle(t *testing.T) {
	reg := NewInflightRegistry()
	if got := reg.Len(); got != 0 {
		t.Fatalf("initial Len = %d, want 0", got)
	}

	start := time.Now().Add(-time.Second)
	h, remove := reg.Register(InflightInitials{
		Protocol:     "openai-completions",
		VModel:       "coding",
		Stream:       true,
		ClientKeyTag: "alice",
		Addr:         "127.0.0.1:54321",
		TS:           start,
		EstIn:        120,
	})
	if h == nil {
		t.Fatal("expected non-nil handle")
	}
	if h.Seq() != 1 {
		t.Errorf("seq = %d, want 1", h.Seq())
	}
	if got := reg.Len(); got != 1 {
		t.Fatalf("after register Len = %d, want 1", got)
	}

	// 1. Queued state before sent
	snaps := reg.Snapshot()
	if len(snaps) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snaps))
	}
	e := snaps[0]
	if e.State != "queued" {
		t.Errorf("state = %q, want queued", e.State)
	}
	if e.SentAt != "" || e.Attempt != 0 || e.Provider != "" || e.Model != "" || e.KeyLabel != "" {
		t.Errorf("queued entry has premature attempt fields: %+v", e)
	}
	if e.FirstByteAt != "" || e.LastByteAt != "" || e.EstOut != 0 {
		t.Errorf("queued entry has premature byte stamps: %+v", e)
	}
	if e.EstIn != 120 {
		t.Errorf("est_in = %d, want 120", e.EstIn)
	}

	// 2. Sent stamp (attempt 1)
	h.stampSent(1, "provider-a", "model-1", "key-lbl-1")
	snaps = reg.Snapshot()
	e = snaps[0]
	if e.State != "running" {
		t.Errorf("state = %q, want running", e.State)
	}
	if e.SentAt == "" {
		t.Error("sent_at must be stamped")
	}
	sentAt1 := e.SentAt
	if e.Attempt != 1 || e.Provider != "provider-a" || e.Model != "model-1" || e.KeyLabel != "key-lbl-1" {
		t.Errorf("attempt 1 triple mismatch: %+v", e)
	}

	// 3. First chunk stamp
	h.stampChunk(15)
	snaps = reg.Snapshot()
	e = snaps[0]
	if e.FirstByteAt == "" || e.LastByteAt == "" {
		t.Fatalf("first/last byte must be stamped: %+v", e)
	}
	firstByte1 := e.FirstByteAt
	lastByte1 := e.LastByteAt
	if e.EstOut != 15 {
		t.Errorf("est_out = %d, want 15", e.EstOut)
	}

	// 4. Second chunk stamp: first_byte_at stays fixed, last_byte_at updates, est_out advances
	time.Sleep(2 * time.Millisecond)
	h.stampChunk(42)
	snaps = reg.Snapshot()
	e = snaps[0]
	if e.FirstByteAt != firstByte1 {
		t.Errorf("first_byte_at moved: %q -> %q", firstByte1, e.FirstByteAt)
	}
	if e.LastByteAt == lastByte1 {
		t.Errorf("last_byte_at did not update on the second chunk: still %q", lastByte1)
	}
	first, errF := time.Parse(time.RFC3339Nano, e.FirstByteAt)
	last, errL := time.Parse(time.RFC3339Nano, e.LastByteAt)
	if errF != nil || errL != nil {
		t.Fatalf("byte stamps not RFC3339Nano: %q %q (%v, %v)", e.FirstByteAt, e.LastByteAt, errF, errL)
	}
	if !last.After(first) {
		t.Errorf("last_byte_at %v not after first_byte_at %v", last, first)
	}
	if e.SentAt != sentAt1 {
		t.Errorf("chunk stamping must not touch sent_at: %q -> %q", sentAt1, e.SentAt)
	}
	if e.EstOut != 42 {
		t.Errorf("est_out = %d, want 42", e.EstOut)
	}

	// 5. Failover attempt stamp (attempt 2 overwrites triple and sent_at)
	h.stampSent(2, "provider-b", "model-2", "key-lbl-2")
	snaps = reg.Snapshot()
	e = snaps[0]
	if e.Attempt != 2 || e.Provider != "provider-b" || e.Model != "model-2" || e.KeyLabel != "key-lbl-2" {
		t.Errorf("attempt 2 failover overwrite failed: %+v", e)
	}

	// 6. Idempotent remove
	remove()
	if got := reg.Len(); got != 0 {
		t.Fatalf("after remove Len = %d, want 0", got)
	}
	if len(reg.Snapshot()) != 0 {
		t.Fatal("snapshot must be empty after remove")
	}

	// Calling remove a second time is a harmless no-op
	remove()
	if got := reg.Len(); got != 0 {
		t.Fatalf("second remove Len = %d, want 0", got)
	}

	// Stamping after remove is safe and does not panic
	h.stampChunk(100)
	h.stampSent(3, "p3", "m3", "k3")
}

func TestInflightRegistry_NilSafe(t *testing.T) {
	var reg *InflightRegistry
	if got := reg.Len(); got != 0 {
		t.Errorf("nil reg Len = %d, want 0", got)
	}
	if snaps := reg.Snapshot(); snaps != nil {
		t.Errorf("nil reg Snapshot = %v, want nil", snaps)
	}
	h, remove := reg.Register(InflightInitials{Protocol: "openai-completions"})
	if h != nil {
		t.Errorf("nil reg Register handle = %v, want nil", h)
	}
	remove() // must not panic

	var nilH *InflightHandle
	if nilH.Seq() != 0 {
		t.Errorf("nil handle Seq = %d, want 0", nilH.Seq())
	}
	nilH.SetEstIn(100)
	nilH.stampSent(1, "p", "m", "k")
	nilH.stampChunk(50)

	ctx := context.Background()
	if WithInflightHandle(ctx, nil) != ctx {
		t.Error("WithInflightHandle(nil) should return original ctx")
	}
	if got := inflightHandleFrom(ctx); got != nil {
		t.Errorf("inflightHandleFrom = %v, want nil", got)
	}
}

func TestInflightRegistry_ConcurrentRace(t *testing.T) {
	reg := NewInflightRegistry()
	const workers = 8
	const iters = 100

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				h, remove := reg.Register(InflightInitials{
					Protocol: "openai-completions",
					VModel:   "vm",
					Addr:     fmt.Sprintf("10.0.0.%d:%d", workerID, j),
				})
				h.SetEstIn(int64(j * 10))
				h.stampSent(1, "p1", "m1", "k1")
				h.stampChunk(int64(j))
				_ = reg.Snapshot()
				h.stampChunk(int64(j * 2))
				remove()
			}
		}(i)
	}
	wg.Wait()

	if got := reg.Len(); got != 0 {
		t.Errorf("final Len = %d, want 0", got)
	}
}

func TestInflight_ServeStreaming(t *testing.T) {
	chunks := []string{
		"data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n",
		"data: {\"id\":\"2\",\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n",
		"data: [DONE]\n\n",
	}
	u := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			w.Write([]byte(c))
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer u.Close()

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: test-key-123456}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
`, u.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	h, remove := rt.Inflight.Register(InflightInitials{
		Protocol:     "openai-completions",
		VModel:       "vm",
		Stream:       true,
		ClientKeyTag: "client-tag",
		Addr:         "127.0.0.1:1234",
	})
	defer remove()

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"vm","stream":true}`)))
	req = req.WithContext(WithInflightHandle(req.Context(), h))
	w := httptest.NewRecorder()

	creq := &core.CanonicalRequest{
		Model:  "vm",
		Stream: true,
		Raw:    []byte(`{"model":"vm","stream":true}`),
		Facts:  core.RequestFacts{EstimatedTokens: 250},
	}

	rt.Serve(w, req, creq, "openai-completions", nil)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Before remove: inspect the stamped handle
	snaps := rt.Inflight.Snapshot()
	if len(snaps) != 1 {
		t.Fatalf("snaps len = %d, want 1", len(snaps))
	}
	e := snaps[0]
	if e.State != "running" {
		t.Errorf("state = %q, want running", e.State)
	}
	if e.Attempt != 1 {
		t.Errorf("attempt = %d, want 1", e.Attempt)
	}
	if e.Provider != "p1" || e.Model != "m1" {
		t.Errorf("provider/model = %s/%s, want p1/m1", e.Provider, e.Model)
	}
	if e.KeyLabel != "123456" {
		t.Errorf("key_label = %q, want 123456", e.KeyLabel)
	}
	if e.SentAt == "" {
		t.Error("sent_at must not be empty")
	}
	if e.FirstByteAt == "" || e.LastByteAt == "" {
		t.Errorf("byte stamps missing: first=%q last=%q", e.FirstByteAt, e.LastByteAt)
	}
	if e.EstIn != 250 {
		t.Errorf("est_in = %d, want 250", e.EstIn)
	}
	if e.EstOut <= 0 {
		t.Errorf("est_out = %d, want > 0", e.EstOut)
	}

	remove()
	if got := rt.Inflight.Len(); got != 0 {
		t.Errorf("after remove Len = %d, want 0", got)
	}
}

func TestInflight_ServeNonStreaming(t *testing.T) {
	u := newMockUpstream(t, 200, `{"id":"test-id","choices":[{"message":{"content":"response text"}}]}`)

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p_sync, base_url: {openai-completions: %s}, api_key: sync-key-654321}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p_sync], models: [m_sync]}
`, u.srv.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	h, remove := rt.Inflight.Register(InflightInitials{
		Protocol: "openai-completions",
		VModel:   "vm",
		Stream:   false,
		Addr:     "127.0.0.1:5678",
	})
	defer remove()

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"vm"}`)))
	req = req.WithContext(WithInflightHandle(req.Context(), h))
	w := httptest.NewRecorder()

	creq := &core.CanonicalRequest{
		Model: "vm",
		Raw:   []byte(`{"model":"vm"}`),
		Facts: core.RequestFacts{EstimatedTokens: 80},
	}

	rt.Serve(w, req, creq, "openai-completions", nil)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	snaps := rt.Inflight.Snapshot()
	if len(snaps) != 1 {
		t.Fatalf("snaps len = %d, want 1", len(snaps))
	}
	e := snaps[0]
	if e.State != "running" {
		t.Errorf("state = %q, want running", e.State)
	}
	if e.Attempt != 1 || e.Provider != "p_sync" || e.Model != "m_sync" {
		t.Errorf("triple = %+v", e)
	}
	if e.FirstByteAt == "" || e.LastByteAt == "" {
		t.Errorf("non-streaming must still stamp first/last byte: %+v", e)
	}
	if e.EstIn != 80 {
		t.Errorf("est_in = %d, want 80", e.EstIn)
	}

	remove()
	if got := rt.Inflight.Len(); got != 0 {
		t.Errorf("after remove Len = %d, want 0", got)
	}
}

func TestInflight_ServeFailoverOverwritesTripleAndIncrementsAttempt(t *testing.T) {
	u1 := newMockUpstream(t, 500, `{"error":"temporary"}`)
	u2 := newMockUpstream(t, 200, `{"id":"ok","model":"m2"}`)

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: key-p1}
  - {name: p2, base_url: {openai-completions: %s}, api_key: key-p2}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
        - {providers: [p2], models: [m2]}
`, u1.srv.URL, u2.srv.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	h, remove := rt.Inflight.Register(InflightInitials{
		Protocol: "openai-completions",
		VModel:   "vm",
		Addr:     "127.0.0.1:9999",
	})
	defer remove()

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"vm"}`)))
	req = req.WithContext(WithInflightHandle(req.Context(), h))
	w := httptest.NewRecorder()

	creq := &core.CanonicalRequest{
		Model: "vm",
		Raw:   []byte(`{"model":"vm"}`),
	}

	rt.Serve(w, req, creq, "openai-completions", nil)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	snaps := rt.Inflight.Snapshot()
	if len(snaps) != 1 {
		t.Fatalf("snaps len = %d, want 1", len(snaps))
	}
	e := snaps[0]
	// Attempt should be 2, and the triple should be provider 2
	if e.Attempt != 2 {
		t.Errorf("attempt = %d, want 2", e.Attempt)
	}
	if e.Provider != "p2" || e.Model != "m2" {
		t.Errorf("triple mismatch after failover: %s/%s, want p2/m2", e.Provider, e.Model)
	}

	remove()
	if got := rt.Inflight.Len(); got != 0 {
		t.Errorf("after remove Len = %d, want 0", got)
	}
}

func TestInflight_Lifecycle_TruncatedPanicCleansUp(t *testing.T) {
	srv := truncatingUpstream(t)
	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p_trunc, base_url: {openai-completions: %s}, api_key: key-trunc}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p_trunc], models: [m_trunc]}
`, srv.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	h, remove := rt.Inflight.Register(InflightInitials{
		Protocol: "openai-completions",
		VModel:   "vm",
		Stream:   true,
		Addr:     "127.0.0.1:4444",
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"vm"}`)))
	req = req.WithContext(WithInflightHandle(req.Context(), h))
	w := httptest.NewRecorder()
	creq := &core.CanonicalRequest{
		Model:  "vm",
		Stream: true,
		Raw:    []byte(`{"model":"vm"}`),
	}

	func() {
		defer remove()
		defer func() {
			r := recover()
			if r != http.ErrAbortHandler {
				t.Fatalf("recover() = %v, want http.ErrAbortHandler", r)
			}
		}()
		rt.Serve(w, req, creq, "openai-completions", nil)
	}()

	// Defer remove was run by the panic unwind: registry must be clean
	if got := rt.Inflight.Len(); got != 0 {
		t.Errorf("after truncated panic unwind, Len = %d, want 0", got)
	}
}

func TestInflight_Lifecycle_ClientCancelCleansUp(t *testing.T) {
	u := newMockUpstream(t, 200, `{"id":"ok","model":"m1"}`)
	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
`, u.srv.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	h, remove := rt.Inflight.Register(InflightInitials{
		Protocol: "openai-completions",
		VModel:   "vm",
		Addr:     "127.0.0.1:8888",
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"vm"}`)))
	req = req.WithContext(WithInflightHandle(req.Context(), h))
	w := &failingWriter{}
	creq := &core.CanonicalRequest{
		Model: "vm",
		Raw:   []byte(`{"model":"vm"}`),
	}

	func() {
		defer remove()
		rt.Serve(w, req, creq, "openai-completions", nil)
	}()

	if got := rt.Inflight.Len(); got != 0 {
		t.Errorf("after client cancel, Len = %d, want 0", got)
	}
}
