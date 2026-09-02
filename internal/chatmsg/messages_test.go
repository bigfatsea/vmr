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

func TestMessages_ResponsesInstructionsAndInputArray(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"instructions": "you are helpful",
		"input": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"type": "function_call", "call_id": "c1", "name": "exec", "arguments": `{"cmd":"ls"}`},
			map[string]any{"type": "function_call_output", "call_id": "c1", "output": "file1\nfile2"},
			map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": "thinking it through"}}},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "output_text", "text": "done"},
			}},
		},
	}
	msgs := Messages(body)
	if len(msgs) != 6 {
		t.Fatalf("got %d messages, want 6 (instructions prepended + 5 input items)", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Text != "you are helpful" {
		t.Errorf("prepended instructions: %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Text != "hi" {
		t.Errorf("user message: %+v", msgs[1])
	}
	if msgs[2].Role != "assistant" || !strings.Contains(msgs[2].Text, "tool_call exec") {
		t.Errorf("function_call not rendered as assistant tool_call: %+v", msgs[2])
	}
	if msgs[3].Role != "tool" || !strings.Contains(msgs[3].Text, "file1") {
		t.Errorf("function_call_output not rendered as tool: %+v", msgs[3])
	}
	if msgs[4].Role != "assistant" || !strings.Contains(msgs[4].Text, "thinking it through") {
		t.Errorf("reasoning item not rendered: %+v", msgs[4])
	}
	if msgs[5].Role != "assistant" || msgs[5].Text != "done" {
		t.Errorf("output_text message: %+v", msgs[5])
	}
	if MsgOffset(body) != 1 {
		t.Errorf("MsgOffset with top-level instructions = %d, want 1", MsgOffset(body))
	}
	if got := RawArray(body); len(got) != 5 {
		t.Errorf("RawArray = %d elements, want 5", len(got))
	}
}

func TestMessages_ResponsesBareStringInput(t *testing.T) {
	t.Parallel()
	body := map[string]any{"model": "vm", "input": "hello there"}
	msgs := Messages(body)
	if len(msgs) != 1 || msgs[0].Role != "user" || msgs[0].Text != "hello there" {
		t.Errorf("bare-string input: %+v", msgs)
	}
	if got := RawArray(body); got != nil {
		t.Errorf("RawArray on a bare-string input = %v, want nil", got)
	}
}

func TestMessages_ResponsesReasoningNoSummary(t *testing.T) {
	t.Parallel()
	encrypted := map[string]any{"type": "reasoning", "encrypted_content": "opaque-ciphertext"}
	if got := responsesItemMessage(encrypted); !strings.Contains(got.Text, "encrypted reasoning") {
		t.Errorf("encrypted reasoning with no summary: %+v", got)
	}
	empty := map[string]any{"type": "reasoning"}
	if got := responsesItemMessage(empty); !strings.Contains(got.Text, "no summary") {
		t.Errorf("reasoning with neither summary nor encrypted_content: %+v", got)
	}
}

func TestRenderPart_ResponsesImageAndFile(t *testing.T) {
	t.Parallel()
	img := map[string]any{"type": "input_image", "image_url": "data:image/jpeg;base64,QUFBQQ=="}
	if got := RenderPart(img); !strings.Contains(got, "image/jpeg") {
		t.Errorf("input_image with image_url: %q", got)
	}
	imgFileID := map[string]any{"type": "input_image", "file_id": "file-abc"}
	if got := RenderPart(imgFileID); !strings.Contains(got, "file-abc") {
		t.Errorf("input_image with only file_id: %q", got)
	}
	file := map[string]any{"type": "input_file", "filename": "a.pdf", "file_data": "QUFBQQ=="}
	if got := RenderPart(file); !strings.Contains(got, "a.pdf") {
		t.Errorf("input_file with file_data: %q", got)
	}
	refusal := map[string]any{"type": "refusal", "refusal": "cannot help with that"}
	if got := RenderPart(refusal); !strings.Contains(got, "cannot help with that") {
		t.Errorf("refusal part: %q", got)
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

// TestRenderPart_AnthropicDocument locks in the "document" case: an
// Anthropic PDF/document attachment carries a multi-MB base64 payload in
// source.data — it must render as a compact placeholder, never fall into
// the default branch's full JSON dump.
func TestRenderPart_AnthropicDocument(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("QUFB", 4096) // 16KB of base64 in the payload
	doc := map[string]any{
		"type": "document",
		"source": map[string]any{
			"type":       "base64",
			"media_type": "application/pdf",
			"data":       big,
		},
	}
	got := RenderPart(doc)
	if !strings.Contains(got, "application/pdf") {
		t.Errorf("document media_type missing: %q", got)
	}
	if !strings.Contains(got, "~12.0KB") {
		t.Errorf("decoded size missing: %q", got)
	}
	if strings.Contains(got, "QUFB") {
		t.Errorf("base64 payload leaked into the render: %q", got)
	}
	if strings.Contains(got, "\n{\n") {
		t.Errorf("document fell into the default JSON dump: %q", got)
	}

	// Through the full Messages walk: the document part renders as the
	// compact placeholder, not the raw blob.
	msgs := Messages(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": []any{doc}},
		},
	})
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "[document application/pdf") {
		t.Errorf("Messages with document part: %+v", msgs)
	}
}
