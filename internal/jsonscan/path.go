// Ver 2026-09-23 08:10, by Claude Opus 5.5
package jsonscan

import "bytes"

// LocateStringValue scans raw as a JSON object and returns the [start,end)
// byte range — quotes included — of the string value at the fixed key path
// path (e.g. "message", "model"). Each intermediate path element must
// resolve to a JSON object; the final element must resolve to a JSON string.
// Duplicate keys follow the last occurrence — the value a standard JSON
// decoder hands back for that key. ok=false means the path did not resolve:
// raw (or an intermediate value) is not a JSON object, a key is absent, the
// final value is not a string, or the bytes are malformed/truncated. It is
// a read-only locator — no bytes are modified — and an empty path never
// matches.
func LocateStringValue(raw []byte, path ...string) (start, end int, ok bool) {
	if len(path) == 0 {
		return 0, 0, false
	}
	objStart, objEnd := 0, len(raw)
	for depth := 0; depth < len(path); depth++ {
		vs, ve, found := objectValueRange(raw, objStart, objEnd, []byte(path[depth]))
		if !found {
			return 0, 0, false
		}
		if depth == len(path)-1 {
			if raw[vs] != '"' {
				return 0, 0, false
			}
			return vs, ve, true
		}
		if raw[vs] != '{' {
			return 0, 0, false
		}
		objStart, objEnd = vs, ve
	}
	return 0, 0, false
}

// objectValueRange scans raw[start:end] as a JSON object (brackets included)
// and returns the [start,end) range of the value of the LAST top-level key
// matching key. The whole object is walked rather than stopping at the first
// match so duplicate-key semantics match a standard decoder. ok=false when
// the window is not a scannable object, the key is absent, or the object is
// truncated before its closing brace.
func objectValueRange(raw []byte, start, end int, key []byte) (vs, ve int, ok bool) {
	if start < 0 || end > len(raw) || start >= end {
		return 0, 0, false
	}
	// Key comparison is against the quoted form raw carries (the package's
	// keyLiteral convention, e.g. `"model"`), built once from the unquoted
	// path element.
	quotedKey := make([]byte, 0, len(key)+2)
	quotedKey = append(quotedKey, '"')
	quotedKey = append(quotedKey, key...)
	quotedKey = append(quotedKey, '"')
	i := SkipJSONWS(raw, start)
	if i >= end || raw[i] != '{' {
		return 0, 0, false
	}
	i++
	for {
		i = SkipJSONWS(raw, i)
		if i >= end {
			// Truncated object, even when the cutoff falls exactly on a
			// value boundary: decline, so a mid-event SSE tail passes
			// through unmodified however it happened to be cut.
			return 0, 0, false
		}
		switch raw[i] {
		case '}':
			return vs, ve, ok
		case ',':
			i++
			continue
		case '"':
			// key follows
		default:
			return 0, 0, false
		}
		keyStart := i
		var okStr bool
		i, okStr = SkipJSONString(raw, i)
		if !okStr {
			return 0, 0, false
		}
		isMatch := bytes.Equal(raw[keyStart:i], quotedKey)
		i = SkipJSONWS(raw, i)
		if i >= end || raw[i] != ':' {
			return 0, 0, false
		}
		i = SkipJSONWS(raw, i+1)
		valStart := i
		var okVal bool
		i, okVal = SkipJSONValue(raw, i)
		if !okVal || i > end {
			return 0, 0, false
		}
		if isMatch {
			vs, ve, ok = valStart, i, true
		}
	}
}
