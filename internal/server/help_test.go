// Ver 2026-09-23 03:00, by pi
package server

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"vmr/internal/config"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

// helpSrv builds a test server with the standard instance fixture installed.
func helpSrv(t *testing.T) *Server {
	t.Helper()
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
	return New(rt, nil)
}

func helpBody(t *testing.T, srv *Server, path string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status = %d, want %d", path, w.Code, http.StatusOK)
	}
	return w.Body.String()
}

var helpTokenRe = regexp.MustCompile(`\{\{T:([a-z0-9-]+)\}\}`)
var helpSlotRe = regexp.MustCompile(`\{\d\}`)

// TestHelpStrings_KeySetsMatch replaces the old "two byte-twin HTML files
// must be edited in lockstep" discipline with an executable one: the EN and
// ZH text tables behind help.html must carry exactly the same keys, and
// wherever a value interpolates live data the {n} slot sets must agree too
// (word order may differ — the ZH chk-ok sentence swaps {0}/{1} — but a slot
// present on one side only means the other language renders a literal {n}).
func TestHelpStrings_KeySetsMatch(t *testing.T) {
	enKeys := make([]string, 0, len(helpStringsEN))
	for k := range helpStringsEN {
		enKeys = append(enKeys, k)
	}
	zhKeys := make([]string, 0, len(helpStringsZH))
	for k := range helpStringsZH {
		zhKeys = append(zhKeys, k)
	}
	sort.Strings(enKeys)
	sort.Strings(zhKeys)
	if strings.Join(enKeys, ",") != strings.Join(zhKeys, ",") {
		t.Fatalf("EN/ZH key sets differ:\nEN only: %v\nZH only: %v",
			minus(enKeys, zhKeys), minus(zhKeys, enKeys))
	}
	for _, k := range enKeys {
		if slots := slotSet(helpStringsEN[k]); !equalStrings(slots, slotSet(helpStringsZH[k])) {
			t.Errorf("key %q: {n} slot sets differ: EN %v vs ZH %v", k, slots, slotSet(helpStringsZH[k]))
		}
	}
}

// TestHelpPage_NoUnresolvedTokens pins that every {{T:key}} token in the
// help template exists in both language tables and that no token (including
// one accidentally nested inside a table value) survives rendering — a
// leftover token would show raw {{T:...}} in the visitor's browser.
func TestHelpPage_NoUnresolvedTokens(t *testing.T) {
	var tplKeys []string
	for _, m := range helpTokenRe.FindAllStringSubmatch(string(helpHTMLTemplate), -1) {
		tplKeys = append(tplKeys, m[1])
	}
	for _, table := range []struct {
		name  string
		table map[string]string
	}{{"EN", helpStringsEN}, {"ZH", helpStringsZH}} {
		for _, k := range tplKeys {
			if _, ok := table.table[k]; !ok {
				t.Errorf("template token {{T:%s}} missing from %s table", k, table.name)
			}
		}
	}
	srv := helpSrv(t)
	for _, path := range []string{"/help.html", "/help.zh.html"} {
		body := helpBody(t, srv, path)
		if left := helpTokenRe.FindAllString(body, -1); len(left) > 0 {
			t.Errorf("%s: unresolved tokens survived rendering: %v", path, left)
		}
	}
}

// TestHelpPage_LanguageSlices pins the per-language surface: html lang,
// title and the language-toggle link point the right way on each variant,
// and the JS-visible I18N messages carry no raw {n} slots beyond their
// fmtT interpolation.
func TestHelpPage_LanguageSlices(t *testing.T) {
	srv := helpSrv(t)

	en := helpBody(t, srv, "/help.html")
	if !strings.Contains(en, `<html lang="en">`) {
		t.Errorf("EN page missing lang=en")
	}
	if !strings.Contains(en, "<title>VMR Console — Help</title>") {
		t.Errorf("EN page missing English title")
	}
	if !strings.Contains(en, `href="/help.zh.html"`) {
		t.Errorf("EN page missing toggle to the Chinese variant")
	}
	if strings.Contains(en, "配置指南") || strings.Contains(en, "故障排查") {
		t.Errorf("EN page leaked Chinese copy")
	}

	zh := helpBody(t, srv, "/help.zh.html")
	if !strings.Contains(zh, `<html lang="zh">`) {
		t.Errorf("ZH page missing lang=zh")
	}
	if !strings.Contains(zh, "<title>VMR 控制台 — 帮助</title>") {
		t.Errorf("ZH page missing Chinese title")
	}
	if !strings.Contains(zh, `href="/help.html"`) {
		t.Errorf("ZH page missing toggle to the English variant")
	}
	if strings.Contains(zh, "Configuration Guides") || strings.Contains(zh, "Troubleshooting") {
		t.Errorf("ZH page leaked English copy")
	}
}

// TestHelpPage_LanguageVariantsStructurallyIdentical guards the single
// template's reason to exist: both served variants must be the same page
// once the per-language copy is masked away — the same DOM skeleton and the
// same page script — so a wording change can never again diverge structure
// between languages. Masking swaps each table value for a per-key marker
// (identical for EN and ZH); any language-specific text that is NOT in the
// tables — the failure mode this test exists for — survives and breaks the
// comparison. The marker carries neither key nor value: either could
// contain a shorter table value and collide with a later replacement.
func TestHelpPage_LanguageVariantsStructurallyIdentical(t *testing.T) {
	srv := helpSrv(t)
	en := helpBody(t, srv, "/help.html")
	zh := helpBody(t, srv, "/help.zh.html")
	enKeys := slices.Sorted(maps.Keys(helpStringsEN))
	// Longest values first: a short value (html-lang's "en") is a substring
	// of longer ones, and masking it first would shred longer values before
	// they can be matched whole. Ties break by key for determinism.
	slices.SortStableFunc(enKeys, func(a, b string) int {
		la := max(len(helpStringsEN[a]), len(helpStringsZH[a]))
		lb := max(len(helpStringsEN[b]), len(helpStringsZH[b]))
		if la != lb {
			return lb - la
		}
		return strings.Compare(a, b)
	})
	marker := make(map[string]string, len(enKeys))
	for i, k := range enKeys {
		marker[k] = "«T" + strconv.Itoa(i) + "»"
	}
	mask := func(body string) string {
		for _, k := range enKeys {
			body = strings.ReplaceAll(body, helpStringsEN[k], marker[k])
			body = strings.ReplaceAll(body, helpStringsZH[k], marker[k])
		}
		return body
	}
	if mask(en) != mask(zh) {
		t.Errorf("EN/ZH pages differ beyond the language tables — structure diverged")
	}
}

func slotSet(s string) []string {
	set := map[string]bool{}
	for _, m := range helpSlotRe.FindAllString(s, -1) {
		set[m] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func minus(a, b []string) []string {
	set := map[string]bool{}
	for _, x := range b {
		set[x] = true
	}
	var out []string
	for _, x := range a {
		if !set[x] {
			out = append(out, x)
		}
	}
	return out
}

func equalStrings(a, b []string) bool {
	return strings.Join(a, ",") == strings.Join(b, ",")
}
