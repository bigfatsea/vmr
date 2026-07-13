// Ver 2026-07-13 02:00, by Fable 5

// Package server is the HTTP surface: auth, /v1/chat/completions, /v1/models,
// /admin/status. Anything else is 404.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
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

// checkAuth enforces the router's own optional API key. Both credential
// conventions are accepted: Authorization: Bearer (OpenAI) and x-api-key
// (Anthropic SDKs send only this).
func (s *Server) checkAuth(r *http.Request) bool {
	key := s.rt.Snapshot().Cfg.APIKey
	if key == "" {
		return true
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		got = r.Header.Get("x-api-key")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(key)) == 1
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(r) {
			writeError(w, http.StatusUnauthorized, "authentication_error", "invalid or missing API key")
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
				switch {
				case rw.status == 0 && r.Context().Err() != nil:
					rec.Outcome = "canceled"
				case rw.status < 400:
					rec.Outcome = "ok"
				default:
					rec.Outcome = "error"
				}
				if err := s.audit.Write(rec); err != nil {
					log.Printf("audit: %v", err)
				}
			}()
		}

		if !s.checkAuth(r) {
			writeError(w, http.StatusUnauthorized, "authentication_error", "invalid or missing API key")
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
				writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body exceeds limit")
			} else {
				writeError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
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
			writeError(w, http.StatusBadRequest, "invalid_request_error", "request body is not valid JSON")
			return
		}
		if probe.Model == "" {
			writeError(w, http.StatusBadRequest, "invalid_request_error", "missing required field: model")
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
		// global default (§7). Metadata is always collected (n<=0 just
		// skips the resize/cache path inside imgprep) so the audit trail
		// describes every request's images regardless of compression
		// settings; enabled-but-no-image requests still cost only one
		// cheap substring scan (imgprep.HasImageMarker).
		route := snap.Models[protocol][probe.Model]
		n := route.EffectiveImageDownscaleMaxPx(snap.Cfg.ImageDownscaleMaxPx)
		var images []imgprep.ImageInfo
		// Skip entirely when there is no consumer: image metadata only goes
		// into the audit record, so with auditing off and downscaling
		// disabled the scan/parse would be pure waste.
		if rec != nil || n > 0 {
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
		// includes the Anthropic protocol headers (anthropic-version,
		// anthropic-beta). The rationale is that LLM SDKs (OpenAI JS,
		// Anthropic) only emit a known fixed set of headers and none of
		// them are dangerous, so a strict whitelist is more brittle than
		// necessary — it strips legitimate metadata (User-Agent,
		// X-Stainless-*, Traceparent) and needs code updates when SDKs
		// add new headers.
		hdr := http.Header{}
		for k, vs := range r.Header {
			if _, blocked := headerBlocklist[strings.ToLower(k)]; blocked {
				continue
			}
			for _, v := range vs {
				hdr.Add(k, v)
			}
		}

		s.rt.Serve(w, r, &core.CanonicalRequest{Model: probe.Model, Stream: probe.Stream, Raw: body, Header: hdr}, protocol, rec)
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
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": list, "has_more": false})
}

// adminStatus reports per-endpoint health. Loopback callers only.
func (s *Server) adminStatus(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		writeError(w, http.StatusForbidden, "permission_error", "admin endpoints are loopback-only")
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
	writeJSON(w, http.StatusOK, map[string]any{
		"models": out,
		"concurrency": map[string]any{
			"limit": limit, "in_flight": inFlight, "waiting": waiting,
		},
		"time": now,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError emits an error body both OpenAI clients (error.message) and
// Anthropic clients (type:"error" envelope) can parse.
func writeError(w http.ResponseWriter, status int, errType, msg string) {
	writeJSON(w, status, map[string]any{
		"type":  "error",
		"error": map[string]string{"type": errType, "message": msg},
	})
}
