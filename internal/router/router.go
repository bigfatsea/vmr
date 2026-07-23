// Ver 2026-07-24 12:00, by Sonnet 5

// Package router holds the failover loop: health filter → multi-key sort →
// try candidates in order. This is the core of the project and should stay small.
package router

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"vmr/internal/adapter"
	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/health"
	"vmr/internal/sticky"
	"vmr/internal/strategy"
)

// ModelRoute is the runtime routing table entry for one virtual model. It
// carries no protocol field: a route only ever exists inside
// Snapshot.Models[protocol], so the protocol is positional, not stored data —
// there is no "protocol" value here that could disagree with where the route
// lives.
type ModelRoute struct {
	Dims      []strategy.Dimension
	Endpoints []*core.Endpoint

	// ImageDownscaleMaxPx mirrors config.ModelConfig.ImageDownscaleMaxPx: nil
	// = this model has no override and inherits the global image_downscale;
	// non-nil (including a pointer to 0) = this model's explicit setting,
	// which always wins over the global one (§7 image downscale).
	ImageDownscaleMaxPx *int

	// Sticky mirrors config.ModelConfig.Sticky, resolved at BuildSnapshot
	// time: nil (field absent in config) defaults to true, so Sticky Model
	// affinity applies unless a virtual model explicitly opts out. See
	// docs/VirtualModelRouter_System_Design_v3.md §6.5.
	Sticky bool
}

// EffectiveOrder returns route's endpoints in the order they would actually
// be tried — health ignored, a static preview only — by copying and running
// the same strategy.Sort every real request goes through (see Serve).
// Shared by every command that previews routing (vmr start's startup log,
// vmr check, vmr diagnose) so they can't silently disagree about try-order
// for the same config: each held its own copy of "append then sort" before
// this was factored out.
func (r *ModelRoute) EffectiveOrder() []*core.Endpoint {
	ordered := append([]*core.Endpoint(nil), r.Endpoints...)
	strategy.Sort(ordered, r.Dims)
	return ordered
}

// EffectiveImageDownscaleMaxPx resolves the image-downscale cap that
// actually applies to this model: its own override if set (even 0, which
// force-disables downscaling for this model regardless of the global
// setting), else globalMaxPx. Safe to call on a nil receiver (an unknown
// model whose route lookup failed) — callers don't need a separate nil
// check before falling back to the global setting.
func (r *ModelRoute) EffectiveImageDownscaleMaxPx(globalMaxPx int) int {
	if r != nil && r.ImageDownscaleMaxPx != nil {
		return *r.ImageDownscaleMaxPx
	}
	return globalMaxPx
}

// Snapshot is an immutable view of the config; hot reload swaps the whole
// thing atomically, so in-flight requests keep the version they started with.
// Models is keyed protocol -> name, mirroring config.Config.Models.
type Snapshot struct {
	Cfg    *config.Config
	Models map[string]map[string]*ModelRoute

	// clients maps "<protocol>/<provider>" to the http.Client serving that
	// provider. Built in Install (travels with the snapshot to avoid races);
	// providers with the same effective proxy resolution (§config.ProxySpecFor)
	// share one client, so connection pooling stays per proxy group —
	// typically one or two clients per snapshot. clientSet is the distinct
	// set, kept for closing idle connections when the snapshot is replaced.
	clients   map[string]*http.Client
	clientSet []*http.Client
}

// clientFor returns the http.Client that carries this endpoint's provider.
// Coverage is guaranteed by construction: BuildSnapshot resolves endpoints
// from the same Cfg.Providers map Install builds clients from.
func (s *Snapshot) clientFor(ep *core.Endpoint) *http.Client {
	return s.clients[ep.AdapterType+"/"+ep.Provider]
}

// BuildSnapshot resolves provider references into concrete endpoints. Because
// an endpoint's provider is looked up within its own model's protocol group
// (cfg.Providers[protocol]), every endpoint of a model is guaranteed to share
// one adapter/protocol by construction — there is no "mixed protocol" case
// left to detect.
func BuildSnapshot(cfg *config.Config) (*Snapshot, error) {
	snap := &Snapshot{Cfg: cfg, Models: map[string]map[string]*ModelRoute{}}
	for protocol, models := range cfg.Models {
		ad, ok := adapter.Get(protocol)
		if !ok { // defensive; config.validate already checked this
			return nil, fmt.Errorf("protocol %q: unknown adapter type (available: %v)", protocol, adapter.Names())
		}
		byName := make(map[string]*ModelRoute, len(models))
		for name, m := range models {
			dims, err := strategy.Build(m.Strategy)
			if err != nil {
				return nil, fmt.Errorf("model %q: %w", name, err)
			}
			route := &ModelRoute{Dims: dims, ImageDownscaleMaxPx: m.ImageDownscaleMaxPx, Sticky: m.Sticky == nil || *m.Sticky}
			for _, ec := range m.Endpoints {
				p, ok := cfg.Providers[protocol][ec.Provider]
				if !ok { // defensive; config.validate already checked this
					return nil, fmt.Errorf("model %q: unknown provider %q in the %s protocol group", name, ec.Provider, protocol)
				}
				stickyTTL := cfg.StickyTTL.D()
				if ec.StickyTTL != nil {
					stickyTTL = ec.StickyTTL.D()
				}
				ep := &core.Endpoint{
					Provider:         ec.Provider,
					AdapterType:      protocol,
					BaseURL:          p.BaseURL,
					FullURL:          ad.ResolveURL(p.BaseURL),
					APIKey:           p.APIKey,
					Model:            ec.Model,
					Priority:         ec.Priority,
					RoleMap:          p.RoleMap,
					Capabilities:     ec.Capabilities,
					MaxContextTokens: ec.MaxContextTokens,
					StickyTTL:        stickyTTL,
				}
				route.Endpoints = append(route.Endpoints, ep)
			}
			byName[name] = route
		}
		snap.Models[protocol] = byName
	}
	return snap, nil
}

type Router struct {
	Health *health.Registry
	Sticky *sticky.Registry
	Logger *log.Logger

	snap atomic.Pointer[Snapshot]

	limiter  atomic.Pointer[limiter] // nil = unlimited
	inFlight atomic.Int64
	waiting  atomic.Int64
}

func New(logger *log.Logger) *Router {
	return &Router{Health: health.New(), Sticky: sticky.New(), Logger: logger}
}

// NewUpstreamClient builds an *http.Client configured exactly like Install
// would for connections to p: same dial/response-header/idle timeouts, same
// proxy resolution (config.ProxySpecFor). Standalone one-shot tools (replay,
// diagnose) that need to speak to a single provider without running a Router
// use this instead of duplicating Install's Transport setup — Install itself
// calls this per distinct proxy resolution.
func NewUpstreamClient(cfg *config.Config, p config.Provider) *http.Client {
	mode, proxyURL := cfg.ProxySpecFor(p)
	// nil Proxy = direct. Proxy environment variables are deliberately not
	// consulted — proxies are explicit config (config.Config.HTTPProxy/
	// HTTPSProxy), nothing implicit.
	var proxyFn func(*http.Request) (*url.URL, error)
	if mode == config.ProxyURL {
		if u, err := url.Parse(proxyURL); err == nil { // validated at config load
			proxyFn = http.ProxyURL(u)
		}
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 proxyFn,
			DialContext:           (&net.Dialer{Timeout: cfg.Timeouts.Connect.D()}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second, // zero = unbounded; a stalled handshake isn't covered by the dial timeout
			ResponseHeaderTimeout: cfg.Timeouts.ResponseHeader.D(),
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second, // zero would keep idle conns forever
		},
		// Never follow upstream redirects: POST 301/302/303 would be
		// silently rewritten to GET by the default policy, violating
		// byte-faithful passthrough (§1). LLM APIs almost never send 3xx,
		// but if one does the client sees exactly what a direct call would
		// — the 3xx status, Location header, and body, untouched.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Install atomically swaps in a new snapshot; in-flight requests keep the old one.
// One http.Client is built per distinct proxy resolution (direct, or a config
// proxy URL) and shared by every provider that resolves the same way — the
// per-provider proxy switch never costs a per-request check.
func (rt *Router) Install(s *Snapshot) {
	byResolution := map[string]*http.Client{}
	s.clients = map[string]*http.Client{}
	for protocol, byName := range s.Cfg.Providers {
		for name, p := range byName {
			mode, proxyURL := s.Cfg.ProxySpecFor(p)
			key := mode + "|" + proxyURL
			c, ok := byResolution[key]
			if !ok {
				c = NewUpstreamClient(s.Cfg, p)
				byResolution[key] = c
				s.clientSet = append(s.clientSet, c)
			}
			s.clients[protocol+"/"+name] = c
		}
	}
	rt.installLimiter(s.Cfg.MaxConcurrency)
	if old := rt.snap.Swap(s); old != nil {
		// Release the previous pools' idle connections now instead of
		// waiting for GC. In-flight requests still holding the old
		// snapshot are unaffected — their connections are active.
		for _, c := range old.clientSet {
			c.CloseIdleConnections()
		}
	}
}

func (rt *Router) Snapshot() *Snapshot { return rt.snap.Load() }

// limiter is the global concurrency gate: a plain semaphore. Requests over
// the limit block in memory (channel send, ~FIFO wakeup) until a slot frees.
type limiter struct {
	sem chan struct{}
	cap int
}

// installLimiter swaps the gate only when the capacity actually changed, so
// hot reloads that don't touch max_concurrency keep the live semaphore.
// During a capacity change, requests holding old slots release into the old
// semaphore — a brief over-admission window, accepted for a local tool.
func (rt *Router) installLimiter(capacity int) {
	cur := rt.limiter.Load()
	if cur != nil && cur.cap == capacity {
		return
	}
	if cur == nil && capacity <= 0 {
		return
	}
	if capacity <= 0 {
		rt.limiter.Store(nil)
		return
	}
	rt.limiter.Store(&limiter{sem: make(chan struct{}, capacity), cap: capacity})
}

// AcquireSlot blocks until a concurrency slot is free (or the client gives
// up). It returns a release func and ok=false when ctx was canceled while
// waiting.
func (rt *Router) AcquireSlot(ctx context.Context) (func(), bool) {
	l := rt.limiter.Load()
	if l == nil {
		rt.inFlight.Add(1)
		return func() { rt.inFlight.Add(-1) }, true
	}
	rt.waiting.Add(1)
	select {
	case l.sem <- struct{}{}:
		rt.waiting.Add(-1)
		rt.inFlight.Add(1)
		return func() {
			rt.inFlight.Add(-1)
			<-l.sem
		}, true
	case <-ctx.Done():
		rt.waiting.Add(-1)
		return nil, false
	}
}

// Concurrency reports the gate state for /admin/status.
func (rt *Router) Concurrency() (limit int, inFlight, waiting int64) {
	if l := rt.limiter.Load(); l != nil {
		limit = l.cap
	}
	return limit, rt.inFlight.Load(), rt.waiting.Load()
}

// Serve routes one chat request through the failover loop. protocol is the
// ingress protocol ("openai" or "anthropic"); a model bound to the other
// protocol is rejected — VMR never converts between protocols. rec (nilable)
// collects the per-attempt audit trail.
func (rt *Router) Serve(w http.ResponseWriter, r *http.Request, creq *core.CanonicalRequest, protocol string, rec *audit.Record) {
	start := time.Now()
	snap := rt.snap.Load()
	route, ok := snap.Models[protocol][creq.Model]
	if !ok {
		if other := otherProtocolFor(snap, protocol, creq.Model); other != "" {
			core.WriteError(w, http.StatusNotFound, "not_found_error",
				fmt.Sprintf("model %q speaks the %s protocol; call it via POST %s", creq.Model, other, IngressPath(other)))
			return
		}
		core.WriteError(w, http.StatusNotFound, "not_found_error",
			fmt.Sprintf("model %q not found; models on this endpoint: %s",
				creq.Model, strings.Join(modelNames(snap, protocol), ", ")))
		return
	}

	// Health filter (read-only) + stable multi-key sort.
	//
	// probe_mode: active (default) never lets real traffic touch a half-open
	// endpoint (fails>0, cooldown expired) at all — instead the first caller
	// to notice it's unprobed claims the single-flight slot (Acquire, same
	// method passive mode's per-candidate loop below uses) and hands it to a
	// background probe goroutine, then treats the endpoint as unavailable
	// for THIS request exactly as if Acquire had failed. Real requests never
	// wait on that probe and are never diverted for as long as it takes to
	// resolve — only for as long as it takes to notice it needs to run.
	// probe_mode: passive skips this and falls through to the original
	// behavior: any request may land on a half-open endpoint and become the
	// probe itself via Acquire in the per-candidate loop below.
	now := time.Now()
	healthOK := make([]*core.Endpoint, 0, len(route.Endpoints))
	activeProbing := snap.Cfg.ProbeMode == config.ProbeModeActive
	for _, ep := range route.Endpoints {
		key := ep.HealthKey()
		if activeProbing && rt.Health.Status(key, now).Fails > 0 {
			if rt.Health.Acquire(key, now) {
				go rt.runProbe(ep, snap)
			}
			continue
		}
		if rt.Health.Available(key, now) {
			healthOK = append(healthOK, ep)
		}
	}

	// Hard capability conditions (image/tools/…, see internal/strategy) are
	// certainties — a request either needs a capability or it doesn't, an
	// endpoint either declares it or doesn't — so rejecting every candidate
	// here is a correct "give up" signal, not a bug.
	hardFiltered := make([]*core.Endpoint, 0, len(healthOK))
	for _, ep := range healthOK {
		if strategy.Eligible(ep, creq.Facts) {
			hardFiltered = append(hardFiltered, ep)
		}
	}

	// Context length is an estimate, not a certainty (see
	// docs/VirtualModelRouter_System_Design_v3.md §6.4), so it never gets to
	// empty a non-empty hardFiltered set on its own — if every declared
	// max_context_tokens looks too small, fall back to hardFiltered and let
	// a real attempt (backed by the ordinary failover loop once the
	// upstream returns a real 400) make the call instead of refusing on a
	// guess.
	candidates := make([]*core.Endpoint, 0, len(hardFiltered))
	for _, ep := range hardFiltered {
		if strategy.WithinContext(ep, creq.Facts) {
			candidates = append(candidates, ep)
		}
	}
	if len(candidates) == 0 && len(hardFiltered) > 0 {
		candidates = hardFiltered
	}
	strategy.Sort(candidates, route.Dims)

	// Sticky Model: prefer whichever endpoint most recently, successfully
	// served this same conversation, so the upstream prompt cache stays
	// warm (docs/VirtualModelRouter_System_Design_v3.md §6.5). Only ever
	// reorders within the already-filtered candidates — an endpoint that's
	// unhealthy or fails a hard condition this turn is never resurrected
	// just because it was the sticky pick last time.
	var stickyKey string
	if route.Sticky {
		if sysHash, firstMsgHash, ok := adapter.SessionFingerprint(creq.Raw, protocol); ok {
			var clientKeyTag string
			if rec != nil {
				clientKeyTag = rec.ClientKeyTag
			}
			stickyKey = clientKeyTag + ":" + hex.EncodeToString(sysHash[:]) + ":" + hex.EncodeToString(firstMsgHash[:])
			if epKey, lastUsed, found := rt.Sticky.Peek(stickyKey); found {
				if ep := findByHealthKey(candidates, epKey); ep != nil && time.Since(lastUsed) < ep.StickyTTL {
					moveToFront(candidates, ep)
				}
			}
		}
	}

	// Failover walks the whole candidate sequence: keep trying until one
	// endpoint succeeds or every available endpoint has been tried once.
	// max_attempts (>0) optionally caps the walk to bound tail latency.
	attempts := 0
	var last *upstreamError
	for _, ep := range candidates {
		if snap.Cfg.MaxAttempts > 0 && attempts >= snap.Cfg.MaxAttempts {
			break
		}
		// Acquire enforces the single-flight probe for half-open endpoints
		// (probe_mode: passive — active mode's half-open endpoints were
		// already filtered out above, so every candidate reaching this line
		// under active mode has fails==0 and this is always a no-op true).
		if !rt.Health.Acquire(ep.HealthKey(), time.Now()) {
			continue
		}
		attempts++
		done, uerr, success := rt.tryOne(w, r, creq, ep, snap, attempts, start, rec)
		if done {
			if success && stickyKey != "" {
				// Every successful completion moves the pointer — including
				// a failover success — so it always follows wherever the
				// conversation's cache is actually warm (design doc §6.5).
				rt.Sticky.Set(stickyKey, ep.HealthKey())
			}
			return
		}
		// Build/network failures return no HTTP response (uerr == nil).
		// Keep the last real upstream error instead of wiping it: "return
		// the last upstream error verbatim" means the last one that HAS a
		// status/headers/body to return.
		if uerr != nil {
			last = uerr
		}
	}

	// All candidates failed or none were available.
	if last != nil {
		// Return the last upstream error verbatim — status, headers
		// (Retry-After included) and body — so the client sees exactly
		// what a direct call would have shown.
		copyRespHeaders(w.Header(), last.header)
		w.Header().Set("X-VMR-Attempts", strconv.Itoa(attempts))
		w.WriteHeader(last.status)
		w.Write(last.body)
	} else {
		w.Header().Set("X-VMR-Attempts", strconv.Itoa(attempts))
		msg := fmt.Sprintf("no available endpoint for model %q (all cooling down or none configured)", creq.Model)
		if attempts > 0 {
			msg = fmt.Sprintf("all %d attempt(s) for model %q failed before an upstream response (network or build errors); see vmr logs", attempts, creq.Model)
		} else if len(healthOK) > 0 {
			// Health had candidates; a Condition rejected every one of
			// them — name which one(s) so the operator doesn't have to
			// guess (see docs/VirtualModelRouter_System_Design_v3.md §6.4).
			msg = fmt.Sprintf("no endpoint for model %q accepts this request (%s)", creq.Model, rejectionSummary(healthOK, creq.Facts))
		}
		core.WriteError(w, http.StatusServiceUnavailable, "vmr_no_candidates", msg)
	}
	rt.logf("%s %s status=all_failed attempts=%d dur=%s", clientTag(rec), creq.Model, attempts, fmtDur(time.Since(start)))
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
func (rt *Router) tryOne(w http.ResponseWriter, r *http.Request, creq *core.CanonicalRequest,
	ep *core.Endpoint, snap *Snapshot, attempt int, start time.Time, rec *audit.Record) (done bool, uerr *upstreamError, success bool) {

	key := ep.HealthKey()
	attemptStart := time.Now()
	// logPrefix carries the fields every log line in this attempt shares
	// (client tag, model, endpoint, attempt number) so each call site below
	// only spells out what actually differs about its outcome. Gains the
	// req= size once BuildRequest succeeds — every line after that point
	// reuses the extended prefix.
	logPrefix := fmt.Sprintf("%s %s %s attempt=%d", clientTag(rec), creq.Model, epLabel(ep, creq.Stream), attempt)
	var att *audit.Attempt
	if rec != nil {
		rec.Attempts = append(rec.Attempts, audit.Attempt{
			Endpoint: strings.Join([]string{ep.AdapterType, ep.Provider, ep.Model}, ":"),
			Protocol: ep.AdapterType,
			Provider: ep.Provider,
			Model:    ep.Model,
		})
		att = &rec.Attempts[len(rec.Attempts)-1]
		defer func() { att.DurMS = time.Since(attemptStart).Milliseconds() }()
	}

	ad, ok := adapter.Get(ep.AdapterType)
	if !ok { // validated at config load; defensive only
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
		rt.Health.ReportNeutral(key)
		rt.logf("%s error=build:%v", logPrefix, err)
		if att != nil {
			att.Error = "build: " + err.Error()
			att.ErrorClass = core.ErrBuild.String()
		}
		return false, nil, false
	}
	logPrefix += " req=" + core.FmtBytes(int64(len(outBody)))
	if att != nil {
		att.URL = req.URL.String()
		// outBody comes straight from BuildRequest (immutable by contract),
		// so the audit trail references it directly — no GetBody+ReadAll
		// round trip duplicating the whole body per attempt.
		att.Request = audit.NewMessage(req.Header, outBody)
	}

	resp, err := snap.clientFor(ep).Do(req)
	if err != nil {
		if r.Context().Err() != nil {
			// Client went away; nothing to write, don't punish the endpoint.
			// But DO release a half-open probe slot if this attempt held one:
			// without this, a client canceling mid-probe leaves probing=true
			// forever and the endpoint is locked out until process restart.
			rt.Health.ReportNeutral(key)
			if att != nil {
				att.Error = "canceled by client"
				att.ErrorClass = core.ErrCanceled.String()
			}
			return true, nil, false
		}
		cd := rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now())
		rt.logf("%s error=network:%v cooldown=%s", logPrefix, err, cd)
		if att != nil {
			att.Error = "network: " + err.Error()
			att.ErrorClass = core.ErrNetwork.String()
		}
		return false, nil, false
	}

	if resp.StatusCode >= 400 {
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
		// uerr.body is forwarded to the client verbatim (byte-faithful, §1) —
		// any truncation marker must go only into the audit copy below, never
		// into this slice.
		uerr := &upstreamError{resp.StatusCode, resp.Header, body}
		if att != nil {
			auditBody := body
			if truncated {
				// A fresh slice, never touched again — safe to hand to
				// EncodeBody under its "referenced, not cloned" ownership
				// contract (audit.go). Reports the cap, not upstream's true
				// size: the LimitReader above deliberately never reads past
				// it, so the real total is unknown.
				auditBody = append(append([]byte(nil), body...), []byte(fmt.Sprintf("\n...(truncated at %d bytes)", errBodyCap))...)
			}
			m := audit.NewMessage(resp.Header, auditBody)
			m.Status = resp.StatusCode
			att.Response = &m
			att.Error = class.String()
			att.ErrorClass = class.String()
		}
		if class == core.ErrContent {
			// Content-policy flag: specific to this request, not this endpoint.
			// Keep failing over (vendors differ in sensitivity) but leave the
			// endpoint's health untouched — only release a probe slot if held.
			rt.Health.ReportNeutral(key)
			rt.logf("%s status=%d class=content (no cooldown)", logPrefix, resp.StatusCode)
			return false, uerr, false
		}
		if class == core.ErrClient {
			// Bad request: every endpoint would fail the same way. Return as-is.
			// Says nothing about the endpoint's health — release a probe slot
			// if this attempt held one (same lockout hazard as client cancel).
			rt.Health.ReportNeutral(key)
			copyRespHeaders(w.Header(), uerr.header)
			w.Header().Set("X-VMR-Attempts", strconv.Itoa(attempt))
			w.WriteHeader(uerr.status)
			w.Write(uerr.body)
			rt.logf("%s status=%d class=client dur=%s", logPrefix, resp.StatusCode, fmtDur(time.Since(start)))
			return true, nil, false
		}
		cd := rt.Health.ReportFailure(key, class, parseRetryAfter(resp.Header), time.Now())
		rt.logf("%s status=%d class=%s cooldown=%s", logPrefix, resp.StatusCode, class, cd)
		return false, uerr, false
	}

	// Success: report health, then forward. From the first byte written the
	// response is committed — no failover past this point.
	rt.Health.ReportSuccess(key)
	if att != nil {
		// Body omitted: passthrough makes it byte-identical to the client
		// response body, which the server layer records.
		att.Response = &audit.Message{Status: resp.StatusCode, Headers: audit.Redact(resp.Header)}
	}
	body := resp.Body
	defer body.Close()

	// Forward the upstream's response headers (minus hop-by-hop) so the
	// client sees what a direct call would have shown — rate-limit
	// headers, request IDs, Date, Retry-After. VMR's own headers go on top.
	copyRespHeaders(w.Header(), resp.Header)
	w.Header().Set("X-VMR-Endpoint", ep.Name())
	w.Header().Set("X-VMR-Attempts", strconv.Itoa(attempt))
	w.WriteHeader(resp.StatusCode)

	// Wrap the upstream body with the response normalizer (response.go):
	// true streaming by default, buffered only when a MiniMax thinking
	// shape is detected, raw passthrough when the body is compressed.
	ct := resp.Header.Get("Content-Type")
	isSSE := strings.Contains(ct, "text/event-stream") || (ct == "" && creq.Stream)
	opaque := resp.Header.Get("Content-Encoding") != ""
	rbody := newRespStream(body, creq.Model, isSSE, ep.AdapterType, opaque)

	// Both SSE and non-SSE bodies go through copyFlush so the stream_idle
	// watchdog covers every upstream response body: a 200 whose body stalls
	// mid-transfer must abort instead of parking the request forever. The
	// per-chunk Flush is a no-op concern for JSON bodies — Content-Length is
	// stripped anyway.
	copyErr := copyFlush(w, rbody, snap.Cfg.Timeouts.StreamIdle.D())
	status := "ok"
	if copyErr != nil && r.Context().Err() == nil {
		status = "truncated" // upstream died mid-stream; the response is already committed
		if att != nil {
			att.Error = "truncated: " + copyErr.Error()
			att.ErrorClass = core.ErrTruncated.String()
		}
	}
	if att != nil {
		att.Norm = rbody.Applied()
		if raw := rbody.RawPreStrip(); len(raw) > 0 {
			att.RawPreStrip = audit.EncodeBody(raw)
		}
	}
	rt.logf("%s status=%s dur=%s", logPrefix, status, fmtDur(time.Since(start)))
	return true, nil, true
}

// copyFlush forwards the body chunk by chunk, flushing after every read so
// SSE tokens reach the client immediately. A watchdog aborts the copy when the
// upstream goes silent for longer than idle. On timeout the caller closes the
// body, which unblocks the reader goroutine.
func copyFlush(w http.ResponseWriter, body io.Reader, idle time.Duration) error {
	flusher, _ := w.(http.Flusher)
	type chunk struct {
		data []byte
		err  error
	}
	ch := make(chan chunk)
	done := make(chan struct{})
	defer close(done)
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := body.Read(buf)
			var data []byte
			if n > 0 {
				data = append([]byte(nil), buf[:n]...)
			}
			select {
			case ch <- chunk{data, err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	timer := time.NewTimer(idle)
	defer timer.Stop()
	for {
		select {
		case c := <-ch:
			if len(c.data) > 0 {
				if _, werr := w.Write(c.data); werr != nil {
					return werr
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			if c.err != nil {
				if c.err == io.EOF {
					return nil
				}
				return c.err
			}
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(idle)
		case <-timer.C:
			return errors.New("stream idle timeout")
		}
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
	return core.SortedKeys(s.Models[protocol])
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
// actually POSTs to ("openai" -> chat completions, anything else ->
// Anthropic messages, mirroring every other protocol switch in this
// codebase). Exported so every consumer that needs to name a protocol's
// ingress route (this package's own 404 redirect message, and
// internal/replay's reconstructed Client.Request.Path) shares one mapping
// instead of each keeping its own copy that could drift if a third
// protocol is ever registered.
func IngressPath(protocol string) string {
	if protocol == "anthropic" {
		return "/v1/messages"
	}
	return "/v1/chat/completions"
}

func (rt *Router) logf(format string, args ...any) {
	if rt.Logger != nil {
		rt.Logger.Printf(format, args...)
	}
}

// Logf is logf, exported so callers outside this package (internal/server,
// for the handful of lines it logs itself — e.g. audit write failures) go
// through the same nil-safe path and pick up the same timestamp/format
// instead of falling back to the unstamped global "log" package.
func (rt *Router) Logf(format string, args ...any) {
	rt.logf(format, args...)
}

// tagCol pads s to a fixed 8-char, left-aligned column — every log line
// starts with one so the fields after it stay vertically aligned regardless
// of how long the actor name is.
func tagCol(s string) string {
	return fmt.Sprintf("%-8s", s)
}

// clientTag is the tagCol value for a real client request: the audit key
// tag that identifies who sent it, or "-" when auditing is off (rec == nil)
// or the request carried no matching key.
func clientTag(rec *audit.Record) string {
	tag := "-"
	if rec != nil && rec.ClientKeyTag != "" {
		tag = rec.ClientKeyTag
	}
	return tagCol(tag)
}

// epLabel is the log-only endpoint label — colon-joined protocol:provider:model
// (as opposed to Endpoint.Name()'s slash form, which is a stable identifier
// used in the admin status API and the X-VMR-Endpoint header and must not
// change shape) — with a "(stream)" suffix when the request is streaming.
func epLabel(ep *core.Endpoint, stream bool) string {
	label := ep.AdapterType + ":" + ep.Provider + ":" + ep.Model
	if stream {
		label += "(stream)"
	}
	return label
}

// fmtDur renders an elapsed duration for the dur= column — see
// core.FmtSeconds; 2 decimals is this log's fixed precision.
func fmtDur(d time.Duration) string {
	return core.FmtSeconds(d, 2)
}
