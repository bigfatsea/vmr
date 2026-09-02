// Ver 2026-08-14, by Sonnet 5

package taskseg

import (
	"strings"
	"testing"

	"vmr/internal/chatmsg"
)

// These cases carry forward the corpus-validated behavior that used to live
// in internal/report/session_test.go before tests were consolidated here
// heuristics themselves here and deleted the byte-identical duplicate tests.
func TestOpenClawAware_ScaffoldingRejected(t *testing.T) {
	for _, text := range []string{
		"OpenClaw runtime context for the immediately preceding user message.\nInternal.",
		"[Thu] Conversation info (untrusted metadata):\n```json\n{}\n```",
		"Attached image(s) from tool result:",
		"The conversation history before this point was compacted into the following summary:\n<summary>x</summary>",
	} {
		if _, ok := OpenClawAware.RealUserText(chatmsg.Message{Role: "user", Text: text}, nil, -1); ok {
			t.Errorf("scaffolding counted as real user: %q", text[:40])
		}
	}
	if _, ok := OpenClawAware.RealUserText(chatmsg.Message{Role: "user", Text: "帮我修个 bug"}, nil, -1); !ok {
		t.Error("real instruction not recognized")
	}
}

func TestOpenClawAware_RealUserTextStripsEnvelope(t *testing.T) {
	wrapped := "[Thu 2026-07-09 06:48 GMT+8] Conversation info (untrusted metadata):\n" +
		"```json\n{\"chat_id\":\"user:ou_x\"}\n```\n\n" +
		"Sender (untrusted metadata):\n```json\n{\"id\":\"ou_x\"}\n```\n\n" +
		"OK，基于你为每个风格设计的提示词，调用 ai-script 批量生成 logo 设计图。"
	text, ok := OpenClawAware.RealUserText(chatmsg.Message{Role: "user", Text: wrapped}, nil, -1)
	if !ok {
		t.Fatal("envelope-wrapped real instruction not recognized")
	}
	if !strings.Contains(text, "OK，基于你为每个风格设计的提示词") {
		t.Errorf("stripped text lost the real instruction: %q", text)
	}
	if strings.Contains(text, "chat_id") {
		t.Errorf("stripped text still carries the JSON envelope: %q", text)
	}
}

func TestOpenClawAware_TimestampOnlyAfterStrip(t *testing.T) {
	wrapped := "[Thu 2026-07-09 06:48 GMT+8] Conversation info (untrusted metadata):\n```json\n{}\n```"
	if _, ok := OpenClawAware.RealUserText(chatmsg.Message{Role: "user", Text: wrapped}, nil, -1); ok {
		t.Error("pure timestamp-plus-envelope should not count as real")
	}
}

func TestOpenClawAware_EnvelopeFollowedByScaffoldingBrackets(t *testing.T) {
	wrapped := "[Thu 2026-07-09 06:48 GMT+8] Conversation info (untrusted metadata):\n" +
		"```json\n{\"chat_id\":\"user:ou_x\"}\n```\n\n" +
		"[message_id: 12345] [Fri 2026-07-10 10:00 GMT+8] 帮我修个 bug"
	text, ok := OpenClawAware.RealUserText(chatmsg.Message{Role: "user", Text: wrapped}, nil, -1)
	if !ok {
		t.Fatal("wrapped instruction not recognized")
	}
	if text != "帮我修个 bug" {
		t.Errorf("got %q, want %q (scaffolding brackets stripped after envelope)", text, "帮我修个 bug")
	}
}

// TestOpenClawAware_SenderEnvelopeAloneStripped pins the fix for a head-check/
// regex mismatch: openClawEnvelopeRe matches EITHER "Conversation info" or
// "Sender" (untrusted metadata) headers, but the 200-byte trigger check used
// to only look for "Conversation info", so a message carrying only a
// "Sender" envelope at its head bypassed stripOpenClawEnvelope entirely and
// leaked the raw JSON envelope into the task title.
func TestOpenClawAware_SenderEnvelopeAloneStripped(t *testing.T) {
	wrapped := "Sender (untrusted metadata):\n```json\n{\"id\":\"ou_x\"}\n```\n\n帮我修个 bug"
	text, ok := OpenClawAware.RealUserText(chatmsg.Message{Role: "user", Text: wrapped}, nil, -1)
	if !ok {
		t.Fatal("Sender-only envelope wrapped instruction not recognized")
	}
	if !strings.Contains(text, "帮我修个 bug") {
		t.Errorf("stripped text lost the real instruction: %q", text)
	}
	if strings.Contains(text, "\"id\"") {
		t.Errorf("stripped text still carries the JSON envelope: %q", text)
	}
}

func TestOpenClawAware_PureToolResultRejected(t *testing.T) {
	raw := []any{
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "ok"},
		}},
	}
	m := chatmsg.Message{Role: "user", Text: "some rendered text"}
	if _, ok := OpenClawAware.RealUserText(m, raw, 0); ok {
		t.Error("an all-tool_result raw message should not count as real user text")
	}
}

func TestOpenClawAware_NonUserRoleRejected(t *testing.T) {
	if _, ok := OpenClawAware.RealUserText(chatmsg.Message{Role: "assistant", Text: "hi"}, nil, -1); ok {
		t.Error("assistant-role message should never count as real user text")
	}
}

// TestOpenClawAware_BareMessageStripsScaffoldingBrackets pins a real corpus
// leak: a "bare" message (no (untrusted metadata) envelope) glues a
// timestamp bracket and a message_id bracket directly onto the real
// instruction, and neither used to be stripped on this path — the message
// only got envelope handling, which bare messages never trigger.
func TestOpenClawAware_BareMessageStripsScaffoldingBrackets(t *testing.T) {
	text, ok := OpenClawAware.RealUserText(chatmsg.Message{Role: "user",
		Text: "[Tue 2026-07-28 00:05 GMT+8] [message_id: om_x100b694c53b4eca8b1cd50932b7aefe] 修一下这个 bug"}, nil, -1)
	if !ok {
		t.Fatal("bare message with scaffolding brackets not recognized as real")
	}
	if strings.Contains(text, "[") || strings.Contains(text, "]") {
		t.Errorf("scaffolding brackets leaked into stripped text: %q", text)
	}
	if !strings.Contains(text, "修一下这个 bug") {
		t.Errorf("stripped text lost the real instruction: %q", text)
	}
}

// TestOpenClawAware_BareMessagePureScaffoldingRejected is the boundary case
// for the above: only the two known brackets, nothing real behind them.
func TestOpenClawAware_BareMessagePureScaffoldingRejected(t *testing.T) {
	if _, ok := OpenClawAware.RealUserText(chatmsg.Message{Role: "user",
		Text: "[Tue 2026-07-28 00:05 GMT+8] [message_id: om_x100b694c53b4eca8b1cd50932b7aefe]"}, nil, -1); ok {
		t.Error("pure scaffolding brackets with nothing real left should not count as real")
	}
}

// TestOpenClawAware_BareMessageLeadingUserBracketSurvives pins the
// narrow-regex design: timestampBracketRe must NOT match an arbitrary
// leading bracket a real message legitimately opens with (a bug prefix, a
// priority tag, a numbered reference) — only OpenClaw's own day-name
// timestamp shape. A generic "strip any leading bracket" rule here would
// both mangle this text and, for a message that's ONLY the bracket (e.g.
// "[WIP]"), wrongly reject it as pure scaffolding.
func TestOpenClawAware_BareMessageLeadingUserBracketSurvives(t *testing.T) {
	for _, text := range []string{
		"[Bug] login page throws a 500",
		"[P0] fix the crash",
		"[1] see the referenced doc",
	} {
		got, ok := OpenClawAware.RealUserText(chatmsg.Message{Role: "user", Text: text}, nil, -1)
		if !ok {
			t.Errorf("legitimate bracket-prefixed message rejected: %q", text)
		}
		if got != text {
			t.Errorf("legitimate bracket-prefixed message mangled: got %q, want unchanged %q", got, text)
		}
	}
}

func TestOpenClawAware_NoReply(t *testing.T) {
	cases := []struct {
		finish, content string
		want            bool
	}{
		{"stop", "", true},
		{"end_turn", "  ", true},
		{"stop", "some reply", false},
		{"stop", "done.\nNO_REPLY", true},
		{"tool_calls", "", false}, // not a terminal finish, not a skip
		{"length", "truncated content", false},
	}
	for _, c := range cases {
		if got := OpenClawAware.NoReply(c.finish, c.content); got != c.want {
			t.Errorf("NoReply(%q, %q) = %v, want %v", c.finish, c.content, got, c.want)
		}
	}
}

// TestOpenClawAware_ChatID mirrors internal/report/session_test.go's
// TestAnalyzeSessionsGrouping chat_id assertion, at the Profile-method level
// rather than through a full AnalyzeSessions run.
func TestOpenClawAware_ChatID(t *testing.T) {
	msgs := []chatmsg.Message{
		{Role: "system", Text: "You are a personal assistant."},
		{Role: "user", Text: "任务开始"},
		{Role: "user", Text: "[Thu 2026-07-09 10:00 GMT+8] Conversation info (untrusted metadata):\n```json\n{\"chat_id\":\"user:ou_test123\"}\n```"},
	}
	if got := OpenClawAware.ChatID(msgs); got != "user:ou_test123" {
		t.Errorf("ChatID = %q, want user:ou_test123", got)
	}
}

func TestOpenClawAware_ChatID_ScansFromEnd(t *testing.T) {
	// Two envelope-carrying messages with different chat_ids: the most
	// recent (last) one wins — the wrapper reflects the current turn's
	// routing, not a stale earlier one.
	msgs := []chatmsg.Message{
		{Role: "user", Text: "Conversation info (untrusted metadata):\n```json\n{\"chat_id\":\"old\"}\n```"},
		{Role: "assistant", Text: "ok"},
		{Role: "user", Text: "Conversation info (untrusted metadata):\n```json\n{\"chat_id\":\"new\"}\n```"},
	}
	if got := OpenClawAware.ChatID(msgs); got != "new" {
		t.Errorf("ChatID = %q, want %q", got, "new")
	}
}

func TestOpenClawAware_ChatID_NoEnvelopeIsEmpty(t *testing.T) {
	msgs := []chatmsg.Message{
		{Role: "system", Text: "sys"},
		{Role: "user", Text: "plain text, no envelope"},
	}
	if got := OpenClawAware.ChatID(msgs); got != "" {
		t.Errorf("ChatID = %q, want empty", got)
	}
}

// TestOpenClawAware_ChatID_LargeTailMessage locks in that the head-bounded
// trigger check (matching RealUserText's own 200-byte head check) doesn't
// cost a full-length scan for a large message, while still finding a
// chat_id that's within the envelope even when the envelope's OWN JSON body
// (not just the trailing message content) extends well past 200 bytes —
// only the "does this message start with the trigger phrase" check is
// head-bounded, the regex extraction itself always runs over the full text.
func TestOpenClawAware_ChatID_LargeTailMessage(t *testing.T) {
	envelope := "Conversation info (untrusted metadata):\n```json\n{\"chat_id\":\"user:ou_big\",\"padding\":\"" +
		strings.Repeat("x", 1000) + "\"}\n```\n\n"
	msgs := []chatmsg.Message{
		{Role: "user", Text: envelope + strings.Repeat("real instruction text ", 5000)},
	}
	if got := OpenClawAware.ChatID(msgs); got != "user:ou_big" {
		t.Errorf("ChatID = %q, want user:ou_big", got)
	}
}

// TestOpenClawAware_ChatID_EnvelopeBeyondHeadWindowMissed documents the
// accepted limitation the head-bounded trigger check inherits from
// RealUserText's own identical convention: if something other than the
// envelope itself pushes the trigger phrase past the first 200 bytes, the
// chat_id is missed. This isn't a real-world shape (OpenClaw always glues
// the envelope onto the very front of the message) — this test exists so a
// future change to the window size is a deliberate edit, not a silent
// behavior shift.
func TestOpenClawAware_ChatID_EnvelopeBeyondHeadWindowMissed(t *testing.T) {
	msgs := []chatmsg.Message{
		{Role: "user", Text: strings.Repeat("padding ", 50) + "Conversation info (untrusted metadata):\n```json\n{\"chat_id\":\"user:ou_late\"}\n```"},
	}
	if got := OpenClawAware.ChatID(msgs); got != "" {
		t.Errorf("ChatID = %q, want empty (envelope trigger phrase starts past the 200-byte head window)", got)
	}
}
