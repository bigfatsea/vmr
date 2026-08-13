// Ver 2026-07-28 19:20, by Opus 5

// Cell and chart formatting: pure functions from numbers to Markdown
// fragments, shared by every section. Nothing here knows what a Report2 is
// — that separation is what keeps the section files about *what* to show
// rather than how to spell it.
package report

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

func cut(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// shortDate renders a Row.Date ("2026-07-14") as "MM-dd" ("07-14") for the
// §5 按日期活跃度 x-axis, where the year is implied by the report's own date
// range and would otherwise crowd 11+ daily labels.
func shortDate(s string) string {
	if len(s) == 10 {
		return s[5:]
	}
	return s
}

func fmtDurMS(v int64) string {
	if v <= 0 {
		return "-"
	}
	if v < 1000 {
		return strconv.FormatInt(v, 10) + "ms"
	}
	return strconv.FormatFloat(float64(v)/1000, 'f', 1, 64) + "s"
}

// numStr formats a float without a trailing ".0" for the common whole-number
// case (an unweighted requests count, or a token_weights sum that happens to
// land on an integer) but keeps two decimals for a genuinely fractional
// value (a weighted token sum, or a $ cost amount) — §2.5's quota-vs-
// consumption sub-table's WindowConsumed/Live.Used cells.
func numStr(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func pctStr2(num, den int) string {
	if den <= 0 {
		return "-"
	}
	return strconv.FormatFloat(float64(num)/float64(den)*100, 'f', 1, 64) + "%"
}

// pctStr64 is pctStr2's int64 version — must keep the same den<=0 guard,
// otherwise a zero denominator renders "NaN%" instead of "-". Added instead
// of narrowing int64 to int at call sites (section_client_endpoint.go's
// TokensIn/clientTotal are int64, and truncating to int before formatting a
// percentage is loss-of-precision for no reason on any of the four release
// targets, even though none is anywhere near overflowing int on any of
// them) — an external review flagged this as a "systemic risk"; it is only
// the cosmetic half of that, which is why the guard above still matters more
// than the widening.
func pctStr64(num, den int64) string {
	if den <= 0 {
		return "-"
	}
	return strconv.FormatFloat(float64(num)/float64(den)*100, 'f', 1, 64) + "%"
}

// cacheEffCell renders a cache_efficiency value with a low-confidence footnote
// marker when its basis (TokensKnown) is < 90% of total requests.
func cacheEffCell(eff float64, basis, total int) string {
	s := pctStr(eff)
	if total > 0 && basis > 0 && float64(basis)/float64(total) < 0.90 {
		s += "¹"
	}
	return s
}

// ppCell renders "p50 / p95" or "p50 / p95 / max" with (n=…) and ⚠️low-n.
func ppCell(p50, p95, max int64, n int) string {
	if n == 0 {
		return "- (n=0)"
	}
	flag := ""
	if n < 20 {
		flag = " ⚠️low-n"
	}
	if max > 0 {
		return fmt.Sprintf("%s / %s / %s (n=%d%s)", fmtDurMS(p50), fmtDurMS(p95), fmtDurMS(max), n, flag)
	}
	return fmt.Sprintf("%s / %s (n=%d%s)", fmtDurMS(p50), fmtDurMS(p95), n, flag)
}

func durCell(p95 int64, n int) string {
	if n == 0 {
		return "-"
	}
	flag := ""
	if n < 20 {
		flag = " ⚠️low-n"
	}
	return fmt.Sprintf("%s (n=%d%s)", fmtDurMS(p95), n, flag)
}

func tokPerSec(f float64) string {
	if f <= 0 {
		return "-"
	}
	return strconv.FormatFloat(float64(f), 'f', 1, 64)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func orDash2(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func sumRoleChars(m map[string]int64) int64 {
	var t int64
	for _, v := range m {
		t += v
	}
	return t
}

func sortedRoles(m map[string]int64) []string {
	// order: tool, assistant, developer, system, user, then others
	order := []string{"tool", "assistant", "developer", "system", "user"}
	out := []string{}
	seen := map[string]bool{}
	for _, r := range order {
		if _, ok := m[r]; ok {
			out = append(out, r)
			seen[r] = true
		}
	}
	for r := range m {
		if !seen[r] {
			out = append(out, r)
		}
	}
	return out
}

// pctHundred formats a value already on a 0-100 percentage scale (e.g.
// EndpointRow.ErrorRate) as "x.x%" — unlike pctStr, which expects a 0-1
// fraction and would multiply by 100 again.
func pctHundred(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64) + "%"
}

// p5095Cell renders a "p50/p95" duration pair for the §5 workload tables.
func p5095Cell(p50, p95 int64) string {
	return fmtDurMS(p50) + "/" + fmtDurMS(p95)
}

// tokP5095Cell renders a "p50/p95" token-count pair (§5 按客户端 In/Out
// columns) - same shape as p5095Cell but token-scaled, not duration-scaled.
func tokP5095Cell(p50, p95 int64) string {
	return fmtTokens(p50) + "/" + fmtTokens(p95)
}

func sortedKeysInt(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---- mermaid charts ----

// hourLabels returns the fixed 24-hour x-axis category list ("00".."23"),
// shared by every hourly chart.
func hourLabels() []string {
	labels := make([]string, 24)
	for i := 0; i < 24; i++ {
		labels[i] = fmt.Sprintf("%02d", i)
	}
	return labels
}

// mermaidHourBar renders 24 hourly integer buckets (requests, error counts)
// as a mermaid xychart-beta bar chart against the fixed 24-hour axis.
func mermaidHourBar(title, yLabel string, vals []int64) string {
	return mermaidBarLabeled(title, yLabel, hourLabels(), vals)
}

// mermaidBarLabeled renders an arbitrary-length integer series (hourly,
// daily, …) as a mermaid xychart-beta bar chart with the given x-axis
// category labels.
func mermaidBarLabeled(title, yLabel string, labels []string, vals []int64) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = strconv.FormatInt(v, 10)
	}
	return mermaidChart(title, yLabel, labels, parts)
}

// mermaidTokenHourBar is mermaidHourBar's token-count counterpart: raw
// token counts run into the tens/hundreds of millions, unreadable as bare
// integers against a chart axis, so values are scaled to millions (2
// decimals) and the axis is labeled accordingly.
func mermaidTokenHourBar(title string, vals []int64) string {
	return mermaidTokenBarLabeled(title, hourLabels(), vals)
}

// mermaidTokenBarLabeled is mermaidBarLabeled's token-count counterpart —
// see mermaidTokenHourBar.
func mermaidTokenBarLabeled(title string, labels []string, vals []int64) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = strconv.FormatFloat(float64(v)/1e6, 'f', 2, 64)
	}
	return mermaidChart(title, "Token (M)", labels, parts)
}

// mermaidChart renders the shared xychart-beta scaffold; parts are already-
// formatted y-values (plain integers for counts, "M"-scaled decimals for
// token charts) — mermaid's bar/line data is a bare numeric list, so no
// thousands-separator can go in here without breaking the chart's own
// comma-delimited syntax.
func mermaidChart(title, yLabel string, labels []string, parts []string) string {
	qlabels := make([]string, len(labels))
	for i, l := range labels {
		qlabels[i] = fmt.Sprintf("%q", l)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "```mermaid\nxychart-beta\n    title %q\n    x-axis [%s]\n    y-axis %q\n    bar [%s]\n```\n\n",
		title, strings.Join(qlabels, ", "), yLabel, strings.Join(parts, ", "))
	return b.String()
}
