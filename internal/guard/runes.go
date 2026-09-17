// Ver 2026-09-14, by Sonnet 5

// Unicode steganography classification (M1.4; design spec §4.4.2). Pure
// classification only — counting which code points appear, never deleting
// or rewriting them. The M4 online sanitizer (not part of this change) is
// the thing that turns a non-zero A/B-tier count into a deletion decision;
// this package only ever produces the count, matching K-G14 (an offline
// report describes what already reached the client, not what was blocked).
package guard

import "unicode/utf8"

// Rune category keys, matching audit.GuardRecord.SanitizedRunes' documented
// vocabulary ("tags"|"bidi"|"zwsp"|...) so a fallback-scanned RuneCounts and
// an eventual M4-stamped SanitizedRunes are the same shape read the same way
// downstream.
const (
	RuneCatTags       = "tags"        // A: Tags block U+E0000-E007F
	RuneCatControl    = "control"     // A: C0 controls (except \t\n\r), DEL, U+FFF9-FFFB
	RuneCatZWSP       = "zwsp"        // B: U+200B zero-width space
	RuneCatSoftHyphen = "soft_hyphen" // B: U+00AD
	RuneCatBOM        = "bom"         // B: U+FEFF (non-leading — every occurrence counts here; leading-BOM stripping is a transport concern, not this package's)
	RuneCatBidi       = "bidi"        // B: U+202A-202E embedding/override, U+2066-2069 isolate — Trojan Source's actual carrier
	RuneCatLineSep    = "line_sep"    // B: U+2028/U+2029
	RuneCatVarSel     = "varsel"      // C: ZWNJ/ZWJ, LRM/RLM, U+FE00-FE0F, U+E0100-E01EF — required by real scripts/emoji, never deleted (K-G7)
)

// ClassifyRunes counts s's occurrences of every category above, added into
// dst (dst may be nil; a nil map is allocated lazily on the first actual
// hit, so the all-ASCII no-hit path — the overwhelming majority of real
// traffic per §2.3's corpus baseline — allocates nothing). s must already
// be decoded text (UTF-8, no outstanding JSON \uXXXX escapes) — the
// decoding is the caller's job via chatmsg.ReassembleSSE/FinalMessage
// (ADR-14 §4: SSE/JSON message assembly does not belong in guard). Scanning
// raw undecoded JSON bytes directly would miss a code point written as a
// \uXXXX escape entirely — exactly the RT-03 evasion the design spec calls
// out — which is why this package never takes raw wire bytes as its input.
func ClassifyRunes(s []byte, dst map[string]int) map[string]int {
	for len(s) > 0 {
		r, size := utf8.DecodeRune(s)
		s = s[size:]
		cat := ClassifyRune(r)
		if cat == "" {
			continue
		}
		if dst == nil {
			dst = make(map[string]int, 4)
		}
		dst[cat]++
	}
	return dst
}

// ClassifyRune returns r's category per §4.4.2's three tiers, or "" when r
// carries no steganographic significance (the overwhelming majority of any
// real rune stream — plain ASCII and ordinary script text both fall through
// every case below). Exported (not just an internal ClassifyRunes helper)
// so a caller that wants per-codepoint detail beyond ClassifyRunes'
// per-category counts — e.g. tools/guard_corpus_scan's calibration report,
// which breaks each tier down by exact code point — can share this same
// classification decision instead of re-deriving the code-point ranges,
// which is exactly the kind of drift CLAUDE.md's "one implementation"
// rule exists to prevent.
func ClassifyRune(r rune) string {
	switch {
	case r >= 0xE0000 && r <= 0xE007F:
		return RuneCatTags
	case (r >= 0x00 && r <= 0x08) || r == 0x0B || r == 0x0C || (r >= 0x0E && r <= 0x1F) || r == 0x7F || (r >= 0xFFF9 && r <= 0xFFFB):
		return RuneCatControl
	case r == 0x200B:
		return RuneCatZWSP
	case r == 0x00AD:
		return RuneCatSoftHyphen
	case r == 0xFEFF:
		return RuneCatBOM
	case (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069):
		return RuneCatBidi
	case r == 0x2028 || r == 0x2029:
		return RuneCatLineSep
	case r == 0x200C || r == 0x200D || r == 0x200E || r == 0x200F || (r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0100 && r <= 0xE01EF):
		return RuneCatVarSel
	default:
		return ""
	}
}
