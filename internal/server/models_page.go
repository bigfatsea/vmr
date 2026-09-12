// Ver 2026-09-12, by Claude Sonnet 5
package server

import (
	_ "embed"
	"net/http"
	"sync"
)

//go:embed models.html
var modelsHTMLPage []byte

var (
	assembledModelsPage []byte
	assembleModelsOnce  sync.Once
)

func getModelsPage() []byte {
	assembleModelsOnce.Do(func() {
		assembledModelsPage = assembleConsolePage(modelsHTMLPage)
	})
	return assembledModelsPage
}

// modelsPage serves the Models dashboard (Quota Budgets + Virtual Models &
// Endpoint Topology). Deliberately unauthenticated, same as statusPage: the
// HTML/JS shell carries zero business or configuration data — the embedded
// JS calls GET /status and /stats, which enforce s.auth().
func (s *Server) modelsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(getModelsPage())
}
