// Ver 2026-07-24 12:35, by Sonnet 5

// Package server is the HTTP surface: auth, /v1/chat/completions, /v1/models,
// /health, /status, /status.html, /help, /help.html, /help.zh, /help.zh.html,
// /log, /log.html. Anything else is 404.
package server

import (
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"vmr/internal/adapter"
	"vmr/internal/audit"
	"vmr/internal/core"
	"vmr/internal/imgprep"
	"vmr/internal/logtee"
	"vmr/internal/router"
)

type Server struct {
	rt     *router.Router
	audit  *audit.Logger // nil = auditing disabled
	inst   instance      // zero value outside `vmr start` (tests, embedding)
	logTee *logtee.Tee   // nil = live-log streaming unavailable (/log answers 503)
	// started is when this Server began serving — /health's uptime basis.
	// Separate from inst.startedAt, which only `vmr start` fills in and
	// which /status therefore reports conditionally: /health has no
	// conditional shape to fall back on, so it needs a value that always
	// exists. The two differ by however long config loading took.
	started time.Time
}

func New(rt *router.Router, auditLog *audit.Logger) *Server {
	return &Server{rt: rt, audit: auditLog, started: time.Now()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.chatHandler(core.ProtocolOpenAICompletions))
	mux.HandleFunc("POST /v1/messages", s.chatHandler(core.ProtocolAnthropicMessages))
	mux.HandleFunc("POST /v1/responses", s.chatHandler(core.ProtocolOpenAIResponses))
	mux.HandleFunc("GET /v1/models", s.auth(s.models))
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /status", s.auth(s.adminStatus))
	mux.HandleFunc("GET /status.html", s.statusPage)
	mux.HandleFunc("GET /help", s.helpPageEN)
	mux.HandleFunc("GET /help.html", s.helpPageEN)
	mux.HandleFunc("GET /help.zh", s.helpPageZH)
	mux.HandleFunc("GET /help.zh.html", s.helpPageZH)
	mux.HandleFunc("GET /log", s.auth(s.adminLog))
	mux.HandleFunc("GET /log.html", s.logPage)
	return mux
}

// health is the unauthenticated liveness endpoint: it answers "is this
// process up and serving HTTP", and nothing else. Deliberately outside
// s.auth — a liveness probe has no credential to present and rarely
// originates from 127.0.0.1 (container runtimes, reverse proxies, external
// monitors).
//
// The body carries the current time and uptime rather than a constant "ok"
// for one reason: a fixed body is indistinguishable from a cached one, so
// an intermediary could keep answering 200 long after the process died.
// Both fields move on every request and uptime moves monotonically, so a
// stale copy is self-evident. Cache-Control says the same thing to
// well-behaved intermediaries; the moving body covers the rest.
//
// Nothing about the instance (config path, PID, models, endpoints, quota)
// belongs in this response. Adding any of it would turn an unauthenticated
// liveness ping into an unauthenticated /status — which is auth-gated
// precisely because it answers those questions.
//
// Liveness, not readiness: 200 means the process is up, never "an upstream
// is healthy". Gating it on endpoint health would have an orchestrator kill
// a perfectly functional router whenever every provider is down — a restart
// cannot fix an upstream outage, so that check would only amplify it.
// Callers who want readiness read /status's health block.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	now := time.Now()
	router.WriteJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"time":           now,
		"uptime_seconds": int64(now.Sub(s.started).Seconds()),
	})
}

// authenticate enforces the router's own optional API keys and reports
// which one matched. Both credential conventions are accepted:
// Authorization: Bearer (OpenAI) and x-api-key (Anthropic SDKs send only
// this). tag is audit.KeyTag(the matched Cfg.APIKeys entry) — "" when auth
// is disabled entirely or (ok == false) nothing matched at all. api_keys is
// the only auth surface (the singular api_key catch-all was removed: it
// added nothing this list can't express, at the cost of a second code path
// here and a second config field to document — config's strict KnownFields
// decoding now rejects it like any other unknown field).
//
// Self-declared tag, no config needed: when APIKeys is empty, the door
// stays fully open, but whatever credential-shaped value the client chooses
// to send still gets KeyTag-derived and recorded — a private-network caller
// can identify itself to `vmr report` just by ending its own
// Authorization/x-api-key value in "-<label>", with zero vmr-side config.
// A client sending nothing still gets "".
func (s *Server) authenticate(r *http.Request) (tag string, ok bool) {
	cfg := s.rt.Snapshot().Cfg
	got := trimBearerPrefix(r.Header.Get("Authorization"))
	if got == "" {
		got = r.Header.Get("x-api-key")
	}
	if len(cfg.APIKeys) == 0 {
		return audit.KeyTag(got), true // no key configured: auth disabled, tag self-declared
	}
	for _, key := range cfg.APIKeys {
		if subtle.ConstantTimeCompare([]byte(got), []byte(key)) == 1 {
			return audit.KeyTag(key), true
		}
	}
	return "", false
}

// bearerPrefix is the auth-scheme token from the Authorization header.
// RFC 7235 defines auth-scheme as case-insensitive, so "bearer " and
// "BEARER " must strip the same as "Bearer " — clients that get this
// technically-correct-but-nonstandard-casing right shouldn't get a 401.
const bearerPrefix = "Bearer "

func trimBearerPrefix(auth string) string {
	if len(auth) >= len(bearerPrefix) && strings.EqualFold(auth[:len(bearerPrefix)], bearerPrefix) {
		return auth[len(bearerPrefix):]
	}
	return auth
}

// checkAuth is the tag-less wrapper for endpoints that only need a pass/
// fail decision, not the caller's identity.
func (s *Server) checkAuth(r *http.Request) bool {
	_, ok := s.authenticate(r)
	return ok
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(r) {
			router.WriteError(w, http.StatusUnauthorized, "authentication_error", "invalid or missing API key")
			return
		}
		next(w, r)
	}
}

func (s *Server) chatHandler(protocol string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.rt.Telemetry.RecordRequest(protocol)
		var rec *audit.Record
		if s.audit != nil {
			rec = &audit.Record{
				TS:       time.Now(),
				Protocol: protocol,
				Client: audit.Exchange{
					Addr:    r.RemoteAddr,
					Request: audit.Message{Method: r.Method, Path: r.URL.Path, Headers: audit.Redact(r.Header)},
				},
			}
			rw := newRecorder(w, rec.TS)
			w = rw
			defer func() {
				rec.DurMS = time.Since(rec.TS).Milliseconds()
				rec.TTFTMS = rw.ttftMS
				rec.Client.Response = rw.message()
				canceled := r.Context().Err() != nil
				rec.Outcome = audit.OutcomeFor(rw.status, canceled)
				if err := s.audit.Write(rec); err != nil {
					s.rt.Logf("audit: %v", err)
				}
			}()
		}

		tag, authed := s.authenticate(r)
		if rec != nil {
			rec.ClientKeyTag = tag
		}
		if !authed {
			s.rt.Telemetry.RecordOutcome(false, false)
			router.WriteError(w, http.StatusUnauthorized, "authentication_error", "invalid or missing API key")
			return
		}

		snap := s.rt.Snapshot()

		// Buffer the whole body up front (streaming included): failover replay needs it.
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, snap.Cfg.MaxRequestBodyBytes()))
		if rec != nil {
			rec.Client.Request.Body = audit.EncodeBody(body)
		}
		if err != nil {
			s.rt.Telemetry.RecordOutcome(false, false)
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				router.WriteError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body exceeds limit")
			} else {
				router.WriteError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
			}
			return
		}

		// Probe the routing fields before acquiring the concurrency gate or
		// downscaling images: both of the latter are per-model (image
		// downscale can be overridden per virtual model) and cheap JSON
		// parsing shouldn't wait behind either. One structural scan yields
		// model/stream/hasTools together (adapter.TopLevelProbe) instead of
		// a reflective json.Unmarshal for model/stream plus a second,
		// independent top-level scan for tools later in computeRequestFacts.
		probeModel, probeStream, probeHasTools, probeOK := adapter.TopLevelProbe(body)
		if !probeOK {
			s.rt.Telemetry.RecordOutcome(false, false)
			router.WriteError(w, http.StatusBadRequest, "invalid_request_error", "request body is not valid JSON")
			return
		}
		if probeModel == "" {
			s.rt.Telemetry.RecordOutcome(false, false)
			router.WriteError(w, http.StatusBadRequest, "invalid_request_error", "missing required field: model")
			return
		}
		if rec != nil {
			rec.Model, rec.Stream = probeModel, probeStream
		}

		// Global concurrency gate: excess requests park here until a slot
		// frees, or the client goes away. Acquired AFTER the body is
		// buffered so a slow upload never occupies a slot — the gate
		// covers the CPU-and-upstream phase (image downscaling included),
		// not client I/O.
		release, ok := s.rt.AcquireSlot(r.Context())
		if !ok {
			s.rt.Telemetry.RecordOutcome(false, true)
			return // client canceled while waiting; nothing to write
		}
		defer release()

		// Request-only image downscaling: shrinks oversized inline
		// attachments before routing so vision-token cost doesn't scale
		// with screenshot resolution. Response bodies are never touched.
		// The effective cap is the virtual model's own override if it set
		// one (even 0, which force-disables it for that model), else the
		// global default. Detection (format/dimensions/count) always
		// runs regardless of that cap (n<=0 just skips the resize/cache path
		// inside imgprep) — a plain-text request with no image marker at all
		// still costs only one cheap substring scan (imgprep.HasImageMarker)
		// before Downscale returns.
		//
		// This one call is the single source of truth for "does this request
		// have images": len(images) feeds rec.Images below AND
		// computeRequestFacts' imageCount, which drives RequestFacts.HasImage
		// and the image portion of EstimatedTokens. One detection pass per
		// request, reused three ways — see computeRequestFacts' doc comment
		// (facts.go) for why the structural answer, not a byte-scan, is what
		// the routing Condition needs. That dependency is also why this call
		// cannot be skipped when rec==nil && n<=0: routing correctness, not
		// just audit metadata, now rests on it.
		// Skip the work for a model name Serve is about to 404 anyway (same
		// snap.Models lookup Serve repeats below) — no point decoding/scaling/
		// caching images for a request that never reaches routing.
		route := snap.Models[protocol][probeModel]
		var images []imgprep.ImageInfo
		if route != nil {
			n := route.EffectiveImageDownscaleMaxPx(snap.Cfg.ImageDownscaleMaxPx)
			body, images = imgprep.Downscale(body, protocol, imgprep.Options{
				MaxPx:        n,
				CacheDir:     snap.Cfg.ImageCacheDir,
				CacheTTLDays: snap.Cfg.ImageCacheTTLDays,
			})
		}
		if rec != nil && len(images) > 0 {
			rec.Images = make([]audit.ImageInfo, len(images))
			for i, img := range images {
				rec.Images[i] = audit.ImageInfo{
					MessageIndex:     img.MessageIndex,
					Format:           img.Format,
					Bytes:            img.Bytes,
					Width:            img.Width,
					Height:           img.Height,
					Remote:           img.Remote,
					Downscaled:       img.Downscaled,
					DownscaledWidth:  img.DownscaledWidth,
					DownscaledHeight: img.DownscaledHeight,
					DownscaledBytes:  img.DownscaledBytes,
					CacheHit:         img.CacheHit,
				}
			}
		}

		// Pass client headers through unless on the blocklist — this
		// includes the anthropic-messages protocol headers (anthropic-version,
		// anthropic-beta). The rationale is that LLM SDKs (OpenAI JS,
		// Anthropic) only emit a known fixed set of headers and none of
		// them are dangerous, so a strict whitelist is more brittle than
		// necessary — it strips legitimate metadata (User-Agent,
		// X-Stainless-*, Traceparent) and needs code updates when SDKs
		// add new headers.
		hdr := router.FilterClientHeaders(r.Header)

		// Computed once, used twice: the routing layer consults it via
		// CanonicalRequest.Facts, and the audit trail records the exact
		// same value (rec.Facts) — never a second, independent computation
		// at write time. See audit.Record.Facts's doc comment for why it's
		// a sibling of Client.Request rather than folded into it.
		facts := computeRequestFacts(body, len(images), probeHasTools)
		if rec != nil {
			rec.Facts = &facts
		}

		s.rt.Serve(w, r, &core.CanonicalRequest{
			Model: probeModel, Stream: probeStream, Raw: body, Header: hdr,
			Facts: facts,
		}, protocol, rec)
	}
}

// models lists all virtual models in a merged shape both OpenAI clients
// (object:"list", object:"model") and Anthropic clients (type:"model",
// has_more) can parse; extra keys are ignored by either side.
func (s *Server) models(w http.ResponseWriter, _ *http.Request) {
	snap := s.rt.Snapshot()
	type model struct {
		ID       string `json:"id"`
		Object   string `json:"object"`
		Type     string `json:"type"`
		OwnedBy  string `json:"owned_by"`
		Protocol string `json:"vmr_protocol"`
	}
	// A virtual model name can be registered under more than one ingress
	// protocol at once (one openai-completions endpoint group and one
	// anthropic-messages one sharing the same name, see config's
	// VirtualModel doc comment) — from a client's perspective that's still
	// one addressable model (it always calls it by the one name it was
	// configured with), so the id must appear exactly once here, not once
	// per protocol. Protocols are walked in sorted order so the reported
	// vmr_protocol for a name registered under more than one is
	// deterministic across requests, not whichever the map's random
	// iteration order happened to visit first.
	seen := make(map[string]bool)
	var list []model
	for _, protocol := range core.SortedKeys(snap.Models) {
		for _, name := range core.SortedKeys(snap.Models[protocol]) {
			if seen[name] {
				continue
			}
			seen[name] = true
			list = append(list, model{ID: name, Object: "model", Type: "model", OwnedBy: "vmr", Protocol: protocol})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	router.WriteJSON(w, http.StatusOK, map[string]any{"object": "list", "data": list, "has_more": false})
}
