// Ver 2026-07-26, by Sonnet 5
package router

import (
	"net/http"
	"strings"
)

// headerBlocklist defines client headers stripped before proxying to upstream endpoints
// to prevent security leaks and transport protocol conflicts.
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
	// response normalizer (internal/respnorm) would run its
	// regexes over gzip bytes, and the client would receive them without a
	// Content-Encoding header (only Content-Type is forwarded back).
	// Blocking it lets the Transport negotiate gzip itself and hand every
	// layer plaintext.
	"accept-encoding": {},
	// Internal routing-pin headers (vmr smoke, manual debugging): consumed
	// by router.Serve via r.Header (see pin.go) and never forwarded — an
	// internal control header must not leak to an upstream as if it were
	// client metadata.
	"x-vmr-provider":     {},
	"x-vmr-target-model": {},
}

// FilterClientHeaders returns a copy of h with headerBlocklist entries
// removed — applied to every live request (internal/server.chatHandler)
// before headers reach an adapter, and reused by internal/replay to
// reconstruct the header set a live request would have carried when
// rebuilding one from an audit record.
//
// Lives here, not in internal/core: deciding what reaches an upstream is
// routing-half behavior, and both callers (server, replay) are routing-half
// packages that already depend on this one. It sat in core only because
// core was the first place both could reach without importing the HTTP
// server — the same reasoning that put WriteJSON/WriteError there, and the
// same reason both moved out once B5 wrote core's admission rule down (see
// core's package comment: shared TYPES, not behavior with a real owner).
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
