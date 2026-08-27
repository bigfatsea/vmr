// Ver 2026-07-29 20:00, by Sonnet 5

package chatmsg

import "testing"

func TestCheckToolPairing_OpenAI_AllMatched(t *testing.T) {
	t.Parallel()
	msgs := []any{
		map[string]any{"role": "user", "content": "go"},
		map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "c1", "function": map[string]any{"name": "read", "arguments": "{}"}},
			map[string]any{"id": "c2", "function": map[string]any{"name": "read", "arguments": "{}"}},
		}},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": "r1"},
		map[string]any{"role": "tool", "tool_call_id": "c2", "content": "r2"},
	}
	r := CheckToolPairing(msgs)
	if !r.OK() {
		t.Fatalf("want OK, got %+v", r)
	}
	if r.Calls != 2 || r.Results != 2 {
		t.Errorf("want 2/2, got calls=%d results=%d", r.Calls, r.Results)
	}
}

func TestCheckToolPairing_OpenAI_OrphanCall(t *testing.T) {
	t.Parallel()
	msgs := []any{
		map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "c1", "function": map[string]any{"name": "read", "arguments": "{}"}},
		}},
		// no matching tool result for c1
	}
	r := CheckToolPairing(msgs)
	if r.OK() {
		t.Fatal("want a detected orphan call, got OK")
	}
	if len(r.OrphanCalls) != 1 || r.OrphanCalls[0] != "c1" {
		t.Errorf("want OrphanCalls=[c1], got %v", r.OrphanCalls)
	}
	if len(r.OrphanResults) != 0 {
		t.Errorf("want no orphan results, got %v", r.OrphanResults)
	}
}

func TestCheckToolPairing_OpenAI_OrphanResult(t *testing.T) {
	t.Parallel()
	msgs := []any{
		map[string]any{"role": "tool", "tool_call_id": "c-unknown", "content": "r"},
	}
	r := CheckToolPairing(msgs)
	if r.OK() {
		t.Fatal("want a detected orphan result, got OK")
	}
	if len(r.OrphanResults) != 1 || r.OrphanResults[0] != "c-unknown" {
		t.Errorf("want OrphanResults=[c-unknown], got %v", r.OrphanResults)
	}
}

func TestCheckToolPairing_Anthropic_AllMatched(t *testing.T) {
	t.Parallel()
	msgs := []any{
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "t1", "name": "read", "input": map[string]any{}},
		}},
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "ok"},
		}},
	}
	r := CheckToolPairing(msgs)
	if !r.OK() {
		t.Fatalf("want OK, got %+v", r)
	}
	if r.Calls != 1 || r.Results != 1 {
		t.Errorf("want 1/1, got calls=%d results=%d", r.Calls, r.Results)
	}
}

func TestCheckToolPairing_Anthropic_OrphanCall(t *testing.T) {
	t.Parallel()
	msgs := []any{
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "t1", "name": "read", "input": map[string]any{}},
		}},
	}
	r := CheckToolPairing(msgs)
	if r.OK() {
		t.Fatal("want a detected orphan call, got OK")
	}
	if len(r.OrphanCalls) != 1 || r.OrphanCalls[0] != "t1" {
		t.Errorf("want OrphanCalls=[t1], got %v", r.OrphanCalls)
	}
}

func TestCheckToolPairing_MixedProtocolsAndNonToolMessages(t *testing.T) {
	t.Parallel()
	msgs := []any{
		map[string]any{"role": "system", "content": "sys"},
		map[string]any{"role": "user", "content": "hi"},
		map[string]any{"role": "assistant", "content": "no tools here"},
		map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "c1", "function": map[string]any{"name": "read", "arguments": "{}"}},
		}},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": "r1"},
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "t1", "name": "grep", "input": map[string]any{}},
		}},
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "ok"},
		}},
	}
	r := CheckToolPairing(msgs)
	if !r.OK() {
		t.Fatalf("want OK across mixed openai+anthropic shapes in one list, got %+v", r)
	}
	if r.Calls != 2 || r.Results != 2 {
		t.Errorf("want 2/2 (one openai-completions pair + one anthropic-messages pair), got calls=%d results=%d", r.Calls, r.Results)
	}
}

func TestCheckToolPairing_Empty(t *testing.T) {
	t.Parallel()
	if r := CheckToolPairing(nil); !r.OK() || r.Calls != 0 || r.Results != 0 {
		t.Errorf("empty input should report OK with zero calls/results, got %+v", r)
	}
}

func TestNormalizeToolCallID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"call_00_xHodG", "call00xHodG"},
		{"call_abc_123_xyz", "callabc123xyz"},
		{"tooluse_123", "tooluse123"},
		{"alreadyclean123", "alreadyclean123"},
	}
	for _, tc := range cases {
		if got := NormalizeToolCallID(tc.in); got != tc.want {
			t.Errorf("NormalizeToolCallID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
