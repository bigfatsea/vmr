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
	if !strings.Contains(body, "VMR Dashboard") {
		t.Errorf("body missing 'VMR Dashboard'")
	}
	if !strings.Contains(body, "renderDashboard") {
		t.Errorf("body missing 'renderDashboard'")
	}
	if !strings.Contains(body, "fetchStatus") {
		t.Errorf("body missing 'fetchStatus'")
	}
	if !strings.Contains(body, `href="/help.html"`) {
		t.Errorf("body missing cross-link to /help.html")
	}
	if !strings.Contains(body, `href="/log.html"`) {
		t.Errorf("body missing cross-link to /log.html")
	}
	if !strings.Contains(body, "connect-base-urls") {
		t.Errorf("body missing connection info card")
	}
	if !strings.Contains(body, "connect-models") {
		t.Errorf("body missing model list")
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
		if !strings.Contains(body, "Claude Code") {
			t.Errorf("%s: body missing 'Claude Code' agent section", path)
		}
		if !strings.Contains(body, "Pi Agent") {
			t.Errorf("%s: body missing 'Pi Agent' section", path)
		}
		if !strings.Contains(body, "Codex CLI") {
			t.Errorf("%s: body missing 'Codex CLI' section", path)
		}
		if !strings.Contains(body, "Aider") {
			t.Errorf("%s: body missing 'Aider' section", path)
		}
		if !strings.Contains(body, "OpenCode") {
			t.Errorf("%s: body missing 'OpenCode' section", path)
		}
		if !strings.Contains(body, "OpenClaw") {
			t.Errorf("%s: body missing 'OpenClaw' section", path)
		}
		if !strings.Contains(body, "WorkBuddy") {
			t.Errorf("%s: body missing 'WorkBuddy' section", path)
		}
		if !strings.Contains(body, "Hermes") {
			t.Errorf("%s: body missing 'Hermes' section", path)
		}
		if !strings.Contains(body, "OpenDesign") {
			t.Errorf("%s: body missing 'OpenDesign' section", path)
		}
		if !strings.Contains(body, "Continue.dev") {
			t.Errorf("%s: body missing 'Continue.dev' section", path)
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

		// Auth-reveal UX: the page must include the modal and password field
		// (opened on click when auth is configured), never a persistent
		// always-visible input alongside the info card.
		if !strings.Contains(body, "help-auth-modal") {
			t.Errorf("%s: body missing auth modal", path)
		}
		if !strings.Contains(body, `id="help-api-key"`) {
			t.Errorf("%s: body missing API key input in modal", path)
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
			`pre.id === 'responses-py-snippet'`,
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
