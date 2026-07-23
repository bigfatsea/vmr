// Ver 2026-07-23 10:00, by Sonnet 5

package adapter

import (
	"encoding/json"
	"testing"
)

func TestSessionFingerprint_Anthropic(t *testing.T) {
	a := json.RawMessage(`{"model":"x","system":"you are Agent A","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]}`)
	b := json.RawMessage(`{"model":"x","system":"you are Agent A","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"different tail"}]}`)
	c := json.RawMessage(`{"model":"x","system":"you are Agent B","messages":[{"role":"user","content":"hi"}]}`)
	d := json.RawMessage(`{"model":"x","system":"you are Agent A","messages":[{"role":"user","content":"bye"}]}`)

	sysA, firstA, ok := SessionFingerprint(a, "anthropic")
	if !ok {
		t.Fatalf("expected ok=true for a")
	}
	sysB, firstB, ok := SessionFingerprint(b, "anthropic")
	if !ok {
		t.Fatalf("expected ok=true for b")
	}
	if sysA != sysB || firstA != firstB {
		t.Errorf("same system+first message across turns must fingerprint identically; a=(%x,%x) b=(%x,%x)", sysA, firstA, sysB, firstB)
	}

	sysC, firstC, ok := SessionFingerprint(c, "anthropic")
	if !ok {
		t.Fatalf("expected ok=true for c")
	}
	if sysC == sysA {
		t.Errorf("different system prompts must produce different sysHash (this is the whole point of §2.1's fix)")
	}
	if firstC != firstA {
		t.Errorf("first message is identical between a and c, firstMsgHash should match: got firstA=%x firstC=%x", firstA, firstC)
	}

	sysD, firstD, ok := SessionFingerprint(d, "anthropic")
	if !ok {
		t.Fatalf("expected ok=true for d")
	}
	if sysD != sysA {
		t.Errorf("system prompt identical between a and d, sysHash should match")
	}
	if firstD == firstA {
		t.Errorf("different first messages must produce different firstMsgHash")
	}
}

func TestSessionFingerprint_OpenAI_LeadingSystem(t *testing.T) {
	single := json.RawMessage(`{"model":"x","messages":[{"role":"system","content":"sys"},{"role":"user","content":"hi"}]}`)
	multi := json.RawMessage(`{"model":"x","messages":[{"role":"system","content":"sys"},{"role":"system","content":"more sys"},{"role":"user","content":"hi"}]}`)
	noSys := json.RawMessage(`{"model":"x","messages":[{"role":"user","content":"hi"}]}`)

	sysS, firstS, ok := SessionFingerprint(single, "openai")
	if !ok {
		t.Fatalf("expected ok=true")
	}
	sysM, firstM, ok := SessionFingerprint(multi, "openai")
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if sysS == sysM {
		t.Errorf("extra leading system message should change sysHash")
	}
	if firstS != firstM {
		t.Errorf("first non-system message identical, firstMsgHash should match: got %x vs %x", firstS, firstM)
	}

	sysN, _, ok := SessionFingerprint(noSys, "openai")
	if !ok {
		t.Fatalf("expected ok=true")
	}
	var zero [16]byte
	if sysN != zero {
		t.Errorf("no leading system message: sysHash should be the zero value, got %x", sysN)
	}
}

func TestSessionFingerprint_NoMessages(t *testing.T) {
	if _, _, ok := SessionFingerprint(json.RawMessage(`{"model":"x"}`), "anthropic"); ok {
		t.Errorf("expected ok=false when there is no top-level messages array")
	}
	if _, _, ok := SessionFingerprint(json.RawMessage(`{"model":"x","messages":[]}`), "openai"); ok {
		t.Errorf("expected ok=false for an empty messages array")
	}
}

func TestHasNonEmptyTopLevelArray(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"present and non-empty", `{"tools":[{"name":"x"}]}`, true},
		{"present but empty", `{"tools":[]}`, false},
		{"absent", `{"model":"x"}`, false},
		{"whitespace inside empty array", `{"tools":[  ]}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := HasNonEmptyTopLevelArray(json.RawMessage(c.body), "tools")
			if got != c.want {
				t.Errorf("HasNonEmptyTopLevelArray(%q) = %v, want %v", c.body, got, c.want)
			}
		})
	}
}
