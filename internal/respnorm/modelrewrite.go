// Ver 2026-09-23 12:05, by pi
//
// The SSE model rewrite, limited to each ingress protocol's KNOWN
// model-field locations — the only byte positions a compliant stream
// carries the upstream model name:
//
//	openai-completions: the chunk's top-level "model"
//	anthropic-messages: "message"."model" on the message_start event
//	openai-responses:   "response"."model" on the response.* lifecycle events
//
// Everything else in an event — including a nested "model" key inside a
// delta, a tool call, or a vendor extension — is client-visible content,
// not protocol framing, and is never touched. Location is structural
// (jsonscan's byte scanner over the data: payload), not a substring regex:
// a "model" merely mentioned inside a JSON string value cannot match, and
// a nested field cannot be confused with the known path.
package respnorm

import (
	"bytes"
	"encoding/json"

	"vmr/internal/core"
	"vmr/internal/jsonscan"
)

// modelKeyMarker gates the per-payload structural scan: a payload without
// the literal "model" substring cannot carry the field at any known path.
// Own copy, per the jsonscan/adapter boundary convention — these are
// immutable protocol-field literals, not shared state.
var modelKeyMarker = []byte(`"model"`)

var donePayloadLiteral = []byte("[DONE]")

// modelValueRange locates the model string value (quotes included) in one
// data: payload per the protocol's known path. ok=false when the protocol
// has no known path or the path does not resolve in this payload — there is
// nothing to rewrite there, which is the normal case for delta events.
func modelValueRange(protocol string, payload []byte) (start, end int, ok bool) {
	switch protocol {
	case core.ProtocolOpenAICompletions:
		return jsonscan.LocateStringValue(payload, "model")
	case core.ProtocolAnthropicMessages:
		return jsonscan.LocateStringValue(payload, "message", "model")
	case core.ProtocolOpenAIResponses:
		return jsonscan.LocateStringValue(payload, "response", "model")
	}
	return 0, 0, false
}

// rewriteSSEModels rewrites the model field at the protocol's known path in
// every data: payload of one SSE block, splicing the virtual name into the
// original bytes; every other byte of the block is preserved. Returns the
// (possibly new) block bytes and whether anything was rewritten.
//
// Each data: line is one complete JSON payload in every upstream this
// package targets; a payload split across multiple data: lines (legal SSE,
// never observed in practice) or truncated by a mid-event EOF declines the
// scan and passes through unmodified — fail-open to "unmodified" on any
// doubt, per the package's byte-fidelity contract. The upstream's own model
// value is captured (once per response) from the same located range, before
// the splice overwrites it — see ObservedModel.
func (s *stream) rewriteSSEModels(block []byte) ([]byte, bool) {
	if !bytes.Contains(block, modelKeyMarker) {
		return block, false
	}
	mv, err := jsonscan.MarshalNoEscape(s.clientModel)
	if err != nil {
		return block, false
	}
	var reps [][2]int
	for off := 0; off < len(block); {
		line := block[off:]
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		if payload, ok := dataPayload(line); ok && !bytes.Equal(bytes.TrimSpace(payload), donePayloadLiteral) {
			if bytes.Contains(payload, modelKeyMarker) {
				if vs, ve, ok := modelValueRange(s.protocol, payload); ok {
					s.noteModelValue(payload[vs:ve])
					if !bytes.Equal(payload[vs:ve], mv) {
						pOff := off + len(line) - len(payload)
						reps = append(reps, [2]int{pOff + vs, pOff + ve})
					}
				}
			}
		}
		off += len(line) + 1
	}
	if len(reps) == 0 {
		return block, false
	}
	return spliceRanges(block, reps, mv), true
}

// hasDataLine reports whether b contains at least one SSE data: line —
// i.e. the body is actually SSE-framed. Used to route the buffered whole-
// body pass: an SSE-declared stream that never showed a data: line (tiny or
// malformed) falls back to the whole-object rewrite instead.
func hasDataLine(b []byte) bool {
	for off := 0; off < len(b); {
		line := b[off:]
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		if _, ok := dataPayload(line); ok {
			return true
		}
		off += len(line) + 1
	}
	return false
}

// dataPayload returns the value bytes of an SSE "data:" line — the single
// optional leading space after the colon stripped, per the SSE grammar;
// a trailing \r from CRLF framing stays, harmlessly outside the JSON
// object. ok=false for any other line kind (event:/id:/retry:/comment).
func dataPayload(line []byte) ([]byte, bool) {
	if !bytes.HasPrefix(line, []byte("data:")) {
		return nil, false
	}
	p := line[5:]
	if len(p) > 0 && p[0] == ' ' {
		p = p[1:]
	}
	return p, true
}

// noteModelValue records the upstream's own model string value (quotes
// included) the first time one is seen — captured before the rewrite
// overwrites it, the only moment the information exists. Deliberately
// records the RAW value and no verdict: telling an alias apart from a
// silent downgrade is an aggregate judgment for vmr analyze, not a
// per-request heuristic (see ObservedModel's contract on NormalizerStream).
func (s *stream) noteModelValue(quoted []byte) {
	if s.modelSeen || s.upstreamModel == "" {
		return
	}
	var got string
	if len(quoted) >= 2 && quoted[0] == '"' && quoted[len(quoted)-1] == '"' {
		if bytes.IndexByte(quoted, '\\') < 0 {
			got = string(quoted[1 : len(quoted)-1])
		} else if err := json.Unmarshal(quoted, &got); err != nil {
			got = string(quoted[1 : len(quoted)-1])
		}
	}
	s.modelSeen = true
	if got != s.upstreamModel {
		s.mu.Lock()
		s.observedModel = got
		s.mu.Unlock()
	}
}

// spliceRanges replaces every ordered, non-overlapping [start,end) range in
// raw with newVal, preserving every other byte (ranges are produced in scan
// order, one per data: payload at most).
func spliceRanges(raw []byte, ranges [][2]int, newVal []byte) []byte {
	extra := 0
	for _, r := range ranges {
		extra += len(newVal) - (r[1] - r[0])
	}
	out := make([]byte, 0, len(raw)+extra)
	prev := 0
	for _, r := range ranges {
		out = append(out, raw[prev:r[0]]...)
		out = append(out, newVal...)
		prev = r[1]
	}
	return append(out, raw[prev:]...)
}
