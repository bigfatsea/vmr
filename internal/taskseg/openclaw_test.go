// Ver 2026-08-14, by Sonnet 5

package taskseg

import (
	"strings"
	"testing"

	"vmr/internal/chatmsg"
)

// These cases mirror internal/report/session_test.go's TestIsRealUserScaffolding
// /TestRealUserTextStripsEnvelope — same corpus-validated behavior, ported
// verbatim rather than re-derived, since this package's whole reason to
// exist is "the exact same heuristics, reachable without depending on
// internal/report".
func TestOpenClawAware_ScaffoldingRejected(t *testing.T) {
	for _, text := range []string{
		"OpenClaw runtime context for the immediately preceding user message.\nInternal.",
		"[Thu] Conversation info (untrusted metadata):\n```json\n{}\n```",
		"Attached image(s) from tool result:",
		"The conversation history before this point was compacted into the following summary:\n<summary>x</summary>",
	} {
		if OpenClawAware.IsRealUser(chatmsg.Message{Role: "user", Text: text}, nil, -1) {
			t.Errorf("scaffolding counted as real user: %q", text[:40])
		}
	}
	if !OpenClawAware.IsRealUser(chatmsg.Message{Role: "user", Text: "帮我修个 bug"}, nil, -1) {
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
	if OpenClawAware.IsRealUser(chatmsg.Message{Role: "user", Text: wrapped}, nil, -1) {
		t.Error("pure timestamp-plus-envelope should not count as real")
	}
}

func TestOpenClawAware_PureToolResultRejected(t *testing.T) {
	raw := []any{
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "ok"},
		}},
	}
	m := chatmsg.Message{Role: "user", Text: "some rendered text"}
	if OpenClawAware.IsRealUser(m, raw, 0) {
		t.Error("an all-tool_result raw message should not count as real user text")
	}
}

func TestOpenClawAware_NonUserRoleRejected(t *testing.T) {
	if OpenClawAware.IsRealUser(chatmsg.Message{Role: "assistant", Text: "hi"}, nil, -1) {
		t.Error("assistant-role message should never count as real user text")
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
