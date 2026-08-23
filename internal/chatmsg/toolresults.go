// Ver 2026-08-05, by Sonnet 5

// ToolResultList is CheckToolPairing's content-carrying counterpart:
// CheckToolPairing only proves every tool_call/tool_use id has a matching
// result (F9's causal-pairing invariant), it doesn't hand back the result's
// own content or error status. Several internal/story Finding detectors
// (Phase 2's precise-retry/unused-result/context-poisoning candidates) need
// exactly that — which tool_call's result errored, what it actually said —
// and the only place that existed before this was Event.Msg.Text, a whole
// Step's flattened-and-deduped text blob with no per-call boundary.
package chatmsg

import "strings"

// NormalizeToolCallID strips underscores from a tool call ID — the deterministic
// rewrite that the OpenClaw family of clients applies when echoing a tool_call id
// back into the next request's history ("call_00_xHodG…" -> "call00xHodG…").
func NormalizeToolCallID(id string) string { return strings.ReplaceAll(id, "_", "") }

// ToolResult is one tool_call's answering tool_result, decoded from a
// request body's raw message list.
type ToolResult struct {
	CallID  string // matches the answering ToolCall.ID
	Text    string // RenderContent's display text for this result's content
	IsError bool   // Anthropic's explicit is_error field; always false for OpenAI-shaped results — see errorRecoveryCount's doc comment for why (no equivalent standard field exists)
}

// ToolResultList decodes every tool_call_id/tool_use_id-bearing result in
// rawMsgs (a decoded request body's "messages" array) into a ToolResult —
// same two-protocol scan CheckToolPairing already does (OpenAI's top-level
// tool_call_id + content on a role=="tool" message, Anthropic's
// tool_result content part), extended to also capture content and
// is_error instead of just proving the id matched.
func ToolResultList(rawMsgs []any) []ToolResult {
	var out []ToolResult
	for _, raw := range rawMsgs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		// OpenAI: a tool-result message carries tool_call_id at top level.
		if id, _ := m["tool_call_id"].(string); id != "" {
			out = append(out, ToolResult{CallID: id, Text: RenderContent(m["content"])})
		}
		// Anthropic: tool_result lives as a content part.
		parts, _ := m["content"].([]any)
		for _, p := range parts {
			pm, ok := p.(map[string]any)
			if !ok || pm["type"] != "tool_result" {
				continue
			}
			id, _ := pm["tool_use_id"].(string)
			if id == "" {
				continue
			}
			isErr, _ := pm["is_error"].(bool)
			out = append(out, ToolResult{CallID: id, Text: RenderContent(pm["content"]), IsError: isErr})
		}
	}
	return out
}
