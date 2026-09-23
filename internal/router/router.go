// Ver 2026-09-23 04:04, by Claude Opus 5.5

// Package router holds the failover loop: health filter → multi-key sort →
// try candidates in order. This is the core of the project and should stay small.
package router

import (
	"context"
	"fmt"
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
	"vmr/internal/guard"
	"vmr/internal/health"
	"vmr/internal/quota"
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

	// Inflight is the per-request live registry behind /stats (the
	// LiveStats design doc). New() wires it; a Router built by struct literal leaves
	// it nil, and every registry method is nil-safe for that case — same
	// convention as Quota above.
	Inflight *InflightRegistry

	// Guard is Agent Guard's online engine, nil unless cmd_start.go's
	// setupGuard wires one up (same "cfg.Guard != nil at startup" gate
	// internal/server.Server.guard uses — see that field's doc comment).
	// forwardSuccess's inbound mount point is nil-safe: a nil Guard means
	// no guard.Inbound wrapping happens at all, byte-identical to this
	// field never having existed.
	Guard *guard.Guard

	// ProviderLimiters gates in-flight concurrency per provider account.
	ProviderLimiters *ProviderLimiterRegistry

	snap atomic.Pointer[Snapshot]

	// ctx is the root lifecycle context, written once at startup
	// (WithContext) and read from background probe goroutines — atomic so
	// -race stays clean regardless of when a caller sets it.
	ctx atomic.Pointer[context.Context]

	installMu sync.Mutex              // guards Install (see Install's doc comment)
	limiter   atomic.Pointer[limiter] // nil = unlimited
	inFlight  atomic.Int64
	waiting   atomic.Int64

	reloads reloadTracker // see reload.go: last hot-reload outcome, for /status
}

func New(logger *log.Logger) *Router {
	rt := &Router{
		Health:           health.New(),
		Sticky:           sticky.New(),
		Inflight:         NewInflightRegistry(),
		ProviderLimiters: NewProviderLimiterRegistry(),
		Logger:           logger,
	}
	rt.SetContext(context.Background())
	return rt
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
	rt.ctx.Store(&ctx)
}

// Context returns the router's root context, defaulting to context.Background().
func (rt *Router) Context() context.Context {
	if p := rt.ctx.Load(); p != nil {
		return *p
	}
	return context.Background()
}

// Serve routes one chat request through the failover loop. protocol is the
// ingress protocol ("openai-completions", "anthropic-messages", "openai-responses", ...); a model
// bound to a different protocol is rejected — VMR never converts between
// protocols. rec (nilable) collects the per-attempt audit trail.
//
// snap is the routing snapshot the caller already loaded once for the whole
// request: the server layer loads it at the request's entry point and
// passes the same instance through authentication, body handling, and
// routing, so a hot reload cannot tear one request across two views.
func (rt *Router) Serve(w http.ResponseWriter, r *http.Request, creq *core.CanonicalRequest, protocol string, snap *Snapshot, rec *audit.Record) {
	start := time.Now()
	// Stamped before any early return so the in-flight row carries the
	// estimate even for requests rejected on model lookup.
	if ifh := InflightHandleFrom(r.Context()); ifh != nil {
		ifh.SetEstIn(creq.Facts.EstimatedTokens)
	}
	if snap == nil {
		// Only reachable if a caller invokes Serve before Install ever ran -
		// the real cmd_start.go startup sequence always calls Install with
		// the first BuildSnapshot before the HTTP server starts listening,
		// so this never fires in production; the server layer also checks
		// nil before calling in. A defensive 503 here is strictly
		// better than the nil-pointer panic snap.Models would otherwise be.
		WriteError(w, http.StatusServiceUnavailable, "service_unavailable", "router not yet initialized")
		return
	}
	route, ok := snap.Models[protocol][creq.Model]
	if !ok {
		if other := otherProtocolFor(snap, protocol, creq.Model); other != "" {
			WriteError(w, http.StatusNotFound, "not_found_error",
				fmt.Sprintf("model %q speaks the %s protocol; call it via POST %s", creq.Model, other, IngressPath(other)))
			return
		}
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
	// Set once, up front: w.Header() is just a map until something calls
	// WriteHeader, and every path that does so (forwardSuccess,
	// handleErrorResponse, the all-failed branch below) runs after this.
	w.Header().Set("X-VMR-Route-Reason", cs.reason.String())

	// Failover walks the whole candidate sequence; max_attempts (>0)
	// optionally caps the walk to bound tail latency.
	attempts := 0
	var last *upstreamError
	var trail failoverTrail
	stickyEscaped := false
	for _, ep := range cs.endpoints {
		if r.Context().Err() != nil {
			break
		}
		if snap.Cfg.MaxAttempts > 0 && attempts >= snap.Cfg.MaxAttempts {
			break
		}
		// Acquire enforces the single-flight rule for half-open endpoints.
		// Candidates reaching this line are either healthy (fails==0: this is
		// a no-op true) or the health filter's last-resort release — a
		// half-open endpoint whose probe slot healthFilter just freed via
		// ReportNeutral, so Acquire re-claims the slot on behalf of THIS real
		// request, making the request itself the occupying probe. It still
		// guards the race against an endpoint turning half-open between the
		// filter and this loop (one that just failed is stopped by its fresh
		// cooldown instead).
		if !rt.Health.Acquire(ep.HealthKey(), time.Now()) {
			continue
		}
		isStickyHit := (cs.stickyEPKey != "" && ep.HealthKey() == cs.stickyEPKey)
		releaseSlot, waited, timedOut, ok := rt.acquireProviderSlot(r.Context(), ep, isStickyHit)
		if !ok {
			rt.Health.ReportNeutral(ep.HealthKey())
			if r.Context().Err() != nil {
				break
			}
			trail.addBusy(ep, timedOut, waited)
			if isStickyHit {
				stickyEscaped = true
			}
			continue
		}
		// Always reset, not just when waited > 0: an earlier candidate's wait
		// must not leak onto this candidate's attempt if this one didn't wait
		// (WithWait(0).String() omits conc_waited, matching that case).
		w.Header().Set("X-VMR-Route-Reason", cs.reason.WithWait(waited).String())
		attempts++
		trail.apply(w.Header()) // failures so far — the attempt about to run writes the headers itself if it succeeds
		ac := newAttemptCtx(rt, w, r, creq, ep, snap, attempts, start, rec)
		done, uerr, success := func() (bool, *upstreamError, bool) {
			if releaseSlot != nil {
				defer releaseSlot()
			}
			return rt.tryOne(ac)
		}()
		if done {
			if success && cs.stickyKey != "" {
				// Move sticky pointer only if we did not escape due to concurrency saturation.
				if !stickyEscaped {
					rt.Sticky.Set(cs.stickyKey, ep.HealthKey())
				}
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
	rt.handleAllFailed(w, r, creq, cs, last, attempts, start, rec, trail)
}

func (rt *Router) handleAllFailed(w http.ResponseWriter, r *http.Request, creq *core.CanonicalRequest, cs candidateSet, last *upstreamError, attempts int, start time.Time, rec *audit.Record, trail failoverTrail) {
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
		WriteError(w, http.StatusServiceUnavailable, "vmr_no_candidates", noCandidatesMessage(r.Context().Err(), creq, cs.reason, attempts, cs.healthOK, trail))
	}
	rt.logf("%s %s, %s, ALL_FAILED(%s, %dx)", clientTag(rec), creq.Model, estTokenField(creq), fmtDur(time.Since(start)), attempts)
}

// rejectIfAllKeyless (with its host-shape helper) lives in keyless.go.

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
func (rt *Router) tryOne(ac *attemptCtx) (done bool, uerr *upstreamError, success bool) {
	attemptStart := time.Now()
	// A half-open probe slot held by this attempt must always resolve via
	// exactly one of ReportSuccess/ReportFailure/ReportNeutral. Every normal
	// path reports (and marks healthReported); this defer is the panic
	// backstop — a handler panic that net/http recovers would otherwise
	// leave probing=true and lock the endpoint out until process restart.
	defer func() {
		if !ac.healthReported {
			rt.Health.ReportNeutral(ac.key)
		}
	}()
	if ac.rec != nil {
		ac.rec.Attempts = append(ac.rec.Attempts, audit.Attempt{
			Endpoint: core.EndpointLabel(ac.ep.AdapterType, ac.ep.Provider, ac.ep.Model),
			Protocol: ac.ep.AdapterType,
			Provider: ac.ep.Provider,
			Model:    ac.ep.Model,
		})
		ac.att = &ac.rec.Attempts[len(ac.rec.Attempts)-1]
		defer func() { ac.att.DurMS = time.Since(attemptStart).Milliseconds() }()
	}

	ad, ok := adapter.Get(ac.ep.AdapterType)
	if !ok { // validated at config load; defensive only
		ac.healthReported = true
		rt.Health.ReportFailure(ac.key, core.ErrTransient, 0, time.Now())
		return false, nil, false
	}

	req, outBody, err := ad.BuildRequest(ac.r.Context(), ac.ep, ac.creq)
	if err != nil {
		// A build failure is about vmr's own request construction (or the
		// client's malformed body), not the endpoint — same call runProbe
		// makes for this class of error (see probe.go). Must not cool the
		// endpoint down: a single malformed client request would otherwise
		// lock out every other client's traffic to it via a bogus transient
		// cooldown.
		ac.healthReported = true
		rt.Health.ReportNeutral(ac.key)
		rt.logf("%s, %s, error=build:%v, attempt=%d", ac.logPrefix, ac.tokenEst, err, ac.attempt)
		ac.att.SetBuildError(err)
		return false, nil, false
	}
	// outBody comes straight from BuildRequest (immutable by contract), so
	// the audit trail references it directly — no GetBody+ReadAll round trip
	// duplicating the whole body per attempt.
	ac.att.SetRequest(req.URL.String(), req.Header, outBody)
	// In-flight sent stamp (the LiveStats design doc), same point as the audit request
	// stamp: every attempt overwrites the previous one, so a request stuck
	// in failover shows the endpoint it is currently waiting on.
	ifh := InflightHandleFrom(ac.r.Context())
	ifh.stampSent(ac.attempt, ac.ep.Provider, ac.ep.Model, ac.ep.KeyLabel)

	resp, err := ac.snap.clientFor(ac.ep).Do(req)
	if err != nil {
		if ac.r.Context().Err() != nil {
			// Client went away; nothing to write, don't punish the endpoint.
			// But DO release a half-open probe slot if this attempt held one:
			// without this, a client canceling mid-probe leaves probing=true
			// forever and the endpoint is locked out until process restart.
			ac.healthReported = true
			rt.Health.ReportNeutral(ac.key)
			ac.att.SetCanceled()
			return true, nil, false
		}
		ac.healthReported = true
		cd := rt.Health.ReportFailure(ac.key, core.ErrTransient, 0, time.Now())
		rt.logf("%s, %s, error=network:%v, cooldown=%s, attempt=%d", ac.logPrefix, ac.tokenEst, err, cd, ac.attempt)
		ac.att.SetNetworkError(err)
		return false, nil, false
	}

	if resp.StatusCode >= 400 {
		return ac.handleErrorResponse(resp, ad)
	}
	return ac.forwardSuccess(resp)
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
// added. Every registered protocol has its own explicit case; an
// unregistered protocol returns "" — replay then rebuilds a visibly broken
// Client.Request.Path instead of silently misrouting it onto another
// protocol's path (a loud wrong answer beats a quiet right-looking one;
// a panic would be worse — replay must not die mid-report).
func IngressPath(protocol string) string {
	switch protocol {
	case core.ProtocolOpenAICompletions:
		return "/v1/chat/completions"
	case core.ProtocolAnthropicMessages:
		return "/v1/messages"
	case core.ProtocolOpenAIResponses:
		return "/v1/responses"
	default:
		return ""
	}
}
