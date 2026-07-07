// Ver 2026-07-07 02:05, by Fable 5

// Package router holds the failover loop: health filter → multi-key sort →
// try candidates in order. This is the core of the project and should stay small.
package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
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
	"vmr/internal/strategy"
)

// ModelRoute is the runtime routing table entry for one virtual model.
// Protocol is inferred from the endpoints' adapters; VMR never converts
// between protocols, so all endpoints of a model must share one.
type ModelRoute struct {
	Protocol  string
	Dims      []strategy.Dimension
	Endpoints []*core.Endpoint
}

// Snapshot is an immutable view of the config; hot reload swaps the whole
// thing atomically, so in-flight requests keep the version they started with.
type Snapshot struct {
	Cfg    *config.Config
	Models map[string]*ModelRoute

	client *http.Client // built in Install; travels with the snapshot to avoid races
}

// BuildSnapshot resolves provider references into concrete endpoints.
func BuildSnapshot(cfg *config.Config) (*Snapshot, error) {
	snap := &Snapshot{Cfg: cfg, Models: map[string]*ModelRoute{}}
	for name, m := range cfg.Models {
		dims, err := strategy.Build(m.Strategy)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", name, err)
		}
		route := &ModelRoute{Dims: dims}
		for _, ec := range m.Endpoints {
			p := cfg.Providers[ec.Provider]
			ad, ok := adapter.Get(p.Type)
			if !ok {
				return nil, fmt.Errorf("model %q: provider %q has unknown adapter type %q", name, ec.Provider, p.Type)
			}
			if route.Protocol == "" {
				route.Protocol = ad.Protocol()
			} else if route.Protocol != ad.Protocol() {
				return nil, fmt.Errorf("model %q mixes protocols %q and %q (endpoint %s/%s): VMR does not convert between protocols, split into separate models",
					name, route.Protocol, ad.Protocol(), ec.Provider, ec.Model)
			}
			route.Endpoints = append(route.Endpoints, &core.Endpoint{
				Provider:    ec.Provider,
				AdapterType: p.Type,
				BaseURL:     p.BaseURL,
				APIKey:      p.APIKey,
				Model:       ec.Model,
				Priority:    ec.Priority,
			})
		}
		snap.Models[name] = route
	}
	return snap, nil
}

type Router struct {
	Health *health.Registry
	Logger *log.Logger

	snap atomic.Pointer[Snapshot]

	limiter  atomic.Pointer[limiter] // nil = unlimited
	inFlight atomic.Int64
	waiting  atomic.Int64
}

func New(logger *log.Logger) *Router {
	return &Router{Health: health.New(), Logger: logger}
}

// Install atomically swaps in a new snapshot; in-flight requests keep the old one.
func (rt *Router) Install(s *Snapshot) {
	s.client = &http.Client{Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: s.Cfg.Timeouts.Connect.D()}).DialContext,
		ResponseHeaderTimeout: s.Cfg.Timeouts.ResponseHeader.D(),
		MaxIdleConnsPerHost:   16,
	}}
	rt.installLimiter(s.Cfg.MaxConcurrency)
	rt.snap.Store(s)
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
	route, ok := snap.Models[creq.Model]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found_error",
			fmt.Sprintf("model %q not found; models on this endpoint: %s",
				creq.Model, strings.Join(modelNames(snap, protocol), ", ")))
		return
	}
	if route.Protocol != protocol {
		writeError(w, http.StatusNotFound, "not_found_error",
			fmt.Sprintf("model %q speaks the %s protocol; call it via %s", creq.Model, route.Protocol, ingressPath(route.Protocol)))
		return
	}

	// Health filter (read-only) + stable multi-key sort.
	now := time.Now()
	candidates := make([]*core.Endpoint, 0, len(route.Endpoints))
	for _, ep := range route.Endpoints {
		if rt.Health.Available(ep.HealthKey(), now) {
			candidates = append(candidates, ep)
		}
	}
	strategy.Sort(candidates, route.Dims)

	// Failover walks the whole candidate sequence: keep trying until one
	// endpoint succeeds or every available endpoint has been tried once.
	// max_attempts (>0) optionally caps the walk to bound tail latency.
	attempts := 0
	var last *upstreamError
	for _, ep := range candidates {
		if snap.Cfg.MaxAttempts > 0 && attempts >= snap.Cfg.MaxAttempts {
			break
		}
		// Acquire enforces the single-flight probe for half-open endpoints.
		if !rt.Health.Acquire(ep.HealthKey(), time.Now()) {
			continue
		}
		attempts++
		done, uerr := rt.tryOne(w, r, creq, ep, snap, attempts, start, rec)
		if done {
			return
		}
		last = uerr
	}

	// All candidates failed or none were available.
	w.Header().Set("X-VMR-Attempts", strconv.Itoa(attempts))
	if last != nil {
		// Return the last upstream error verbatim so the client sees the real cause.
		if last.contentType != "" {
			w.Header().Set("Content-Type", last.contentType)
		}
		w.WriteHeader(last.status)
		w.Write(last.body)
	} else {
		writeError(w, http.StatusServiceUnavailable, "vmr_no_candidates",
			fmt.Sprintf("no available endpoint for model %q (all cooling down or none configured)", creq.Model))
	}
	rt.logf("model=%s status=all_failed attempts=%d dur=%s", creq.Model, attempts, time.Since(start).Round(time.Millisecond))
}

type upstreamError struct {
	status      int
	contentType string
	body        []byte
}

// tryOne sends the request to a single endpoint. It returns done=true when a
// response (success or non-retryable error) has been written to the client.
func (rt *Router) tryOne(w http.ResponseWriter, r *http.Request, creq *core.CanonicalRequest,
	ep *core.Endpoint, snap *Snapshot, attempt int, start time.Time, rec *audit.Record) (bool, *upstreamError) {

	key := ep.HealthKey()
	attemptStart := time.Now()
	var att *audit.Attempt
	if rec != nil {
		rec.Attempts = append(rec.Attempts, audit.Attempt{Endpoint: ep.Name()})
		att = &rec.Attempts[len(rec.Attempts)-1]
		defer func() { att.DurMS = time.Since(attemptStart).Milliseconds() }()
	}

	ad, ok := adapter.Get(ep.AdapterType)
	if !ok { // validated at config load; defensive only
		rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now())
		return false, nil
	}

	req, err := ad.BuildRequest(r.Context(), ep, creq)
	if err != nil {
		rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now())
		rt.logf("model=%s ep=%s attempt=%d build_error=%v", creq.Model, ep.Name(), attempt, err)
		if att != nil {
			att.Error = "build: " + err.Error()
		}
		return false, nil
	}
	if att != nil {
		att.URL = req.URL.String()
		var outBody []byte
		if req.GetBody != nil {
			if rc, err := req.GetBody(); err == nil {
				outBody, _ = io.ReadAll(io.LimitReader(rc, audit.MaxBodyBytes+1))
				rc.Close()
			}
		}
		att.Request = audit.NewMessage(req.Header, outBody)
	}

	resp, err := snap.client.Do(req)
	if err != nil {
		if r.Context().Err() != nil {
			if att != nil {
				att.Error = "canceled by client"
			}
			return true, nil // client went away; nothing to write, don't punish the endpoint
		}
		cd := rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now())
		rt.logf("model=%s ep=%s attempt=%d net_error=%v cooldown=%s", creq.Model, ep.Name(), attempt, err, cd)
		if att != nil {
			att.Error = "network: " + err.Error()
		}
		return false, nil
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		class := ad.ClassifyError(resp.StatusCode, body)
		uerr := &upstreamError{resp.StatusCode, resp.Header.Get("Content-Type"), body}
		if att != nil {
			m := audit.NewMessage(resp.Header, body)
			m.Status = resp.StatusCode
			att.Response = &m
			att.Error = class.String()
		}
		if class == core.ErrContent {
			// Content-policy flag: specific to this request, not this endpoint.
			// Keep failing over (vendors differ in sensitivity) but leave the
			// endpoint's health untouched — only release a probe slot if held.
			rt.Health.ReportNeutral(key)
			rt.logf("model=%s ep=%s attempt=%d status=%d class=content (no cooldown)",
				creq.Model, ep.Name(), attempt, resp.StatusCode)
			return false, uerr
		}
		if class == core.ErrClient {
			// Bad request: every endpoint would fail the same way. Return as-is.
			w.Header().Set("X-VMR-Attempts", strconv.Itoa(attempt))
			if uerr.contentType != "" {
				w.Header().Set("Content-Type", uerr.contentType)
			}
			w.WriteHeader(uerr.status)
			w.Write(uerr.body)
			rt.logf("model=%s ep=%s attempt=%d status=%d class=client dur=%s",
				creq.Model, ep.Name(), attempt, resp.StatusCode, time.Since(start).Round(time.Millisecond))
			return true, nil
		}
		cd := rt.Health.ReportFailure(key, class, parseRetryAfter(resp.Header), time.Now())
		rt.logf("model=%s ep=%s attempt=%d status=%d class=%s cooldown=%s",
			creq.Model, ep.Name(), attempt, resp.StatusCode, class, cd)
		return false, uerr
	}

	// Success: report health, then forward. From the first byte written the
	// response is committed — no failover past this point.
	rt.Health.ReportSuccess(key)
	if att != nil {
		// Body omitted: passthrough makes it byte-identical to the client
		// response body, which the server layer records.
		att.Response = &audit.Message{Status: resp.StatusCode, Headers: audit.Redact(resp.Header)}
	}
	body := ad.TransformBody(resp.Body, creq.Stream)
	defer body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("X-VMR-Endpoint", ep.Name())
	w.Header().Set("X-VMR-Attempts", strconv.Itoa(attempt))
	w.WriteHeader(resp.StatusCode)

	var copyErr error
	if creq.Stream {
		copyErr = copyFlush(w, body, snap.Cfg.Timeouts.StreamIdle.D())
	} else {
		_, copyErr = io.Copy(w, body)
	}
	status := "ok"
	if copyErr != nil && r.Context().Err() == nil {
		status = "truncated" // upstream died mid-stream; the response is already committed
	}
	rt.logf("model=%s ep=%s attempt=%d status=%s stream=%v dur=%s",
		creq.Model, ep.Name(), attempt, status, creq.Stream, time.Since(start).Round(time.Millisecond))
	return true, nil
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
	names := make([]string, 0, len(s.Models))
	for n, route := range s.Models {
		if route.Protocol == protocol {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

func ingressPath(protocol string) string {
	if protocol == "anthropic" {
		return "POST /v1/messages"
	}
	return "POST /v1/chat/completions"
}

// writeError emits an error body that both OpenAI clients (error.message)
// and Anthropic clients (type:"error" envelope) can parse.
func writeError(w http.ResponseWriter, status int, errType, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"type":  "error",
		"error": map[string]string{"type": errType, "message": msg},
	})
}

func (rt *Router) logf(format string, args ...any) {
	if rt.Logger != nil {
		rt.Logger.Printf(format, args...)
	}
}
