// Ver 2026-07-30, by Sonnet 5

// CLI-wiring tests for `vmr diagnose` and `vmr replay`. The underlying
// mechanics (env checks, connectivity classification, request rebuilding)
// are covered in internal/diagnose and internal/replay's own test suites;
// these tests only lock in flag parsing and the command's exit-error
// contract, using local httptest servers so nothing here touches the
// network.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func diagnoseConfigYAML(t *testing.T, upstreamURL string) string {
	t.Helper()
	return writeTempFile(t, "config.yaml", fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: %q}, api_key: test-key}
models:
  m1:
    endpoints:
      - {protocol: openai, provider: p1, models: [real-model]}
`, upstreamURL))
}

func TestCmdDiagnose_NoTestRoutingSucceeds(t *testing.T) {
	// -no-test-routing never dials the upstream, so an unreachable base_url
	// is fine here — only phase 1/2 (config + DNS/TLS/api_key) run.
	path := diagnoseConfigYAML(t, "http://127.0.0.1:1/unreachable")
	if err := cmdDiagnose([]string{"-c", path, "-no-test-routing"}); err != nil {
		t.Errorf("cmdDiagnose -no-test-routing: %v", err)
	}
}

func TestCmdDiagnose_FailingConnectivityIsNonZeroExit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()
	path := diagnoseConfigYAML(t, ts.URL)
	if err := cmdDiagnose([]string{"-c", path}); err == nil {
		t.Error("cmdDiagnose should return an error when a connectivity check fails")
	}
}

func TestCmdDiagnose_PassingConnectivitySucceeds(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	}))
	defer ts.Close()
	path := diagnoseConfigYAML(t, ts.URL)
	if err := cmdDiagnose([]string{"-c", path}); err != nil {
		t.Errorf("cmdDiagnose with a healthy endpoint: %v", err)
	}
}

func TestCmdDiagnose_JSONOutputIsValid(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	}))
	defer ts.Close()
	path := diagnoseConfigYAML(t, ts.URL)
	got := captureStdout(t, func() {
		if err := cmdDiagnose([]string{"-c", path, "-json"}); err != nil {
			t.Fatalf("cmdDiagnose -json: %v", err)
		}
	})
	var results []map[string]any
	if err := json.Unmarshal([]byte(got), &results); err != nil {
		t.Fatalf("-json output is not a JSON array: %v\n%s", err, got)
	}
	if len(results) == 0 {
		t.Error("expected at least one result")
	}
}

func TestCmdDiagnose_MissingConfig(t *testing.T) {
	if err := cmdDiagnose([]string{"-c", filepath.Join(t.TempDir(), "does-not-exist.yaml")}); err == nil {
		t.Error("cmdDiagnose on a missing config should return an error")
	}
}

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
