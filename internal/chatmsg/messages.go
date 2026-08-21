// Ver 2026-07-28 22:10, by Sonnet 5

// Package chatmsg parses OpenAI/Anthropic chat request and response bodies
// into display-ready and content-addressable shapes. It is the shared,
// stateless leaf underneath internal/report (rendering) and internal/ctxgraph
// (content-addressed manifests) — pure functions over `any`-typed decoded
// JSON, no dependency on audit/router/config, so both consumers can depend
// on it without a layering conflict.
//
// Relocated from what was then internal/report's own renderer (that file
// has since moved again, to internal/reqdetail/render.go), where this logic
// used to live as unexported functions only report's own renderer could
// reach. That move originally landed behind a thin delegation layer in
// report so its call sites stayed untouched; the layer is gone and every
// consumer now calls this package directly.
package chatmsg

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"vmr/internal/fmtutil"
)

// Message is one rendered message: role plus display-ready text in which
// images are placeholders and tool calls/results are formatted inline.
type Message struct {
	Role string
	Text string
}

// NewUserWindow guards task/instruction-boundary splitting: a real user
// message only counts as a NEW instruction when it sits within this many
// messages of the request's end. An in-place history edit (e.g. image
// pruning) can push the delta boundary far back and sweep an old user
// message into the "new" range; those must not open a task. Consumed by
// internal/taskseg.HasNewInstruction — the one shared implementation
// report's session.go and story's journey.go both call through (the
// converged what used to be two independent
// copies of this same boundary rule). Declared here rather than in taskseg
// so the two can't silently drift apart even before B3, and so taskseg
// itself doesn't have to own a constant that's really about message-list
// shape, not agent-dialect knowledge.
const NewUserWindow = 8

// RenderContent flattens a message "content" value (string, or a list of
// typed parts in either protocol's shape) into display text. Base64 images
// become placeholders — never dumped.
func RenderContent(v any) string {
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
			parts = append(parts, RenderPart(m))
		}
		return strings.Join(parts, "\n")
	default:
		return jsonIndent(v)
	}
}

// RenderPart formats one typed content part (openai, anthropic, or
// openai-responses shape).
func RenderPart(m map[string]any) string {
	switch m["type"] {
	case "text", "input_text", "output_text": // "text": openai chat completions/anthropic; the other two: openai-responses
		s, _ := m["text"].(string)
		return s
	case "image_url": // openai
		u, _ := Nested(m, "image_url", "url").(string)
		return ImagePlaceholder(u)
	case "image": // anthropic
		mt, _ := Nested(m, "source", "media_type").(string)
		data, _ := Nested(m, "source", "data").(string)
		return fmt.Sprintf("🖼 [image %s ~%s]", mt, fmtutil.FmtBytes(int64(base64.StdEncoding.DecodedLen(len(data)))))
	case "input_image": // openai-responses — image_url is a FLAT string field here, not nested like openai chat completions
		if u, _ := m["image_url"].(string); u != "" {
			return ImagePlaceholder(u)
		}
		if fid, _ := m["file_id"].(string); fid != "" {
			return fmt.Sprintf("🖼 [image file_id=%s]", fid)
		}
		return "🖼 [image]"
	case "input_file": // openai-responses
		name, _ := m["filename"].(string)
		if data, _ := m["file_data"].(string); data != "" {
			return fmt.Sprintf("📄 [file %s ~%s]", name, fmtutil.FmtBytes(int64(base64.StdEncoding.DecodedLen(len(data)))))
		}
		if u, _ := m["file_url"].(string); u != "" {
			return fmt.Sprintf("📄 [file %s: %s]", name, u)
		}
		if fid, _ := m["file_id"].(string); fid != "" {
			return fmt.Sprintf("📄 [file %s, file_id=%s]", name, fid)
		}
		return fmt.Sprintf("📄 [file %s]", name)
	case "refusal": // openai-responses
		s, _ := m["refusal"].(string)
		return "⚠️ refusal: " + s
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
		return fmt.Sprintf("↩️ tool_result (id=%s)%s\n%s", id, status, RenderContent(m["content"]))
	default:
		return jsonIndent(m)
	}
}

// ImagePlaceholder summarizes an image URL: data URLs by media type and
// decoded size, remote URLs verbatim.
func ImagePlaceholder(u string) string {
	if rest, ok := strings.CutPrefix(u, "data:"); ok {
		mt, b64, _ := strings.Cut(rest, ",")
		mt = strings.TrimSuffix(mt, ";base64")
		return fmt.Sprintf("🖼 [image %s ~%s]", mt, fmtutil.FmtBytes(int64(base64.StdEncoding.DecodedLen(len(b64)))))
	}
	return "🖼 [image url: " + u + "]"
}

// MsgOffset is the index shift between Messages' output and RawArray's
// element array: a leading synthetic message is prepended as message #0
// when the body carries its system-equivalent as a separate top-level
// field — anthropic's "system", or openai-responses' "instructions".
func MsgOffset(body map[string]any) int {
	if _, ok := body["system"]; ok {
		return 1
	}
	if _, ok := body["instructions"]; ok {
		return 1
	}
	return 0
}

// RawArray returns the raw top-level conversation array a request body
// carries — "messages" for Chat Completions/Anthropic Messages, "input" for
// openai-responses. nil when the body has neither, or when "input" is a
// bare string (Responses' simplest valid shape — no array to index into at
// all). Callers that need a message's EXACT raw JSON encoding (as opposed
// to Messages' rendered text — e.g. for content-addressed hashing) index
// into this with MsgOffset as the alignment shift; every other caller
// should just use Messages.
func RawArray(body map[string]any) []any {
	if arr, ok := body["messages"].([]any); ok {
		return arr
	}
	arr, _ := body["input"].([]any)
	return arr
}

// Messages extracts the conversation from a request body: anthropic keeps
// system as a top-level field (rendered as message #0), openai carries it in
// the messages list, openai-responses carries it in "instructions" (rendered
// as message #0) plus a top-level "input" array or bare string in place of
// "messages". Non-map bodies yield nil.
func Messages(body any) []Message {
	obj, ok := body.(map[string]any)
	if !ok {
		return nil
	}
	var out []Message
	if sys, ok := obj["system"]; ok { // anthropic top-level system prompt
		out = append(out, Message{Role: "system", Text: RenderContent(sys)})
	}
	if instr, ok := obj["instructions"]; ok { // openai-responses top-level system-equivalent
		out = append(out, Message{Role: "system", Text: RenderContent(instr)})
	}
	msgs := RawArray(obj)
	if msgs == nil {
		if s, ok := obj["input"].(string); ok && s != "" { // Responses' bare-string input shape
			out = append(out, Message{Role: "user", Text: s})
		}
		return out
	}
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			out = append(out, Message{Role: "?", Text: jsonIndent(raw)})
			continue
		}
		role, hasRole := m["role"].(string)
		if !hasRole { // openai-responses non-message Item: function_call/function_call_output/reasoning/...
			out = append(out, responsesItemMessage(m))
			continue
		}
		text := RenderContent(m["content"])
		if rc, _ := m["reasoning_content"].(string); rc != "" {
			text = "🤔 [reasoning_content]\n" + rc + "\n" + text
		}
		if id, _ := m["tool_call_id"].(string); id != "" { // openai tool result
			text = fmt.Sprintf("↩️ tool_call_id=%s\n%s", id, text)
		}
		for _, tc := range ToolCallList(m["tool_calls"]) {
			text += fmt.Sprintf("\n🔧 tool_call %s [id=%s]\n%s", tc.Name, tc.ID, tc.Args)
		}
		out = append(out, Message{Role: role, Text: strings.TrimSpace(text)})
	}
	return out
}

// responsesItemMessage renders one openai-responses "input" Item that has no
// "role" key — a function_call/function_call_output/reasoning Item, as
// opposed to a role-bearing message Item (handled inline by Messages).
// Exported behavior lives here, not duplicated in internal/report's own
// role-breakdown walk (roleMeasure calls this too via ResponsesItemMessage),
// so the two never disagree on how a given Item categorizes.
func responsesItemMessage(m map[string]any) Message {
	switch m["type"] {
	case "function_call": // the assistant's tool invocation — Responses' flat counterpart to Chat Completions' message.tool_calls[]
		name, _ := m["name"].(string)
		args, _ := m["arguments"].(string)
		id, _ := m["call_id"].(string)
		return Message{Role: "assistant", Text: fmt.Sprintf("🔧 tool_call %s [id=%s]\n%s", name, id, args)}
	case "function_call_output": // the tool's result being fed back — Responses' counterpart to a role:"tool" message
		id, _ := m["call_id"].(string)
		return Message{Role: "tool", Text: fmt.Sprintf("↩️ call_id=%s\n%s", id, RenderContent(m["output"]))}
	case "reasoning":
		return Message{Role: "assistant", Text: "🤔 [reasoning]\n" + reasoningSummaryText(m)}
	default:
		return Message{Role: "?", Text: jsonIndent(m)}
	}
}

// ResponsesItemMessage exports responsesItemMessage for internal/report's
// roleMeasure, which needs the same Item-type categorization Messages uses
// internally but can't just call Messages itself (it buckets Anthropic
// tool_result parts under a "tool" role distinct from their containing
// message's role — a per-part split Messages' one-Message-per-Item output
// doesn't preserve).
func ResponsesItemMessage(m map[string]any) (role, text string) {
	msg := responsesItemMessage(m)
	return msg.Role, msg.Text
}

// reasoningSummaryText renders a Responses "reasoning" Item's summary parts
// (when the provider includes one) or a placeholder — encrypted_content (if
// present instead) is opaque ciphertext vmr has no business rendering.
func reasoningSummaryText(m map[string]any) string {
	arr, _ := m["summary"].([]any)
	var parts []string
	for _, s := range arr {
		sm, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := sm["text"].(string); t != "" {
			parts = append(parts, t)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	if _, ok := m["encrypted_content"]; ok {
		return "[encrypted reasoning, no summary]"
	}
	return "[reasoning, no summary]"
}

// ToolCall is one decoded tool invocation (openai tool_calls entry or
// anthropic tool_use block), normalized to a common shape.
type ToolCall struct {
	ID, Name, Args string
}

// ToolCallList decodes an openai assistant-message tool_calls array.
func ToolCallList(v any) []ToolCall {
	arr, _ := v.([]any)
	out := make([]ToolCall, 0, len(arr))
	for _, raw := range arr {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		tc := ToolCall{}
		tc.ID, _ = m["id"].(string)
		tc.Name, _ = Nested(m, "function", "name").(string)
		tc.Args, _ = Nested(m, "function", "arguments").(string)
		out = append(out, tc)
	}
	return out
}

// ToolNames lists the tool names declared in a request body (both shapes:
// openai {"function":{"name":…}}, anthropic {"name":…}).
func ToolNames(body any) []string {
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
		if n, _ := Nested(m, "function", "name").(string); n != "" {
			names = append(names, n)
		} else if n, _ := m["name"].(string); n != "" {
			names = append(names, n)
		} else {
			names = append(names, "?")
		}
	}
	return names
}

// jsonIndent pretty-prints any decoded JSON value.
func jsonIndent(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// Nested walks a chain of map keys, returning nil the moment any link isn't
// a map[string]any. Exported so internal/ctxgraph (which already imports
// chatmsg for Messages/ExtractUsage) doesn't need its own private copy —
// report package used to keep a third copy in its own usage-extraction
// file, deleted as tech-debt cleanup during the Step 3 migration onto
// ctxgraph; at that point two independent copies for a 12-line helper
// stopped being cheaper than one shared export.
func Nested(obj map[string]any, keys ...string) any {
	var cur any = obj
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

// num coerces a decoded-JSON numeric value (float64 from encoding/json, or
// json.Number when a decoder was configured with UseNumber) to int64.
func num(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}
