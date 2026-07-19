// Ver 2026-07-16 21:00, by Fable 5
package adapter

import (
	"bytes"
	"encoding/json"
	"strings"

	"vmr/internal/core"
)

// DefaultClassify is the error classification shared by the passthrough
// adapters. Provider quirks verified against live APIs and official docs:
//   - MiniMax returns 400 (not 404) for an unknown model; its content
//     violations carry codes 1026/1027;
//   - DeepSeek's Anthropic endpoint words a bad model "The supported API model
//     names are …"; its content filter answers 400 with a "risk" message;
//   - OpenRouter reports insufficient credits as 402 and moderation/guardrail
//     blocks as 403 with "flagged"/"guardrail" in the body;
//   - some providers report exhausted quota/balance as 429.
//
// classifySnippetBytes bounds the body sniff. Some vendors attach verbose
// debug payloads to 4xx bodies; a marker past the cutoff would misclassify a
// failover-able error as ErrClient (which never fails over), so lean large —
// a 32 KB lowercase+scan is nanosecond-scale and off the happy path.
const classifySnippetBytes = 32 << 10

func DefaultClassify(status int, body []byte) core.ErrorClass {
	snippet := strings.ToLower(string(body[:min(len(body), classifySnippetBytes)]))
	switch {
	case status == 451: // unavailable for legal reasons
		return core.ErrContent
	case status == 401:
		return core.ErrAuth
	case status == 403:
		// OpenRouter uses 403 for moderation flags and guardrail blocks —
		// request-specific, the endpoint itself is healthy.
		if contentHint(snippet) {
			return core.ErrContent
		}
		return core.ErrAuth
	case status == 402 || status == 404:
		// Same content-first rule as 403/429/generic 4xx: a vendor that
		// carries a moderation rejection on one of these codes must fail
		// over without cooling the (healthy) endpoint down.
		if contentHint(snippet) {
			return core.ErrContent
		}
		return core.ErrEndpoint
	case status == 408:
		return core.ErrTransient
	case status == 429:
		if containsAny(snippet, "insufficient", "quota", "balance", "credit") {
			return core.ErrEndpoint
		}
		return core.ErrRateLimit
	case status >= 400 && status < 500:
		// Content flags first: they may mention "model" too, and a flagged
		// request must keep failing over (vendors differ in sensitivity).
		if contentHint(snippet) {
			return core.ErrContent
		}
		// A wrong model name is a per-endpoint config error where switching helps.
		if strings.Contains(snippet, "model") &&
			containsAny(snippet, "unknown", "not found", "not_found", "does not exist", "invalid model", "supported") {
			return core.ErrEndpoint
		}
		// A relay/gateway hop reporting its own forwarding failure — it names
		// the failure as coming from "upstream"/"provider"/"gateway", not from
		// anything about the request (no field name, no "invalid X" wording).
		// This is per-endpoint (that specific hop choking, e.g. on a request
		// size it can't forward), not a request every endpoint would reject
		// identically — switching helps, so keep failing over instead of
		// handing this straight back to the client (see reports/incident-
		// 20260718-console-go-400-failover_Sonnet5.md for the case that
		// motivated this: an opencode.ai relay's "Upstream request failed"
		// silently dead-ended an otherwise-recoverable failover walk).
		if upstreamHint(snippet) {
			return core.ErrEndpoint
		}
		return core.ErrClient
	default:
		return core.ErrTransient
	}
}

// upstreamHint spots a relay/gateway reporting its own forwarding failure
// rather than describing a problem with the request. Deliberately narrow
// (unlike contentHint's "lean wide"): "upstream"/"gateway" alone would also
// match legitimate request-content errors that happen to mention those words,
// so this only fires on phrasing that names the failure as belonging to the
// hop itself.
func upstreamHint(snippet string) bool {
	return containsAny(snippet,
		"upstream request failed", "upstream error", "upstream connect error",
		"error from provider", "bad gateway", "gateway timeout")
}

// contentHint spots content-policy rejections across vendors (EN + ZH wording).
// A false positive only costs one harmless extra failover; a miss either stops
// failover (400) or wrongly cools a healthy endpoint (403) — so lean wide.
func contentHint(snippet string) bool {
	return containsAny(snippet,
		"content_filter", "content_policy", "content policy", "content management policy",
		"moderation", "flagged", "guardrail", "inappropriate",
		"exists risk", "content risk", "data_inspection",
		"(1026)", "(1027)", "sensitive", "敏感", "违规", "合规")
}

// RewriteModel replaces the value of the top-level "model" key by splicing
// the new value into the original bytes: prefix + rewritten value + suffix.
// Every other byte of the request — key order, whitespace, unknown upstream
// parameters — reaches the provider exactly as the client sent it, which is
// as close to direct-connection equivalence as a model rewrite can get. The
// locate step is a single allocation-free scan (strings are skipped at
// bytes.IndexByte speed), replacing the previous full
// unmarshal-into-map + re-serialize round trip that cost a parse and a copy
// of the whole body on every failover attempt.
//
// Nested "model" keys (inside messages, tool schemas, metadata objects) are
// not touched — only top-level values are rewritten. Inputs the scanner
// can't handle (not a JSON object, no literal top-level "model" key, or a
// syntax problem) fall back to the generic parse-and-rebuild path, which
// preserves the historical semantics for those shapes, including adding the
// key when absent.
func RewriteModel(raw json.RawMessage, model string) ([]byte, error) {
	mv, err := core.MarshalNoEscape(model)
	if err != nil {
		return nil, err
	}
	ranges, ok := topLevelValues(raw, modelKeyLiteral)
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
	ranges, ok := topLevelValues(raw, streamKeyLiteral)
	if !ok || len(ranges) == 0 {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		m["stream"] = sv
		return core.MarshalNoEscape(m)
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
	m["model"] = mv
	return core.MarshalNoEscape(m)
}

var (
	modelKeyLiteral    = []byte(`"model"`)
	streamKeyLiteral   = []byte(`"stream"`)
	messagesKeyLiteral = []byte(`"messages"`)
	roleKeyLiteral     = []byte(`"role"`)
)

// topLevelValues scans raw as a JSON object and returns the [start,end)
// byte range of the value of every top-level key matching keyLiteral (the
// quoted key, e.g. `"model"`). Duplicate keys are pathological but legal
// JSON — rewriting all of them keeps the guarantee regardless of which
// duplicate the upstream honors. ok=false means the scanner declined (not
// an object, or malformed) and the caller should use the generic path; it
// is not a validation verdict.
func topLevelValues(raw, keyLiteral []byte) (ranges [][2]int, ok bool) {
	i := skipJSONWS(raw, 0)
	if i >= len(raw) || raw[i] != '{' {
		return nil, false
	}
	i++
	for {
		i = skipJSONWS(raw, i)
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
		i, ok = skipJSONString(raw, i)
		if !ok {
			return nil, false
		}
		isMatch := bytes.Equal(raw[keyStart:i], keyLiteral)
		i = skipJSONWS(raw, i)
		if i >= len(raw) || raw[i] != ':' {
			return nil, false
		}
		i = skipJSONWS(raw, i+1)
		valStart := i
		i, ok = skipJSONValue(raw, i)
		if !ok {
			return nil, false
		}
		if isMatch {
			ranges = append(ranges, [2]int{valStart, i})
		}
	}
}

func skipJSONWS(b []byte, i int) int {
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

// skipJSONString advances past the string starting at b[i] (must be '"'),
// returning the index just after the closing quote. Escaped quotes are
// recognized by backslash parity, so the jump-to-next-quote stays at
// bytes.IndexByte (memchr) speed even through multi-MB content strings.
func skipJSONString(b []byte, i int) (int, bool) {
	i++ // opening quote
	for {
		j := bytes.IndexByte(b[i:], '"')
		if j < 0 {
			return 0, false
		}
		k, n := i+j-1, 0
		for k >= 0 && b[k] == '\\' {
			n++
			k--
		}
		if n%2 == 0 {
			return i + j + 1, true
		}
		i += j + 1
	}
}

// skipJSONValue advances past one JSON value (string, object, array, number,
// or literal) starting at b[i].
func skipJSONValue(b []byte, i int) (int, bool) {
	if i >= len(b) {
		return 0, false
	}
	switch b[i] {
	case '"':
		return skipJSONString(b, i)
	case '{', '[':
		depth := 0
		for i < len(b) {
			switch b[i] {
			case '"':
				var ok bool
				if i, ok = skipJSONString(b, i); !ok {
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
		// number / true / false / null: scan to the next structural delimiter
		for i < len(b) {
			switch b[i] {
			case ',', '}', ']', ' ', '\t', '\n', '\r':
				return i, true
			}
			i++
		}
		return 0, false
	}
}

// RewriteRoles replaces "role" values inside the top-level "messages" array
// according to roleMap (e.g. {"developer":"system"}). Only the string values
// of "role" keys within message objects are rewritten; every other byte —
// key order, whitespace, unknown parameters, message content — is preserved
// exactly as the client sent it, consistent with RewriteModel's byte-splice
// philosophy.
//
// The scan is JSON-aware: it descends into the top-level "messages" array,
// visits each element object, and checks for a "role" key. This avoids false
// positives from the string "developer" appearing in message content (JSON
// string escaping ensures unescaped "role":"developer" only occurs as a
// key-value pair, but the scanner also skips string values correctly). An
// empty roleMap returns the input unchanged (zero-copy).
func RewriteRoles(raw json.RawMessage, roleMap map[string]string) ([]byte, error) {
	if len(roleMap) == 0 {
		return raw, nil
	}

	// Locate the top-level "messages" value.
	msgRanges, ok := topLevelValues(raw, messagesKeyLiteral)
	if !ok || len(msgRanges) == 0 {
		return raw, nil // not a JSON object or no messages key
	}

	arrStart, arrEnd := msgRanges[0][0], msgRanges[0][1]
	i := skipJSONWS(raw, arrStart)
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
		i = skipJSONWS(raw, i)
		if i >= arrEnd || raw[i] == ']' {
			break
		}
		if raw[i] == ',' {
			i++
			continue
		}
		// Each element should be a JSON object; skip anything else.
		if raw[i] != '{' {
			i, ok = skipJSONValue(raw, i)
			if !ok {
				break
			}
			continue
		}

		// Scan the message object for a "role" key.
		i++ // skip '{'
		for i < arrEnd {
			i = skipJSONWS(raw, i)
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
			i, ok = skipJSONString(raw, i)
			if !ok {
				break
			}
			isRole := bytes.Equal(raw[keyStart:i], roleKeyLiteral)
			i = skipJSONWS(raw, i)
			if i >= arrEnd || raw[i] != ':' {
				break
			}
			i = skipJSONWS(raw, i+1)
			valStart := i
			i, ok = skipJSONValue(raw, i)
			if !ok {
				break
			}
			if isRole {
				// The value should be a JSON string; unquote and look up.
				var roleStr string
				if err := json.Unmarshal(raw[valStart:i], &roleStr); err == nil {
					if newRole, exists := roleMap[roleStr]; exists {
						nv, _ := core.MarshalNoEscape(newRole)
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

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
