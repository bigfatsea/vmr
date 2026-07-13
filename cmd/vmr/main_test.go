// Ver 2026-07-13 02:00, by Sonnet 5
package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
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
// resolved log_dir/image_cache_dir (post-defaults) rather than an
// env-var-derived path — log_dir and image_cache_dir moved from environment
// variables to config fields (design doc §7.1, 2026-07-13).
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

// TestCmdDirs_RequiresValidConfig documents a real behavior change: unlike
// the pre-2026-07-13 implementation (which resolved log_dir/cache_dir from
// an environment variable, independent of config.yaml), `vmr dirs` now loads
// and validates the full config — a missing or malformed config.yaml makes
// it fail. vmr.sh must never call this unconditionally for commands (stop/
// status/logs) that have to keep working when config.yaml is broken; see
// resolve_log_dir's lazy resolution in vmr.sh.
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
// report.Build: glob expansion, output directory creation, and writing both
// the JSON and Markdown artifacts (design doc §9.4).
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
	for _, name := range []string{"vmr-report.json", "vmr-report.md"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Errorf("expected %s to be written: %v", name, err)
		}
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

func TestCountNested(t *testing.T) {
	m := map[string]map[string]int{
		"a": {"x": 1, "y": 2},
		"b": {"z": 3},
	}
	if got := countNested(m); got != 3 {
		t.Errorf("countNested = %d, want 3", got)
	}
	if got := countNested(map[string]map[string]int{}); got != 0 {
		t.Errorf("countNested(empty) = %d, want 0", got)
	}
}
