// Ver 2026-08-20 18:00, by Sonnet 5

package replay

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vmr/internal/ctxgraph"
)

// writeMinimalConfig writes a config.yaml with log_dir set. config.Load
// requires at least one provider and one model to validate at all (see
// internal/config/config.go's own "providers: at least one required"),
// even though resolveReqAuditPath only ever reads LogDir from the result
// — a config.yaml that fails to load contributes nothing to the search
// (fails open, not an error), so these dummy entries just need to be
// syntactically valid, never actually dialed in these -print-only tests.
func writeMinimalConfig(t *testing.T, dir, logDir string) string {
	t.Helper()
	yaml := `
listen: 127.0.0.1:0
log_dir: ` + logDir + `
providers:
  - {name: p1, base_url: {openai: "http://127.0.0.1:1/v1"}, api_key: unused-key}
models:
  vm:
    endpoints:
      - {protocol: openai, providers: [p1], models: [upstream-model]}
`
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRunPrint_ReqWithoutAuditPath_SearchesCWD covers KNOWN_ISSUES §1.25:
// -req alone (no positional audit file argument) must locate the file by
// its coordinate's basename in the current working directory.
func TestRunPrint_ReqWithoutAuditPath_SearchesCWD(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeMinimalConfig(t, dir, filepath.Join(dir, "logs-elsewhere"))
	auditPath := writeAuditLine(t, dir, "vmr-audit-2026-08-05.jsonl", chatRecord("vm", "first"), chatRecord("vm", "second"))

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	var out bytes.Buffer
	runErr := Run(context.Background(), Options{
		ConfigPath: cfgPath, Print: true,
		Req: ctxgraph.ReqCoord(auditPath, 2),
	}, &out)
	if runErr != nil {
		t.Fatalf("Run -print -req (no audit path): %v", runErr)
	}
	if !strings.Contains(out.String(), "second") || strings.Contains(out.String(), "first") {
		t.Errorf("output = %q, want only line 2's record", out.String())
	}
}

// TestRunPrint_ReqWithDirectoryHint covers passing a directory instead of
// a bare file path — the positional argument becomes a "search here"
// hint rather than the exact file.
func TestRunPrint_ReqWithDirectoryHint(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "somewhere")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeMinimalConfig(t, dir, filepath.Join(dir, "unrelated-log-dir"))
	auditPath := writeAuditLine(t, logsDir, "vmr-audit-2026-08-05.jsonl", chatRecord("vm", "only"))

	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, Print: true, AuditPath: logsDir,
		Req: ctxgraph.ReqCoord(auditPath, 1),
	}, &out); err != nil {
		t.Fatalf("Run -print -req <dir>: %v", err)
	}
	if !strings.Contains(out.String(), "only") {
		t.Errorf("output = %q, want line 1's record", out.String())
	}
}

// TestRunPrint_ReqFallsBackToLogDir covers the log_dir fallback: when the
// file isn't in the current directory, config.yaml's log_dir is searched
// next.
func TestRunPrint_ReqFallsBackToLogDir(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeMinimalConfig(t, dir, logsDir)
	auditPath := writeAuditLine(t, logsDir, "vmr-audit-2026-08-05.jsonl", chatRecord("vm", "in-log-dir"))

	emptyCWD := t.TempDir() // guaranteed not to contain the audit file
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(emptyCWD); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, Print: true,
		Req: ctxgraph.ReqCoord(auditPath, 1),
	}, &out); err != nil {
		t.Fatalf("Run -print -req (log_dir fallback): %v", err)
	}
	if !strings.Contains(out.String(), "in-log-dir") {
		t.Errorf("output = %q, want the log_dir record", out.String())
	}
}

// TestRunPrint_ReqNotFoundAnywhere covers the honest failure case: no
// audit path, no config.yaml reachable, nothing found — a clear error,
// not a silent empty result.
func TestRunPrint_ReqNotFoundAnywhere(t *testing.T) {
	emptyCWD := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(emptyCWD); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	err = Run(context.Background(), Options{
		ConfigPath: filepath.Join(emptyCWD, "no-such-config.yaml"), Print: true,
		Req: "vmr-audit-2026-08-05.jsonl:1",
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Run -print -req with nothing findable: want an error, got nil")
	}
}

// TestSelectRecord_TSStillRequiresAuditPath proves -req's new fallback
// doesn't leak into -ts/-line, which have no basename to search with.
func TestSelectRecord_TSStillRequiresAuditPath(t *testing.T) {
	_, _, _, err := selectRecord(Options{TS: "2026-08-05T00:00:00Z"})
	if err == nil {
		t.Fatal("selectRecord with -ts and no AuditPath: want an error, got nil")
	}
}
