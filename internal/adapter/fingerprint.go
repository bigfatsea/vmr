// Ver 2026-07-24 12:00, by Sonnet 5

package adapter

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
)

var (
	systemKeyLiteral       = []byte(`"system"`)
	instructionsKeyLiteral = []byte(`"instructions"`)
)

// SessionFingerprint locates the system prompt (if any) and the first
// non-system message in raw, and returns the md5 of each as separate
// values. This is Sticky Model's own extraction (see
// docs/VirtualModelRouter_Design_v4_Core.md's Sticky Model section) — a
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
	if protocol == "openai-responses" {
		return responsesSessionFingerprint(raw)
	}

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

// responsesSessionFingerprint is SessionFingerprint's Responses-protocol
// case, split out because the shape it scans differs from "messages" in two
// ways at once: the system-prompt equivalent can come from a top-level
// "instructions" string, a leading "input" element (Responses allows
// role:"system" and role:"developer" on message Items, unlike Chat
// Completions' "system" only), or both at once — and "input" itself may be
// a bare string rather than an array. Both signals are folded into one
// sysHash (concatenated, then hashed once) since either can legitimately
// carry the system-equivalent instructions for a given client, and a Sticky
// Model anchor needs both to agree for two turns to fingerprint identically.
func responsesSessionFingerprint(raw json.RawMessage) (sysHash, firstMsgHash [16]byte, ok bool) {
	var sysBytes []byte
	if instrRanges, iok := topLevelValues(raw, instructionsKeyLiteral); iok && len(instrRanges) > 0 {
		sysBytes = append(sysBytes, raw[instrRanges[0][0]:instrRanges[0][1]]...)
	}

	inputRanges, iok := topLevelValues(raw, inputKeyLiteral)
	if !iok || len(inputRanges) == 0 {
		return sysHash, firstMsgHash, false
	}
	inStart, inEnd := inputRanges[0][0], inputRanges[0][1]
	j := skipJSONWS(raw, inStart)
	if j >= inEnd {
		return sysHash, firstMsgHash, false
	}
	if raw[j] == '"' {
		// input is a bare string (no message array at all): the whole value
		// is the first-message anchor; only "instructions" can contribute a
		// system-equivalent signal in this shape.
		if len(sysBytes) > 0 {
			sysHash = md5.Sum(sysBytes)
		}
		return sysHash, md5.Sum(raw[inStart:inEnd]), true
	}

	// input is an array of Items: walk past any leading role:"system"/
	// role:"developer" message Items (folded into sysBytes alongside
	// "instructions" above) to find the first Item that isn't one — same
	// bounded-cost walk as the "openai" case's leadingSystemAndFirstOther,
	// generalized to Responses' extra "developer" role.
	sys2, firstBytes, fok := leadingSystemAndFirstOtherResponses(raw, inStart, inEnd)
	if !fok {
		return sysHash, firstMsgHash, false
	}
	sysBytes = append(sysBytes, sys2...)
	if len(sysBytes) > 0 {
		sysHash = md5.Sum(sysBytes)
	}
	return sysHash, md5.Sum(firstBytes), true
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

// leadingSystemAndFirstOtherResponses is leadingSystemAndFirstOther's
// Responses-protocol counterpart: same bounded leading-role walk, but a
// Responses message Item's role can be "system" OR "developer" (the SDK's
// Message type allows both — OpenAI's Chat Completions-only "developer"
// role concept carries over here as a first-class input role, not just a
// role_map rewrite target), so both count as "leading system-equivalent"
// for sysHash purposes. A non-message Item (function_call, reasoning, …)
// has no "role" key at all — elementRole reports roleOK=false for it, which
// this treats the same as a role that isn't system/developer: it becomes
// the first-message anchor, stopping the walk.
func leadingSystemAndFirstOtherResponses(raw []byte, arrStart, arrEnd int) (sysBytes, firstOther []byte, ok bool) {
	var sys []byte
	found, walkOK := walkArrayElements(raw, arrStart, arrEnd, func(s, e int) bool {
		if role, roleOK := elementRole(raw, s, e); roleOK && (role == "system" || role == "developer") {
			sys = append(sys, raw[s:e]...)
			return false // keep walking past leading system/developer elements
		}
		firstOther = raw[s:e]
		return true // first other element found: stop
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

var toolsKeyLiteral = []byte(`"tools"`)

// TopLevelProbe extracts the three top-level fields server.go's ingress path
// needs before routing — model, stream, and whether "tools" is a non-empty
// array — in one structural scan over raw, replacing what used to be a
// reflective json.Unmarshal into a 2-field struct (for model/stream) plus a
// second, independent top-level array-non-empty scan (for tools).
//
// ok=false exactly where json.Unmarshal(raw, &struct{Model string; Stream
// bool}{}) would have errored: raw isn't a JSON object, is malformed, or
// either field is present with a value that isn't its expected JSON type
// (mirroring encoding/json's behavior, a JSON null for either field is a
// no-op, not an error — same as unmarshaling null into a string/bool
// struct field leaves it at its zero value). Unrecognized top-level keys
// (including a malformed "tools" value only matters if it makes the whole
// document unparsable, in which case ok=false here exactly as the old
// whole-body json.Unmarshal would have failed too) are skipped, matching
// encoding/json's default "ignore unknown fields" behavior. Duplicate
// top-level keys resolve last-write-wins, same as encoding/json.
func TopLevelProbe(raw json.RawMessage) (model string, stream bool, hasTools bool, ok bool) {
	i := skipJSONWS(raw, 0)
	if i >= len(raw) || raw[i] != '{' {
		return "", false, false, false
	}
	i++
	for {
		i = skipJSONWS(raw, i)
		if i >= len(raw) {
			return "", false, false, false
		}
		switch raw[i] {
		case '}':
			return model, stream, hasTools, true
		case ',':
			i++
			continue
		case '"':
			// key follows
		default:
			return "", false, false, false
		}
		keyStart := i
		var kok bool
		i, kok = skipJSONString(raw, i)
		if !kok {
			return "", false, false, false
		}
		key := raw[keyStart:i]
		i = skipJSONWS(raw, i)
		if i >= len(raw) || raw[i] != ':' {
			return "", false, false, false
		}
		i = skipJSONWS(raw, i+1)
		valStart := i
		i, kok = skipJSONValue(raw, i)
		if !kok {
			return "", false, false, false
		}
		val := raw[valStart:i]
		switch {
		case bytes.Equal(key, modelKeyLiteral):
			if bytes.Equal(val, nullLiteral) {
				continue // JSON null: no-op, same as unmarshaling null into a string field
			}
			if err := json.Unmarshal(val, &model); err != nil {
				return "", false, false, false
			}
		case bytes.Equal(key, streamKeyLiteral):
			switch {
			case bytes.Equal(val, nullLiteral):
				// no-op, same as unmarshaling null into a bool field
			case bytes.Equal(val, trueLiteral):
				stream = true
			case bytes.Equal(val, falseLiteral):
				stream = false
			default:
				return "", false, false, false
			}
		case bytes.Equal(key, toolsKeyLiteral):
			vi := skipJSONWS(val, 0)
			hasTools = vi < len(val) && val[vi] == '['
			if hasTools {
				vi = skipJSONWS(val, vi+1)
				hasTools = vi < len(val) && val[vi] != ']'
			}
		}
	}
}

var (
	nullLiteral  = []byte("null")
	trueLiteral  = []byte("true")
	falseLiteral = []byte("false")
)
