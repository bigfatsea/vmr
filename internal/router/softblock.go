// Ver 2026-08-28, by Sonnet 5

// Soft-block failover (config.EndpointGroup.SoftBlockFailover, opt-in): a
// 2xx response that carries a vendor content-policy flag but no real answer
// — the "soft block" respnorm has always only *observed* — becomes a
// failover-worthy ErrContent when the endpoint opted in, instead of reaching
// an unattended agent as a clean empty 200 it silently continues from.
package router

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"vmr/internal/adapter"
	"vmr/internal/audit"
	"vmr/internal/core"
	"vmr/internal/respnorm"
)

// softBlockPeekCap bounds how much of a soft-block-eligible 2xx body
// checkSoftBlock reads before giving up: a content-policy block with an
// empty/canned answer is always a small body, so anything past this is
// definitely a real response — stop, and stream the rest normally.
const softBlockPeekCap = 64 << 10

// softBlockMaxTextRunes is the assistant-text length at or below which a
// content-flagged response counts as "no real answer". Generous on purpose:
// the vendor marker already makes a false positive near-impossible, so this
// only needs to clear empty strings and short canned refusals.
const softBlockMaxTextRunes = 64

// checkSoftBlock implements the opt-in described in this file's package
// comment. Only non-SSE, non-compressed bodies are eligible — a streaming
// response is already being forwarded event-by-event by the time a verdict
// is possible. blocked=true means the caller should treat this as a
// zero-cooldown content rejection (fail over, no health penalty);
// otherwise resp.Body has been restored to a re-readable reader and
// forwardSuccess proceeds unchanged.
func (rt *Router) checkSoftBlock(resp *http.Response, creq *core.CanonicalRequest, ep *core.Endpoint, att *audit.Attempt,
	logPrefix, tokenEst string, snap *Snapshot, attempt int, key string, healthReported *bool) (uerr *upstreamError, blocked bool) {

	ct := resp.Header.Get("Content-Type")
	isSSE := strings.Contains(ct, "text/event-stream") || (ct == "" && creq.Stream)
	if !ep.SoftBlockFailover || isSSE || resp.Header.Get("Content-Encoding") != "" {
		return nil, false
	}

	watchdog := time.AfterFunc(snap.Cfg.Timeouts.StreamIdle.D(), func() { resp.Body.Close() })
	peek, readErr := io.ReadAll(io.LimitReader(resp.Body, softBlockPeekCap+1))
	watchdog.Stop()
	if readErr != nil {
		// Upstream broke mid-peek, or the stream_idle watchdog tripped. The
		// bytes so far are a fragment, not a small complete body — never hand
		// forwardSuccess a fragment as a finished response (that is exactly
		// B1's silent-truncation failure, and this opt-in path must not
		// reintroduce it). Give back a body that replays the fragment and
		// then surfaces the error, so forwardSuccess's copyFlush aborts the
		// client connection (http.ErrAbortHandler) the same way a mid-stream
		// break on the normal path does.
		resp.Body = readCloser{io.MultiReader(bytes.NewReader(peek), errReader{readErr}), resp.Body}
		return nil, false
	}
	if len(peek) > softBlockPeekCap {
		// Too large to be an empty block — hand forwardSuccess a body that
		// replays what we consumed, then the rest of the live stream.
		resp.Body = readCloser{io.MultiReader(bytes.NewReader(peek), resp.Body), resp.Body}
		return nil, false
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(peek))

	if !respnorm.ContainsSoftBlockMarker(peek) {
		return nil, false
	}
	textRunes, hasToolCall, ok := adapter.ResponseAssistantText(ep.AdapterType, peek)
	if !ok || hasToolCall || textRunes > softBlockMaxTextRunes {
		return nil, false
	}
	// Same treatment as handleErrorResponse's ErrContent branch: fail over,
	// no health penalty, only release a probe slot if held.
	rt.Health.ReportNeutral(key)
	*healthReported = true
	att.SetErrorResponse(resp.Header, peek, resp.StatusCode, core.ErrContent)
	rt.logf("%s, %s, status=%d, class=soft_block, attempt=%d (no cooldown)", logPrefix, tokenEst, resp.StatusCode, attempt)
	return &upstreamError{resp.StatusCode, resp.Header, peek}, true
}

// readCloser splices a Reader onto a separate Closer — used to rebuild an
// http.Response body from bytes already peeked plus the untouched tail.
type readCloser struct {
	io.Reader
	io.Closer
}

// errReader yields a fixed error on every Read. Spliced after the peeked
// bytes when the peek itself failed, so a broken or timed-out soft-block
// probe reaches forwardSuccess as a truncated transfer, never a clean
// short body.
type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }
