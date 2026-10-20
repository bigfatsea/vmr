package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/router"
	"vmr/internal/stats"
)

// statsTestServer wires a one-provider router (provider p1 with the api_keys
// label "prod", so the expanded provider name is p1-prod) to a fresh
// statistics store. apiKeys nil = an unauthenticated instance.
func statsTestServer(t *testing.T, upstream http.HandlerFunc, apiKeys bool) (*httptest.Server, *stats.Store) {
	t.Helper()
	u := newJSONUpstream(t, upstream)
	yaml := "listen: 127.0.0.1:0\n"
	if apiKeys {
		yaml += "api_keys:\n  - sk-vmr-team-alice\n"
	}
	yaml += `providers:
  - name: p1
    base_url: {openai-completions: ` + u.URL + `}
    api_keys:
      prod: sk-upstream-secret-key-123
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [model-two]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	st, err := stats.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	// New(rt, nil): no audit logger — statistics must not depend on it.
	ts := httptest.NewServer(New(rt, nil).WithStats(st).Handler())
	t.Cleanup(ts.Close)
	return ts, st
}

func okUpstream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","model":"model-two","choices":[],"usage":{"prompt_tokens":40,"completion_tokens":20}}`))
}

func getWithKey(t *testing.T, url, key string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

// waitEvents polls the store until n events are booked: the completion
// hook runs after the client already has its response.
func waitEvents(t *testing.T, st *stats.Store, n int) []stats.Event {
	t.Helper()
	var evs []stats.Event
	for i := 0; i < 200; i++ {
		if evs = st.Events(stats.EventsQuery{Limit: 100}); len(evs) >= n {
			return evs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("booked events = %d, want %d", len(evs), n)
	return nil
}

func TestStatsRoutes_AuthShellsAndRedirect(t *testing.T) {
	ts, _ := statsTestServer(t, okUpstream, true)
	const key = "sk-vmr-team-alice"

	for _, path := range []string{"/stats/query", "/stats/events", "/stats/schema", "/status/live"} {
		if resp, _ := getWithKey(t, ts.URL+path, ""); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s without a key = %d, want 401", path, resp.StatusCode)
		}
		if resp, _ := getWithKey(t, ts.URL+path, key); resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s with a key = %d, want 200", path, resp.StatusCode)
		}
	}

	// The page shell is data-free and unauthenticated, with the console
	// chrome injected (no marker left behind).
	resp, body := getWithKey(t, ts.URL+"/stats.html", "")
	if resp.StatusCode != 200 || !strings.Contains(string(body), "VMRStats") || strings.Contains(string(body), "{{CONSOLE_JS}}") {
		t.Errorf("/stats.html: status %d, console injected=%v", resp.StatusCode, !strings.Contains(string(body), "{{CONSOLE_JS}}"))
	}
	if !strings.Contains(string(body), "mountConsole") {
		t.Error("/stats.html must carry the shared console script")
	}

	// Assets: stats.js revalidates by ETag, uPlot is immutable.
	resp, _ = getWithKey(t, ts.URL+"/stats/assets/stats.js", "")
	etag := resp.Header.Get("ETag")
	if resp.StatusCode != 200 || etag == "" || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("stats.js: status %d etag %q cache %q", resp.StatusCode, etag, resp.Header.Get("Cache-Control"))
	}
	req, _ := http.NewRequest("GET", ts.URL+"/stats/assets/stats.js", nil)
	req.Header.Set("If-None-Match", etag)
	if r2, err := http.DefaultClient.Do(req); err != nil || r2.StatusCode != http.StatusNotModified {
		t.Errorf("stats.js revalidation: %v %v, want 304", err, r2)
	}
	resp, _ = getWithKey(t, ts.URL+"/stats/assets/uplot-1.6.32.min.js", "")
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("uplot: status %d cache %q", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
	for name, ct := range map[string]string{"dashboard.js": "text/javascript", "dashboard.css": "text/css"} {
		resp, _ = getWithKey(t, ts.URL+"/stats/assets/"+name, "")
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), ct) || resp.Header.Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: status %d type %q cache %q", name, resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Cache-Control"))
		}
	}
	if resp, _ := getWithKey(t, ts.URL+"/stats/assets/nope.js", ""); resp.StatusCode != 404 {
		t.Errorf("unknown asset = %d, want 404", resp.StatusCode)
	}

	// The old JSON endpoint now only redirects a human to the page.
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r3, err := noFollow.Get(ts.URL + "/stats")
	if err != nil || r3.StatusCode != http.StatusFound || r3.Header.Get("Location") != "/stats.html" {
		t.Errorf("GET /stats = %v %v, want 302 → /stats.html", err, r3)
	}

	// A malformed query is a 400 with a reason.
	resp, body = getWithKey(t, ts.URL+"/stats/query?metrics=nope", key)
	if resp.StatusCode != 400 || !strings.Contains(string(body), "unknown metric") {
		t.Errorf("bad query = %d %s, want 400 unknown metric", resp.StatusCode, body)
	}
}

func TestStatsRoutes_BookAndQuery(t *testing.T) {
	ts, st := statsTestServer(t, okUpstream, true)
	const key = "sk-vmr-team-alice"
	if resp, _ := chat(t, ts, simpleReq, map[string]string{"Authorization": "Bearer " + key}); resp.StatusCode != 200 {
		t.Fatalf("chat status = %d", resp.StatusCode)
	}
	evs := waitEvents(t, st, 1)
	e := evs[0]
	if e.VModel != "vm" || e.Provider != "p1-prod" || e.Model != "model-two" || e.KeyLabel != "prod" || e.ClientKeyTag != "alice" || !e.Forwarded || e.Outcome != stats.OutcomeOK {
		t.Fatalf("booked event = %+v", e)
	}

	resp, body := getWithKey(t, ts.URL+"/stats/query?from=-1h&group=endpoint,client&metrics=req,tok_out,err_rate", key)
	if resp.StatusCode != 200 {
		t.Fatalf("query = %d %s", resp.StatusCode, body)
	}
	var res stats.Result
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("rows = %v", res.Rows)
	}
	row := res.Rows[0]
	if row[0] != "p1-prod:model-two" || row[1] != "alice" || row[2] != float64(1) || row[3] != float64(20) || row[4] != float64(0) {
		t.Errorf("row = %v, want [p1-prod:model-two alice 1 20 0]", row)
	}

	resp, body = getWithKey(t, ts.URL+"/stats/events?from=-1h&where=client:alice", key)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"provider":"p1-prod"`) {
		t.Errorf("events = %d %s", resp.StatusCode, body)
	}
}

// TestStatsPreProbeFailuresNotBooked: a request that never reached a
// virtual model (401, invalid JSON, missing model) has nothing to attribute
// and is not booked; a request that routes, even to an unknown model (404,
// no candidate), is.
func TestStatsPreProbeFailuresNotBooked(t *testing.T) {
	ts, st := statsTestServer(t, okUpstream, true)
	auth := map[string]string{"Authorization": "Bearer sk-vmr-team-alice"}
	if resp, _ := chat(t, ts, simpleReq, map[string]string{"Authorization": "Bearer wrong"}); resp.StatusCode != 401 {
		t.Fatalf("wrong key status = %d, want 401", resp.StatusCode)
	}
	if resp, _ := chat(t, ts, `{not json`, auth); resp.StatusCode != 400 {
		t.Fatalf("invalid JSON status = %d, want 400", resp.StatusCode)
	}
	if resp, _ := chat(t, ts, `{"messages":[]}`, auth); resp.StatusCode != 400 {
		t.Fatalf("missing model status = %d, want 400", resp.StatusCode)
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(st.Events(stats.EventsQuery{Limit: 10})); n != 0 {
		t.Fatalf("pre-probe failures booked: %d events", n)
	}

	if resp, _ := chat(t, ts, `{"model":"missing","messages":[{"role":"user","content":"hi"}]}`, auth); resp.StatusCode != 404 {
		t.Fatalf("unknown model status = %d, want 404", resp.StatusCode)
	}
	e := waitEvents(t, st, 1)[0]
	if e.Outcome != stats.OutcomeError || e.ErrorClass != "no_candidate" || e.Status != 404 || e.Attempts != 0 || e.Provider != "" {
		t.Errorf("no-candidate event = %+v, want error/no_candidate/404/0 attempts/no endpoint", e)
	}
}

func TestStatsRoutes_NoStore503(t *testing.T) {
	rt := router.New(nil)
	ts := httptest.NewServer(New(rt, nil).Handler())
	t.Cleanup(ts.Close)
	for _, path := range []string{"/stats/query", "/stats/events"} {
		if resp, _ := getWithKey(t, ts.URL+path, ""); resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("GET %s without a store = %d, want 503", path, resp.StatusCode)
		}
	}
	rec := httptest.NewRecorder()
	stats.NewHandler(nil).Schema(rec, httptest.NewRequest("GET", "/stats/schema", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"metrics"`) {
		t.Errorf("schema must not need a store, got %d", rec.Code)
	}
}
