// Ver 2026-07-29 14:15, by Sonnet 5

package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func parseArgs(t *testing.T, args []string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("fs.Parse: %v", err)
	}
	return fs
}

// TestResolveInputPaths_ExplicitArgsUnchanged locks in that giving explicit
// file/glob arguments behaves exactly as before this function existed:
// glob-expanded, de-duplicated, sorted — configPath is never even touched.
func TestResolveInputPaths_ExplicitArgsUnchanged(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.jsonl")
	b := filepath.Join(dir, "b.jsonl")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fs := parseArgs(t, []string{a, b, a}) // "a" repeated: must de-dup
	paths, err := resolveInputPaths(fs, filepath.Join(dir, "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("resolveInputPaths: %v", err)
	}
	if len(paths) != 2 || paths[0] != a || paths[1] != b {
		t.Fatalf("got %v, want [%s %s]", paths, a, b)
	}
}

// TestResolveInputPaths_DefaultsToConfigLogDir covers the new fallback: no
// positional arguments given falls back to <config's log_dir>/vmr-audit-*,
// matching both plain .jsonl and compressed .jsonl.zst audit files.
func TestResolveInputPaths_DefaultsToConfigLogDir(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(logDir, "vmr-audit-2026-07-29.jsonl")
	compressed := filepath.Join(logDir, "vmr-audit-2026-07-20.jsonl.zst")
	unrelated := filepath.Join(logDir, "other.txt")
	for _, p := range []string{live, compressed, unrelated} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configPath := writeTempFile(t, "config.yaml", minimalConfigYAML+"log_dir: "+logDir+"\n")

	fs := parseArgs(t, nil)
	paths, err := resolveInputPaths(fs, configPath)
	if err != nil {
		t.Fatalf("resolveInputPaths: %v", err)
	}
	// Sorted lexically, so the "-20" (.zst) file sorts before "-29" (.jsonl).
	if len(paths) != 2 || paths[0] != compressed || paths[1] != live {
		t.Fatalf("got %v, want [%s %s] (unrelated.txt must not match)", paths, compressed, live)
	}
}

// TestResolveInputPaths_NoArgsAndNoConfig covers the error path: no
// positional files AND no loadable config is a clear error, not a panic or
// a silent empty result.
func TestResolveInputPaths_NoArgsAndNoConfig(t *testing.T) {
	fs := parseArgs(t, nil)
	if _, err := resolveInputPaths(fs, filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Error("resolveInputPaths with no args and no config should return an error")
	}
}

// TestResolveInputPaths_NoArgsAndEmptyLogDir covers the case where the
// config loads fine but its log_dir has no vmr-audit-* files yet (a fresh
// instance that hasn't logged anything) — still a clear error, not an
// empty-but-successful report/story.
func TestResolveInputPaths_NoArgsAndEmptyLogDir(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := writeTempFile(t, "config.yaml", minimalConfigYAML+"log_dir: "+logDir+"\n")

	fs := parseArgs(t, nil)
	if _, err := resolveInputPaths(fs, configPath); err == nil {
		t.Error("resolveInputPaths with an empty log_dir should return an error")
	}
}

// TestCmdReport_DefaultsToConfigLogDir exercises the full cmdReport path
// with no positional arguments: it must resolve its input from -c's
// log_dir, exactly like `vmr story`'s equivalent default.
func TestCmdReport_DefaultsToConfigLogDir(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"ts":"2026-07-08T10:00:00Z","dur_ms":5,"model":"m1","protocol":"openai-completions","outcome":"ok","client":{"request":{}}}` + "\n"
	if err := os.WriteFile(filepath.Join(logDir, "vmr-audit-2026-07-08.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeTempFile(t, "config.yaml", minimalConfigYAML+"log_dir: "+logDir+"\n")
	outDir := filepath.Join(dir, "out")

	if err := cmdReport([]string{"-c", configPath, "-o", outDir}); err != nil {
		t.Fatalf("cmdReport with no input files (config default): %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "vmr-report.json")); err != nil {
		t.Errorf("expected vmr-report.json to be written: %v", err)
	}
}
