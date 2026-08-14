// Ver 2026-07-26, by Sonnet 5

// Package fmtutil holds small, dependency-free display-formatting functions
// shared between vmr's live router log (internal/router) and its offline
// `vmr report`/`vmr diagnose` output. Split out of internal/core: these are
// display formatting, not routing-domain types, and keeping them separate
// from core means the analysis layer (internal/report) doesn't have to pull
// in core.Endpoint/core.CanonicalRequest and friends just to render a number.
package fmtutil

import (
	"fmt"
	"time"
	"unicode/utf8"
)

// FmtBytes renders a byte count human-readably (B/KB/MB) — request/response
// bodies range from a few hundred bytes to several MB (inline images), so a
// fixed unit would be either unreadable or falsely precise at one end.
// Shared by every place that prints a body size (`vmr report` rendering,
// chatmsg's inline-attachment placeholder text) so they don't each carry
// their own copy of this threshold logic.
func FmtBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// FmtSeconds renders d as fixed-decimal seconds ("6.32s") instead of
// Duration.String()'s mixed units (ms/s/m) — a column where some rows read
// "141ms" and others "1m4s" doesn't scan as a column; one unit throughout
// does. decimals lets callers trade precision for width (2 for the live
// router log, 3 for `vmr diagnose`'s sub-10ms-sensitive latency columns).
func FmtSeconds(d time.Duration, decimals int) string {
	return fmt.Sprintf("%.*fs", decimals, d.Seconds())
}

// FmtPercent renders a 0..1 fraction as a percentage string ("42.3%").
// decimals follows FmtSeconds' convention (trade precision for width): 1
// for `vmr report`'s dense per-cell metrics tables, 0 for `vmr story`'s
// narrative text. Before this, internal/report and internal/story each
// carried their own independently-written pctStr with this same
// multiply-and-format line — one at 1 decimal, one at 0 — and a comment in
// story claiming the two "matched" report's, which had already gone stale.
// Both packages' pctStr are now thin aliases over this, the same pattern
// internal/report/render.go's fmtBytes already uses for FmtBytes.
func FmtPercent(f float64, decimals int) string {
	return fmt.Sprintf("%.*f%%", decimals, f*100)
}

// CapStr caps s at n BYTES without cutting a UTF-8 sequence in half — the
// cut point backs up to the nearest rune boundary, so Chinese/emoji content
// near the cap can't produce invalid UTF-8. Used both for display (TraceID
// truncation, response-text previews) and for non-display truncation
// (compaction needle matching, a scaffolding-prefix check in
// internal/taskseg's dialect detection) — none of those uses carry any
// domain knowledge, which is why this lives here rather than in whichever
// package happened to need it first.
func CapStr(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
