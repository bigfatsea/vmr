// Ver 2026-07-25, by Sonnet 5
package report

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/core"
	"vmr/internal/i18n"
)

// TestNormDescriptions_AllKnownStepsHaveText guards against a norm step
// name being added to the router-side trail (response.go/responsefix.go)
// without a matching entry here — writeNorms falls back to "（未知步骤）"
// for anything missing, which is silent and easy to forget.
func TestNormDescriptions_AllKnownStepsHaveText(t *testing.T) {
	for _, step := range []string{
		"model_rewrite", "done_appended", "think_strip", "thinking_process_strip",
		"buffered", "resumed_stream", "soft_block_detected", "opaque",
		"overflow_raw_passthrough", "crlf_framing_suspected",
		"thinking_process_pattern_detected",
	} {
		var b strings.Builder
		writeNorms(&b, []string{step}, i18n.Detail(i18n.EN))
		if strings.Contains(b.String(), "unknown step") {
			t.Errorf("norm step %q has no description in normDescriptions", step)
		}
	}
}

// TestAttemptUpstreamFallback covers backward compatibility with audit
// logs written before Attempt.Protocol/Provider/Model existed: the three
// segments must still be recoverable by splitting the "/"-joined Endpoint
// those old logs used (current logs join with ":" and always carry the
// structured fields directly, so they never hit the fallback).
func TestAttemptUpstreamFallback(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		a                                 audit.Attempt
		wantProtocol, wantProv, wantModel string
	}{
		{"new log: structured fields used directly",
			audit.Attempt{Endpoint: "openai:minimax:MiniMax-M3", Protocol: "openai", Provider: "minimax", Model: "MiniMax-M3"},
			"openai", "minimax", "MiniMax-M3"},
		{"old log: falls back to splitting the '/'-joined Endpoint",
			audit.Attempt{Endpoint: "openai/minimax/MiniMax-M3"},
			"openai", "minimax", "MiniMax-M3"},
		{"old log: model name itself contains '/' (OpenRouter-style), only first two separators are structural",
			audit.Attempt{Endpoint: "openai/openrouter/z-ai/glm-5.2"},
			"openai", "openrouter", "z-ai/glm-5.2"},
		{"old log, unparseable endpoint: no crash, empty triple",
			audit.Attempt{Endpoint: "not-a-real-endpoint"},
			"", "", ""},
		{"no attempt at all", audit.Attempt{}, "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			protocol, provider, model := attemptUpstream(tc.a)
			if protocol != tc.wantProtocol || provider != tc.wantProv || model != tc.wantModel {
				t.Errorf("attemptUpstream(%+v) = (%q,%q,%q), want (%q,%q,%q)",
					tc.a, protocol, provider, model, tc.wantProtocol, tc.wantProv, tc.wantModel)
			}
		})
	}
}

// TestRealModelFallback covers realModel() specifically against an
// old-format record whose only endpoint info is the "/"-joined Endpoint
// string — the exact scenario that regressed detail filenames to "_none_"
// for every historical record before attemptUpstream's fallback was added.
func TestRealModelFallback(t *testing.T) {
	rec := &audit.Record{Attempts: []audit.Attempt{{Endpoint: "openai/minimax/MiniMax-M3"}}}
	if got := realModel(rec); got != "MiniMax-M3" {
		t.Errorf("realModel = %q, want %q", got, "MiniMax-M3")
	}
	if got := realModel(&audit.Record{}); got != "none" {
		t.Errorf("realModel with no attempts = %q, want none", got)
	}
}

func TestDetailFileName(t *testing.T) {
	ts := time.Date(2026, 7, 9, 0, 31, 6, 804_000_000, time.FixedZone("CST", 8*3600))
	rec := &audit.Record{TS: ts, Model: "agent", Outcome: "ok",
		Attempts: []audit.Attempt{{Endpoint: "openai:minimax:MiniMax-M3", Model: "MiniMax-M3"}}}
	used := map[string]int{}
	got := detailFileName(rec, used)
	want := "20260709-003106.804_agent_MiniMax-M3_ok.md"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	// Same-millisecond collision gets a numeric suffix.
	if got2 := detailFileName(rec, used); got2 != "20260709-003106.804_agent_MiniMax-M3_ok-2.md" {
		t.Errorf("collision name = %q", got2)
	}

	// Error outcome carries the error class; unsafe characters sanitized.
	rec2 := &audit.Record{TS: ts, Model: "my model/v2", Outcome: "error",
		Attempts: []audit.Attempt{{Endpoint: "openai:x:y", Model: "y", Error: "network: dial tcp: refused", ErrorClass: "network"}}}
	got = detailFileName(rec2, map[string]int{})
	if want := "20260709-003106.804_my-model-v2_y_error-network.md"; got != want {
		t.Errorf("got %q want %q", got, want)
	}

	// Rejected request: no model, no attempts.
	rec3 := &audit.Record{TS: ts, Outcome: "error"}
	if got := detailFileName(rec3, map[string]int{}); !strings.Contains(got, "rejected") || !strings.Contains(got, "none") {
		t.Errorf("rejected name = %q", got)
	}
}

func TestDetailFileNameSortsByTime(t *testing.T) {
	// Lexical order of generated names must equal time order.
	zone := time.FixedZone("CST", 8*3600)
	times := []time.Time{
		time.Date(2026, 7, 9, 9, 59, 59, 999_000_000, zone),
		time.Date(2026, 7, 9, 10, 0, 0, 0, zone),
		time.Date(2026, 7, 10, 0, 0, 0, 1_000_000, zone),
	}
	var prev string
	for _, ts := range times {
		name := detailFileName(&audit.Record{TS: ts, Model: "m", Outcome: "ok"}, map[string]int{})
		if prev != "" && !(prev < name) {
			t.Errorf("names not sorted: %q then %q", prev, name)
		}
		prev = name
	}
}

func TestCodeFence(t *testing.T) {
	// Content containing a triple-backtick run must get a longer fence.
	out := codeFence("hi\n```go\ncode\n```")
	if !strings.HasPrefix(out, "````\n") || !strings.HasSuffix(out, "````\n") {
		t.Errorf("fence not extended:\n%s", out)
	}
	if out := codeFence("plain"); !strings.HasPrefix(out, "```\n") {
		t.Errorf("plain fence wrong:\n%s", out)
	}
}

func TestDiffHeaderTable(t *testing.T) {
	base := http.Header{"Same": {"v"}, "Changed": {"old"}, "Removed": {"gone"}}
	other := http.Header{"Same": {"v"}, "Changed": {"new"}, "Added": {"fresh"}}
	table, changed := diffHeaderTable(base, other, i18n.Detail(i18n.EN))
	if changed != 3 {
		t.Errorf("changed = %d, want 3", changed)
	}
	for _, want := range []string{
		"| 🟢 | Added | fresh |",
		"| 🔴 | Removed | ~~gone~~ |",
		"| 🔶 | Changed | old → new |",
		"| | Same | v |", // unchanged still listed, unmarked
	} {
		if !strings.Contains(table, want) {
			t.Errorf("table missing %q:\n%s", want, table)
		}
	}
}

func TestRenderBodyDiffMarksChanges(t *testing.T) {
	client := map[string]any{
		"model": "agent", "stream": true,
		"messages": []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	attempt := map[string]any{
		"model": "MiniMax-M3", "stream": true,
		"messages": []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "user", "content": "hello resized"},
		},
	}
	var b strings.Builder
	renderBodyDiff(&b, client, attempt, i18n.Detail(i18n.EN))
	out := b.String()
	for _, want := range []string{
		`| 🔶 | model | "agent" → "MiniMax-M3" |`,
		"| | stream | true |",         // unchanged field still listed
		"- #1 system · 3 chars",       // unchanged message still listed, unmarked
		"🔶 #2 user · 5 → 13 chars",    // changed message marked with sizes
		"hello resized",               // attempt-side content available inline
		"Messages diff (2, 1 changed", // summary carries the change count
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestReassembleSSEOpenAI(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}],"model":"agent"}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"reasoning_content":"hmm"}}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{\"pa"}}]}}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":1}"}}]}}]}`,
		``,
		`data: {"choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}`,
		``,
		`data: [DONE]`,
	}, "\n")
	s := reassembleSSE(raw)
	if s == nil {
		t.Fatal("nil summary")
	}
	if s.Content != "Hello" || s.Reasoning != "hmm" || s.Finish != "tool_calls" || s.Model != "agent" {
		t.Errorf("got %+v", s)
	}
	if len(s.ToolCalls) != 1 || s.ToolCalls[0].Name != "read" || s.ToolCalls[0].Args != `{"path":1}` {
		t.Errorf("tool calls: %+v", s.ToolCalls)
	}
}

func TestReassembleSSEAnthropic(t *testing.T) {
	raw := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"model":"claude-x","usage":{"input_tokens":10}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"let me see"}}`,
		``,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hi "}}`,
		``,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"there"}}`,
		``,
		`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"t1","name":"search"}}`,
		``,
		`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"q\":\"x\"}"}}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
	}, "\n")
	s := reassembleSSE(raw)
	if s == nil {
		t.Fatal("nil summary")
	}
	if s.Content != "Hi there" || s.Reasoning != "let me see" || s.Finish != "end_turn" || s.Model != "claude-x" {
		t.Errorf("got %+v", s)
	}
	if len(s.ToolCalls) != 1 || s.ToolCalls[0].Name != "search" || s.ToolCalls[0].Args != `{"q":"x"}` {
		t.Errorf("tool calls: %+v", s.ToolCalls)
	}
}

func TestReassembleSSEUnparseable(t *testing.T) {
	if s := reassembleSSE("plain text, not SSE at all"); s != nil {
		t.Errorf("expected nil, got %+v", s)
	}
}

func TestImagePlaceholder(t *testing.T) {
	got := renderContent([]any{
		map[string]any{"type": "text", "text": "look:"},
		map[string]any{"type": "image_url", "image_url": map[string]any{
			"url": "data:image/png;base64," + strings.Repeat("A", 4096)}},
	})
	if strings.Contains(got, "AAAA") {
		t.Error("base64 payload leaked into output")
	}
	if !strings.Contains(got, "🖼 [image image/png ~3.0KB]") {
		t.Errorf("placeholder missing: %q", got)
	}
}

func TestChatMessagesAnthropicSystem(t *testing.T) {
	body := map[string]any{
		"system": "be nice",
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "hi"},
			}},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "tool_use", "name": "run", "input": map[string]any{"cmd": "ls"}},
			}},
		},
	}
	msgs := chatMessages(body)
	if len(msgs) != 3 || msgs[0].Role != "system" || msgs[0].Text != "be nice" {
		t.Fatalf("msgs = %+v", msgs)
	}
	if !strings.Contains(msgs[2].Text, "🔧 tool_use run") {
		t.Errorf("tool_use not rendered: %q", msgs[2].Text)
	}
}

// TestRenderBodyDiffExcludesResponsesConversationFields proves the
// param-diff table's "bulky" exclusion — already covering "messages"/
// "tools"/"system" — also covers openai-responses' "input"/"instructions",
// so a Responses-protocol detail page doesn't dump the entire conversation
// twice: once (correctly) under "Messages diff", and again as raw JSON in
// the top-level param table meant for small metadata fields only.
func TestRenderBodyDiffExcludesResponsesConversationFields(t *testing.T) {
	client := map[string]any{
		"model": "agent", "instructions": "you are helpful",
		"input": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	attempt := map[string]any{
		"model": "real-model", "instructions": "you are helpful",
		"input": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	var b strings.Builder
	renderBodyDiff(&b, client, attempt, i18n.Detail(i18n.EN))
	out := b.String()
	if strings.Contains(out, "\"input\"") || strings.Contains(out, "| input |") {
		t.Errorf("input array leaked into the param diff table instead of staying in Messages diff:\n%s", out)
	}
	if strings.Contains(out, "| instructions |") {
		t.Errorf("instructions leaked into the param diff table:\n%s", out)
	}
}

func TestRoleChars(t *testing.T) {
	// openai shape: roles taken as-is, tool_calls counted to assistant.
	openai := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": strings.Repeat("s", 10)},
			map[string]any{"role": "user", "content": strings.Repeat("u", 30)},
			map[string]any{"role": "assistant", "content": strings.Repeat("a", 20)},
			map[string]any{"role": "tool", "content": strings.Repeat("t", 40), "tool_call_id": "c1"},
		},
	}
	rc := roleChars(openai)
	if rc["system"] != 10 || rc["user"] != 30 || rc["assistant"] != 20 || rc["tool"] != 40 {
		t.Errorf("openai roleChars = %v", rc)
	}

	// anthropic shape: top-level system counts as one message; tool_result
	// parts inside a user message count as "tool", not "user".
	anthropic := map[string]any{
		"system": strings.Repeat("s", 5),
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": strings.Repeat("u", 7)},
				map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "xx"},
			}},
		},
	}
	rc = roleChars(anthropic)
	if rc["system"] != 5 || rc["user"] != 7 || rc["tool"] == 0 {
		t.Errorf("anthropic roleChars = %v", rc)
	}

	// openai-responses shape: top-level instructions counts as "system";
	// function_call/function_call_output Items have no "role" key at all
	// and must still land under a sensible bucket ("assistant"/"tool"), not
	// silently vanish into an empty-string role.
	responses := map[string]any{
		"instructions": strings.Repeat("s", 5),
		"input": []any{
			map[string]any{"role": "user", "content": strings.Repeat("u", 7)},
			map[string]any{"type": "function_call", "call_id": "c1", "name": "exec", "arguments": strings.Repeat("a", 9)},
			map[string]any{"type": "function_call_output", "call_id": "c1", "output": strings.Repeat("t", 11)},
		},
	}
	rc = roleChars(responses)
	if rc["system"] != 5 || rc["user"] != 7 || rc["assistant"] == 0 || rc["tool"] == 0 {
		t.Errorf("openai-responses roleChars = %v", rc)
	}
	if _, hasEmptyRole := rc[""]; hasEmptyRole {
		t.Errorf("roleChars should never bucket a Responses non-message Item under an empty role: %v", rc)
	}

	// Non-chat bodies yield nothing.
	if rc := roleChars("not json"); rc != nil {
		t.Errorf("string body roleChars = %v", rc)
	}
}

// TestRoleTokens locks in that roleTokens shares roleChars' traversal (same
// per-role attribution) but sizes each fragment with core.EstimateTextTokens
// instead of a rune count — ascii text should divide down by ~4x, not equal
// the character count.
func TestRoleTokens(t *testing.T) {
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": strings.Repeat("u", 40)},
			map[string]any{"role": "assistant", "content": strings.Repeat("a", 80)},
		},
	}
	rt := roleTokens(body)
	if rt["user"] != 10 || rt["assistant"] != 20 {
		t.Errorf("roleTokens = %v, want user=10 assistant=20 (ascii/4)", rt)
	}
	if rc := roleTokens("not json"); rc != nil {
		t.Errorf("string body roleTokens = %v", rc)
	}
}

func TestRoleStatLine(t *testing.T) {
	chars := map[string]int64{"system": 10, "user": 30, "assistant": 20, "tool": 40}
	got := roleStatLine(chars, false, false)
	want := "system 10.0% · user 30.0% · assistant 20.0% · tool 40.0%"
	if got != want {
		t.Errorf("share-only line:\ngot  %q\nwant %q", got, want)
	}
	withChars := roleStatLine(chars, true, false)
	if !strings.Contains(withChars, "tool 40 (40.0%)") {
		t.Errorf("withChars line missing counts: %q", withChars)
	}
	if roleStatLine(nil, false, false) != "" {
		t.Error("empty map should render empty line")
	}
}

func TestFinalMessageJSON(t *testing.T) {
	body := map[string]any{
		"model": "m",
		"choices": []any{map[string]any{
			"finish_reason": "stop",
			"message":       map[string]any{"role": "assistant", "content": "done"},
		}},
	}
	s, ok := finalMessage(body)
	if !ok || s.Content != "done" || s.Finish != "stop" {
		t.Errorf("ok=%v s=%+v", ok, s)
	}
	if _, ok := finalMessage("not json"); ok {
		t.Error("string body should not parse")
	}
}

func TestWriteDetailsEndToEnd(t *testing.T) {
	// Two records out of time order in the file; INDEX must sort by ts and
	// the norm trail must be translated in the passthrough note.
	lines := `{"ts":"2026-07-09T10:00:01+08:00","dur_ms":500,"model":"agent","protocol":"openai","stream":false,"outcome":"ok","client":{"addr":"1.2.3.4:5","request":{"method":"POST","path":"/v1/chat/completions","headers":{"Content-Type":["application/json"]},"body":{"model":"agent","messages":[{"role":"user","content":"hi"}]}},"response":{"status":200,"headers":{"Content-Type":["application/json"]},"body":{"model":"agent","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}}},"attempts":[{"endpoint":"openai/prov/real-1","protocol":"openai","provider":"prov","model":"real-1","url":"https://x/v1","dur_ms":450,"request":{"method":"POST","path":"/v1","headers":{"Content-Type":["application/json"]},"body":{"model":"real-1","messages":[{"role":"user","content":"hi"}]}},"response":{"status":200,"headers":{"Content-Type":["application/json"]}},"norm":["model_rewrite"]}]}
{"ts":"2026-07-09T09:00:00+08:00","dur_ms":100,"model":"agent","protocol":"openai","stream":false,"outcome":"error","client":{"addr":"1.2.3.4:5","request":{"method":"POST","path":"/v1/chat/completions","headers":{},"body":{"model":"agent","messages":[]}}},"attempts":[{"endpoint":"openai/prov/real-1","protocol":"openai","provider":"prov","model":"real-1","url":"https://x/v1","dur_ms":90,"request":{"headers":{},"body":{"model":"real-1","messages":[]}},"error":"network: dial tcp: refused","error_class":"network"}]}
`
	dir := t.TempDir()
	src := filepath.Join(dir, "audit.jsonl")
	if err := os.WriteFile(src, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "details")
	n, err := WriteDetails([]string{src}, out, nil, nil, i18n.EN)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}

	okFile, err := os.ReadFile(filepath.Join(out, "20260709-100001.000_agent_real-1_ok.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## ① Client → VMR Request",
		"### Attempt 1/1 · openai/prov/real-1 · ✅ · HTTP 200",
		`| 🔶 | model | "agent" → "real-1" |`,
		"`model_rewrite` — The real upstream model name was rewritten back to the virtual model name",
		"hello", // reassembled final message
	} {
		if !strings.Contains(string(okFile), want) {
			t.Errorf("ok file missing %q", want)
		}
	}

	errFile, err := os.ReadFile(filepath.Join(out, "20260709-090000.000_agent_real-1_error-network.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"❌ **error**: network: dial tcp: refused",
		"(no response record — connection dropped or the request was canceled)",
	} {
		if !strings.Contains(string(errFile), want) {
			t.Errorf("error file missing %q", want)
		}
	}

	// Same-named .json sibling for the ok record.
	if _, err := os.ReadFile(filepath.Join(out, "20260709-100001.000_agent_real-1_ok.json")); err != nil {
		t.Errorf("ok record missing .json sibling: %v", err)
	}

	// Exports carry the same conversation bodies as the 0600 audit source —
	// they must not loosen its permissions (owner-only, no group/other bits).
	for _, p := range []string{
		out,
		filepath.Join(out, "20260709-100001.000_agent_real-1_ok.md"),
		filepath.Join(out, "20260709-100001.000_agent_real-1_ok.json"),
	} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s: perm %o leaks group/other access, want owner-only", p, st.Mode().Perm())
		}
	}
}

// TestWriteDetailsByTag covers that ClientKeyTag never affects
// WriteDetails' own output: details/ stays one shared, unfiltered,
// un-duplicated pool of per-request files regardless of how many distinct
// tags the records carry (per-tag *views* over this data are the report's
// job now — vmr-requests-<tag>.md — not WriteDetails').
func TestWriteDetailsByTag(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, zone) }
	sys := msg("system", "sys")

	// Distinct opening user messages so each record anchors its own
	// session — three unrelated callers, not three turns of one
	// conversation (identical content would collapse them into a single
	// shared session, which would defeat the point of this test: three
	// independently-tagged records writing to the same shared details/).
	r1 := mkRec(at(0), "", []any{sys, msg("user", "alice's question")}, nil, sseText("a1"))
	r1.ClientKeyTag = "alice"
	r2 := mkRec(at(1), "", []any{sys, msg("user", "bob's question")}, nil, sseText("b1"))
	r2.ClientKeyTag = "bob"
	r3 := mkRec(at(2), "", []any{sys, msg("user", "an untagged question")}, nil, sseText("untagged"))
	// r3.ClientKeyTag left "" — legacy/catch-all/no-auth traffic.

	src := writeJSONL(t, []audit.Record{r1, r2, r3})
	a, err := AnalyzeSessions([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "details")
	n, err := WriteDetails([]string{src}, out, a, nil, i18n.EN)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("n = %d, want 3", n)
	}

	// details/ holds all three records' files, unfiltered, exactly once.
	detailFiles, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(detailFiles) != 6 { // 3 records × (.md + .json), tags notwithstanding
		t.Fatalf("details/ entries = %d, want 6: %v", len(detailFiles), detailFiles)
	}

	// Nothing else gets written next to details/ — WriteDetails no longer
	// produces any tag-aware output of its own.
	topEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(topEntries) != 1 || topEntries[0].Name() != "details" {
		t.Errorf("top-level entries = %v, want exactly details/", topEntries)
	}
}

// TestRenderDetail_RawPreStrip covers both states of the "② VMR → 上游" raw
// pre-strip display: full content when the audit record captured it
// (RawPreStrip populated — see internal/router/response.go), and a graceful
// "not captured" note for records logged before that capture existed.
func TestRenderDetail_RawPreStrip(t *testing.T) {
	base := func(rawPreStrip any) *audit.Record {
		return &audit.Record{TS: time.Now(), Model: "agent", Outcome: "ok",
			Client: audit.Exchange{
				Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: http.Header{}, Body: map[string]any{}},
			},
			Attempts: []audit.Attempt{{
				Endpoint: "openai/minimax/MiniMax-M3", URL: "https://x/v1",
				Request:     audit.Message{Headers: http.Header{}},
				Response:    &audit.Message{Status: 200, Headers: http.Header{}},
				Norm:        []string{"buffered", "think_strip", "model_rewrite"},
				RawPreStrip: rawPreStrip,
			}},
		}
	}

	withRaw := renderDetail(base(`data: {"choices":[{"delta":{"content":"<think>step 1</think>final answer"}}]}`+"\n\n"), nil, i18n.EN)
	if !strings.Contains(withRaw, "Pre-strip raw content") || !strings.Contains(withRaw, "<think>step 1</think>final answer") {
		t.Errorf("full pre-strip content not rendered:\n%s", withRaw)
	}
	if strings.Contains(withRaw, "didn't retain the pre-strip raw content") {
		t.Error("should not show the unavailable note when RawPreStrip is populated")
	}

	withoutRaw := renderDetail(base(nil), nil, i18n.EN)
	if !strings.Contains(withoutRaw, "didn't retain the pre-strip raw content") {
		t.Errorf("missing graceful fallback note when RawPreStrip is nil:\n%s", withoutRaw)
	}
}

// TestRenderDetail_FactsLine locks in that the detail Markdown surfaces
// audit.Record.Facts (vmr's own pre-routing analysis) verbatim — no
// recomputation from the stored request body — and that it appears near
// the top of the document (before section ① renders the full request),
// not buried after the detailed sections. A nil Facts (request rejected
// before fact computation ran) must render nothing, not a blank/zero line.
func TestRenderDetail_FactsLine(t *testing.T) {
	base := func(facts *core.RequestFacts) *audit.Record {
		return &audit.Record{TS: time.Now(), Model: "agent", Outcome: "ok",
			Client: audit.Exchange{
				Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: http.Header{}, Body: map[string]any{}},
			},
			Facts: facts,
		}
	}

	withFacts := renderDetail(base(&core.RequestFacts{HasImage: true, HasTools: false, EstimatedTokens: 1234}), nil, i18n.EN)
	if !strings.Contains(withFacts, "VMR pre-routing judgment") {
		t.Errorf("facts line missing:\n%s", withFacts)
	}
	if !strings.Contains(withFacts, "Capabilities required: `image`") {
		t.Errorf("facts line should list only the detected capability `image`:\n%s", withFacts)
	}
	if strings.Contains(withFacts, "`tools`") {
		t.Errorf("facts line should not list tools when HasTools is false:\n%s", withFacts)
	}
	if factsIdx, reqIdx := strings.Index(withFacts, "VMR pre-routing judgment"), strings.Index(withFacts, "① Client"); factsIdx < 0 || reqIdx < 0 || factsIdx > reqIdx {
		t.Errorf("facts line must appear before section ①, got factsIdx=%d reqIdx=%d", factsIdx, reqIdx)
	}
	if !strings.Contains(withFacts, "Estimated token count: 1.2 KT") {
		t.Errorf("facts line should render the plain (non-EST-suffixed) token estimate:\n%s", withFacts)
	}

	both := renderDetail(base(&core.RequestFacts{HasImage: true, HasTools: true, EstimatedTokens: 500}), nil, i18n.EN)
	if !strings.Contains(both, "Capabilities required: `image`, `tools`") {
		t.Errorf("facts line should list both detected capabilities joined by \", \":\n%s", both)
	}
	if !strings.Contains(both, "Estimated token count: 500 T") {
		t.Errorf("facts line should render sub-1000 estimate as plain T:\n%s", both)
	}

	neither := renderDetail(base(&core.RequestFacts{HasImage: false, HasTools: false, EstimatedTokens: 10}), nil, i18n.EN)
	if !strings.Contains(neither, "Capabilities required: none") {
		t.Errorf("facts line should show none when no capability is detected:\n%s", neither)
	}

	withoutFacts := renderDetail(base(nil), nil, i18n.EN)
	if strings.Contains(withoutFacts, "VMR pre-routing judgment") {
		t.Errorf("nil Facts must render nothing:\n%s", withoutFacts)
	}
}

// TestBuildOnRecordMatchesWriteDetails is the regression test for merging
// Build's aggregation pass with detail export: Build's onRecord hook
// (DetailWriter.Submit called inline, one pass over the audit source) must
// produce byte-identical output to the old two-pass path
// (AnalyzeSessions -> a separate WriteDetails pass, an independent second
// read of the same file). Runs both over the same input and diffs every
// file in both details/ directories.
func TestBuildOnRecordMatchesWriteDetails(t *testing.T) {
	dir := t.TempDir()
	records := smallAuditRecords()
	path := writeTempJSONL(t, dir, records)

	sess, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	oldDir := filepath.Join(dir, "old-details")
	oldN, err := WriteDetails([]string{path}, oldDir, sess, nil, i18n.EN)
	if err != nil {
		t.Fatal(err)
	}

	newDir := filepath.Join(dir, "new-details")
	dw, err := NewDetailWriter(newDir, i18n.EN)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Build([]string{path}, time.Now(), nil, nil, dw.Submit); err != nil {
		t.Fatal(err)
	}
	newN, err := dw.Close()
	if err != nil {
		t.Fatal(err)
	}

	if oldN != newN {
		t.Fatalf("record count mismatch: old=%d new=%d", oldN, newN)
	}
	if oldN == 0 {
		t.Fatal("expected at least one detail file written; test fixture produced none")
	}

	oldFiles, err := os.ReadDir(oldDir)
	if err != nil {
		t.Fatal(err)
	}
	newFiles, err := os.ReadDir(newDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldFiles) != len(newFiles) {
		t.Fatalf("file count mismatch: old=%d new=%d", len(oldFiles), len(newFiles))
	}
	for _, fi := range oldFiles {
		oldBytes, err := os.ReadFile(filepath.Join(oldDir, fi.Name()))
		if err != nil {
			t.Fatal(err)
		}
		newBytes, err := os.ReadFile(filepath.Join(newDir, fi.Name()))
		if err != nil {
			t.Fatalf("missing in new-details: %s", fi.Name())
		}
		if string(oldBytes) != string(newBytes) {
			t.Fatalf("content mismatch for %s:\n--- old ---\n%s\n--- new ---\n%s", fi.Name(), oldBytes, newBytes)
		}
	}
}
