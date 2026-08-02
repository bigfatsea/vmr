// Ver 2026-07-28 22:15, by Sonnet 5

// Chat message/SSE/usage parsing has moved to internal/chatmsg (see that
// package's doc comment for why: internal/ctxgraph and internal/story also
// need it, and report's own copy would otherwise have had to be either
// duplicated a second time or exported wholesale). This file is the thin
// delegation layer that keeps every existing call site in detail.go/
// session.go/render.go (and their tests) working unchanged — same private
// names, same types (via alias, so struct literals like
// `chatMessage{Role: ..., Text: ...}` still compile as-is).
package report

import "vmr/internal/chatmsg"

type chatMessage = chatmsg.Message

func chatMessages(body any) []chatMessage { return chatmsg.Messages(body) }
func renderContent(v any) string          { return chatmsg.RenderContent(v) }
func renderPart(m map[string]any) string  { return chatmsg.RenderPart(m) }
func imagePlaceholder(u string) string    { return chatmsg.ImagePlaceholder(u) }
func toolNames(body any) []string         { return chatmsg.ToolNames(body) }
func msgOffset(body map[string]any) int   { return chatmsg.MsgOffset(body) }
func rawArray(body map[string]any) []any  { return chatmsg.RawArray(body) }
func responsesItemMessage(m map[string]any) (role, text string) {
	return chatmsg.ResponsesItemMessage(m)
}

type toolCall = chatmsg.ToolCall

func toolCallList(v any) []toolCall { return chatmsg.ToolCallList(v) }

type streamSummary = chatmsg.StreamSummary

func reassembleSSE(raw string) *streamSummary      { return chatmsg.ReassembleSSE(raw) }
func finalMessage(body any) (*streamSummary, bool) { return chatmsg.FinalMessage(body) }

type Usage = chatmsg.Usage

// ExtractUsage is exported (report's own public API, used by cmd_report.go
// callers and external tooling) — kept as a real wrapper, not a bare var
// alias, so its doc comment stays discoverable via `go doc`.
func ExtractUsage(body any) (Usage, bool) { return chatmsg.ExtractUsage(body) }
func extractFinish(body any) string       { return chatmsg.ExtractFinish(body) }
