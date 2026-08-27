// Ver 2026-08-23, by Gemini

// CLI-wiring tests for `vmr smoke`. The routing mechanics (pinned routing,
// header stripping) are covered in internal/router and internal/server's
// own suites; these tests lock in the command's contract: it dials a real
// router-backed server, reports pass/fail per backend, honors the filters,
// and exits non-zero when any smoke fails — all against local httptest
// servers so nothing touches the network.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vmr/internal/config"
	"vmr/internal/router"
	"vmr/internal/server"

	_ "vmr/internal/adapter/openai"
)

// smokeServerYAML is a config whose providers point at real local upstreams
// and whose listen is irrelevant (the test passes -addr). api_keys is set
// so cmdSmoke's auto-key path (cfg.APIKeys[0]) is exercised against the
// router-backed server's real auth.
func smokeServerYAML(t *testing.T, u1, u2 string) string {
	t.Helper()
	return fmt.Sprintf(`
listen: 127.0.0.1:0
api_keys: ["sk-vmr-smoke-test-key"]
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: key-a}
  - {name: p2, base_url: {openai-completions: %s}, api_key: key-b}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [model-a], priority: 1}
      - {protocol: openai-completions, providers: [p2], models: [model-b], priority: 2}
`, u1, u2)
}

func TestCmdSmoke_AllHealthy(t *testing.T) {
	up1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","model":"x","choices":[]}`)
	}))
	defer up1.Close()
	up2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","model":"x","choices":[]}`)
	}))
	defer up2.Close()

	yaml := smokeServerYAML(t, up1.URL, up2.URL)
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	ts := httptest.NewServer(server.New(rt, nil).Handler())
	defer ts.Close()

	path := writeTempFile(t, "config.yaml", yaml)
	// The config's listen is 127.0.0.1:0 (a placeholder); the running
	// server lives at ts.URL. Point smoke at it explicitly.
	addr := strings.TrimPrefix(ts.URL, "http://")
	out := captureStdout(t, func() {
		if err := cmdSmoke([]string{"-c", path, "-addr", addr, "-parallel", "2"}); err != nil {
			t.Fatalf("cmdSmoke with healthy backends: %v", err)
		}
	})
	if !strings.Contains(out, "smoke: 2/2 ok") {
		t.Errorf("summary missing; got:\n%s", out)
	}
	if !strings.Contains(out, "model-a") || !strings.Contains(out, "model-b") {
		t.Errorf("table missing target models; got:\n%s", out)
	}
}

func TestCmdSmoke_FailureIsNonZeroExit(t *testing.T) {
	upOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","model":"x","choices":[]}`)
	}))
	defer upOK.Close()
	upBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"message":"boom"}}`)
	}))
	defer upBad.Close()

	yaml := smokeServerYAML(t, upOK.URL, upBad.URL)
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	ts := httptest.NewServer(server.New(rt, nil).Handler())
	defer ts.Close()

	path := writeTempFile(t, "config.yaml", yaml)
	addr := strings.TrimPrefix(ts.URL, "http://")
	if err := cmdSmoke([]string{"-c", path, "-addr", addr}); err == nil {
		t.Error("cmdSmoke should return an error when a backend fails")
	}
}

func TestCmdSmoke_ProviderFilter(t *testing.T) {
	up1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	}))
	defer up1.Close()
	yaml := smokeServerYAML(t, up1.URL, up1.URL)
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, _ := router.BuildSnapshot(cfg)
	rt.Install(snap)
	ts := httptest.NewServer(server.New(rt, nil).Handler())
	defer ts.Close()

	path := writeTempFile(t, "config.yaml", yaml)
	addr := strings.TrimPrefix(ts.URL, "http://")
	out := captureStdout(t, func() {
		if err := cmdSmoke([]string{"-c", path, "-addr", addr, "-provider", "p1", "-json"}); err != nil {
			t.Fatalf("cmdSmoke -provider p1: %v", err)
		}
	})
	var results []smokeResult
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out)
	}
	if len(results) != 1 || results[0].Provider != "p1" || results[0].Target != "model-a" {
		t.Fatalf("provider filter = %+v, want single p1/model-a", results)
	}
}

func TestCmdSmoke_MissingConfig(t *testing.T) {
	if err := cmdSmoke([]string{"-c", "/nonexistent/config.yaml"}); err == nil {
		t.Error("cmdSmoke on a missing config should return an error")
	}
}
