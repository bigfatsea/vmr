// Ver 2026-09-14, by ox-alpha

package server

import (
	_ "embed"
	"io"
	"net/http"
	"time"

	"vmr/internal/logtee"
	"vmr/internal/router"
)

//go:embed log.html
var logHTMLPage []byte

// logHeartbeat is how long /log tolerates silence before writing a bare
// keepalive newline — proxies and LBs in the path close idle connections,
// and a log tail is idle by definition between requests. A var, not a
// const, only so tests can shorten it.
var logHeartbeat = 30 * time.Second

// WithLogTee wires the live-log source. Only `vmr start` calls it; without
// it /log answers 503 (tests construct Servers with no tee — same pattern
// as WithInstance's zero-value instance).
func (s *Server) WithLogTee(tee *logtee.Tee) *Server {
	s.logTee = tee
	return s
}

// logPage serves the self-contained static live-log shell, unauthenticated:
// like status.html it contains zero business data. The embedded JS opens
// GET /log itself (which enforces s.auth) and prompts for a key on 401.
func (s *Server) logPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(logHTMLPage)
}

// adminLog streams the process's live console log as text/plain — one line
// per log line, byte-identical to what stderr carries: replay of the tee's
// ring buffer first, then new lines as they arrive, forever (or until the
// client hangs up). This is the browser-side replacement for `tail -f` on
// the terminal; the audit JSONL is a different dataset and stays untouched.
//
// No query parameters: the replay window IS the ring buffer size, so
// exposing a count would invite "asked for 5000, got 512".
func (s *Server) adminLog(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok || s.logTee == nil {
		// s.logTee == nil is the real case (no `vmr start` wiring); a
		// non-flushing ResponseWriter merely can't stream, so it gets the
		// same refusal rather than a silently-terminating replay.
		router.WriteError(w, http.StatusServiceUnavailable, "unavailable",
			"live log streaming is not available on this server instance")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	// Follow, not Recent+Subscribe: the atomic snapshot-and-register means a
	// line written while this connection opens lands in exactly one of
	// replay or live stream, never silently in neither.
	replay, ch, cancel := s.logTee.Follow()
	defer cancel()
	for _, line := range replay {
		io.WriteString(w, line+"\n")
	}
	flusher.Flush()
	timer := time.NewTimer(logHeartbeat)
	defer timer.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case line := <-ch:
			io.WriteString(w, line+"\n")
			flusher.Flush()
			timer.Reset(logHeartbeat)
		case <-timer.C:
			io.WriteString(w, "\n")
			flusher.Flush()
			timer.Reset(logHeartbeat)
		}
	}
}
