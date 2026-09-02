// Ver 2026-08-02, by Sonnet 5

// Package router holds the failover loop: health filter → multi-key sort →
// try candidates in order. This is the core of the project and should stay small.
package router

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vmr/internal/adapter"
	"vmr/internal/audit"
	"vmr/internal/core"
	"vmr/internal/fmtutil"
	"vmr/internal/health"
	"vmr/internal/quota"
	"vmr/internal/respnorm"
	"vmr/internal/sticky"
	"vmr/internal/strategy"
)

type Router struct {
	Health *health.Registry
	Sticky *sticky.Registry
	Logger *log.Logger
	// Quota is nil unless cmd_start.go wires one up (see quota.NewRegistry) —
	// every call site touching it (chargeQuota, reorderByQuota) must be
	// nil-safe: a plain no-op, not a panic. New() deliberately leaves it
	// nil because the large majority of Router construction sites (tests,
	// vmr diagnose) have no config.yaml log_dir to persist quota state
	// into and no use for it.
	Quota *quota.Registry

	snap atomic.Pointer[Snapshot]

	ctx context.Context

	installMu sync.Mutex              // guards Install (see Install's doc comment)
	limiter   atomic.Pointer[limiter] // nil = unlimited
	inFlight  atomic.Int64
	waiting   atomic.Int64

	reloads reloadTracker // see reload.go: last hot-reload outcome, for /status

	Telemetry Telemetry // see telemetry.go: in-memory process-lifetime traffic counters
}

func New(logger *log.Logger) *Router {
	return &Router{Health: health.New(), Sticky: sticky.New(), Logger: logger, ctx: context.Background()}
}

// WithContext returns the router with the given root context set for graceful shutdown.
func (rt *Router) WithContext(ctx context.Context) *Router {
	rt.SetContext(ctx)
	return rt
}

// SetContext sets the router's root lifecycle context.
func (rt *Router) SetContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	rt.ctx = ctx
}

// Context returns the router's root context, defaulting to context.Background().
func (rt *Router) Context() context.Context {
	if rt.ctx != nil {
		return rt.ctx
	}
	return context.Background()
}

// Serve routes one chat request through the failover loop. protocol is the
// ingress protocol ("openai-completions", "anthropic-messages", "openai-responses", ...); a model
// bound to a different protocol is rejected — VMR never converts between
// protocols. rec (nilable) collects the per-attempt audit trail.
//
// Serve loads the routing snapshot itself and delegates to ServeWithSnap;
// the server layer uses ServeWithSnap directly with the snapshot it already
// loaded once for the whole request (Q14) so a hot reload cannot tear one
// request across two views.
func (rt *Router) Serve(w http.ResponseWriter, r *http.Request, creq *core.CanonicalRequest, protocol string, rec *audit.Record) {
	rt.ServeWithSnap(w, r, creq, protocol, rt.snap.Load(), rec)
}

// ServeWithSnap is Serve with an already-loaded snapshot: the caller (the
// server layer) loads the routing table once per request and passes the same
// instance through authentication, body handling, and routing (Q14).
func (rt *Router) ServeWithSnap(w http.ResponseWriter, r *http.Request, creq *core.CanonicalRequest, protocol string, snap *Snapshot, rec *audit.Record) {
	start := time.Now()
	if snap == nil {
		// Only reachable if a caller invokes Serve before Install ever ran -
		// the real cmd_start.go startup sequence always calls Install with
		// the first BuildSnapshot before the HTTP server starts listening,
		// so this never fires in production; the server layer also checks
		// nil before calling in. A defensive 503 here is strictly
		// better than the nil-pointer panic snap.Models would otherwise be.
		rt.Telemetry.RecordOutcome(false, false)
		WriteError(w, http.StatusServiceUnavailable, "service_unavailable", "router not yet initialized")
		return
	}
	route, ok := snap.Models[protocol][creq.Model]
	if !ok {
		if other := otherProtocolFor(snap, protocol, creq.Model); other != "" {
			rt.Telemetry.RecordOutcome(false, false)
			WriteError(w, http.StatusNotFound, "not_found_error",
				fmt.Sprintf("model %q speaks the %s protocol; call it via POST %s", creq.Model, other, IngressPath(other)))
			return
		}
		rt.Telemetry.RecordOutcome(false, false)
		WriteError(w, http.StatusNotFound, "not_found_error",
			fmt.Sprintf("model %q not found; models on this endpoint: %s",
				creq.Model, strings.Join(modelNames(snap, protocol), ", ")))
		return
	}

	if rt.rejectIfAllKeyless(w, creq, route.Endpoints) {
		return
	}

	now := time.Now()
	cs := rt.buildCandidates(snap, protocol, creq, route, r, now)
	w.Header().Set("X-VMR-Route-Reason", cs.reason.String())

	// Failover walks the whole candidate sequence	// max_attempts (>0) optionally caps the walk to bound tail latency.
	// Set once, up front: w.Header() is just a map until something calls
	// WriteHeader, and every path that does so (forwardSuccess,
	// handleErrorResponse, the all-failed branch below) runs after this.

	attempts := 0
	var last *upstreamError
	var trail failoverTrail
	for _, ep := range cs.endpoints {
		if snap.Cfg.MaxAttempts > 0 && attempts >= snap.Cfg.MaxAttempts {
			break
		}
		// Acquire enforces the single-flight probe for half-open endpoints.
		// Every candidate reaching this line was already Available() above
		// (fails==0, or the health-filter loop would have diverted it to a
		// background probe instead), so this is normally a no-op true — it
		// only matters as a race guard against an endpoint going half-open
		// in the gap between that filter and this loop.
		if !rt.Health.Acquire(ep.HealthKey(), time.Now()) {
			continue
		}
		attempts++
		trail.apply(w.Header()) // failures so far — the attempt about to run writes the headers itself if it succeeds
		done, uerr, success := rt.tryOne(w, r, creq, ep, snap, attempts, start, rec)
		if done {
			if success && cs.stickyKey != "" {
				// Every successful completion moves the pointer — including
				// a failover success — so it always follows wherever the
				// conversation's cache is actually warm.
				rt.Sticky.Set(cs.stickyKey, ep.HealthKey())
			}
			return
		}
		// Build/network failures return no HTTP response (uerr == nil).
		// Keep the last real upstream error instead of wiping it: "return
		// the last upstream error verbatim" means the last one that HAS a
		// status/headers/body to return.
		if uerr != nil {
			last = uerr
			trail.add(ep, uerr.status)
		} else {
			trail.add(ep, 0)
		}
	}
	trail.apply(w.Header()) // every candidate failed: the all-failed branch below writes the response

	// All candidates failed or none were available.
	if last != nil {
		// Returns the last upstream error verbatim (status and headers)
		// (Retry-After included) and body — so the client sees exactly
		// what a direct call would have shown.
		copyRespHeaders(w.Header(), last.header)
		w.Header().Set("X-VMR-Attempts", strconv.Itoa(attempts))
		w.WriteHeader(last.status)
		w.Write(last.body)
	} else {
		w.Header().Set("X-VMR-Attempts", strconv.Itoa(attempts))
		WriteError(w, http.StatusServiceUnavailable, "vmr_no_candidates", noCandidatesMessage(creq, cs.reason, attempts, cs.healthOK))
	}
	rt.Telemetry.RecordOutcome(false, r.Context().Err() != nil)
	rt.logf("%s %s, %s, ALL_FAILED(%s, %dx)", clientTag(rec), creq.Model, estTokenField(creq), fmtDur(time.Since(start)), attempts)
}

// rejectIfAllKeyless answers a request for a model whose every endpoint has
// an empty api_key — a forgotten or unset ${ENV_VAR} that loaded as valid
// YAML — with one clear vmr-side error instead of letting each attempt 401
// upstream (raw provider/CDN HTML to the client) and cool the endpoints down
// for 10min+. Checked against the full endpoint set, not the health-filtered
// one: an endpoint that already 401'd is in cooldown and would drop out,
// turning this into a vaguer "no candidates" message. Returns true when it
// handled the response.
func (rt *Router) rejectIfAllKeyless(w http.ResponseWriter, creq *core.CanonicalRequest, eps []*core.Endpoint) bool {
	if len(eps) == 0 {
		return false
	}
	for _, ep := range eps {
		if ep.APIKey != "" {
			return false
		}
	}
	rt.Telemetry.RecordOutcome(false, false)
	WriteError(w, http.StatusServiceUnavailable, "vmr_no_api_key", fmt.Sprintf(
		"all %d endpoint(s) for model %q have no api_key — set the provider api_key (or the ${ENV_VAR} it references) and reload",
		len(eps), creq.Model))
	return true
}

// findByHealthKey returns the endpoint in candidates whose HealthKey
// matches key, or nil if none does — e.g. a sticky pointer recorded before
// this turn's health/condition filtering ran, for an endpoint that's since
// become unhealthy or no longer meets a hard condition.
func findByHealthKey(candidates []*core.Endpoint, key string) *core.Endpoint {
	for _, ep := range candidates {
		if ep.HealthKey() == key {
			return ep
		}
	}
	return nil
}

// moveToFront reorders candidates in place so ep is tried first, preserving
// the relative order of everything else.
func moveToFront(candidates []*core.Endpoint, ep *core.Endpoint) {
	for i, e := range candidates {
		if e == ep {
			if i == 0 {
				return
			}
			copy(candidates[1:i+1], candidates[:i])
			candidates[0] = ep
			return
		}
	}
}

// rejectionSummary names every Condition that rejected at least one of
// endpoints, for the "no endpoint accepts this request" error message.
// Called only on that failure path, never on the hot path.
func rejectionSummary(endpoints []*core.Endpoint, facts core.RequestFacts) string {
	seen := map[string]bool{}
	var names []string
	for _, ep := range endpoints {
		for _, name := range strategy.RejectedBy(ep, facts) {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return "rejected by an unspecified condition"
	}
	sort.Strings(names)
	return "rejected by condition(s): " + strings.Join(names, ", ")
}

type upstreamError struct {
	status int
	header http.Header
	body   []byte
}

// respHeaderBlocklist is the set of upstream response headers VMR does NOT
// forward to the client: hop-by-hop headers (they describe the VMR↔upstream
// connection, not the client↔VMR one) plus Content-Length (normalization
// can change the body size; Go recomputes framing). Everything else —
// Retry-After, x-ratelimit-*, request IDs, Date, Content-Encoding — passes
// through so the client sees what a direct call would have shown.
var respHeaderBlocklist = map[string]struct{}{
	"connection":         {},
	"keep-alive":         {},
	"proxy-authenticate": {},
	"proxy-connection":   {},
	"te":                 {},
	"trailer":            {},
	"transfer-encoding":  {},
	"upgrade":            {},
	"content-length":     {},
}

// copyRespHeaders forwards upstream response headers minus the blocklist.
func copyRespHeaders(dst http.Header, src http.Header) {
	for k, vs := range src {
		if _, blocked := respHeaderBlocklist[strings.ToLower(k)]; blocked {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// errBodyCap bounds how much of an upstream >=400 response body tryOne reads
// into memory (and forwards to the client / audit trail). See the read site
// below for why 128KB.
const errBodyCap = 128 << 10

// tryOne sends the request to a single endpoint. It returns done=true when a
// response (success or non-retryable error) has been written to the client;
// success=true only for a genuine 2xx completion (never for a client-error
// passthrough or a canceled request, both of which are also done=true) —
// Serve uses it to decide whether to update the Sticky Model registry.
//
// att is nil-safe throughout (see audit.Attempt's Set* methods): when
// auditing is disabled, rec/att stay nil and every audit write below is a
// no-op instead of a guarded branch.
func (rt *Router) tryOne(w http.ResponseWriter, r *http.Request, creq *core.CanonicalRequest,
	ep *core.Endpoint, snap *Snapshot, attempt int, start time.Time, rec *audit.Record) (done bool, uerr *upstreamError, success bool) {

	key := ep.HealthKey()
	attemptStart := time.Now()
	// A half-open probe slot held by this attempt must always resolve via
	// exactly one of ReportSuccess/ReportFailure/ReportNeutral. Every normal
	// path reports (and marks healthReported); this defer is the panic
	// backstop — a handler panic that net/http recovers would otherwise
	// leave probing=true and lock the endpoint out until process restart.
	healthReported := false
	defer func() {
		if !healthReported {
			rt.Health.ReportNeutral(key)
		}
	}()
	// logPrefix carries the fields every log line in this attempt shares
	// (client tag, virtual model -> physical endpoint, capabilities) so
	// each call site below only spells out what actually differs about its
	// outcome. tokenEst is the pre-call estimate every error/failover tail
	// uses (none of them ever learns actual usage); forwardSuccess prefers
	// real usage over it once one comes back.
	logPrefix := attemptPrefix(rec, creq, ep)
	tokenEst := estTokenField(creq)
	var att *audit.Attempt
	if rec != nil {
		rec.Attempts = append(rec.Attempts, audit.Attempt{
			Endpoint: core.EndpointLabel(ep.AdapterType, ep.Provider, ep.Model),
			Protocol: ep.AdapterType,
			Provider: ep.Provider,
			Model:    ep.Model,
		})
		att = &rec.Attempts[len(rec.Attempts)-1]
		defer func() { att.DurMS = time.Since(attemptStart).Milliseconds() }()
	}

	ad, ok := adapter.Get(ep.AdapterType)
	if !ok { // validated at config load; defensive only
		healthReported = true
		rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now())
		return false, nil, false
	}

	req, outBody, err := ad.BuildRequest(r.Context(), ep, creq)
	if err != nil {
		// A build failure is about vmr's own request construction (or the
		// client's malformed body), not the endpoint — same call runProbe
		// makes for this class of error (see probe.go). Must not cool the
		// endpoint down: a single malformed client request would otherwise
		// lock out every other client's traffic to it via a bogus transient
		// cooldown.
		healthReported = true
		rt.Health.ReportNeutral(key)
		rt.logf("%s, %s, error=build:%v, attempt=%d", logPrefix, tokenEst, err, attempt)
		att.SetBuildError(err)
		return false, nil, false
	}
	// outBody comes straight from BuildRequest (immutable by contract), so
	// the audit trail references it directly — no GetBody+ReadAll round trip
	// duplicating the whole body per attempt.
	att.SetRequest(req.URL.String(), req.Header, outBody)

	resp, err := snap.clientFor(ep).Do(req)
	if err != nil {
		if r.Context().Err() != nil {
			// Client went away; nothing to write, don't punish the endpoint.
			// But DO release a half-open probe slot if this attempt held one:
			// without this, a client canceling mid-probe leaves probing=true
			// forever and the endpoint is locked out until process restart.
			healthReported = true
			rt.Health.ReportNeutral(key)
			att.SetCanceled()
			rt.Telemetry.RecordOutcome(false, true)
			return true, nil, false
		}
		healthReported = true
		cd := rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now())
		rt.logf("%s, %s, error=network:%v, cooldown=%s, attempt=%d", logPrefix, tokenEst, err, cd, attempt)
		att.SetNetworkError(err)
		return false, nil, false
	}

	if resp.StatusCode >= 400 {
		return rt.handleErrorResponse(w, resp, ad, att, logPrefix, tokenEst, snap, attempt, start, key, &healthReported)
	}
	if uerr, blocked := rt.checkSoftBlock(resp, creq, ep, att, logPrefix, tokenEst, snap, attempt, key, &healthReported); blocked {
		return false, uerr, false
	}
	return rt.forwardSuccess(w, r, resp, creq, ep, att, logPrefix, snap, attempt, start, key, &healthReported)
}

// handleErrorResponse reads, classifies, and records a >=400 upstream
// response. A content-policy flag, a context-window overflow, or a bad
// request (ErrClient) never cools the endpoint down — see the per-class
// comments below; everything else reports failure and lets the caller's
// failover loop move to the next candidate.
func (rt *Router) handleErrorResponse(w http.ResponseWriter, resp *http.Response, ad adapter.Adapter, att *audit.Attempt,
	logPrefix, tokenEst string, snap *Snapshot, attempt int, start time.Time, key string, healthReported *bool) (done bool, uerr *upstreamError, success bool) {

	// The error body is read with a deadline: ResponseHeaderTimeout only
	// covers the headers, and an upstream that stalls after sending error
	// headers would otherwise park this read — and with it the whole
	// failover walk — until the client gives up. Reuses stream_idle as
	// the bound; 128KB within that window is generous (most vendors stay
	// under 8KB; a few — e.g. Anthropic-shaped errors[] arrays — run
	// longer, hence the headroom over the read itself being cheap).
	watchdog := time.AfterFunc(snap.Cfg.Timeouts.StreamIdle.D(), func() { resp.Body.Close() })
	body, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyCap+1)) // +1 to detect truncation without reading past the cap
	watchdog.Stop()
	resp.Body.Close()
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
	att.SetErrorResponse(resp.Header, auditBody, resp.StatusCode, class)

	if class == core.ErrContent || class == core.ErrContextLimit || class == core.ErrQuirk {
		// Content-policy flag, context-window overflow, or a vendor-specific
		// protocol-constraint rejection: all facts about this particular
		// request (vendor sensitivity; this endpoint's model's window size;
		// the history shape THIS endpoint enforces), not evidence the
		// endpoint is unhealthy. Keep failing over — another candidate may
		// accept the content, have a larger window, or not enforce the quirk —
		// but leave the endpoint's health untouched; only release a probe
		// slot if held.
		rt.Health.ReportNeutral(key)
		*healthReported = true
		rt.logf("%s, %s, status=%d, class=%s, attempt=%d (no cooldown)", logPrefix, tokenEst, resp.StatusCode, class, attempt)
		return false, uerr, false
	}
	if class == core.ErrClient {
		// Bad request: every endpoint would fail the same way. Return as-is.
		// Says nothing about the endpoint's health — release a probe slot
		// if this attempt held one (same lockout hazard as client cancel).
		rt.Health.ReportNeutral(key)
		*healthReported = true
		copyRespHeaders(w.Header(), uerr.header)
		w.Header().Set("X-VMR-Attempts", strconv.Itoa(attempt))
		w.WriteHeader(uerr.status)
		w.Write(uerr.body)
		// "status/class=" rather than the two separate fields the cooldown
		// line below uses: class is always ErrClient on this branch (it's
		// how execution got here), so a second "class=client" field would
		// repeat information the branch itself already carries.
		rt.logf("%s, %s, status/class=%d(%s, %dx)", logPrefix, tokenEst, resp.StatusCode, fmtDur(time.Since(start)), attempt)
		rt.Telemetry.RecordOutcome(false, false)
		return true, nil, false
	}
	cd := rt.Health.ReportFailure(key, class, parseRetryAfter(resp.Header), time.Now())
	*healthReported = true
	rt.logf("%s, %s, status=%d, class=%s, cooldown=%s, attempt=%d", logPrefix, tokenEst, resp.StatusCode, class, cd, attempt)
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
func (rt *Router) forwardSuccess(w http.ResponseWriter, r *http.Request, resp *http.Response, creq *core.CanonicalRequest,
	ep *core.Endpoint, att *audit.Attempt, logPrefix string, snap *Snapshot, attempt int, start time.Time, key string, healthReported *bool) (done bool, uerr *upstreamError, success bool) {

	rt.Health.ReleaseProbe(key)
	// Body omitted: the client-facing response body is recorded by the
	// server layer, and this attempt records only the headers. It is not
	// byte-identical to the upstream's — model rewrite, [DONE] completion
	// and quirk repairs may have changed bytes — so any deviation is
	// traceable through the response normalizer's Norm markers, RawPreStrip
	// and ObservedModel.
	att.SetSuccessResponse(resp.StatusCode, resp.Header)
	body := resp.Body
	defer body.Close()

	// Forward the upstream's response headers (minus hop-by-hop) so the
	// client sees what a direct call would have shown — rate-limit
	// headers, request IDs, Date, Retry-After. VMR's own headers go on top.
	copyRespHeaders(w.Header(), resp.Header)
	w.Header().Set("X-VMR-Endpoint", ep.Name())
	w.Header().Set("X-VMR-Attempts", strconv.Itoa(attempt))
	w.WriteHeader(resp.StatusCode)

	// Wrap the upstream body with the response normalizer (internal/respnorm):
	// true streaming by default, buffered only when a known upstream quirk
	// shape is detected, raw passthrough when the body is compressed — see
	// that package's doc comment for what triggers buffering.
	ct := resp.Header.Get("Content-Type")
	isSSE := strings.Contains(ct, "text/event-stream") || (ct == "" && creq.Stream)
	opaque := resp.Header.Get("Content-Encoding") != ""
	rbody := respnorm.Wrap(body, respnorm.Options{
		ClientModel:   creq.Model,
		UpstreamModel: ep.Model,
		IsSSE:         isSSE,
		Protocol:      ep.AdapterType,
		Opaque:        opaque,
	})

	// Both SSE and non-SSE bodies go through copyFlush so the stream_idle
	// watchdog covers every upstream response body: a 200 whose body stalls
	// mid-transfer must abort instead of parking the request forever. The
	// per-chunk Flush is a no-op concern for JSON bodies — Content-Length is
	// stripped anyway.
	copyErr := copyFlush(r.Context(), w, rbody, snap.Cfg.Timeouts.StreamIdle.D())
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
		// The response is already committed (headers + partial body), so
		// there's nothing more to write; skip the ErrAbortHandler abort below
		// because the connection is already gone.
		status = "CANCELED"
		att.SetCanceled()
	} else if copyErr != nil {
		status = "TRUNCATED" // upstream died mid-stream; the response is already committed
		att.SetTruncated(copyErr)
	}
	rt.reportStreamOutcome(key, status)
	*healthReported = true
	// Charged here regardless of copyErr — a truncated response still
	// consumed whatever tokens actually reached the client (see
	// chargeQuota's doc comment); nil-safe when no quota.Registry is wired
	// up or this endpoint carries no quota: config.
	rt.chargeQuota(ep, rbody, creq, time.Now())
	att.SetNorm(rbody.Applied(), rbody.RawPreStrip())
	att.SetUpstreamModel(rbody.ObservedModel())
	// rbody.Usage() is safe to read here even on copyFlush's early-return
	// paths (idle timeout, canceled, write error), where the reader
	// goroutine may still be mid-trailing-read: NormalizerStream's
	// inspection methods are mutex-guarded internally (see the mu field on
	// respnorm's stream type), not merely "stable once copyFlush returns".
	usage, ok := rbody.Usage()
	if ok {
		rt.Telemetry.RecordTokens(usage.Fresh(), usage.CacheWrite, usage.CacheRead, usage.Reasoning, usage.Out)
	}
	// TRUNCATED counts as error here while the audit record's top-level
	// outcome says ok (see audit.OutcomeFor) — deliberate split, see
	// Telemetry.RecordOutcome's doc comment.
	rt.Telemetry.RecordOutcome(copyErr == nil && status != "CANCELED", status == "CANCELED")
	rt.logf("%s, %s, %s(%s, %dx)", logPrefix, usageTokenField(usage, ok, creq), status, fmtDur(time.Since(start)), attempt)
	if status == "TRUNCATED" {
		// The upstream body died mid-stream after we already committed a
		// 200 + headers. respnorm flushed whatever it was safe to deliver
		// into the client response above (see flushRawOnError); abort the
		// connection now (net/http recovers ErrAbortHandler silently,
		// dropping the terminating chunk) so the client SDK sees a broken
		// transfer instead of a clean empty/partial success. All
		// bookkeeping above — quota charge, audit norm/usage, telemetry,
		// the log line — has already run; server.chatHandler's deferred
		// audit write still fires during the unwind.
		panic(http.ErrAbortHandler)
	}
	return true, nil, true
}

// reportStreamOutcome turns a finished (or aborted) stream's outcome into
// the health verdict forwardSuccess deferred until the stream ended: a full
// response is a real success; a mid-stream cut is transient-failure
// evidence even though the 200 was already committed and failover can no
// longer react to it; a client-side cancel says nothing about the endpoint
// and must not deepen the backoff.
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

func parseRetryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func modelNames(s *Snapshot, protocol string) []string {
	return fmtutil.SortedKeys(s.Models[protocol])
}

// otherProtocolFor reports which protocol group (other than protocol) defines
// name, or "" if none does. Used to give a helpful "wrong entry point" 404
// instead of a bare "not found" when the client hit the wrong ingress path.
func otherProtocolFor(s *Snapshot, protocol, name string) string {
	for p, byName := range s.Models {
		if p == protocol {
			continue
		}
		if _, ok := byName[name]; ok {
			return p
		}
	}
	return ""
}

// IngressPath is the vmr entry point for protocol — what a live client
// actually POSTs to. Exported so every consumer that needs to name a
// protocol's ingress route (this package's own 404 redirect message, and
// internal/replay's reconstructed Client.Request.Path) shares one mapping
// instead of each keeping its own copy that could drift as protocols are
// added. Falls back to the Chat Completions path for any protocol string
// this doesn't recognize (including "openai-completions" itself) — a third+
// protocol that reuses this default without adding a case here would
// misroute, so every registered protocol must have its own explicit case,
// not rely on falling through.
func IngressPath(protocol string) string {
	switch protocol {
	case core.ProtocolAnthropicMessages:
		return "/v1/messages"
	case core.ProtocolOpenAIResponses:
		return "/v1/responses"
	default:
		return "/v1/chat/completions"
	}
}
