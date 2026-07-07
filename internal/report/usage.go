// Ver 2026-07-07, by Fable 5
package report

import (
	"encoding/json"
	"strings"
)

// ExtractUsage pulls token usage from a recorded client response body.
// Four shapes are understood (audit stores JSON bodies as objects, SSE
// streams as strings):
//
//	OpenAI JSON:     {"usage":{"prompt_tokens":N,"completion_tokens":N}}
//	Anthropic JSON:  {"usage":{"input_tokens":N,"output_tokens":N}}
//	OpenAI SSE:      final chunk carries "usage" (when the provider emits it)
//	Anthropic SSE:   message_start carries usage.input_tokens,
//	                 message_delta carries cumulative usage.output_tokens
//
// Streams without any usage-bearing event yield ok=false — byte counts are
// the fallback measure there.
func ExtractUsage(body any) (in, out int64, ok bool) {
	switch b := body.(type) {
	case map[string]any:
		in, out = mergeUsage(b, 0, 0)
	case string:
		for _, line := range strings.Split(b, "\n") {
			line = strings.TrimSpace(line)
			data, found := strings.CutPrefix(line, "data:")
			if !found {
				continue
			}
			var obj map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(data)), &obj) != nil {
				continue
			}
			in, out = mergeUsage(obj, in, out)
		}
	}
	return in, out, in > 0 || out > 0
}

// mergeUsage folds usage found in obj (top-level or under "message", as in
// Anthropic's message_start) into the running totals, keeping the maximum
// seen per field — robust for cumulative streams and single final objects.
func mergeUsage(obj map[string]any, in, out int64) (int64, int64) {
	for _, holder := range []any{obj["usage"], nested(obj, "message", "usage")} {
		u, ok := holder.(map[string]any)
		if !ok {
			continue
		}
		in = max(in, num(u["prompt_tokens"]), num(u["input_tokens"]))
		out = max(out, num(u["completion_tokens"]), num(u["output_tokens"]))
	}
	return in, out
}

func nested(obj map[string]any, keys ...string) any {
	var cur any = obj
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

func num(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}
