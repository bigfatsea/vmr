// Ver 2026-08-23 15:45, by Gemini
package server

import (
	_ "embed"
	"net/http"
)

//go:embed status.html
var statusHTMLPage []byte

// statusPage serves the self-contained static HTML dashboard.
// Deliberately unauthenticated: the HTML/JS shell contains zero business
// or configuration data. The embedded JS immediately calls GET /status,
// which enforces s.auth() and prompts for credentials if required.
func (s *Server) statusPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(statusHTMLPage)
}
