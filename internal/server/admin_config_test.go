// Ver 2026-10-05 17:20, by Claude Opus 4.6

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

// baseConfigYAML is a minimal valid config with the editor enabled. Each test
// rewrites it into a temp file so the PUT path can verify what actually lands
// on disk.
const baseConfigYAML = `
listen: 127.0.0.1:18801
admin:
  config_edit: true
providers:
  - {name: p1, base_url: {openai-completions: http://127.0.0.1:1}, api_key: k1}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
`

func newConfigEditServer(t *testing.T, yaml string) (*Server, *httptest.Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
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
	s := New(rt, nil).WithInstance(path, time.Now())
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts, path
}

func configEditRequest(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "text/yaml")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestConfigEditDisabledReturns403(t *testing.T) {
	disabled := strings.Replace(baseConfigYAML, "config_edit: true", "config_edit: false", 1)
	_, ts, _ := newConfigEditServer(t, disabled)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/config"}, {"PUT", "/config"}, {"POST", "/config/validate"},
	} {
		resp := configEditRequest(t, tc.method, ts.URL+tc.path, baseConfigYAML)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403 (admin.config_edit off must gate all three endpoints)", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestConfigGetReturnsDiskBytes(t *testing.T) {
	_, ts, path := newConfigEditServer(t, baseConfigYAML)
	resp := configEditRequest(t, "GET", ts.URL+"/config", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /config = %d, want 200", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	want, _ := os.ReadFile(path)
	if string(got) != string(want) {
		t.Error("GET /config must return the file on disk byte-identically, not a marshaled config object")
	}
	if resp.Header.Get("ETag") == "" {
		t.Error("ETag header missing — PUT's If-Match precondition has nothing to compare against")
	}
	if resp.Header.Get("X-Reload-State") != "never" {
		t.Errorf("X-Reload-State = %q, want \"never\" (no reload attempted since start)", resp.Header.Get("X-Reload-State"))
	}
}

func TestConfigPutValidAppliesAndReloads(t *testing.T) {
	s, ts, path := newConfigEditServer(t, baseConfigYAML)
	reloaded := false
	s.WithConfigReload(func() router.ReloadState {
		reloaded = true
		return router.ReloadState{At: time.Now(), Trigger: "api", OK: true, Count: 1, OKAt: time.Now()}
	})
	edited := strings.Replace(baseConfigYAML, "api_key: k1", "api_key: k2", 1)
	resp := configEditRequest(t, "PUT", ts.URL+"/config", edited)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT /config = %d, want 200 (body: %s)", resp.StatusCode, body)
	}
	onDisk, _ := os.ReadFile(path)
	if string(onDisk) != edited {
		t.Error("PUT must write the candidate text back to the file verbatim (comments included)")
	}
	if !reloaded {
		t.Error("injected reload hook was not called after a successful write")
	}
}

func TestConfigPutInvalidLeavesFileUntouched(t *testing.T) {
	s, ts, path := newConfigEditServer(t, baseConfigYAML)
	called := false
	s.WithConfigReload(func() router.ReloadState { called = true; return router.ReloadState{} })
	before, _ := os.ReadFile(path)
	resp := configEditRequest(t, "PUT", ts.URL+"/config", "not_a_valid_key: true\n")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT invalid config = %d, want 400", resp.StatusCode)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("a rejected candidate must never touch the file")
	}
	if called {
		t.Error("reload hook must not run for a candidate that failed validation")
	}
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.OK || out.Error == "" {
		t.Errorf("400 body should carry ok:false and the validator's own error text, got %+v", out)
	}
}

func TestConfigPutIfMatchMismatchConflicts(t *testing.T) {
	s, ts, path := newConfigEditServer(t, baseConfigYAML)
	s.WithConfigReload(func() router.ReloadState { return router.ReloadState{At: time.Now(), Trigger: "api", OK: true} })
	// Someone edits the file on disk after the client fetched its ETag.
	if err := os.WriteFile(path, []byte(baseConfigYAML+"\n# external edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("PUT", ts.URL+"/config", strings.NewReader(baseConfigYAML))
	req.Header.Set("Content-Type", "text/yaml")
	req.Header.Set("If-Match", "0000000000000000")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("PUT with stale If-Match = %d, want 409", resp.StatusCode)
	}
}

func TestConfigPutWithoutReloadHookReturns501(t *testing.T) {
	_, ts, _ := newConfigEditServer(t, baseConfigYAML)
	// No WithConfigReload wired (test/embedding shape): writing a file that
	// would never take effect is worse than refusing the write.
	resp := configEditRequest(t, "PUT", ts.URL+"/config", baseConfigYAML)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("PUT without reload hook = %d, want 501", resp.StatusCode)
	}
}

func TestConfigValidateEndpoint(t *testing.T) {
	_, ts, _ := newConfigEditServer(t, baseConfigYAML)
	resp := configEditRequest(t, "POST", ts.URL+"/config/validate", baseConfigYAML)
	defer resp.Body.Close()
	var out struct {
		OK     bool           `json:"ok"`
		Error  string         `json:"error"`
		Issues []config.Issue `json:"issues"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Errorf("validate of a valid config = %+v", out)
	}

	bad := configEditRequest(t, "POST", ts.URL+"/config/validate", "listen: [1,2]\n")
	defer bad.Body.Close()
	var badOut struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(bad.Body).Decode(&badOut); err != nil {
		t.Fatal(err)
	}
	if badOut.OK || badOut.Error == "" {
		t.Errorf("validate of an invalid config should answer ok:false with the parse error, got %+v", badOut)
	}
}

func TestAtomicWriteConfigKeepsModeAndNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteConfig(path, []byte("a: 2\n")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("config mode after atomic write = %v, want 0600 (the file carries every upstream credential)", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "config.yaml" {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}
