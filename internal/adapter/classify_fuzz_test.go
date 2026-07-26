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
