// Ver 2026-07-30, by Sonnet 5

// CLI-wiring tests for `vmr replay`. The underlying mechanics (request
// rebuilding) are covered in internal/replay's own test suite; these tests
// only lock in flag parsing and the command's exit-error contract, using
// local httptest servers so nothing here touches the network.
package main

import (
	"encoding/json"
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

// detailFileJSON mimics what `vmr report`'s WriteDetails actually writes
// under details/*.json: json.MarshalIndent of the raw audit.Record, no
// JSONL framing (see internal/report/detail.go).
func detailFileJSON(t *testing.T) string {
	t.Helper()
	rec := map[string]any{
		"model": "m1", "protocol": "openai", "stream": false,
		"client": map[string]any{"request": map[string]any{
			"headers": map[string]any{},
			"body":    map[string]any{"model": "m1", "messages": []any{map[string]any{"role": "user", "content": "from-detail"}}},
		}},
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return writeTempFile(t, "20260713-153042.100_m1_real-model_ok.json", string(data))
}

func TestCmdReplay_DetailFlag(t *testing.T) {
	path := diagnoseConfigYAML(t, "http://127.0.0.1:1/unreachable")
	detailPath := detailFileJSON(t)
	got := captureStdout(t, func() {
		if err := cmdReplay([]string{"-c", path, "-provider", "p1", "-dry-run", "-detail", detailPath}); err != nil {
			t.Fatalf("cmdReplay -detail: %v", err)
		}
	})
	if !strings.Contains(got, "DRY-RUN") || !strings.Contains(got, "from-detail") {
		t.Errorf("dry-run output missing expected markers: %q", got)
	}
}

func TestCmdReplay_DetailWithAuditFileErrors(t *testing.T) {
	path := diagnoseConfigYAML(t, "http://127.0.0.1:1/unreachable")
	detailPath := detailFileJSON(t)
	auditPath := replayAuditFile(t)
	if err := cmdReplay([]string{"-c", path, "-provider", "p1", "-dry-run", "-detail", detailPath, auditPath}); err == nil {
		t.Error("cmdReplay -detail with an extra audit file argument should return an error")
	}
}
