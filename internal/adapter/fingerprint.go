// Ver 2026-07-24 12:00, by Sonnet 5

package adapter

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
)

var systemKeyLiteral = []byte(`"system"`)

// SessionFingerprint locates the system prompt (if any) and the first
// non-system message in raw, and returns the md5 of each as separate
// values. This is Sticky Model's own extraction (see
// docs/VirtualModelRouter_System_Design_v3.md §6.5) — a
// standalone byte-range scan, no full unmarshal. It is deliberately NOT
// shared with internal/report/session.go's offline session-grouping
// anchor: session.go tolerates system-prompt drift within one conversation
// (it tracks that separately as SysChanged rather than splitting the
// group), while Sticky Model's routing decision needs the opposite
// tradeoff — see that doc section for the full reasoning. The two
// implementations are independent on purpose, not an oversight.
//
// ok is false when raw isn't a scannable shape (no top-level "messages"
// array, or the array is empty) — the caller should skip Sticky routing
// for this request, not fail it.
func SessionFingerprint(raw json.RawMessage, protocol string) (sysHash, firstMsgHash [16]byte, ok bool) {
	msgsRanges, msgsOK := topLevelValues(raw, messagesKeyLiteral)
	if !msgsOK || len(msgsRanges) == 0 {
		return sysHash, firstMsgHash, false
	}
	arrStart, arrEnd := msgsRanges[0][0], msgsRanges[0][1]

	switch protocol {
	case "anthropic":
		// Anthropic's system prompt is an independent top-level field, never
		// an element of "messages" — so messages[0] is always non-system.
		if sysRanges, sok := topLevelValues(raw, systemKeyLiteral); sok && len(sysRanges) > 0 {
			sysHash = md5.Sum(raw[sysRanges[0][0]:sysRanges[0][1]])
		}
		first, fok := firstArrayElement(raw, arrStart, arrEnd)
		if !fok {
			return sysHash, firstMsgHash, false
		}
		return sysHash, md5.Sum(first), true
	case "openai":
		// OpenAI carries system as leading role:"system" entries inside
		// "messages" — walk only far enough to find the first non-system
		// element, so cost is bounded by the leading system block's size,
		// not the conversation's full length.
		sysBytes, firstBytes, fok := leadingSystemAndFirstOther(raw, arrStart, arrEnd)
		if !fok {
			return sysHash, firstMsgHash, false
		}
		if len(sysBytes) > 0 {
			sysHash = md5.Sum(sysBytes)
		}
		return sysHash, md5.Sum(firstBytes), true
	default:
		return sysHash, firstMsgHash, false
	}
}

// walkArrayElements scans the JSON array whose value occupies
// raw[arrStart:arrEnd] (brackets included, as returned by topLevelValues)
// and calls visit once per element with that element's own byte range.
// Stops as soon as visit returns true. ok=false means the array couldn't be
// scanned (not actually an array, or malformed) — distinct from found=false
// (scanned fine, visit never returned true).
func walkArrayElements(raw []byte, arrStart, arrEnd int, visit func(start, end int) bool) (found, ok bool) {
	i := skipJSONWS(raw, arrStart)
	if i >= len(raw) || raw[i] != '[' {
		return false, false
	}
	i++
	for i < arrEnd {
		i = skipJSONWS(raw, i)
		if i >= arrEnd || raw[i] == ']' {
			return false, true
		}
		if raw[i] == ',' {
			i++
			continue
		}
		elemStart := i
		var svOK bool
		i, svOK = skipJSONValue(raw, i)
		if !svOK {
			return false, false
		}
		if visit(elemStart, i) {
			return true, true
		}
	}
	return false, true
}

// firstArrayElement returns the byte range of the array's first element.
func firstArrayElement(raw []byte, arrStart, arrEnd int) ([]byte, bool) {
	var result []byte
	found, ok := walkArrayElements(raw, arrStart, arrEnd, func(s, e int) bool {
		result = raw[s:e]
		return true
	})
	if !ok || !found {
		return nil, false
	}
	return result, true
}

// leadingSystemAndFirstOther walks the messages array from the start,
// concatenating the raw bytes of every leading role:"system" element, and
// returns that plus the byte range of the first element whose role isn't
// "system". Stops at the first non-system element — never walks the rest
// of the (possibly very long) conversation history.
func leadingSystemAndFirstOther(raw []byte, arrStart, arrEnd int) (sysBytes, firstOther []byte, ok bool) {
	var sys []byte
	found, walkOK := walkArrayElements(raw, arrStart, arrEnd, func(s, e int) bool {
		if role, roleOK := elementRole(raw, s, e); roleOK && role == "system" {
			sys = append(sys, raw[s:e]...)
			return false // keep walking past leading system elements
		}
		firstOther = raw[s:e]
		return true // first non-system element found: stop
	})
	if !walkOK || !found {
		return nil, nil, false
	}
	return sys, firstOther, true
}

// elementRole scans one JSON object (raw[elemStart:elemEnd], braces
// included) for a top-level "role" string key, returning its value.
func elementRole(raw []byte, elemStart, elemEnd int) (string, bool) {
	if elemStart >= elemEnd || raw[elemStart] != '{' {
		return "", false
	}
	i := elemStart + 1
	for i < elemEnd {
		i = skipJSONWS(raw, i)
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
		i, ok = skipJSONString(raw, i)
		if !ok {
			return "", false
		}
		isRole := bytes.Equal(raw[keyStart:i], roleKeyLiteral)
		i = skipJSONWS(raw, i)
		if i >= elemEnd || raw[i] != ':' {
			return "", false
		}
		i = skipJSONWS(raw, i+1)
		valStart := i
		i, ok = skipJSONValue(raw, i)
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

// HasNonEmptyTopLevelArray reports whether raw's top-level object has a key
// whose value is a JSON array containing at least one element. Used for
// cheap RequestFacts detection (e.g. "tools") — a structural check, no
// full unmarshal.
func HasNonEmptyTopLevelArray(raw json.RawMessage, key string) bool {
	ranges, ok := topLevelValues(raw, []byte(`"`+key+`"`))
	if !ok || len(ranges) == 0 {
		return false
	}
	start, end := ranges[0][0], ranges[0][1]
	i := skipJSONWS(raw, start)
	if i >= end || raw[i] != '[' {
		return false
	}
	i = skipJSONWS(raw, i+1)
	return i < end && raw[i] != ']'
}
