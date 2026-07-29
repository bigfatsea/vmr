// Ver 2026-07-28 23:05, by Sonnet 5

package profile

import (
	"strings"
	"testing"

	"vmr/internal/chatmsg"
)

// These cases mirror internal/report/session_test.go's TestIsRealUserScaffolding
// /TestRealUserTextStripsEnvelope — same corpus-validated behavior, ported
// verbatim rather than re-derived, since this package's whole reason to
// exist is "the exact same heuristics, reachable without depending on
// internal/report" (design doc §7.3/§11 D5).
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
