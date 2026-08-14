// Ver 2026-08-14, by Sonnet 5

package taskseg

import (
	"testing"

	"vmr/internal/chatmsg"
)

func TestGeneric_AnyNonEmptyUserTextIsReal(t *testing.T) {
	// Unlike OpenClawAware, Generic has no envelope/scaffolding knowledge —
	// text that OpenClawAware would strip or reject passes through here,
	// which is the whole point: it's the fallback for agents with
	// different (or no) transport conventions.
	text := "OpenClaw runtime context for the immediately preceding user message."
	if !Generic.IsRealUser(chatmsg.Message{Role: "user", Text: text}, nil, -1) {
		t.Error("Generic should not recognize OpenClaw-specific scaffolding markers")
	}
}

func TestGeneric_EmptyRejected(t *testing.T) {
	if Generic.IsRealUser(chatmsg.Message{Role: "user", Text: "   "}, nil, -1) {
		t.Error("whitespace-only text should not count as real")
	}
}

func TestGeneric_NonUserRoleRejected(t *testing.T) {
	if Generic.IsRealUser(chatmsg.Message{Role: "assistant", Text: "hi"}, nil, -1) {
		t.Error("assistant-role message should never count as real user text")
	}
}

func TestGeneric_NoReply(t *testing.T) {
	if !Generic.NoReply("stop", "") {
		t.Error("empty content at a terminal finish should be NoReply")
	}
	if Generic.NoReply("stop", "done.\nNO_REPLY") {
		t.Error("Generic has no NO_REPLY marker convention — should not trigger on it")
	}
	if Generic.NoReply("tool_calls", "") {
		t.Error("non-terminal finish should never be NoReply")
	}
}

func TestGeneric_ChatIDAlwaysEmpty(t *testing.T) {
	msgs := []chatmsg.Message{
		{Role: "user", Text: "Conversation info (untrusted metadata):\n```json\n{\"chat_id\":\"x\"}\n```"},
	}
	if got := Generic.ChatID(msgs); got != "" {
		t.Errorf("Generic.ChatID = %q, want empty (no framework convention to recognize)", got)
	}
}
