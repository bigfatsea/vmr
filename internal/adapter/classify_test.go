// Ver 2026-07-13 04:00, by Sonnet 5
package adapter

import (
	"strings"
	"testing"

	"vmr/internal/core"
)

func TestDefaultClassify_MarkerDeepInBody(t *testing.T) {
	// Vendors may attach verbose debug payloads before the actual error
	// message; a marker several KB into the body must still be sniffed
	// within the snippet cutoff — a miss classifies as ErrClient, which
	// never fails over.
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

// TestDefaultClassify_UpstreamGatewayFailure locks in the fix for the
// incident documented in reports/incident-20260718-console-go-400-failover_
// Sonnet5.md: a relay hop reporting its own forwarding failure must not
// dead-end the failover walk the way a genuine bad-request 400 correctly
// does.
func TestDefaultClassify_UpstreamGatewayFailure(t *testing.T) {
	cases := []struct {
		name string
		body string
		want core.ErrorClass
	}{
		{
			"opencode Console Go relay failure (the actual incident body)",
			`{"message":"Error from provider (Console Go): Upstream request failed","type":"invalid_request_error","param":null,"code":"invalid_request_error"}`,
			core.ErrEndpoint,
		},
		{"bad gateway wording", `{"error":{"message":"502 Bad Gateway from upstream"}}`, core.ErrEndpoint},
		{"gateway timeout wording", `{"error":{"message":"Gateway Timeout while contacting upstream"}}`, core.ErrEndpoint},
		// Genuine request-content errors must still classify as ErrClient —
		// upstreamHint must not swallow these just because "model" or generic
		// wording appears nearby.
		{"missing field is still ErrClient", `{"error":{"message":"missing required field: messages"}}`, core.ErrClient},
		{"malformed json is still ErrClient", `{"error":{"message":"invalid JSON payload"}}`, core.ErrClient},
		// contentHint and the model-not-found rule still take priority over
		// upstreamHint when both could apply.
		{"content flag beats upstream wording", `{"error":{"message":"upstream request failed: content_policy violation"}}`, core.ErrContent},
	}
	for _, tc := range cases {
		if got := DefaultClassify(400, []byte(tc.body)); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
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

func TestRewriteStream(t *testing.T) {
	cases := []struct {
		name, raw, want string
		stream          bool
	}{
		{"false to true, bytes otherwise preserved",
			`{"messages":[{"role":"user","content":"hi"}],  "stream" : false , "model":"m"}`,
			`{"messages":[{"role":"user","content":"hi"}],  "stream" : true , "model":"m"}`, true},
		{"true to false",
			`{"stream":true,"model":"m"}`, `{"stream":false,"model":"m"}`, false},
		{"nested stream key untouched",
			`{"stream":false,"metadata":{"stream":true}}`, `{"stream":true,"metadata":{"stream":true}}`, true},
		{"escaped mention in content untouched",
			`{"content":"set {\"stream\":false} please","stream":false}`,
			`{"content":"set {\"stream\":false} please","stream":true}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := RewriteStream([]byte(c.raw), c.stream)
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != c.want {
				t.Errorf("got %s\nwant %s", out, c.want)
			}
		})
	}
}

func TestRewriteStream_SameValueZeroCopy(t *testing.T) {
	raw := []byte(`{"stream":true,"model":"m"}`)
	out, err := RewriteStream(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if &out[0] != &raw[0] {
		t.Error("expected zero-copy return when the value already matches")
	}
}

func TestRewriteStream_MissingKeyAdded(t *testing.T) {
	out, err := RewriteStream([]byte(`{"model":"m","messages":[]}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"stream":true`) {
		t.Errorf("stream key not added: %s", out)
	}
}

func TestRewriteRoles_DeveloperToSystem(t *testing.T) {
	raw := []byte(`{"model":"vm","messages":[{"role":"developer","content":"be helpful"},{"role":"user","content":"hi"}]}`)
	out, err := RewriteRoles(raw, map[string]string{"developer": "system"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `{"role":"system","content":"be helpful"}`) {
		t.Errorf("developer role not remapped: %s", s)
	}
	if !strings.Contains(s, `{"role":"user","content":"hi"}`) {
		t.Errorf("user role should be untouched: %s", s)
	}
	if strings.Contains(s, `"developer"`) {
		t.Errorf("developer role still present: %s", s)
	}
}

func TestRewriteRoles_MultipleMatchesAllReplaced(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"developer","content":"a"},{"role":"user","content":"b"},{"role":"developer","content":"c"}],"model":"vm"}`)
	out, err := RewriteRoles(raw, map[string]string{"developer": "system"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Count(s, `"system"`) != 2 {
		t.Errorf("expected 2 system roles, got %d: %s", strings.Count(s, `"system"`), s)
	}
	if strings.Contains(s, `"developer"`) {
		t.Errorf("developer role still present: %s", s)
	}
}

func TestRewriteRoles_ByteSplicePreservesEverythingElse(t *testing.T) {
	// The splice must keep every byte outside role values — key order,
	// whitespace, formatting, other fields — exactly as the client sent it.
	raw := []byte(`{"messages":[ {"role":"developer", "content":"hi"} ],  "model" : "vm"}`)
	out, err := RewriteRoles(raw, map[string]string{"developer": "system"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"messages":[ {"role":"system", "content":"hi"} ],  "model" : "vm"}`
	if string(out) != want {
		t.Errorf("splice output mismatch:\n got %s\nwant %s", out, want)
	}
}

func TestRewriteRoles_DeveloperInContentUntouched(t *testing.T) {
	// The string "developer" inside message content must NOT be remapped.
	// JSON escaping ensures the scanner sees it as part of a string value,
	// not as a key-value pair.
	raw := []byte(`{"messages":[{"role":"user","content":"ask the developer about this"}],"model":"vm"}`)
	out, err := RewriteRoles(raw, map[string]string{"developer": "system"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `ask the developer about this`) {
		t.Errorf("content word 'developer' was wrongly modified: %s", out)
	}
}

func TestRewriteRoles_EscapedRoleInContentUntouched(t *testing.T) {
	// A literal {"role":"developer"} mentioned inside a content string
	// arrives JSON-escaped; the scanner must skip over it.
	raw := []byte(`{"messages":[{"role":"user","content":"send {\"role\":\"developer\"} please"}],"model":"vm"}`)
	out, err := RewriteRoles(raw, map[string]string{"developer": "system"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"messages":[{"role":"user","content":"send {\"role\":\"developer\"} please"}],"model":"vm"}`
	if string(out) != want {
		t.Errorf("escaped content was modified:\n got %s\nwant %s", out, want)
	}
}

func TestRewriteRoles_NoHTMLEscaping(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"developer","content":"a < b && c > d"}],"model":"vm"}`)
	out, err := RewriteRoles(raw, map[string]string{"developer": "system"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, esc := range []string{"\\u003c", "\\u003e", "\\u0026"} {
		if strings.Contains(s, esc) {
			t.Errorf("HTML escape %s leaked: %s", esc, s)
		}
	}
}

func TestRewriteRoles_NoMatchZeroCopy(t *testing.T) {
	// No role matches the map: original slice returned as-is.
	raw := []byte(`{"messages":[{"role":"user","content":"hi"}],"model":"vm"}`)
	out, err := RewriteRoles(raw, map[string]string{"developer": "system"})
	if err != nil {
		t.Fatal(err)
	}
	if &out[0] != &raw[0] {
		t.Error("expected zero-copy return when no role matches")
	}
}

func TestRewriteRoles_EmptyMapZeroCopy(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"developer","content":"hi"}],"model":"vm"}`)
	out, err := RewriteRoles(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if &out[0] != &raw[0] {
		t.Error("expected zero-copy return when roleMap is empty")
	}
}

func TestRewriteRoles_NoMessagesKey(t *testing.T) {
	raw := []byte(`{"model":"vm","prompt":"hi"}`)
	out, err := RewriteRoles(raw, map[string]string{"developer": "system"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(raw) {
		t.Errorf("body should be unchanged when no messages key: %s", out)
	}
}

func TestRewriteRoles_MessagesNotArray(t *testing.T) {
	raw := []byte(`{"messages":"not-an-array","model":"vm"}`)
	out, err := RewriteRoles(raw, map[string]string{"developer": "system"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(raw) {
		t.Errorf("body should be unchanged when messages is not an array: %s", out)
	}
}

func TestRewriteRoles_MultipleMappings(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"developer","content":"a"},{"role":"tool","content":"b"}],"model":"vm"}`)
	out, err := RewriteRoles(raw, map[string]string{"developer": "system", "tool": "user"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `{"role":"system","content":"a"}`) {
		t.Errorf("developer not mapped to system: %s", s)
	}
	if !strings.Contains(s, `{"role":"user","content":"b"}`) {
		t.Errorf("tool not mapped to user: %s", s)
	}
}

func TestRewriteRoles_SameRoleZeroCopy(t *testing.T) {
	// Role already maps to itself: no replacement needed, zero-copy return.
	raw := []byte(`{"messages":[{"role":"system","content":"hi"}],"model":"vm"}`)
	out, err := RewriteRoles(raw, map[string]string{"system": "system"})
	if err != nil {
		t.Fatal(err)
	}
	if &out[0] != &raw[0] {
		t.Error("expected zero-copy return when role value already matches target")
	}
}

func BenchmarkRewriteRoles(b *testing.B) {
	raw := benchBody(200 << 10)
	// Inject a developer role at the start of the messages array.
	body := []byte(`{"messages":[{"role":"developer","content":"system prompt"},` + string(raw[14:]))
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := RewriteRoles(body, map[string]string{"developer": "system"}); err != nil {
			b.Fatal(err)
		}
	}
}
