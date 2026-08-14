// Ver 2026-08-14, by Sonnet 5

// Direct fuzz coverage for the two lowest-level structural scanners:
// TopLevelValues and WalkArrayElements. Both were already exercised
// indirectly through FuzzRewriteModel/FuzzRewriteRoles/FuzzSessionFingerprint
// (which found and fixed a real infinite loop in SkipJSONValue — see
// FuzzSessionFingerprint's doc comment in internal/adapter), but indirect
// coverage only ever proves "the caller didn't crash" for whatever inputs
// happen to survive the caller's own preconditions. These two fuzz targets
// assert the scanners' own contract directly: every byte range they hand
// back is in-bounds, and (for WalkArrayElements) successive elements never
// overlap or go backwards — unconditionally, since an offset bug here is
// exactly N7's concern (a scanner silently splicing a corrupted request
// body). The stronger "range content is itself valid JSON" check only holds
// when raw as a whole is valid JSON: SkipJSONValue's number/true/false/null
// branch is deliberately a delimiter, not a validator (same "structural
// scanner, not a strict validator" contract documented on TopLevelValues and
// FuzzSessionFingerprint) — for malformed input like `{"model":A}` it
// correctly bounds the bogus token "A" at [9,10) without claiming "A" is
// itself well-formed JSON. FuzzRewriteModel's own invariant already gates
// its stronger checks the same way (skips entirely unless raw parses).
package jsonscan

import (
	"encoding/json"
	"strings"
	"testing"
)

func FuzzTopLevelValues(f *testing.F) {
	seeds := []string{
		`{"model":"m","stream":true}`,
		`{"model":{"nested":"model"},"a":1}`,
		`not json`,
		`[]`,
		``,
		`{"model":`,
		`{"model":"a","model":"b"}`,
		`{"model" : "spaced" , "x":  1  }`,
		`{"model":"a\"b\\c"}`,
		`{"model":"trail\ud83d"}`, // truncated UTF-16 escape sequence
		strings.Repeat(`{"a":`, 200) + `1` + strings.Repeat(`}`, 200), // deep nesting before the target key
		`{"model":"` + strings.Repeat("x", 5000) + `"}`,               // long string value
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		rawValid := json.Valid(raw)
		for _, key := range [][]byte{modelKeyLiteral, streamKeyLiteral, roleKeyLiteral} {
			ranges, ok := TopLevelValues(raw, key)
			if !ok {
				continue // declining malformed/non-object input is an accepted outcome
			}
			for _, r := range ranges {
				if r[0] < 0 || r[1] > len(raw) || r[0] > r[1] {
					t.Fatalf("key=%s: range %v out of bounds for input length %d: raw=%s", key, r, len(raw), raw)
				}
				if rawValid && !json.Valid(raw[r[0]:r[1]]) {
					t.Fatalf("key=%s: range %v is not a valid JSON value though raw is valid JSON: %q (raw=%s)", key, r, raw[r[0]:r[1]], raw)
				}
			}
		}
	})
}

func FuzzWalkArrayElements(f *testing.F) {
	seeds := []string{
		`[1,2,3]`,
		`[{"role":"a"},{"role":"b"}]`,
		`[]`,
		`[  ]`,
		`not an array`,
		`[1,2,`,
		`[[1,2],[3,4]]`,
		`["a\"b",2]`,
		`[{"nested":{"deep":[1,2,[3,[4,5]]]}}]`,
		`[` + strings.Repeat(`[`, 200) + `1` + strings.Repeat(`]`, 200) + `]`, // deep nesting inside one element
		`["trail\ud83d"]`, // truncated UTF-16 escape sequence
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		rawValid := json.Valid(raw)
		prevEnd := -1
		_, _ = WalkArrayElements(raw, 0, len(raw), func(start, end int) bool {
			if start < 0 || end > len(raw) || start > end {
				t.Fatalf("element range [%d,%d) out of bounds for input length %d: raw=%s", start, end, len(raw), raw)
			}
			if start < prevEnd {
				t.Fatalf("element range [%d,%d) overlaps or regresses past previous end %d: raw=%s", start, end, prevEnd, raw)
			}
			if rawValid && !json.Valid(raw[start:end]) {
				t.Fatalf("element range [%d,%d) is not valid JSON though raw is valid JSON: %q (raw=%s)", start, end, raw[start:end], raw)
			}
			prevEnd = end
			return false // visit every element
		})
	})
}
