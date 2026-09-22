// Ver 2026-09-21, by Sonnet 5

package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestServeAndOpen_BindsLoopbackOnly pins the one non-negotiable part of the
// -open spec: never 0.0.0.0. reports/ carries full conversation bodies.
func TestServeAndOpen_BindsLoopbackOnly(t *testing.T) {
	ln, err := newLoopbackListener()
	if err != nil {
		t.Fatalf("newLoopbackListener: %v", err)
	}
	defer ln.Close()

	host, _, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split addr %q: %v", ln.Addr().String(), err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		t.Fatalf("listener bound to %q, want a loopback address", ln.Addr().String())
	}
}

// TestServeAndOpen_RejectsEscape covers the escape os.Root exists to close:
// a symlink inside outDir pointing outside it must not be servable.
func TestServeAndOpen_RejectsEscape(t *testing.T) {
	outside := t.TempDir()
	secretPath := filepath.Join(outside, "secret.json")
	if err := os.WriteFile(secretPath, []byte(`{"leaked":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	escapeLink := filepath.Join(outDir, "escape.json")
	if err := os.Symlink(secretPath, escapeLink); err != nil {
		t.Skipf("symlink not supported on this platform: %v", err)
	}

	handler, root, err := buildReportsHandler(outDir)
	if err != nil {
		t.Fatalf("buildReportsHandler: %v", err)
	}
	defer root.Close()

	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/escape.json")
	if err != nil {
		t.Fatalf("GET /escape.json: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		t.Errorf("GET /escape.json = %d, want a non-2xx rejection of the symlink escape", resp.StatusCode)
	}
}

// TestServeAndOpen_ServesIndex covers the ordinary path: a real file inside
// outDir must be servable byte-for-byte.
func TestServeAndOpen_ServesIndex(t *testing.T) {
	outDir := t.TempDir()
	want := []byte(`{"format":12,"lang":"en"}`)
	if err := os.WriteFile(filepath.Join(outDir, "manifest.json"), want, 0o600); err != nil {
		t.Fatal(err)
	}

	handler, root, err := buildReportsHandler(outDir)
	if err != nil {
		t.Fatalf("buildReportsHandler: %v", err)
	}
	defer root.Close()

	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/manifest.json")
	if err != nil {
		t.Fatalf("GET /manifest.json: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /manifest.json = %d, want 200", resp.StatusCode)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("body = %q, want %q", got, want)
	}
}
