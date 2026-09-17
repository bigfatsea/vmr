// Ver 2026-09-16, by Sonnet 5

package guard

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func mustSanitizeJSON(t *testing.T, obj any) []byte {
	t.Helper()
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

// TestSanitize_ATierAlwaysStripped covers the Tags block (U+E0000, a
// surrogate-pair escape on the wire since it's above the BMP) and a C0
// control character (U+0001) -- both must be removed, in both wire forms
// (literal UTF-8 and \u escape).
func TestSanitize_ATierAlwaysStripped(t *testing.T) {
	tagsLiteral := mustSanitizeJSON(t, map[string]string{"content": "hello" + string(rune(0xE0000)) + "world"})
	tagsEscaped := []byte(`{"content":"hello\uDB40\uDC00world"}`)
	controlEscaped := []byte(`{"content":"hello\u0001world"}`)

	for _, raw := range [][]byte{tagsLiteral, tagsEscaped, controlEscaped} {
		out, changed, dst := SanitizeEventJSON(raw, nil)
		if !changed {
			t.Errorf("raw=%s: changed=false, want A-tier stripped", raw)
		}
		if strings.Contains(string(out), "world") && strings.Contains(string(out), "hello") && !bytes.Equal(out, []byte(`{"content":"helloworld"}`)) {
			t.Errorf("raw=%s: out=%s, want exactly {\"content\":\"helloworld\"}", raw, out)
		}
		if dst["tags"] == 0 && dst["control"] == 0 {
			t.Errorf("raw=%s: dst=%v, want a tags or control count", raw, dst)
		}
	}
}

// TestSanitize_BTierAlwaysStripped covers a bidi override character
// (U+202E, Trojan Source's actual carrier) in both wire forms. B-tier is
// stripped unconditionally: the former strip/flag_only rung was removed
// after corpus calibration showed B-tier's residual harm is visual only.
func TestSanitize_BTierAlwaysStripped(t *testing.T) {
	literal := mustSanitizeJSON(t, map[string]string{"content": "safe" + string(rune(0x202E)) + "text"})
	escaped := []byte(`{"content":"safe\u202etext"}`)

	for _, raw := range [][]byte{literal, escaped} {
		out, changed, dst := SanitizeEventJSON(raw, nil)
		if !changed || bytes.Contains(out, []byte("safetext")) == false {
			t.Errorf("raw=%s out=%s changed=%v, want the bidi override removed", raw, out, changed)
		}
		if dst["bidi"] != 1 {
			t.Errorf("dst=%v, want bidi count 1", dst)
		}
	}
}

// TestSanitize_CTierNeverStripped is K-G7: variant-selector-class
// characters are never deletion candidates, and (unlike A/B-tier) are never
// counted into dst either -- dst is SanitizedRunes' "what this call
// actually removed" tally, and a never-stripped tier has nothing to add
// to it (the separate offline ClassifyRunes is what counts every
// classified occurrence, stripped or not).
func TestSanitize_CTierNeverStripped(t *testing.T) {
	raw := mustSanitizeJSON(t, map[string]string{"content": "a" + string(rune(0x200D)) + "b" + string(rune(0xFE0F))})
	out, changed, dst := SanitizeEventJSON(raw, nil)
	if changed {
		t.Errorf("changed=true, want C-tier never stripped")
	}
	if !bytes.Equal(out, raw) {
		t.Errorf("out=%s, want byte-identical to input", out)
	}
	if dst["varsel"] != 0 {
		t.Errorf("dst=%v, want varsel not counted -- SanitizedRunes must never claim credit for a character it never touched", dst)
	}
}

// TestSanitize_DELAndLiteralC0 covers literal 0x7F (DEL) and unescaped C0
// control bytes (< 0x20) in JSON string content — both are Tier A (RuneCatControl)
// and must be stripped online and recorded in dst.
func TestSanitize_DELAndLiteralC0(t *testing.T) {
	delLiteral := []byte(`{"content":"hello` + string([]byte{0x7F}) + `world"}`)
	c0Literal := []byte(`{"content":"hello` + string([]byte{0x01}) + `world"}`)

	for _, raw := range [][]byte{delLiteral, c0Literal} {
		out, changed, dst := SanitizeEventJSON(raw, nil)
		if !changed {
			t.Errorf("raw=%q: changed=false, want Tier A control/DEL stripped", raw)
		}
		if !bytes.Equal(out, []byte(`{"content":"helloworld"}`)) {
			t.Errorf("raw=%q: out=%s, want {\"content\":\"helloworld\"}", raw, out)
		}
		if dst["control"] != 1 {
			t.Errorf("raw=%q: dst=%v, want control count 1", raw, dst)
		}
	}
}

// TestSanitize_MultilingualNonCorruption pins the design spec's own
// acceptance bar: Persian ZWNJ, Hindi ZWJ conjuncts, Arabic RLM, and a ZWJ
// emoji sequence must all survive byte-for-byte -- every character
// involved is C-tier (K-G7), so blind deletion would corrupt real content.
func TestSanitize_MultilingualNonCorruption(t *testing.T) {
	cases := map[string]string{
		"persian_zwnj":  "می" + string(rune(0x200C)) + "خواهم",                         // ZWNJ joins "mi" + ZWNJ + "khāham"
		"hindi_zwj":     "क्" + string(rune(0x200D)) + "ष",                             // ZWJ-joined conjunct
		"arabic_rlm":    "مرحبا" + string(rune(0x200F)) + "!",                          // trailing RLM
		"zwj_emoji_seq": "👨" + string(rune(0x200D)) + "👩" + string(rune(0x200D)) + "👧", // family emoji, two ZWJ joins
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			raw := mustSanitizeJSON(t, map[string]string{"content": text})
			out, changed, _ := SanitizeEventJSON(raw, nil)
			if changed {
				t.Errorf("changed=true, want no corruption of %s", name)
			}
			if !bytes.Equal(out, raw) {
				t.Errorf("out=%s, want byte-identical to input %s", out, raw)
			}
		})
	}
}

// TestSanitize_ASCIIFastPathNoAlloc confirms the "no \u, all ASCII" fast
// path returns the identical input with zero allocation, per §4.8's
// stated performance requirement for the common case.
func TestSanitize_ASCIIFastPathNoAlloc(t *testing.T) {
	raw := []byte(`{"choices":[{"delta":{"content":"the quick brown fox jumps over the lazy dog"}}]}`)
	allocs := testing.AllocsPerRun(100, func() {
		out, changed, _ := SanitizeEventJSON(raw, nil)
		if changed {
			t.Fatal("changed=true on pure ASCII input")
		}
		if len(out) != len(raw) {
			t.Fatal("output length mismatch")
		}
	})
	if allocs != 0 {
		t.Errorf("AllocsPerRun = %v, want 0 for the pure-ASCII fast path", allocs)
	}
}

// TestSanitize_OrdinaryEscapesUntouched confirms \n/\"/\\ and similar
// structural escapes (never code-point escapes) are left alone and don't
// trip the fast path into a needless full walk producing a false change.
// TestSanitize_PrefixEditAppliedDespiteMalformedTail pins SanitizeEventJSON's
// deliberate half-edit behavior (independent review finding, doc comment
// updated to match): sanitizeWalkValue's return value is intentionally
// unchecked, so an edit already found in a well-formed prefix survives even
// when a later, unrelated part of the same event is malformed and stops the
// walk -- a genuinely truncated event benefits from stripping what was
// already safely found, rather than discarding it over an unrelated later
// failure. Only the malformed tail itself is left untouched.
func TestSanitize_PrefixEditAppliedDespiteMalformedTail(t *testing.T) {
	raw := []byte(`{"a":"x` + "​" + `y","b":nonsense_garbage}`)
	out, changed, _ := SanitizeEventJSON(raw, nil)
	if !changed {
		t.Fatal("changed=false, want true -- the well-formed prefix's rune should still be stripped")
	}
	if bytes.Contains(out, []byte("​")) {
		t.Errorf("out=%q, want the rune stripped from the well-formed prefix", out)
	}
	if !bytes.Contains(out, []byte("nonsense_garbage")) {
		t.Errorf("out=%q, want the malformed tail left byte-for-byte untouched", out)
	}
}

func TestSanitize_OrdinaryEscapesUntouched(t *testing.T) {
	raw := []byte(`{"content":"line one\nline two \"quoted\" and a \\ backslash"}`)
	out, changed, _ := SanitizeEventJSON(raw, nil)
	if changed {
		t.Errorf("changed=true, want no change for ordinary structural escapes, got %s", out)
	}
	if !bytes.Equal(out, raw) {
		t.Errorf("out=%s, want byte-identical to input", out)
	}
}

// TestInbound_SanitizeStripsInvisibleRunes confirms sanitize-on re-frames
// and strips A/B-tier content end to end through the public Inbound entry
// point (ADR-15: this is now the only online inbound behavior there is).
func TestInbound_SanitizeStripsInvisibleRunes(t *testing.T) {
	g := NewGuard(testEngine(t))
	raw := mustSanitizeJSON(t, map[string]any{
		"choices": []map[string]any{{"delta": map[string]string{"content": "safe" + string(rune(0x202E)) + "text"}}},
	})
	src := bytes.NewReader(append([]byte("data: "), append(raw, []byte("\n\n")...)...))
	s := g.Inbound(src, InboundOpts{SanitizeInvisibleRunes: true})
	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if strings.Contains(string(got), "‮") {
		t.Errorf("got %q, want the bidi override stripped", got)
	}
	if !strings.Contains(string(got), "safetext") {
		t.Errorf("got %q, want the surrounding text preserved", got)
	}
	found := false
	for _, m := range s.Applied() {
		if m == "guard_runes_sanitized" {
			found = true
		}
	}
	if !found {
		t.Errorf("Applied() = %v, want guard_runes_sanitized", s.Applied())
	}
}
