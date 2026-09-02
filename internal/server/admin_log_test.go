// Ver 2026-09-14, by ox-alpha

package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/logtee"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

func newTeeServer(t *testing.T, yaml string) (*Server, *logtee.Tee) {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)
	tee := logtee.New(logtee.DefaultCapLines)
	return New(rt, nil).WithLogTee(tee), tee
}

// logStream opens /log against a real HTTP server and returns a line reader
// plus a cancel that disconnects the client.
func logStream(t *testing.T, h http.Handler, headers map[string]string) (*bufio.Reader, func()) {
	t.Helper()
	ts := httptest.NewServer(h)
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "GET", ts.URL+"/log", nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		ts.Close()
		t.Fatal(err)
	}
	return bufio.NewReader(resp.Body), func() {
		cancel()
		resp.Body.Close()
		ts.Close()
	}
}

func TestAdminLog_NoTeeReturns503(t *testing.T) {
	cfg, err := config.Parse([]byte(oneProviderYAML("http://127.0.0.1:1")))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)
	srv := New(rt, nil)

	req := httptest.NewRequest("GET", "/log", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestAdminLog_ReplayThenFollow(t *testing.T) {
	srv, tee := newTeeServer(t, oneProviderYAML("http://127.0.0.1:1"))
	tee.Write([]byte("history-1\n"))
	tee.Write([]byte("history-2\n"))

	rd, stop := logStream(t, srv.Handler(), nil)
	defer stop()

	assertLogLine(t, rd, "history-1")
	assertLogLine(t, rd, "history-2")

	// A line written after the connection opened must arrive live.
	tee.Write([]byte("live-after-connect\n"))
	assertLogLine(t, rd, "live-after-connect")

	// And it must be in the buffer for the *next* connection's replay.
	replay, _, cancelReplay := tee.Follow()
	defer cancelReplay()
	if got := strings.Join(replay, ","); !strings.Contains(got, "live-after-connect") {
		t.Fatalf("buffer = %q, missing live-after-connect", got)
	}
}

// TestAdminLog_WriteErrorOnDisconnect exercises the IS-20 fix: a write to the
// response body after the client has hung up returns an error, and the server
// must return immediately (triggering defer cancel()) rather than continuing
// to the next select iteration and waiting for the heartbeat timeout or
// context-done path to clean up the subscriber.
func TestAdminLog_WriteErrorOnDisconnect(t *testing.T) {
	srv, tee := newTeeServer(t, oneProviderYAML("http://127.0.0.1:1"))
	tee.Write([]byte("history-1\n"))

	rd, stop := logStream(t, srv.Handler(), nil)
	assertLogLine(t, rd, "history-1")
	if got := tee.Subscribers(); got != 1 {
		t.Fatalf("Subscribers while connected = %d, want 1", got)
	}

	// Disconnect the client and immediately write to the tee. The server is
	// blocked on <-ch waiting for a new line; when the client disconnect
	// propagates (the write fails), the server must exit quickly.
	stop()
	// Give the server a moment to detect the disconnect.
	time.Sleep(50 * time.Millisecond)
	tee.Write([]byte("after-disconnect\n"))

	deadline := time.Now().Add(2 * time.Second)
	for tee.Subscribers() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := tee.Subscribers(); got != 0 {
		t.Fatalf("Subscribers after disconnect + write = %d, want 0", got)
	}
}

func TestAdminLog_ClientDisconnectCleansUp(t *testing.T) {
	srv, tee := newTeeServer(t, oneProviderYAML("http://127.0.0.1:1"))
	tee.Write([]byte("history-1\n"))

	rd, stop := logStream(t, srv.Handler(), nil)
	assertLogLine(t, rd, "history-1")
	if got := tee.Subscribers(); got != 1 {
		t.Fatalf("Subscribers while connected = %d, want 1", got)
	}

	stop()
	deadline := time.Now().Add(2 * time.Second)
	for tee.Subscribers() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := tee.Subscribers(); got != 0 {
		t.Fatalf("Subscribers after disconnect = %d, want 0", got)
	}
}

func TestAdminLog_AuthEnforced(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
api_keys: [test-key-0123456789]
providers:
  - {name: p1, base_url: {openai-completions: http://127.0.0.1:1}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [upstream-model]}
`
	srv, _ := newTeeServer(t, yaml)

	req := httptest.NewRequest("GET", "/log", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no-key status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest("GET", "/log", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad-key status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// Good key: past auth the handler streams forever, so this needs a real
	// client round-trip that hangs up after seeing 200 — a recorder-based
	// call would block inside ServeHTTP until the test timeout.
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req2, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/log", nil)
	req2.Header.Set("Authorization", "Bearer test-key-0123456789")
	resp, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("good-key status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestAdminLog_ResponseHeaders(t *testing.T) {
	srv, _ := newTeeServer(t, oneProviderYAML("http://127.0.0.1:1"))

	req := httptest.NewRequest("GET", "/log", nil)
	w := httptest.NewRecorder()
	// Cancel immediately: headers are set before any blocking work.
	ctx, cancel := context.WithCancel(req.Context())
	go cancel()
	srv.Handler().ServeHTTP(w, req.WithContext(ctx))

	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

func TestAdminLog_IdleHeartbeat(t *testing.T) {
	old := logHeartbeat
	logHeartbeat = 30 * time.Millisecond
	defer func() { logHeartbeat = old }()

	srv, tee := newTeeServer(t, oneProviderYAML("http://127.0.0.1:1"))
	tee.Write([]byte("only-line\n"))

	rd, stop := logStream(t, srv.Handler(), nil)
	defer stop()

	assertLogLine(t, rd, "only-line")
	// With no further writes, only bare keepalive newlines should arrive.
	blank := 0
	deadline := time.Now().Add(2 * time.Second)
	for blank < 3 && time.Now().Before(deadline) {
		line, err := rd.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if strings.TrimSpace(line) == "" {
			blank++
		} else {
			t.Fatalf("unexpected non-blank line during idle: %q", line)
		}
	}
	if blank < 3 {
		t.Fatalf("got %d heartbeats in window, want >= 3", blank)
	}
}

func TestLogPage_ServesHTML(t *testing.T) {
	cfg, err := config.Parse([]byte(oneProviderYAML("http://127.0.0.1:1")))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)
	srv := New(rt, nil)

	req := httptest.NewRequest("GET", "/log.html", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := w.Body.String()
	for _, marker := range []string{"VMR Live Log", "vmr_status_key", "readStream", "btn-clear", `href="/status.html"`} {
		if !strings.Contains(body, marker) {
			t.Errorf("body missing %q", marker)
		}
	}
}

// assertLogLine reads one newline-terminated line from the stream and
// compares it with want; an empty want matches a bare keepalive newline.
func assertLogLine(t *testing.T, rd *bufio.Reader, want string) {
	t.Helper()
	line, err := rd.ReadString('\n')
	if err != nil {
		t.Fatalf("read log line (want %q): %v", want, err)
	}
	if got := strings.TrimRight(line, "\n"); got != want {
		t.Fatalf("log line = %q, want %q", got, want)
	}
}
