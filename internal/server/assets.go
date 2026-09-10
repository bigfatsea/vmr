// Ver 2026-09-14, by pi

// Shared console assets: one console.css and one console.js for all console
// pages (console-unification §7). Pages keep the injection markers
// /*{{CONSOLE_CSS}}*/ and /*{{CONSOLE_JS}}*/ in their embedded source;
// assembleConsolePage swaps them for the asset contents in one
// server-startup pass — never per request (same one-time pattern as
// internal/dashboard's WriteSkeletons).
package server

import (
	"bytes"
	"embed"
	"sync"
)

//go:embed assets/console.css assets/console.js
var consoleAssets embed.FS

var (
	consoleAssetsOnce sync.Once
	consoleCSS        []byte
	consoleJS         []byte
)

func loadConsoleAssets() {
	consoleAssetsOnce.Do(func() {
		// embed guarantees presence; a failure here is a build-time bug
		consoleCSS, _ = consoleAssets.ReadFile("assets/console.css")
		consoleJS, _ = consoleAssets.ReadFile("assets/console.js")
	})
}

// Injection markers a page carries where the shared asset belongs.
const (
	consoleCSSTag = "/*{{CONSOLE_CSS}}*/"
	consoleJSTag  = "/*{{CONSOLE_JS}}*/"
)

// assembleConsolePage returns page with the injection markers replaced by
// the shared asset contents. Input without any marker is returned as-is,
// byte-identical — pages predating the unified skeleton stay unaffected.
//
// Call it once per page at server start and hold the result; the assets are
// static for the lifetime of the process, so per-request assembly would be
// waste (and a data race on the sync.Once-cached buffers is avoided by
// never mutating the returned slices).
func assembleConsolePage(page []byte) []byte {
	if !bytes.Contains(page, []byte(consoleCSSTag)) && !bytes.Contains(page, []byte(consoleJSTag)) {
		return page
	}
	loadConsoleAssets()
	out := bytes.ReplaceAll(page, []byte(consoleCSSTag), consoleCSS)
	out = bytes.ReplaceAll(out, []byte(consoleJSTag), consoleJS)
	return out
}
