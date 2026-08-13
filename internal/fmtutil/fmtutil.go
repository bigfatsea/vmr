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
