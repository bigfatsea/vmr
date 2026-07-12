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

func TestRewriteModel_ByteSplicePreservesEverythingElse(t *testing.T) {
	// The splice path must keep every byte outside the model value —
	// key order, whitespace, formatting — exactly as the client sent it.
	raw := []byte(`{"messages":[{"role":"user","content":"hi"}],  "model" : "vm-name" , "stream":true}`)
	out, err := RewriteModel(raw, "real")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"messages":[{"role":"user","content":"hi"}],  "model" : "real" , "stream":true}`
	if string(out) != want {
		t.Errorf("splice output mismatch:\n got %s\nwant %s", out, want)
	}
}

func TestRewriteModel_NestedModelKeysUntouched(t *testing.T) {
	// Only the TOP-LEVEL model key is rewritten; "model" keys nested in
	// tool schemas / metadata objects / arrays must pass through verbatim.
	raw := []byte(`{"model":"vm","metadata":{"model":"keep-a"},"tools":[{"parameters":{"model":{"type":"string"}}}]}`)
	out, err := RewriteModel(raw, "up")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"metadata":{"model":"keep-a"}`) {
		t.Errorf("nested object model rewritten: %s", s)
	}
	if !strings.Contains(s, `"model":{"type":"string"}`) {
		t.Errorf("tool-schema model rewritten: %s", s)
	}
	if !strings.HasPrefix(s, `{"model":"up",`) {
		t.Errorf("top-level model not rewritten: %s", s)
	}
}

func TestRewriteModel_EscapedTextInContentUntouched(t *testing.T) {
	// A literal `"model":"x"` mentioned inside a content string arrives
	// JSON-escaped; the scanner must skip over it, not rewrite it.
	raw := []byte(`{"content":"send {\"model\":\"fake\"} please","model":"vm"}`)
	out, err := RewriteModel(raw, "up")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"content":"send {\"model\":\"fake\"} please","model":"up"}`
	if string(out) != want {
		t.Errorf("got %s want %s", out, want)
	}
}

func TestRewriteModel_SameNameZeroCopy(t *testing.T) {
	// Virtual name == upstream name: the original slice is returned as-is.
	raw := []byte(`{"model":"same","messages":[]}`)
	out, err := RewriteModel(raw, "same")
	if err != nil {
		t.Fatal(err)
	}
	if &out[0] != &raw[0] {
		t.Error("expected zero-copy return of the original slice")
	}
}

func TestRewriteModel_MissingKeyFallsBackAndAdds(t *testing.T) {
	// No top-level model key: the generic fallback adds it (historical
	// behavior for direct callers; the server rejects such requests earlier).
	out, err := RewriteModel([]byte(`{"messages":[]}`), "up")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"model":"up"`) {
		t.Errorf("model key not added: %s", out)
	}
}

// benchBody builds an agent-shaped request body of roughly n bytes.
func benchBody(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"messages":[`)
	chunk := `{"role":"user","content":"` + strings.Repeat(`tool output with \"quotes\" and text `, 50) + `"},`
	for b.Len() < n {
		b.WriteString(chunk)
	}
	s := strings.TrimSuffix(b.String(), ",")
	return []byte(s + `],"model":"virtual-name","stream":true}`)
}

func BenchmarkRewriteModelSplice(b *testing.B) {
	raw := benchBody(200 << 10)
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := RewriteModel(raw, "upstream-model"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRewriteModelGeneric(b *testing.B) {
	raw := benchBody(200 << 10)
	mv, _ := core.MarshalNoEscape("upstream-model")
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := rewriteModelGeneric(raw, mv); err != nil {
			b.Fatal(err)
		}
	}
}
