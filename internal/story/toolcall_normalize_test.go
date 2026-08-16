// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

package story

import (
	"testing"
	"vmr/internal/chatmsg"
)

func TestCanonicalizeToolArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		in1       string
		in2       string
		wantEqual bool
	}{
		{
			name:      "json keys in different order",
			in1:       `{"path":"a.go","line":10}`,
			in2:       `{"line":10,"path":"a.go"}`,
			wantEqual: true,
		},
		{
			name:      "json with indentation and whitespace",
			in1:       "{\n  \"line\": 10,\n  \"path\": \"a.go\"\n}",
			in2:       `{"path":"a.go","line":10}`,
			wantEqual: true,
		},
		{
			name:      "nested json object key sorting",
			in1:       `{"meta":{"b":2,"a":1},"tool":"bash"}`,
			in2:       `{"tool":"bash","meta":{"a":1,"b":2}}`,
			wantEqual: true,
		},
		{
			name:      "plain text command whitespace normalization",
			in1:       "bash   -c   \"go test  ./...\"",
			in2:       "bash -c \"go test  ./...\"",
			wantEqual: true,
		},
		{
			name:      "different arguments are not equal",
			in1:       `{"path":"a.go"}`,
			in2:       `{"path":"b.go"}`,
			wantEqual: false,
		},
		{
			name: "large integers past float64's exact range stay distinct",
			// Nanosecond timestamps and large IDs exceed 2^53; decoding
			// through float64 (instead of json.Number) rounds both of these
			// to the same value and would falsely canonicalize them equal.
			in1:       `{"since_ns":1700000000123456789}`,
			in2:       `{"since_ns":1700000000123456700}`,
			wantEqual: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res1 := canonicalizeToolArgs(tc.in1)
			res2 := canonicalizeToolArgs(tc.in2)
			if (res1 == res2) != tc.wantEqual {
				t.Errorf("canonicalizeToolArgs(%q) = %q, canonicalizeToolArgs(%q) = %q; equal = %v, want %v",
					tc.in1, res1, tc.in2, res2, res1 == res2, tc.wantEqual)
			}
		})
	}
}

func TestToolCallKey_Canonicalization(t *testing.T) {
	t.Parallel()

	tc1 := chatmsg.ToolCall{Name: "read_file", Args: `{"path": "foo.go", "start": 1}`}
	tc2 := chatmsg.ToolCall{Name: "read_file", Args: `{"start": 1, "path": "foo.go"}`}

	k1 := toolCallKey(tc1)
	k2 := toolCallKey(tc2)

	if k1 != k2 {
		t.Errorf("toolCallKey should produce identical keys for permutation-equivalent JSON args, got %q vs %q", k1, k2)
	}
}
