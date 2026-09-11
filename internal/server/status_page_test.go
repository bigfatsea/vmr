// Ver 2026-08-23 15:47, by Gemini
package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vmr/internal/config"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

func TestStatusPage_ServesHTML(t *testing.T) {
	cfg, err := config.Parse([]byte(instanceYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)
	srv := New(rt, nil)

	// status.html
	req := httptest.NewRequest("GET", "/status.html", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}

	cc := w.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}

	body := w.Body.String()
	if !strings.Contains(body, "VMR Console — Overview") {
		t.Errorf("body missing 'VMR Console — Overview'")
	}

	// Overview page markers: 5 section anchors (Live Requests and Recent Failures
	// moved to log.html)
	for _, anchor := range []string{
		`id="vitals"`,
		`id="quota"`,
		`id="models"`,
		`id="perf"`,
		`id="traffic"`,
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("body missing overview section anchor %q", anchor)
		}
	}

	if !strings.Contains(body, "mountConsole") {
		t.Errorf("body missing 'mountConsole'")
	}
	if !strings.Contains(body, "active: 'overview'") {
		t.Errorf("body missing mountConsole active: 'overview'")
	}

	// Shared console assets injection
	if !strings.Contains(body, "console-header") {
		t.Errorf("body missing injected console-header class from console.css")
	}
	if !strings.Contains(body, "VMRAuth") {
		t.Errorf("body missing injected VMRAuth from console.js")
	}

	// /stats.html must now return 404
	reqStats := httptest.NewRequest("GET", "/stats.html", nil)
	wStats := httptest.NewRecorder()
	srv.Handler().ServeHTTP(wStats, reqStats)
	if wStats.Code != http.StatusNotFound {
		t.Fatalf("/stats.html status = %d, want %d", wStats.Code, http.StatusNotFound)
	}
}

func TestHelpPage_ServesHTML(t *testing.T) {
	cfg, err := config.Parse([]byte(instanceYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)
	srv := New(rt, nil)

	for _, path := range []string{"/help", "/help.html"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want %d", path, w.Code, http.StatusOK)
		}

		ct := w.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: Content-Type = %q, want text/html", path, ct)
		}

		cc := w.Header().Get("Cache-Control")
		if cc != "no-cache" {
			t.Errorf("%s: Cache-Control = %q, want no-cache", path, cc)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Agent Configuration Guide") {
			t.Errorf("%s: body missing 'Agent Configuration Guide'", path)
		}
		for _, name := range []string{
			"Claude Code",
			"Codex",
			"OpenClaw",
			"OpenCode",
			"Cursor",
			"Hermes",
			"Pi Agent",
			"WorkBuddy",
		} {
			if !strings.Contains(body, name) {
				t.Errorf("%s: body missing %q agent section", path, name)
			}
		}
		if !strings.Contains(body, "help-base-urls") {
			t.Errorf("%s: body missing dynamic connection info section", path)
		}
		if !strings.Contains(body, `href="/status.html"`) {
			t.Errorf("%s: body missing cross-link to /status.html", path)
		}
		if !strings.Contains(body, `href="/help.zh.html"`) {
			t.Errorf("%s: body missing language toggle to /help.zh.html", path)
		}

		// Server-side base-URL injection: the page must carry the request's
		// real host, not a {{...}} placeholder — so a copy / view-source /
		// no-JS reader sees the address it actually reached. httptest sets
		// Host to "example.com".
		if !strings.Contains(body, "http://example.com/v1") {
			t.Errorf("%s: body missing injected OpenAI base URL", path)
		}
		if !strings.Contains(body, "http://example.com") {
			t.Errorf("%s: body missing injected Anthropic base URL", path)
		}
		if strings.Contains(body, "{{BASE_URL_OPENAI}}") || strings.Contains(body, "{{BASE_URL_ANTHROPIC}}") {
			t.Errorf("%s: body still contains {{BASE_URL_*}} placeholders", path)
		}

		// Auth-reveal UX: the page has no page-local auth markup — the shared
		// console keyOverlay (console.js) is the only key entry point, opened on
		// click or on 401 by VMRAuth.guard.
		if !strings.Contains(body, `id="vmr-key-input"`) {
			t.Errorf("%s: body missing shared console auth modal input", path)
		}
	}
}

func TestHelpZHPage_ServesHTML(t *testing.T) {
	cfg, err := config.Parse([]byte(instanceYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)
	srv := New(rt, nil)

	for _, path := range []string{"/help.zh", "/help.zh.html"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want %d", path, w.Code, http.StatusOK)
		}

		ct := w.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: Content-Type = %q, want text/html", path, ct)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Agent 配置指南") {
			t.Errorf("%s: body missing Chinese title 'Agent 配置指南'", path)
		}
		if !strings.Contains(body, "连接信息") {
			t.Errorf("%s: body missing Chinese '连接信息'", path)
		}
		if !strings.Contains(body, `href="/help.html"`) {
			t.Errorf("%s: body missing language toggle to English /help.html", path)
		}
		if !strings.Contains(body, "help-base-urls") {
			t.Errorf("%s: body missing help-base-urls element", path)
		}

		// Server-side base-URL injection works on Chinese page too
		if !strings.Contains(body, "http://example.com/v1") {
			t.Errorf("%s: body missing injected OpenAI base URL", path)
		}
	}
}

// TestHelpPage_SnippetFillEngine is a cheap build-time guard (not a JS test):
// both language pages must carry the render engine + per-agent generators and
// keep the static default literals a no-JS / pre-auth visitor relies on (the
// JS only swaps these once /status is readable). JS behaviour itself is
// verified by hand.
func TestHelpPage_SnippetFillEngine(t *testing.T) {
	cfg, err := config.Parse([]byte(instanceYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)
	srv := New(rt, nil)

	for _, path := range []string{"/help.html", "/help.zh.html"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		body := w.Body.String()

		for _, want := range []string{
			"function renderSnippets(",
			"function fillSnippet(",
			"cacheSnippetTemplates()",
			"function genPi(",
			"function genOpenCode(",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: snippet-fill engine missing %q", path, want)
			}
		}
		// Static defaults must survive verbatim so a copy works before auth.
		for _, want := range []string{
			`<span class="string">YOUR_VMR_API_KEY</span>`,
			`<span class="string">claude</span>`,
			`<span class="bool">200000</span>`,
			`<span class="string">high</span>`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: static snippet default missing %q", path, want)
			}
		}
	}
}

func TestHelpPage_404OnOtherPaths(t *testing.T) {
	cfg, err := config.Parse([]byte(instanceYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)
	srv := New(rt, nil)

	req := httptest.NewRequest("GET", "/help/other", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHelpPage_BaseURLsFollowRequestHost(t *testing.T) {
	cfg, err := config.Parse([]byte(instanceYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)
	srv := New(rt, nil)

	// A visitor on a LAN reaching http://192.168.0.32:8800/help.html must see
	// that exact address in every snippet — the URL they typed, the URL their
	// agent should point at.
	req := httptest.NewRequest("GET", "/help.html", nil)
	req.Host = "192.168.0.32:8800"
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "http://192.168.0.32:8800/v1") {
		t.Errorf("body missing OpenAI base URL for host 192.168.0.32:8800")
	}
	if !strings.Contains(body, "http://192.168.0.32:8800") {
		t.Errorf("body missing Anthropic base URL for host 192.168.0.32:8800")
	}
	if strings.Contains(body, "{{BASE_URL_OPENAI}}") {
		t.Errorf("body still contains {{BASE_URL_OPENAI}} placeholder")
	}
}

func TestHelpPage_MissingHostFallsBackToLoopback(t *testing.T) {
	cfg, err := config.Parse([]byte(instanceYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)
	srv := New(rt, nil)

	req := httptest.NewRequest("GET", "/help.html", nil)
	req.Host = ""
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "http://127.0.0.1/v1") {
		t.Errorf("body missing loopback OpenAI base URL fallback")
	}
	if !strings.Contains(body, "http://127.0.0.1") {
		t.Errorf("body missing loopback Anthropic base URL fallback")
	}
}

func TestStatusPage_AdaptivePollerStructure(t *testing.T) {
	cfg, err := config.Parse([]byte(instanceYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)
	srv := New(rt, nil)

	req := httptest.NewRequest("GET", "/status.html", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()

	// Adaptive poller fast and idle cadences (concurrency vitals)
	for _, want := range []string{
		"const LIVE_POLL_FAST_MS = 2000;",
		"const LIVE_POLL_IDLE_MS = 15000;",
		"function armLivePoll(ms)",
		"async function livePollTick()",
		"renderConcurrencyVitals(",
		"fetch('/stats', { headers })",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("status.html missing poller element %q", want)
		}
	}

	// Poller must NOT call VMRAuth.guard or wrap with authFetch (owns no auth modal)
	if strings.Contains(body, "authFetch('/stats')") {
		t.Errorf("status.html poller must not call authFetch('/stats')")
	}

	// Tab visibility pausing: armLivePoll must drop timer when hidden
	if !strings.Contains(body, "if (document.hidden) return;   // parked — visibilitychange wakes us up") {
		t.Errorf("status.html armLivePoll missing document.hidden check")
	}
}

func TestLogPage_RefreshStatusAuthHeaders(t *testing.T) {
	cfg, err := config.Parse([]byte(instanceYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)
	srv := New(rt, nil)

	req := httptest.NewRequest("GET", "/log.html", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()

	// refreshStatus in log.html must send Authorization header when VMRAuth.has()
	wantHeader := "headers: VMRAuth.has() ? { 'Authorization': 'Bearer ' + VMRAuth.get() } : {}"
	if !strings.Contains(body, wantHeader) {
		t.Errorf("log.html refreshStatus missing auth headers: %q", wantHeader)
	}

	// log.html adaptive poller and moved sections. The layout order is
	// pinned: Live Requests / Recent Failures above, then the terminal
	// toolbar, then the log area (which wraps by default).
	for _, want := range []string{
		`id="live"`,
		`id="failures-head"`,
		`id="failures-body-wrap" hidden`,
		`id="term-toolbar"`,
		"const LIVE_POLL_FAST_MS = 2000;",
		"const LIVE_POLL_IDLE_MS = 15000;",
		"renderLive(",
		"renderFailures(",
		"white-space:pre-wrap",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("log.html missing element %q", want)
		}
	}
	liveIdx := strings.Index(body, `id="live"`)
	toolbarIdx := strings.Index(body, `id="term-toolbar"`)
	logIdx := strings.Index(body, `<pre id="log">`)
	if liveIdx == -1 || toolbarIdx == -1 || logIdx == -1 || liveIdx > toolbarIdx || toolbarIdx > logIdx {
		t.Errorf("log.html layout order wrong: want live (%d) < toolbar (%d) < log area (%d)", liveIdx, toolbarIdx, logIdx)
	}
}
