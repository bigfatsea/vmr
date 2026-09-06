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
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed assets
var assets embed.FS

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
// dir is created (0700) if missing; files are written 0600 — they are
// derived artifacts in the same output tree as the conversation-bearing
// slices, and this package never loosens that.
func WriteSkeletons(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("dashboard: create output dir: %w", err)
	}
	for _, name := range skeletonPages {
		data, err := assets.ReadFile("assets/" + name)
		if err != nil {
			return fmt.Errorf("dashboard: embedded asset %s: %w", name, err)
		}
		dst := filepath.Join(dir, name)
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			return fmt.Errorf("dashboard: write %s: %w", dst, err)
		}
	}
	return nil
}

// AssetNames exposes the embedded skeleton page names (walked from the
// embed FS, not the hardcoded list) — Group 2B's analyze wiring uses this
// to report what it refreshed; the filesystem walk over the embed FS is the
// source of truth, so a page added to assets/ but missed in skeletonPages
// would show up here as a test failure instead of silently not shipping.
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
