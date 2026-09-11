// Ver 2026-08-23 15:45, by Gemini
package server

import (
	_ "embed"
	"net/http"
	"sync"
)

//go:embed status.html
var overviewHTMLPage []byte

var (
	assembledOverviewPage []byte
	assembleOverviewOnce  sync.Once
)

func getOverviewPage() []byte {
	assembleOverviewOnce.Do(func() {
		assembledOverviewPage = assembleConsolePage(overviewHTMLPage)
	})
	return assembledOverviewPage
}

// statusPage serves the unified Overview dashboard.
// Deliberately unauthenticated: the HTML/JS shell contains zero business
// or configuration data. The embedded JS calls GET /status and /stats,
// which enforce s.auth() and prompt for credentials if required.
func (s *Server) statusPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(getOverviewPage())
}
