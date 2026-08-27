// Ver 2026-08-04, by Opus 5

// Fuzz coverage for SessionFingerprint's three protocol paths. Added
// alongside FuzzRewriteRoles after that fuzzer found a real infinite loop in
// the shared skipJSONValue primitive (a stray/unmatched delimiter byte made
// it return success at the same offset it was called with, so every caller
// that loops as `i, ok = skipJSONValue(...); continue` on ok==true spun
// forever). SessionFingerprint's walkArrayElements loop (fingerprint.go) has
// the identical shape and is reachable on every request for Sticky Model
// routing, not gated behind any opt-in config like role_map — so it carries
// the same hazard class and deserves the same regression lock now that the
// root cause is fixed in skipJSONValue itself. The invariant fuzzed here is
// deliberately narrow (termination, no panic, determinism): like
// topLevelValues elsewhere in this package, the scan is a structural
// scanner, not a strict validator, so it's expected (not a bug) that some
// malformed-but-bracket-balanced input still produces ok=true — the one
// property worth asserting is that it always produces the *same* verdict
// and hashes for the same bytes, never a hang or a panic.
package adapter

import (
	"testing"
)

func FuzzSessionFingerprint(f *testing.F) {
	seeds := []struct {
		raw      string
		protocol string
	}{
		{`{"messages":[{"role":"system","content":"sp"},{"role":"user","content":"hi"}]}`, "openai-completions"},
		{`{"messages":[{"role":"user","content":"hi"}]}`, "openai-completions"},
		{`{"messages":[]}`, "openai-completions"},
		{`{"system":"sp","messages":[{"role":"user","content":"hi"}]}`, "anthropic-messages"},
		{`{"messages":[{"role":"user","content":"hi"}]}`, "anthropic-messages"},
		{`{"instructions":"sp","input":[{"role":"developer","content":"d"},{"role":"user","content":"hi"}]}`, "openai-responses"},
		{`{"input":"hello"}`, "openai-responses"},
		{`{"input":[{"type":"function_call","name":"f","arguments":"{}"}]}`, "openai-responses"},
		{`not json at all`, "openai-completions"},
		{`null`, "anthropic-messages"},
		{`[]`, "openai-responses"},
		{``, "openai-completions"},
		{`{"messages":[{"role":"system" `, "openai-completions"},           // truncated element
		{`{"messages":[{"role""x","content":"y"}]}`, "openai-completions"}, // missing colon after key (the shape that hung skipJSONValue)
		{`{"messages":[1,2,{"role":"user"}]}`, "openai-completions"},       // non-object elements before the real one
		{`{"input":[{"role""x"}]}`, "openai-responses"},
		{`{"messages":[{"role":"system","content":"a"},{"role":"system","content":"b"},{"role":"user","content":"c"}]}`, "openai-completions"},
	}
	for _, s := range seeds {
		f.Add([]byte(s.raw), s.protocol)
	}

	f.Fuzz(func(t *testing.T, raw []byte, protocol string) {
		sysA, firstA, okA := SessionFingerprint(raw, protocol)
		sysB, firstB, okB := SessionFingerprint(raw, protocol)
		if okA != okB || sysA != sysB || firstA != firstB {
			t.Fatalf("SessionFingerprint not deterministic for protocol=%q raw=%s: run1=(%x,%x,%v) run2=(%x,%x,%v)",
				protocol, raw, sysA, firstA, okA, sysB, firstB, okB)
		}
	})
}
