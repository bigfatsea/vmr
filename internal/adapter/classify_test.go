// Ver 2026-07-08 12:05, by Fable 5
package adapter

import (
	"strings"
	"testing"

	"vmr/internal/core"
)

func TestDefaultClassify_MarkerBeyond2KB(t *testing.T) {
	// Vendors may attach verbose debug payloads before the actual error
	// message; a marker past the old 2 KB cutoff must still be sniffed —
	// a miss classifies as ErrClient, which never fails over.
	padding := strings.Repeat(`{"debug":"xxxxxxxxxxxxxxxx"},`, 200) // ~5.6 KB
	cases := []struct {
		name string
		body string
		want core.ErrorClass
	}{
		{"model not found late", `{"trace":[` + padding + `],"error":{"message":"model gpt-x not found"}}`, core.ErrEndpoint},
		{"content flag late", `{"trace":[` + padding + `],"error":{"message":"output data may contain inappropriate content (1027)"}}`, core.ErrContent},
	}
	for _, tc := range cases {
		if got := DefaultClassify(400, []byte(tc.body)); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
	// Beyond the 32 KB bound the marker is invisible by design.
	huge := strings.Repeat("x", classifySnippetBytes) + "model not found"
	if got := DefaultClassify(400, []byte(huge)); got != core.ErrClient {
		t.Errorf("marker past bound: got %v, want %v", got, core.ErrClient)
	}
}

func TestRewriteModel_NoHTMLEscaping(t *testing.T) {
	// Direct-equivalence: re-serialization must not rewrite < > & in
	// message content to their \uXXXX escape forms — a direct call
	// would send the raw characters.
	raw := []byte(`{"model":"vm","messages":[{"role":"user","content":"a < b && c > d <think>keep literal</think>"}]}`)
	out, err := RewriteModel(raw, "upstream-model")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, esc := range []string{"\\u003c", "\\u003e", "\\u0026"} {
		if strings.Contains(s, esc) {
			t.Errorf("HTML escape %s leaked into rewritten body: %s", esc, s)
		}
	}
	if !strings.Contains(s, "a < b && c > d") || !strings.Contains(s, "<think>keep literal</think>") {
		t.Errorf("content bytes not preserved: %s", s)
	}
	if !strings.Contains(s, `"model":"upstream-model"`) {
		t.Errorf("model not rewritten: %s", s)
	}
}

func TestRewriteModel_UnknownFieldsPreserved(t *testing.T) {
	raw := []byte(`{"model":"vm","future_param":{"nested":[1,2,3]},"temperature":0.5}`)
	out, err := RewriteModel(raw, "up")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`"future_param":{"nested":[1,2,3]}`, `"temperature":0.5`, `"model":"up"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
}
