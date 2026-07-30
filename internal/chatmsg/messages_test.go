// Ver 2026-07-28 22:10, by Sonnet 5

package chatmsg

import (
	"strings"
	"testing"
)

func TestMessages_OpenAI(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "user", "content": "hello"},
			map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
				map[string]any{"id": "c1", "function": map[string]any{"name": "exec", "arguments": `{"cmd":"ls"}`}},
			}},
			map[string]any{"role": "tool", "tool_call_id": "c1", "content": "file1\nfile2"},
		},
	}
	msgs := Messages(body)
	if len(msgs) != 4 {
		t.Fatalf("got %d messages, want 4", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Text != "sys" {
		t.Errorf("system message: %+v", msgs[0])
	}
	if msgs[2].Role != "assistant" || msgs[2].Text == "" {
		t.Errorf("assistant tool_call not rendered: %+v", msgs[2])
	}
	if msgs[3].Role != "tool" {
		t.Errorf("tool message role: %+v", msgs[3])
	}
}

func TestMessages_AnthropicSystemPrepended(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"system": "you are helpful",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	msgs := Messages(body)
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2 (system prepended)", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Text != "you are helpful" {
		t.Errorf("prepended system: %+v", msgs[0])
	}
	if MsgOffset(body) != 1 {
		t.Errorf("MsgOffset with top-level system = %d, want 1", MsgOffset(body))
	}
	if MsgOffset(map[string]any{"messages": []any{}}) != 0 {
		t.Errorf("MsgOffset without top-level system should be 0")
	}
}

func TestMessages_AnthropicToolResultAndImage(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "tool_use", "name": "read", "input": map[string]any{"path": "a.go"}},
			}},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "ok"},
				map[string]any{"type": "image", "source": map[string]any{"media_type": "image/png", "data": "AAAA"}},
			}},
		},
	}
	msgs := Messages(body)
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if !strings.Contains(msgs[0].Text, "tool_use read") {
		t.Errorf("tool_use not rendered: %q", msgs[0].Text)
	}
	if !strings.Contains(msgs[1].Text, "tool_result") || !strings.Contains(msgs[1].Text, "image") {
		t.Errorf("tool_result/image not rendered: %q", msgs[1].Text)
	}
}

func TestRenderContent_Nil(t *testing.T) {
	t.Parallel()
	if got := RenderContent(nil); got != "" {
		t.Errorf("RenderContent(nil) = %q, want empty", got)
	}
}

func TestImagePlaceholder_DataURL(t *testing.T) {
	t.Parallel()
	got := ImagePlaceholder("data:image/jpeg;base64,QUFBQQ==")
	if !strings.Contains(got, "image/jpeg") {
		t.Errorf("ImagePlaceholder data url = %q", got)
	}
}

func TestImagePlaceholder_RemoteURL(t *testing.T) {
	t.Parallel()
	got := ImagePlaceholder("https://example.com/x.png")
	if !strings.Contains(got, "https://example.com/x.png") {
		t.Errorf("ImagePlaceholder remote url = %q", got)
	}
}

func TestToolNames(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "exec"}},
			map[string]any{"name": "read"}, // anthropic shape
		},
	}
	names := ToolNames(body)
	if len(names) != 2 || names[0] != "exec" || names[1] != "read" {
		t.Errorf("ToolNames = %v", names)
	}
}

func TestMessages_NonMapBody(t *testing.T) {
	t.Parallel()
	if got := Messages("not a map"); got != nil {
		t.Errorf("Messages(non-map) = %v, want nil", got)
	}
	if got := ToolNames(42); got != nil {
		t.Errorf("ToolNames(non-map) = %v, want nil", got)
	}
}
