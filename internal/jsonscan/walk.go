// Ver 2026-08-15, by Opus 5
//
// Structural walkers: the layer above scan.go's byte-level primitives.
// Everything here takes a window into a buffer and hands back byte ranges of
// the values inside it, deferring every byte-level decision to scan.go.
package jsonscan

import (
	"bytes"
	"encoding/json"
)

// roleKeyLiteral is shared by ElementRole below and rewrite.go's
// RewriteRoles/RewriteInputRoles — one definition, since both live in this
// package. internal/adapter keeps its own independent copy for the callers
// that stayed there (TopLevelProbe etc.) — these are immutable protocol-field
// literals, not shared state, so the duplication carries no drift risk.
var roleKeyLiteral = []byte(`"role"`)

// TopLevelValues scans raw as a JSON object and returns the [start,end)
// byte range of the value of every top-level key matching keyLiteral (the
// quoted key, e.g. `"model"`). Duplicate keys are pathological but legal
// JSON — rewriting all of them keeps the guarantee regardless of which
// duplicate the upstream honors. ok=false means the scanner declined (not
// an object, or malformed) and the caller should use the generic path; it
// is not a validation verdict.
func TopLevelValues(raw, keyLiteral []byte) (ranges [][2]int, ok bool) {
	i := SkipJSONWS(raw, 0)
	if i >= len(raw) || raw[i] != '{' {
		return nil, false
	}
	i++
	for {
		i = SkipJSONWS(raw, i)
		if i >= len(raw) {
			return nil, false
		}
		switch raw[i] {
		case '}':
			return ranges, true
		case ',':
			i++
			continue
		case '"':
			// key follows
		default:
			return nil, false
		}
		keyStart := i
		i, ok = SkipJSONString(raw, i)
		if !ok {
			return nil, false
		}
		isMatch := bytes.Equal(raw[keyStart:i], keyLiteral)
		i = SkipJSONWS(raw, i)
		if i >= len(raw) || raw[i] != ':' {
			return nil, false
		}
		i = SkipJSONWS(raw, i+1)
		valStart := i
		i, ok = SkipJSONValue(raw, i)
		if !ok {
			return nil, false
		}
		if isMatch {
			ranges = append(ranges, [2]int{valStart, i})
		}
	}
}

// WalkArrayElements scans the JSON array whose value occupies
// raw[arrStart:arrEnd] (brackets included, as returned by TopLevelValues)
// and calls visit once per element with that element's own byte range.
// Stops as soon as visit returns true. ok=false means the array couldn't be
// scanned (not actually an array, or malformed) — distinct from found=false
// (scanned fine, visit never returned true).
func WalkArrayElements(raw []byte, arrStart, arrEnd int, visit func(start, end int) bool) (found, ok bool) {
	if !inRange(raw, arrStart, arrEnd) {
		return false, false
	}
	i := SkipJSONWS(raw, arrStart)
	if i >= arrEnd || raw[i] != '[' {
		return false, false
	}
	i++
	for i < arrEnd {
		i = SkipJSONWS(raw, i)
		if i >= arrEnd || raw[i] == ']' {
			return false, true
		}
		if raw[i] == ',' {
			i++
			continue
		}
		elemStart := i
		var svOK bool
		i, svOK = SkipJSONValue(raw, i)
		if !svOK {
			return false, false
		}
		if visit(elemStart, i) {
			return true, true
		}
	}
	return false, true
}

// FirstArrayElement returns the byte range of the array's first element.
func FirstArrayElement(raw []byte, arrStart, arrEnd int) ([]byte, bool) {
	var result []byte
	found, ok := WalkArrayElements(raw, arrStart, arrEnd, func(s, e int) bool {
		result = raw[s:e]
		return true
	})
	if !ok || !found {
		return nil, false
	}
	return result, true
}

// ElementRole scans one JSON object (raw[elemStart:elemEnd], braces
// included) for a top-level "role" string key, returning its value.
func ElementRole(raw []byte, elemStart, elemEnd int) (string, bool) {
	if !inRange(raw, elemStart, elemEnd) {
		return "", false
	}
	i := SkipJSONWS(raw, elemStart)
	if i >= elemEnd || raw[i] != '{' {
		return "", false
	}
	i++
	for i < elemEnd {
		i = SkipJSONWS(raw, i)
		if i >= elemEnd || raw[i] == '}' {
			return "", false
		}
		if raw[i] == ',' {
			i++
			continue
		}
		if raw[i] != '"' {
			return "", false
		}
		keyStart := i
		var ok bool
		i, ok = SkipJSONString(raw, i)
		if !ok {
			return "", false
		}
		isRole := bytes.Equal(raw[keyStart:i], roleKeyLiteral)
		i = SkipJSONWS(raw, i)
		if i >= elemEnd || raw[i] != ':' {
			return "", false
		}
		i = SkipJSONWS(raw, i+1)
		valStart := i
		i, ok = SkipJSONValue(raw, i)
		if !ok {
			return "", false
		}
		if isRole {
			var roleStr string
			if err := json.Unmarshal(raw[valStart:i], &roleStr); err != nil {
				return "", false
			}
			return roleStr, true
		}
	}
	return "", false
}
