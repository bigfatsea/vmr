// Ver 2026-08-14, by Sonnet 5
package jsonscan

import (
	"bytes"
	"encoding/json"
)

// roleKeyLiteral is shared by ElementRole (below) and rewrite.go's
// RewriteRoles/RewriteInputRoles — one definition, since both live in this
// package. internal/adapter keeps its own independent copy for the callers
// that stayed there (TopLevelProbe etc.) — these are immutable protocol-field
// literals, not shared state, so the duplication carries no drift risk.
var roleKeyLiteral = []byte(`"role"`)

func SkipJSONWS(b []byte, i int) int {
	for i < len(b) {
		switch b[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

// SkipJSONString advances past the string starting at b[i] (must be '"'),
// returning the index just after the closing quote. A thin wrapper over
// IndexUnescapedQuote — the two only differ in what "start" means (b[i] is
// the opening quote here; IndexUnescapedQuote's b is everything after one).
func SkipJSONString(b []byte, i int) (int, bool) {
	j := IndexUnescapedQuote(b[i+1:])
	if j < 0 {
		return 0, false
	}
	return i + 1 + j + 1, true
}

// IndexUnescapedQuote returns the index of the first unescaped '"' in b, or
// -1 if there isn't one — b holds the bytes right after an opening quote,
// not including it. Escaped quotes are recognized by backslash parity (an
// even run of '\' before the '"' leaves it unescaped: each pair is one
// literal '\' in the string content; an odd run means the last '\' escapes
// the '"'), so the jump-to-next-quote stays at bytes.IndexByte (memchr)
// speed even through multi-MB content strings.
//
// Exported for internal/server/facts.go's estimateDocumentTokens, which
// needs the exact same "find where this JSON string value ends" scan to
// size a base64 document payload for token estimation.
func IndexUnescapedQuote(b []byte) int {
	i := 0
	for {
		j := bytes.IndexByte(b[i:], '"')
		if j < 0 {
			return -1
		}
		k, n := i+j-1, 0
		for k >= 0 && b[k] == '\\' {
			n++
			k--
		}
		if n%2 == 0 {
			return i + j
		}
		i += j + 1
	}
}

// SkipJSONValue advances past one JSON value (string, object, array, number,
// or literal) starting at b[i].
func SkipJSONValue(b []byte, i int) (int, bool) {
	if i >= len(b) {
		return 0, false
	}
	switch b[i] {
	case '"':
		return SkipJSONString(b, i)
	case '{', '[':
		depth := 0
		for i < len(b) {
			switch b[i] {
			case '"':
				var ok bool
				if i, ok = SkipJSONString(b, i); !ok {
					return 0, false
				}
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1, true
				}
			}
			i++
		}
		return 0, false
	default:
		// number / true / false / null: scan to the next structural
		// delimiter. start guards against zero-progress "success": if b[i]
		// is itself already a delimiter, there was no value token here at
		// all (e.g. a stray "," or unmatched "}" where a value was
		// expected) — returning (i, true) unchanged would let every caller
		// here, all of which loop as `i, ok = SkipJSONValue(...); continue`
		// on ok==true, spin forever re-scanning the same byte. Malformed
		// input must fail the scan (ok=false), not stall it.
		start := i
		for i < len(b) {
			switch b[i] {
			case ',', '}', ']', ' ', '\t', '\n', '\r':
				if i == start {
					return 0, false
				}
				return i, true
			}
			i++
		}
		return 0, false
	}
}

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
	i := SkipJSONWS(raw, arrStart)
	if i >= len(raw) || raw[i] != '[' {
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
	if elemStart >= elemEnd || raw[elemStart] != '{' {
		return "", false
	}
	i := elemStart + 1
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
