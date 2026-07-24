// Ver 2026-07-24 12:35, by Sonnet 5

// Package server is the HTTP surface: auth, /v1/chat/completions, /v1/models,
// /admin/status. Anything else is 404.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"vmr/internal/audit"
	"vmr/internal/core"
	"vmr/internal/health"
	"vmr/internal/imgprep"
	"vmr/internal/router"
)

type Server struct {
	rt    *router.Router
	audit *audit.Logger // nil = auditing disabled
}

func New(rt *router.Router, auditLog *audit.Logger) *Server {
	return &Server{rt: rt, audit: auditLog}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.chatHandler("openai"))
	mux.HandleFunc("POST /v1/messages", s.chatHandler("anthropic"))
	mux.HandleFunc("GET /v1/models", s.auth(s.models))
	mux.HandleFunc("GET /admin/status", s.adminStatus)
	return mux
}

// authenticate enforces the router's own optional API keys and reports
// which one matched. Both credential conventions are accepted:
// Authorization: Bearer (OpenAI) and x-api-key (Anthropic SDKs send only
// this). tag is audit.KeyTag(the matched Cfg.APIKeys entry) — "" when auth
// is disabled entirely or (ok == false) nothing matched at all. api_keys is
// the only auth surface (the singular api_key catch-all was removed: it
// added nothing this list can't express, at the cost of a second code path
// here and a second config field to document — config.validate rejects it
// with a migration message).
//
// Self-declared tag, no config needed: when APIKeys is empty, the door
// stays fully open, but whatever credential-shaped value the client chooses
// to send still gets KeyTag-derived and recorded — a private-network caller
// can identify itself to `vmr report` just by ending its own
// Authorization/x-api-key value in "-<label>", with zero vmr-side config.
// A client sending nothing still gets "" (design doc §4.3/§9.4).
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
			core.WriteError(w, http.StatusUnauthorized, "authentication_error", "invalid or missing API key")
			return
		}
		next(w, r)
	}
}

// headerBlocklist is the set of client headers VMR never forwards to the
// upstream. The pass-through policy is the inverse of the prior strict
// whitelist: most client headers (User-Agent, X-Stainless-*, Traceparent,
// Accept-Language, etc.) are legitimate metadata and are forwarded as-is,
// so SDK upgrades and tracing work without VMR code changes. The items
// here are the ones that would cause a security or protocol-correctness
// problem if leaked to the upstream.
//
// "Must override" headers (Authorization, Host, Content-Length, etc.) are
// also listed here as a defense in depth, but the primary mechanism is
// adapter.BuildRequest using httpReq.Header.Set to replace them.
var headerBlocklist = map[string]struct{}{
	"authorization":       {}, // credential — adapter injects its own
	"x-api-key":           {}, // Anthropic credential — same reason
	"cookie":              {}, // browser/session state — never belongs in LLM API
	"x-forwarded-for":     {},
	"x-forwarded-proto":   {},
	"x-forwarded-host":    {},
	"x-real-ip":           {},
	"proxy-authorization": {},
	"host":                {}, // Go http.Request.Host follows URL, but block anyway
	"content-length":      {}, // Go Transport recomputes
	"transfer-encoding":   {}, // Go Transport manages
	"connection":          {}, // Go Transport manages
	// Forwarding the client's Accept-Encoding disables Go Transport's
	// transparent gzip: the upstream may then answer compressed, the
	// response normalizer (response.go) would run its regexes over gzip
	// bytes, and the client would receive them without a Content-Encoding
	// header (only Content-Type is forwarded back). Blocking it lets the
	// Transport negotiate gzip itself and hand every layer plaintext.
	"accept-encoding": {},
}

// FilterClientHeaders returns a copy of h with headerBlocklist entries
// removed — the same filtering chatHandler applies to a live request before
// handing headers to an adapter. Exported so `vmr replay` (internal/replay)
// can reconstruct the exact header set a live request would have carried
// when rebuilding one from an audit record.
func FilterClientHeaders(h http.Header) http.Header {
	out := http.Header{}
	for k, vs := range h {
		if _, blocked := headerBlocklist[strings.ToLower(k)]; blocked {
			continue
		}
		for _, v := range vs {
			out.Add(k, v)
		}
	}
	return out
}

func (s *Server) chatHandler(protocol string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
				canceled := rw.status == 0 && r.Context().Err() != nil
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
			core.WriteError(w, http.StatusUnauthorized, "authentication_error", "invalid or missing API key")
			return
		}

		snap := s.rt.Snapshot()

		// Buffer the whole body up front (streaming included): failover replay needs it.
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, snap.Cfg.MaxRequestBodyBytes()))
		if rec != nil {
			rec.Client.Request.Body = audit.EncodeBody(body)
		}
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				core.WriteError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body exceeds limit")
			} else {
				core.WriteError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
			}
			return
		}

		// Probe the routing fields before acquiring the concurrency gate or
		// downscaling images: both of the latter are per-model (image
		// downscale can be overridden per virtual model, §7) and cheap JSON
		// parsing shouldn't wait behind either.
		var probe struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.Unmarshal(body, &probe); err != nil {
			core.WriteError(w, http.StatusBadRequest, "invalid_request_error", "request body is not valid JSON")
			return
		}
		if probe.Model == "" {
			core.WriteError(w, http.StatusBadRequest, "invalid_request_error", "missing required field: model")
			return
		}
		if rec != nil {
			rec.Model, rec.Stream = probe.Model, probe.Stream
		}

		// Global concurrency gate: excess requests park here until a slot
		// frees, or the client goes away. Acquired AFTER the body is
		// buffered so a slow upload never occupies a slot — the gate
		// covers the CPU-and-upstream phase (image downscaling included),
		// not client I/O.
		release, ok := s.rt.AcquireSlot(r.Context())
		if !ok {
			return // client canceled while waiting; nothing to write
		}
		defer release()

		// Request-only image downscaling: shrinks oversized inline
		// attachments before routing so vision-token cost doesn't scale
		// with screenshot resolution. Response bodies are never touched.
		// The effective cap is the virtual model's own override if it set
		// one (even 0, which force-disables it for that model), else the
		// global default (§7). Detection (format/dimensions/count) always
		// runs regardless of that cap (n<=0 just skips the resize/cache path
		// inside imgprep) — a plain-text request with no image marker at all
		// still costs only one cheap substring scan (imgprep.HasImageMarker)
		// before Downscale returns.
		//
		// This one call is now the single source of truth for "does this
		// request have images" across the whole request, not just the audit
		// trail: images/len(images) feeds both rec.Images below AND
		// computeRequestFacts' imageCount argument, which drives
		// RequestFacts.HasImage — consulted by a hard capability Condition
		// (internal/strategy/conditions.go) with no fallback — and the image
		// portion of EstimatedTokens. Neither of those re-derives anything
		// from body; there is exactly one image-detection pass per request,
		// reused three ways, so a request with no image never pays for a
		// second scan. That reuse is also why this call can no longer be
		// skipped when rec==nil && n<=0 (as it once was, purely as an audit-
		// metadata cost optimization): routing correctness now depends on
		// it. The condition needs the real, structurally-detected answer
		// (imgprep walks actual message/content-block shapes) — a plain-text
		// request that merely quotes something like "image_downscale=512px"
		// must never be misrouted as needing image support, which is exactly
		// what the cheap imgprep.HasImageMarker byte-scan this replaced as
		// the routing signal would do.
		route := snap.Models[protocol][probe.Model]
		n := route.EffectiveImageDownscaleMaxPx(snap.Cfg.ImageDownscaleMaxPx)
		body, images := imgprep.Downscale(body, protocol, imgprep.Options{
			MaxPx:        n,
			CacheDir:     snap.Cfg.ImageCacheDir,
			CacheTTLDays: snap.Cfg.ImageCacheTTLDays,
		})
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
		// includes the Anthropic protocol headers (anthropic-version,
		// anthropic-beta). The rationale is that LLM SDKs (OpenAI JS,
		// Anthropic) only emit a known fixed set of headers and none of
		// them are dangerous, so a strict whitelist is more brittle than
		// necessary — it strips legitimate metadata (User-Agent,
		// X-Stainless-*, Traceparent) and needs code updates when SDKs
		// add new headers.
		hdr := FilterClientHeaders(r.Header)

		// Computed once, used twice: the routing layer consults it via
		// CanonicalRequest.Facts, and the audit trail records the exact
		// same value (rec.Facts) — never a second, independent computation
		// at write time. See audit.Record.Facts's doc comment for why it's
		// a sibling of Client.Request rather than folded into it.
		facts := computeRequestFacts(body, len(images))
		if rec != nil {
			rec.Facts = &facts
		}

		s.rt.Serve(w, r, &core.CanonicalRequest{
			Model: probe.Model, Stream: probe.Stream, Raw: body, Header: hdr,
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
	var list []model
	for protocol, byName := range snap.Models {
		for name := range byName {
			list = append(list, model{ID: name, Object: "model", Type: "model", OwnedBy: "vmr", Protocol: protocol})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	core.WriteJSON(w, http.StatusOK, map[string]any{"object": "list", "data": list, "has_more": false})
}

// adminStatus reports per-endpoint health. Loopback callers only.
func (s *Server) adminStatus(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		core.WriteError(w, http.StatusForbidden, "permission_error", "admin endpoints are loopback-only")
		return
	}
	snap := s.rt.Snapshot()
	now := time.Now()
	type epStatus struct {
		Endpoint string `json:"endpoint"`
		Protocol string `json:"protocol"`
		Priority int    `json:"priority"`
		health.Status
	}
	// Keyed "name [protocol]": the same virtual-model name may exist in
	// both protocol groups (§10), and mixing their endpoints under one
	// key reads as one model with double the endpoints.
	out := map[string][]epStatus{}
	for protocol, byName := range snap.Models {
		for name, route := range byName {
			key := name + " [" + protocol + "]"
			for _, ep := range route.Endpoints {
				out[key] = append(out[key], epStatus{
					Endpoint: ep.Name(),
					Protocol: protocol,
					Priority: ep.Priority,
					Status:   s.rt.Health.Status(ep.HealthKey(), now),
				})
			}
		}
	}
	limit, inFlight, waiting := s.rt.Concurrency()
	core.WriteJSON(w, http.StatusOK, map[string]any{
		"models": out,
		"concurrency": map[string]any{
			"limit": limit, "in_flight": inFlight, "waiting": waiting,
		},
		"time": now,
	})
}
