// Ver 2026-08-14, by Sonnet 5
package adapter

import (
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

// contextLimitHint spots a genuine "conversation/prompt exceeds this
// endpoint's context window" rejection across vendors (EN + ZH wording,
// same "lean wide" reasoning as contentHint: a false positive only costs
// one harmless extra failover, a miss dead-ends a failover walk that could
// have succeeded on a candidate with a larger window). Checked AFTER
// maxOutputHint: a request's own max_tokens/output-length parameter being
// set larger than this endpoint allows is a different failure mode —
// switching endpoints can't fix a client-supplied number, so that case
// stays ErrClient (see maxOutputHint's own doc comment).
func contextLimitHint(snippet string) bool {
	if maxOutputHint(snippet) {
		return false
	}
	return containsAny(snippet,
		"context_length_exceeded", "context_window_exceeded",
		"maximum context length", "context window",
		"prompt is too long", "input is too long",
		"reduce the length of the messages",
		"上下文长度", "上下文过长", "超出上下文", "超过最大上下文", "超出最大上下文")
}

// maxOutputHint spots a rejection specifically about the request's own
// max_tokens/output-length parameter exceeding what this endpoint allows —
// a client-supplied number, not the conversation history, so switching
// endpoints can't fix it (another candidate's larger context window is
// irrelevant to an output-length cap). Kept narrow and checked before
// contextLimitHint precisely because vendor wording for this case often
// ALSO mentions "context"/"tokens" in the same sentence (e.g. Anthropic's
// "max_tokens: 100000 > 64000, which is the maximum allowed number of
// output tokens").
func maxOutputHint(snippet string) bool {
	if containsAny(snippet, "max_tokens to sample", "maximum allowed number of output tokens", "max_output_tokens") {
		return true
	}
	return strings.Contains(snippet, "max_tokens") && containsAny(snippet, "completion tokens", "output tokens")
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

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
