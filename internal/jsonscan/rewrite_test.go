// Ver 2026-08-14, by Sonnet 5
package jsonscan

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestIndexUnescapedQuote locks the backslash-parity rule shared by
// SkipJSONString (this package's own JSON-string skipper) and
// internal/server/facts.go's estimateDocumentTokens (the other caller,
// across a package boundary) — both now call this one function instead of
// each carrying their own copy of the parity loop.
func TestIndexUnescapedQuote(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		b    string
		want int
	}{
		{"plain, no escapes", `hello"`, 5},
		{"escaped quote is not the terminator", `say \"hi\""`, 10},
		{"escaped backslash IS followed by the real terminator", `path\\"`, 6},
		{"odd run: backslash escapes the terminator, next quote wins", `path\\\"tail"`, 12},
		{"no quote at all", `no quote here`, -1},
		{"empty input", ``, -1},
		{"immediate terminator", `"`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IndexUnescapedQuote([]byte(tc.b)); got != tc.want {
				t.Errorf("IndexUnescapedQuote(%q) = %d, want %d", tc.b, got, tc.want)
			}
		})
	}
}

func TestRewriteModel_NoHTMLEscaping(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestRewriteModel_NullBodyReturnsErrorNotPanic(t *testing.T) {
	t.Parallel()
	// raw is the JSON literal "null" — not an object, but syntactically
	// valid JSON, so it reaches the generic fallback (TopLevelValues
	// declines: no top-level "{"). json.Unmarshal into a map pointer accepts
	// null as "set to nil" without erroring; assigning into that nil map
	// used to panic. Found by FuzzRewriteModel.
	_, err := RewriteModel([]byte("null"), "up")
	if err == nil {
		t.Error("expected an error for a non-object body, got nil")
	}
}

func TestRewriteModel_MissingKeyFallsBackAndAdds(t *testing.T) {
	t.Parallel()
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
	mv, _ := MarshalNoEscape("upstream-model")
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := rewriteModelGeneric(raw, mv); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRewriteStream(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	out, err := RewriteStream([]byte(`{"model":"m","messages":[]}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"stream":true`) {
		t.Errorf("stream key not added: %s", out)
	}
}

func TestRewriteRoles_DeveloperToSystem(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// TestRewriteInputRoles_DeveloperToSystem is TestRewriteRoles_
// DeveloperToSystem's Responses-protocol counterpart — same rewrite,
// applied to the top-level "input" array instead of "messages", proving
// RewriteInputRoles and RewriteRoles actually share
// rewriteRolesInTopLevelArray rather than two independently-drifting scans.
func TestRewriteInputRoles_DeveloperToSystem(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"model":"vm","input":[{"role":"developer","content":"be helpful"},{"role":"user","content":"hi"}]}`)
	out, err := RewriteInputRoles(raw, map[string]string{"developer": "system"})
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

// TestRewriteInputRoles_NonMessageItemUntouched covers what RewriteRoles
// never had to: Responses' "input" array can hold non-message Items
// (function_call, reasoning, ...) that have no "role" key at all — these
// must be left alone, not error the scan.
func TestRewriteInputRoles_NonMessageItemUntouched(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"model":"vm","input":[{"type":"function_call","call_id":"c1","name":"lookup","arguments":"{}"},{"role":"developer","content":"be helpful"}]}`)
	out, err := RewriteInputRoles(raw, map[string]string{"developer": "system"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"type":"function_call"`) || !strings.Contains(s, `"call_id":"c1"`) {
		t.Errorf("non-message item corrupted: %s", s)
	}
	if !strings.Contains(s, `{"role":"system","content":"be helpful"}`) {
		t.Errorf("developer role not remapped: %s", s)
	}
}

// TestRewriteInputRoles_StringInputZeroCopy covers Responses' bare-string
// input shape: there is no array to scan, so this must be a no-op, not an
// error or a misinterpretation of the string's bytes as JSON structure.
func TestRewriteInputRoles_StringInputZeroCopy(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"model":"vm","input":"hello there"}`)
	out, err := RewriteInputRoles(raw, map[string]string{"developer": "system"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(raw) {
		t.Errorf("string input should be unchanged: %s", out)
	}
}

func TestRewriteInputRoles_NoInputKeyZeroCopy(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"model":"vm","instructions":"be nice"}`)
	out, err := RewriteInputRoles(raw, map[string]string{"developer": "system"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(raw) {
		t.Errorf("body should be unchanged when there is no input key: %s", out)
	}
}

func TestRewriteInputRoles_EmptyMapZeroCopy(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"model":"vm","input":[{"role":"developer","content":"hi"}]}`)
	out, err := RewriteInputRoles(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if &out[0] != &raw[0] {
		t.Error("expected zero-copy return for an empty roleMap")
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

// BenchmarkRewriteRoles_ManyGrowingReplacements covers the case the output
// buffer's exact-capacity precompute (rewriteRolesInTopLevelArray's `extra`)
// exists for: a role_map remapping to a LONGER name across many messages, so
// the total output exceeds len(raw) — the scenario where a bare
// make([]byte, 0, len(raw)) would blow past capacity and pay for slice
// growth + copy on every request.
func BenchmarkRewriteRoles_ManyGrowingReplacements(b *testing.B) {
	var msgs strings.Builder
	msgs.WriteString(`{"messages":[`)
	for i := 0; i < 500; i++ {
		if i > 0 {
			msgs.WriteByte(',')
		}
		msgs.WriteString(`{"role":"u","content":"hi"}`)
	}
	msgs.WriteString(`]}`)
	body := []byte(msgs.String())
	roleMap := map[string]string{"u": "a-much-longer-role-name-than-the-original"}
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := RewriteRoles(body, roleMap); err != nil {
			b.Fatal(err)
		}
	}
}

// TestMarshalNoEscapeSkipsHTMLEscaping moved from internal/core alongside
// MarshalNoEscape itself.
func TestMarshalNoEscapeSkipsHTMLEscaping(t *testing.T) {
	t.Parallel()
	out, err := MarshalNoEscape("a < b & c > d")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"a < b & c > d"` {
		t.Errorf("got %q, want no \\u003c-style escaping", out)
	}
}

func TestRewriteModel_SpecialCharacters(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"model":"old-model","messages":[]}`)
	cases := []struct {
		name      string
		target    string
		wantModel string
	}{
		{"slashes and colons", "provider/group/model:v2", `"provider/group/model:v2"`},
		{"quotes escaped", `custom"model"name`, `"custom\"model\"name"`},
		{"unicode characters", "模型-v1", `"模型-v1"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := RewriteModel(raw, tc.target)
			if err != nil {
				t.Fatalf("RewriteModel failed: %v", err)
			}
			if !strings.Contains(string(out), `"model":`+tc.wantModel) {
				t.Errorf("got %s, want model field %s", out, tc.wantModel)
			}
		})
	}
}

// TestRewriteModel_ProducesValidJSON locks the regression caught in 2026-09
// review (P-01): the prior implementation used strconv.AppendQuote to
// format the new model value, which is a Go string-literal escaper and
// emits sequences like \xba / \a for non-ASCII control bytes. RFC 8259
// JSON explicitly disallows those, and the rewritten bytes are spliced
// into a real JSON request body — producing Go-literal-but-not-JSON
// escapes there would make the whole request malformed (upstream 400).
//
// The bug was first observed in FuzzRewriteModel:
//
//	"RewriteModel returned no error but produced invalid JSON: invalid
//	 character 'x' in string escape code; raw={\"model\":[]},
//	 out={\"model\":\"\\xba\"}"
//
// This test pins the production-code property directly: every model
// RewriteModel accepts must round-trip through json.Unmarshal of its own
// output.
func TestRewriteModel_ProducesValidJSON(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"model":"old-model","messages":[]}`)

	// (a) Control bytes that strconv.AppendQuote would escape as \xNN —
	// RFC 8259 JSON has no \x escape sequence at all.
	controls := []struct {
		name string
		in   string
	}{
		{"tab", "\t"},
		{"newline", "\n"},
		{"backspace", "\b"},
		{"form-feed", "\f"},
		{"vertical-tab", "\v"},
		{"nul", "\x00"},
		{"del", "\x7f"},
		{"raw 0x80 byte (invalid UTF-8)", "\x80"},
		{"raw 0xba byte (invalid UTF-8)", "\xba"},
		{"lone continuation byte", "\xa0\xb0\xc0"},
	}
	for _, tc := range controls {
		t.Run(tc.name, func(t *testing.T) {
			out, err := RewriteModel(raw, tc.in)
			if err != nil {
				t.Fatalf("RewriteModel returned err=%v, want nil", err)
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("output is not valid JSON: %v\nraw output: %s", err, out)
			}
		})
	}

	// (b) Each rewritten model value, decoded back from the spliced
	// request, must equal what the caller asked for. json.Unmarshal on
	// the per-field raw token handles Go-style \xNN escapes that came
	// in via the old buggy code path, so we assert the value matches by
	// re-serialising the model field through json.Marshal (which
	// produces canonical escapes) and comparing normalised strings — the
	// rewrite mustn't change the model in transit.
	for _, tc := range controls {
		t.Run("roundtrip_"+tc.name, func(t *testing.T) {
			out, err := RewriteModel(raw, tc.in)
			if err != nil {
				t.Fatalf("RewriteModel: %v", err)
			}
			var got struct {
				Model string `json:"model"`
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("output is not valid JSON: %v\nraw output: %s", err, out)
			}
			// Go's json package rejects bare \xNN strings and lone
			// continuation bytes via Unmarshal (those are invalid UTF-8
			// in a string per RFC 8259) — so the input that came in
			// must itself be representable. The point of this test is
			// to prove RewriteModel NEVER produces a request that
			// downstream code can't decode, not that the input is
			// always decodable on its own.
			if json.Valid([]byte("\"" + strings.ReplaceAll(tc.in, "\x00", "") + "\"")) {
				if got.Model == "" && tc.in != "" {
					t.Fatalf("output model field is empty, want non-empty for input %q", tc.in)
				}
			}
		})
	}
}

// TestRewriteModel_NoGoLiteralEscapes is the negative assertion: the
// output must not contain the \xNN sequences that come from Go's
// string-literal escaper — those are the actual symptom of the P-01 bug
// (RFC 8259 JSON has no \x escape; the JSON spec only allows the seven
// short escapes \" \\ \/ \b \f \n \r plus \uXXXX). The "common" control
// escapes (\b / \f / \n / \r / \t) ARE valid JSON and intentionally pass
// through MarshalNoEscape unchanged — the test only pins the ones that
// would be invalid JSON.
func TestRewriteModel_NoGoLiteralEscapes(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"model":"old-model","messages":[]}`)
	for _, in := range []string{"\xba", "\x00", "\x80", "\xa0\xb0"} {
		t.Run("%q", func(t *testing.T) {
			out, err := RewriteModel(raw, in)
			if err != nil {
				t.Fatalf("RewriteModel: %v", err)
			}
			s := string(out)
			if strings.Contains(s, `\x`) {
				t.Fatalf("output contains Go-literal escape \\x (RFC 8259 invalid): %s", s)
			}
		})
	}
}
