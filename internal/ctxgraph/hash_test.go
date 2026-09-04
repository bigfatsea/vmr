package ctxgraph

import (
	"testing"
)

// TestHashMsgJSON_StripsCacheControl pins the exported single-message entry
// point: a cache_control marker (at any depth) must not change the digest,
// so a breakpoint moved between turns is not a content edit.
func TestHashMsgJSON_StripsCacheControl(t *testing.T) {
	t.Parallel()
	plain := map[string]any{"role": "user", "content": "hi"}
	markedTop := map[string]any{"role": "user", "content": "hi", "cache_control": map[string]any{"type": "ephemeral"}}
	markedDeep := map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "text", "text": "hi", "cache_control": map[string]any{"type": "ephemeral"}},
	}}
	deepPlain := map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "text", "text": "hi"},
	}}
	want := HashMsgJSON(plain)
	if got := HashMsgJSON(markedTop); got != want {
		t.Errorf("top-level marker hash = %v, want %v", got, want)
	}
	if got := HashMsgJSON(markedDeep); got != HashMsgJSON(deepPlain) {
		t.Errorf("deep marker hash = %v, want %v", got, HashMsgJSON(deepPlain))
	}
}

// TestHashMsgJSON_DistinguishesContent guards the stripping against becoming
// a total wash: different real content must still hash differently.
func TestHashMsgJSON_DistinguishesContent(t *testing.T) {
	t.Parallel()
	a := HashMsgJSON(map[string]any{"role": "system", "content": "You are terse."})
	b := HashMsgJSON(map[string]any{"role": "system", "content": "You are verbose."})
	if a == b {
		t.Error("distinct system prompts must not share a hash")
	}
}
