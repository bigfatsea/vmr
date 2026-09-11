// Ver 2026-09-10, by Sonnet 5
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/livestats"
	"vmr/internal/router"
)

// TestAdaptivePoller_E2E verifies the backend telemetry contract driving
// the console Overview adaptive short poller:
//  1. Inactive: concurrency in_flight is 0, inflight list is empty.
//  2. Active streaming request: in_flight becomes 1, inflight entry appears with
//     "running", sent_at, first_byte_at, last_byte_at, and monotonically increasing est_out.
//  3. Completion: inflight list clears instantaneously, in_flight returns to 0.
//  4. Unauthenticated server: GET /stats returns 200 without Authorization header.
//  5. Authenticated server: GET /stats rejects unauthenticated requests with 401.
func TestAdaptivePoller_E2E(t *testing.T) {
	chunk1Signal := make(chan struct{})
	chunk2Signal := make(chan struct{})
	doneSignal := make(chan struct{})

	// Upstream streaming server simulating multi-chunk SSE responses
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter is not a Flusher")
			return
		}

		// First chunk: wait for test signal
		<-chunk1Signal
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"Hello \"}}]}\n\n")
		flusher.Flush()

		// Second chunk: wait for test signal
		<-chunk2Signal
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"2\",\"choices\":[{\"delta\":{\"content\":\"world!\"}}]}\n\n")
		flusher.Flush()

		// Finish stream
		<-doneSignal
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	t.Cleanup(upstream.Close)

	testKey := "sk-vmr-test-key-0123456789"
	yamlConfig := fmt.Sprintf(`listen: 127.0.0.1:0
api_keys:
  - %s
providers:
  - name: p-test
    base_url: {openai-completions: %s}
    api_keys:
      primary: sk-upstream-key-0123456789
models:
  chat:
    endpoints:
      openai-completions:
        - {providers: [p-test], models: [gpt-test]}
`, testKey, upstream.URL)

	cfg, err := config.Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("config.Parse failed: %v", err)
	}

	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatalf("BuildSnapshot failed: %v", err)
	}
	rt.Install(snap)

	statsDir := t.TempDir()
	lstats, err := livestats.New(statsDir)
	if err != nil {
		t.Fatalf("livestats.New failed: %v", err)
	}
	t.Cleanup(func() { _ = lstats.Close() })

	srv := New(rt, nil).WithLiveStats(lstats)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	client := ts.Client()

	// Helper to poll /stats
	getStats := func(t *testing.T, key string) (statsResponse, int) {
		t.Helper()
		req, err := http.NewRequest("GET", ts.URL+"/stats", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET /stats: %v", err)
		}
		defer resp.Body.Close()

		var stats statsResponse
		if resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
				t.Fatalf("Decode /stats: %v", err)
			}
		}
		return stats, resp.StatusCode
	}

	// Step 1: Initial idle state verification
	{
		// 1a. Unauthenticated request rejected with 401
		_, status := getStats(t, "")
		if status != http.StatusUnauthorized {
			t.Fatalf("initial unauthenticated GET /stats status = %d, want 401", status)
		}

		// 1b. Authenticated request succeeds, idle
		stats, status := getStats(t, testKey)
		if status != http.StatusOK {
			t.Fatalf("initial authenticated GET /stats status = %d, want 200", status)
		}
		if stats.Concurrency.InFlight != 0 {
			t.Errorf("initial in_flight = %d, want 0", stats.Concurrency.InFlight)
		}
		if len(stats.Inflight) != 0 {
			t.Errorf("initial inflight count = %d, want 0", len(stats.Inflight))
		}
	}

	// Step 2: Start background streaming chat request
	var clientWG sync.WaitGroup
	clientWG.Add(1)

	var streamBody string
	var streamErr error
	go func() {
		defer clientWG.Done()
		reqBody := `{"model":"chat","stream":true,"messages":[{"role":"user","content":"Hi"}]}`
		req, err := http.NewRequest("POST", ts.URL+"/v1/chat/completions", strings.NewReader(reqBody))
		if err != nil {
			streamErr = err
			return
		}
		req.Header.Set("Authorization", "Bearer "+testKey)
		req.Header.Set("Content-Type", "application/json")

		res, err := client.Do(req)
		if err != nil {
			streamErr = err
			return
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		if err != nil {
			streamErr = err
			return
		}
		streamBody = string(b)
	}()

	// Wait for request to enter in-flight before chunk 1 is released
	var inflightBeforeChunk1 statsResponse
	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		st, code := getStats(t, testKey)
		if code == http.StatusOK && len(st.Inflight) > 0 {
			inflightBeforeChunk1 = st
			break
		}
	}
	if len(inflightBeforeChunk1.Inflight) == 0 {
		close(chunk1Signal)
		close(chunk2Signal)
		close(doneSignal)
		clientWG.Wait()
		t.Fatal("request failed to register in in-flight table")
	}

	entry0 := inflightBeforeChunk1.Inflight[0]
	if entry0.VModel != "chat" {
		t.Errorf("entry0.VModel = %q, want chat", entry0.VModel)
	}
	if entry0.State != "running" {
		t.Errorf("entry0.State = %q, want running", entry0.State)
	}
	if entry0.Provider != "p-test-primary" || entry0.Model != "gpt-test" || entry0.KeyLabel != "primary" {
		t.Errorf("entry0 triple = %s:%s:%s, want p-test-primary:gpt-test:primary", entry0.Provider, entry0.Model, entry0.KeyLabel)
	}
	if entry0.FirstByteAt != "" {
		t.Errorf("entry0.FirstByteAt before chunk 1 should be empty, got %q", entry0.FirstByteAt)
	}

	// Step 3: Release Chunk 1 and observe first_byte_at and est_out
	close(chunk1Signal)

	var inflightAfterChunk1 statsResponse
	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		st, code := getStats(t, testKey)
		if code == http.StatusOK && len(st.Inflight) > 0 && st.Inflight[0].FirstByteAt != "" {
			inflightAfterChunk1 = st
			break
		}
	}
	if len(inflightAfterChunk1.Inflight) == 0 || inflightAfterChunk1.Inflight[0].FirstByteAt == "" {
		close(chunk2Signal)
		close(doneSignal)
		clientWG.Wait()
		t.Fatal("FirstByteAt was not stamped after chunk 1")
	}

	entry1 := inflightAfterChunk1.Inflight[0]
	firstByteAt := entry1.FirstByteAt
	estOut1 := entry1.EstOut
	if estOut1 <= 0 {
		t.Errorf("estOut after chunk 1 = %d, want > 0", estOut1)
	}

	// Step 4: Release Chunk 2 and observe last_byte_at and est_out increase
	close(chunk2Signal)

	var inflightAfterChunk2 statsResponse
	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		st, code := getStats(t, testKey)
		if code == http.StatusOK && len(st.Inflight) > 0 {
			e := st.Inflight[0]
			if e.EstOut > estOut1 {
				inflightAfterChunk2 = st
				break
			}
		}
	}
	if len(inflightAfterChunk2.Inflight) == 0 {
		close(doneSignal)
		clientWG.Wait()
		t.Fatal("Inflight request vanished before stream finish")
	}
	entry2 := inflightAfterChunk2.Inflight[0]
	if entry2.FirstByteAt != firstByteAt {
		t.Errorf("FirstByteAt changed across chunks: %q vs %q", entry2.FirstByteAt, firstByteAt)
	}
	if entry2.LastByteAt == "" {
		t.Error("LastByteAt should be non-empty after chunk 2")
	}
	if entry2.EstOut <= estOut1 {
		t.Errorf("EstOut did not increase: %d vs %d", entry2.EstOut, estOut1)
	}

	// Step 5: Complete stream and verify instantaneous clearing
	close(doneSignal)
	clientWG.Wait()

	if streamErr != nil {
		t.Fatalf("Stream client error: %v", streamErr)
	}
	if !strings.Contains(streamBody, "Hello ") || !strings.Contains(streamBody, "world!") {
		t.Fatalf("Stream body missing chunks: %q", streamBody)
	}

	// Instantaneous inflight clearing check
	statsDone, code := getStats(t, testKey)
	if code != http.StatusOK {
		t.Fatalf("GET /stats after completion status=%d", code)
	}
	if statsDone.Concurrency.InFlight != 0 {
		t.Errorf("statsDone.Concurrency.InFlight = %d, want 0", statsDone.Concurrency.InFlight)
	}
	if len(statsDone.Inflight) != 0 {
		t.Errorf("statsDone.Inflight length = %d, want 0", len(statsDone.Inflight))
	}
}

// TestAdaptivePoller_UnauthenticatedServer verifies that an unauthenticated VMR instance
// (no api_keys configured) successfully responds to GET /stats without an Authorization header,
// ensuring status.html's adaptive poller functions out of the box in keyless mode.
func TestAdaptivePoller_UnauthenticatedServer(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`))
	}))
	t.Cleanup(upstream.Close)

	yamlConfig := fmt.Sprintf(`listen: 127.0.0.1:0
providers:
  - name: p-free
    base_url: {openai-completions: %s}
models:
  free-model:
    endpoints:
      openai-completions:
        - {providers: [p-free], models: [m-free]}
`, upstream.URL)

	cfg, err := config.Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("config.Parse: %v", err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	rt.Install(snap)

	srv := New(rt, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// GET /stats without auth header must return 200 OK
	req, _ := http.NewRequest("GET", ts.URL+"/stats", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /stats on unauthenticated instance returned status %d, want 200", resp.StatusCode)
	}

	var stats statsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("Decode statsResponse: %v", err)
	}

	if stats.Inflight == nil {
		t.Errorf("stats.Inflight should be non-nil empty slice")
	}
}
