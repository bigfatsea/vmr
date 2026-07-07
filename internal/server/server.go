// Ver 2026-07-07 17:45, by Fable 5

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

// protocolHeaders is the whitelist of client headers forwarded to adapters
// for their protocol-semantic value (Anthropic version negotiation). All
// other client headers are passed through unless they appear in
// headerBlocklist — see chatHandler for the forwarding loop.
var protocolHeaders = []string{"anthropic-version", "anthropic-beta"}

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
			rw := newRecorder(w)
			w = rw
			defer func() {
				rec.DurMS = time.Since(rec.TS).Milliseconds()
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

		// Global concurrency gate: excess requests park here until a slot
		// frees, or the client goes away.
		release, ok := s.rt.AcquireSlot(r.Context())
		if !ok {
			return // client canceled while waiting; nothing to write
		}
		defer release()

		snap := s.rt.Snapshot()

		// Buffer the whole body up front (streaming included): failover replay needs it.
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, snap.Cfg.MaxBodyBytes()))
		if rec != nil {
			rec.Client.Request.Body, rec.Client.Request.BodyTruncated = audit.EncodeBody(body)
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

		// Request-only image downscaling: shrinks oversized inline
		// attachments before routing so vision-token cost doesn't scale
		// with screenshot resolution. Response bodies are never touched.
		// Disabled (n<=0) is a single int comparison; enabled-but-no-image
		// requests cost one cheap substring scan (imgprep.HasImageMarker).
		if n := snap.Cfg.ImageDownscaleMaxPx; n > 0 {
			body = imgprep.Downscale(body, protocol, n)
		}

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

		hdr := http.Header{}
		// Protocol-semantic headers (Anthropic version/beta) come first
		// and are always passed through.
		for _, name := range protocolHeaders {
			if v := r.Header.Get(name); v != "" {
				hdr.Set(name, v)
			}
		}
		// Everything else: pass through unless on the blocklist. The
		// rationale is that LLM SDKs (OpenAI JS, Anthropic) only emit a
		// known fixed set of headers and none of them are dangerous, so
		// a strict whitelist is more brittle than necessary — it strips
		// legitimate metadata (User-Agent, X-Stainless-*, Traceparent)
		// and needs code updates when SDKs add new headers.
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
	out := map[string][]epStatus{}
	for protocol, byName := range snap.Models {
		for name, route := range byName {
			for _, ep := range route.Endpoints {
				out[name] = append(out[name], epStatus{
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
