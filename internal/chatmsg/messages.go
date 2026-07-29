// Ver 2026-07-28 22:10, by Sonnet 5

// Package chatmsg parses OpenAI/Anthropic chat request and response bodies
// into display-ready and content-addressable shapes. It is the shared,
// stateless leaf underneath internal/report (rendering) and internal/ctxgraph
// (content-addressed manifests) — pure functions over `any`-typed decoded
// JSON, no dependency on audit/router/config, so both consumers can depend
// on it without a layering conflict.
//
// Relocated from internal/report/render.go + usage.go, where this logic
// used to live as unexported functions only report's own renderer could
// reach. internal/report keeps thin delegating wrappers (chatmsg_compat.go)
// so its own call sites and tests are untouched byte-for-byte.
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

// RenderPart formats one typed content part (openai or anthropic shape).
func RenderPart(m map[string]any) string {
	switch m["type"] {
	case "text":
		s, _ := m["text"].(string)
		return s
	case "image_url": // openai
		u, _ := Nested(m, "image_url", "url").(string)
		return ImagePlaceholder(u)
	case "image": // anthropic
		mt, _ := Nested(m, "source", "media_type").(string)
		data, _ := Nested(m, "source", "data").(string)
		return fmt.Sprintf("🖼 [image %s ~%s]", mt, fmtutil.FmtBytes(int64(base64.StdEncoding.DecodedLen(len(data)))))
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

// MsgOffset is the index shift between Messages' output and the raw
// messages array: anthropic's top-level system is prepended as message #0.
func MsgOffset(body map[string]any) int {
	if _, ok := body["system"]; ok {
		return 1
	}
	return 0
}

// Messages extracts the conversation from a request body: anthropic keeps
// system as a top-level field (rendered as message #0), openai carries it in
// the messages list. Non-map bodies yield nil.
func Messages(body any) []Message {
	obj, ok := body.(map[string]any)
	if !ok {
		return nil
	}
	var out []Message
	if sys, ok := obj["system"]; ok { // anthropic top-level system prompt
		out = append(out, Message{Role: "system", Text: RenderContent(sys)})
	}
	msgs, _ := obj["messages"].([]any)
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			out = append(out, Message{Role: "?", Text: jsonIndent(raw)})
			continue
		}
		role, _ := m["role"].(string)
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
// internal/report/usage.go used to keep a third one, deleted as tech-debt
// cleanup during the Step 3 migration onto ctxgraph (see the design doc's
// Appendix F.5); at that point two independent copies for a 12-line helper
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
