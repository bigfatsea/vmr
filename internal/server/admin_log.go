// Ver 2026-09-14, by ox-alpha

package server

import (
	_ "embed"
	"io"
	"net/http"
	"sync"
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

// logWriteTimeout bounds a single write to a /log client. The global
// http.Server WriteTimeout is 0 (a streaming endpoint cannot carry one), so
// without this a client that stops reading — deliberately or not — parks
// its broadcast-follower goroutine on a blocked write forever. Generous:
// any healthy client drains a line in microseconds.
var logWriteTimeout = 10 * time.Second

// WithLogTee wires the live-log source. Only `vmr start` calls it; without
// it /log answers 503 (tests construct Servers with no tee — same pattern
// as WithInstance's zero-value instance).
func (s *Server) WithLogTee(tee *logtee.Tee) *Server {
	s.logTee = tee
	return s
}

// logPageAssembled is logHTMLPage with console.css/console.js injected once
// at server start (same pattern as other console pages using assembleConsolePage).
var logPageAssembled = sync.OnceValue(func() []byte {
	return assembleConsolePage(logHTMLPage)
})

// logPage serves the assembled live-log shell, unauthenticated: like
// status.html it contains zero business data. The embedded JS opens
// GET /log itself (which enforces s.auth) and prompts for a key on 401.
func (s *Server) logPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(logPageAssembled())
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

	// writeLine refreshes the per-write deadline (see logWriteTimeout) right
	// before each write — the connection sits in select between writes, so a
	// deadline set once would expire mid-wait and kill a healthy quiet tail.
	// A ResponseWriter that can't set a deadline (some test recorders) just
	// gets the pre-existing unbounded behavior.
	rc := http.NewResponseController(w)
	writeLine := func(s string) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(logWriteTimeout))
		_, err := io.WriteString(w, s)
		return err == nil
	}

	// Follow, not Recent+Subscribe: the atomic snapshot-and-register means a
	// line written while this connection opens lands in exactly one of
	// replay or live stream, never silently in neither.
	replay, ch, cancel := s.logTee.Follow()
	defer cancel()
	for _, line := range replay {
		if !writeLine(line + "\n") {
			return
		}
	}
	flusher.Flush()
	timer := time.NewTimer(logHeartbeat)
	defer timer.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case line := <-ch:
			if !writeLine(line + "\n") {
				return
			}
			flusher.Flush()
			timer.Reset(logHeartbeat)
		case <-timer.C:
			if !writeLine("\n") {
				return
			}
			flusher.Flush()
			timer.Reset(logHeartbeat)
		}
	}
}
