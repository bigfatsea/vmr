// Ver 2026-07-26, by Sonnet 5

// Fuzz coverage for RewriteModel: the request-side rewrite is a hand-written
// scanner, exactly the kind of code where manually enumerated test cases
// miss shapes a fuzzer finds in seconds (escaped quotes mid-string,
// duplicate keys, truncated objects, non-object top level). RewriteModel
// returning an error is an accepted outcome for input it can't safely
// rewrite (see BuildRequest's callers, which turn that into a build-error
// attempt, not a passthrough) — the invariant fuzzed here is narrower and
// matches what the code actually promises: whenever it DOES return success,
// the output must still decode and "model" must carry the requested value.
// How strictly "every other key" must be preserved depends on which of
// RewriteModel's two internal paths ran: the fast splice path promises
// byte-for-byte preservation outside the model value's range; the generic
// fallback re-serializes the whole body and only promises semantic
// equivalence (its own doc comment already accepts reformatting as that
// path's known cost) — see tookFastPath below.
package adapter

import (
	"encoding/json"
	"reflect"
	"testing"
	"unicode/utf8"
)

// fuzzRoleRewrite is the shared invariant check for FuzzRewriteRoles and
// FuzzRewriteInputRoles: rewriteRolesInTopLevelArray is a pure byte splice
// confined to "role" string values inside objects that are direct elements
// of the named top-level array — unlike RewriteModel it has no generic
// fallback path, so the promise is unconditional: every top-level key byte-
// identical except the array itself, the array's element count unchanged,
// and every element's keys other than "role" semantically unchanged.
func fuzzRoleRewrite(t *testing.T, raw []byte, roleMap map[string]string, arrayKey string, rewrite func(json.RawMessage, map[string]string) ([]byte, error)) {
	out, err := rewrite(raw, roleMap)
	if err != nil {
		return // rejecting input it can't safely rewrite is an accepted outcome
	}
	if !json.Valid(raw) {
		return // garbage in: no shape guarantee on the output, only that it didn't crash (checked by the fuzzer regardless)
	}
	if !json.Valid(out) {
		t.Fatalf("rewrite returned no error but produced invalid JSON: raw=%s\nout=%s", raw, out)
	}

	var rawTop, outTop map[string]json.RawMessage
	if json.Unmarshal(raw, &rawTop) != nil {
		return // valid JSON but not a top-level object (e.g. top-level array/string): must have been left untouched
	}
	if err := json.Unmarshal(out, &outTop); err != nil {
		t.Fatalf("out isn't a top-level object though raw was: raw=%s\nout=%s", raw, out)
	}
	if len(rawTop) != len(outTop) {
		t.Fatalf("top-level key count changed: raw had %d, out has %d\nraw=%s\nout=%s", len(rawTop), len(outTop), raw, out)
	}
	for k, v := range rawTop {
		if k == arrayKey {
			continue
		}
		ov, ok := outTop[k]
		if !ok || string(ov) != string(v) {
			t.Fatalf("key %q changed by a role-only splice: raw=%s out=%s", k, v, ov)
		}
	}

	rawArr, rawHasArr := rawTop[arrayKey]
	outArr, outHasArr := outTop[arrayKey]
	if rawHasArr != outHasArr {
		t.Fatalf("presence of %q key changed: raw=%v out=%v", arrayKey, rawHasArr, outHasArr)
	}
	if !rawHasArr {
		return
	}

	var rawElems, outElems []json.RawMessage
	if json.Unmarshal(rawArr, &rawElems) != nil {
		if string(rawArr) != string(outArr) {
			t.Fatalf("%q value wasn't a JSON array but was still rewritten: raw=%s out=%s", arrayKey, rawArr, outArr)
		}
		return
	}
	if err := json.Unmarshal(outArr, &outElems); err != nil {
		t.Fatalf("%q was a valid array in raw but isn't in out: raw=%s\nout=%s", arrayKey, rawArr, outArr)
	}
	if len(rawElems) != len(outElems) {
		t.Fatalf("%q element count changed: raw had %d, out has %d\nraw=%s\nout=%s", arrayKey, len(rawElems), len(outElems), rawArr, outArr)
	}

	for idx := range rawElems {
		var rawObj, outObj map[string]json.RawMessage
		rawIsObj := json.Unmarshal(rawElems[idx], &rawObj) == nil
		outIsObj := json.Unmarshal(outElems[idx], &outObj) == nil
		if !rawIsObj {
			if string(rawElems[idx]) != string(outElems[idx]) {
				t.Fatalf("non-object element %d changed: raw=%s out=%s", idx, rawElems[idx], outElems[idx])
			}
			continue
		}
		if !outIsObj {
			t.Fatalf("element %d was an object in raw but isn't in out: raw=%s\nout=%s", idx, rawElems[idx], outElems[idx])
		}
		if len(rawObj) != len(outObj) {
			t.Fatalf("element %d key count changed: raw=%s out=%s", idx, rawElems[idx], outElems[idx])
		}
		for k, v := range rawObj {
			if k != "role" {
				ov, ok := outObj[k]
				if !ok || string(ov) != string(v) {
					t.Fatalf("element %d key %q changed by a role-only splice: raw=%s out=%s", idx, k, rawElems[idx], outElems[idx])
				}
				continue
			}
			ov, ok := outObj["role"]
			if !ok {
				t.Fatalf("element %d lost its \"role\" key: raw=%s out=%s", idx, rawElems[idx], outElems[idx])
			}
			var rawRole, outRole string
			if json.Unmarshal(v, &rawRole) != nil {
				if string(v) != string(ov) {
					t.Fatalf("element %d non-string role value changed: raw=%s out=%s", idx, v, ov)
				}
				continue
			}
			if json.Unmarshal(ov, &outRole) != nil {
				t.Fatalf("element %d role value went from string to non-string: raw=%s out=%s", idx, v, ov)
			}
			want := rawRole
			if mapped, exists := roleMap[rawRole]; exists {
				want = mapped
			}
			if outRole != want {
				t.Fatalf("element %d role = %q, want %q (roleMap=%v): raw=%s out=%s", idx, outRole, want, roleMap, rawElems[idx], outElems[idx])
			}
		}
	}
}

// tookFastPath reports whether RewriteModel would take the fast splice path
// for raw (mirrors the exact condition in RewriteModel's own body) — used
// only to pick which invariant applies below, never to predict the
// resulting bytes.
func tookFastPath(raw []byte) bool {
	ranges, ok := topLevelValues(raw, modelKeyLiteral)
	return ok && len(ranges) > 0
}

func FuzzRewriteModel(f *testing.F) {
	seeds := []struct {
		raw   string
		model string
	}{
		{`{"model":"vm","messages":[{"role":"user","content":"hi"}]}`, "real-model"},
		{`{"messages":[{"role":"user","content":"send {\"model\":\"fake\"} please"}],"model":"vm"}`, "up"},
		{`{"model":"vm","metadata":{"model":"keep"},"tools":[{"parameters":{"model":{"type":"string"}}}]}`, "up"},
		{`{"messages":[]}`, "up"},          // no top-level model key: generic fallback adds it
		{`{"model":"a","model":"b"}`, "c"}, // duplicate top-level key
		{`not json at all`, "x"},
		{`{"model": "vm" `, "x"}, // truncated / unterminated object
		{``, "x"},
		{`[]`, "x"},            // top-level array, not object
		{`{"model":123}`, "x"}, // non-string model value
		{`{"model":null}`, "x"},
		{`{"model":"vm","unicode":"é中"}`, "üp"},
		{`  {  "model"  :  "vm"  ,  "stream" : true }  `, "spaced"},
	}
	for _, s := range seeds {
		f.Add([]byte(s.raw), s.model)
	}

	f.Fuzz(func(t *testing.T, raw []byte, model string) {
		out, err := RewriteModel(raw, model)
		if err != nil {
			return // rejecting input it can't safely rewrite is an accepted outcome
		}

		// Reference decode via the standard library — independent of this
		// package's own byte scanner — so a bug shared between the scanner
		// and this check can't hide a real divergence.
		var rawMap map[string]json.RawMessage
		if json.Unmarshal(raw, &rawMap) != nil {
			return // raw wasn't a clean top-level JSON object; nothing to compare
		}
		var outMap map[string]json.RawMessage
		if err := json.Unmarshal(out, &outMap); err != nil {
			t.Fatalf("RewriteModel returned no error but produced invalid JSON: %v\nraw=%s\nout=%s", err, raw, out)
		}

		var gotModel string
		if err := json.Unmarshal(outMap["model"], &gotModel); err != nil {
			t.Fatalf("output's \"model\" field isn't a JSON string: %s\nraw=%s", outMap["model"], raw)
		}
		// Invalid UTF-8 in the fuzzed model string round-trips through
		// encoding/json as U+FFFD replacement runes — that's json.Marshal's
		// documented behavior, not a RewriteModel bug, so only compare when
		// the fuzzed value was valid UTF-8 to begin with.
		if utf8.ValidString(model) && gotModel != model {
			t.Fatalf("model = %q, want %q\nraw=%s\nout=%s", gotModel, model, raw, out)
		}

		// Which path RewriteModel took decides how strict the "other keys
		// untouched" check can be: the fast splice path promises byte-for-
		// byte preservation outside the model value's range (its whole
		// reason to exist); the generic fallback (used when no top-level
		// "model" key was found to splice) re-serializes the entire body —
		// its own doc comment already accepts reformatting (key order,
		// insignificant whitespace) as a known cost of that path, so only
		// semantic equality applies there.
		fastPath := tookFastPath(raw)
		for k, v := range rawMap {
			if k == "model" {
				continue
			}
			ov, ok := outMap[k]
			if !ok {
				t.Fatalf("key %q present in raw is missing from out\nraw=%s\nout=%s", k, raw, out)
			}
			if fastPath {
				if string(ov) != string(v) {
					t.Fatalf("key %q value changed by a model-only splice: raw=%s out=%s", k, v, ov)
				}
				continue
			}
			var rv, ov2 any
			if json.Unmarshal(v, &rv) != nil || json.Unmarshal(ov, &ov2) != nil {
				continue // not comparable this way; the length check below still applies
			}
			if !reflect.DeepEqual(rv, ov2) {
				t.Fatalf("key %q semantically changed by the generic fallback: raw=%s out=%s", k, v, ov)
			}
		}
		wantLen := len(rawMap)
		if _, hadModel := rawMap["model"]; !hadModel {
			wantLen++ // the generic fallback path adds the key when absent
		}
		if len(outMap) != wantLen {
			t.Fatalf("top-level key count changed: raw had %d (model present=%v), out has %d\nraw=%s\nout=%s",
				len(rawMap), rawMap["model"] != nil, len(outMap), raw, out)
		}
	})
}

// FuzzRewriteStream mirrors FuzzRewriteModel for RewriteStream, which
// splices/adds the top-level "stream" boolean via the same scanner +
// generic-fallback shape (and shared the same nil-map panic on a
// non-object body, fixed alongside RewriteModel's).
func FuzzRewriteStream(f *testing.F) {
	seeds := []string{
		`{"model":"vm","stream":true}`,
		`{"model":"vm"}`, // no top-level stream key: generic fallback adds it
		`not json at all`,
		`null`,
		`[]`,
		``,
		`{"stream": false `,
	}
	for _, s := range seeds {
		f.Add([]byte(s), true)
	}

	f.Fuzz(func(t *testing.T, raw []byte, stream bool) {
		out, err := RewriteStream(raw, stream)
		if err != nil {
			return
		}
		if !json.Valid(raw) {
			// topLevelValues (shared with ingress's TopLevelProbe) is a
			// structural scanner, not a strict validator — same leniency
			// already relied on for trailing-garbage bodies. Garbage in
			// produces no guarantee about the shape of what comes out,
			// only that RewriteStream doesn't crash on it (checked by the
			// fuzzer regardless of the assertions below).
			return
		}
		var outMap map[string]json.RawMessage
		if err := json.Unmarshal(out, &outMap); err != nil {
			t.Fatalf("RewriteStream returned no error but produced invalid JSON: %v\nraw=%s\nout=%s", err, raw, out)
		}
		var gotStream bool
		if err := json.Unmarshal(outMap["stream"], &gotStream); err != nil {
			t.Fatalf("output's \"stream\" field isn't a JSON bool: %s\nraw=%s", outMap["stream"], raw)
		}
		if gotStream != stream {
			t.Fatalf("stream = %v, want %v\nraw=%s\nout=%s", gotStream, stream, raw, out)
		}
	})
}

// fuzzRoleMap is fixed rather than fuzzed: RewriteRoles/RewriteInputRoles'
// own contract is defined in terms of "does the configured map apply
// correctly", and a fuzzed map would mostly generate table misses (rewrite
// keeps roles unchanged) — the input worth fuzzing is the request body's
// shape, not the (small, config-driven) roleMap itself. Two entries so the
// invariant check in fuzzRoleRewrite exercises both a mapped-and-rewritten
// role and a present-but-unmapped role staying put.
var fuzzRoleMap = map[string]string{"developer": "system", "user": "human"}

// FuzzRewriteRoles is rewriteRolesInTopLevelArray's messages-array path
// (Chat Completions / Anthropic Messages shape) — the most complex
// hand-written scanner in this package (descends into an array, then into
// each element object, tracking a "role" key match) and, unlike
// RewriteModel/RewriteStream, previously had no fuzz coverage at all despite
// running on every request whose endpoint-group configures role_map.
func FuzzRewriteRoles(f *testing.F) {
	seeds := []string{
		`{"model":"vm","messages":[{"role":"developer","content":"hi"}]}`,
		`{"model":"vm","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"there"}]}`,
		`{"model":"vm","messages":[{"role":"unmapped","content":"hi"}]}`, // role present, no roleMap entry: stays put
		`{"model":"vm","messages":[{"content":"no role field here"}]}`,   // element without a "role" key
		`{"model":"vm","messages":[]}`,                                   // empty array
		`{"model":"vm"}`,                                                 // no top-level "messages" key at all
		`{"messages":"not an array"}`,                                    // messages is a string, not an array
		`{"messages":[1,2,"three",null,{"role":"developer"}]}`,           // non-object elements mixed with an object one
		`{"messages":[{"role":"developer","role":"user"}]}`,              // duplicate "role" key in one object
		`{"messages":[{"role":{"nested":"object"}}]}`,                    // "role" value is not a string
		`{"messages":[{"content":"the word \"role\" appears in content, not as a key"}]}`,
		`not json at all`,
		`null`,
		`[]`,
		``,
		`{"messages":[{"role":"developer" `, // truncated element
		`{"messages":[{"role": "developer" , "content" : "spaced"  }]}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		fuzzRoleRewrite(t, raw, fuzzRoleMap, "messages", RewriteRoles)
	})
}

// FuzzRewriteInputRoles is rewriteRolesInTopLevelArray's Responses-protocol
// counterpart — the top-level "input" array, where (unlike "messages") a
// bare string input and role-less Items (function_call/reasoning) are both
// expected, not edge cases.
func FuzzRewriteInputRoles(f *testing.F) {
	seeds := []string{
		`{"model":"vm","input":[{"role":"developer","content":"hi"}]}`,
		`{"model":"vm","input":"hello"}`,                                                // bare string input: not an array, must be left untouched
		`{"model":"vm","input":[{"type":"function_call","name":"f","arguments":"{}"}]}`, // role-less Item
		`{"model":"vm","input":[{"role":"user","content":"hi"},{"type":"reasoning","summary":[]}]}`,
		`{"model":"vm"}`,
		`{"input":[]}`,
		`not json at all`,
		`[]`,
		``,
		`{"input":[{"role":"developer" `,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		fuzzRoleRewrite(t, raw, fuzzRoleMap, "input", RewriteInputRoles)
	})
}
