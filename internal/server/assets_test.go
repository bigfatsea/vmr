// Ver 2026-09-14, by pi

package server

import (
	"bytes"
	"strings"
	"testing"
)

// TestAssembleConsolePage_ReplacesMarkers pins the core contract: a page
// carrying both injection markers comes back with the shared assets inlined
// and no marker residue (a surviving marker would ship dead comments that
// silently diverge from the real asset).
func TestAssembleConsolePage_ReplacesMarkers(t *testing.T) {
	page := []byte("<style>/*{{CONSOLE_CSS}}*/</style><script>/*{{CONSOLE_JS}}*/</script>")
	out := assembleConsolePage(page)
	if !bytes.Contains(out, []byte("--bg:#0d1117")) {
		t.Error("console.css not injected at the CSS marker")
	}
	if !bytes.Contains(out, []byte("function mountConsole")) {
		t.Error("console.js not injected at the JS marker")
	}
	if bytes.Contains(out, []byte("{{CONSOLE_")) {
		t.Errorf("injection marker survived assembly: %q", snippet(out, "{{CONSOLE_"))
	}
}

// TestAssembleConsolePage_NoMarkerPassthrough: pages without markers (the
// existing self-contained pages) must come back byte-identical.
func TestAssembleConsolePage_NoMarkerPassthrough(t *testing.T) {
	page := []byte("<html><style>body{color:red}</style><script>var x=1;</script></html>")
	out := assembleConsolePage(page)
	if !bytes.Equal(out, page) {
		t.Error("marker-free input was modified — old pages must pass through untouched")
	}
}

// TestConsoleAssets_NonEmpty guards against an emptied asset still passing
// the replace test trivially.
func TestConsoleAssets_NonEmpty(t *testing.T) {
	loadConsoleAssets()
	if len(consoleCSS) < 1024 {
		t.Fatalf("console.css suspiciously small: %d bytes", len(consoleCSS))
	}
	if len(consoleJS) < 1024 {
		t.Fatalf("console.js suspiciously small: %d bytes", len(consoleJS))
	}
}

// TestConsoleAssets_NoPrivateStyles: page-private styles (demo terminal,
// chart, snippets, demo chrome) must not leak into the shared asset.
func TestConsoleAssets_NoPrivateStyles(t *testing.T) {
	loadConsoleAssets()
	for _, private := range []string{".chart-", ".demo-ribbon", ".demo-panel", "#log", "#jump", "pre.snippet", ".lvl{", "#conn-banner"} {
		if bytes.Contains(consoleCSS, []byte(private)) {
			t.Errorf("page-private selector %q leaked into shared console.css", private)
		}
	}
}

// TestConsoleJS_APIs pins the frozen contracts §4 API surface by string
// presence — the page tasks consume these names verbatim.
func TestConsoleJS_APIs(t *testing.T) {
	loadConsoleAssets()
	js := string(consoleJS)
	for _, want := range []string{
		"function mountConsole", // skeleton injection
		"VMRAuth",               // auth singleton
		"'vmr_key'",             // storage key
		"'vmr_status_key'",      // legacy key migrated
		"async guard(doFetch)",  // 401 flow entry
		"function openOverlay", "function closeOverlay", "function wireOverlay",
		"ConsoleAlerts",  // alerts pill + modal
		"setStreamState", // stream slot: streaming/paused/down
		"function dec2", "function fmtKMG", "function fmtBytes", "function fmtPct", "function fmtHeadroom",
		"function fmtInt", "function toast", // formatting & feedback
	} {
		if !strings.Contains(js, want) {
			t.Errorf("console.js missing §4 API %q", want)
		}
	}
}

// TestConsoleCSS_TokensAndComponents spot-checks the §3 token list and the
// component classes the design names explicitly.
func TestConsoleCSS_TokensAndComponents(t *testing.T) {
	loadConsoleAssets()
	css := string(consoleCSS)
	for _, want := range []string{
		"--bg:#0d1117", "--surface:#161b22", "--surface-2:#1c2128", "--border:#30363d",
		"--text:#c9d1d9", "--text-bright:#f0f6fc", "--accent:#58a6ff",
		"--green:#3fb950", "--yellow:#d29922", "--red:#f85149", "--purple:#bc8cff", "--cyan:#39c5cf",
		"--orange:#e8813a", "--pink:#f778ba", "--font-mono:", "--font-sans:",
		"--fs-md:13px", "--r-pill:999px", "--page-max:1280px", "--hd-h:88px",
		".badge{", ".pill{", ".seg{", ".modal-overlay{", ".t-ok{", ".refresh-pill{",
		".refresh-pill.paused{", ".vitals{", ".ep{", ".zero td{", ".mode{",
		".p-live{", ".p-connecting{", ".p-down{", ".p-paused{", // four pill states, all defined
	} {
		if !strings.Contains(css, want) {
			t.Errorf("console.css missing token/component %q", want)
		}
	}
}

// snippet returns the 40 bytes at the first occurrence of needle, for
// error messages only.
func snippet(b []byte, needle string) string {
	i := bytes.Index(b, []byte(needle))
	if i < 0 {
		return ""
	}
	j := i + len(needle) + 40
	if j > len(b) {
		j = len(b)
	}
	return string(b[i:j])
}
