// Ver 2026-09-15, by pi

// The help surface: /help and /help.html (English), /help.zh and
// /help.zh.html (中文). Split out of server.go the same way status_page.go
// holds the dashboard: the page carries only setup instructions and fills
// its live data client-side, so it is served unauthenticated (like
// status.html / log.html). The page script opens GET /status itself (which
// enforces s.auth) through the shared VMRAuth.guard — a server with no
// api_keys configured just shows the data (no key prompt at all), and one
// with auth opens the shared key modal on 401 and retries once.
//
// The two language variants are plain embedded files (help.html /
// help.zh.html), each carrying a link to the other — no runtime language
// table, no query-param state. This mirrors the repo's established
// .zh-sibling convention (README.zh.md, config.example.zh.yaml) and is
// deliberately bounded at two languages: adding a third means adding a
// third embedded file and two more routes, nothing else.
//
// Shared console chrome (console.css / console.js) is injected once at
// first serve via assembleConsolePage (contracts.md §4); the page files
// carry the {{CONSOLE_CSS}} / {{CONSOLE_JS}} markers.
package server

import (
	"bytes"
	_ "embed"
	"html"
	"net/http"
	"strings"
	"sync"

	"vmr/internal/core"
)

//go:embed help.html
var helpHTMLPage []byte

//go:embed help.zh.html
var helpZHHTMLPage []byte

var (
	helpAssembleOnce sync.Once
	helpPageEN       []byte
	helpPageZH       []byte
)

// assembledHelpPages bakes the shared console assets into both help pages
// exactly once per process. The assets are static for the process lifetime,
// so the result is computed once and only read afterwards (per-request
// base-URL substitution below allocates fresh slices and never mutates
// these).
func assembledHelpPages() (en, zh []byte) {
	helpAssembleOnce.Do(func() {
		helpPageEN = assembleConsolePage(helpHTMLPage)
		helpPageZH = assembleConsolePage(helpZHHTMLPage)
	})
	return helpPageEN, helpPageZH
}

// helpPageEN / helpPageZH are the two language variants of the agent
// configuration guide. Deliberately unauthenticated: the page contains only
// static setup instructions; the virtual-model list, connection check and
// alerts are fetched client-side from the auth-gated /status endpoint.
func (s *Server) helpPageEN(w http.ResponseWriter, r *http.Request) {
	s.renderHelp(w, r, helpPageLanguageEN)
}

func (s *Server) helpPageZH(w http.ResponseWriter, r *http.Request) {
	s.renderHelp(w, r, helpPageLanguageZH)
}

type helpPageLanguage bool

const (
	helpPageLanguageEN helpPageLanguage = true
	helpPageLanguageZH helpPageLanguage = false
)

// renderHelp serves one language variant of the guide.
//
// The {{BASE_URL_OPENAI}} / {{BASE_URL_ANTHROPIC}} placeholders are baked
// into real values here, derived from the request's own Host the same way
// /status derives instance.base_urls — so a visitor who reached
// http://192.168.0.32:8800/help.html sees that exact address in every
// snippet and in the connection card, with no HOST:PORT mental substitution,
// and the page stays correct even when copied or viewed without JavaScript.
// The embedded JS repeats the fill from window.location.origin (the URL the
// visitor's browser actually shows), which wins when a proxy rewrites Host.
func (s *Server) renderHelp(w http.ResponseWriter, r *http.Request, lang helpPageLanguage) {
	en, zh := assembledHelpPages()
	page := en
	if !lang {
		page = zh
	}
	baseURLs := instanceBaseURLs(requestScheme(r), r.Host)
	openai := strings.TrimSuffix(baseURLs[core.ProtocolOpenAICompletions], "/")
	anthropic := strings.TrimSuffix(strings.TrimSuffix(baseURLs[core.ProtocolAnthropicMessages], "/"), "/v1")
	out := bytes.ReplaceAll(page, []byte("{{BASE_URL_OPENAI}}"), []byte(html.EscapeString(openai)))
	out = bytes.ReplaceAll(out, []byte("{{BASE_URL_ANTHROPIC}}"), []byte(html.EscapeString(anthropic)))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(out)
}
