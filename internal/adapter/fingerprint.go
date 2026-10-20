// Ver 2026-08-14, by Sonnet 5

package adapter

import (
	"bytes"
	"crypto/md5"
	"encoding/json"

	"vmr/internal/core"
	"vmr/internal/jsonscan"
)

// systemKeyLiteral/instructionsKeyLiteral/modelKeyLiteral/streamKeyLiteral/
// messagesKeyLiteral/inputKeyLiteral are this package's own copies of the
// same protocol-field literals jsonscan's
// RewriteModel/RewriteStream/RewriteRoles/RewriteInputRoles use internally —
// duplicated on purpose rather than exported from jsonscan and imported here:
// they're immutable byte constants, not shared state, and TopLevelProbe/
// SessionFingerprint knowing what "model"/"stream"/"messages"/"input" mean is
// exactly the domain knowledge that keeps these functions in adapter instead
// of jsonscan (see jsonscan's package doc comment for the boundary).
var (
	systemKeyLiteral       = []byte(`"system"`)
	instructionsKeyLiteral = []byte(`"instructions"`)
	modelKeyLiteral        = []byte(`"model"`)
	streamKeyLiteral       = []byte(`"stream"`)
	messagesKeyLiteral     = []byte(`"messages"`)
	inputKeyLiteral        = []byte(`"input"`)
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
	if protocol == core.ProtocolOpenAIResponses {
		return responsesSessionFingerprint(raw)
	}

	msgsRanges, msgsOK := jsonscan.TopLevelValues(raw, messagesKeyLiteral)
	if !msgsOK || len(msgsRanges) == 0 {
		return sysHash, firstMsgHash, false
	}
	arrStart, arrEnd := msgsRanges[0][0], msgsRanges[0][1]

	switch protocol {
	case core.ProtocolAnthropicMessages:
		// Anthropic's system prompt is an independent top-level field, never
		// an element of "messages" — so messages[0] is always non-system.
		if sysRanges, sok := jsonscan.TopLevelValues(raw, systemKeyLiteral); sok && len(sysRanges) > 0 {
			sysHash = md5.Sum(raw[sysRanges[0][0]:sysRanges[0][1]])
		}
		first, fok := jsonscan.FirstArrayElement(raw, arrStart, arrEnd)
		if !fok {
			return sysHash, firstMsgHash, false
		}
		return sysHash, md5.Sum(first), true
	case core.ProtocolOpenAICompletions:
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
	if instrRanges, iok := jsonscan.TopLevelValues(raw, instructionsKeyLiteral); iok && len(instrRanges) > 0 {
		sysBytes = append(sysBytes, raw[instrRanges[0][0]:instrRanges[0][1]]...)
	}

	inputRanges, iok := jsonscan.TopLevelValues(raw, inputKeyLiteral)
	if !iok || len(inputRanges) == 0 {
		return sysHash, firstMsgHash, false
	}
	inStart, inEnd := inputRanges[0][0], inputRanges[0][1]
	j := jsonscan.SkipJSONWS(raw, inStart)
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
	// bounded-cost walk as the openai-completions case's leadingSystemAndFirstOther,
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

// leadingSystemAndFirstOther walks the messages array from the start,
// concatenating the raw bytes of every leading role:"system" element, and
// returns that plus the byte range of the first element whose role isn't
// "system". Stops at the first non-system element — never walks the rest
// of the (possibly very long) conversation history.
func leadingSystemAndFirstOther(raw []byte, arrStart, arrEnd int) (sysBytes, firstOther []byte, ok bool) {
	var sys []byte
	found, walkOK := jsonscan.WalkArrayElements(raw, arrStart, arrEnd, func(s, e int) bool {
		if role, roleOK := jsonscan.ElementRole(raw, s, e); roleOK && role == "system" {
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
// has no "role" key at all — jsonscan.ElementRole reports roleOK=false for
// it, which this treats the same as a role that isn't system/developer: it
// becomes the first-message anchor, stopping the walk.
func leadingSystemAndFirstOtherResponses(raw []byte, arrStart, arrEnd int) (sysBytes, firstOther []byte, ok bool) {
	var sys []byte
	found, walkOK := jsonscan.WalkArrayElements(raw, arrStart, arrEnd, func(s, e int) bool {
		if role, roleOK := jsonscan.ElementRole(raw, s, e); roleOK && (role == "system" || role == "developer") {
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

var (
	toolsKeyLiteral           = []byte(`"tools"`)
	thinkingKeyLiteral        = []byte(`"thinking"`)
	reasoningKeyLiteral       = []byte(`"reasoning"`)
	reasoningEffortKeyLiteral = []byte(`"reasoning_effort"`)
	enableThinkingKeyLiteral  = []byte(`"enable_thinking"`)
)

// Probe is what server.go's ingress path needs from the top level of a
// request body before routing.
type Probe struct {
	Model  string
	Stream bool
	// HasTools: "tools" is a non-empty array.
	HasTools bool
	// WantsThinking: the request turns reasoning on through one of the
	// top-level switches clients actually send — anthropic-messages
	// thinking.type enabled/adaptive, openai-responses reasoning.effort,
	// openai-completions reasoning_effort, plus the vendor variants
	// reasoning.enabled (OpenRouter), thinking.type enabled (DeepSeek/GLM)
	// and enable_thinking (Qwen). Only explicit "on" values count, because
	// this feeds a hard Condition: a false positive could empty the
	// candidate set. A conversation history carrying earlier thinking
	// blocks does not count — that is a vendor-quirk concern (ErrQuirk),
	// not a request for reasoning.
	WantsThinking bool
}

// TopLevelProbe extracts the top-level fields of Probe in one structural
// scan over raw, replacing what used to be a reflective json.Unmarshal into
// a 2-field struct (for model/stream) plus a second, independent top-level
// array-non-empty scan (for tools).
//
// ok=false exactly where json.Unmarshal(raw, &struct{Model string; Stream
// bool}{}) would have errored: raw isn't a JSON object, is malformed, or
// either field is present with a value that isn't its expected JSON type
// (mirroring encoding/json's behavior, a JSON null for either field is a
// no-op, not an error — same as unmarshaling null into a string/bool
// struct field leaves it at its zero value). Unrecognized top-level keys,
// and a tools/thinking/reasoning value of an unexpected shape, are skipped
// (only a document that is unparsable as a whole gives ok=false), matching
// encoding/json's default "ignore unknown fields" behavior. Duplicate
// top-level keys resolve last-write-wins, same as encoding/json.
func TopLevelProbe(raw json.RawMessage) (p Probe, ok bool) {
	var thinking, reasoning, effort, enable bool // one per key, so a duplicate key overrides only itself
	i := jsonscan.SkipJSONWS(raw, 0)
	if i >= len(raw) || raw[i] != '{' {
		return Probe{}, false
	}
	i++
	for {
		i = jsonscan.SkipJSONWS(raw, i)
		if i >= len(raw) {
			return Probe{}, false
		}
		switch raw[i] {
		case '}':
			p.WantsThinking = thinking || reasoning || effort || enable
			return p, true
		case ',':
			i++
			continue
		case '"':
			// key follows
		default:
			return Probe{}, false
		}
		keyStart := i
		var kok bool
		i, kok = jsonscan.SkipJSONString(raw, i)
		if !kok {
			return Probe{}, false
		}
		key := raw[keyStart:i]
		i = jsonscan.SkipJSONWS(raw, i)
		if i >= len(raw) || raw[i] != ':' {
			return Probe{}, false
		}
		i = jsonscan.SkipJSONWS(raw, i+1)
		valStart := i
		i, kok = jsonscan.SkipJSONValue(raw, i)
		if !kok {
			return Probe{}, false
		}
		val := raw[valStart:i]
		switch {
		case bytes.Equal(key, modelKeyLiteral):
			if bytes.Equal(val, nullLiteral) {
				continue // JSON null: no-op, same as unmarshaling null into a string field
			}
			if err := json.Unmarshal(val, &p.Model); err != nil {
				return Probe{}, false
			}
		case bytes.Equal(key, streamKeyLiteral):
			switch {
			case bytes.Equal(val, nullLiteral):
				// no-op, same as unmarshaling null into a bool field
			case bytes.Equal(val, trueLiteral):
				p.Stream = true
			case bytes.Equal(val, falseLiteral):
				p.Stream = false
			default:
				return Probe{}, false
			}
		case bytes.Equal(key, toolsKeyLiteral):
			vi := jsonscan.SkipJSONWS(val, 0)
			p.HasTools = vi < len(val) && val[vi] == '['
			if p.HasTools {
				vi = jsonscan.SkipJSONWS(val, vi+1)
				p.HasTools = vi < len(val) && val[vi] != ']'
			}
		case bytes.Equal(key, thinkingKeyLiteral):
			var t struct {
				Type string `json:"type"`
			}
			thinking = json.Unmarshal(val, &t) == nil && (t.Type == "enabled" || t.Type == "adaptive")
		case bytes.Equal(key, reasoningKeyLiteral):
			var r struct {
				Effort  string `json:"effort"`
				Enabled bool   `json:"enabled"`
			}
			reasoning = json.Unmarshal(val, &r) == nil && (effortOn(r.Effort) || r.Enabled)
		case bytes.Equal(key, reasoningEffortKeyLiteral):
			var e string
			effort = json.Unmarshal(val, &e) == nil && effortOn(e)
		case bytes.Equal(key, enableThinkingKeyLiteral):
			enable = bytes.Equal(val, trueLiteral)
		}
	}
}

// effortOn: any effort level except "none" asks for reasoning ("minimal"
// still does on the models that accept it).
func effortOn(e string) bool { return e != "" && e != "none" }

var (
	nullLiteral  = []byte("null")
	trueLiteral  = []byte("true")
	falseLiteral = []byte("false")
)
