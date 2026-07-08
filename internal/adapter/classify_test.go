// Ver 2026-07-08 12:05, by Fable 5
package adapter

import (
	"strings"
	"testing"
)

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
