// Ver 2026-08-20, by Sonnet 5

package taskseg

import (
	"regexp"
	"strings"
)

// openClawEnvelopeRe matches OpenClaw's "Conversation info (untrusted
// metadata)" / "Sender (untrusted metadata)" JSON blocks glued onto the
// front of real inbound messages.
var openClawEnvelopeRe = regexp.MustCompile(`(?s)(?:Conversation info|Sender) \(untrusted metadata\):\s*` + "```" + `(?:json)?\n.*?` + "```" + `\s*`)

func stripOpenClawEnvelope(text string) string {
	return strings.TrimSpace(openClawEnvelopeRe.ReplaceAllString(text, ""))
}

// leadingBracketRe matches ANY leading "[...]" bracket — deliberately
// generic, used only to test whether the (untrusted metadata) envelope
// branch's already-stripped remainder is "just a bracket, nothing real
// left" (a real message never starts with a bracket AND has nothing
// after it in this corpus, so over-matching here has no observed cost).
// The bare-message loop in RealUserText does NOT use this — see
// timestampBracketRe.
var leadingBracketRe = regexp.MustCompile(`^\[[^\]]*\]\s*`)

// timestampBracketRe matches OpenClaw's "[Day[ YYYY-MM-DD HH:MM[ GMT+N]]]"
// user-typed-time prefix specifically (day name required, the rest
// optional) — unlike leadingBracketRe, narrow on purpose: the bare-message
// loop must not misfire on a real message that happens to open with an
// unrelated bracket (e.g. "[Bug] fix the login page").
var timestampBracketRe = regexp.MustCompile(`^\[(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun)(?:\s+\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}(?:\s+GMT[+-]\d+)?)?\]\s*`)

// messageIDBracketRe matches OpenClaw's "[message_id: ...]" bracket, seen
// glued after the timestamp bracket on bare messages.
var messageIDBracketRe = regexp.MustCompile(`^\[message_id:[^\]]*\]\s*`)

// stripLeadingBrackets peels timestamp and message_id brackets off the front
// in a loop, order-agnostic.
func stripLeadingBrackets(text string) string {
	for {
		stripped := timestampBracketRe.ReplaceAllString(text, "")
		stripped = messageIDBracketRe.ReplaceAllString(stripped, "")
		if stripped == text {
			break
		}
		text = stripped
	}
	return text
}
