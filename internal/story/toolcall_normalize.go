// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

package story

import (
	"encoding/json"
	"strings"
)

// canonicalizeToolArgs normalizes tool call arguments for equivalence comparison:
// 1. If valid JSON object or array, parses and marshals it back, sorting object keys in lexicographical order.
// 2. Otherwise, trims and normalizes internal whitespace sequences.
func canonicalizeToolArgs(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
		// UseNumber keeps integers as their original digit string instead of
		// decoding through float64, which loses precision past 2^53 — tool
		// args routinely carry nanosecond timestamps or large IDs in that
		// range, and two genuinely different values would otherwise both
		// round to the same float64 and canonicalize to the same key.
		dec := json.NewDecoder(strings.NewReader(trimmed))
		dec.UseNumber()
		var parsed any
		if err := dec.Decode(&parsed); err == nil {
			if canonical, err := json.Marshal(parsed); err == nil {
				return string(canonical)
			}
		}
	}

	return strings.Join(strings.Fields(trimmed), " ")
}
