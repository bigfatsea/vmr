// Ver 2026-09-03, by pi-agent
package adapter

import (
	"strings"
	"testing"

	"vmr/internal/core"
)

// TestErrorSnippet_CodeField locks in the VA2 + NF-1 fix: a vendor
// whose message is empty or genericized but whose machine-stable error.code
// carries the classifying keyword must still surface that keyword in the
// snippet used by the hint tables. Before the fix, a body like
// {"error":{"type":"invalid_request_error","code":"context_length_exceeded",
// "message":""}} produced a snippet of "invalid_request_error" only — no
// hint matched, the failover-eligible ErrContextLimit was lost.
func TestErrorSnippet_CodeField(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want []string // each must appear in the lowercase snippet
	}{
		{
			"code inside error object (the motivating case)",
			`{"error":{"type":"invalid_request_error","code":"context_length_exceeded","message":""}}`,
			[]string{"context_length_exceeded", "invalid_request_error"},
		},
		{
			"code inside error object, insufficient_quota",
			`{"error":{"code":"insufficient_quota","message":""}}`,
			[]string{"insufficient_quota"},
		},
		{
			"code at top level (some gateways carry it that way)",
			`{"code":"context_length_exceeded","message":"too long"}`,
			[]string{"context_length_exceeded", "too long"},
		},
		{
			"code at top level, insufficient_quota",
			`{"code":"insufficient_quota"}`,
			[]string{"insufficient_quota"},
		},
		{
			"numeric code in error object does not break sibling extraction",
			// Gemini-style code:400 must NOT throw the whole object back to
			// the raw window and lose message. If we typed the struct field
			// as string, this would fail Unmarshal and the snippet would
			// lose structured extraction entirely.
			`{"error":{"code":400,"message":"Function call is missing a thought_signature in functionCall parts.","status":"INVALID_ARGUMENT"}}`,
			[]string{"function call is missing a thought_signature"},
		},
		{
			"no code, no regression",
			`{"error":{"message":"you are not allowed","type":"permission_error"}}`,
			[]string{"you are not allowed", "permission_error"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snippet := errorSnippet([]byte(tc.body))
			lc := strings.ToLower(snippet)
			for _, want := range tc.want {
				if !strings.Contains(lc, want) {
					t.Errorf("snippet missing %q\nbody:   %s\nsnippet: %s", want, tc.body, snippet)
				}
			}
		})
	}
}

// TestErrorSnippet_NonJSONStaysRaw pins the fail-open contract: a plain-text
// body (not a JSON object at all) must still flow through the bounded raw
// window. The new code-extraction branch lives downstream of the
// bytes.TrimLeft+'{'-prefix test; if that test ever loosens, the contract
// is broken.
func TestErrorSnippet_NonJSONStaysRaw(t *testing.T) {
	t.Parallel()
	body := []byte("plain text gateway error: insufficient balance, please top up")
	snippet := errorSnippet(body)
	if !strings.Contains(snippet, "insufficient balance") {
		t.Errorf("plain-text body should expose raw content, got: %s", snippet)
	}
}

// TestDefaultClassify_CodeDrivesClassification exercises the full
// DefaultClassify pipeline against the VA2 acceptance body. Three of these
// used to land in ErrClient and dead-end the failover walk; the code field
// is what rescues them now.
func TestDefaultClassify_CodeDrivesClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		body   string
		want   core.ErrorClass
	}{
		{
			"empty message but code=context_length_exceeded → ErrContextLimit",
			400,
			`{"error":{"type":"invalid_request_error","code":"context_length_exceeded","message":""}}`,
			core.ErrContextLimit,
		},
		{
			"empty message but code=insufficient_quota → ErrEndpoint",
			400,
			`{"error":{"code":"insufficient_quota","message":""}}`,
			core.ErrEndpoint,
		},
		{
			"top-level code=context_length_exceeded → ErrContextLimit",
			400,
			`{"code":"context_length_exceeded"}`,
			core.ErrContextLimit,
		},
		{
			"top-level code=insufficient_quota → ErrEndpoint",
			400,
			`{"code":"insufficient_quota"}`,
			core.ErrEndpoint,
		},
		{
			"non-429 balance error with underscore form (NF-1)",
			400,
			`{"error":{"message":"","code":"insufficient_quota"}}`,
			core.ErrEndpoint,
		},
		{
			"regression: permission_error without code stays ErrAuth (403)",
			403,
			`{"error":{"message":"you are not allowed","type":"permission_error"}}`,
			core.ErrAuth,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DefaultClassify(tc.status, []byte(tc.body)); got != tc.want {
				t.Errorf("status=%d body=%s: got %v, want %v", tc.status, tc.body, got, tc.want)
			}
		})
	}
}
