// Ver 2026-08-14, by Sonnet 5

package taskseg

import (
	"regexp"
	"strings"

	"vmr/internal/chatmsg"
	"vmr/internal/fmtutil"
)

// OpenClawAware ports internal/report/session.go's original realUserText/
// NoReply/chat_id heuristics verbatim — patterns that corpus already
// validated (in particular: the compaction-summary marker must be checked
// at the message head, not anywhere in the text, or a tool_result that
// happens to quote it gets misread as a real turn boundary). It is harmless
// on input from any other agent: none of these patterns match generic chat
// text, so a non-OpenClaw message just falls through to "non-empty user
// text counts as real", the same baseline Generic uses.
var OpenClawAware Profile = openClawAware{}

type openClawAware struct{}

func (openClawAware) Name() string { return "openclaw" }

// RealUserText's classification rules: transport scaffolding never counts —
// OpenClaw runtime wrappers, tool-produced image attachments, compaction
// summaries, and anthropic messages that are purely tool_result parts.
// OpenClaw's metadata envelope (chat_id/sender JSON) is stripped rather than
// disqualifying the whole message — the real ask is often glued right
// behind it, and discarding it entirely made task titles fall back to an
// earlier, unrelated message (observed in real logs: a 06:48 launch
// instruction wrapped in the envelope was dropped, so the task title showed
// an unrelated "continue" ping from 6 minutes earlier instead). A message
// that's PURELY the envelope — nothing real left after stripping — still
// doesn't count.
func (openClawAware) RealUserText(m chatmsg.Message, rawMsgs []any, rawIdx int) (string, bool) {
	if m.Role != "user" {
		return "", false
	}
	head := fmtutil.CapStr(m.Text, 200)
	if strings.HasPrefix(head, "OpenClaw runtime context") ||
		strings.HasPrefix(head, "Attached image(s) from tool result") ||
		strings.HasPrefix(head, "The conversation history before this point was compacted") {
		return "", false
	}
	text := m.Text
	if strings.Contains(head, "(untrusted metadata)") {
		text = stripOpenClawEnvelope(text)
		if leadingBracketRe.ReplaceAllString(text, "") == "" {
			return "", false // just a timestamp bracket, nothing real left
		}
	} else {
		// Bare message: peel timestamp/message_id brackets off the front
		// in a loop, order-agnostic — not a generic "strip any leading
		// bracket" rule (misfires on a message that opens with one).
		for {
			stripped := timestampBracketRe.ReplaceAllString(text, "")
			stripped = messageIDBracketRe.ReplaceAllString(stripped, "")
			if stripped == text {
				break
			}
			text = stripped
		}
		if strings.TrimSpace(text) == "" {
			return "", false // only scaffolding brackets, nothing real left
		}
	}
	if rawIdx >= 0 && rawIdx < len(rawMsgs) {
		if rm, ok := rawMsgs[rawIdx].(map[string]any); ok {
			if parts, ok := rm["content"].([]any); ok && len(parts) > 0 {
				allToolResult := true
				for _, p := range parts {
					pm, _ := p.(map[string]any)
					if pm == nil || pm["type"] != "tool_result" {
						allToolResult = false
						break
					}
				}
				if allToolResult {
					return "", false
				}
			}
		}
	}
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	return text, true
}

func (openClawAware) NoReply(finish, content string) bool {
	if finish != "stop" && finish != "end_turn" {
		return false
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return true
	}
	return strings.HasSuffix(trimmed, "NO_REPLY")
}

// chatIDRe extracts OpenClaw's chat_id from its "Conversation info
// (untrusted metadata)" JSON wrapper.
var chatIDRe = regexp.MustCompile(`"chat_id"\s*:\s*"([^"]+)"`)

// ChatID scans msgs from the end (the wrapper is glued onto the most recent
// user turn, not necessarily the first) for a user message carrying one of
// OpenClaw's "(untrusted metadata)" envelopes and extracts its chat_id. The
// trigger check is head-bounded the same way RealUserText's is — the
// envelope is always glued onto the FRONT of a message, so a message whose
// first 200 bytes don't mention it can't be a match regardless of how long
// the rest of the message is (a large tool result or attachment shouldn't
// cost a full-length scan just to be ruled out). The regex extraction itself
// still runs over the full text once triggered, unbounded — chat_id's exact
// offset within the envelope JSON isn't guaranteed to fit any fixed window.
func (openClawAware) ChatID(msgs []chatmsg.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		head := fmtutil.CapStr(msgs[i].Text, 200)
		if !strings.Contains(head, "(untrusted metadata)") {
			continue
		}
		if m := chatIDRe.FindStringSubmatch(msgs[i].Text); m != nil {
			return m[1]
		}
	}
	return ""
}
