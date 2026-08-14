// Ver 2026-08-14, by Sonnet 5
package jsonscan

import (
	"bytes"
	"encoding/json"
	"errors"
)

var (
	modelKeyLiteral    = []byte(`"model"`)
	streamKeyLiteral   = []byte(`"stream"`)
	messagesKeyLiteral = []byte(`"messages"`)
	inputKeyLiteral    = []byte(`"input"`)
)

// RewriteModel replaces the value of the top-level "model" key by splicing
// the new value into the original bytes: prefix + rewritten value + suffix.
// Every other byte of the request — key order, whitespace, unknown upstream
// parameters — reaches the provider exactly as the client sent it, which is
// as close to direct-connection equivalence as a model rewrite can get. The
// locate step is a single allocation-free scan (strings are skipped at
// bytes.IndexByte speed) — no unmarshal-into-map + re-serialize round trip
// paying for a parse and a copy of the whole body on every failover attempt.
//
// Nested "model" keys (inside messages, tool schemas, metadata objects) are
// not touched — only top-level values are rewritten. Inputs the scanner
// can't handle (not a JSON object, no literal top-level "model" key, or a
// syntax problem) fall back to the generic parse-and-rebuild path, which
// handles those shapes the same way, including adding the key when absent.
func RewriteModel(raw json.RawMessage, model string) ([]byte, error) {
	mv, err := MarshalNoEscape(model)
	if err != nil {
		return nil, err
	}
	ranges, ok := TopLevelValues(raw, modelKeyLiteral)
	if !ok || len(ranges) == 0 {
		return rewriteModelGeneric(raw, mv)
	}
	return spliceValues(raw, ranges, mv), nil
}

// RewriteStream replaces (or, via the generic fallback, adds) the top-level
// "stream" value with the given boolean, using the same byte-splice scanner
// as RewriteModel so every other byte is preserved. Used by `vmr replay
// -stream` — live traffic never rewrites this field.
func RewriteStream(raw json.RawMessage, stream bool) ([]byte, error) {
	sv := []byte("false")
	if stream {
		sv = []byte("true")
	}
	ranges, ok := TopLevelValues(raw, streamKeyLiteral)
	if !ok || len(ranges) == 0 {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		if m == nil {
			// Same "null" corner case as rewriteModelGeneric: a nillable
			// target unmarshals a JSON null without error, leaving m nil.
			return nil, errors.New("jsonscan: cannot rewrite stream: request body is not a JSON object")
		}
		m["stream"] = sv
		return MarshalNoEscape(m)
	}
	return spliceValues(raw, ranges, sv), nil
}

// spliceValues replaces every [start,end) range in raw with newVal,
// returning raw itself (zero-copy) when nothing would change.
func spliceValues(raw []byte, ranges [][2]int, newVal []byte) []byte {
	if len(ranges) == 1 && bytes.Equal(raw[ranges[0][0]:ranges[0][1]], newVal) {
		return raw // already the target value: zero-copy
	}
	out := make([]byte, 0, len(raw)+len(newVal))
	prev := 0
	for _, r := range ranges {
		out = append(out, raw[prev:r[0]]...)
		out = append(out, newVal...)
		prev = r[1]
	}
	return append(out, raw[prev:]...)
}

// rewriteModelGeneric is the pre-splice implementation, kept as the fallback
// for shapes the scanner declines: it re-serializes the whole body (key
// order not preserved — no semantics) with HTML escaping disabled, since the
// default json.Marshal would rewrite every < > & in message content to
// \uXXXX — semantically identical, but a gratuitous byte-level deviation
// from what a direct call would send.
func rewriteModelGeneric(raw json.RawMessage, mv []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		// raw was the JSON literal "null" (or similar) — Unmarshal into a
		// map pointer accepts that as "set to nil" without erroring, per
		// encoding/json's documented null-into-nillable-type rule. Not an
		// object, so there's nowhere to add "model"; without this check the
		// assignment below panics on a nil map.
		return nil, errors.New("jsonscan: cannot rewrite model: request body is not a JSON object")
	}
	m["model"] = mv
	return MarshalNoEscape(m)
}

// RewriteRoles replaces "role" values inside the top-level "messages" array
// according to roleMap (e.g. {"developer":"system"}) — the Chat Completions/
// Anthropic Messages shape, where every array element is a role-bearing
// message object. See rewriteRolesInTopLevelArray for the shared scan;
// RewriteInputRoles below is the Responses-protocol counterpart (top-level
// "input" instead of "messages", and not every element carries a role).
func RewriteRoles(raw json.RawMessage, roleMap map[string]string) ([]byte, error) {
	return rewriteRolesInTopLevelArray(raw, roleMap, messagesKeyLiteral)
}

// RewriteInputRoles is RewriteRoles' Responses-protocol counterpart: same
// byte-splice rewrite, applied to the top-level "input" array instead of
// "messages". Responses' input can also be a bare string (no messages array
// at all, e.g. input: "hello") — TopLevelValues still locates the "input"
// key's value range in that case, but the array-open check inside the
// shared scan (raw[i] != '[') declines it, so a string input is correctly
// left untouched rather than misread as an empty array.
func RewriteInputRoles(raw json.RawMessage, roleMap map[string]string) ([]byte, error) {
	return rewriteRolesInTopLevelArray(raw, roleMap, inputKeyLiteral)
}

// rewriteRolesInTopLevelArray replaces "role" values inside the top-level
// array named by arrayKeyLiteral (e.g. `"messages"` or `"input"`), according
// to roleMap (e.g. {"developer":"system"}). Only the string values of "role"
// keys within element objects are rewritten; every other byte — key order,
// whitespace, unknown parameters, message content — is preserved exactly as
// the client sent it, consistent with RewriteModel's byte-splice philosophy.
//
// The scan is JSON-aware: it descends into the named array, visits each
// element, and checks for a "role" key on objects that have one (elements
// without one — e.g. a Responses function_call/reasoning Item — are left
// alone, not an error). This avoids false positives from the string
// "developer" appearing in message content (JSON string escaping ensures
// unescaped "role":"developer" only occurs as a key-value pair, but the
// scanner also skips string values correctly). An empty roleMap returns the
// input unchanged (zero-copy).
func rewriteRolesInTopLevelArray(raw json.RawMessage, roleMap map[string]string, arrayKeyLiteral []byte) ([]byte, error) {
	if len(roleMap) == 0 {
		return raw, nil
	}

	// Locate the top-level array value.
	msgRanges, ok := TopLevelValues(raw, arrayKeyLiteral)
	if !ok || len(msgRanges) == 0 {
		return raw, nil // not a JSON object or no such key
	}

	arrStart, arrEnd := msgRanges[0][0], msgRanges[0][1]
	i := SkipJSONWS(raw, arrStart)
	if i >= len(raw) || raw[i] != '[' {
		return raw, nil // messages value is not an array
	}
	i++ // skip '['

	type replacement struct {
		start, end int
		newVal     []byte
	}
	var reps []replacement

	for i < arrEnd {
		i = SkipJSONWS(raw, i)
		if i >= arrEnd || raw[i] == ']' {
			break
		}
		if raw[i] == ',' {
			i++
			continue
		}
		// Each element should be a JSON object; skip anything else.
		if raw[i] != '{' {
			i, ok = SkipJSONValue(raw, i)
			if !ok {
				break
			}
			continue
		}

		// Scan the message object for a "role" key.
		i++ // skip '{'
		for i < arrEnd {
			i = SkipJSONWS(raw, i)
			if i >= arrEnd {
				break
			}
			if raw[i] == '}' {
				i++
				break
			}
			if raw[i] == ',' {
				i++
				continue
			}
			if raw[i] != '"' {
				break // malformed
			}
			keyStart := i
			i, ok = SkipJSONString(raw, i)
			if !ok {
				break
			}
			isRole := bytes.Equal(raw[keyStart:i], roleKeyLiteral)
			i = SkipJSONWS(raw, i)
			if i >= arrEnd || raw[i] != ':' {
				break
			}
			i = SkipJSONWS(raw, i+1)
			valStart := i
			i, ok = SkipJSONValue(raw, i)
			if !ok {
				break
			}
			if isRole {
				// The value should be a JSON string; unquote and look up.
				var roleStr string
				if err := json.Unmarshal(raw[valStart:i], &roleStr); err == nil {
					if newRole, exists := roleMap[roleStr]; exists {
						nv, _ := MarshalNoEscape(newRole)
						if !bytes.Equal(raw[valStart:i], nv) {
							reps = append(reps, replacement{valStart, i, nv})
						}
					}
				}
			}
		}
	}

	if len(reps) == 0 {
		return raw, nil
	}

	// Apply replacements via forward splice (offsets are absolute in raw).
	out := make([]byte, 0, len(raw))
	prev := 0
	for _, r := range reps {
		out = append(out, raw[prev:r.start]...)
		out = append(out, r.newVal...)
		prev = r.end
	}
	return append(out, raw[prev:]...), nil
}
