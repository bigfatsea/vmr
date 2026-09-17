// Ver 2026-09-14, by Sonnet 5

package guard

import "testing"

// Every case is written as an explicit \u/\U escape, never a pasted
// invisible character: the whole point of these code points is that they
// don't render, so a literal in source would be unverifiable by reading it
// (and survives editors/diff tools inconsistently).
func TestClassifyRunes_Categories(t *testing.T) {
	cases := []struct {
		name string
		s    string
		cat  string
	}{
		{"tags block", "\U000E0041", RuneCatTags}, // TAG LATIN SMALL LETTER A
		{"C0 control", "\x01", RuneCatControl},    // SOH
		{"DEL", "\x7f", RuneCatControl},
		{"zwsp", "\u200b", RuneCatZWSP},
		{"soft hyphen", "\u00ad", RuneCatSoftHyphen},
		{"bom mid-string", "a\ufeffb", RuneCatBOM},
		{"bidi override (RLO)", "\u202e", RuneCatBidi},
		{"bidi isolate (LRI)", "\u2066", RuneCatBidi},
		{"line separator", "\u2028", RuneCatLineSep},
		{"zwj joiner", "\u200d", RuneCatVarSel},
		{"variation selector-16", "\ufe0f", RuneCatVarSel},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClassifyRunes([]byte(c.s), nil)
			if got[c.cat] == 0 {
				t.Errorf("ClassifyRunes(%q) = %v, want a count under %q", c.s, got, c.cat)
			}
		})
	}
}

// TestClassifyRunes_PlainASCIINoHit matches the design spec's own
// real-corpus finding that ordinary text produces zero A/B-tier hits -- the
// common case must stay cheap and must not misclassify plain punctuation.
func TestClassifyRunes_PlainASCIINoHit(t *testing.T) {
	got := ClassifyRunes([]byte("the quick brown fox jumps over the lazy dog 123!@#"), nil)
	if len(got) != 0 {
		t.Errorf("ClassifyRunes(plain ASCII) = %v, want empty", got)
	}
}

// TestClassifyRunes_EmojiSequenceNotDeleted documents K-G7: ZWJ/variation
// selectors are the "varsel" (mark-only) category, never control/zwsp --
// callers must be able to tell "count but don't touch" apart from
// "delete", and this is the type-level guarantee that distinction rests on.
func TestClassifyRunes_EmojiSequenceNotDeleted(t *testing.T) {
	// family emoji: MAN + ZWJ + WOMAN + ZWJ + GIRL
	family := "\U0001F468\u200d\U0001F469\u200d\U0001F467"
	got := ClassifyRunes([]byte(family), nil)
	if got[RuneCatControl] != 0 || got[RuneCatZWSP] != 0 || got[RuneCatBidi] != 0 {
		t.Errorf("ClassifyRunes(ZWJ emoji sequence) = %v, want no A/B-tier hits", got)
	}
	if got[RuneCatVarSel] != 2 {
		t.Errorf("ClassifyRunes(ZWJ emoji sequence)[varsel] = %d, want 2 (two ZWJ joiners)", got[RuneCatVarSel])
	}
}

func TestClassifyRunes_AccumulatesIntoDst(t *testing.T) {
	dst := map[string]int{RuneCatZWSP: 5}
	got := ClassifyRunes([]byte("\u200b"), dst)
	if got[RuneCatZWSP] != 6 {
		t.Errorf("ClassifyRunes with pre-populated dst = %v, want zwsp=6", got)
	}
}
