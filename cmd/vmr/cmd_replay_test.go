// Ver 2026-07-30, by Sonnet 5

// CLI-wiring tests for `vmr replay`. The underlying mechanics (request
// rebuilding) are covered in internal/replay's own test suite; these tests
// only lock in flag parsing and the command's exit-error contract, using
// local httptest servers so nothing here touches the network.
package main

import (
	"strings"
	"testing"
)

func replayAuditFile(t *testing.T) string {
	t.Helper()
	line := `{"model":"m1","protocol":"openai","stream":false,"client":{"request":{"headers":{},"body":{"model":"m1","messages":[{"role":"user","content":"hi"}]}}}}` + "\n"
	return writeTempFile(t, "audit.jsonl", line)
}

func TestCmdReplay_RequiresProvider(t *testing.T) {
	path := diagnoseConfigYAML(t, "http://127.0.0.1:1/unreachable")
	auditPath := replayAuditFile(t)
	if err := cmdReplay([]string{"-c", path, auditPath}); err == nil {
		t.Error("cmdReplay without -provider should return an error")
	}
}

func TestCmdReplay_WrongArgCount(t *testing.T) {
	path := diagnoseConfigYAML(t, "http://127.0.0.1:1/unreachable")
	if err := cmdReplay([]string{"-c", path, "-provider", "p1"}); err == nil {
		t.Error("cmdReplay with no audit file argument should return an error")
	}
}

func TestCmdReplay_BadStreamFlag(t *testing.T) {
	path := diagnoseConfigYAML(t, "http://127.0.0.1:1/unreachable")
	auditPath := replayAuditFile(t)
	if err := cmdReplay([]string{"-c", path, "-provider", "p1", "-stream", "not-a-bool", auditPath}); err == nil {
		t.Error("cmdReplay with an invalid -stream value should return an error")
	}
}

func TestCmdReplay_DryRun(t *testing.T) {
	path := diagnoseConfigYAML(t, "http://127.0.0.1:1/unreachable")
	auditPath := replayAuditFile(t)
	got := captureStdout(t, func() {
		if err := cmdReplay([]string{"-c", path, "-provider", "p1", "-dry-run", auditPath}); err != nil {
			t.Fatalf("cmdReplay -dry-run: %v", err)
		}
	})
	if !strings.Contains(got, "DRY-RUN") || !strings.Contains(got, "real-model") {
		t.Errorf("dry-run output missing expected markers: %q", got)
	}
}

func TestCmdReplay_TSFlag(t *testing.T) {
	path := diagnoseConfigYAML(t, "http://127.0.0.1:1/unreachable")
	line := `{"ts":"2026-07-13T15:30:42.123Z","model":"m1","protocol":"openai","stream":false,"client":{"request":{"headers":{},"body":{"model":"m1","messages":[{"role":"user","content":"hi"}]}}}}` + "\n"
	auditPath := writeTempFile(t, "audit.jsonl", line)
	got := captureStdout(t, func() {
		if err := cmdReplay([]string{"-c", path, "-provider", "p1", "-dry-run", "-ts", "2026-07-13T15:30:42.123Z", auditPath}); err != nil {
			t.Fatalf("cmdReplay -ts: %v", err)
		}
	})
	if !strings.Contains(got, "DRY-RUN") {
		t.Errorf("dry-run output missing expected markers: %q", got)
	}
}

func TestCmdReplay_ReqFlag(t *testing.T) {
	path := diagnoseConfigYAML(t, "http://127.0.0.1:1/unreachable")
	auditPath := replayAuditFile(t)
	got := captureStdout(t, func() {
		if err := cmdReplay([]string{"-c", path, "-provider", "p1", "-dry-run", "-req", "audit.jsonl:1", auditPath}); err != nil {
			t.Fatalf("cmdReplay -req: %v", err)
		}
	})
	if !strings.Contains(got, "DRY-RUN") {
		t.Errorf("dry-run output missing expected markers: %q", got)
	}
}

func TestCmdReplay_ReqMismatchedBasenameErrors(t *testing.T) {
	path := diagnoseConfigYAML(t, "http://127.0.0.1:1/unreachable")
	auditPath := replayAuditFile(t)
	if err := cmdReplay([]string{"-c", path, "-provider", "p1", "-dry-run", "-req", "other-file.jsonl:1", auditPath}); err == nil {
		t.Error("cmdReplay -req with a mismatched basename should return an error")
	}
}

func TestCmdReplay_PrintFlag(t *testing.T) {
	path := diagnoseConfigYAML(t, "http://127.0.0.1:1/unreachable")
	auditPath := replayAuditFile(t)
	got := captureStdout(t, func() {
		if err := cmdReplay([]string{"-c", path, "-print", "-line", "1", auditPath}); err != nil {
			t.Fatalf("cmdReplay -print: %v", err)
		}
	})
	if !strings.Contains(got, `"model":"m1"`) {
		t.Errorf("-print output missing the raw record: %q", got)
	}
}

func TestCmdReplay_PrintDoesNotRequireProvider(t *testing.T) {
	path := diagnoseConfigYAML(t, "http://127.0.0.1:1/unreachable")
	auditPath := replayAuditFile(t)
	if err := cmdReplay([]string{"-c", path, "-print", "-line", "1", auditPath}); err != nil {
		t.Errorf("cmdReplay -print without -provider: %v, want no error", err)
	}
}
