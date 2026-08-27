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
    base_url: {openai-completions: https://example.com/v1}
    api_key: test-key
models:
  m1:
    endpoints:
      - protocol: openai-completions
        providers: [p1]
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
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: ""}
models:
  m1: {endpoints: [{protocol: openai-completions, providers: [p1], models: [real-model]}]}
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
    base_url: {openai-completions: https://example.com/v1, anthropic-messages: https://example.com/anthropic}
    api_key: test-key
models:
  m1: {endpoints: [{protocol: openai-completions, providers: [p1], models: [real-model]}]}
`)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if strings.Contains(out, "proxy(openai)") || strings.Contains(out, "proxy(anthropic)") {
		t.Errorf("expected one collapsed proxy: line, got per-protocol lines:\n%s", out)
	}
	if !strings.Contains(out, checkLine(2, "proxy", "direct")) {
		t.Errorf("expected a single \"proxy: direct\" line:\n%s", out)
	}
}

// TestCmdCheck_ProxyURLRedacted locks in the production rendering path for
// a provider whose traffic actually resolves through a configured proxy:
// the "proxy:" line must show the (credential-redacted) proxy URL, not just
// a "direct"/"(proxy)" marker — and a provider with no proxy configured at
// all must still render "direct". Also covers the global http_proxy/
// https_proxy fields in "=== Global Settings ===", which carry the same raw
// credential risk and must be redacted the same way (found by this test:
// they weren't, until now). Both this and logConfigSummary (the
// startup/reload log) render through the same printGlobalSettings/
// printProviders, so this is the one place that needs to cover the
// redaction path itself.
func TestCmdCheck_ProxyURLRedacted(t *testing.T) {
	path := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
https_proxy: http://user:pass@127.0.0.1:7890
providers:
  - {name: proxied, base_url: {openai-completions: https://a.example/v1}, api_key: k, proxy: true}
  - {name: direct, base_url: {openai-completions: https://b.example/v1}, api_key: k}
models:
  m1: {endpoints: [{protocol: openai-completions, providers: [proxied], models: [m]}]}
`)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if !strings.Contains(out, checkLine(0, "https_proxy", "http://user:xxxxx@127.0.0.1:7890")) {
		t.Errorf("global https_proxy should show its (credential-redacted) URL:\n%s", out)
	}
	if !strings.Contains(out, checkLine(2, "proxy", "http://user:xxxxx@127.0.0.1:7890")) {
		t.Errorf("proxied provider should show its (credential-redacted) proxy URL:\n%s", out)
	}
	if strings.Contains(out, "user:pass") {
		t.Errorf("proxy URL credentials must be redacted, found raw password in output:\n%s", out)
	}
	if !strings.Contains(out, checkLine(2, "proxy", "direct")) {
		t.Errorf("provider with no proxy configured should show \"direct\":\n%s", out)
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
    base_url: {openai-completions: https://example.com/v1}
    api_key: test-key
models:
  m1:
    capabilities: [text, tools]
    max_context_tokens: 128000
    endpoints:
      - protocol: openai-completions
        providers: [p1]
        models: [with-extra]
        capabilities: [image]
        max_context_tokens: 512000
      - protocol: openai-completions
        providers: [p1]
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

// TestCmdCheck_ModelImageDownscaleOverride locks in that a model declaring
// its own image_downscale renders it in the "=== Models ===" section (a
// per-model override of the global image_downscale setting, config.
// VirtualModel.ImageDownscaleMaxPx), and a model that doesn't override it
// renders no such line at all — this is the production path both `vmr
// check` and logConfigSummary (the startup/reload log) share via
// printModels.
func TestCmdCheck_ModelImageDownscaleOverride(t *testing.T) {
	path := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
image_downscale: 1024
providers:
  - name: p1
    base_url: {openai-completions: https://example.com/v1}
    api_key: test-key
models:
  plain: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m]}]}
  custom: {image_downscale: 256, endpoints: [{protocol: openai-completions, providers: [p1], models: [m]}]}
`)
	out := captureStdout(t, func() { _ = cmdCheck([]string{"-c", path}) })
	if !strings.Contains(out, checkLine(2, "image_downscale", "256px")) {
		t.Errorf("custom model should show its image_downscale override:\n%s", out)
	}
	if got, want := strings.Count(out, checkLine(2, "image_downscale", "256px")), 1; got != want {
		t.Errorf("image_downscale override should render exactly once (for \"custom\" only, not \"plain\"), got %d:\n%s", got, out)
	}
}

// TestCmdCheck_BlankLineBetweenModels locks in the blank-line separator
// between consecutive virtual models in the "=== Models ===" section.
func TestCmdCheck_BlankLineBetweenModels(t *testing.T) {
	path := writeTempFile(t, "config.yaml", `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai-completions: https://example.com/v1}
    api_key: test-key
models:
  a: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m1]}]}
  b: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m2]}]}
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
// writing both the JSON and Markdown artifacts. -details is off by
// default (see TestCmdReport_DetailsOffByDefault for that), so it's passed
// explicitly here to also cover the session-analysis-driven details/
// output in the same pass.
func TestCmdReport_ProducesOutputFiles(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "vmr-audit-2026-07-08.jsonl")
	line := `{"ts":"2026-07-08T10:00:00Z","dur_ms":5,"model":"m1","protocol":"openai-completions","outcome":"ok","client":{"request":{}}}` + "\n"
	if err := os.WriteFile(auditPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")

	if err := cmdReport([]string{"-o", outDir, "-details", auditPath}); err != nil {
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

// TestCmdReport_DetailsOffByDefault locks in P3.3's default flip: a plain
// `vmr report` run (no -details, no report.yaml) must not materialize
// details/ at all, while vmr-requests.json still carries a non-empty "req"
// (and, once P4/P5 wire a consumer, a computable detail filename) for
// every row — the index never needs the file to exist to link to it.
func TestCmdReport_DetailsOffByDefault(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "vmr-audit-2026-07-08.jsonl")
	line := `{"ts":"2026-07-08T10:00:00Z","dur_ms":5,"model":"m1","protocol":"openai-completions","outcome":"ok","client":{"request":{}}}` + "\n"
	if err := os.WriteFile(auditPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")

	if err := cmdReport([]string{"-o", outDir, auditPath}); err != nil {
		t.Fatalf("cmdReport: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "details")); err == nil {
		t.Error("details/ should not exist when -details wasn't passed")
	}
	data, err := os.ReadFile(filepath.Join(outDir, "vmr-requests.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"req":`) {
		t.Error("vmr-requests.json rows should still carry a \"req\" coordinate with -details off")
	}
}

// TestCmdReport_ReportYamlDefaultsOutputAndDetails covers report.yaml's
// output/details fields feeding cmdReport's -o/-details when the flags
// themselves aren't passed — the same "-flag > report.yaml > built-in
// default" merge order resolveLanguage already established for -lang.
func TestCmdReport_ReportYamlDefaultsOutputAndDetails(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "vmr-audit-2026-07-08.jsonl")
	line := `{"ts":"2026-07-08T10:00:00Z","dur_ms":5,"model":"m1","protocol":"openai-completions","outcome":"ok","client":{"request":{}}}` + "\n"
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
//
// logConfigSummary now literally calls printGlobalSettings/printProviders/
// printModels — the same functions `vmr check` prints through — so the
// startup/reload log and `vmr check`'s stdout output can never drift apart
// in format again. The detailed rendering rules (column widths, override-
// only endpoint lines, proxy resolution, etc.) are already locked by the
// TestCmdCheck_* tests above against those functions directly; these tests
// only need to confirm logConfigSummary wires logger/cfg/snap/issues into
// them correctly.

// TestLogConfigSummary_MatchesCheckFormat verifies logConfigSummary's output
// is built from the exact same section renderers and section headers as
// `vmr check` — the two must read as one format, not two.
func TestLogConfigSummary_MatchesCheckFormat(t *testing.T) {
	yaml := `
listen: 127.0.0.1:8800
api_keys: ["sk-vmr-local-test-key-001"]
max_attempts: 3
max_concurrency: 8
image_downscale: 512
audit_retention_days: 30
providers:
  - {name: p1, base_url: {openai-completions: https://a.example/v1}, api_key: key-aaaa}
  - {name: p2, base_url: {openai-completions: https://b.example/v1}, api_key: key-bbbb, proxy: false}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [real-a], priority: 1}
      - {protocol: openai-completions, providers: [p2], models: [real-b], priority: 2}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	issues := cfg.Check()

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	logConfigSummary(logger, cfg, snap, issues)
	out := buf.String()

	// Same section headers as `vmr check`.
	for _, header := range []string{"=== Global Settings ===", "=== Providers ===", "=== Models ==="} {
		if !strings.Contains(out, header) {
			t.Errorf("logConfigSummary output missing %q in:\n%s", header, out)
		}
	}

	// Global settings, in checkLine's "key:<pad>value" column format.
	for _, line := range []string{
		checkLine(0, "listen", "127.0.0.1:8800"),
		checkLine(0, "auth", "on (1 key(s))"),
		checkLine(0, "max_attempts", "3"),
		checkLine(0, "max_concurrency", "8"),
		checkLine(0, "image_downscale", "512px"),
		checkLine(0, "audit_retention", "30d"),
	} {
		if !strings.Contains(out, line) {
			t.Errorf("logConfigSummary output missing %q in:\n%s", line, out)
		}
	}

	// Providers section: base_url + proxy lines per provider.
	if !strings.Contains(out, "p1:\n") || !strings.Contains(out, checkLine(2, "base_url(openai-completions)", "https://a.example/v1")) {
		t.Errorf("output missing p1 provider block:\n%s", out)
	}
	if !strings.Contains(out, checkLine(2, "proxy", "direct")) {
		t.Errorf("output missing a direct proxy line:\n%s", out)
	}

	// Models section: endpoints in try order, same "- p=N. provider/model:"
	// label printModels uses.
	if !strings.Contains(out, "vm:\n") || !strings.Contains(out, "openai-completions:\n") {
		t.Errorf("output missing model group:\n%s", out)
	}
	if !strings.Contains(out, "- p=1. p1/real-a:") || !strings.Contains(out, "- p=2. p2/real-b:") {
		t.Errorf("output missing endpoints in try order:\n%s", out)
	}
}

// TestLogConfigSummary_IssuesThreadThrough verifies the caller's cfg.Check()
// result reaches printGlobalSettings/printModels through logConfigSummary,
// so a warning gets the same ⚠️ marker here as it does in `vmr check` —
// this is what the reload log's own "WARN config check: ..." line stays
// consistent with.
func TestLogConfigSummary_IssuesThreadThrough(t *testing.T) {
	yaml := `
listen: 0.0.0.0:8800
providers:
  - {name: p1, base_url: {openai-completions: https://a.example/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m]}]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	issues := cfg.Check()
	if !hasIssue(issues, "", "", "", "listen") {
		t.Fatal("test setup: expected an open-listen config issue")
	}

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	logConfigSummary(logger, cfg, snap, issues)
	out := buf.String()

	if !strings.Contains(out, checkLine(0, "listen", warn("0.0.0.0:8800"))) {
		t.Errorf("logConfigSummary did not mark the listen warning:\n%s", out)
	}

	// Passing no issues at all must drop the marker — confirms the marker
	// tracks the issues argument, not some independent recomputation.
	buf.Reset()
	logConfigSummary(logger, cfg, snap, nil)
	if strings.Contains(buf.String(), "⚠️") {
		t.Errorf("logConfigSummary with nil issues should not show a warning marker:\n%s", buf.String())
	}
}

// TestLogConfigSummary_NameGroupsAcrossProtocols locks in that a virtual
// model name reachable from more than one protocol renders as ONE block
// (name line, both protocols nested under it) — printModels' grouping,
// reused here rather than reimplemented.
func TestLogConfigSummary_NameGroupsAcrossProtocols(t *testing.T) {
	yaml := `
listen: 127.0.0.1:8800
providers:
  - {name: p1, base_url: {openai-completions: https://a.example/v1, anthropic-messages: https://a.example/v1}, api_key: k}
models:
  agent:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [m-openai]}
      - {protocol: anthropic-messages, providers: [p1], models: [m-anthropic]}
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
	logConfigSummary(logger, cfg, snap, cfg.Check())
	out := buf.String()

	// Both protocol faces must appear as children of the SAME "agent:"
	// block — this config only declares one virtual model, so everything
	// after "agent:" belongs to it.
	i := strings.Index(out, "agent:\n")
	if i < 0 {
		t.Fatalf("missing \"agent:\" model group:\n%s", out)
	}
	rest := out[i:]
	if !strings.Contains(rest, "openai-completions:") || !strings.Contains(rest, "anthropic-messages:") {
		t.Errorf("both protocol faces must nest under the same agent: block, got:\n%s", rest)
	}
	if !strings.Contains(rest, "p1/m-openai") || !strings.Contains(rest, "p1/m-anthropic") {
		t.Errorf("both protocol faces' endpoints must be present, got:\n%s", rest)
	}
}

// --- providerProxyEntries tests ---
//
// These exercise providerProxyEntries/redactProxyURL directly (the shared
// logic printProviders' providerProxyLine actually renders through), not a
// separate formatting wrapper — see printProviders in cmd_check.go.

func TestProviderProxyEntries_Direct(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://a.example/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m]}]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	entries := providerProxyEntries(cfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Proxy != "direct" {
		t.Errorf("expected direct, got %q", entries[0].Proxy)
	}
}

func TestProviderProxyEntries_ProxyFalse(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
https_proxy: http://127.0.0.1:7890
providers:
  - {name: p1, base_url: {openai-completions: https://a.example/v1}, api_key: k, proxy: false}
models:
  vm: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m]}]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	entries := providerProxyEntries(cfg)
	if !strings.Contains(entries[0].Proxy, "direct") {
		t.Errorf("expected direct, got %q", entries[0].Proxy)
	}
}

func TestProviderProxyEntries_ProxyURL(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
https_proxy: http://user:pass@127.0.0.1:7890
providers:
  - {name: p1, base_url: {openai-completions: https://a.example/v1}, api_key: k, proxy: true}
models:
  vm: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m]}]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	entries := providerProxyEntries(cfg)
	// Credentials in the proxy URL must be redacted.
	if strings.Contains(entries[0].Proxy, "user:pass") {
		t.Errorf("proxy URL credentials not redacted: %q", entries[0].Proxy)
	}
	if !strings.Contains(entries[0].Proxy, "127.0.0.1:7890") {
		t.Errorf("proxy URL host missing: %q", entries[0].Proxy)
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
			"instance": map[string]any{
				"concurrency": map[string]any{
					"limit":     8,
					"in_flight": 2,
					"waiting":   0,
				},
			},
			"models": []map[string]any{
				{
					"id":       "vm",
					"protocol": "openai-completions",
					"endpoints": []map[string]any{
						{
							"endpoint":             "openai-completions/p1/m",
							"protocol":             "openai-completions",
							"priority":             1,
							"consecutive_failures": 0,
							"available":            true,
							"serving":              true,
						},
					},
				},
			},
			"current_time": "2026-07-19T12:00:00Z",
		})
	}))
	defer ts.Close()

	yaml := fmt.Sprintf(`
listen: %s
providers:
  - {name: p1, base_url: {openai-completions: https://example.com/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m]}]}
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
	if !strings.Contains(got, "vm [openai-completions]") {
		t.Errorf("output should show model name: %q", got)
	}
	if !strings.Contains(got, "openai-completions/p1/m") {
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
			"models": []map[string]any{
				{
					"id":       "vm",
					"protocol": "openai-completions",
					"endpoints": []map[string]any{
						{
							"endpoint":             "openai-completions/p1/m",
							"protocol":             "openai-completions",
							"priority":             1,
							"consecutive_failures": 3,
							"last_error":           "transient",
							"available":            true,
							"serving":              false,
						},
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
  - {name: p1, base_url: {openai-completions: https://example.com/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m]}]}
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

// TestCmdStatus_ServerNotRunning verifies that cmdStatus returns a clear
// error when no vmr instance is listening.
func TestCmdStatus_ServerNotRunning(t *testing.T) {
	yaml := `
listen: 127.0.0.1:1
providers:
  - {name: p1, base_url: {openai-completions: https://example.com/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m]}]}
`
	path := writeTempFile(t, "config.yaml", yaml)
	if err := cmdStatus([]string{"-c", path}); err == nil {
		t.Error("cmdStatus should return an error when no server is running")
	}
}

// TestCmdStatus_WithIssues verifies that cmdStatus renders WARNING lines when
// /status reports config.Check() issues.
func TestCmdStatus_WithIssues(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"instance": map[string]any{
				"config": map[string]any{
					"issues": []map[string]any{
						{
							"field":    "listen",
							"severity": "warning",
							"message":  "listen (0.0.0.0:8800) is not loopback-only and no api_keys are configured",
						},
					},
				},
			},
			"current_time": "2026-07-19T12:00:00Z",
		})
	}))
	defer ts.Close()

	yaml := fmt.Sprintf(`
listen: %s
providers:
  - {name: p1, base_url: {openai-completions: https://example.com/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m]}]}
`, ts.Listener.Addr().String())

	path := writeTempFile(t, "config.yaml", yaml)
	got := captureStdout(t, func() {
		if err := cmdStatus([]string{"-c", path}); err != nil {
			t.Fatalf("cmdStatus: %v", err)
		}
	})

	if !strings.Contains(got, "WARNING: listen (0.0.0.0:8800) is not loopback-only") {
		t.Errorf("output should show check warning: %q", got)
	}
}

// TestCmdStatus_WithAPIKeys verifies that cmdStatus automatically injects the
// configured API key into the Authorization header.
func TestCmdStatus_WithAPIKeys(t *testing.T) {
	const expectedKey = "test-secret-key-12345678"
	var receivedAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		if receivedAuth != "Bearer "+expectedKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"time": "2026-07-19T12:00:00Z",
		})
	}))
	defer ts.Close()

	yaml := fmt.Sprintf(`
listen: %s
api_keys:
  - %s
providers:
  - {name: p1, base_url: {openai-completions: https://example.com/v1}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai-completions, providers: [p1], models: [m]}]}
`, ts.Listener.Addr().String(), expectedKey)

	path := writeTempFile(t, "config.yaml", yaml)
	if err := cmdStatus([]string{"-c", path}); err != nil {
		t.Fatalf("cmdStatus with api_keys: %v", err)
	}
	if receivedAuth != "Bearer "+expectedKey {
		t.Errorf("received auth = %q, want Bearer %s", receivedAuth, expectedKey)
	}

	// Also test -key flag with -addr
	receivedAuth = ""
	if err := cmdStatus([]string{"-addr", ts.Listener.Addr().String(), "-key", expectedKey}); err != nil {
		t.Fatalf("cmdStatus with -addr and -key: %v", err)
	}
	if receivedAuth != "Bearer "+expectedKey {
		t.Errorf("received auth via -key = %q, want Bearer %s", receivedAuth, expectedKey)
	}
}

// dialHost turns a bind address into something you can actually connect
// to. cfg.Listen is routinely a wildcard ("0.0.0.0:8800") and lsof reports
// the same socket as "*:8800" — vmr.sh ps feeds both forms straight into
// `vmr status -addr`.
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
