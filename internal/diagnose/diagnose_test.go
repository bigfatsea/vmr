// Ver 2026-07-13 19:00, by Sonnet 5
package diagnose

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
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

// echoUpstream returns an httptest.Server that answers a probe.Request-shaped
// body with a 200 whose content echoes back the nonce it was asked for — the
// mock stand-in for "a real, working provider" used by every test that needs
// testEndpoint to classify an endpoint as StatusOK now that a bare 200 is no
// longer enough (see TestTestEndpoint_EchoVerification).
func echoUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		reply := ""
		if err := json.Unmarshal(body, &req); err == nil && len(req.Messages) > 0 {
			content := req.Messages[0].Content
			const prefix = "Reply with exactly this token and nothing else: "
			if i := strings.Index(content, prefix); i >= 0 {
				reply = content[i+len(prefix):]
			}
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q}}]}`, reply)
	}))
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
	if !strings.Contains(r.Detail, "proxy:yes") {
		t.Errorf("detail = %q, want proxy:yes (this provider is configured to go through a proxy)", r.Detail)
	}
	if !strings.Contains(r.Detail, "proxy_reachable:ok") {
		t.Errorf("detail = %q, want proxy_reachable:ok (an accepting listener stands in for a reachable proxy)", r.Detail)
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
	if !strings.Contains(r.Detail, "proxy:yes") || !strings.Contains(r.Detail, "proxy_reachable:ok") {
		t.Errorf("detail = %q, want proxy:yes and proxy_reachable:ok", r.Detail)
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

// TestTestEndpoint_EchoVerification covers the two 200-OK outcomes the nonce
// echo check distinguishes: a server that genuinely echoes probe.Request's
// nonce back (real, fresh completion — StatusOK) versus one that returns 200
// with unrelated content, as a relay/gateway serving a cached or canned
// response would (StatusWarn, not StatusFail — the endpoint IS reachable).
func TestTestEndpoint_EchoVerification(t *testing.T) {
	for _, tc := range []struct {
		name       string
		echo       bool
		wantStatus Status
	}{
		{"nonce echoed back: verified live completion", true, StatusOK},
		{"200 but nonce absent: unverified, not a hard failure", false, StatusWarn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ts *httptest.Server
			if tc.echo {
				ts = echoUpstream(t)
			} else {
				ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"I can't help with that."}}]}`)
				}))
			}
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
	// One unit throughout, not Duration.String()'s default of "141ms" below
	// a second and "1.068s" above it — a mixed-unit column doesn't scan as
	// a column.
	if matched, _ := regexp.MatchString(`\(\d+\.\d{3}s\)`, r.Detail); !matched {
		t.Errorf("detail = %q, want a NN.NNNs latency, not Duration.String()'s default unit-switching format", r.Detail)
	}
}

// TestFormatSeconds locks in the single-unit rendering testEndpoint uses for
// every latency it reports, above and below the 1-second boundary where
// time.Duration.String() would otherwise switch from "141ms" to "1.068s".
func TestFormatSeconds(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{141 * time.Millisecond, "0.141s"},
		{7 * time.Millisecond, "0.007s"},
		{1068 * time.Millisecond, "1.068s"},
		{0, "0.000s"},
	}
	for _, tc := range cases {
		if got := formatSeconds(tc.d); got != tc.want {
			t.Errorf("formatSeconds(%s) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestRun_FullReport(t *testing.T) {
	goodUp := echoUpstream(t)
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
			// "model(s)" alone is ambiguous with the real upstream models
			// named in the Connectivity/Routing sections just below it —
			// this count is virtual models (config.Models), so the label
			// must say so.
			if !strings.Contains(r.Detail, "virtual model(s)") {
				t.Errorf("config detail = %q, want it to say \"virtual model(s)\"", r.Detail)
			}
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
	if routeEntries[0].Group != "vm [openai]" || routeEntries[1].Group != "vm [openai]" {
		t.Errorf("route entries should share Group %q: got %q, %q", "vm [openai]", routeEntries[0].Group, routeEntries[1].Group)
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

// TestRun_ConnectivityResultsSortedByProviderThenModel locks in the fix for
// scattered connectivity output: the old order fell out of iterating
// virtual models (alphabetical by *model* name), so two endpoints on the
// same provider landed apart whenever a different, alphabetically-earlier
// model referenced one of them first. Phase 3 results must instead sort by
// (protocol, provider, model) — every "alpha" endpoint adjacent, regardless
// of which virtual model declared it or in what order.
func TestRun_ConnectivityResultsSortedByProviderThenModel(t *testing.T) {
	up := echoUpstream(t)
	defer up.Close()
	// vm-a (alphabetically first) references zulu before alpha; vm-z (last)
	// references alpha's second model. A model-iteration order would emit
	// zulu/m1, alpha/m2 (from vm-a) then alpha/m1 (from vm-z) — alpha's two
	// endpoints split apart. Provider-sorted order must not do that.
	cfgPath := writeConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai:
    zulu:  {base_url: %[1]q, api_key: k1}
    alpha: {base_url: %[1]q, api_key: k2}
models:
  openai:
    vm-a: {endpoints: [{provider: zulu, model: m1}, {provider: alpha, model: m2}]}
    vm-z: {endpoints: [{provider: alpha, model: m1}]}
`, up.URL))

	rep, err := Run(context.Background(), Options{ConfigPath: cfgPath, TestRouting: true, TestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var targets []string
	for _, r := range rep.Results {
		if r.Phase == "connect" {
			targets = append(targets, r.Target)
		}
	}
	got := strings.Join(targets, ",")
	want := "openai/alpha/m1,openai/alpha/m2,openai/zulu/m1"
	if got != want {
		t.Errorf("connect targets = %q, want %q (provider-sorted, same provider's endpoints adjacent)", got, want)
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

// TestFormatTable_GroupedFixedWidth locks in the layout FormatTable produces:
// one section per phase (blank line separated, human-readable title instead
// of the bare phase string), route entries sub-grouped and indented under
// their Group header, and target columns padded to that section's own
// longest entry so status/detail line up.
func TestFormatTable_GroupedFixedWidth(t *testing.T) {
	ranAt := time.Date(2026, 7, 19, 10, 42, 7, 0, time.UTC)
	rep := &Report{RanAt: ranAt, Results: []Result{
		{Phase: "config", Target: "config.yaml", Status: StatusOK, Detail: "1 provider(s), 1 model(s)"},
		{Phase: "env", Target: "openai/short", Status: StatusOK, Detail: "dns:ok api_key:set"},
		{Phase: "env", Target: "openai/a-much-longer-name", Status: StatusFail, Detail: "dns:FAIL"},
		{Phase: "route", Group: "agent [openai]", Target: "1. p1/m1", Status: StatusOK},
		{Phase: "route", Group: "agent [openai]", Target: "2. p2/a-longer-model-name", Status: StatusFail, Detail: "connectivity test: boom"},
		{Phase: "route", Group: "coding [openai]", Target: "1. p3/m3", Status: StatusOK},
	}}
	table := FormatTable(rep)
	lines := strings.Split(strings.TrimRight(table, "\n"), "\n")

	rule := strings.Repeat("=", 50)
	wantPrefix := []string{
		"", // blank line separates the Result heading from whatever printed before it (e.g. live progress narration)
		rule,
		"Result",
		rule,
		"Config", // no blank line here: Config, like every other section, opens immediately under its title
		"  config.yaml  ok    1 provider(s), 1 model(s)",
		"",
		"Environment",
	}
	for i, want := range wantPrefix {
		if lines[i] != want {
			t.Errorf("line %d = %q, want %q", i, lines[i], want)
		}
	}

	// Both env rows must align: same target-column width within the
	// section, computed from "openai/a-much-longer-name" (25 chars).
	envShort := lines[8]
	envLong := lines[9]
	if envShort != "  openai/short               ok    dns:ok api_key:set" {
		t.Errorf("env short row not padded to the section's longest target: %q", envShort)
	}
	if envLong != "  openai/a-much-longer-name  fail  dns:FAIL" {
		t.Errorf("env long row: %q", envLong)
	}

	if !strings.Contains(table, "\n\nRouting\n  agent [openai]\n") {
		t.Errorf("route section must open with a blank line, the phase title, then the first Group header: %q", table)
	}
	if !strings.Contains(table, "    1. p1/m1") || !strings.Contains(table, "    2. p2/a-longer-model-name") {
		t.Errorf("route rows under a Group must be indented one level deeper than ungrouped rows: %q", table)
	}
	if !strings.Contains(table, "  coding [openai]\n    1. p3/m3") {
		t.Errorf("a Group change must print a new sub-header: %q", table)
	}
	if strings.Count(table, "agent [openai]") != 1 {
		t.Errorf("a Group header must print once, not once per row: %q", table)
	}
	if !strings.HasSuffix(table, "Summary: 4 ok, 0 warn, 2 fail (2026-07-19 10:42:07)\n") {
		t.Errorf("summary line must report ok/warn/fail counts and RanAt: %q", table)
	}
}

// TestRun_ProgressReportsPerCheck verifies Options.Progress gets a header
// line before each concurrent phase plus one line per completed check, and
// that a nil Progress (the default) produces no output at all — nothing
// about Run's returned Report should depend on whether Progress is set.
func TestRun_ProgressReportsPerCheck(t *testing.T) {
	up := echoUpstream(t)
	defer up.Close()
	cfgPath := writeConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai: {p1: {base_url: %q, api_key: k1}}
models:
  openai: {vm: {endpoints: [{provider: p1, model: m}]}}
`, up.URL))

	var progress strings.Builder
	rep, err := Run(context.Background(), Options{ConfigPath: cfgPath, TestRouting: true, TestTimeout: 5 * time.Second, Progress: &progress})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := progress.String()
	rule := strings.Repeat("=", 50)
	wantTitle := rule + "\nvmr diagnose\n" + rule + "\n"
	if !strings.HasPrefix(out, wantTitle) {
		t.Errorf("progress must open with a rule-bracketed title: got %q, want prefix %q", out, wantTitle)
	}
	if strings.Contains(out, "Config") {
		t.Errorf("config load is synchronous and already resolved by the time there's anything to narrate — it must not get its own progress line: %q", out)
	}
	// The config Result itself (not narrated, but still part of the
	// Report) must carry the resolved absolute path and come first.
	if rep.Results[0].Phase != "config" {
		t.Fatalf("first result phase = %q, want config", rep.Results[0].Phase)
	}
	absCfgPath, _ := filepath.Abs(cfgPath)
	if rep.Results[0].Target != absCfgPath {
		t.Errorf("config result target = %q, want absolute path %q", rep.Results[0].Target, absCfgPath)
	}
	if !strings.Contains(out, "Environment: checking 1 provider(s)") {
		t.Errorf("missing environment phase header: %q", out)
	}
	if !strings.Contains(out, "  Done  openai/p1\n") {
		t.Errorf("a successful per-check progress line must read Done, not ok: %q", out)
	}
	if !strings.Contains(out, "Connectivity: probing 1 endpoint(s)") {
		t.Errorf("missing connectivity phase header: %q", out)
	}
	if !strings.Contains(out, "  Done  openai/p1/m\n") {
		t.Errorf("missing per-check connectivity progress line: %q", out)
	}

	// Progress is purely observational: a nil Progress must produce the
	// identical Report (same data, just no narration written anywhere).
	repSilent, err := Run(context.Background(), Options{ConfigPath: cfgPath, TestRouting: true, TestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Run (no progress): %v", err)
	}
	if len(repSilent.Results) != len(rep.Results) {
		t.Errorf("result count differs with Progress unset: %d vs %d", len(repSilent.Results), len(rep.Results))
	}
}
