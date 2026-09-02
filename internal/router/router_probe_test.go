// Ver 2026-08-02 12:30, by Sonnet 5
//
// runProbe protocol dispatch test:
// Regression test: ensures Responses endpoints send appropriate probe bodies,
// preventing healthy endpoints from being misclassified and stuck half-open.
package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vmr/internal/adapter"
	"vmr/internal/core"

	_ "vmr/internal/adapter/openairesponses"
)

// panicAdapter is a test-only adapter that panics on demand — used to verify
// runProbe/tryOne recover an unexpected panic and still release the half-open
// probe slot (Q02/Q03), instead of crashing the process or locking the
// endpoint into "probing" forever.
type panicAdapter struct {
	panicIn string // "build" = panic in BuildRequest, "classify" = panic in ClassifyError
}

func (p panicAdapter) Protocol() string { return "openai-completions" }

func (p panicAdapter) ResolveURL(baseURL string) string { return baseURL + "/v1/chat/completions" }

func (p panicAdapter) BuildRequest(ctx context.Context, ep *core.Endpoint, req *core.CanonicalRequest) (*http.Request, []byte, error) {
	if p.panicIn == "build" {
		panic("panicAdapter: injected BuildRequest panic")
	}
	r, err := http.NewRequest(http.MethodPost, ep.FullURL, nil)
	return r, nil, err
}

func (p panicAdapter) ClassifyError(int, []byte) core.ErrorClass {
	if p.panicIn == "classify" {
		panic("panicAdapter: injected ClassifyError panic")
	}
	return core.ErrTransient
}

// recordingUpstream captures the raw body of every request it receives and
// answers with a fixed 200 JSON body — enough for runProbe to record a
// success, which is all these tests need (they assert on the outbound
// probe body, not the health outcome).
func recordingUpstream(t *testing.T, captured *[]byte, respBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*captured = b
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunProbe_PanicRecovery(t *testing.T) {
	t.Parallel()
	// Unique name per run: the adapter registry has no unregister, so a
	// fixed name would panic on a second Register (e.g. -count=2).
	panicAdapterName := fmt.Sprintf("test-panic-probe-%d", time.Now().UnixNano())
	adapter.Register(panicAdapterName, panicAdapter{panicIn: "build"})

	var captured []byte
	upstream := recordingUpstream(t, &captured, `{}`)

	cfg := mustConfig(t, `
listen: 127.0.0.1:0
probe_timeout: 2s
providers:
  - {name: p1, base_url: {openai-completions: `+upstream.URL+`}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [model-one]}
`)
	// Override the endpoint's adapter type to the panic adapter.
	snap := mustSnapshot(t, cfg)
	ep := snap.Models["openai-completions"]["vm"].Endpoints[0]
	ep.AdapterType = panicAdapterName

	rt := New(nil)
	rt.Install(snap)
	snap = rt.Snapshot()

	key := ep.HealthKey()

	// Simulate a half-open probing state: ReportFailure at a past time so
	// the cooldown is already expired, then Acquire to claim the probe slot.
	rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now().Add(-10*time.Second))
	if !rt.Health.Acquire(key, time.Now()) {
		t.Fatal("Acquire should succeed after expired cooldown")
	}

	// runProbe must recover the panic, call ReportNeutral, and release the slot.
	// The panic is caught inside runProbe, so calling it synchronously is safe.
	rt.runProbe(ep, snap)

	// After recover + ReportNeutral, the endpoint should be available again.
	if !rt.Health.Available(key, time.Now()) {
		t.Error("endpoint should be available after panic recovery (probing slot released)")
	}
}

func TestRunProbe_ResponsesProtocolSendsResponsesShapedBody(t *testing.T) {
	t.Parallel()
	var captured []byte
	upstream := recordingUpstream(t, &captured, `{"id":"resp_1","model":"m","output":[]}`)

	cfg := mustConfig(t, `
listen: 127.0.0.1:0
probe_timeout: 2s
providers:
  - {name: p1, base_url: {openai-responses: `+upstream.URL+`}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-responses, providers: [p1], models: [model-one]}
`)
	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Install(snap)
	snap = rt.Snapshot()
	ep := snap.Models["openai-responses"]["vm"].Endpoints[0]

	rt.runProbe(ep, snap)

	if captured == nil {
		t.Fatal("probe never reached the upstream")
	}
	if bytes.Contains(captured, []byte(`"messages"`)) {
		t.Errorf("probe body must not be Chat-Completions-shaped for a Responses endpoint: %s", captured)
	}
	if !bytes.Contains(captured, []byte(`"input"`)) {
		t.Errorf("probe body must carry the top-level \"input\" field Responses requires: %s", captured)
	}
}

func TestRunProbe_ChatCompletionsProtocolUnaffected(t *testing.T) {
	t.Parallel()
	var captured []byte
	upstream := recordingUpstream(t, &captured, `{"id":"x","choices":[]}`)

	cfg := mustConfig(t, `
listen: 127.0.0.1:0
probe_timeout: 2s
providers:
  - {name: p1, base_url: {openai-completions: `+upstream.URL+`}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [model-one]}
`)
	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Install(snap)
	snap = rt.Snapshot()
	ep := snap.Models["openai-completions"]["vm"].Endpoints[0]

	rt.runProbe(ep, snap)

	if captured == nil {
		t.Fatal("probe never reached the upstream")
	}
	if !bytes.Contains(captured, []byte(`"messages"`)) {
		t.Errorf("the existing Chat Completions probe shape must be unchanged: %s", captured)
	}
}

func TestRunProbe_ContextCanceledDuringInFlight(t *testing.T) {
	t.Parallel()
	blockUpstream := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockUpstream
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()
	defer close(blockUpstream)

	cfg := mustConfig(t, `
listen: 127.0.0.1:0
probe_timeout: 5s
providers:
  - {name: p1, base_url: {openai-completions: `+srv.URL+`}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [model-one]}
`)
	snap := mustSnapshot(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	rt := New(nil).WithContext(ctx)
	rt.Install(snap)
	snap = rt.Snapshot()
	ep := snap.Models["openai-completions"]["vm"].Endpoints[0]
	key := ep.HealthKey()

	rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now().Add(-10*time.Second))
	if !rt.Health.Acquire(key, time.Now()) {
		t.Fatal("Acquire should succeed after expired cooldown")
	}

	done := make(chan struct{})
	go func() {
		rt.runProbe(ep, snap)
		close(done)
	}()

	// Wait briefly so probe begins HTTP request, then cancel router context.
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Succeeded in aborting probe without waiting for full 5s probe_timeout.
	case <-time.After(2 * time.Second):
		t.Fatal("runProbe did not return promptly when router context was canceled")
	}

	// Probe slot must be released via ReportNeutral, and endpoint available again.
	if !rt.Health.Available(key, time.Now()) {
		t.Error("endpoint should be available after probe canceled by router shutdown")
	}
}

func TestRunProbe_ContextAlreadyCanceled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be reached when context is already canceled")
	}))
	defer srv.Close()

	cfg := mustConfig(t, `
listen: 127.0.0.1:0
probe_timeout: 2s
providers:
  - {name: p1, base_url: {openai-completions: `+srv.URL+`}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [model-one]}
`)
	snap := mustSnapshot(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	rt := New(nil).WithContext(ctx)
	rt.Install(snap)
	snap = rt.Snapshot()
	ep := snap.Models["openai-completions"]["vm"].Endpoints[0]
	key := ep.HealthKey()

	rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now().Add(-10*time.Second))
	if !rt.Health.Acquire(key, time.Now()) {
		t.Fatal("Acquire should succeed after expired cooldown")
	}

	rt.runProbe(ep, snap)

	if !rt.Health.Available(key, time.Now()) {
		t.Error("endpoint should be available after probe canceled on entry")
	}
}
