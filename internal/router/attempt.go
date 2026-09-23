// Ver 2026-09-23 03:30, by Claude Opus 5.5

package router

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vmr/internal/adapter"
	"vmr/internal/audit"
	"vmr/internal/core"
	"vmr/internal/respnorm"
)

// attemptCtx carries the per-attempt context across tryOne, handleErrorResponse,
// forwardSuccess, and relayResponseBody, replacing flat parameter lists.
type attemptCtx struct {
	rt             *Router
	w              http.ResponseWriter
	r              *http.Request
	creq           *core.CanonicalRequest
	ep             *core.Endpoint
	snap           *Snapshot
	attempt        int
	start          time.Time
	rec            *audit.Record
	att            *audit.Attempt
	key            string
	logPrefix      string
	tokenEst       string
	healthReported bool
}

func newAttemptCtx(rt *Router, w http.ResponseWriter, r *http.Request, creq *core.CanonicalRequest,
	ep *core.Endpoint, snap *Snapshot, attempt int, start time.Time, rec *audit.Record) *attemptCtx {
	return &attemptCtx{
		rt:        rt,
		w:         w,
		r:         r,
		creq:      creq,
		ep:        ep,
		snap:      snap,
		attempt:   attempt,
		start:     start,
		rec:       rec,
		key:       ep.HealthKey(),
		logPrefix: attemptPrefix(rec, creq, ep),
		tokenEst:  estTokenField(creq),
	}
}

// handleErrorResponse reads, classifies, and records a >=400 upstream
// response. A content-policy flag, a context-window overflow, or a bad
// request (ErrClient) never cools the endpoint down — see the per-class
// comments below; everything else reports failure and lets the caller's
// failover loop move to the next candidate.
func (ac *attemptCtx) handleErrorResponse(resp *http.Response, ad adapter.Adapter) (done bool, uerr *upstreamError, success bool) {
	// The error body is read with a deadline: ResponseHeaderTimeout only
	// covers the headers, and an upstream that stalls after sending error
	// headers would otherwise park this read — and with it the whole
	// failover walk — until the client gives up. Reuses stream_idle as
	// the bound; 128KB within that window is generous (most vendors stay
	// under 8KB; a few — e.g. Anthropic-shaped errors[] arrays — run
	// longer, hence the headroom over the read itself being cheap).
	watchdog := time.AfterFunc(ac.snap.Cfg.Timeouts.StreamIdle.D(), func() { resp.Body.Close() })
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, errBodyCap+1)) // +1 to detect truncation without reading past the cap
	watchdog.Stop()
	resp.Body.Close()
	if readErr != nil {
		// Upstream stalled after the error headers and the watchdog (or a
		// mid-body break) killed the read. Whatever came back is a fragment,
		// not a classifiable error body — an empty/garbled one would classify
		// as ErrClient and be returned to the client as-is, when really this
		// attempt never delivered a verdict. Treat as a transport failure and
		// keep failing over.
		cd := ac.rt.Health.ReportFailure(ac.key, core.ErrTransient, 0, time.Now())
		ac.healthReported = true
		ac.rt.logf("%s, %s, error=network:%v, cooldown=%s, attempt=%d", ac.logPrefix, ac.tokenEst, readErr, cd, ac.attempt)
		ac.att.SetNetworkError(readErr)
		return false, nil, false
	}
	truncated := len(body) > errBodyCap
	if truncated {
		body = body[:errBodyCap]
	}
	class := ad.ClassifyError(resp.StatusCode, body)
	// uerr.body is forwarded to the client verbatim (byte-faithful) —
	// any truncation marker must go only into the audit copy below, never
	// into this slice.
	uerr = &upstreamError{resp.StatusCode, resp.Header, body}
	auditBody := body
	if truncated {
		// A fresh slice, never touched again — safe to hand to EncodeBody
		// under its "referenced, not cloned" ownership contract (audit.go).
		// Reports the cap, not upstream's true size: the LimitReader above
		// deliberately never reads past it, so the real total is unknown.
		auditBody = append(append([]byte(nil), body...), []byte(fmt.Sprintf("\n...(truncated at %d bytes)", errBodyCap))...)
	}
	ac.att.SetErrorResponse(resp.Header, auditBody, resp.StatusCode, class)

	if class == core.ErrContent || class == core.ErrContextLimit || class == core.ErrQuirk {
		// Content-policy flag, context-window overflow, or a vendor-specific
		// protocol-constraint rejection: all facts about this particular
		// request (vendor sensitivity; this endpoint's model's window size;
		// the history shape THIS endpoint enforces), not evidence the
		// endpoint is unhealthy. Keep failing over — another candidate may
		// accept the content, have a larger window, or not enforce the quirk —
		// but leave the endpoint's health untouched; only release a probe
		// slot if held.
		ac.rt.Health.ReportNeutral(ac.key)
		ac.healthReported = true
		ac.rt.logf("%s, %s, status=%d, class=%s, attempt=%d (no cooldown)", ac.logPrefix, ac.tokenEst, resp.StatusCode, class, ac.attempt)
		return false, uerr, false
	}
	if class == core.ErrClient {
		// Bad request: every endpoint would fail the same way. Return as-is.
		// Says nothing about the endpoint's health — release a probe slot
		// if this attempt held one (same lockout hazard as client cancel).
		ac.rt.Health.ReportNeutral(ac.key)
		ac.healthReported = true
		copyRespHeaders(ac.w.Header(), uerr.header)
		ac.w.Header().Set("X-VMR-Attempts", strconv.Itoa(ac.attempt))
		ac.w.WriteHeader(uerr.status)
		ac.w.Write(uerr.body)
		// "status/class=" rather than the two separate fields the cooldown
		// line below uses: class is always ErrClient on this branch (it's
		// how execution got here), so a second "class=client" field would
		// repeat information the branch itself already carries.
		ac.rt.logf("%s, %s, status/class=%d(%s, %dx)", ac.logPrefix, ac.tokenEst, resp.StatusCode, fmtDur(time.Since(ac.start)), ac.attempt)
		return true, nil, false
	}
	cd := ac.rt.Health.ReportFailure(ac.key, class, parseRetryAfter(resp.Header), time.Now())
	ac.healthReported = true
	ac.rt.logf("%s, %s, status=%d, class=%s, cooldown=%s, attempt=%d", ac.logPrefix, ac.tokenEst, resp.StatusCode, class, cd, ac.attempt)
	return false, uerr, false
}

// forwardSuccess streams a 2xx upstream response to the client through the
// response normalizer. From the first byte written the response is
// committed — no failover past this point, so this always returns
// done=true, success=true.
//
// The half-open probe slot is released up front (ReleaseProbe), and the
// health verdict itself waits until the stream's true outcome is known
// (reportStreamOutcome): a 200 header followed by a mid-stream cut is the
// relay layer's most common failure shape, and reporting success before the
// first byte made it invisible to the health state machine entirely.
func (ac *attemptCtx) forwardSuccess(resp *http.Response) (done bool, uerr *upstreamError, success bool) {
	ac.rt.Health.ReleaseProbe(ac.key)
	// Body omitted: the client-facing response body is recorded by the
	// server layer, and this attempt records only the headers. It is not
	// byte-identical to the upstream's — model rewrite, [DONE] completion
	// and quirk repairs may have changed bytes — so any deviation is
	// traceable through the response normalizer's Norm markers, RawPreStrip
	// and ObservedModel.
	ac.att.SetSuccessResponse(resp.StatusCode, resp.Header)
	// SetForwarded is the single point that flips Attempt.Forwarded on. Every
	// other path (handleErrorResponse, SetBuildError, SetNetworkError,
	// SetCanceled) leaves it false; a later SetTruncated
	// (mid-stream cut AFTER the 200 was committed) does NOT undo it, so the
	// field is the exact reproduction basis for the router's per-forwarded-
	// attempt quota charging. See Attempt.Forwarded's own doc comment.
	ac.att.SetForwarded()
	body := resp.Body
	defer body.Close()

	// Forward the upstream's response headers (minus hop-by-hop) so the
	// client sees what a direct call would have shown — rate-limit
	// headers, request IDs, Date, Retry-After. VMR's own headers go on top.
	copyRespHeaders(ac.w.Header(), resp.Header)
	ac.w.Header().Set("X-VMR-Endpoint", ac.ep.Name())
	ac.w.Header().Set("X-VMR-Attempts", strconv.Itoa(ac.attempt))

	// isSSE/opaque decide respnorm's transport mode. Agent Guard's inbound
	// side never blocks or changes the status code, so there is no reason
	// to delay WriteHeader for either
	// response shape — it is always written immediately.
	ct := resp.Header.Get("Content-Type")
	isSSE := strings.Contains(ct, "text/event-stream") || (ct == "" && ac.creq.Stream)
	opaque := resp.Header.Get("Content-Encoding") != ""
	ac.w.WriteHeader(resp.StatusCode)

	// Wrap the upstream body with the response normalizer (internal/respnorm):
	// true streaming by default, buffered only when a known upstream quirk
	// shape is detected, raw passthrough when the body is compressed — see
	// that package's doc comment for what triggers buffering.
	rbody := respnorm.Wrap(body, respnorm.Options{
		ClientModel:   ac.creq.Model,
		UpstreamModel: ac.ep.Model,
		IsSSE:         isSSE,
		Protocol:      ac.ep.AdapterType,
		Opaque:        opaque,
	})

	_, status := ac.relayResponseBody(rbody, body, isSSE, opaque)
	ac.rt.reportStreamOutcome(ac.key, status)
	ac.healthReported = true
	// One read of the stream's token state, shared by the ledger and the
	// audit stamp. Reading twice would let them disagree: on copyFlush's
	// early-return paths (idle timeout, client write error, cancel) the
	// reader goroutine can still ingest one more chunk before it exits, and
	// an SSE stream's usage event usually rides that last chunk — so one
	// side could bill a degraded estimate while the other stamped exact
	// usage, and estimated_pct would contradict its own evidence.
	rawTokens, estimated, _, _ := tokenCharge(rbody, ac.creq)
	// Charged regardless of copyErr — a truncated response still consumed
	// whatever tokens reached the client (see chargeQuota's doc comment);
	// nil-safe when no quota.Registry is wired up or this endpoint carries
	// no quota: config.
	ac.rt.chargeQuota(ac.ep, rawTokens, estimated, time.Now())
	ac.att.SetTokens(tokenStamp(rawTokens))
	ac.att.SetKeyLabel(ac.ep.KeyLabel)
	ac.att.SetUpstreamModel(rbody.ObservedModel())
	usage, ok := rbody.Usage()
	ac.rt.logf("%s, %s, %s(%s, %dx)", ac.logPrefix, usageTokenField(usage, ok, ac.creq), status, fmtDur(time.Since(ac.start)), ac.attempt)
	abortIfBrokenStream(status)
	return true, nil, true
}
