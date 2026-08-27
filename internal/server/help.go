// Ver 2026-09-14, by Gemini

// The help surface: /help and /help.html (English), /help.zh and
// /help.zh.html (中文). Split out of server.go the same way status_page.go
// holds the dashboard: this is a static, self-contained page embedding zero
// business data, so it is served unauthenticated (like status.html /
// log.html). The embedded JS opens GET /status itself (which enforces
// s.auth) to fill in the live virtual-model list, probing first so a server
// with no api_keys configured just shows the data (no key prompt at all)
// and one with auth prompts on a click, sharing the dashboard's
// localStorage key so an operator who already unlocked /status.html sees
// live values here with no second login.
//
// The two language variants are plain embedded files (help.html /
// help.zh.html), each carrying a header link to the other — no runtime
// language table, no query-param state. This mirrors the repo's established
// .zh-sibling convention (README.zh.md, config.example.zh.yaml) and is
// deliberately bounded at two languages: adding a third means adding a
// third embedded file and two more routes, nothing else.
package server

import (
	"bytes"
	_ "embed"
	"html"
	"net/http"
	"strings"

	"vmr/internal/core"
)

//go:embed help.html
var helpHTMLPage []byte

//go:embed help.zh.html
var helpZHHTMLPage []byte

// helpPageEN / helpPageZH are the two language variants of the agent
// configuration guide. Deliberately unauthenticated: the page contains only
// static setup instructions; the virtual-model list is fetched client-side
// from the auth-gated /status endpoint.
func (s *Server) helpPageEN(w http.ResponseWriter, r *http.Request) {
	s.renderHelp(w, r, helpHTMLPage)
}

func (s *Server) helpPageZH(w http.ResponseWriter, r *http.Request) {
	s.renderHelp(w, r, helpZHHTMLPage)
}

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
func (s *Server) renderHelp(w http.ResponseWriter, r *http.Request, page []byte) {
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
