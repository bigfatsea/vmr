// Ver 2026-09-23 08:10, by Claude Opus 5.5

// Agent Guard's inbound mount point helpers:
// wrapInboundGuard for SSE event re-framing and Unicode-steganography rune sanitization,
// and prepareNonStreamGuard for non-streaming response bodies. Fail-Open all the way through.
package router

import (
	"bytes"
	"io"
	"time"

	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/guard"
)

// wrapInboundGuard wraps istream (already inflight-stamped) with
// rt.Guard.Inbound for a streaming (SSE) response, re-framing per event and
// stripping invisible Unicode runes -- see guard.Inbound's own doc comment.
// Returns istream unchanged when guard isn't wired, guard: isn't declared
// in this snapshot, or this endpoint's provider is fully trusted (the
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
// remainder relayed as-is (Fail-Open: there is no "block" outcome
// to fall back to instead).
const nonStreamGuardCap = 8 << 20

// idleResetReader wraps an io.Reader and resets timer to idle whenever Read
// returns n > 0 bytes, so the watchdog bounds idle gaps between reads rather
// than total transfer duration.
type idleResetReader struct {
	r     io.Reader
	timer *time.Timer
	idle  time.Duration
}

func (ir *idleResetReader) Read(p []byte) (int, error) {
	n, err := ir.r.Read(p)
	if (n > 0 || (n == 0 && err == nil)) && ir.timer != nil {
		ir.timer.Reset(ir.idle)
	}
	return n, err
}

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
// 200 header then hangs would otherwise park this request's handler goroutine
// indefinitely. An idleResetReader resets the watchdog whenever n > 0 bytes
// are read, ensuring the watchdog bounds idle gaps between reads rather than
// total transfer duration (the same idle bound copyFlush enforces). If the
// idle timeout expires, bodyCloser is closed to force the stuck Read to
// return an error -- the same close-to-unblock mechanism handleErrorResponse
// uses for a >=400 error body, though that one is a fixed deadline: an error
// body is small and bounded, a success body is not.
func prepareNonStreamGuard(rt *Router, snap *Snapshot, ep *core.Endpoint, opaque bool, rbody io.Reader, bodyCloser io.Closer, streamIdle time.Duration) (io.Reader, []string, map[string]int) {
	if rt.Guard == nil || snap.Cfg.Guard == nil || endpointTrusted(snap.Cfg.Guard, ep) {
		return rbody, nil, nil
	}
	if opaque || !snap.Cfg.Guard.Inbound.SanitizeRunes() {
		return rbody, nil, nil
	}
	var watchdog *time.Timer
	var reader io.Reader = rbody
	if streamIdle > 0 && bodyCloser != nil {
		watchdog = time.AfterFunc(streamIdle, func() { bodyCloser.Close() })
		reader = &idleResetReader{r: rbody, timer: watchdog, idle: streamIdle}
	}
	buffered, err := io.ReadAll(io.LimitReader(reader, nonStreamGuardCap+1))
	if watchdog != nil {
		watchdog.Stop()
	}
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
// listed in guard.trusted_providers.
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
