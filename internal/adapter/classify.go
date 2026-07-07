// Ver 2026-07-07, by Fable 5
package adapter

import (
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
func DefaultClassify(status int, body []byte) core.ErrorClass {
	snippet := strings.ToLower(string(body[:min(len(body), 2048)]))
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
		return core.ErrClient
	default:
		return core.ErrTransient
	}
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

// RewriteModel replaces only the "model" key, keeping every other field's raw
// bytes intact so unknown upstream parameters stay forward-compatible.
// (JSON key order is not preserved; that carries no semantics.)
func RewriteModel(raw json.RawMessage, model string) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	mv, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	m["model"] = mv
	return json.Marshal(m)
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
