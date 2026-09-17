// Ver 2026-09-17, by Sonnet 5

// Agent Guard's inbound mount point helpers, split out of forwardSuccess
// (router.go) purely to stay under archtest's file/function line budgets —
// no behavior split from that function's own reasoning, see its call
// sites' comments for the actual design rationale (ADR-15, M4). ADR-15
// removed the online Tool Call gate, circuit-breaker frames, and the
// non-streaming block path: guard.Inbound (and this file's non-streaming
// counterpart) never withholds a response or changes its status code
// anymore, only its bytes -- Fail-Open all the way through.
package router

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/guard"
	"vmr/internal/respnorm"
)

// wrapInboundGuard wraps istream (already inflight-stamped) with
// rt.Guard.Inbound for a streaming (SSE) response, re-framing per event and
// stripping invisible Unicode runes -- see guard.Inbound's own doc comment.
// Returns istream unchanged when guard isn't wired, guard: isn't declared
// in this snapshot, or this endpoint's provider is fully trusted (§4.3's
// inbound trust rule).
func wrapInboundGuard(rt *Router, snap *Snapshot, ep *core.Endpoint, opaque bool, istream io.Reader) io.Reader {
	if rt.Guard == nil || snap.Cfg.Guard == nil || endpointTrusted(snap.Cfg.Guard, ep) {
		return istream
	}
	return rt.Guard.Inbound(istream, guard.InboundOpts{
		Opaque:                 opaque,
		SanitizeInvisibleRunes: snap.Cfg.Guard.Inbound.SanitizeRunes(),
	})
}

// nonStreamGuardCap bounds how much of a non-SSE response body sanitization
// will buffer in memory before giving up -- mirrors respnorm's own
// unexported bufferedCap (not importable, so matched rather than reused):
// a non-streaming LLM completion is essentially never anywhere near this
// size in practice. Past the cap, sanitization is skipped and the
// remainder relayed as-is (ADR-15: Fail-Open, there is no "block" outcome
// to fall back to instead).
const nonStreamGuardCap = 8 << 20

// prepareNonStreamGuard buffers a non-SSE response (bounded) and runs
// Unicode-steganography sanitization directly on the assembled JSON bytes
// -- a non-streaming body is already one complete document, so unlike the
// SSE path it needs none of guard.Inbound's event re-framing. Returns the
// per-category rune counts alongside the reader/markers so the caller can
// stamp Record.Guard.SanitizedRunes exactly like the streaming path does
// (via istream's RuneCounts()) -- a *bytes.Reader has no such method, so
// this is the non-streaming path's own way of surfacing the same fact.
//
// io.ReadAll below has no timeout of its own -- unlike the streaming path,
// where copyFlush's own idle timer bounds every read, this buffering read
// runs before copyFlush ever sees the body, so an upstream that commits a
// 200 header then hangs after a few bytes would otherwise park this
// request's handler goroutine indefinitely. The
// watchdog reuses streamIdle (the same idle bound copyFlush would have
// enforced) and closes bodyCloser to force the stuck Read to return an
// error -- the same pattern handleErrorResponse (router.go) already uses
// for a >=400 response's error body, applied here for the identical reason.
func prepareNonStreamGuard(rt *Router, snap *Snapshot, ep *core.Endpoint, opaque bool, rbody io.Reader, bodyCloser io.Closer, streamIdle time.Duration) (io.Reader, []string, map[string]int) {
	if rt.Guard == nil || snap.Cfg.Guard == nil || endpointTrusted(snap.Cfg.Guard, ep) {
		return rbody, nil, nil
	}
	if opaque || !snap.Cfg.Guard.Inbound.SanitizeRunes() {
		return rbody, nil, nil
	}
	watchdog := time.AfterFunc(streamIdle, func() { bodyCloser.Close() })
	buffered, err := io.ReadAll(io.LimitReader(rbody, nonStreamGuardCap+1))
	watchdog.Stop()
	if len(buffered) > nonStreamGuardCap {
		// An upstream padding a non-streaming response past the cap just
		// means sanitization gives up on this one -- Fail-Open, not a
		// reason to hold the response up any further.
		return io.MultiReader(bytes.NewReader(buffered), rbody), []string{"guard_sanitize_hold_exceeded"}, nil
	}
	if err != nil {
		return io.MultiReader(bytes.NewReader(buffered), staticErrReader{err}), nil, nil
	}
	return sanitizeNonStreamSafe(buffered)
}

// sanitizeNonStreamSafe runs guard.SanitizeEventJSON over buffered with its
// own panic recovery that Fail-Opens to buffered itself -- the
// non-streaming counterpart to guard.sanitizeEventSafe (inbound.go), and
// for the same reason: by the time SanitizeEventJSON could panic here,
// rbody has already been fully drained into buffered by the io.ReadAll
// above, so relayResponseBody's own outer recover() (which falls back to
// istream = rbody) would hand copyFlush an already-exhausted reader --
// a 200 status line already committed to the client, followed by 0 bytes.
// Recovering here, with buffered still in scope, is what makes Fail-Open
// actually mean "relay the original response" instead of "relay nothing."
func sanitizeNonStreamSafe(buffered []byte) (out io.Reader, applied []string, counts map[string]int) {
	defer func() {
		if recover() != nil {
			out = bytes.NewReader(buffered)
			applied = []string{"guard_inbound_error"}
			counts = nil
		}
	}()
	sanitized, changed, c := guard.SanitizeEventJSON(buffered, nil)
	if !changed {
		return bytes.NewReader(buffered), nil, nil
	}
	return bytes.NewReader(sanitized), []string{"guard_runes_sanitized"}, c
}

// endpointTrusted reports whether this specific endpoint's provider is
// listed in guard.trusted_providers (spec §4.3's inbound rule).
func endpointTrusted(g *config.Guard, ep *core.Endpoint) bool {
	if g == nil || len(g.TrustedProviders) == 0 || ep == nil {
		return false
	}
	for _, p := range g.TrustedProviders {
		if p == ep.Provider {
			return true
		}
	}
	return false
}

// staticErrReader always fails with a fixed error — used to let copyFlush's
// existing TRUNCATED classification handle a genuine read failure that
// happened while prepareNonStreamGuard was buffering, without duplicating
// that classification logic here.
type staticErrReader struct{ err error }

func (r staticErrReader) Read(p []byte) (int, error) { return 0, r.err }

// relayResponseBody wraps the response body with the inbound sanitizer
// (the SSE and non-streaming shapes need different wrapping, see
// wrapInboundGuard/prepareNonStreamGuard's own doc comments), copies it to
// the client, and sets att's norm markers. Split out of forwardSuccess
// purely for archtest's line budgets. Unlike the pre-ADR-15 shape, there is
// no guard-triggered short circuit here at all: copyFlush always runs.
func relayResponseBody(rt *Router, snap *Snapshot, w http.ResponseWriter, r *http.Request, ep *core.Endpoint, att *audit.Attempt, rbody respnorm.NormalizerStream, bodyCloser io.Closer, isSSE bool, opaque bool, streamIdle time.Duration) (error, string) {
	var istream io.Reader
	var guardApplied []string
	var nonStreamRuneCounts map[string]int
	// R-12: guard construction runs before any response byte is committed
	// (the header is already written by forwardSuccess by this point,
	// ADR-15), so a panic here can still fail open (spec §4.9) — relay the
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
			istream = wrapInboundGuard(rt, snap, ep, opaque, inflightStamped(rbody, InflightHandleFrom(r.Context())))
		} else {
			istream, guardApplied, nonStreamRuneCounts = prepareNonStreamGuard(rt, snap, ep, opaque, inflightStamped(rbody, InflightHandleFrom(r.Context())), bodyCloser, streamIdle)
		}
	}()
	copyErr := copyFlush(r.Context(), w, istream, streamIdle)
	// R-5: expose the sanitizer's per-category rune counts on the Attempt
	// so the server layer's completion hook can stamp
	// Record.Guard.SanitizedRunes -- the streaming path's counts live on
	// the InboundStream, the non-streaming path's are returned directly.
	if rc, ok := istream.(interface{ RuneCounts() map[string]int }); ok {
		att.SetSanitizedRunes(rc.RuneCounts())
	} else if nonStreamRuneCounts != nil {
		att.SetSanitizedRunes(nonStreamRuneCounts)
	}
	// For the SSE path, Applied() is populated as the stream is drained.
	// Read the live stream's Applied() now that copyFlush has drained it.
	if gs, ok := istream.(guard.InboundStream); ok {
		guardApplied = gs.Applied()
	}
	att.SetNorm(append(rbody.Applied(), guardApplied...), rbody.RawPreStrip())

	status := "OK"
	if r.Context().Err() != nil {
		status = "CANCELED"
		att.SetCanceled()
	} else if isClientWriteError(copyErr) {
		// The client disconnected (or its connection died) mid-transfer: the
		// context cancellation can trail the write failure by a few
		// microseconds, so this branch catches what the ctx check above would
		// miss. Count it as a client-side cancel, not an upstream TRUNCATED —
		// the supplier didn't fail, its output just stopped being deliverable.
		status = "CANCELED"
		att.SetCanceled()
	} else if copyErr != nil {
		status = "TRUNCATED" // upstream died mid-stream; the response is already committed
		att.SetTruncated(copyErr)
	}
	return copyErr, status
}

// abortIfBrokenStream aborts the HTTP handler when the transfer terminated
// abnormally mid-stream (TRUNCATED) — the only broken-stream shape left
// once ADR-15 removed the guard-triggered SSE abort.
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
