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
	"strconv"
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
// FmtBytes already established for byte counts.
func FmtPercent(f float64, decimals int) string {
	return fmt.Sprintf("%.*f%%", decimals, f*100)
}

// FmtTokens renders a token count for a dense Markdown table cell
// (K/M/B suffix, no space, no unit letter below 1000) — `vmr report`'s
// per-cell metrics tables and `vmr story`'s narrative tables both want this
// same compact bare-number shape. Before this, internal/report/metrics.go
// and internal/story/render_md.go each carried their own independently
// written fmtTokens with this same threshold logic, drifted apart only by
// decimal-place count and B being report-only (report's corpus-wide totals
// can reach billions; a single story Journey never does) — accidental
// drift, not an intentional difference, so both converge here rather than
// getting two names. No "(est)" marker: callers use this for actual,
// already-billed usage counts, never an estimate — a caller that does need
// to render an estimated count (FmtTokensCompact's estTokenField, e.g.)
// carries that "(est)" label itself, at the call site, rather than baking
// it into this function.
func FmtTokens(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return strconv.FormatFloat(float64(n)/1e9, 'f', 2, 64) + "B"
	case n >= 1_000_000:
		return strconv.FormatFloat(float64(n)/1e6, 'f', 2, 64) + "M"
	case n >= 1_000:
		return strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64) + "K"
	default:
		return strconv.FormatInt(n, 10)
	}
}

// FmtTokensPlain renders a token count with a space-separated unit letter
// ("500 T", "1.2 KT", "1.5 MT") — `vmr report`'s detail.go facts line wants
// each value visually self-labeled rather than relying on a table header,
// unlike FmtTokens' bare-number table-cell shape. This is a genuinely
// different format from FmtTokens (not just a formatting accident), so it
// keeps its own name instead of collapsing into FmtTokens. No "(est)"/"EST"
// marker on the unit itself: unlike the live router log's estTokenField
// (internal/router/logfmt.go, built on FmtTokensCompact), which needs that
// marker inline since it shares a line with actual-usage numbers, the
// detail page's field is already labeled as an estimate by its surrounding
// text, so the terser unit reads better here.
func FmtTokensPlain(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1f MT", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1f KT", float64(n)/1000)
	default:
		return fmt.Sprintf("%d T", n)
	}
}

// FmtTokensCompact renders a token count for the live router log line
// (always "KT"/"MT", no bare-number tier below 1000) — the live log wants
// every token field to line up on the same unit rather than switching shape
// at the 1000 boundary the way FmtTokens/FmtTokensPlain do, since a log
// line is read scrolling past rather than scanned as an aligned column.
// Sub-1K values get 2 decimals instead of K/M's 1: at 1 decimal a value
// under 100 tokens would round to "0.0KT" and lose the number entirely.
// No "(est)"/"EST" marker of its own — every caller already spells out
// estimated-vs-actual in the surrounding text (router/logfmt.go's
// estTokenField appends "(est)" itself; usageTokenField is only reached
// with real usage), so baking the marker into the unit would be redundant.
func FmtTokensCompact(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fMT", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1fKT", float64(n)/1000)
	default:
		return fmt.Sprintf("%.2fKT", float64(n)/1000)
	}
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
