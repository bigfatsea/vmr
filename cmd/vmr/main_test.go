// Ver 2026-07-25, by Sonnet 5
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vmr/internal/config"
	"vmr/internal/router"
)

const minimalConfigYAML = `
listen: 127.0.0.1:0
providers:
  openai:
    p1:
      base_url: https://example.com/v1
      api_key: test-key
models:
  openai:
    m1:
      endpoints:
        - provider: p1
          model: real-model
`

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestCmdCheck_ValidConfig is the pure-function slice of `vmr check`: given a
// config that validates and resolves to at least one route, it must return a
// nil error (the CLI's contract for "config is safe to run").
func TestCmdCheck_ValidConfig(t *testing.T) {
	path := writeTempFile(t, "config.yaml", minimalConfigYAML)
	if err := cmdCheck([]string{"-c", path}); err != nil {
		t.Errorf("cmdCheck on a valid config returned an error: %v", err)
	}
}

// TestCmdCheck_InvalidConfig ensures an unloadable/invalid config surfaces as
// an error rather than a panic or a silent success — this is what stops a
// bad hot-reload or a bad `vmr start` from booting (design doc §10).
func TestCmdCheck_InvalidConfig(t *testing.T) {
	path := writeTempFile(t, "config.yaml", "listen: not-a-valid-address\n")
	if err := cmdCheck([]string{"-c", path}); err == nil {
		t.Error("cmdCheck on an invalid config should return an error")
	}
}

func TestCmdCheck_MissingFile(t *testing.T) {
	if err := cmdCheck([]string{"-c", filepath.Join(t.TempDir(), "does-not-exist.yaml")}); err == nil {
		t.Error("cmdCheck on a missing file should return an error")
	}
}

// TestCmdDirs_ValidConfig locks in that `vmr dirs` prints the config's
// resolved log_dir/image_cache_dir (post-defaults) — these are config
// fields, not environment variables (design doc §7.1).
func TestCmdDirs_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, "config.yaml", minimalConfigYAML+"log_dir: "+dir+"/logs\n")
	got := captureStdout(t, func() {
		if err := cmdDirs([]string{"-c", path, "log"}); err != nil {
			t.Fatalf("cmdDirs log: %v", err)
		}
	})
	if want := dir + "/logs\n"; got != want {
		t.Errorf("cmdDirs log: got %q, want %q", got, want)
	}
}

// TestCmdDirs_RequiresValidConfig locks in that `vmr dirs` loads and
// validates the full config — a missing or malformed config.yaml makes it
// fail, rather than resolving a default independent of config.yaml. vmr.sh
// must never call this unconditionally for commands (stop/status/logs) that
// have to keep working when config.yaml is broken; see resolve_log_dir's
// lazy resolution in vmr.sh.
func TestCmdDirs_RequiresValidConfig(t *testing.T) {
	if err := cmdDirs([]string{"-c", filepath.Join(t.TempDir(), "does-not-exist.yaml"), "log"}); err == nil {
		t.Error("cmdDirs on a missing config should return an error, not resolve a default")
	}
	bad := writeTempFile(t, "config.yaml", "not: valid: yaml: [\n")
	if err := cmdDirs([]string{"-c", bad, "log"}); err == nil {
		t.Error("cmdDirs on an unparseable config should return an error")
	}
}

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	fn()
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestCmdReport_ProducesOutputFiles exercises the CLI wiring around
// report2.Build: glob expansion, output directory creation, and writing both
// the JSON and Markdown artifacts (design doc §9.4), plus the session-
// analysis outputs (vmr-requests.jsonl/.md + details/).
func TestCmdReport_ProducesOutputFiles(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "vmr-audit-2026-07-08.jsonl")
	line := `{"ts":"2026-07-08T10:00:00Z","dur_ms":5,"model":"m1","protocol":"openai","outcome":"ok","client":{"request":{}}}` + "\n"
	if err := os.WriteFile(auditPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")

	if err := cmdReport([]string{"-o", outDir, auditPath}); err != nil {
		t.Fatalf("cmdReport: %v", err)
	}
	for _, name := range []string{"vmr-report.json", "vmr-report.md", "vmr-requests.jsonl"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Errorf("expected %s to be written: %v", name, err)
		}
	}
	if fi, err := os.Stat(filepath.Join(outDir, "details")); err != nil || !fi.IsDir() {
		t.Errorf("expected details/ directory to be written: %v", err)
	}
}

// TestCmdReport_NoMatches ensures a glob matching nothing is a clear error,
// not an empty-but-successful report.
func TestCmdReport_NoMatches(t *testing.T) {
	dir := t.TempDir()
	if err := cmdReport([]string{filepath.Join(dir, "no-such-*.jsonl")}); err == nil {
		t.Error("cmdReport with a non-matching glob should return an error")
	}
}

func TestCmdReport_NoInputFiles(t *testing.T) {
	if err := cmdReport(nil); err == nil {
		t.Error("cmdReport with no input files should return an error")
	}
}

// --- logConfigSummary tests ---

// TestLogConfigSummary_Output verifies that logConfigSummary emits the
// key config fields a operator would scan at startup: listen address, auth
// state, limits, timeouts, directories, per-model endpoints in try order,
// and proxy resolution per provider.
func TestLogConfigSummary_Output(t *testing.T) {
	yaml := `
listen: 127.0.0.1:8800
api_keys: ["sk-vmr-local-test-key-001"]
max_attempts: 3
max_concurrency: 8
image_downscale: 512
audit_retention_days: 30
probe_mode: active
providers:
  openai:
    p1: {base_url: https://a.example/v1, api_key: key-aaaa}
    p2: {base_url: https://b.example/v1, api_key: key-bbbb, proxy: false}
models:
  openai:
    vm:
      endpoints:
        - {provider: p1, model: real-a, priority: 1}
        - {provider: p2, model: real-b, priority: 2}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	logConfigSummary(logger, cfg, snap)
	out := buf.String()

	// Core config fields.
	checks := []string{
		"listen            = 127.0.0.1:8800",
		"auth              = on",
		"max_attempts      = 3",
		"max_concurrency   = 8",
		"image_downscale   = 512px",
		"audit_retention   = 30d",
		"probe_mode        = active",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("logConfigSummary output missing %q in:\n%s", want, out)
		}
	}

	// Timeouts block.
	if !strings.Contains(out, "timeouts") || !strings.Contains(out, "connect           = 10s") {
		t.Errorf("output missing timeouts block:\n%s", out)
	}

	// Model group with endpoints in try order.
	if !strings.Contains(out, "openai/vm") {
		t.Errorf("output missing model group:\n%s", out)
	}
	if !strings.Contains(out, "1.p1/real-a, max_context_tokens=<empty>, capabilities=<empty>") {
		t.Errorf("output missing first endpoint:\n%s", out)
	}
	if !strings.Contains(out, "2.p2/real-b, max_context_tokens=<empty>, capabilities=<empty>") {
		t.Errorf("output missing second endpoint:\n%s", out)
	}

	// Provider lines (in the "provider config:" group, no "provider" prefix per line).
	if !strings.Contains(out, "openai/p1 base_url=https://a.example/v1") {
		t.Errorf("output missing p1 base_url line:\n%s", out)
	}
	if !strings.Contains(out, "openai/p2 base_url=https://b.example/v1") {
		t.Errorf("output missing p2 base_url line:\n%s", out)
	}
	if strings.Contains(out, "openai/p1 base_url=https://a.example/v1 (proxy)") {
		t.Errorf("p1 has no proxy configured, must not show (proxy) marker:\n%s", out)
	}
}

// TestLogConfigSummary_ProxyMarker verifies the "provider config:" block
// shows base_url= always and appends the "(proxy)" marker only for a
// provider whose traffic actually resolves to a configured proxy — not for
// one that's direct because no proxy was ever configured at all.
func TestLogConfigSummary_ProxyMarker(t *testing.T) {
	yaml := `
listen: 127.0.0.1:8800
proxy: true
https_proxy: http://127.0.0.1:7890
providers:
  openai:
    proxied: {base_url: https://a.example/v1, api_key: k}
    direct: {base_url: https://b.example/v1, api_key: k, proxy: false}
models:
  openai:
    vm:
      endpoints:
        - {provider: proxied, model: m1}
        - {provider: direct, model: m2}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	logConfigSummary(logger, cfg, snap)
	out := buf.String()

	if !strings.Contains(out, "openai/proxied base_url=https://a.example/v1 (proxy)") {
		t.Errorf("proxied provider missing (proxy) marker: %s", out)
	}
	if !strings.Contains(out, "openai/direct  base_url=https://b.example/v1\n") {
		t.Errorf("direct provider must not show (proxy) marker: %s", out)
	}
}

// TestLogConfigSummary_AuthOffAndDefaults verifies the default-value
// rendering: auth=off when no api_keys, max_attempts=unlimited, etc.
func TestLogConfigSummary_AuthOffAndDefaults(t *testing.T) {
	yaml := `
listen: 127.0.0.1:8800
providers:
  openai:
    p1: {base_url: https://a.example/v1, api_key: k}
models:
  openai:
    vm: {endpoints: [{provider: p1, model: m}]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	logConfigSummary(logger, cfg, snap)
	out := buf.String()

	if !strings.Contains(out, "auth              = off") {
		t.Errorf("auth should be off: %s", out)
	}
	if !strings.Contains(out, "max_attempts      = unlimited") {
		t.Errorf("max_attempts should be unlimited: %s", out)
	}
	if !strings.Contains(out, "max_concurrency   = unlimited") {
		t.Errorf("max_concurrency should be unlimited: %s", out)
	}
	if !strings.Contains(out, "image_downscale   = off") {
		t.Errorf("image_downscale should be off: %s", out)
	}
	if !strings.Contains(out, "audit_retention   = forever") {
		t.Errorf("audit_retention should be forever: %s", out)
	}
}

// TestLogConfigSummary_ImageDownscaleOverride verifies per-model
// image_downscale override is rendered.
func TestLogConfigSummary_ImageDownscaleOverride(t *testing.T) {
	yaml := `
listen: 127.0.0.1:8800
image_downscale: 1024
providers:
  openai:
    p1: {base_url: https://a.example/v1, api_key: k}
models:
  openai:
    plain: {endpoints: [{provider: p1, model: m}]}
    custom: {image_downscale: 256, endpoints: [{provider: p1, model: m}]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	logConfigSummary(logger, cfg, snap)
	out := buf.String()

	if !strings.Contains(out, "openai/plain\n") {
		t.Errorf("plain model should not have image_downscale override: %s", out)
	}
	if !strings.Contains(out, "openai/custom (image_downscale=256px)") {
		t.Errorf("custom model should show image_downscale=256px: %s", out)
	}
}

// TestLogConfigSummary_MaxContextTokensAndCapabilities verifies the
// "model config:" block's per-endpoint line renders declared
// max_context_tokens (round-thousands as "Nk") and capabilities (declared
// list joined with "/"), and falls back to "<empty>" for an endpoint that
// declares neither — the unconstrained default (core.Endpoint.HasCapability),
// not an error state.
func TestLogConfigSummary_MaxContextTokensAndCapabilities(t *testing.T) {
	yaml := `
listen: 127.0.0.1:8800
providers:
  openai:
    p1: {base_url: https://a.example/v1, api_key: k}
models:
  openai:
    vm:
      endpoints:
        - {provider: p1, model: declared, max_context_tokens: 128000, capabilities: [text, image, tools]}
        - {provider: p1, model: bare}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	logConfigSummary(logger, cfg, snap)
	out := buf.String()

	if !strings.Contains(out, "p1/declared, max_context_tokens=128k, capabilities=text/image/tools") {
		t.Errorf("declared endpoint not rendered correctly: %s", out)
	}
	if !strings.Contains(out, "p1/bare, max_context_tokens=<empty>, capabilities=<empty>") {
		t.Errorf("bare (unconstrained) endpoint not rendered correctly: %s", out)
	}
}

// --- providerProxyLines tests ---

func TestProviderProxyLines_Direct(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  openai:
    p1: {base_url: https://a.example/v1, api_key: k}
models:
  openai:
    vm: {endpoints: [{provider: p1, model: m}]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	lines := providerProxyLines(cfg)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "proxy=direct") {
		t.Errorf("expected direct, got %q", lines[0])
	}
}

func TestProviderProxyLines_ProxyFalse(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
https_proxy: http://127.0.0.1:7890
providers:
  openai:
    p1: {base_url: https://a.example/v1, api_key: k, proxy: false}
models:
  openai:
    vm: {endpoints: [{provider: p1, model: m}]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	lines := providerProxyLines(cfg)
	if !strings.Contains(lines[0], "proxy: false") {
		t.Errorf("expected proxy: false, got %q", lines[0])
	}
}

func TestProviderProxyLines_ProxyURL(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
proxy: true
https_proxy: http://user:pass@127.0.0.1:7890
providers:
  openai:
    p1: {base_url: https://a.example/v1, api_key: k}
models:
  openai:
    vm: {endpoints: [{provider: p1, model: m}]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	lines := providerProxyLines(cfg)
	// Credentials in the proxy URL must be redacted.
	if strings.Contains(lines[0], "user:pass") {
		t.Errorf("proxy URL credentials not redacted: %q", lines[0])
	}
	if !strings.Contains(lines[0], "127.0.0.1:7890") {
		t.Errorf("proxy URL host missing: %q", lines[0])
	}
}

// --- cmdStatus tests ---

// TestCmdStatus_WithMockServer starts a mock admin endpoint, points a
// config at it, and verifies cmdStatus parses the JSON and prints
// human-readable status lines.
func TestCmdStatus_WithMockServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"models": map[string]any{
				"vm [openai]": []map[string]any{
					{
						"endpoint":             "openai/p1/m",
						"protocol":             "openai",
						"priority":             1,
						"consecutive_failures": 0,
						"available":            true,
					},
				},
			},
			"concurrency": map[string]any{
				"limit":     8,
				"in_flight": 2,
				"waiting":   0,
			},
			"time": "2026-07-19T12:00:00Z",
		})
	}))
	defer ts.Close()

	yaml := fmt.Sprintf(`
listen: %s
providers:
  openai:
    p1: {base_url: https://example.com/v1, api_key: k}
models:
  openai:
    vm: {endpoints: [{provider: p1, model: m}]}
`, ts.Listener.Addr().String())

	path := writeTempFile(t, "config.yaml", yaml)
	got := captureStdout(t, func() {
		if err := cmdStatus([]string{"-c", path}); err != nil {
			t.Fatalf("cmdStatus: %v", err)
		}
	})

	if !strings.Contains(got, "concurrency: 2/8") {
		t.Errorf("output should show concurrency 2/8: %q", got)
	}
	if !strings.Contains(got, "vm [openai]") {
		t.Errorf("output should show model name: %q", got)
	}
	if !strings.Contains(got, "openai/p1/m") {
		t.Errorf("output should show endpoint: %q", got)
	}
	if !strings.Contains(got, "ok") {
		t.Errorf("endpoint state should be ok: %q", got)
	}
}

// TestCmdStatus_ServerNotRunning verifies that cmdStatus returns a clear
// error when no vmr instance is listening.
func TestCmdStatus_ServerNotRunning(t *testing.T) {
	yaml := `
listen: 127.0.0.1:1
providers:
  openai:
    p1: {base_url: https://example.com/v1, api_key: k}
models:
  openai:
    vm: {endpoints: [{provider: p1, model: m}]}
`
	path := writeTempFile(t, "config.yaml", yaml)
	if err := cmdStatus([]string{"-c", path}); err == nil {
		t.Error("cmdStatus should return an error when no server is running")
	}
}
