// Ver 2026-09-23 08:10, by Claude Opus 5.5
package jsonscan

import "bytes"

// TopLevelValuesPrefix scans raw as the beginning of a JSON object and returns
// the [start,end) byte ranges of top-level keys matching keyLiteral that were
// fully scanned before any truncation or syntax error. Used when a response
// stream was truncated mid-body and we need to locate top-level keys that
// arrived cleanly before the cutoff. On a complete object ok=true even with
// no match (ranges empty); on a truncated or malformed prefix, ok=true iff at
// least one matching key was completely scanned. Callers test len(ranges).
func TopLevelValuesPrefix(raw, keyLiteral []byte) (ranges [][2]int, ok bool) {
	i := SkipJSONWS(raw, 0)
	if i >= len(raw) || raw[i] != '{' {
		return nil, false
	}
	i++
	for {
		i = SkipJSONWS(raw, i)
		if i >= len(raw) {
			return ranges, len(ranges) > 0
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
			return ranges, len(ranges) > 0
		}
		keyStart := i
		var okStr bool
		i, okStr = SkipJSONString(raw, i)
		if !okStr {
			return ranges, len(ranges) > 0
		}
		isMatch := bytes.Equal(raw[keyStart:i], keyLiteral)
		i = SkipJSONWS(raw, i)
		if i >= len(raw) || raw[i] != ':' {
			return ranges, len(ranges) > 0
		}
		i = SkipJSONWS(raw, i+1)
		valStart := i
		var okVal bool
		i, okVal = SkipJSONValue(raw, i)
		if !okVal {
			return ranges, len(ranges) > 0
		}
		if isMatch {
			ranges = append(ranges, [2]int{valStart, i})
		}
	}
}

// ReplaceTopLevelValuePrefix replaces the value of every top-level key matching
// keyLiteral with newVal in raw. It uses TopLevelValuesPrefix to locate the keys,
// so it succeeds even when raw was truncated mid-body after the matching key(s)
// were received cleanly. If no matching top-level key was completely scanned,
// it returns (raw, false). newVal should be pre-encoded JSON bytes (e.g. produced
// by MarshalNoEscape).
func ReplaceTopLevelValuePrefix(raw, keyLiteral, newVal []byte) ([]byte, bool) {
	ranges, ok := TopLevelValuesPrefix(raw, keyLiteral)
	if !ok || len(ranges) == 0 {
		return raw, false
	}
	return spliceValues(raw, ranges, newVal), true
}
