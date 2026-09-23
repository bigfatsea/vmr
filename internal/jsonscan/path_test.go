// Ver 2026-09-23 08:10, by Claude Opus 5.5

// Coverage for LocateStringValue: unit tests pin the known-path resolution
// rules (nested descent, string-only final values, last-duplicate-key
// semantics, decline-on-malformed), and the fuzz target asserts the scanner
// contract directly — every returned range is in-bounds and points at a JSON
// string, and whenever raw as a whole is valid JSON the located value
// matches what encoding/json's independent reference walk reports for the
// same path (the same "reference decode via the standard library" rule the
// rewrite fuzz tests use, so a bug shared between the scanner and this
// check can't hide a real divergence).
package jsonscan

import (
	"encoding/json"
	"testing"
)

func TestLocateStringValue(t *testing.T) {
	locate := func(raw string, path ...string) string {
		s, e, ok := LocateStringValue([]byte(raw), path...)
		if !ok {
			return "<declined>"
		}
		var got string
		if err := json.Unmarshal([]byte(raw)[s:e], &got); err != nil {
			t.Fatalf("located range [%d,%d) is not a JSON string: raw=%s", s, e, raw)
		}
		return got
	}

	cases := []struct {
		name string
		raw  string
		path []string
		want string
	}{
		{"top-level key", `{"id":"x","model":"gpt-4"}`, []string{"model"}, "gpt-4"},
		{"nested message.model", `{"type":"message_start","message":{"id":"m","model":"claude-x"}}`, []string{"message", "model"}, "claude-x"},
		{"nested response.model", `{"type":"response.created","response":{"id":"r","model":"gpt-5"}}`, []string{"response", "model"}, "gpt-5"},
		{"whitespace around keys and values", `  {  "message"  :  {  "model"  :  "spaced"  }  }  `, []string{"message", "model"}, "spaced"},
		{"empty object values", `{}`, []string{"model"}, "<declined>"},
		{"key absent", `{"id":"x"}`, []string{"model"}, "<declined>"},
		{"nested key absent", `{"message":{"id":"m"}}`, []string{"message", "model"}, "<declined>"},
		{"intermediate value not an object", `{"message":"not an object","model":"top"}`, []string{"message", "model"}, "<declined>"},
		{"intermediate value is an array", `{"message":[{"model":"x"}]}`, []string{"message", "model"}, "<declined>"},
		{"final value not a string", `{"message":{"model":{"nested":true}}}`, []string{"message", "model"}, "<declined>"},
		{"final value null", `{"model":null}`, []string{"model"}, "<declined>"},
		{"final value number", `{"model":123}`, []string{"model"}, "<declined>"},
		{"truncated object", `{"model":"trunc`, []string{"model"}, "<declined>"},
		{"truncated nested object", `{"response":{"model":"a","tail":`, []string{"response", "model"}, "<declined>"},
		{"truncated right after the value", `{"id":"x","model":"up"`, []string{"model"}, "<declined>"},
		{"truncated after a trailing comma", `{"id":"x","model":"up",`, []string{"model"}, "<declined>"},
		{"inner object closed, outer truncated", `{"message":{"model":"up"},`, []string{"message", "model"}, "<declined>"},
		{"not json at all", `not json`, []string{"model"}, "<declined>"},
		{"top-level array", `[{"model":"x"}]`, []string{"model"}, "<declined>"},
		{"empty path", `{"model":"x"}`, nil, "<declined>"},
		{"duplicate key: last occurrence wins", `{"model":"first","model":"last"}`, []string{"model"}, "last"},
		{"duplicate nested key: last occurrence wins", `{"response":{"model":"d1","model":"d2"}}`, []string{"response", "model"}, "d2"},
		{"escaped quotes in a sibling string do not derail the scan", `{"a":"x\"y\",\"model\":\"fake\"}","model":"real"}`, []string{"model"}, "real"},
		{"escaped quote inside the located value", `{"message":{"model":"a\"b"}}`, []string{"message", "model"}, "a\"b"},
		{"model mentioned inside a nested string is not the path", `{"delta":{"text":"see \"model\":\"fake\" docs"}}`, []string{"delta", "text"}, `see "model":"fake" docs`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := locate(c.raw, c.path...); got != c.want {
				t.Errorf("LocateStringValue(%q, %v) = %q, want %q", c.raw, c.path, got, c.want)
			}
		})
	}
}

func FuzzLocateStringValue(f *testing.F) {
	seeds := []string{
		`{"model":"m"}`,
		`{"message":{"model":"claude-x"}}`,
		`{"response":{"id":"r","model":"gpt"}}`,
		`{"message":{"model":{"nested":true}}}`,
		`{"message":"not an object","model":"top"}`,
		`{"a":"x\"y","model":"m"}`,
		`{"model":null}`,
		`{"model":123}`,
		`not json`,
		`{"model":"trunc`,
		`{"response":{"model":"a","tail":`,
		`{"message":{"other":"m"}}`,
		`{"response":{"model":"dup1","model":"dup2"}}`,
		`  {  "message" :  { "model" : "spaced" }  }  `,
		`{"model":"trail\ud83d"}`,
		`{"message":{"model":"a\\\"b"}}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s), uint8(0))
	}

	// The fuzzed path selection is bounded to these shapes — the fuzzed
	// input worth generating is the byte soup, not arbitrary key spellings
	// that would never resolve.
	paths := [][]string{
		{"model"},
		{"message", "model"},
		{"response", "model"},
		{"message", "model", "deeper"},
		{"a"},
	}

	f.Fuzz(func(t *testing.T, raw []byte, sel uint8) {
		path := paths[int(sel)%len(paths)]
		start, end, ok := LocateStringValue(raw, path...)
		if !ok {
			return // declining malformed/non-object input is an accepted outcome
		}
		if start < 0 || end > len(raw) || start >= end {
			t.Fatalf("range [%d,%d) out of bounds for input length %d: raw=%s", start, end, len(raw), raw)
		}
		if raw[start] != '"' || raw[end-1] != '"' {
			t.Fatalf("located range is not a JSON string: %q (raw=%s)", raw[start:end], raw)
		}
		if !json.Valid(raw) {
			return // SkipJSONValue is a delimiter scanner, not a validator
		}
		// Reference walk via the standard library — independent of this
		// package's own byte scanner. encoding/json keeps the last
		// duplicate key, the same semantics LocateStringValue documents.
		var v any
		if json.Unmarshal(raw, &v) != nil {
			return
		}
		cur := v
		for i, k := range path {
			m, isObj := cur.(map[string]any)
			if !isObj {
				t.Fatalf("path %v resolved though the reference walk declines at %q: raw=%s", path, k, raw)
			}
			cur, isObj = m[k]
			if !isObj {
				t.Fatalf("path %v resolved though the reference walk finds no %q: raw=%s", path, k, raw)
			}
			if i == len(path)-1 {
				want, isStr := cur.(string)
				if !isStr {
					t.Fatalf("located a string though the reference value at %v is not one: raw=%s", path, raw)
				}
				var got string
				if err := json.Unmarshal(raw[start:end], &got); err != nil || got != want {
					t.Fatalf("located %q, reference walk says %q (err=%v): raw=%s", got, want, err, raw)
				}
			} else if _, isObj := cur.(map[string]any); !isObj {
				t.Fatalf("intermediate %q is not an object per the reference walk: raw=%s", k, raw)
			}
		}
	})
}
