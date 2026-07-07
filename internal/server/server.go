// Ver 2026-07-07 02:10, by Fable 5

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

	"vmr/internal/core"
	"vmr/internal/health"
	"vmr/internal/router"
)

type Server struct {
	rt *router.Router
}

func New(rt *router.Router) *Server { return &Server{rt: rt} }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.auth(s.chatHandler("openai")))
	mux.HandleFunc("POST /v1/messages", s.auth(s.chatHandler("anthropic")))
	mux.HandleFunc("GET /v1/models", s.auth(s.models))
	mux.HandleFunc("GET /admin/status", s.adminStatus)
	return mux
}

// auth enforces the router's own optional API key on /v1/*. Both credential
// conventions are accepted: Authorization: Bearer (OpenAI) and x-api-key
// (Anthropic SDKs send only this).
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := s.rt.Snapshot().Cfg.APIKey
		if key != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if got == "" {
				got = r.Header.Get("x-api-key")
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
				writeError(w, http.StatusUnauthorized, "authentication_error", "invalid or missing API key")
				return
			}
		}
		next(w, r)
	}
}

// protocolHeaders is the whitelist of client headers forwarded to adapters.
var protocolHeaders = []string{"anthropic-version", "anthropic-beta"}

func (s *Server) chatHandler(protocol string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body exceeds limit")
			} else {
				writeError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
			}
			return
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

		hdr := http.Header{}
		for _, name := range protocolHeaders {
			if v := r.Header.Get(name); v != "" {
				hdr.Set(name, v)
			}
		}

		s.rt.Serve(w, r, &core.CanonicalRequest{Model: probe.Model, Stream: probe.Stream, Raw: body, Header: hdr}, protocol)
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
	list := make([]model, 0, len(snap.Models))
	for name, route := range snap.Models {
		list = append(list, model{ID: name, Object: "model", Type: "model", OwnedBy: "vmr", Protocol: route.Protocol})
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
	for name, route := range snap.Models {
		for _, ep := range route.Endpoints {
			out[name] = append(out[name], epStatus{
				Endpoint: ep.Name(),
				Protocol: route.Protocol,
				Priority: ep.Priority,
				Status:   s.rt.Health.Status(ep.HealthKey(), now),
			})
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
