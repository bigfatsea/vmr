// Ver 2026-08-23 15:47, by Gemini
package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vmr/internal/config"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

func TestStatusPage_ServesHTML(t *testing.T) {
	cfg, err := config.Parse([]byte(instanceYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Install(snap)
	srv := New(rt, nil)

	req := httptest.NewRequest("GET", "/status.html", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}

	cc := w.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}

	body := w.Body.String()
	if !strings.Contains(body, "VMR Dashboard") {
		t.Errorf("body missing 'VMR Dashboard'")
	}
	if !strings.Contains(body, "renderDashboard") {
		t.Errorf("body missing 'renderDashboard'")
	}
	if !strings.Contains(body, "fetchStatus") {
		t.Errorf("body missing 'fetchStatus'")
	}
	if !strings.Contains(body, `href="/log.html"`) {
		t.Errorf("body missing cross-link to /log.html")
	}
}
