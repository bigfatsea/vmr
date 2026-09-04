package reqdetail

import (
	"testing"

	"vmr/internal/chatmsg"
	"vmr/internal/tokenutil"
)

// TestRoleMeasure_ReasoningFields pins roleMeasure's reasoning branch to
// chatmsg.ExtractReasoning: all three reasoning carriers must land in the
// role's share, not just reasoning_content — the detail page renders a
// `reasoning`/`thought` field's text, so a single-field read here would put
// the page's role share in contradiction with its own body.
func TestRoleMeasure_ReasoningFields(t *testing.T) {
	t.Parallel()
	const answer, hidden = "the answer", "step by step, secretly"
	for _, field := range []string{"reasoning_content", "reasoning", "thought"} {
		with := map[string]any{"messages": []any{
			map[string]any{"role": "assistant", "content": answer, field: hidden},
		}}
		without := map[string]any{"messages": []any{
			map[string]any{"role": "assistant", "content": answer},
		}}
		tokDelta := RoleTokens(with)["assistant"] - RoleTokens(without)["assistant"]
		if want := tokenutil.EstimateText(hidden); tokDelta != want {
			t.Errorf("%s: RoleTokens delta = %d, want %d", field, tokDelta, want)
		}
		charDelta := RoleChars(with)["assistant"] - RoleChars(without)["assistant"]
		if want := int64(len([]rune(hidden))); charDelta != want {
			t.Errorf("%s: RoleChars delta = %d, want %d", field, charDelta, want)
		}
	}
}

// TestRoleMeasure_MatchesExtractReasoning is the SSOT pin in the other
// direction: for the same message map, whatever ExtractReasoning can pull
// out must be exactly what the role share counts — byte-identical basis,
// so the detail page's rendered reasoning and its header share can never
// disagree again.
func TestRoleMeasure_MatchesExtractReasoning(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"reasoning_content", "reasoning", "thought"} {
		m := map[string]any{"role": "assistant", "content": "the answer", field: "visible thinking"}
		body := map[string]any{"messages": []any{m}}
		rc := chatmsg.ExtractReasoning(m)
		if rc == "" {
			t.Fatalf("%s: ExtractReasoning returned empty", field)
		}
		want := int64(len([]rune("the answer"))) + int64(len([]rune(rc)))
		if got := RoleChars(body)["assistant"]; got != want {
			t.Errorf("%s: RoleChars = %d, want answer+ExtractReasoning = %d", field, got, want)
		}
	}
}
