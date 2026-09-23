// Ver 2026-09-23 02:55, by pi

// The help surface: /help and /help.html (English), /help.zh and
// /help.zh.html (中文). Split out of server.go the same way status_page.go
// holds the dashboard: the page carries only setup instructions and fills
// its live data client-side, so it is served unauthenticated (like
// status.html / log.html). The page script opens GET /status itself (which
// enforces s.auth) through the shared VMRAuth.guard — a server with no
// api_keys configured just shows the data (no key prompt at all), and one
// with auth opens the shared key modal on 401 and retries once.
//
// One embedded template serves both languages (help.html; see
// helpstrings.go for the EN/ZH text tables it is filled from). The routes
// stay language-addressed — no runtime negotiation, no query-param state —
// and the two pages cross-link to each other. This keeps the .zh-sibling
// convention of README.zh.md / config.example.zh.yaml while bounding a
// third language to one more text table and nothing else.
//
// Shared console chrome (console.css / console.js) is injected once at
// first serve via assembleConsolePage (see the console contract);
// the page carries the {{CONSOLE_CSS}} / {{CONSOLE_JS}} markers.
package server

import (
	"bytes"
	_ "embed"
	"html"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"

	"vmr/internal/core"
)

//go:embed help.html
var helpHTMLTemplate []byte

var (
	helpAssembleOnce sync.Once
	helpPageEN       []byte
	helpPageZH       []byte
)

// assembledHelpPages bakes the shared console assets and the per-language
// text table into the help template exactly once per process. The result is
// static for the process lifetime and only read afterwards (per-request
// base-URL substitution below allocates fresh slices and never mutates
// these).
func assembledHelpPages() (en, zh []byte) {
	helpAssembleOnce.Do(func() {
		tpl := assembleConsolePage(helpHTMLTemplate)
		helpPageEN = applyHelpStrings(tpl, helpStringsEN)
		helpPageZH = applyHelpStrings(tpl, helpStringsZH)
	})
	return helpPageEN, helpPageZH
}

// applyHelpStrings replaces every {{T:key}} token in the page with the
// language table's value. Keys are sorted so the replacer's pair order (and
// therefore the output) is deterministic regardless of map iteration order.
// No {{T:...}} token may survive: TestHelpPage_NoUnresolvedTokens pins that
// every template token exists in both tables.
func applyHelpStrings(page []byte, table map[string]string) []byte {
	pairs := make([]string, 0, 2*len(table))
	for _, key := range slices.Sorted(maps.Keys(table)) {
		pairs = append(pairs, "{{T:"+key+"}}", table[key])
	}
	return []byte(strings.NewReplacer(pairs...).Replace(string(page)))
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
