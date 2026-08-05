// Ver 2026-08-05, by Sonnet 5

package chatmsg

import "testing"

func TestToolResultList_OpenAI(t *testing.T) {
	t.Parallel()
	msgs := []any{
		map[string]any{"role": "user", "content": "go"},
		map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "c1", "function": map[string]any{"name": "read", "arguments": "{}"}},
		}},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": "file contents here"},
	}
	got := ToolResultList(msgs)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	if got[0].CallID != "c1" || got[0].Text != "file contents here" {
		t.Errorf("got %+v", got[0])
	}
	if got[0].IsError {
		t.Error("OpenAI-shaped results have no error field — must always decode as IsError=false")
	}
}

func TestToolResultList_Anthropic(t *testing.T) {
	t.Parallel()
	msgs := []any{
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "tu1", "name": "bash", "input": map[string]any{"cmd": "go build"}},
		}},
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "tu1", "is_error": true, "content": "build failed: syntax error"},
		}},
	}
	got := ToolResultList(msgs)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	if got[0].CallID != "tu1" || got[0].Text != "build failed: syntax error" || !got[0].IsError {
		t.Errorf("got %+v", got[0])
	}
}

func TestToolResultList_MultipleAnthropicPartsInOneMessage(t *testing.T) {
	t.Parallel()
	msgs := []any{
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "tu1", "content": "ok1"},
			map[string]any{"type": "tool_result", "tool_use_id": "tu2", "is_error": true, "content": "ok2 failed"},
		}},
	}
	got := ToolResultList(msgs)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if got[0].CallID != "tu1" || got[0].IsError {
		t.Errorf("first result: %+v", got[0])
	}
	if got[1].CallID != "tu2" || !got[1].IsError {
		t.Errorf("second result: %+v", got[1])
	}
}

func TestToolResultList_IgnoresNonResultParts(t *testing.T) {
	t.Parallel()
	msgs := []any{
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "let me check"},
		}},
		map[string]any{"role": "user", "content": "plain string content, no tool_call_id"},
	}
	if got := ToolResultList(msgs); len(got) != 0 {
		t.Errorf("got %d results, want 0: %+v", len(got), got)
	}
}
