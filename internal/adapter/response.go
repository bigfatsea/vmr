// Ver 2026-08-28, by Sonnet 5

package adapter

import (
	"encoding/json"
	"unicode/utf8"
)

// ResponseAssistantText inspects a fully buffered NON-STREAMING upstream 2xx
// body and reports how much assistant text it carries and whether it also
// carries a tool call. It exists for one caller — internal/router's
// soft_block_failover check — which needs a protocol-aware "did this
// response actually answer, or is it an empty content-policy block wearing a
// 200" verdict without importing the analytics-half parser (chatmsg).
//
// textRunes is the rune count of the concatenated assistant text
// (choices[].message.content for openai-completions, content[].text for
// anthropic-messages, output[].content[].text for openai-responses).
// hasToolCall is true when the response instead (or also) contains a
// tool_call / tool_use — an empty-text response with a tool call is a
// perfectly normal turn, never a block. ok is false when the body doesn't
// parse or doesn't match the protocol's response shape at all, in which case
// the caller must not draw any conclusion from textRunes.
func ResponseAssistantText(protocol string, body []byte) (textRunes int, hasToolCall bool, ok bool) {
	switch protocol {
	case "openai-completions":
		return openaiCompletionText(body)
	case "anthropic-messages":
		return anthropicMessageText(body)
	case "openai-responses":
		return responsesOutputText(body)
	default:
		return 0, false, false
	}
}

func openaiCompletionText(body []byte) (int, bool, bool) {
	var r struct {
		Choices []struct {
			Message struct {
				Content   json.RawMessage   `json:"content"`
				ToolCalls []json.RawMessage `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &r); err != nil || len(r.Choices) == 0 {
		return 0, false, false
	}
	n := 0
	tool := false
	for _, c := range r.Choices {
		n += rawStringRunes(c.Message.Content)
		if len(c.Message.ToolCalls) > 0 {
			tool = true
		}
	}
	return n, tool, true
}

func anthropicMessageText(body []byte) (int, bool, bool) {
	var r struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &r); err != nil || r.Type != "message" {
		return 0, false, false
	}
	n := 0
	tool := false
	for _, block := range r.Content {
		switch block.Type {
		case "text":
			n += utf8.RuneCountInString(block.Text)
		case "tool_use":
			tool = true
		}
	}
	return n, tool, true
}

func responsesOutputText(body []byte) (int, bool, bool) {
	var r struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &r); err != nil || len(r.Output) == 0 {
		return 0, false, false
	}
	n := 0
	tool := false
	for _, item := range r.Output {
		switch item.Type {
		case "function_call", "tool_call":
			tool = true
		case "message", "":
			for _, block := range item.Content {
				if block.Type == "output_text" || block.Type == "text" {
					n += utf8.RuneCountInString(block.Text)
				}
			}
		}
	}
	return n, tool, true
}

// rawStringRunes counts the runes in a JSON value that is expected to be a
// string; anything else (null, an array, an object) counts as zero.
func rawStringRunes(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0
	}
	return utf8.RuneCountInString(s)
}
