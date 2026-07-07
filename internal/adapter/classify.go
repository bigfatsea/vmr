// Ver 2026-07-07, by Fable 5
package adapter

import (
	"encoding/json"
	"strings"

	"vmr/internal/core"
)

// DefaultClassify is the error classification shared by the passthrough
// adapters. Provider quirks verified against live APIs:
//   - MiniMax returns 400 (not 404) for an unknown model;
//   - DeepSeek's Anthropic endpoint words it "The supported API model names are …";
//   - OpenRouter reports insufficient credits as 402;
//   - some providers report exhausted quota/balance as 429.
func DefaultClassify(status int, body []byte) core.ErrorClass {
	snippet := strings.ToLower(string(body[:min(len(body), 2048)]))
	switch {
	case status == 401 || status == 403:
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
