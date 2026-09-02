// Ver 2026-08-15, by Sonnet 5
package adapter

import (
	"bytes"
	"encoding/json"
	"strings"

	"vmr/internal/core"
)

// DefaultClassify is the error classification shared by the passthrough
// adapters. Provider quirks verified against live APIs and official docs:
// - MiniMax returns 400 (not 404) for an unknown model; its content
// violations carry codes 1026/1027;
// - DeepSeek's Anthropic endpoint words a bad model "The supported API model
// names are …"; its content filter answers 400 with a "risk" message; its
// thinking-mode endpoint rejects tool-call histories missing the previous
// turn's reasoning_content with 400 "must be passed back";
// - OpenRouter reports insufficient credits as 402 and moderation/guardrail
// blocks as 403 with "flagged"/"guardrail" in the body; some providers
// report exhausted quota/balance as 429;
// - Google Gemini rejects multi-turn tool calls lacking thought_signature as
// 400 INVALID_ARGUMENT;
//
// classifySnippetBytes bounds the raw-body sniff used when the error body is
// NOT structured JSON (plain-text gateway errors). Structured bodies are
// matched against their extracted error.message/error.type fields instead —
// see errorSnippet — so this window only ever sees small plain-text bodies.
// 4 KB is generous for those; it stays small because the same body may echo
// the client's own prompt back at us (an "Invalid value for messages[2].content:
// …" 400 that quotes the offending message), and a wide window would let
// ordinary words from that echo trip content keywords (Q06).
const classifySnippetBytes = 4 << 10

// errorSnippet reduces a 4xx body to the text hint matching should actually
// look at. Structured vendor errors are almost always
// {"error":{"message":…,"type":…}} (or a top-level message/type); extracting
// those fields means keywords match the error itself, not whatever verbose
// debug payload the vendor attached around it. When the body isn't JSON (or
// carries no recognizable field), falls back to a bounded raw scan of the
// first classifySnippetBytes — small plain-text errors fit entirely.
func errorSnippet(body []byte) string {
	raw := func() string {
		return strings.ToLower(string(body[:min(len(body), classifySnippetBytes)]))
	}
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return raw()
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &m); err != nil {
		return raw()
	}
	var parts []string
	add := func(v json.RawMessage) {
		var s string
		if err := json.Unmarshal(v, &s); err == nil && s != "" {
			parts = append(parts, s)
		}
	}
	if raw, ok := m["error"]; ok {
		// error may be a string (OpenAI/Anthropic both allow it) or an
		// object with message/type (the common case).
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			parts = append(parts, s)
		} else {
			var obj struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			}
			if err := json.Unmarshal(raw, &obj); err == nil {
				if obj.Message != "" {
					parts = append(parts, obj.Message)
				}
				if obj.Type != "" {
					parts = append(parts, obj.Type)
				}
			}
		}
	}
	add(m["message"])
	add(m["type"])
	if len(parts) == 0 {
		return raw()
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func DefaultClassify(status int, body []byte) core.ErrorClass {
	snippet := errorSnippet(body)
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
		if balanceExhaustedHint(snippet) {
			return core.ErrEndpoint
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
		if balanceExhaustedHint(snippet) || containsAny(snippet, "insufficient", "quota", "balance", "credit") {
			return core.ErrEndpoint
		}
		return core.ErrRateLimit
	case status >= 400 && status < 500:
		// Content flags first: they may mention "model" too, and a flagged
		// request must keep failing over (vendors differ in sensitivity).
		if contentHint(snippet) {
			return core.ErrContent
		}
		// Non-standard relays returning 400 with insufficient balance / quota exhausted.
		if balanceExhaustedHint(snippet) {
			return core.ErrEndpoint
		}
		// OAuth-standard error codes next (RFC 6749/6750): an expired or revoked
		// grant arriving on a non-401 status (e.g. a relay answering its own
		// token-refresh failure with 400 invalid_grant) is an auth problem —
		// long cooldown + switch; mirrors 403's "content before auth" shape.
		if authHint(snippet) {
			return core.ErrAuth
		}
		// Context-window overflow next: OpenAI-shaped context_length_exceeded
		// wording contains "model" ("this model's maximum context length is
		// ..."), so it must be checked before the model-unknown rule below or
		// it would misclassify as ErrEndpoint (still failover-eligible, but
		// the wrong cooldown treatment — ErrEndpoint cools the endpoint down,
		// which is wrong for a static per-model property nothing about this
		// attempt changed).
		if contextLimitHint(snippet) {
			return core.ErrContextLimit
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
		// handing this straight back to the client (motivating case: an
		// opencode.ai relay's "Upstream request failed" dead-ending an
		// otherwise-recoverable failover walk).
		if upstreamHint(snippet) {
			return core.ErrEndpoint
		}
		// Vendor-specific protocol constraints (DeepSeek thinking-mode's
		// reasoning_content pass-back requirement, Gemini's thought_signature
		// requirement on tool-call history) that other endpoints behind the same
		// virtual model do not enforce — the endpoint is healthy, the request
		// history just doesn't fit its rules: failover without health penalty.
		if vendorQuirkHint(snippet) {
			return core.ErrQuirk
		}
		return core.ErrClient
	default:
		return core.ErrTransient
	}
}
