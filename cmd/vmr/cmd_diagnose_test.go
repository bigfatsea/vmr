// Ver 2026-07-30, by Sonnet 5

// CLI-wiring tests for `vmr diagnose`. The underlying mechanics (env
// checks, connectivity classification) are covered in internal/diagnose's
// own test suite; these tests only lock in flag parsing and the command's
// exit-error contract, using local httptest servers so nothing here
// touches the network.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
      - {protocol: openai, providers: [p1], models: [real-model]}
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
