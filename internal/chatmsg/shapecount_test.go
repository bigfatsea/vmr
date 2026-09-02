// Ver 2026-09, by pi-agent

// S-2 shape-recognition counters: chatmsg counts unrecognized content-part
// types and unrecognized usage-holder shapes so `vmr analyze` can surface
// the unknown-shape lineage to an operator instead of silently dropping it.
package chatmsg

import (
	"testing"
)

func TestUnrecognizedShapeCounts_RecognizedShapesStayZero(t *testing.T) {
	ResetUnrecognizedShapeCounts()

	// A recognized content part and a recognized usage object must not bump
	// either counter.
	RenderPart(map[string]any{"type": "text", "text": "hello"})
	RenderPart(map[string]any{"type": "tool_use", "name": "web_search", "input": map[string]any{}})
	if _, u := ExtractUsageWithProtocol(map[string]any{
		"usage": map[string]any{"prompt_tokens": float64(10), "completion_tokens": float64(5)},
	}, "openai-completions"); !u {
		t.Fatal("recognized usage object should parse")
	}
	parts, holders := UnrecognizedShapeCounts()
	if parts != 0 || holders != 0 {
		t.Fatalf("recognized shapes bumped counters: parts=%d holders=%d, want 0/0", parts, holders)
	}
}

func TestUnrecognizedShapeCounts_UnknownPartTypeCounted(t *testing.T) {
	ResetUnrecognizedShapeCounts()

	// An unknown content-part type (e.g. a vendor's future "audio" part)
	// falls into RenderPart's default branch — count it, don't just render
	// the raw JSON silently.
	got := RenderPart(map[string]any{"type": "input_audio", "audio": "..."})
	if got == "" {
		t.Fatal("unrecognized part should still render raw JSON, not empty")
	}
	parts, holders := UnrecognizedShapeCounts()
	if parts != 1 || holders != 0 {
		t.Fatalf("counters after unknown part = parts=%d holders=%d, want 1/0", parts, holders)
	}

	// Second occurrence increments again.
	RenderPart(map[string]any{"type": "video", "video": "..."})
	if parts, _ := UnrecognizedShapeCounts(); parts != 2 {
		t.Fatalf("parts = %d, want 2 after second unknown part", parts)
	}
}

func TestUnrecognizedShapeCounts_UnknownUsageHolderShapeCounted(t *testing.T) {
	ResetUnrecognizedShapeCounts()

	// An unrecognized usage-holder shape: the "usage" key exists but isn't a
	// JSON object (e.g. a gateway emitting "usage": "accounted-elsewhere").
	// mergeUsage's 3-holder list skips it silently today — the counter makes
	// it audible.
	u, ok := ExtractUsageWithProtocol(map[string]any{
		"usage": "not-an-object",
	}, "openai-completions")
	if ok {
		t.Fatalf("usage from a string holder should not parse, got %+v", u)
	}
	parts, holders := UnrecognizedShapeCounts()
	if parts != 0 || holders != 1 {
		t.Fatalf("counters after unknown holder = parts=%d holders=%d, want 0/1", parts, holders)
	}
}

// TestUnrecognizedShapeCounts_AbsentUsageNotCounted pins the nil-holder
// distinction: obj["usage"] being absent is the normal case on every request
// (e.g. an error response), and must never be counted as an unrecognized
// shape — only a holder that is present but wrong-shaped is.
func TestUnrecognizedShapeCounts_AbsentUsageNotCounted(t *testing.T) {
	ResetUnrecognizedShapeCounts()

	if _, ok := ExtractUsageWithProtocol(map[string]any{"error": map[string]any{"message": "boom"}}, "openai-completions"); ok {
		t.Fatal("error body without usage should not parse as usage")
	}
	if _, ok := ExtractUsageWithProtocol(map[string]any{"choices": []any{}}, "openai-completions"); ok {
		t.Fatal("choices-only body without usage should not parse as usage")
	}
	_, holders := UnrecognizedShapeCounts()
	if holders != 0 {
		t.Fatalf("holders = %d, want 0 (absent usage is normal, not unrecognized)", holders)
	}
}
