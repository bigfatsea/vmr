// Ver 2026-08-31

package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vmr/internal/config"
	"vmr/internal/router"
)

// reportsFixtureYAML builds a config with analytics.serve on and serve_dir
// pointed at dir. extra is spliced at top level (same convention as
// twoEndpointYAML's `extra`).
func reportsFixtureYAML(dir string, extra string) string {
	return fmt.Sprintf(`
listen: 127.0.0.1:0
%s
analytics:
  serve: true
  serve_dir: %s
providers:
  - {name: p1, base_url: {openai-completions: https://upstream.example/v1}, api_key: k1}
models:
  vm:
    sticky: false
    endpoints:
      openai-completions:
        - {providers: [p1], models: [model-one], priority: 1}
`, extra, dir)
}

const reportsTestKey = "sk-vmr-reports-key-001"

// newReportsServer mounts a server whose /reports/* is served from a fresh
// temp directory with a real report layout. The served files are tiny
// stand-ins, but the tree shape (root skeleton pages, requests/,
// requests/details/, journeys/) is the one the design doc pins.
func newReportsServer(t *testing.T, withKey bool, missingDir bool) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	if missingDir {
		dir = filepath.Join(dir, "not-generated-yet")
	} else {
		writeReportsTree(t, dir)
	}
	apiKeys := fmt.Sprintf("api_keys:\n  - %s\n", reportsTestKey)
	if !withKey {
		apiKeys = ""
	}
	ts := newRouterServer(t, reportsFixtureYAML(dir, apiKeys))
	return ts, dir
}

func writeReportsTree(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"macro-dashboard.html":    "<!doctype html><html><body>skeleton</body></html>",
		"request-browser.html":    "<!doctype html><html><body>skeleton2</body></html>",
		"requests/index.json":     `{"requests": []}`,
		"requests/failed.md":      "# failed requests",
		"requests/failed.jsonl":   `{"id": "r-1"}` + "\n",
		"requests/details/r-1.md": "# request r-1\n\nfull conversation body — sensitive\n",
		"journeys/index.json":     `{"journeys": []}`,
	}
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func getReports(t *testing.T, ts *httptest.Server, path, key string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+path, nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

// TestReports_ServeOffRouteAbsent: analytics.serve absent/false must leave
// /reports/* answering the mux's plain 404 — not a handler that answers
// differently, not a 403, just "no such route".
func TestReports_ServeOffRouteAbsent(t *testing.T) {
	ts := newRouterServer(t, twoEndpointYAML("http://u1", "http://u2", ""))
	for _, path := range []string{"/reports", "/reports/", "/reports/macro-dashboard.html", "/reports/requests/index.json"} {
		resp, _ := getReports(t, ts, path, reportsTestKey)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s with serve off: status=%d, want 404 (route must not exist)", path, resp.StatusCode)
		}
	}
}

// TestReports_NoKeysHardReject pins D9's hard rule: with api_keys absent,
// EVERY /reports/* request is 403 — including the unauthenticated-by-design
// .html skeletons. The generic auth wrapper's "no key = open door" must
// never apply to reports: they carry conversation bodies.
func TestReports_NoKeysHardReject(t *testing.T) {
	ts, _ := newReportsServer(t, false, false)
	for _, path := range []string{
		"/reports/macro-dashboard.html",
		"/reports/requests/index.json",
		"/reports/requests/details/r-1.md",
		"/reports/requests/",
	} {
		resp, body := getReports(t, ts, path, "")
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s with no api_keys: status=%d, want 403", path, resp.StatusCode)
		}
		if !strings.Contains(body, "api_keys") {
			t.Errorf("%s: 403 body should name the cause, got: %s", path, body)
		}
	}
}

// TestReports_SkeletonUnauthenticated: with keys configured, .html skeleton
// pages are served with no credential at all (same contract as status.html
// — zero business data in the page).
func TestReports_SkeletonUnauthenticated(t *testing.T) {
	ts, _ := newReportsServer(t, true, false)
	resp, body := getReports(t, ts, "/reports/macro-dashboard.html", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "skeleton") {
		t.Errorf("skeleton content missing: %s", body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type=%q, want text/html", ct)
	}
}

// TestReports_DataFilesRequireAuth: every data extension needs a valid key;
// a wrong or missing key gets 401 BEFORE any existence signal (404 vs 200)
// can leak whether the file is there.
func TestReports_DataFilesRequireAuth(t *testing.T) {
	ts, _ := newReportsServer(t, true, false)
	cases := []struct {
		path, wantCT string
	}{
		{"/reports/requests/index.json", "application/json"},
		{"/reports/requests/failed.jsonl", "application/jsonl"},
		{"/reports/requests/failed.md", "text/markdown"},
	}
	for _, tc := range cases {
		for _, key := range []string{"", "sk-wrong-key-0000000001"} {
			resp, _ := getReports(t, ts, tc.path, key)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("%s key=%q: status=%d, want 401", tc.path, key, resp.StatusCode)
			}
		}
		resp, body := getReports(t, ts, tc.path, reportsTestKey)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s with valid key: status=%d, want 200", tc.path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, tc.wantCT) {
			t.Errorf("%s: Content-Type=%q, want prefix %q", tc.path, ct, tc.wantCT)
		}
		if tc.path == "/reports/requests/details/r-1.md" && !strings.Contains(body, "conversation body") {
			t.Errorf("content missing: %s", body)
		}
	}
}

// TestReports_PathTraversalRejected: ".." escapes and absolute-path escapes
// must never reach the filesystem — 404 either way, and the outside file's
// content must never appear.
func TestReports_PathTraversalRejected(t *testing.T) {
	ts, dir := newReportsServer(t, true, false)
	secret := filepath.Join(filepath.Dir(dir), "vmr-secret-outside.txt")
	if err := os.WriteFile(secret, []byte("TOP SECRET OUTSIDE"), 0600); err != nil {
		t.Fatal(err)
	}
	// The http.ServeMux normalizes some of these before the handler ever
	// sees them; the ones that survive (raw %2e%2e, double-slash forms)
	// exercise the handler's own prefix check.
	paths := []string{
		"/reports/../../" + filepath.Base(filepath.Dir(dir)) + "/vmr-secret-outside.txt",
		"/reports/%2e%2e/%2e%2e/" + filepath.Base(filepath.Dir(dir)) + "/vmr-secret-outside.txt",
		"/reports/..%2f..%2f" + filepath.Base(filepath.Dir(dir)) + "/vmr-secret-outside.txt",
		"/reports/requests/../../vmr-secret-outside.txt",
	}
	for _, p := range paths {
		resp, body := getReports(t, ts, p, reportsTestKey)
		if resp.StatusCode == http.StatusOK {
			t.Errorf("%s: status=200 with body %q — traversal succeeded", p, body)
		}
		if strings.Contains(body, "TOP SECRET OUTSIDE") {
			t.Errorf("%s: outside file content leaked", p)
		}
	}
}

// TestReports_SymlinkRejected: a symlink inside serve_dir pointing outside
// must be refused at any level of the path.
func TestReports_SymlinkRejected(t *testing.T) {
	ts, dir := newReportsServer(t, true, false)
	outside := filepath.Join(filepath.Dir(dir), "vmr-secret-outside.txt")
	if err := os.WriteFile(outside, []byte("TOP SECRET OUTSIDE"), 0600); err != nil {
		t.Fatal(err)
	}
	// Final-component symlink.
	if err := os.Symlink(outside, filepath.Join(dir, "leak.md")); err != nil {
		t.Fatal(err)
	}
	// Intermediate-directory symlink.
	if err := os.Symlink(filepath.Dir(dir), filepath.Join(dir, "requests", "escape")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/reports/leak.md", "/reports/escape/vmr-secret-outside.txt"} {
		resp, body := getReports(t, ts, p, reportsTestKey)
		if resp.StatusCode == http.StatusOK {
			t.Errorf("%s: status=200 with body %q — symlink followed", p, body)
		}
		if strings.Contains(body, "TOP SECRET OUTSIDE") {
			t.Errorf("%s: outside content leaked through symlink", p)
		}
	}
}

// TestReports_DirectoryListingDisabled: any directory under serve_dir 404s
// — the listing of requests/details/ is itself the DoS the design doc
// calls out, so there is no index and no fallback.
func TestReports_DirectoryListingDisabled(t *testing.T) {
	ts, _ := newReportsServer(t, true, false)
	for _, p := range []string{"/reports", "/reports/", "/reports/requests", "/reports/requests/", "/reports/requests/details", "/reports/requests/details/"} {
		resp, _ := getReports(t, ts, p, reportsTestKey)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status=%d, want 404 (no directory listing)", p, resp.StatusCode)
		}
	}
}

// TestReports_MissingDir404: serve_dir not existing is a normal state (the
// products are regenerable), never a startup error — the mounted route
// just answers 404 for everything.
func TestReports_MissingDir404(t *testing.T) {
	ts, _ := newReportsServer(t, true, true)
	for _, p := range []string{"/reports/macro-dashboard.html", "/reports/requests/index.json"} {
		resp, _ := getReports(t, ts, p, reportsTestKey)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s with missing serve_dir: status=%d, want 404", p, resp.StatusCode)
		}
	}
}

// TestReports_UnknownExtensionNotServed: only the analyze suite's own file
// extensions are served; anything else (e.g. a stray binary dropped into
// the directory) 404s rather than guessing at a MIME type.
func TestReports_UnknownExtensionNotServed(t *testing.T) {
	ts, dir := newReportsServer(t, true, false)
	if err := os.WriteFile(filepath.Join(dir, "payload.bin"), []byte("\x00\x01binary"), 0600); err != nil {
		t.Fatal(err)
	}
	resp, _ := getReports(t, ts, "/reports/payload.bin", reportsTestKey)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown extension: status=%d, want 404", resp.StatusCode)
	}
}

// TestReports_LogThrottledPerDirectory: the "not generated yet" notice logs
// once per directory, not per request.
func TestReports_LogThrottledPerDirectory(t *testing.T) {
	dir := t.TempDir() // exists, so the handler's stat succeeds; use a missing path via state directly
	w := &missingDirWarner{}
	calls := 0
	warn := func(string, ...any) { calls++ }
	w.warn(filepath.Base(dir), warn)
	w.warn(filepath.Base(dir), warn)
	w.warn(filepath.Base(dir), warn)
	if calls != 1 {
		t.Errorf("warn called %d times for the same dir, want 1", calls)
	}
	w.warn(filepath.Base(dir)+"-other", warn)
	if calls != 2 {
		t.Errorf("warn not re-armed for a new dir: calls=%d, want 2", calls)
	}
}

// installYAML rebuilds a routing snapshot from yaml and installs it on rt —
// the same atomic swap a config hot-reload performs.
func installYAML(t *testing.T, rt *router.Router, yaml string) {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("config.Parse: %v", err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	rt.Install(snap)
}

// newReportsServerRT is newReportsServer's sibling that also hands back the
// router, so a test can install a new snapshot mid-flight (hot reload).
func newReportsServerRT(t *testing.T, yaml string) (*httptest.Server, *router.Router) {
	t.Helper()
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
	ts := httptest.NewServer(New(rt, nil).Handler())
	t.Cleanup(ts.Close)
	return ts, rt
}

// TestReports_HotReloadServeToggle: /reports must follow analytics.serve
// across a config hot reload — mountReports runs once at startup, so the
// gate has to be read from the live snapshot per request.
func TestReports_HotReloadServeToggle(t *testing.T) {
	dir := t.TempDir()
	writeReportsTree(t, dir)
	keys := fmt.Sprintf("api_keys:\n  - %s\n", reportsTestKey)
	offYAML := fmt.Sprintf(`
listen: 127.0.0.1:0
%s
analytics:
  serve: false
  serve_dir: %s
providers:
  - {name: p1, base_url: {openai-completions: https://u/v1}, api_key: k1}
models:
  vm: {sticky: false, endpoints: {openai-completions: [{providers: [p1], models: [m1]}]}}
`, keys, dir)
	onYAML := strings.Replace(offYAML, "serve: false", "serve: true", 1)

	ts, rt := newReportsServerRT(t, offYAML)

	resp, _ := getReports(t, ts, "/reports/macro-dashboard.html", reportsTestKey)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("serve off: status=%d, want 404", resp.StatusCode)
	}

	installYAML(t, rt, onYAML)
	resp, _ = getReports(t, ts, "/reports/macro-dashboard.html", reportsTestKey)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("after hot-reload to serve:true: status=%d, want 200", resp.StatusCode)
	}

	installYAML(t, rt, offYAML)
	resp, _ = getReports(t, ts, "/reports/macro-dashboard.html", reportsTestKey)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("after hot-reload back to serve:false: status=%d, want 404", resp.StatusCode)
	}
}

// TestReports_HotReloadServeDir: changing serve_dir at hot reload must
// redirect /reports at the new tree, not keep serving the old one.
func TestReports_HotReloadServeDir(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeReportsTree(t, dirA)
	writeReportsTree(t, dirB)
	if err := os.WriteFile(filepath.Join(dirA, "only-a.json"), []byte(`{"a":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "only-b.json"), []byte(`{"b":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	keys := fmt.Sprintf("api_keys:\n  - %s\n", reportsTestKey)
	yamlFor := func(dir string) string {
		return fmt.Sprintf(`
listen: 127.0.0.1:0
%s
analytics: {serve: true, serve_dir: %s}
providers:
  - {name: p1, base_url: {openai-completions: https://u/v1}, api_key: k1}
models:
  vm: {sticky: false, endpoints: {openai-completions: [{providers: [p1], models: [m1]}]}}
`, keys, dir)
	}

	ts, rt := newReportsServerRT(t, yamlFor(dirA))

	if resp, _ := getReports(t, ts, "/reports/only-a.json", reportsTestKey); resp.StatusCode != http.StatusOK {
		t.Fatalf("dirA/only-a.json: status=%d, want 200", resp.StatusCode)
	}

	installYAML(t, rt, yamlFor(dirB))
	if resp, _ := getReports(t, ts, "/reports/only-b.json", reportsTestKey); resp.StatusCode != http.StatusOK {
		t.Fatalf("after serve_dir hot-reload: dirB/only-b.json status=%d, want 200", resp.StatusCode)
	}
	if resp, _ := getReports(t, ts, "/reports/only-a.json", reportsTestKey); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("after serve_dir hot-reload: stale dirA/only-a.json status=%d, want 404", resp.StatusCode)
	}
}
