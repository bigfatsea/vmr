// Ver 2026-07-28 22:15, by Sonnet 5

// Markdown rendering primitives for per-request detail files (detail.go):
// collapsible sections and dynamic code fences. Chat-message rendering and
// SSE stream reassembly live in internal/chatmsg (see that package's doc
// comment for why: internal/ctxgraph and internal/story also need the same
// parsing). Everything here turns one piece of a recorded exchange into
// human-readable Markdown; the document skeleton and diffing live in
// detail.go.
package report

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/core"
	"vmr/internal/fmtutil"
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

// details wraps body in a collapsed-by-default disclosure block. summary is
// HTML-escaped by the caller only if it embeds user content (see escapeHTML).
// The blank lines around body are required for Markdown to render inside
// <details> on GitHub and VS Code.
func details(summary, body string) string {
	return "<details><summary>" + summary + "</summary>\n\n" + body + "\n</details>\n"
}

// escapeHTML neutralizes content destined for a <summary> line, where raw
// < > & would be parsed as HTML.
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// escapeCell neutralizes a value for use inside a Markdown table cell.
func escapeCell(s string) string {
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

// fmtBytes renders a byte count human-readably — thin local alias for
// fmtutil.FmtBytes so the ~10 call sites across this package (detail.go,
// render.go) don't all need the "fmtutil." qualifier.
func fmtBytes(n int64) string {
	return fmtutil.FmtBytes(n)
}

// jsonIndent pretty-prints any decoded JSON value.
func jsonIndent(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// roleChars sums displayed characters (runes) per role across a request
// body's conversation. Anthropic tool_result parts are counted as "tool"
// regardless of the containing message's role, mirroring openai's dedicated
// "tool" role so both protocols yield comparable shares. Returns nil when
// the body isn't a chat object.
func roleChars(body any) map[string]int64 {
	return roleMeasure(body, func(s string) int64 { return int64(len([]rune(s))) })
}

// roleTokens is roleChars' token-estimate sibling: same per-role traversal,
// but each text fragment is sized with core.EstimateTextTokens (the same
// formula behind RequestFacts.EstimatedTokens) instead of a raw rune count —
// a token share is a much closer proxy for "what's actually costing money in
// this conversation" than a character share.
func roleTokens(body any) map[string]int64 {
	return roleMeasure(body, func(s string) int64 { return core.EstimateTextTokens([]byte(s)) })
}

// roleMeasure walks a request body's conversation once, calling measure on
// every role's displayed text and summing the result per role. Shared by
// roleChars (rune count) and roleTokens (estimated token count) so the two
// only differ in how a text fragment is sized, not how the tree is walked.
func roleMeasure(body any, measure func(string) int64) map[string]int64 {
	obj, ok := body.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]int64{}
	add := func(role, text string) {
		if text != "" {
			out[role] += measure(text)
		}
	}
	if sys, ok := obj["system"]; ok {
		add("system", chatmsg.RenderContent(sys))
	}
	if instr, ok := obj["instructions"]; ok { // openai-responses
		add("system", chatmsg.RenderContent(instr))
	}
	for _, raw := range chatmsg.RawArray(obj) {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, hasRole := m["role"].(string)
		if !hasRole { // openai-responses non-message Item: function_call/function_call_output/reasoning/...
			itemRole, text := chatmsg.ResponsesItemMessage(m)
			add(itemRole, text)
			continue
		}
		switch c := m["content"].(type) {
		case []any:
			for _, p := range c {
				pm, isMap := p.(map[string]any)
				if isMap && pm["type"] == "tool_result" {
					add("tool", chatmsg.RenderPart(pm))
				} else if isMap {
					add(role, chatmsg.RenderPart(pm))
				} else {
					add(role, jsonIndent(p))
				}
			}
		default:
			add(role, chatmsg.RenderContent(c))
		}
		if rc, _ := m["reasoning_content"].(string); rc != "" {
			add(role, rc)
		}
		for _, tc := range chatmsg.ToolCallList(m["tool_calls"]) {
			add(role, tc.Name+tc.Args)
		}
	}
	return out
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
	summary := t.MessageSummary(prefix, head, fmtCount(chars), escapeHTML(taskseg.Preview(m.Text)))
	return details(summary, codeFence(m.Text))
}

// fmtCount renders a character count compactly (12.3k style above 10k).
func fmtCount(n int) string {
	if n >= 10_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// ---- small formatting/extraction helpers shared with session.go and
// detail.go's per-request rendering (relocated here from the old report.go/
// markdown.go aggregate-report code when that code was removed) ----

// attemptErrorClass returns the attempt's structured error class, falling
// back to parsing the free-text Error field for logs written before
// ErrorClass existed: HTTP-classified failures stored the bare class name
// (no colon) directly in Error, and the four non-HTTP failure paths
// (build/network/canceled/truncated) used a "class: detail" prefix — both
// forms are still exactly recoverable from Error alone. New logs always
// carry ErrorClass and never touch this fallback.
func attemptErrorClass(a audit.Attempt) string {
	if a.ErrorClass != "" {
		return a.ErrorClass
	}
	if a.Error == "" {
		return ""
	}
	if i := strings.IndexByte(a.Error, ':'); i > 0 {
		return a.Error[:i]
	}
	return a.Error
}

// countImages tallies a record's inline request images and the subset that
// triggered downscaling.
func countImages(images []audit.ImageInfo) (total, compressed int) {
	total = len(images)
	for _, img := range images {
		if img.Downscaled {
			compressed++
		}
	}
	return total, compressed
}

// bodyBytes sizes a recorded body: JSON bodies by re-serialization, string
// bodies (SSE etc.) by length. Truncated bodies undercount; that matches
// what was recorded.
func bodyBytes(body any) int64 {
	return int64(len(bodyRaw(body)))
}

// bodyRaw is bodyBytes' byte-returning counterpart: the recorded body as the
// bytes it occupied on the wire. audit.EncodeBody stores a JSON body as
// json.RawMessage and anything else (SSE text) as a string, so the two cases
// are unwrapped differently — but both must yield the same byte sequence a
// re-marshal would produce, since the only consumer needing the bytes rather
// than their count (estimateDegradedTokens) has to reproduce a byte-count
// formula the routing half already applied to this same body.
func bodyRaw(body any) []byte {
	switch b := body.(type) {
	case nil:
		return nil
	case string:
		return []byte(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return nil
		}
		return raw
	}
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
