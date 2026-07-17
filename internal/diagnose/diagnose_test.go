// Ver 2026-07-13 19:00, by Sonnet 5
package diagnose

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"vmr/internal/config"
	"vmr/internal/core"

	_ "vmr/internal/adapter/openai"
)

func mkEndpoint(cfg *config.Config, protocol, provider, model string) *core.Endpoint {
	p := cfg.Providers[protocol][provider]
	return &core.Endpoint{Provider: provider, AdapterType: protocol, BaseURL: p.BaseURL, APIKey: p.APIKey, Model: model}
}

func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnvCheck_DNSFailure(t *testing.T) {
	cfg, err := config.Parse([]byte(`
listen: 127.0.0.1:0
providers:
  openai: {p1: {base_url: "https://this-host-does-not-exist.invalid", api_key: k}}
models:
  openai: {vm: {endpoints: [{provider: p1, model: m}]}}
`))
	if err != nil {
		t.Fatal(err)
	}
	r := envCheck(context.Background(), cfg, "openai", "p1", cfg.Providers["openai"]["p1"])
	if r.Status != StatusFail {
		t.Errorf("status = %s, want fail", r.Status)
	}
	if !strings.Contains(r.Detail, "dns:FAIL") {
		t.Errorf("detail = %q, want dns:FAIL", r.Detail)
	}
}

func TestEnvCheck_UntrustedTLSFails(t *testing.T) {
	// httptest.NewTLSServer uses a self-signed cert: a real client (and
	// diagnose, which validates against system roots like one) must reject
	// it — this proves the TLS check actually verifies the chain, not just
	// "a TLS handshake happened".
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	cfg, err := config.Parse([]byte(fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai: {p1: {base_url: %q, api_key: k}}
models:
  openai: {vm: {endpoints: [{provider: p1, model: m}]}}
`, ts.URL)))
	if err != nil {
		t.Fatal(err)
	}
	r := envCheck(context.Background(), cfg, "openai", "p1", cfg.Providers["openai"]["p1"])
	if r.Status != StatusFail || !strings.Contains(r.Detail, "tls:FAIL") {
		t.Errorf("result = %+v, want fail with tls:FAIL", r)
	}
}

func TestEnvCheck_EmptyAPIKeyWarns(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	cfg, err := config.Parse([]byte(fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai: {p1: {base_url: %q, api_key: ""}}
models:
  openai: {vm: {endpoints: [{provider: p1, model: m}]}}
`, ts.URL)))
	if err != nil {
		t.Fatal(err)
	}
	r := envCheck(context.Background(), cfg, "openai", "p1", cfg.Providers["openai"]["p1"])
	if r.Status != StatusWarn || !strings.Contains(r.Detail, "api_key:EMPTY") {
		t.Errorf("result = %+v, want warn with api_key:EMPTY", r)
	}
}

func TestEnvCheck_ProxyReachability(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream.Close()

	openProxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer openProxy.Close()
	go func() {
		for {
			c, err := openProxy.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	cfg, err := config.Parse([]byte(fmt.Sprintf(`
listen: 127.0.0.1:0
http_proxy: "http://%s"
providers:
  openai: {p1: {base_url: %q, api_key: k}}
models:
  openai: {vm: {endpoints: [{provider: p1, model: m}]}}
`, openProxy.Addr().String(), upstream.URL)))
	if err != nil {
		t.Fatal(err)
	}
	r := envCheck(context.Background(), cfg, "openai", "p1", cfg.Providers["openai"]["p1"])
	if !strings.Contains(r.Detail, "proxy:ok") {
		t.Errorf("detail = %q, want proxy:ok (an accepting listener stands in for a reachable proxy)", r.Detail)
	}
}

// TestEnvCheck_ProxiedProviderSkipsDirectDNS locks in the fix for a real
// false-positive: a provider only ever reachable through the configured
// proxy (router.NewUpstreamClient never dials it directly) must not be
// reported as failing because a direct DNS lookup for its host fails —
// that lookup answers a question the real request path never asks.
func TestEnvCheck_ProxiedProviderSkipsDirectDNS(t *testing.T) {
	openProxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer openProxy.Close()
	go func() {
		for {
			c, err := openProxy.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	cfg, err := config.Parse([]byte(fmt.Sprintf(`
listen: 127.0.0.1:0
http_proxy: "http://%s"
providers:
  openai: {p1: {base_url: "http://this-host-does-not-exist.invalid", api_key: k}}
models:
  openai: {vm: {endpoints: [{provider: p1, model: m}]}}
`, openProxy.Addr().String())))
	if err != nil {
		t.Fatal(err)
	}
	r := envCheck(context.Background(), cfg, "openai", "p1", cfg.Providers["openai"]["p1"])
	if r.Status != StatusOK {
		t.Errorf("status = %s, want ok (a proxy-only-reachable host must not fail on a direct DNS check it never needs); detail=%q", r.Status, r.Detail)
	}
	if strings.Contains(r.Detail, "dns:") {
		t.Errorf("detail = %q, should not attempt a direct DNS lookup when a proxy applies", r.Detail)
	}
	if !strings.Contains(r.Detail, "proxy:ok") {
		t.Errorf("detail = %q, want proxy:ok", r.Detail)
	}
}

func TestTestEndpoint_StatusClassification(t *testing.T) {
	for _, tc := range []struct {
		name       string
		httpStatus int
		body       string
		wantStatus Status
		wantSubstr string
	}{
		{"ok", http.StatusOK, `{}`, StatusOK, "200 OK"},
		{"unauthorized", http.StatusUnauthorized, `{}`, StatusFail, "auth failed"},
		{"not_found", http.StatusNotFound, `{}`, StatusFail, "model not found"},
		{"rate_limited", http.StatusTooManyRequests, `{}`, StatusWarn, "rate-limited"},
		{"server_error", http.StatusInternalServerError, `{}`, StatusFail, "upstream error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.httpStatus)
				fmt.Fprint(w, tc.body)
			}))
			defer ts.Close()
			cfg, err := config.Parse([]byte(fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai: {p1: {base_url: %q, api_key: k}}
models:
  openai: {vm: {endpoints: [{provider: p1, model: m}]}}
`, ts.URL)))
			if err != nil {
				t.Fatal(err)
			}
			ep := mkEndpoint(cfg, "openai", "p1", "m")
			r := testEndpoint(context.Background(), cfg, ep, 5*time.Second)
			if r.Status != tc.wantStatus {
				t.Errorf("status = %s, want %s (detail=%q)", r.Status, tc.wantStatus, r.Detail)
			}
			if !strings.Contains(r.Detail, tc.wantSubstr) {
				t.Errorf("detail = %q, want substring %q", r.Detail, tc.wantSubstr)
			}
		})
	}
}

func TestTestEndpoint_NetworkError(t *testing.T) {
	// Nothing listens here: a closed local listener's former address.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	cfg, err := config.Parse([]byte(fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai: {p1: {base_url: "http://%s", api_key: k}}
models:
  openai: {vm: {endpoints: [{provider: p1, model: m}]}}
`, addr)))
	if err != nil {
		t.Fatal(err)
	}
	ep := mkEndpoint(cfg, "openai", "p1", "m")
	r := testEndpoint(context.Background(), cfg, ep, 2*time.Second)
	if r.Status != StatusFail || !strings.Contains(r.Detail, "network error") {
		t.Errorf("result = %+v, want fail with network error", r)
	}
}

func TestRun_FullReport(t *testing.T) {
	goodUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	}))
	defer goodUp.Close()
	badUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"model not found"}`)
	}))
	defer badUp.Close()

	cfgPath := writeConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai:
    good: {base_url: %q, api_key: k1}
    bad:  {base_url: %q, api_key: k2}
models:
  openai:
    vm:
      endpoints:
        - {provider: bad, model: m, priority: 0}
        - {provider: good, model: m, priority: 1}
`, goodUp.URL, badUp.URL))

	rep, err := Run(context.Background(), Options{ConfigPath: cfgPath, TestRouting: true, TestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep == nil {
		t.Fatal("rep is nil")
	}

	var haveConfig, haveEnvGood, haveEnvBad, haveConnGood, haveConnBad bool
	var routeEntries []Result
	for _, r := range rep.Results {
		switch {
		case r.Phase == "config":
			haveConfig = true
		case r.Phase == "env" && r.Target == "openai/good":
			haveEnvGood = true
		case r.Phase == "env" && r.Target == "openai/bad":
			haveEnvBad = true
		case r.Phase == "connect" && strings.Contains(r.Target, "/good/"):
			haveConnGood = true
			if r.Status != StatusOK {
				t.Errorf("good endpoint connect status = %s, want ok", r.Status)
			}
		case r.Phase == "connect" && strings.Contains(r.Target, "/bad/"):
			haveConnBad = true
			if r.Status != StatusFail {
				t.Errorf("bad endpoint connect status = %s, want fail", r.Status)
			}
		case r.Phase == "route":
			routeEntries = append(routeEntries, r)
		}
	}
	if !haveConfig || !haveEnvGood || !haveEnvBad || !haveConnGood || !haveConnBad {
		t.Fatalf("missing expected result rows: config=%v envGood=%v envBad=%v connGood=%v connBad=%v",
			haveConfig, haveEnvGood, haveEnvBad, haveConnGood, haveConnBad)
	}
	if len(routeEntries) != 2 {
		t.Fatalf("route entries = %d, want 2", len(routeEntries))
	}
	// priority 0 (bad) sorts first; its connectivity failure must be
	// reflected in the route preview even though diagnose never touches a
	// live health registry.
	if !strings.Contains(routeEntries[0].Target, "1. bad/m") {
		t.Errorf("first route entry = %+v, want priority-0 (bad) endpoint first", routeEntries[0])
	}
	if routeEntries[0].Status != StatusFail {
		t.Errorf("route entry for the failing endpoint = %+v, want fail", routeEntries[0])
	}
	if routeEntries[1].Status != StatusOK {
		t.Errorf("route entry for the healthy endpoint = %+v, want ok", routeEntries[1])
	}
	if got := rep.FailCount(); got == 0 {
		t.Errorf("FailCount() = %d, want > 0 (bad endpoint should count)", got)
	}

	table := FormatTable(rep)
	if !strings.Contains(table, "Summary:") {
		t.Errorf("FormatTable output missing summary line: %q", table)
	}
}

// TestRun_ChecksRunConcurrently locks in the fix for the worst-case-latency
// problem: several slow providers must not each be waited out sequentially.
// Phase 2 never sends an HTTP request (DNS + TLS-if-https only, and these
// providers are plain http:// so not even TLS), so only Phase 3's real
// request hits the slow handler — with N=6 endpoints each taking 300ms,
// sequential Phase 3 would take at least N*300ms=1.8s; concurrent execution
// (checkConcurrency covers all 6 at once) should finish in well under half
// that.
func TestRun_ChecksRunConcurrently(t *testing.T) {
	const (
		delay = 300 * time.Millisecond
		n     = 6
	)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		fmt.Fprint(w, `{}`)
	}))
	defer slow.Close()

	var providers strings.Builder
	var models strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&providers, "    p%d: {base_url: %q, api_key: k}\n", i, slow.URL)
		fmt.Fprintf(&models, "    vm%d: {endpoints: [{provider: p%d, model: m}]}\n", i, i)
	}
	cfgPath := writeConfig(t, fmt.Sprintf("listen: 127.0.0.1:0\nproviders:\n  openai:\n%s\nmodels:\n  openai:\n%s", providers.String(), models.String()))

	start := time.Now()
	rep, err := Run(context.Background(), Options{ConfigPath: cfgPath, TestRouting: true, TestTimeout: 5 * time.Second})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := rep.FailCount(); got != 0 {
		t.Errorf("FailCount() = %d, want 0 (all providers respond, just slowly)", got)
	}
	// Sequential Phase 3 alone would take at least n*delay=1.8s; concurrent
	// execution should land well under half of that.
	if want := n * delay / 2; elapsed >= want {
		t.Errorf("elapsed = %s, want under %s (checks should run concurrently, sequential worst case is %s)", elapsed, want, n*delay)
	}
}

func TestRun_NoTestRoutingSkipsConnectivity(t *testing.T) {
	cfgPath := writeConfig(t, `
listen: 127.0.0.1:0
providers:
  openai: {p1: {base_url: "http://127.0.0.1:1/unreachable", api_key: k}}
models:
  openai: {vm: {endpoints: [{provider: p1, model: m}]}}
`)
	rep, err := Run(context.Background(), Options{ConfigPath: cfgPath, TestRouting: false})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, r := range rep.Results {
		if r.Phase == "connect" {
			t.Errorf("connect phase should be skipped, got %+v", r)
		}
	}
	// The route preview should still appear, just without any annotation.
	var sawRoute bool
	for _, r := range rep.Results {
		if r.Phase == "route" {
			sawRoute = true
			if r.Status != StatusOK {
				t.Errorf("route entry without a connectivity test should default to ok, got %+v", r)
			}
		}
	}
	if !sawRoute {
		t.Error("expected at least one route entry")
	}
}

func TestRun_BadConfigReturnsNilReport(t *testing.T) {
	cfgPath := writeConfig(t, "not: [valid, config")
	rep, err := Run(context.Background(), Options{ConfigPath: cfgPath})
	if err == nil {
		t.Fatal("expected an error for invalid config")
	}
	if rep != nil {
		t.Errorf("rep = %+v, want nil on config failure", rep)
	}
}

// TestSnippetRuneSafe: provider error bodies are frequently Chinese; the
// 120-byte cut must land on a rune boundary, never mid-character.
func TestSnippetRuneSafe(t *testing.T) {
	body := []byte(strings.Repeat("错", 100)) // 300 bytes of 3-byte runes
	got := snippet(body)
	if !utf8.ValidString(got) {
		t.Errorf("snippet produced invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("long body must be truncated with an ellipsis: %q", got)
	}
	if short := snippet([]byte("plain")); short != "plain" {
		t.Errorf("short body must pass through: %q", short)
	}
}
