// Ver 2026-07-23 10:00, by Sonnet 5

package adapter

import (
	"encoding/json"
	"testing"
)

func TestSessionFingerprint_Anthropic(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	if _, _, ok := SessionFingerprint(json.RawMessage(`{"model":"x"}`), "anthropic"); ok {
		t.Errorf("expected ok=false when there is no top-level messages array")
	}
	if _, _, ok := SessionFingerprint(json.RawMessage(`{"model":"x","messages":[]}`), "openai"); ok {
		t.Errorf("expected ok=false for an empty messages array")
	}
}

// TestTopLevelProbe_MatchesStructUnmarshalSemantics locks in that the
// hand-rolled scanner accepts/rejects exactly what
// json.Unmarshal(raw, &struct{Model string; Stream bool}{}) would have —
// see TopLevelProbe's doc comment. This is the regression net for server.go
// replacing that reflective unmarshal with this scanner.
func TestTopLevelProbe_MatchesStructUnmarshalSemantics(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		body       string
		wantModel  string
		wantStream bool
		wantTools  bool
		wantOK     bool
	}{
		{"basic", `{"model":"agent","stream":true,"tools":[{"name":"x"}]}`, "agent", true, true, true},
		{"defaults when absent", `{"model":"agent"}`, "agent", false, false, true},
		{"empty tools array", `{"model":"agent","tools":[]}`, "agent", false, false, true},
		{"whitespace-only tools array", `{"model":"agent","tools":[  ]}`, "agent", false, false, true},
		{"unrecognized keys ignored", `{"foo":"bar","model":"agent","nested":{"tools":[1]}}`, "agent", false, false, true},
		{"model null is a no-op, not an error", `{"model":null}`, "", false, false, true},
		{"stream null is a no-op, not an error", `{"model":"agent","stream":null}`, "agent", false, false, true},
		{"stream false explicit", `{"model":"agent","stream":false}`, "agent", false, false, true},
		{"duplicate keys: last wins", `{"model":"a","model":"b","stream":false,"stream":true}`, "b", true, false, true},
		{"top level array, not object", `[1,2,3]`, "", false, false, false},
		{"malformed json", `{"model":`, "", false, false, false},
		{"model wrong type errors like unmarshal would", `{"model":123}`, "", false, false, false},
		{"stream wrong type errors like unmarshal would", `{"model":"agent","stream":"yes"}`, "", false, false, false},
		{"stream number errors like unmarshal would", `{"model":"agent","stream":1}`, "", false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			model, stream, hasTools, ok := TopLevelProbe(json.RawMessage(c.body))
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if model != c.wantModel || stream != c.wantStream || hasTools != c.wantTools {
				t.Errorf("got (model=%q, stream=%v, hasTools=%v), want (model=%q, stream=%v, hasTools=%v)",
					model, stream, hasTools, c.wantModel, c.wantStream, c.wantTools)
			}
		})
	}
}

// TestTopLevelProbe_AgreesWithStructUnmarshal cross-checks a handful of
// shapes directly against the real json.Unmarshal path it replaced, so this
// doesn't just encode the author's assumptions about encoding/json's
// null/type-mismatch rules.
func TestTopLevelProbe_AgreesWithStructUnmarshal(t *testing.T) {
	t.Parallel()
	bodies := []string{
		`{"model":"agent","stream":true}`,
		`{"model":"agent"}`,
		`{"model":null}`,
		`{"model":"agent","stream":null}`,
		`{"model":123}`,
		`{"model":"agent","stream":"yes"}`,
		`[1,2,3]`,
		`{"model":`,
		`{"model":"agent","stream":false,"extra":{"nested":true}}`,
	}
	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			var probe struct {
				Model  string `json:"model"`
				Stream bool   `json:"stream"`
			}
			wantErr := json.Unmarshal([]byte(body), &probe) != nil

			model, stream, _, ok := TopLevelProbe(json.RawMessage(body))
			if ok == wantErr {
				t.Fatalf("ok=%v but json.Unmarshal error=%v (wantErr=%v) disagree for %q", ok, !wantErr, wantErr, body)
			}
			if ok && (model != probe.Model || stream != probe.Stream) {
				t.Errorf("TopLevelProbe=(%q,%v), json.Unmarshal=(%q,%v) disagree for %q", model, stream, probe.Model, probe.Stream, body)
			}
		})
	}
}
