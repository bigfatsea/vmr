// Ver 2026-07-28 22:10, by Sonnet 5

package chatmsg

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"vmr/internal/core"
	"vmr/internal/tokenutil"
)

// Usage is one record's extracted token usage, split by whether the input
// tokens were served from the provider's prompt cache.
//
// CacheRead is the "cache hit" portion of In (already included in it):
// Anthropic's cache_read_input_tokens, OpenAI's
// usage.prompt_tokens_details.cached_tokens, DeepSeek's
// prompt_cache_hit_tokens. CacheWrite is Anthropic-only: tokens newly written
// into the cache on this turn (cache_creation_input_tokens) — billed at a
// premium, not a "hit", but still part of In. In - CacheRead - CacheWrite is
// the fresh (non-cached) portion.
type Usage struct {
	In, Out               int64
	CacheRead, CacheWrite int64
	// Reasoning is the thinking-token portion of Out when the provider
	// reports it (usage.completion_tokens_details.reasoning_tokens); 0 when
	// absent — consumers treat 0 as "not reported".
	Reasoning int64
}

// Fresh returns In - CacheRead - CacheWrite, floored at 0 — the non-cached
// portion of In (see this type's own doc comment). Floored rather than
// left negative: that would require an upstream usage object whose
// reported cache components exceed its reported total, and every consumer
// of this value (quota charging, report/story cache-efficiency metrics)
// needs a non-negative token count. This was independently hand-written at
// four call sites (internal/router/quota.go's tokenCharge,
// internal/report/{cost,sticky}.go, internal/story/render_md.go) before
// being collected here — the one formula this type's doc comment already
// specifies, now backed by one implementation instead of four.
func (u Usage) Fresh() int64 {
	f := u.In - u.CacheRead - u.CacheWrite
	if f < 0 {
		f = 0
	}
	return f
}

// ExtractUsage pulls token usage from a recorded client response body.
// Four shapes are understood (audit stores JSON bodies as objects, SSE
// streams as strings):
//
//	OpenAI JSON:     {"usage":{"prompt_tokens":N,"completion_tokens":N,"prompt_tokens_details":{"cached_tokens":N}}}
//	Anthropic JSON:  {"usage":{"input_tokens":N,"output_tokens":N,"cache_read_input_tokens":N,"cache_creation_input_tokens":N}}
//	OpenAI SSE:      final chunk carries "usage" (when the provider emits it)
//	Anthropic SSE:   message_start carries usage.input_tokens (+ cache fields),
//	                 message_delta carries cumulative usage.output_tokens
//
// Streams without any usage-bearing event yield ok=false — byte counts are
// the fallback measure there.
//
// This is the protocol-unknown form: the rule for whether reported input
// tokens already include the cached portion is guessed from field presence
// (see usageFromObj), which aggregated gateways can make guess wrong in
// both directions. Callers that know the ingress protocol must use
// ExtractUsageWithProtocol instead.
func ExtractUsage(body any) (Usage, bool) {
	return ExtractUsageWithProtocol(body, "")
}

// ExtractUsageWithProtocol is ExtractUsage with the record's ingress
// protocol (core.Protocol* values; "" means unknown, falling back to the
// field-presence guess). The protocol selects the In-additivity rule
// outright — see usageFromObj — instead of leaving it to whether a
// particular gateway happens to emit input_tokens_details.
func ExtractUsageWithProtocol(body any, protocol string) (Usage, bool) {
	var u Usage
	switch b := body.(type) {
	case map[string]any:
		u = mergeUsage(b, u, protocol)
	case string:
		u = MergeUsageWithProtocol([]byte(b), u, protocol)
	}
	return u, u.In > 0 || u.Out > 0
}

// MergeUsageBytes parses usage out of b and folds it into acc, returning the
// merged result — the byte-oriented entry point internal/respnorm needs (a
// normalizer block can be either a complete JSON object body or SSE text,
// depending on which transport mode is in play; see that package's doc
// comment), auto-detecting which shape b is. This and ExtractUsage's
// string case share this one implementation rather than each parsing SSE
// lines independently — the same "one parser, not two" rule this package
// exists to enforce (see CLAUDE.md's chatmsg invariant: it is the one
// shared source of truth for message/usage parsing, so a router-side
// re-implementation can't silently drift from this one).
//
// Protocol-unknown form: see MergeUsageWithProtocol — callers that know the
// ingress protocol (respnorm's stream does) must use that instead.
func MergeUsageBytes(b []byte, acc Usage) Usage {
	return MergeUsageWithProtocol(b, acc, "")
}

// MergeUsageWithProtocol is MergeUsageBytes with the ingress protocol (see
// ExtractUsageWithProtocol): the protocol selects the In-additivity rule
// outright instead of the field-presence guess.
func MergeUsageWithProtocol(b []byte, acc Usage, protocol string) Usage {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var obj map[string]any
		if json.Unmarshal(trimmed, &obj) == nil {
			return mergeUsage(obj, acc, protocol)
		}
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		data, found := strings.CutPrefix(line, "data:")
		if !found {
			continue
		}
		var obj map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(data)), &obj) != nil {
			continue
		}
		acc = mergeUsage(obj, acc, protocol)
	}
	return acc
}

// mergeUsage folds usage found in obj (top-level or under "message", as in
// Anthropic's message_start) into the running totals. The max is taken per
// SIDE, not per field, and the fields of one side move as a GROUP: In,
// CacheRead and CacheWrite are mutually constrained (Anthropic reports
// In = fresh + cacheRead + cacheWrite; Usage.Fresh() depends on it), so
// keeping In from one object and CacheRead from another fabricates a usage
// no upstream ever reported (Fresh would silently absorb the difference).
// Per-side — rather than whole-object — is deliberate: Anthropic's stream
// never puts both sides in one object (message_start / message_delta), so
// an object-level max would discard the In side entirely.
func mergeUsage(obj map[string]any, u Usage, protocol string) Usage {
	for _, holder := range []any{obj["usage"], Nested(obj, "message", "usage")} {
		m, ok := holder.(map[string]any)
		if !ok {
			continue
		}
		o := usageFromObj(m, protocol)
		if o.In > u.In {
			u.In, u.CacheRead, u.CacheWrite = o.In, o.CacheRead, o.CacheWrite
		}
		if o.Out > u.Out {
			u.Out, u.Reasoning = o.Out, o.Reasoning
		}
	}
	return u
}

// usageFromObj reads one usage object. The two provider families disagree on
// whether "total input" already includes cached tokens:
//
// - Anthropic: input_tokens EXCLUDES cache_read/cache_creation — they are
// separate counters, so total In is their sum.
// - OpenAI/DeepSeek: prompt_tokens is already the total; cached_tokens /
// prompt_cache_hit_tokens is a subset carved out for display, not additive.
// - openai-responses reuses Anthropic's input_tokens/output_tokens field
// names but, like Chat Completions and unlike Anthropic, already includes
// cached tokens in that total — it must NOT take the "+ cacheRead +
// cacheWrite" branch.
//
// protocol (a core.Protocol* value; "" = unknown) selects that rule
// outright. The field-presence tell ("does the object carry
// input_tokens_details") is only the FALLBACK for protocol-unknown
// callers: aggregated gateways routinely synthesize or drop details
// objects, which would make the presence guess wrong in both
// directions — and the router's quota metering runs on this same parse, so
// a wrong guess mis-bills real quota, not just a report column.
func usageFromObj(m map[string]any, protocol string) Usage {
	var u Usage
	cacheRead := num(m["cache_read_input_tokens"])
	if v := Nested(m, "prompt_tokens_details", "cached_tokens"); v != nil {
		cacheRead = max(cacheRead, num(v))
	}
	if v := Nested(m, "input_tokens_details", "cached_tokens"); v != nil {
		cacheRead = max(cacheRead, num(v))
	}
	if v, ok := m["prompt_cache_hit_tokens"]; ok {
		cacheRead = max(cacheRead, num(v))
	}
	cacheWrite := num(m["cache_creation_input_tokens"])
	u.CacheRead, u.CacheWrite = cacheRead, cacheWrite

	switch {
	case protocol == core.ProtocolAnthropicMessages:
		u.In = num(m["input_tokens"]) + cacheRead + cacheWrite
	case protocol == core.ProtocolOpenAIResponses:
		u.In = num(m["input_tokens"])
	case Nested(m, "input_tokens_details", "cached_tokens") != nil: // responses-shaped, protocol unknown
		u.In = num(m["input_tokens"])
	case m["input_tokens"] != nil: // anthropic-shaped, protocol unknown
		u.In = num(m["input_tokens"]) + cacheRead + cacheWrite
	default:
		u.In = num(m["prompt_tokens"])
	}
	u.Out = max(num(m["completion_tokens"]), num(m["output_tokens"]))
	u.Reasoning = max(num(Nested(m, "completion_tokens_details", "reasoning_tokens")), num(Nested(m, "output_tokens_details", "reasoning_tokens")))
	return u
}

// BodyRaw normalizes one audit-record body — which the JSONL decoder hands
// back as a string, a map, or nil depending on how it was written — into
// the raw JSON bytes every estimator and parser downstream wants. Lives
// here, next to ExtractUsage (the other function that has to cope with that
// same `any`), because THREE packages need it and the dependency graph only
// admits one: internal/reqdetail imports internal/ctxgraph, so the helper
// could not live in reqdetail and still be reachable from ctxgraph's
// manifest build.
func BodyRaw(body any) []byte {
	switch b := body.(type) {
	case nil:
		return nil
	case string:
		return []byte(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return nil
		}
		return raw
	}
}

// ExtractResponseText extracts the assistant response text (content, reasoning,
// tool calls) from a response body (SSE stream string, non-streaming JSON
// response object/string, or byte slice), excluding SSE envelopes and JSON structure.
// Returns "" when body is nil, empty, or not a recognized response shape.
func ExtractResponseText(body any) string {
	switch b := body.(type) {
	case nil:
		return ""
	case map[string]any:
		if s, ok := FinalMessage(b); ok {
			return summaryText(s)
		}
		return ""
	case string:
		return extractResponseTextFromString(b)
	case []byte:
		return extractResponseTextFromString(string(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return ""
		}
		return extractResponseTextFromString(string(raw))
	}
}

func summaryText(s *StreamSummary) string {
	if s == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(s.Content)
	sb.WriteString(s.Reasoning)
	for _, tc := range s.ToolCalls {
		sb.WriteString(tc.Name)
		sb.WriteString(tc.Args)
	}
	return sb.String()
}

var (
	contentMarker          = []byte(`"content":"`)
	textMarker             = []byte(`"text":"`)
	reasoningContentMarker = []byte(`"reasoning_content":"`)
	thinkingMarker         = []byte(`"thinking":"`)
	deltaTextMarker        = []byte(`"delta":"`)
)

func extractTruncatedText(raw []byte) string {
	var sb strings.Builder
	for _, m := range [][]byte{contentMarker, textMarker, reasoningContentMarker, thinkingMarker, deltaTextMarker} {
		rem := raw
		for {
			i := bytes.Index(rem, m)
			if i < 0 {
				break
			}
			val := rem[i+len(m):]
			end := bytes.IndexByte(val, '"')
			if end < 0 {
				sb.Write(val)
				rem = nil
				break
			}
			sb.Write(val[:end])
			rem = val[end+1:]
		}
	}
	return sb.String()
}

func extractResponseTextFromString(s string) string {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) == 0 {
		return ""
	}
	if strings.Contains(s, "data:") {
		if sum := ReassembleSSE(s); sum != nil {
			return summaryText(sum)
		}
		if t := extractTruncatedText([]byte(s)); len(t) > 0 {
			return t
		}
		return ""
	}
	if trimmed[0] == '{' {
		var obj map[string]any
		if json.Unmarshal([]byte(trimmed), &obj) == nil {
			if sum, ok := FinalMessage(obj); ok {
				return summaryText(sum)
			}
			return ""
		}
		if t := extractTruncatedText([]byte(trimmed)); len(t) > 0 {
			return t
		}
		return ""
	}
	return s
}

// EstimateBodyTokens computes the degraded token estimate for a REQUEST body:
// the actual content/text (message content, reasoning, tool calls) when
// extraction succeeds, the raw request bytes otherwise — the request-side
// branch of the degraded basis rule documented on EstimateDegradedTokens.
func EstimateBodyTokens(body any) int64 {
	text := ExtractResponseText(body)
	if text == "" {
		raw := BodyRaw(body)
		if len(raw) == 0 {
			return 0
		}
		return tokenutil.Estimate(raw)
	}
	return tokenutil.EstimateText(text)
}

// EstimateResponseBodyTokens computes the degraded output-token estimate for
// a recorded response body: extracted assistant text only, 0 when none is
// usable — the response-side branch of the degraded basis rule documented on
// EstimateDegradedTokens. There is deliberately no raw-byte fallback: for
// response shapes whose text extraction fails (truncated SSE, unparseable
// JSON remnants), counting envelope bytes would reintroduce the inflated
// pre-degrade-basis estimate. Opaque (compressed passthrough) responses
// survive the audit JSONL round-trip as strings with U+FFFD runs where
// invalid bytes were replaced; the router charges 0 for them (OutTokens
// returns 0), so this returns 0 rather than estimating over mangled bytes.
// A body legitimately containing U+FFFD in its text is rare and already
// corrupted, so under-counting it to zero is the acceptable side of the
// tradeoff.
func EstimateResponseBodyTokens(body any) int64 {
	switch b := body.(type) {
	case nil:
		return 0
	case string:
		if strings.ContainsRune(b, utf8.RuneError) {
			return 0
		}
	case []byte:
		if !utf8.Valid(b) {
			return 0
		}
	}
	if text := ExtractResponseText(body); text != "" {
		return tokenutil.EstimateText(text)
	}
	return 0
}
