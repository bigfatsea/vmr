// Ver 2026-08-15, by Sonnet 5
package adapter

import "strings"

// authHint spots OAuth-standard error codes (RFC 6749/6750) in a 4xx body:
// protocol-reserved tokens, not free-form error prose, so near-zero false
// positives.
func authHint(snippet string) bool {
	return containsAny(snippet,
		"invalid_grant", "invalid_token", "token has expired")
}

// balanceExhaustedHint spots insufficient balance/quota across non-standard relays
// returning 400/403 instead of 402/429 (EN + ZH wording).
func balanceExhaustedHint(snippet string) bool {
	return containsAny(snippet,
		"insufficient balance", "insufficient credit", "insufficient quota",
		"quota exhausted", "out of credit",
		"余额不足", "额度不足", "账户欠费")
}

// vendorQuirkHint spots rejections caused by vendor-specific protocol
// constraints (DeepSeek thinking-mode requiring reasoning_content passed back
// on every turn, Gemini's thought_signature requirement on tool calls) that
// other candidate endpoints behind the same virtual model do NOT enforce.
// Kept narrow to avoid swallowing genuine malformed-request errors; markers
// are full phrases where a bare field name could appear in unrelated prose.
func vendorQuirkHint(snippet string) bool {
	return containsAny(snippet,
		"thought_signature", "thought-signature", "thought signature",
		"must be passed back")
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
		"prompt is too long", "input is too long", "input token exceed",
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
// Single words that could appear in an echoed user prompt (sensitive, flagged,
// guardrail, inappropriate) are matched as phrases only — the review's Q06
// finding: a vendor echoing a "case-sensitive" or "guardrail" mention back
// would otherwise misclassify a parameter error as content-blocked. Chinese
// single words (敏感, 违规, 合规) are kept as-is: they are far less likely to
// appear by accident in an echoed prompt.
func contentHint(snippet string) bool {
	return containsAny(snippet,
		"content_filter", "content_policy", "content policy", "content management policy",
		"moderation", "was flagged", "flagged_input", "flagged as",
		"blocked by guardrail", "guardrail blocked", "guardrail:",
		"inappropriate content", "exists risk", "content risk", "data_inspection",
		"(1026)", "(1027)",
		"sensitive content", "content sensitive", "sensitive words", "sensitive topic", "sensitive data",
		// Noun+flagged compounds: cover relays that word the block without
		// "was" ("Request flagged", "Content flagged") without reopening the
		// bare-word false-positive trap Q06 closed.
		"request flagged", "content flagged", "input flagged", "message flagged", "prompt flagged",
		"敏感", "违规", "合规")
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
