// Ver 2026-07-28 23:05, by Sonnet 5

package profile

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"vmr/internal/chatmsg"
)

// OpenClawAware ports internal/report/session.go's realUserText/NoReply
// heuristics verbatim — patterns that corpus already validated (in
// particular: the compaction-summary marker must be checked at the
// message head, not anywhere in the text, or a tool_result that happens to
// quote it gets misread as a real turn boundary). It is harmless on input
// from any other agent: none of these patterns match generic chat text, so
// a non-OpenClaw message just falls through to "non-empty user text counts
// as real", the same baseline Generic uses.
var OpenClawAware Profile = openClawAware{}

type openClawAware struct{}

func (openClawAware) Name() string { return "openclaw" }

// openClawEnvelopeRe matches OpenClaw's "Conversation info (untrusted
// metadata)" / "Sender (untrusted metadata)" JSON blocks glued onto the
// front of real inbound messages.
var openClawEnvelopeRe = regexp.MustCompile(`(?s)(?:Conversation info|Sender) \(untrusted metadata\):\s*` + "```" + `(?:json)?\n.*?` + "```" + `\s*`)

func stripOpenClawEnvelope(text string) string {
	return strings.TrimSpace(openClawEnvelopeRe.ReplaceAllString(text, ""))
}

// leadingBracketRe matches OpenClaw's "[Day Mon DD HH:MM TZ]" user-typed-
// time prefix, so a message that's just a timestamp plus an
// (already-stripped) envelope isn't mistaken for a real instruction.
var leadingBracketRe = regexp.MustCompile(`^\[[^\]]*\]\s*`)

func (openClawAware) RealUserText(m chatmsg.Message, rawMsgs []any, rawIdx int) (string, bool) {
	if m.Role != "user" {
		return "", false
	}
	head := capStr(m.Text, 200)
	if strings.HasPrefix(head, "OpenClaw runtime context") ||
		strings.HasPrefix(head, "Attached image(s) from tool result") ||
		strings.HasPrefix(head, "The conversation history before this point was compacted") {
		return "", false
	}
	text := m.Text
	if strings.Contains(head, "Conversation info (untrusted metadata)") {
		text = stripOpenClawEnvelope(text)
		if leadingBracketRe.ReplaceAllString(text, "") == "" {
			return "", false // just a timestamp bracket, nothing real left
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

func (p openClawAware) IsRealUser(m chatmsg.Message, rawMsgs []any, rawIdx int) bool {
	_, ok := p.RealUserText(m, rawMsgs, rawIdx)
	return ok
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

// capStr caps s at n bytes without cutting a UTF-8 sequence in half — same
// rationale/implementation as internal/report/session.go's capStr.
func capStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
