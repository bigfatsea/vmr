// Ver 2026-07-25, by Sonnet 5

// Markdown rendering primitives for per-request detail files (detail.go):
// collapsible sections, dynamic code fences, chat-message rendering for both
// ingress protocols, and SSE stream reassembly. Everything here turns one
// piece of a recorded exchange into human-readable Markdown; the document
// skeleton and diffing live in detail.go.
package report

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"vmr/internal/audit"
	"vmr/internal/core"
)

// previewLen bounds the one-line preview shown in a <details> summary.
const previewLen = 80

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

// preview returns a single-line, length-capped excerpt of s for summaries.
func preview(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > previewLen {
		return string(r[:previewLen]) + "…"
	}
	return s
}

// escapeCell neutralizes a value for use inside a Markdown table cell.
func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// truncCell caps a table-cell value, noting the original length.
func truncCell(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return fmt.Sprintf("%s… (共 %d 字符)", string(r[:n]), len(r))
}

// fmtBytes renders a byte count human-readably — thin local alias for
// core.FmtBytes so the ~10 call sites across this package (detail.go,
// render.go) don't all need the "core." qualifier.
func fmtBytes(n int64) string {
	return core.FmtBytes(n)
}

// jsonIndent pretty-prints any decoded JSON value.
func jsonIndent(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// ---- chat message rendering ----

// chatMessage is one rendered message: role plus display-ready text in which
// images are placeholders and tool calls/results are formatted inline.
type chatMessage struct {
	Role string
	Text string
}

// renderContent flattens a message "content" value (string, or a list of
// typed parts in either protocol's shape) into display text. Base64 images
// become placeholders — never dumped.
func renderContent(v any) string {
	switch c := v.(type) {
	case nil:
		return ""
	case string:
		return c
	case []any:
		parts := make([]string, 0, len(c))
		for _, p := range c {
			m, ok := p.(map[string]any)
			if !ok {
				parts = append(parts, jsonIndent(p))
				continue
			}
			parts = append(parts, renderPart(m))
		}
		return strings.Join(parts, "\n")
	default:
		return jsonIndent(v)
	}
}

// renderPart formats one typed content part (openai or anthropic shape).
func renderPart(m map[string]any) string {
	switch m["type"] {
	case "text":
		s, _ := m["text"].(string)
		return s
	case "image_url": // openai
		u, _ := nested(m, "image_url", "url").(string)
		return imagePlaceholder(u)
	case "image": // anthropic
		mt, _ := nested(m, "source", "media_type").(string)
		data, _ := nested(m, "source", "data").(string)
		return fmt.Sprintf("🖼 [image %s ~%s]", mt, fmtBytes(int64(base64.StdEncoding.DecodedLen(len(data)))))
	case "thinking": // anthropic
		s, _ := m["thinking"].(string)
		return "🤔 [thinking]\n" + s
	case "tool_use": // anthropic
		name, _ := m["name"].(string)
		return fmt.Sprintf("🔧 tool_use %s(%s)", name, jsonIndent(m["input"]))
	case "tool_result": // anthropic
		id, _ := m["tool_use_id"].(string)
		status := ""
		if isErr, _ := m["is_error"].(bool); isErr {
			status = " ❌ is_error"
		}
		return fmt.Sprintf("↩️ tool_result (id=%s)%s\n%s", id, status, renderContent(m["content"]))
	default:
		return jsonIndent(m)
	}
}

// imagePlaceholder summarizes an image URL: data URLs by media type and
// decoded size, remote URLs verbatim.
func imagePlaceholder(u string) string {
	if rest, ok := strings.CutPrefix(u, "data:"); ok {
		mt, b64, _ := strings.Cut(rest, ",")
		mt = strings.TrimSuffix(mt, ";base64")
		return fmt.Sprintf("🖼 [image %s ~%s]", mt, fmtBytes(int64(base64.StdEncoding.DecodedLen(len(b64)))))
	}
	return "🖼 [image url: " + u + "]"
}

// chatMessages extracts the conversation from a request body: anthropic keeps
// system as a top-level field (rendered as message #0), openai carries it in
// the messages list. Non-map bodies yield nil.
func chatMessages(body any) []chatMessage {
	obj, ok := body.(map[string]any)
	if !ok {
		return nil
	}
	var out []chatMessage
	if sys, ok := obj["system"]; ok { // anthropic top-level system prompt
		out = append(out, chatMessage{Role: "system", Text: renderContent(sys)})
	}
	msgs, _ := obj["messages"].([]any)
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			out = append(out, chatMessage{Role: "?", Text: jsonIndent(raw)})
			continue
		}
		role, _ := m["role"].(string)
		text := renderContent(m["content"])
		if rc, _ := m["reasoning_content"].(string); rc != "" {
			text = "🤔 [reasoning_content]\n" + rc + "\n" + text
		}
		if id, _ := m["tool_call_id"].(string); id != "" { // openai tool result
			text = fmt.Sprintf("↩️ tool_call_id=%s\n%s", id, text)
		}
		for _, tc := range toolCallList(m["tool_calls"]) {
			text += fmt.Sprintf("\n🔧 tool_call %s [id=%s]\n%s", tc.Name, tc.ID, tc.Args)
		}
		out = append(out, chatMessage{Role: role, Text: strings.TrimSpace(text)})
	}
	return out
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
		add("system", renderContent(sys))
	}
	msgs, _ := obj["messages"].([]any)
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		switch c := m["content"].(type) {
		case []any:
			for _, p := range c {
				pm, isMap := p.(map[string]any)
				if isMap && pm["type"] == "tool_result" {
					add("tool", renderPart(pm))
				} else if isMap {
					add(role, renderPart(pm))
				} else {
					add(role, jsonIndent(p))
				}
			}
		default:
			add(role, renderContent(c))
		}
		if rc, _ := m["reasoning_content"].(string); rc != "" {
			add(role, rc)
		}
		for _, tc := range toolCallList(m["tool_calls"]) {
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

type toolCall struct {
	ID, Name, Args string
}

// toolCallList decodes an openai assistant-message tool_calls array.
func toolCallList(v any) []toolCall {
	arr, _ := v.([]any)
	out := make([]toolCall, 0, len(arr))
	for _, raw := range arr {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		tc := toolCall{}
		tc.ID, _ = m["id"].(string)
		tc.Name, _ = nested(m, "function", "name").(string)
		tc.Args, _ = nested(m, "function", "arguments").(string)
		out = append(out, tc)
	}
	return out
}

// renderMessageSection renders one message. All messages are folded by
// default (no length-based inline branch) so the document has uniform
// rhythm — a reader can scan a 300-message conversation without parsing
// "is this expanded or folded?". prefix is prepended to the summary line
// (🆕 for messages added by this turn vs the parent) and is "" for
// historical context.
func renderMessageSection(idx int, m chatMessage, prefix string) string {
	head := fmt.Sprintf("#%d %s", idx, m.Role)
	if m.Text == "" {
		return fmt.Sprintf("%s**%s** · (空)\n", prefix, head)
	}
	chars := len([]rune(m.Text))
	summary := fmt.Sprintf("<b>%s%s</b> · %s 字符 · %s",
		prefix, head, fmtCount(chars), escapeHTML(preview(m.Text)))
	return details(summary, codeFence(m.Text))
}

// fmtCount renders a character count compactly (12.3k style above 10k).
func fmtCount(n int) string {
	if n >= 10_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// toolNames lists the tool names declared in a request body (both shapes:
// openai {"function":{"name":…}}, anthropic {"name":…}).
func toolNames(body any) []string {
	obj, ok := body.(map[string]any)
	if !ok {
		return nil
	}
	arr, _ := obj["tools"].([]any)
	names := make([]string, 0, len(arr))
	for _, raw := range arr {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := nested(m, "function", "name").(string); n != "" {
			names = append(names, n)
		} else if n, _ := m["name"].(string); n != "" {
			names = append(names, n)
		} else {
			names = append(names, "?")
		}
	}
	return names
}

// ---- SSE stream reassembly ----

// streamSummary is a full response reassembled from SSE events: what the
// model actually said, extracted from the delta soup.
type streamSummary struct {
	Events    int
	Reasoning string
	Content   string
	ToolCalls []toolCall
	Finish    string // finish_reason / stop_reason
	Model     string // model名 as seen in events (post-rewrite on client side)
}

// reassembleSSE rebuilds the assistant message from a raw SSE body. Handles
// both protocols (chunk shapes are self-describing enough that protocol is
// only a hint). Returns nil if no data line parses.
func reassembleSSE(raw string) *streamSummary {
	s := &streamSummary{}
	var reasoning, content strings.Builder
	tools := map[int]*toolCall{}
	parsed := 0
	for _, line := range strings.Split(raw, "\n") {
		data, found := strings.CutPrefix(strings.TrimSpace(line), "data:")
		if !found {
			continue
		}
		s.Events++
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			continue
		}
		var obj map[string]any
		if json.Unmarshal([]byte(data), &obj) != nil {
			continue
		}
		parsed++
		if m, _ := obj["model"].(string); m != "" {
			s.Model = m
		}
		// openai chunk: choices[0].delta / finish_reason
		if choices, _ := obj["choices"].([]any); len(choices) > 0 {
			ch, _ := choices[0].(map[string]any)
			if fr, _ := ch["finish_reason"].(string); fr != "" {
				s.Finish = fr
			}
			delta, _ := ch["delta"].(map[string]any)
			if t, _ := delta["content"].(string); t != "" {
				content.WriteString(t)
			}
			if t, _ := delta["reasoning_content"].(string); t != "" {
				reasoning.WriteString(t)
			}
			for _, raw := range toolCallDeltas(delta["tool_calls"]) {
				tc := tools[raw.idx]
				if tc == nil {
					tc = &toolCall{}
					tools[raw.idx] = tc
				}
				if raw.id != "" {
					tc.ID = raw.id
				}
				if raw.name != "" {
					tc.Name = raw.name
				}
				tc.Args += raw.args
			}
			continue
		}
		// anthropic events, keyed by "type"
		switch obj["type"] {
		case "message_start":
			if m, _ := nested(obj, "message", "model").(string); m != "" {
				s.Model = m
			}
		case "content_block_start":
			idx := int(num(obj["index"]))
			if cb, _ := obj["content_block"].(map[string]any); cb != nil && cb["type"] == "tool_use" {
				name, _ := cb["name"].(string)
				id, _ := cb["id"].(string)
				tools[idx] = &toolCall{ID: id, Name: name}
			}
		case "content_block_delta":
			idx := int(num(obj["index"]))
			switch d, _ := obj["delta"].(map[string]any); d["type"] {
			case "text_delta":
				t, _ := d["text"].(string)
				content.WriteString(t)
			case "thinking_delta":
				t, _ := d["thinking"].(string)
				reasoning.WriteString(t)
			case "input_json_delta":
				t, _ := d["partial_json"].(string)
				if tc := tools[idx]; tc != nil {
					tc.Args += t
				}
			}
		case "message_delta":
			if sr, _ := nested(obj, "delta", "stop_reason").(string); sr != "" {
				s.Finish = sr
			}
		}
	}
	if parsed == 0 {
		return nil
	}
	s.Reasoning, s.Content = reasoning.String(), content.String()
	idxs := make([]int, 0, len(tools))
	for i := range tools {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	for _, i := range idxs {
		s.ToolCalls = append(s.ToolCalls, *tools[i])
	}
	return s
}

type tcDelta struct {
	idx      int
	id, name string
	args     string
}

// toolCallDeltas decodes one openai delta.tool_calls array.
func toolCallDeltas(v any) []tcDelta {
	arr, _ := v.([]any)
	out := make([]tcDelta, 0, len(arr))
	for _, raw := range arr {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		d := tcDelta{idx: int(num(m["index"]))}
		d.id, _ = m["id"].(string)
		d.name, _ = nested(m, "function", "name").(string)
		d.args, _ = nested(m, "function", "arguments").(string)
		out = append(out, d)
	}
	return out
}

// finalMessage extracts the assistant output from a non-streaming JSON
// response body (either protocol). ok=false when the shape isn't recognized.
func finalMessage(body any) (*streamSummary, bool) {
	obj, ok := body.(map[string]any)
	if !ok {
		return nil, false
	}
	s := &streamSummary{}
	s.Model, _ = obj["model"].(string)
	// openai: choices[0].message
	if choices, _ := obj["choices"].([]any); len(choices) > 0 {
		ch, _ := choices[0].(map[string]any)
		s.Finish, _ = ch["finish_reason"].(string)
		msg, _ := ch["message"].(map[string]any)
		s.Content = renderContent(msg["content"])
		s.Reasoning, _ = msg["reasoning_content"].(string)
		s.ToolCalls = toolCallList(msg["tool_calls"])
		return s, true
	}
	// anthropic: top-level content blocks
	if blocks, _ := obj["content"].([]any); blocks != nil {
		s.Finish, _ = obj["stop_reason"].(string)
		var content strings.Builder
		for _, raw := range blocks {
			m, _ := raw.(map[string]any)
			if m == nil {
				continue
			}
			switch m["type"] {
			case "text":
				t, _ := m["text"].(string)
				content.WriteString(t)
			case "thinking":
				t, _ := m["thinking"].(string)
				s.Reasoning += t
			case "tool_use":
				name, _ := m["name"].(string)
				id, _ := m["id"].(string)
				s.ToolCalls = append(s.ToolCalls, toolCall{ID: id, Name: name, Args: jsonIndent(m["input"])})
			}
		}
		s.Content = content.String()
		return s, true
	}
	return nil, false
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
	switch b := body.(type) {
	case nil:
		return 0
	case string:
		return int64(len(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return 0
		}
		return int64(len(raw))
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

// ms renders a millisecond duration as fixed-decimal seconds above 1000ms,
// or plain milliseconds below.
func ms(v int64) string {
	if v > 1000 {
		return fmt.Sprintf("%.1fs", float64(v)/1000)
	}
	return fmt.Sprintf("%dms", v)
}
