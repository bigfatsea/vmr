// Ver 2026-07-10 01:40, by Fable 5
package report

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
)

func TestDetailFileName(t *testing.T) {
	ts := time.Date(2026, 7, 9, 0, 31, 6, 804_000_000, time.FixedZone("CST", 8*3600))
	rec := &audit.Record{TS: ts, Model: "agent", Outcome: "ok",
		Attempts: []audit.Attempt{{Endpoint: "openai/minimax/MiniMax-M3"}}}
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
		Attempts: []audit.Attempt{{Endpoint: "openai/x/y", Error: "network: dial tcp: refused"}}}
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
	table, changed := diffHeaderTable(base, other)
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
	renderBodyDiff(&b, client, attempt)
	out := b.String()
	for _, want := range []string{
		`| 🔶 | model | "agent" → "MiniMax-M3" |`,
		"| | stream | true |",    // unchanged field still listed
		"- #1 system · 3 字符",     // unchanged message still listed, unmarked
		"🔶 #2 user · 5 → 13 字符",  // changed message marked with sizes
		"hello resized",          // attempt-side content available inline
		"Messages 对比 (2 条，1 处变化", // summary carries the change count
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

func TestRoleCharsAndMessageCount(t *testing.T) {
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
	if n, ok := messageCount(openai); !ok || n != 4 {
		t.Errorf("messageCount = %d %v", n, ok)
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
	if n, ok := messageCount(anthropic); !ok || n != 2 {
		t.Errorf("messageCount = %d %v", n, ok)
	}

	// Non-chat bodies yield nothing.
	if rc := roleChars("not json"); rc != nil {
		t.Errorf("string body roleChars = %v", rc)
	}
	if _, ok := messageCount(map[string]any{"model": "x"}); ok {
		t.Error("body without messages/system should not count")
	}
}

func TestRoleStatLine(t *testing.T) {
	chars := map[string]int64{"system": 10, "user": 30, "assistant": 20, "tool": 40}
	got := roleStatLine(chars, false)
	want := "system 10.0% · user 30.0% · assistant 20.0% · tool 40.0%"
	if got != want {
		t.Errorf("share-only line:\ngot  %q\nwant %q", got, want)
	}
	withChars := roleStatLine(chars, true)
	if !strings.Contains(withChars, "tool 40 (40.0%)") {
		t.Errorf("withChars line missing counts: %q", withChars)
	}
	if roleStatLine(nil, false) != "" {
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
	lines := `{"ts":"2026-07-09T10:00:01+08:00","dur_ms":500,"model":"agent","protocol":"openai","stream":false,"outcome":"ok","client":{"addr":"1.2.3.4:5","request":{"method":"POST","path":"/v1/chat/completions","headers":{"Content-Type":["application/json"]},"body":{"model":"agent","messages":[{"role":"user","content":"hi"}]}},"response":{"status":200,"headers":{"Content-Type":["application/json"]},"body":{"model":"agent","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}}},"attempts":[{"endpoint":"openai/prov/real-1","url":"https://x/v1","dur_ms":450,"request":{"method":"POST","path":"/v1","headers":{"Content-Type":["application/json"]},"body":{"model":"real-1","messages":[{"role":"user","content":"hi"}]}},"response":{"status":200,"headers":{"Content-Type":["application/json"]}},"norm":["model_rewrite"]}]}
{"ts":"2026-07-09T09:00:00+08:00","dur_ms":100,"model":"agent","protocol":"openai","stream":false,"outcome":"error","client":{"addr":"1.2.3.4:5","request":{"method":"POST","path":"/v1/chat/completions","headers":{},"body":{"model":"agent","messages":[]}}},"attempts":[{"endpoint":"openai/prov/real-1","url":"https://x/v1","dur_ms":90,"request":{"headers":{},"body":{"model":"real-1","messages":[]}},"error":"network: dial tcp: refused"}]}
`
	dir := t.TempDir()
	src := filepath.Join(dir, "audit.jsonl")
	if err := os.WriteFile(src, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "details")
	n, err := WriteDetails([]string{src}, out, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}
	idx, err := os.ReadFile(filepath.Join(out, "INDEX.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Sorted by ts: the 09:00 error row precedes the 10:00 ok row.
	errPos := strings.Index(string(idx), "09:00:00")
	okPos := strings.Index(string(idx), "10:00:01")
	if errPos < 0 || okPos < 0 || errPos > okPos {
		t.Errorf("INDEX not time-sorted:\n%s", idx)
	}

	okFile, err := os.ReadFile(filepath.Join(out, "20260709-100001.000_agent_real-1_ok.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## ① Client → VMR 请求",
		"### Attempt 1/1 · openai/prov/real-1 · ✅ · HTTP 200",
		`| 🔶 | model | "agent" → "real-1" |`,
		"`model_rewrite` — 上游返回的真实模型名被改写回虚拟模型名",
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
		"（无响应记录——连接中断或请求被取消）",
	} {
		if !strings.Contains(string(errFile), want) {
			t.Errorf("error file missing %q", want)
		}
	}
}
