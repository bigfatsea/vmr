// Ver 2026-09-23 03:30, by Claude Opus 5.5

package router

import (
	"io"
	"net/http"
	"time"

	"vmr/internal/core"
	"vmr/internal/guard"
	"vmr/internal/respnorm"
)

// staticErrReader always fails with a fixed error — used to let copyFlush's
// existing TRUNCATED classification handle a genuine read failure that
// happened while prepareNonStreamGuard was buffering, without duplicating
// that classification logic here.
type staticErrReader struct{ err error }

func (r staticErrReader) Read(p []byte) (int, error) { return 0, r.err }

// relayResponseBody wraps the response body with the inbound sanitizer
// (the SSE and non-streaming shapes need different wrapping, see
// wrapInboundGuard/prepareNonStreamGuard's own doc comments), copies it to
// the client, and sets att's norm markers.
func (ac *attemptCtx) relayResponseBody(rbody respnorm.NormalizerStream, bodyCloser io.Closer, isSSE, opaque bool) (error, string) {
	var istream io.Reader
	var guardApplied []string
	var nonStreamRuneCounts map[string]int
	streamIdle := ac.snap.Cfg.Timeouts.StreamIdle.D()
	// Guard construction runs before any response byte is committed
	// (the header is already written by forwardSuccess by this point),
	// so a panic here can still fail open — relay the
	// untouched stream and leave a guard_inbound_error trace.
	func() {
		defer func() {
			if recover() != nil {
				istream = rbody
				guardApplied = []string{"guard_inbound_error"}
				nonStreamRuneCounts = nil
			}
		}()
		if isSSE {
			istream = wrapInboundGuard(ac.rt, ac.snap, ac.ep, opaque, inflightStamped(rbody, InflightHandleFrom(ac.r.Context())))
		} else {
			istream, guardApplied, nonStreamRuneCounts = prepareNonStreamGuard(ac.rt, ac.snap, ac.ep, opaque, inflightStamped(rbody, InflightHandleFrom(ac.r.Context())), bodyCloser, streamIdle)
		}
	}()
	copyErr := copyFlush(ac.r.Context(), ac.w, istream, streamIdle)
	// Expose the sanitizer's per-category rune counts on the Attempt
	// so the server layer's completion hook can stamp
	// Record.Guard.SanitizedRunes -- the streaming path's counts live on
	// the InboundStream, the non-streaming path's are returned directly.
	if rc, ok := istream.(interface{ RuneCounts() map[string]int }); ok {
		ac.att.SetSanitizedRunes(rc.RuneCounts())
	} else if nonStreamRuneCounts != nil {
		ac.att.SetSanitizedRunes(nonStreamRuneCounts)
	}
	// For the SSE path, Applied() is populated as the stream is drained.
	// Read the live stream's Applied() now that copyFlush has drained it.
	if gs, ok := istream.(guard.InboundStream); ok {
		guardApplied = gs.Applied()
	}
	ac.att.SetNorm(append(rbody.Applied(), guardApplied...), rbody.RawPreStrip())

	status := "OK"
	if ac.r.Context().Err() != nil {
		status = "CANCELED"
		ac.att.SetCanceled()
	} else if isClientWriteError(copyErr) {
		// The client disconnected (or its connection died) mid-transfer: the
		// context cancellation can trail the write failure by a few
		// microseconds, so this branch catches what the ctx check above would
		// miss. Count it as a client-side cancel, not an upstream TRUNCATED —
		// the supplier didn't fail, its output just stopped being deliverable.
		status = "CANCELED"
		ac.att.SetCanceled()
	} else if copyErr != nil {
		status = "TRUNCATED" // upstream died mid-stream; the response is already committed
		ac.att.SetTruncated(copyErr)
	}
	return copyErr, status
}

// abortIfBrokenStream aborts the HTTP handler when the transfer terminated
// abnormally mid-stream (TRUNCATED) — the only broken-stream shape left
// (the guard-triggered SSE abort no longer exists).
func abortIfBrokenStream(status string) {
	if status == "TRUNCATED" {
		panic(http.ErrAbortHandler)
	}
}

// reportStreamOutcome turns a finished (or aborted) stream's outcome into
// the health verdict forwardSuccess deferred until the stream ended: a
// full response is a real success; a mid-stream cut is transient-failure
// evidence even though the 200 was already committed and failover can no
// longer react to it; a client-side cancel says nothing about the
// endpoint and must not deepen the backoff.
func (rt *Router) reportStreamOutcome(key, status string) {
	switch status {
	case "OK":
		rt.Health.ReportSuccess(key)
	case "TRUNCATED":
		rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now())
	default: // CANCELED
		rt.Health.ReportNeutral(key)
	}
}
