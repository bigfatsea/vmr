// Ver 2026-08-29, by Sonnet 5

// RenderToolWasteHTML: the standalone tool-schema-waste card
// ({out}/tool-waste.html). A shareable single-screen artifact built from the
// report's own rep.Tools rows — "your agent ships N tool definitions on
// every request and calls M of them." Self-contained (inline CSS, zero
// external requests), part of the same "VMR Forensics" visual system as the
// story dashboards (same design tokens, held by convention not code — the
// two live in different packages). Carries no conversation content: tool
// names, counts and byte sizes only.
package report

import (
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"

	"vmr/internal/i18n"
)

// toolWasteTopN bounds the per-shape table — the same "top offenders, full
// list is in vmr-report.json" discipline §7's own tool-waste table uses,
// one notch wider since this card has nothing else competing for the screen.
const toolWasteTopN = 8

// toolWasteBytesPerToken is the rough JSON→token divisor for the "≈ tokens
// wasted" figure. Tool-schema JSON is dense ASCII (keys, braces, quotes), so
// ~4 bytes/token holds close; the card labels the number "≈" and the
// footnote says it's an estimate. A precise count would mean threading the
// marshaled schema text through the report's aggregation just for this one
// display number — not worth it against a <10% error on dense ASCII.
const toolWasteBytesPerToken = 4

// RenderToolWasteHTML renders rep.Tools as one self-contained card in lang.
// Callers gate on len(rep.Tools) > 0.
func RenderToolWasteHTML(rep *Report2, lang i18n.Lang) string {
	t := i18n.ToolWaste(lang)
	rows := append([]ToolShapeRow(nil), rep.Tools...)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].SchemaWasteBytes != rows[j].SchemaWasteBytes {
			return rows[i].SchemaWasteBytes > rows[j].SchemaWasteBytes
		}
		return rows[i].Shape < rows[j].Shape
	})

	var shipped, waste int64
	for _, r := range rows {
		shipped += r.SchemaBytesShipped
		waste += r.SchemaWasteBytes
	}
	pct := 0.0
	if shipped > 0 {
		pct = float64(waste) / float64(shipped) * 100
	}

	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f, a...) }
	w("<!doctype html>\n<html lang=%q>\n<head>\n<meta charset=\"utf-8\">\n", twLangAttr(lang))
	w("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	w("<title>%s</title>\n<style>\n%s</style>\n</head>\n<body>\n<div class=\"wrap\">\n", twe(t.DocTitle), toolWasteStyle)

	w("<div class=\"recbar\"><span class=\"rl\">%s</span></div>\n", twe(t.RecorderBar))
	w("<div class=\"hero\">\n<div class=\"hpct\">%s</div>\n<div class=\"hcap\">%s</div>\n</div>\n",
		twe(t.HeroPct(strconv.FormatFloat(pct, 'f', 0, 64)+"%")), twe(t.HeroCaption))

	w("<div class=\"stats\">\n")
	twStat(w, t.StatShipped, fmtBytesGB(shipped))
	twStat(w, t.StatDead, fmtBytesGB(waste))
	twStat(w, t.StatTokens, twTokens(waste))
	twStat(w, t.StatShapes, strconv.Itoa(len(rows)))
	w("</div>\n")

	twTable(w, rows, t)

	w("<div class=\"foot\">%s</div>\n", twe(t.Footnote))
	w("<footer class=\"gennote\">%s</footer>\n</div>\n</body>\n</html>\n", twe(t.GeneratedNote))
	return b.String()
}

func twTable(w func(string, ...any), rows []ToolShapeRow, t i18n.ToolWasteText) {
	// A waste card shows the wasteful shapes: drop the fully-used (zero-waste)
	// rows the top-N would otherwise pad with. Keep all rows only in the
	// degenerate "nothing is wasted" case, where the card has nothing better
	// to show anyway.
	shown := make([]ToolShapeRow, 0, len(rows))
	for _, r := range rows {
		if r.SchemaWasteBytes > 0 {
			shown = append(shown, r)
		}
	}
	if len(shown) == 0 {
		shown = rows
	}
	if len(shown) > toolWasteTopN {
		shown = shown[:toolWasteTopN]
	}
	w("<table>\n<thead><tr><th>%s</th><th>%s</th><th>%s</th><th class=\"num\">%s</th></tr></thead>\n<tbody>\n",
		twe(t.ColShape), twe(t.ColRequests), twe(t.ColUsage), twe(t.ColWaste))
	for _, r := range shown {
		usedFrac := r.DeclareUtilization
		if usedFrac < 0 {
			usedFrac = 0
		} else if usedFrac > 1 {
			usedFrac = 1
		}
		w("<tr>\n<td class=\"shape\">%s<span class=\"sig\">%s</span></td>\n<td>%s</td>\n",
			twe(twNeverCalled(r.NeverCalled, t)), twe(twSig(r.Shape)), twe(strconv.Itoa(r.Requests)))
		w("<td><div class=\"bar\"><span style=\"width:%.1f%%\"></span></div><span class=\"bl\">%s</span></td>\n",
			usedFrac*100, twe(t.UsedCalled(r.DistinctCalled, len(r.Declared))))
		w("<td class=\"num\">%s<span class=\"tk\"> · ≈%s</span></td>\n</tr>\n",
			twe(fmtBytesGB(r.SchemaWasteBytes)), twe(twTokens(r.SchemaWasteBytes)))
	}
	w("</tbody>\n</table>\n")
}

func twStat(w func(string, ...any), k, v string) {
	w("<div class=\"stat\"><div class=\"k\">%s</div><div class=\"v\">%s</div></div>\n", twe(k), twe(v))
}

func twTokens(bytes int64) string {
	tok := bytes / toolWasteBytesPerToken
	switch {
	case tok >= 1_000_000:
		return strconv.FormatFloat(float64(tok)/1e6, 'f', 1, 64) + "M"
	case tok >= 1_000:
		return strconv.FormatFloat(float64(tok)/1e3, 'f', 1, 64) + "K"
	default:
		return strconv.FormatInt(tok, 10)
	}
}

// twNeverCalledShown bounds how many never-called tool names the shape cell
// spells out before collapsing the rest to a "+K more" tail.
const twNeverCalledShown = 4

// twNeverCalled names the tools this shape declares but never calls — the
// card's whole point, made concrete instead of hidden behind a hash. Empty
// NeverCalled (a fully-used shape that still reached the top-N on raw shipped
// bytes) renders the all-called note.
func twNeverCalled(names []string, t i18n.ToolWasteText) string {
	if len(names) == 0 {
		return t.AllCalled
	}
	shown := names
	if len(shown) > twNeverCalledShown {
		shown = shown[:twNeverCalledShown]
	}
	s := strings.Join(shown, ", ")
	if extra := len(names) - len(shown); extra > 0 {
		s += " " + t.NeverCalledMore(extra)
	}
	return s
}

// twSig caps the deduplicated-declaration fingerprint (reqdetail.ToolsSig,
// "tools:<N>/<hash>") shown small beneath the names — the key to this row's
// full entry in vmr-report.json's tools[].
func twSig(s string) string {
	const maxLen = 40
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen-1]) + "…"
}

// toolWasteStyle is the card's inline stylesheet — the same "VMR Forensics"
// design tokens the story dashboards use (mono-dominant, dark instrument /
// light graph paper, amber as the single alert accent), duplicated here
// rather than shared because the two renderers live in different packages
// and the token set is small.
const toolWasteStyle = `
:root {
  --bg:#eef0f2; --panel:#fff; --ink:#1a1f28; --ink-dim:#5c6675; --rule:#d4d9e0;
  --amber:#b8730a; --go:#1a7a5e; --code:#eceef1;
  --mono: ui-monospace,"SF Mono","JetBrains Mono",Menlo,Consolas,monospace;
  --sans: -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;
}
@media (prefers-color-scheme: dark) {
  :root { --bg:#0a0e14; --panel:#121821; --ink:#d8dee9; --ink-dim:#7d8899; --rule:#232c3a;
    --amber:#ffb000; --go:#35d0a5; --code:#1a2030; }
}
* { box-sizing:border-box; }
body { margin:0; background:var(--bg); color:var(--ink); font:14px/1.6 var(--mono);
  font-variant-numeric:tabular-nums; -webkit-font-smoothing:antialiased; }
.wrap { max-width:760px; margin:0 auto; padding:0 20px 50px; }
.recbar { padding:10px 0; margin-bottom:26px; border-bottom:1px solid var(--rule);
  font-size:11px; letter-spacing:.18em; text-transform:uppercase; color:var(--ink-dim); }
.hero { text-align:center; padding:18px 0 22px; }
.hpct { font-size:72px; font-weight:700; color:var(--amber); letter-spacing:-.02em; line-height:1; }
.hcap { font-family:var(--sans); font-size:15px; color:var(--ink); max-width:460px; margin:10px auto 0; }
.stats { display:grid; grid-template-columns:repeat(4,1fr); gap:9px; margin-bottom:24px; }
.stat { border:1px solid var(--rule); border-radius:5px; padding:9px 11px; background:var(--panel); }
.stat .k { font-size:10px; text-transform:uppercase; letter-spacing:.06em; color:var(--ink-dim); }
.stat .v { font-size:17px; font-weight:700; margin-top:3px; }
table { border-collapse:collapse; width:100%; font-size:12.5px; }
th,td { padding:7px 9px; border-bottom:1px solid var(--rule); text-align:left; vertical-align:middle; }
th { font-size:10px; text-transform:uppercase; letter-spacing:.06em; color:var(--ink-dim); }
td.num, th.num { text-align:right; white-space:nowrap; }
td.shape { font-size:11.5px; color:var(--ink); word-break:break-word; }
td.shape .sig { display:block; margin-top:3px; font-size:10px; color:var(--ink-dim); }
.tk { color:var(--ink-dim); }
.bar { display:inline-block; width:96px; height:9px; border-radius:5px; background:var(--amber);
  overflow:hidden; vertical-align:middle; border:1px solid var(--rule); }
.bar span { display:block; height:100%; background:var(--go); }
.bl { margin-left:8px; font-size:11px; color:var(--ink-dim); }
.foot { margin-top:20px; font-size:11px; color:var(--ink-dim); font-family:var(--sans); line-height:1.5; }
.gennote { color:var(--ink-dim); font-size:11px; padding-top:20px; }
@media (max-width:640px) { .stats { grid-template-columns:repeat(2,1fr); } .hpct { font-size:56px; } }
`

func twLangAttr(lang i18n.Lang) string {
	if lang == i18n.ZH {
		return "zh"
	}
	return "en"
}

func twe(s string) string { return html.EscapeString(s) }
