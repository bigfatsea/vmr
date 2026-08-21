// Ver 2026-08-20 00:00, by Sonnet 5

package reqdetail

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"vmr/internal/chatmsg"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// codeFence wraps s in a fenced code block whose fence is longer than any
// backtick run inside s, so message content can never break out of its block.
func codeFence(s string) string {
	n := 3
	run := 0
	for _, r := range s {
		if r == '`' {
			run++
			if run >= n {
				n = run + 1
			}
		} else {
			run = 0
		}
	}
	f := strings.Repeat("`", n)
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return f + "\n" + s + f + "\n"
}

// EscapeHTML neutralizes user/model-derived text before it enters raw
// Markdown/HTML output (a <summary> line, a bare paragraph) — unescaped
// < > & get parsed as HTML, up to and including an unescaped <!-- that
// silently swallows everything up to the next --> a renderer finds,
// which real corpus content has actually triggered (see KNOWN_ISSUES
// §1.37). Exported so internal/story can call this exact implementation
// instead of maintaining its own copy — see internal/story/render_md.go's
// escapeHTML wrapper for why a second copy is the failure mode this
// avoids. Content already inside a codeFence block never needs this:
// CommonMark doesn't parse HTML inside a fenced code block, which is the
// one rule that decides whether a given piece of text needs escaping.
func EscapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// renderTemplateVersion bumps whenever a change to Render's OUTPUT SHAPE
// should invalidate every previously-written detail page, even though the
// record and its filename (FileName — a pure function of identity, not of
// render logic) are unchanged. Bumping this is the mechanism a future
// change (e.g. a content-volume reduction) uses to force EnsureRendered to
// rewrite every existing page instead of introducing a fourth axis
// alongside lang/linkEvidence — see renderFingerprint.
const renderTemplateVersion = 1

// renderFingerprint is a one-line, machine-checkable summary of every input
// to Render's output shape that FileName does NOT capture: language,
// evidence-link mode, and the template version itself. Written as the
// page's first line (an HTML comment, invisible when rendered) so
// EnsureRendered's existence check can read just this one line — via
// readRenderFingerprint (ensure.go) — instead of re-rendering or reading
// the whole page to decide whether a pre-existing file is still current.
// This is what replaces the "same filename implies same content" assumption
// KNOWN_ISSUES §1.41 documents as false (proven false on two real axes:
// -lang and evidence linking) with an actual check.
func renderFingerprint(lang i18n.Lang, linkEvidence bool) string {
	return fmt.Sprintf("<!-- reqdetail:v%d lang=%s evidence=%t -->\n", renderTemplateVersion, lang, linkEvidence)
}

// EscapeCell neutralizes a value for use inside a Markdown table cell: an
// unescaped "|" splits into extra columns (corrupting that row and every
// later column in it, not just eating content the way an unescaped "<!--"
// does), and a literal newline breaks the one-line-per-row structure
// GFM tables depend on. Exported for the same reason as EscapeHTML — see
// its doc comment — internal/story's index table (storyindex.go) writes
// user-derived titles into table cells too.
func EscapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// truncCell caps a table-cell value, noting the original length.
func truncCell(s string, n int, t i18n.DetailText) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + t.TruncSuffix(len(r))
}

// jsonIndent pretty-prints any decoded JSON value.
func jsonIndent(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// roleOrder fixes the display order of the well-known roles; anything else
// (rare) is appended alphabetically.
var roleOrder = []string{"system", "user", "assistant", "tool"}

// roleStatLine renders per-role character stats as one line, e.g.
// "system 62.3k (18.2%) · user 45.1k (13.2%)"; withChars=false drops the
// absolute counts and keeps only the shares.
func roleStatLine(chars map[string]int64, withChars, bold bool) string {
	var total int64
	for _, c := range chars {
		total += c
	}
	if total == 0 {
		return ""
	}
	known := map[string]bool{}
	order := make([]string, 0, len(chars))
	for _, r := range roleOrder {
		known[r] = true
		if chars[r] > 0 {
			order = append(order, r)
		}
	}
	rest := make([]string, 0)
	for r := range chars {
		if !known[r] {
			rest = append(rest, r)
		}
	}
	sort.Strings(rest)
	order = append(order, rest...)

	parts := make([]string, 0, len(order))
	for _, r := range order {
		share := fmt.Sprintf("%.1f%%", float64(chars[r])/float64(total)*100)
		val := share
		if withChars {
			val = fmt.Sprintf("%s (%s)", fmtCount(int(chars[r])), share)
		}
		if bold {
			val = "<strong>" + val + "</strong>"
		}
		parts = append(parts, fmt.Sprintf("%s %s", r, val))
	}
	return strings.Join(parts, " · ")
}

// renderMessageSection renders one message. All messages are folded by
// default (no length-based inline branch) so the document has uniform
// rhythm — a reader can scan a 300-message conversation without parsing
// "is this expanded or folded?". prefix is prepended to the summary line
// (🆕 for messages added by this turn vs the parent) and is "" for
// historical context.
func renderMessageSection(idx int, m chatmsg.Message, prefix string, t i18n.DetailText) string {
	head := fmt.Sprintf("#%d %s", idx, m.Role)
	if m.Text == "" {
		return t.EmptyMessage(prefix, head)
	}
	chars := len([]rune(m.Text))
	summary := t.MessageSummary(prefix, head, fmtCount(chars), EscapeHTML(taskseg.Preview(m.Text)))
	return Details(summary, codeFence(m.Text))
}

// fmtCount renders a character count compactly (12.3k style above 10k).
func fmtCount(n int) string {
	if n >= 10_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// pct renders n/total as a percentage string ("-" when total is 0).
func pct(n, total int) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", float64(n)/float64(total)*100)
}

// fmtN renders a count compactly (K/M-scaled above 10k/1M).
func fmtN(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// tokensTriple renders the 3-tuple (In / CacheHit(share%) / Out).
func tokensTriple(in, hit, out int64) string {
	if in == 0 && out == 0 {
		return "-"
	}
	return fmt.Sprintf("%s / %s(%s) / %s",
		fmtN(in), fmtN(hit), pct(int(hit), int(in)), fmtN(out))
}

// ms renders a millisecond duration as fixed-decimal seconds at 1000ms and
// above, or plain milliseconds below.
func ms(v int64) string {
	if v >= 1000 {
		return fmt.Sprintf("%.1fs", float64(v)/1000)
	}
	return fmt.Sprintf("%dms", v)
}

// unsafeName matches filename characters we replace; keeps letters, digits,
// dot, underscore, hyphen.
var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitizeName(s string) string {
	s = unsafeName.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func outcomeMark(outcome string) string {
	switch outcome {
	case "ok":
		return "✅ ok"
	case "canceled":
		return "🚫 canceled"
	default:
		return "❌ " + outcome
	}
}
