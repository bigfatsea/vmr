// Ver 2026-08-02, by Sonnet 5
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
  - name: p1
    base_url: {openai: https://example.com/v1}
    api_key: test-key
models:
  m1:
    endpoints:
      - protocol: openai
        provider: p1
        models: [real-model]
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
// bad hot-reload or a bad `vmr start` from booting.
func TestCmdCheck_InvalidConfig(t *testing.T) {
	path := writeTempFile(t, "config.yaml", "listen: not-a-valid-address\n")
	if err := cmdCheck([]string{"-c", path}); err == nil {
		t.Error("cmdCheck on an invalid config should return an error")
	}
}

// TestCmdCheck_ConsistencyIssueFails locks in that cmdCheck returns an
// error whenever config.Check finds something — a structurally valid
// config (Load succeeds) that's still operationally broken (here: a
// missing provider api_key) must gate `vmr.sh start`/`vmr diagnose` just
// like a hard Load failure does, not just print a warning and exit 0.
func TestCmdCheck_ConsistencyIssueFails(t *testing.T) {
	path := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: https://example.com}, api_key: ""}
models:
  m1: {endpoints: [{protocol: openai, provider: p1, models: [real-model]}]}
`)
	var err error
	out := captureStdout(t, func() {
		err = cmdCheck([]string{"-c", path})
	})
	if err == nil {
		t.Error("cmdCheck with a missing provider api_key should return an error")
	}
	if !strings.Contains(out, "=== Failed ===") {
		t.Errorf("cmdCheck output missing Failed summary:\n%s", out)
	}
	if !strings.Contains(out, "api_key missing") {
		t.Errorf("cmdCheck output missing the api_key issue detail:\n%s", out)
	}
}

// TestCmdCheck_CleanConfigPrintsOK locks in the success-path rendering: a
// config with no structural or consistency issues prints "=== OK ===" and
// cmdCheck returns nil.
func TestCmdCheck_CleanConfigPrintsOK(t *testing.T) {
	path := writeTempFile(t, "config.yaml", minimalConfigYAML)
	var err error
	out := captureStdout(t, func() {
		err = cmdCheck([]string{"-c", path})
	})
	if err != nil {
		t.Errorf("cmdCheck on a clean config returned an error: %v", err)
	}
	if !strings.Contains(out, "=== OK ===") {
		t.Errorf("cmdCheck output missing OK summary:\n%s", out)
	}
}

// TestCheckLineAlignsValueColumn locks the fixed-width column every `vmr
// check` section relies on: the value always starts at checkKeyWidth,
// regardless of indent or key length.
func TestCheckLineAlignsValueColumn(t *testing.T) {
	tests := []struct {
		indent   int
		key, val string
	}{
		{0, "listen", "127.0.0.1:8800"},
		{2, "api_key", "sk***1234"},
		{2, "log", "/var/log"},
	}
	for _, tt := range tests {
		line := checkLine(tt.indent, tt.key, tt.val)
		label := strings.Repeat(" ", tt.indent) + tt.key + ":"
		if !strings.HasPrefix(line, label) || !strings.HasSuffix(line, tt.val) {
			t.Fatalf("checkLine(%d, %q, %q) = %q, want prefix %q and suffix %q", tt.indent, tt.key, tt.val, line, label, tt.val)
		}
		if col := len(line) - len(tt.val); col != checkKeyWidth {
			t.Errorf("checkLine(%d, %q, %q): value starts at column %d, want %d", tt.indent, tt.key, tt.val, col, checkKeyWidth)
		}
	}
}

// TestPadLabelOverWidthKeyGetsOneSpace locks the over-width fallback: a
// label already at or past the target width still gets exactly one
// separating space rather than being crammed against its value.
func TestPadLabelOverWidthKeyGetsOneSpace(t *testing.T) {
	longLabel := strings.Repeat("x", checkKeyWidth) + ":"
	if got, want := padLabel(longLabel, checkKeyWidth)+"value", longLabel+" value"; got != want {
		t.Errorf("padLabel with an over-width label = %q, want %q", got, want)
	}
}

// TestCmdCheck_SingleProxyLinePerProvider locks in that a provider
// declaring more than one protocol still renders one "proxy:" line, not a
// "proxy(openai):"/"proxy(anthropic):" pair — Provider.Proxy is one switch
// per provider, not per protocol.
func TestCmdCheck_SingleProxyLinePerProvider(t *testing.T) {
	path := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai: https://example.com/v1, anthropic: https://example.com/anthropic}
    api_key: test-key
models:
  m1: {endpoints: [{protocol: openai, provider: p1, models: [real-model]}]}
`)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if strings.Contains(out, "proxy(openai)") || strings.Contains(out, "proxy(anthropic)") {
		t.Errorf("expected one collapsed proxy: line, got per-protocol lines:\n%s", out)
	}
	if !strings.Contains(out, checkLine(2, "proxy", "direct")) {
		t.Errorf("expected a single \"proxy: direct\" line:\n%s", out)
	}
}

// TestCmdCheck_ModelCapabilitiesBaseAndEndpointExtra locks in the display
// contract for the model-level capabilities/max_context_tokens base: the
// model line shows the base as declared, an endpoint that adds its own
// shows only its own addition/override under "extra_capabilities="/
// "max_context_tokens=" (not the merged effective set — that's what
// core.Endpoint.Capabilities is for), and an endpoint declaring neither
// shows a bare "- p=N. provider/model:" with nothing after the colon.
func TestCmdCheck_ModelCapabilitiesBaseAndEndpointExtra(t *testing.T) {
	path := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai: https://example.com/v1}
    api_key: test-key
models:
  m1:
    capabilities: [text, tools]
    max_context_tokens: 128000
    endpoints:
      - protocol: openai
        provider: p1
        models: [with-extra]
        capabilities: [image]
        max_context_tokens: 512000
      - protocol: openai
        provider: p1
        models: [plain]
`)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if !strings.Contains(out, checkLine(2, "capabilities", "text,tools")) {
		t.Errorf("model base capabilities not rendered:\n%s", out)
	}
	if !strings.Contains(out, checkLine(2, "max_context_tokens", "128000")) {
		t.Errorf("model base max_context_tokens not rendered:\n%s", out)
	}
	wantEndpointLine := padLabel("    - p=0. p1/with-extra:", endpointKeyWidth) + "extra_capabilities=image; max_context_tokens=512000"
	if !strings.Contains(out, wantEndpointLine) {
		t.Errorf("endpoint's own extra/override not rendered at column %d:\ngot:  %s\nwant: %q", endpointKeyWidth, out, wantEndpointLine)
	}
	if !strings.Contains(out, "- p=0. p1/plain:\n") {
		t.Errorf("endpoint declaring neither should render a bare label with nothing after the colon:\n%s", out)
	}
}

// TestCmdCheck_BlankLineBetweenModels locks in the blank-line separator
// between consecutive virtual models in the "=== Models ===" section.
func TestCmdCheck_BlankLineBetweenModels(t *testing.T) {
	path := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai: https://example.com/v1}
    api_key: test-key
models:
  a: {endpoints: [{protocol: openai, provider: p1, models: [m1]}]}
  b: {endpoints: [{protocol: openai, provider: p1, models: [m2]}]}
`)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if !strings.Contains(out, "- p=0. p1/m1:\n\nb:\n") {
		t.Errorf("expected exactly one blank line between model \"a\" and model \"b\":\n%s", out)
	}
}

func TestCmdCheck_MissingFile(t *testing.T) {
	if err := cmdCheck([]string{"-c", filepath.Join(t.TempDir(), "does-not-exist.yaml")}); err == nil {
		t.Error("cmdCheck on a missing file should return an error")
	}
}

// TestCmdCheck_DirValidConfig locks in that `vmr check log|cache` prints
// just the config's resolved log_dir/image_cache_dir (post-defaults) — these
// are config fields, not environment variables. Absorbed from the former
// standalone `vmr dirs` subcommand.
func TestCmdCheck_DirValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, "config.yaml", minimalConfigYAML+"log_dir: "+dir+"/logs\n")
	got := captureStdout(t, func() {
		if err := cmdCheck([]string{"-c", path, "log"}); err != nil {
			t.Fatalf("cmdCheck log: %v", err)
		}
	})
	if want := dir + "/logs\n"; got != want {
		t.Errorf("cmdCheck log: got %q, want %q", got, want)
	}
}

// TestCmdCheck_DirRequiresValidConfig locks in that `vmr check log|cache`
// loads and validates the full config — a missing or malformed config.yaml
// makes it fail, rather than resolving a default independent of
// config.yaml. vmr.sh must never call this unconditionally for commands
// (stop/status/logs) that have to keep working when config.yaml is broken;
// see resolve_log_dir's lazy resolution in vmr.sh.
func TestCmdCheck_DirRequiresValidConfig(t *testing.T) {
	if err := cmdCheck([]string{"-c", filepath.Join(t.TempDir(), "does-not-exist.yaml"), "log"}); err == nil {
		t.Error("cmdCheck log on a missing config should return an error, not resolve a default")
	}
	bad := writeTempFile(t, "config.yaml", "not: valid: yaml: [\n")
	if err := cmdCheck([]string{"-c", bad, "log"}); err == nil {
		t.Error("cmdCheck log on an unparseable config should return an error")
	}
}

// TestCmdCheck_DirUnknownArg locks in the usage-error path for an unknown
// trailing argument (anything but log/cache).
func TestCmdCheck_DirUnknownArg(t *testing.T) {
	path := writeTempFile(t, "config.yaml", minimalConfigYAML)
	if err := cmdCheck([]string{"-c", path, "bogus"}); err == nil {
		t.Error("cmdCheck with an unknown trailing arg should return an error")
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
// report.BuildCached: glob expansion, output directory creation, and
// writing both the JSON and Markdown artifacts, plus the session-analysis
// outputs (vmr-requests.json/.md + details/).
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
	for _, name := range []string{"vmr-report.json", "vmr-report.md", "vmr-requests.json", "vmr-requests-failed.jsonl", "vmr-requests-failed.md"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Errorf("expected %s to be written: %v", name, err)
		}
	}
	if fi, err := os.Stat(filepath.Join(outDir, "details")); err != nil || !fi.IsDir() {
		t.Errorf("expected details/ directory to be written: %v", err)
	}
}

// TestCmdReport_ReportYamlDefaultsOutputAndDetails covers report.yaml's
// output/details fields feeding cmdReport's -o/-details when the flags
// themselves aren't passed — the same "-flag > report.yaml > built-in
// default" merge order resolveLanguage already established for -lang.
func TestCmdReport_ReportYamlDefaultsOutputAndDetails(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "vmr-audit-2026-07-08.jsonl")
	line := `{"ts":"2026-07-08T10:00:00Z","dur_ms":5,"model":"m1","protocol":"openai","outcome":"ok","client":{"request":{}}}` + "\n"
	if err := os.WriteFile(auditPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.yaml"), []byte("output: myout\ndetails: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := cmdReport([]string{auditPath}); err != nil {
		t.Fatalf("cmdReport: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "myout", "vmr-report.json")); err != nil {
		t.Errorf("expected report.yaml's output dir 'myout' to be used: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "myout", "details")); err == nil {
		t.Error("report.yaml's details: false should have suppressed details/ export")
	}

	// An explicit -details=true must still win over report.yaml's false.
	if err := cmdReport([]string{"-o", filepath.Join(dir, "myout2"), "-details=true", auditPath}); err != nil {
		t.Fatalf("cmdReport: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "myout2", "details")); err != nil || !fi.IsDir() {
		t.Errorf("explicit -details=true should override report.yaml's details: false: %v", err)
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
providers:
  - {name: p1, base_url: {openai: https://a.example/v1}, api_key: key-aaaa}
  - {name: p2, base_url: {openai: https://b.example/v1}, api_key: key-bbbb, proxy: false}
models:
  vm:
    endpoints:
      - {protocol: openai, provider: p1, models: [real-a], priority: 1}
      - {protocol: openai, provider: p2, models: [real-b], priority: 2}
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

	// Model group with endpoints in try order — grouped by name first,
	// protocol nested inside (see logConfigSummary's doc comment).
	if !strings.Contains(out, "vm:") || !strings.Contains(out, "openai:") {
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
https_proxy: http://127.0.0.1:7890
providers:
  - {name: proxied, base_url: {openai: https://a.example/v1}, api_key: k, proxy: true}
  - {name: direct, base_url: {openai: https://b.example/v1}, api_key: k}
models:
  vm:
    endpoints:
      - {protocol: openai, provider: proxied, models: [m1]}
      - {protocol: openai, provider: direct, models: [m2]}
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
  - {name: p1, base_url: {openai: https://a.example/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai, provider: p1, models: [m]}]}
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
  - {name: p1, base_url: {openai: https://a.example/v1}, api_key: k}
models:
  plain: {endpoints: [{protocol: openai, provider: p1, models: [m]}]}
  custom: {image_downscale: 256, endpoints: [{protocol: openai, provider: p1, models: [m]}]}
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

	if !strings.Contains(out, "plain:\n") {
		t.Errorf("plain model should not have image_downscale override: %s", out)
	}
	if !strings.Contains(out, "custom: (image_downscale=256px)") {
		t.Errorf("custom model should show image_downscale=256px: %s", out)
	}
}

// TestLogConfigSummary_NameGroupsAcrossProtocols locks in a startup-log
// readability fix: a virtual model name reachable from more than one
// protocol must render as ONE block (name line, both protocols nested under
// it), not as two separate, non-adjacent top-level "protocol/name" lines the
// old protocol-outer grouping produced — matching cmd_check.go's
// printModels order.
func TestLogConfigSummary_NameGroupsAcrossProtocols(t *testing.T) {
	yaml := `
listen: 127.0.0.1:8800
providers:
  - {name: p1, base_url: {openai: https://a.example/v1, anthropic: https://a.example/v1}, api_key: k}
models:
  agent:
    endpoints:
      - {protocol: openai, provider: p1, models: [m-openai]}
      - {protocol: anthropic, provider: p1, models: [m-anthropic]}
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

	// Both protocol faces must appear as children of the SAME "agent:"
	// block — this config only declares one virtual model, so everything
	// after "agent:" belongs to it.
	i := strings.Index(out, "\n    agent:")
	if i < 0 {
		t.Fatalf("missing \"agent:\" model group:\n%s", out)
	}
	rest := out[i:]
	if !strings.Contains(rest, "openai:") || !strings.Contains(rest, "anthropic:") {
		t.Errorf("both protocol faces must nest under the same agent: block, got:\n%s", rest)
	}
	if !strings.Contains(rest, "p1/m-openai") || !strings.Contains(rest, "p1/m-anthropic") {
		t.Errorf("both protocol faces' endpoints must be present, got:\n%s", rest)
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
  - {name: p1, base_url: {openai: https://a.example/v1}, api_key: k}
models:
  vm:
    endpoints:
      - {protocol: openai, provider: p1, models: [declared], max_context_tokens: 128000, capabilities: [text, image, tools]}
      - {protocol: openai, provider: p1, models: [bare]}
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
  - {name: p1, base_url: {openai: https://a.example/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai, provider: p1, models: [m]}]}
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
  - {name: p1, base_url: {openai: https://a.example/v1}, api_key: k, proxy: false}
models:
  vm: {endpoints: [{protocol: openai, provider: p1, models: [m]}]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	lines := providerProxyLines(cfg)
	if !strings.Contains(lines[0], "direct") {
		t.Errorf("expected direct, got %q", lines[0])
	}
}

func TestProviderProxyLines_ProxyURL(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
https_proxy: http://user:pass@127.0.0.1:7890
providers:
  - {name: p1, base_url: {openai: https://a.example/v1}, api_key: k, proxy: true}
models:
  vm: {endpoints: [{protocol: openai, provider: p1, models: [m]}]}
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
						"serving":              true,
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
  - {name: p1, base_url: {openai: https://example.com/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai, provider: p1, models: [m]}]}
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

// TestCmdStatus_HalfOpenRendersDistinctFromOK pins the fix for a finding
// from the 2026-08-12 review (VMR_项目全面Review报告 B2): a half-open
// endpoint (available=true, serving=false, fails>0) must render as
// "half-open", not "ok" — cmd_status.go used to key this off fails>0 alone,
// which happened to give the same answer, but now explicitly checks the
// serving field so it can't silently regress if available's semantics ever
// change again.
func TestCmdStatus_HalfOpenRendersDistinctFromOK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"models": map[string]any{
				"vm [openai]": []map[string]any{
					{
						"endpoint":             "openai/p1/m",
						"protocol":             "openai",
						"priority":             1,
						"consecutive_failures": 3,
						"last_error":           "transient",
						"available":            true,
						"serving":              false,
					},
				},
			},
			"concurrency": map[string]any{"limit": 8, "in_flight": 0, "waiting": 0},
			"time":        "2026-07-19T12:00:00Z",
		})
	}))
	defer ts.Close()

	yaml := fmt.Sprintf(`
listen: %s
providers:
  - {name: p1, base_url: {openai: https://example.com/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai, provider: p1, models: [m]}]}
`, ts.Listener.Addr().String())

	path := writeTempFile(t, "config.yaml", yaml)
	got := captureStdout(t, func() {
		if err := cmdStatus([]string{"-c", path}); err != nil {
			t.Fatalf("cmdStatus: %v", err)
		}
	})
	if !strings.Contains(got, "half-open") {
		t.Errorf("available=true, serving=false should render as half-open: %q", got)
	}
	if strings.Contains(got, "COOLDOWN") {
		t.Errorf("available=true should never render as COOLDOWN: %q", got)
	}
}

// TestCmdStatus_OlderServerMissingServingField_HealthyEndpointStillOK covers
// a version-skew gap an independent review found: a `vmr status` binary
// built after the Serving field was added can still query an older,
// already-running `vmr start` process (e.g. right after a Homebrew tap
// upgrade, before the service restarts) whose /admin/status response omits
// "serving" entirely. A healthy endpoint (available=true, fails=0, no
// "serving" key at all) must still render "ok", not misread the absent
// field as serving=false and render a false "half-open" alarm.
func TestCmdStatus_OlderServerMissingServingField_HealthyEndpointStillOK(t *testing.T) {
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
						// "serving" deliberately absent — simulates an older server.
					},
				},
			},
			"concurrency": map[string]any{"limit": 8, "in_flight": 0, "waiting": 0},
			"time":        "2026-07-19T12:00:00Z",
		})
	}))
	defer ts.Close()

	yaml := fmt.Sprintf(`
listen: %s
providers:
  - {name: p1, base_url: {openai: https://example.com/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai, provider: p1, models: [m]}]}
`, ts.Listener.Addr().String())

	path := writeTempFile(t, "config.yaml", yaml)
	got := captureStdout(t, func() {
		if err := cmdStatus([]string{"-c", path}); err != nil {
			t.Fatalf("cmdStatus: %v", err)
		}
	})
	if strings.Contains(got, "half-open") || strings.Contains(got, "COOLDOWN") {
		t.Errorf("a healthy endpoint with no \"serving\" key (older server) should render ok, not half-open/COOLDOWN: %q", got)
	}
}

// TestCmdStatus_ServerNotRunning verifies that cmdStatus returns a clear
// error when no vmr instance is listening.
func TestCmdStatus_ServerNotRunning(t *testing.T) {
	yaml := `
listen: 127.0.0.1:1
providers:
  - {name: p1, base_url: {openai: https://example.com/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai, provider: p1, models: [m]}]}
`
	path := writeTempFile(t, "config.yaml", yaml)
	if err := cmdStatus([]string{"-c", path}); err == nil {
		t.Error("cmdStatus should return an error when no server is running")
	}
}

// dialHost turns a bind address into something you can actually connect
// to. cfg.Listen is routinely a wildcard ("0.0.0.0:8800") and lsof reports
// the same socket as "*:8800" — vmr.sh ps feeds both forms straight into
// `vmr status -addr`, and /admin/status is loopback-only regardless.
func TestDialHostRewritesWildcardBinds(t *testing.T) {
	cases := map[string]string{
		"0.0.0.0:8800":   "127.0.0.1:8800",
		"*:8800":         "127.0.0.1:8800",
		":8800":          "127.0.0.1:8800",
		"[::]:8800":      "127.0.0.1:8800",
		"127.0.0.1:8901": "127.0.0.1:8901",
		"localhost:8800": "localhost:8800",
		"garbage":        "garbage", // not host:port — pass through so the dial reports the real reason
	}
	for in, want := range cases {
		if got := dialHost(in); got != want {
			t.Errorf("dialHost(%q) = %q, want %q", in, got, want)
		}
	}
}
