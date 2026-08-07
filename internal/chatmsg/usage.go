// Ver 2026-07-28 22:10, by Sonnet 5

package chatmsg

import (
	"bytes"
	"encoding/json"
	"strings"
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
func ExtractUsage(body any) (Usage, bool) {
	var u Usage
	switch b := body.(type) {
	case map[string]any:
		u = mergeUsage(b, u)
	case string:
		u = MergeUsageBytes([]byte(b), u)
	}
	return u, u.In > 0 || u.Out > 0
}

// MergeUsageBytes parses usage out of b and folds it into acc, returning the
// merged result — the byte-oriented entry point internal/router/response.go
// needs (a respStream block can be either a complete JSON object body or SSE
// text, depending on which transport mode is in play; see respStream's own
// doc comment), auto-detecting which shape b is. This and ExtractUsage's
// string case share this one implementation rather than each parsing SSE
// lines independently — the same "one parser, not two" rule this package
// exists to enforce (see CLAUDE.md's chatmsg invariant: it is the one
// shared source of truth for message/usage parsing, so a router-side
// re-implementation can't silently drift from this one).
func MergeUsageBytes(b []byte, acc Usage) Usage {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var obj map[string]any
		if json.Unmarshal(trimmed, &obj) == nil {
			return mergeUsage(obj, acc)
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
		acc = mergeUsage(obj, acc)
	}
	return acc
}

// mergeUsage folds usage found in obj (top-level or under "message", as in
// Anthropic's message_start) into the running totals, keeping the maximum
// seen per field — robust for cumulative streams and single final objects.
func mergeUsage(obj map[string]any, u Usage) Usage {
	for _, holder := range []any{obj["usage"], Nested(obj, "message", "usage")} {
		m, ok := holder.(map[string]any)
		if !ok {
			continue
		}
		o := usageFromObj(m)
		u.In = max(u.In, o.In)
		u.Out = max(u.Out, o.Out)
		u.CacheRead = max(u.CacheRead, o.CacheRead)
		u.CacheWrite = max(u.CacheWrite, o.CacheWrite)
		u.Reasoning = max(u.Reasoning, o.Reasoning)
	}
	return u
}

// usageFromObj reads one usage object. The two provider families disagree on
// whether "total input" already includes cached tokens:
//
//   - Anthropic: input_tokens EXCLUDES cache_read/cache_creation — they are
//     separate counters, so total In is their sum.
//   - OpenAI/DeepSeek: prompt_tokens is already the total; cached_tokens /
//     prompt_cache_hit_tokens is a subset carved out for display, not additive.
//
// Presence of the "input_tokens" key (Anthropic's field name) selects which
// rule applies.
func usageFromObj(m map[string]any) Usage {
	var u Usage
	cacheRead := num(m["cache_read_input_tokens"])
	if v := Nested(m, "prompt_tokens_details", "cached_tokens"); v != nil {
		cacheRead = max(cacheRead, num(v))
	}
	if v := Nested(m, "input_tokens_details", "cached_tokens"); v != nil { // openai-responses
		cacheRead = max(cacheRead, num(v))
	}
	if v, ok := m["prompt_cache_hit_tokens"]; ok {
		cacheRead = max(cacheRead, num(v))
	}
	cacheWrite := num(m["cache_creation_input_tokens"])
	u.CacheRead, u.CacheWrite = cacheRead, cacheWrite

	// openai-responses reuses Anthropic's input_tokens/output_tokens field
	// names (unlike Chat Completions' prompt_tokens/completion_tokens) but,
	// like Chat Completions and unlike Anthropic, already includes cached
	// tokens in that total rather than counting them separately — so it
	// must NOT take the "+ cacheRead + cacheWrite" branch below. Anthropic's
	// own usage object never carries "input_tokens_details", so checking
	// for that key is what tells the two apart.
	switch {
	case Nested(m, "input_tokens_details", "cached_tokens") != nil: // openai-responses
		u.In = num(m["input_tokens"])
	case m["input_tokens"] != nil: // anthropic
		u.In = num(m["input_tokens"]) + cacheRead + cacheWrite
	default:
		u.In = num(m["prompt_tokens"])
	}
	u.Out = max(num(m["completion_tokens"]), num(m["output_tokens"]))
	u.Reasoning = max(num(Nested(m, "completion_tokens_details", "reasoning_tokens")), num(Nested(m, "output_tokens_details", "reasoning_tokens")))
	return u
}

// ExtractFinish pulls the response's finish_reason (openai) / stop_reason
// (anthropic) from a recorded client response body — JSON object or SSE
// stream. "" when the response never carried one (errors, canceled, or
// mid-stream breaks). Cheap on purpose: SSE lines are JSON-parsed only when
// they mention the field.
func ExtractFinish(body any) string {
	finish := ""
	fromObj := func(obj map[string]any) {
		if choices, _ := obj["choices"].([]any); len(choices) > 0 {
			ch, _ := choices[0].(map[string]any)
			if fr, _ := ch["finish_reason"].(string); fr != "" {
				finish = fr
			}
		}
		if sr, _ := obj["stop_reason"].(string); sr != "" {
			finish = sr
		}
		if sr, _ := Nested(obj, "delta", "stop_reason").(string); sr != "" {
			finish = sr
		}
	}
	switch b := body.(type) {
	case map[string]any:
		fromObj(b)
	case string:
		for _, line := range strings.Split(b, "\n") {
			if !strings.Contains(line, "finish_reason") && !strings.Contains(line, "stop_reason") {
				continue
			}
			data, found := strings.CutPrefix(strings.TrimSpace(line), "data:")
			if !found {
				continue
			}
			var obj map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(data)), &obj) != nil {
				continue
			}
			fromObj(obj)
		}
	}
	return finish
}
