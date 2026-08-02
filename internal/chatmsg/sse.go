// Ver 2026-08-02 16:00, by Sonnet 5

package chatmsg

import (
	"encoding/json"
	"sort"
	"strings"
)

// StreamSummary is a full response reassembled from SSE events (or a
// non-streaming JSON body): what the model actually said, extracted from
// the delta soup.
type StreamSummary struct {
	Events    int
	Reasoning string
	Content   string
	ToolCalls []ToolCall
	Finish    string // finish_reason / stop_reason
	Model     string // model名 as seen in events (post-rewrite on client side)
}

// ReassembleSSE rebuilds the assistant message from a raw SSE body. Handles
// both protocols (chunk shapes are self-describing enough that protocol is
// only a hint). Returns nil if no data line parses.
func ReassembleSSE(raw string) *StreamSummary {
	s := &StreamSummary{}
	var reasoning, content strings.Builder
	tools := map[int]*ToolCall{}
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
					tc = &ToolCall{}
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
			if m, _ := Nested(obj, "message", "model").(string); m != "" {
				s.Model = m
			}
		case "content_block_start":
			idx := int(num(obj["index"]))
			if cb, _ := obj["content_block"].(map[string]any); cb != nil && cb["type"] == "tool_use" {
				name, _ := cb["name"].(string)
				id, _ := cb["id"].(string)
				tools[idx] = &ToolCall{ID: id, Name: name}
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
			if sr, _ := Nested(obj, "delta", "stop_reason").(string); sr != "" {
				s.Finish = sr
			}
		case "response.completed":
			// openai-responses: unlike the openai/anthropic branches above,
			// this doesn't accumulate per-delta events — Responses' delta
			// event field names (response.output_text.delta, .../
			// function_call_arguments.delta, ...) aren't confirmed against
			// real traffic yet, and guessing them risks silently
			// mis-assembling content (worse than not trying). "completed" is
			// the one terminal event guaranteed present and self-contained:
			// it carries the full final Response object (nested under
			// "response"), same "output" typed-Item array shape FinalMessage
			// already parses for the non-streaming case — so this reuses
			// responsesFinalMessage instead of a second implementation.
			if resp, _ := obj["response"].(map[string]any); resp != nil {
				if rs, ok := responsesFinalMessage(resp); ok {
					if rs.Model != "" {
						s.Model = rs.Model
					}
					if rs.Finish != "" {
						s.Finish = rs.Finish
					}
					content.WriteString(rs.Content)
					reasoning.WriteString(rs.Reasoning)
					s.ToolCalls = append(s.ToolCalls, rs.ToolCalls...)
				}
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
		d.name, _ = Nested(m, "function", "name").(string)
		d.args, _ = Nested(m, "function", "arguments").(string)
		out = append(out, d)
	}
	return out
}

// FinalMessage extracts the assistant output from a non-streaming JSON
// response body (either protocol). ok=false when the shape isn't recognized.
func FinalMessage(body any) (*StreamSummary, bool) {
	obj, ok := body.(map[string]any)
	if !ok {
		return nil, false
	}
	s := &StreamSummary{}
	s.Model, _ = obj["model"].(string)
	// openai: choices[0].message
	if choices, _ := obj["choices"].([]any); len(choices) > 0 {
		ch, _ := choices[0].(map[string]any)
		s.Finish, _ = ch["finish_reason"].(string)
		msg, _ := ch["message"].(map[string]any)
		s.Content = RenderContent(msg["content"])
		s.Reasoning, _ = msg["reasoning_content"].(string)
		s.ToolCalls = ToolCallList(msg["tool_calls"])
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
				s.ToolCalls = append(s.ToolCalls, ToolCall{ID: id, Name: name, Args: jsonIndent(m["input"])})
			}
		}
		s.Content = content.String()
		return s, true
	}
	// openai-responses: top-level "output" typed-Item array
	if s, ok := responsesFinalMessage(obj); ok {
		return s, true
	}
	return nil, false
}

// responsesFinalMessage parses a Responses-protocol response object's
// content: the top-level "output" typed-Item array (message/reasoning/
// function_call — the same vocabulary responsesItemMessage already
// categorizes on the request side), "model", and "status" (Responses'
// finish_reason/stop_reason equivalent). false when there's no "output"
// array at all, i.e. this isn't a Responses-shaped object.
//
// Shared between FinalMessage (given a non-streaming JSON body directly)
// and ReassembleSSE's "response.completed" case (given that event's nested
// "response" object) — both carry the identical shape, so there is exactly
// one place that decides how a Responses output Item becomes display text.
func responsesFinalMessage(obj map[string]any) (*StreamSummary, bool) {
	output, ok := obj["output"].([]any)
	if !ok {
		return nil, false
	}
	s := &StreamSummary{}
	s.Model, _ = obj["model"].(string)
	s.Finish, _ = obj["status"].(string)
	var content strings.Builder
	for _, raw := range output {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch m["type"] {
		case "message":
			content.WriteString(RenderContent(m["content"]))
		case "reasoning":
			s.Reasoning += reasoningSummaryText(m)
		case "function_call":
			name, _ := m["name"].(string)
			args, _ := m["arguments"].(string)
			id, _ := m["call_id"].(string)
			s.ToolCalls = append(s.ToolCalls, ToolCall{ID: id, Name: name, Args: args})
		}
	}
	s.Content = content.String()
	return s, true
}
