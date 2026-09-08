// Ver 2026-09-06, by Claude

// Package dashboard delivers vmr analyze's static skeleton dashboard pages
// (§6 of the analyze architecture redesign doc): six self-contained HTML
// files with zero business data, written to the report output root on every
// analyze run. All rendering happens browser-side — the pages fetch
// relative-path JSON slices (manifest.json, macro/*.json, requests/,
// journeys/, compares/) and render DOM + inline SVG from them, so writing
// the JSON slices is what "delivers the dashboard". The Go side is embed +
// WriteSkeletons only; the bulk of the work lives in the embedded
// assets/*.html files (§6.2), whose line budget archtest deliberately does
// not track. Leaf package: stdlib only, zero vmr/internal dependencies.
package dashboard

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed assets
var assets embed.FS

// commonJSTag is the shared-runtime include every skeleton page carries in
// the embed source. WriteSkeletons replaces it with the inlined contents of
// assets/common.js so the written page is genuinely self-contained (§6.3):
// /reports/ only serves .html/.json/.jsonl/.md, and a page that 404s on a
// sibling .js renders nothing. common.js guards its Node export with
// `typeof module !== 'undefined'`, so inlining it into a browser <script> is
// safe.
const commonJSTag = `<script src="common.js"></script>`

// skeletonPages are the six skeleton HTML pages (§6.2), written flat into
// the report output root — never a subdirectory. Root placement is
// deliberate: a page that loads both journeys/ and compares/ slices would
// need ../-relative fetch paths from inside either one; at the root every
// #data= path matches the on-disk layout (§4) byte-for-byte. Each file is
// self-contained (inline CSS/JS): no extra .css/.js assets to serve, so the
// HTML skeletons stay auth-exempt (§6.5) while only .json/.jsonl/.md data
// requests carry the Bearer key.
var skeletonPages = []string{
	"macro-dashboard.html",
	"request-browser.html",
	"journey-viewer.html",
	"journey-compare.html",
	"benchmarks.html",
	"tool-waste.html",
}

// WriteSkeletons writes the six skeleton dashboard pages into dir's root,
// overwriting whatever a previous run (or the user) put there — the pages
// carry no business data, so overwrite is always safe, and idempotency is
// what keeps /reports/ serving pages in sync with the running binary (§6.6
// renders "stale skeleton + fresh JSON" a non-issue on the normal path).
// The shared common.js runtime is inlined into each page at write time so
// the written file has no sibling-asset dependency (see commonJSTag).
// dir is created (0700) if missing; files are written 0600 — they are
// derived artifacts in the same output tree as the conversation-bearing
// slices, and this package never loosens that.
func WriteSkeletons(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("dashboard: create output dir: %w", err)
	}
	commonJS, err := assets.ReadFile("assets/common.js")
	if err != nil {
		return fmt.Errorf("dashboard: embedded asset common.js: %w", err)
	}
	inlined := append(append([]byte("<script>\n"), commonJS...), []byte("\n</script>")...)
	for _, name := range skeletonPages {
		data, err := assets.ReadFile("assets/" + name)
		if err != nil {
			return fmt.Errorf("dashboard: embedded asset %s: %w", name, err)
		}
		if !bytes.Contains(data, []byte(commonJSTag)) {
			return fmt.Errorf("dashboard: skeleton %s does not include %q — the shared runtime would be missing", name, commonJSTag)
		}
		data = bytes.ReplaceAll(data, []byte(commonJSTag), inlined)
		dst := filepath.Join(dir, name)
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			return fmt.Errorf("dashboard: write %s: %w", dst, err)
		}
	}
	return nil
}

// AssetNames exposes the embedded skeleton page names, walked from the
// embed FS rather than the hardcoded skeletonPages list — a test asserts
// the two agree, so a page added to assets/ but missed in skeletonPages
// shows up as a test failure instead of silently not shipping.
func AssetNames() ([]string, error) {
	var out []string
	err := fs.WalkDir(assets, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".html" {
			out = append(out, filepath.Base(path))
		}
		return nil
	})
	return out, err
}
