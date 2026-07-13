// Ver 2026-07-13 04:00, by Sonnet 5
package report

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
)

// sseToolCall builds a minimal OpenAI SSE stream that calls one tool and
// finishes with finish_reason=tool_calls.
func sseToolCall(tool string) string {
	return strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":""}}],"model":"agent"}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"` + tool + `","arguments":"{}"}}]}}]}`,
		``,
		`data: {"choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}`,
		``,
		`data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10,"completion_tokens_details":{"reasoning_tokens":4},"prompt_tokens_details":{"cached_tokens":80}}}`,
		``,
		`data: [DONE]`,
	}, "\n")
}

func sseText(text string) string {
	return strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"` + text + `"}}],"model":"agent"}`,
		``,
		`data: {"choices":[{"index":0,"finish_reason":"stop","delta":{}}]}`,
		``,
		`data: [DONE]`,
	}, "\n")
}

// mkRec builds one openai-protocol audit record.
func mkRec(ts time.Time, trace string, msgs []any, tools []string, respBody any) audit.Record {
	body := map[string]any{"model": "agent", "stream": true, "messages": msgs}
	if tools != nil {
		arr := make([]any, 0, len(tools))
		for _, t := range tools {
			arr = append(arr, map[string]any{"type": "function", "function": map[string]any{"name": t}})
		}
		body["tools"] = arr
	}
	h := http.Header{}
	if trace != "" {
		h.Set("Traceparent", "00-"+trace+"-abcdef0123456789-01")
	}
	rec := audit.Record{
		TS: ts, DurMS: 100, Model: "agent", Protocol: "openai", Stream: true, Outcome: "ok",
		Client: audit.Exchange{
			Request:  audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: h, Body: body},
			Response: &audit.Message{Status: 200, Headers: http.Header{}, Body: respBody},
		},
		Attempts: []audit.Attempt{{Endpoint: "openai:prov:real-1", Protocol: "openai", Provider: "prov", Model: "real-1", URL: "https://x/v1", DurMS: 90,
			Request:  audit.Message{Headers: http.Header{}},
			Response: &audit.Message{Status: 200, Headers: http.Header{}},
			Norm:     []string{"model_rewrite"}}},
	}
	return rec
}

func msg(role, content string) map[string]any {
	return map[string]any{"role": role, "content": content}
}

func writeJSONL(t *testing.T, recs []audit.Record) string {
	t.Helper()
	var b strings.Builder
	for _, r := range recs {
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// fixture builds the canonical scenario: session A with a tool-loop task, a
// tail-replacement new-task turn, a compaction call, and the continuation
// session A2, plus a truncated record.
func fixture(t *testing.T) (string, []audit.Record) {
	t.Helper()
	zone := time.FixedZone("CST", 8*3600)
	at := func(min, sec int) time.Time { return time.Date(2026, 7, 9, 10, min, sec, 0, zone) }

	sys := msg("system", "You are a personal assistant running inside OpenClaw. Tools listed.")
	u1 := msg("user", "[Thu 2026-07-09 10:00 GMT+8] 任务A开始：请调研 X 并输出报告")
	a1 := map[string]any{"role": "assistant", "content": "好的,开工",
		"tool_calls": []any{map[string]any{"id": "c1", "function": map[string]any{"name": "exec", "arguments": "{}"}}}}
	t1 := map[string]any{"role": "tool", "content": "exec ok", "tool_call_id": "c1"}
	wrap1 := msg("user", "OpenClaw runtime context for the immediately preceding user message.\nInternal.")
	wrap2 := msg("user", "[Thu 2026-07-09 10:00 GMT+8] Conversation info (untrusted metadata):\n```json\n{\"chat_id\":\"user:ou_test123\"}\n```")
	tools := []string{"exec", "read", "write"}

	// r1: opening turn (trace T1) → model calls exec.
	r1 := mkRec(at(0, 0), "1111aaaa1111aaaa1111aaaa1111aaaa", []any{sys, u1, wrap1, wrap2}, tools, sseToolCall("exec"))
	// r2: tool loop continues (same trace): wrappers replaced by history a1+t1.
	a2 := map[string]any{"role": "assistant", "content": "继续",
		"tool_calls": []any{map[string]any{"id": "c2", "function": map[string]any{"name": "exec", "arguments": "{}"}}}}
	t2 := map[string]any{"role": "tool", "content": "exec ok 2", "tool_call_id": "c2"}
	r2 := mkRec(at(0, 30), "1111aaaa1111aaaa1111aaaa1111aaaa", []any{sys, u1, a1, t1, wrap1, wrap2}, tools, sseToolCall("exec"))
	// r3: new user turn (trace T2), wrappers replaced by the condensed
	// instruction; delta carries a real user message near the end.
	u2 := msg("user", "[Thu 2026-07-09 10:01 GMT+8] 换个方向,先停掉任务")
	r3 := mkRec(at(1, 0), "2222bbbb2222bbbb2222bbbb2222bbbb", []any{sys, u1, a1, t1, a2, t2, u2, wrap1, wrap2}, tools, sseText("已停"))

	// compaction call: no traceparent, no tools, summarizes session A.
	compBody := map[string]any{"model": "agent", "stream": true, "max_completion_tokens": 16000,
		"messages": []any{
			msg("system", "You are a context summarization assistant. Summarize."),
			msg("user", "<conversation>\n[User]: 任务A开始：请调研 X 并输出报告\n[Assistant]: 好的,开工\n</conversation>"),
		}}
	comp := audit.Record{
		TS: at(2, 0), DurMS: 50, Model: "agent", Protocol: "openai", Stream: true, Outcome: "ok",
		Client: audit.Exchange{
			Request:  audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: http.Header{}, Body: compBody},
			Response: &audit.Message{Status: 200, Headers: http.Header{}, Body: sseText("## Goal 调研 X 的总结摘要")},
		},
		Attempts: []audit.Attempt{{Endpoint: "openai:prov:real-1", Protocol: "openai", Provider: "prov", Model: "real-1", Response: &audit.Message{Status: 200, Headers: http.Header{}}}},
	}

	// session A2: continuation whose anchor embeds the compaction output.
	uc := msg("user", "The conversation history before this point was compacted into the following summary:\n\n<summary>\n## Goal 调研 X 的总结摘要\n</summary>")
	r4 := mkRec(at(3, 0), "3333cccc3333cccc3333cccc3333cccc", []any{sys, uc, wrap1, wrap2}, tools, sseToolCall("read"))

	// truncated stream: ok outcome, truncated attempt error.
	r5 := mkRec(at(4, 0), "3333cccc3333cccc3333cccc3333cccc", []any{sys, uc, a1, t1, wrap1, wrap2}, tools, sseText("部分输出"))
	r5.Attempts[0].Error = "truncated: stream idle timeout"
	r5.Attempts[0].ErrorClass = "truncated"

	recs := []audit.Record{r1, r2, r3, comp, r4, r5}
	return writeJSONL(t, recs), recs
}

func TestAnalyzeSessionsGrouping(t *testing.T) {
	path, _ := fixture(t)
	a, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Recs) != 6 {
		t.Fatalf("recs = %d, want 6", len(a.Recs))
	}
	if len(a.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2 (A and A2): %+v", len(a.Sessions), a.Sessions)
	}
	if len(a.Compactions) != 1 {
		t.Fatalf("compactions = %d, want 1", len(a.Compactions))
	}

	sA, sA2 := a.Sessions[0], a.Sessions[1]
	if len(sA.Recs) != 3 || len(sA2.Recs) != 2 {
		t.Fatalf("session sizes = %d/%d, want 3/2", len(sA.Recs), len(sA2.Recs))
	}
	// r1+r2 are one tool-loop task; r3 (new trace + real user in delta) opens t02.
	if len(sA.Tasks) != 2 {
		t.Fatalf("session A tasks = %d, want 2", len(sA.Tasks))
	}
	if got := sA.Tasks[0].Recs[1].TaskSeq; got != 2 {
		t.Errorf("r2 task seq = %d, want 2", got)
	}
	r3 := sA.Tasks[1].Recs[0]
	if r3.NewInstruction == "" || !strings.Contains(r3.NewInstruction, "换个方向") {
		t.Errorf("r3 new instruction = %q", r3.NewInstruction)
	}
	// r2's parent is r1: 2 wrapper messages were replaced by a1+t1.
	r2 := sA.Tasks[0].Recs[1]
	if r2.Parent == nil || r2.ReplacedTail != 2 {
		t.Errorf("r2 parent=%v replacedTail=%d, want 2", r2.Parent != nil, r2.ReplacedTail)
	}
	// Session title from the opening instruction.
	if !strings.Contains(sA.Title, "任务A开始") {
		t.Errorf("session A title = %q", sA.Title)
	}
	// chat_id extracted from the Conversation info wrapper.
	if sA.ChatID != "user:ou_test123" {
		t.Errorf("chat_id = %q", sA.ChatID)
	}

	// Compaction links: summarizes A, continues to A2.
	c := a.Compactions[0]
	if c.Summarizes != sA.ID || c.ContinuesTo != sA2.ID {
		t.Errorf("compaction links: summarizes=%q continuesTo=%q (want %s/%s)",
			c.Summarizes, c.ContinuesTo, sA.ID, sA2.ID)
	}
	if sA2.ContinuedFrom != sA.ID {
		t.Errorf("A2.ContinuedFrom = %q, want %s", sA2.ContinuedFrom, sA.ID)
	}
	if !sA2.IsContinuation {
		t.Error("A2 should be flagged as continuation")
	}

	// Truncated flag on the last record.
	r5 := sA2.Recs[1]
	if !r5.Truncated {
		t.Error("r5 should be flagged truncated")
	}
	// Per-turn tool calls come from the response only (history repeats ignored).
	r1i := sA.Tasks[0].Recs[0]
	if len(r1i.ToolCalls) != 1 || r1i.ToolCalls[0] != "exec" {
		t.Errorf("r1 tool calls = %v", r1i.ToolCalls)
	}
	if r1i.Usage.Reasoning != 4 || r1i.Usage.CacheRead != 80 {
		t.Errorf("usage details = %+v", r1i.Usage)
	}
	if r1i.Finish != "tool_calls" {
		t.Errorf("finish = %q", r1i.Finish)
	}
}

func TestToolShapesAggregation(t *testing.T) {
	path, _ := fixture(t)
	a, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	shapes := a.ToolShapes()
	if len(shapes) != 1 { // compaction has no tools and no calls → skipped
		t.Fatalf("shapes = %d, want 1: %+v", len(shapes), shapes)
	}
	s := shapes[0]
	if s.Requests != 5 || len(s.Declared) != 3 {
		t.Errorf("shape requests=%d declared=%d", s.Requests, len(s.Declared))
	}
	// exec called twice (r1, r2), read once (r4); write never.
	if s.Calls["exec"] != 2 || s.Calls["read"] != 1 {
		t.Errorf("calls = %v", s.Calls)
	}
	if len(s.NeverCalled) != 1 || s.NeverCalled[0] != "write" {
		t.Errorf("never called = %v", s.NeverCalled)
	}
	if s.DeclaredBytes == 0 {
		t.Error("declared bytes not measured")
	}
}

func TestWriteRequestsExport(t *testing.T) {
	path, _ := fixture(t)
	a, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "vmr-requests.jsonl")
	n, err := WriteRequests(a, out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Fatalf("rows = %d, want 6", n)
	}
	raw, _ := os.ReadFile(out)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 6 {
		t.Fatalf("lines = %d", len(lines))
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ts", "session", "task", "trace_id", "chat_id", "shape",
		"tool_calls", "finish_reason", "tokens_in", "detail_file"} {
		if _, ok := first[key]; !ok {
			t.Errorf("first row missing %q: %s", key, lines[0])
		}
	}
	if first["session"] != "s01" || first["task"] != "t01" {
		t.Errorf("first row coords: session=%v task=%v", first["session"], first["task"])
	}
	// The compaction row carries no session coordinates.
	var comp map[string]any
	if err := json.Unmarshal([]byte(lines[3]), &comp); err != nil {
		t.Fatal(err)
	}
	if _, hasSession := comp["session"]; hasSession {
		t.Errorf("compaction row should have no session: %s", lines[3])
	}
	if tags, _ := comp["tags"].([]any); len(tags) == 0 {
		t.Errorf("compaction row should be tagged: %s", lines[3])
	}
}

// TestUngroupedFoldedIntoUnresolved covers a non-chat/rejected record (no
// "messages" field, so it never gets a SessKey): it must render as a small
// "其他" sub-section nested under "## Chat User (unresolved)", not as its
// own top-level "## 未分组" heading.
func TestUngroupedFoldedIntoUnresolved(t *testing.T) {
	line := `{"ts":"2026-07-09T08:00:00+08:00","dur_ms":10,"model":"","protocol":"openai","outcome":"error","client":{"request":{"method":"POST","path":"/v1/chat/completions","body":null}}}` + "\n"
	dir := t.TempDir()
	src := filepath.Join(dir, "audit.jsonl")
	if err := os.WriteFile(src, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := AnalyzeSessions([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Ungrouped) != 1 {
		t.Fatalf("ungrouped = %d, want 1", len(a.Ungrouped))
	}
	out := filepath.Join(dir, "details")
	if _, err := WriteDetails([]string{src}, out, a); err != nil {
		t.Fatal(err)
	}
	idx, err := os.ReadFile(filepath.Join(dir, "vmr-requests-index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(idx), "## 未分组") {
		t.Error("Ungrouped must no longer render as its own top-level ## 未分组 section")
	}
	if !strings.Contains(string(idx), "## Chat User (unresolved)") {
		t.Errorf("missing unresolved user section:\n%s", idx)
	}
	if !strings.Contains(string(idx), "### 其他 · 非聊天体/被拒请求 × 1") {
		t.Errorf("missing folded 其他 sub-section:\n%s", idx)
	}
	if i, j := strings.Index(string(idx), "## Chat User (unresolved)"), strings.Index(string(idx), "### 其他"); i < 0 || j < i {
		t.Error("其他 sub-section must sit inside the (unresolved) user section")
	}
}

func TestWriteDetailsGroupedIndex(t *testing.T) {
	path, _ := fixture(t)
	a, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "details")
	n, err := WriteDetails([]string{path}, dir, a)
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Fatalf("n = %d, want 6", n)
	}
	idx, err := os.ReadFile(filepath.Join(filepath.Dir(dir), "vmr-requests-index.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Chat User ou_test123",
		"### s01 · 2 任务", // Session heading keeps the id, drops the "Session" label word
		"**t01 · 2 轮",    // Task heading keeps the id, drops the "Task" label word
		"任务A开始",          // user instruction appears in a quote block under the task heading
		"### s02",
		"## Chat User (unresolved)",
		"### 压缩任务 · compaction 会话 × 1",
		"## 全部请求（时间序）",
		"s01/t02",
		"⚠️截断",
	} {
		if !strings.Contains(string(idx), want) {
			t.Errorf("INDEX missing %q", want)
		}
	}
	if strings.Contains(string(idx), "### Session s01") || strings.Contains(string(idx), "**Task t01") {
		t.Error("headings should drop the \"Session\"/\"Task\" label word, keeping only the id")
	}

	// The r3 detail file carries the session header and delta section.
	r3 := a.Sessions[0].Tasks[1].Recs[0]
	body, err := os.ReadFile(filepath.Join(dir, r3.DetailFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"**会话 s01** · **任务 t02**",
		"🆕 #7 user",       // per-message emoji on the increment
		"换个方向",            // NewInstruction still surfaces in meta line
		"本轮增量（相对上一轮,+5 条", // footer summary on the message list (5 msgs in this r3 delta)
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("r3 detail missing %q", want)
		}
	}
}

func TestAnthropicMetadataSessionKey(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	ts := time.Date(2026, 7, 9, 12, 0, 0, 0, zone)
	body := map[string]any{
		"model": "claude", "stream": false,
		"system":   "be helpful",
		"metadata": map[string]any{"user_id": "user_abc_account_def_session_11112222-3333"},
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "写个函数"},
			}},
		},
	}
	resp := map[string]any{
		"model": "claude", "stop_reason": "tool_use",
		"content": []any{
			map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{}},
		},
		"usage": map[string]any{"input_tokens": 10, "output_tokens": 5},
	}
	rec := audit.Record{TS: ts, Model: "claude", Protocol: "anthropic", Outcome: "ok",
		Client: audit.Exchange{
			Request:  audit.Message{Headers: http.Header{}, Body: body},
			Response: &audit.Message{Status: 200, Headers: http.Header{}, Body: resp},
		}}
	// Second turn, same metadata session: history + assistant tool_use +
	// tool_result-only user message (must NOT count as a new instruction).
	body2 := map[string]any{
		"model": "claude", "stream": false,
		"system":   "be helpful",
		"metadata": map[string]any{"user_id": "user_abc_account_def_session_11112222-3333"},
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "写个函数"},
			}},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{}},
			}},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "done"},
			}},
		},
	}
	rec2 := rec
	rec2.TS = ts.Add(30 * time.Second)
	rec2.Client.Request.Body = body2

	path := writeJSONL(t, []audit.Record{rec, rec2})
	a, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(a.Sessions))
	}
	s := a.Sessions[0]
	if !strings.HasPrefix(s.Recs[0].SessKey, "meta:session_") {
		t.Errorf("session key = %q, want metadata-derived", s.Recs[0].SessKey)
	}
	if len(s.Tasks) != 1 {
		t.Errorf("tasks = %d, want 1 (tool_result user msg must not open a task)", len(s.Tasks))
	}
	if got := s.Recs[0].ToolCalls; len(got) != 1 || got[0] != "Bash" {
		t.Errorf("anthropic tool calls = %v", got)
	}
	if s.Title != "写个函数" {
		t.Errorf("title = %q", s.Title)
	}
}

// TestCollapsedSessionRowShowsRealAverages locks in that the merged/collapsed
// scheduled-session row in vmr-report.md's Agent 会话 table renders real
// computed values — avg tokens, avg messages, TTFT, duration — instead of
// placeholder dashes: mergeIntoCollapsed accumulates TokensKnown/
// MessagesKnown/TTFTMSSum/DurMSSum across the merged sessions, and the
// render code must actually read them. Two single-request heartbeat firings
// (different content, so they land in separate 1-request sessions and both
// qualify for collapsing) with distinct token/message/latency values verify
// the merged row shows real computed values.
func TestCollapsedSessionRowShowsRealAverages(t *testing.T) {
	lines := `{"ts":"2026-07-09T09:00:00+08:00","dur_ms":4000,"ttft_ms":1000,"model":"agent","protocol":"openai","outcome":"ok","client":{"request":{"body":{"model":"agent","messages":[{"role":"system","content":"sys"},{"role":"user","content":"heartbeat A [OpenClaw heartbeat poll]"}]}},"response":{"status":200,"body":{"model":"agent","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":100,"completion_tokens":10}}}},"attempts":[{"endpoint":"openai/prov/real-1","response":{"status":200}}]}
{"ts":"2026-07-09T10:00:00+08:00","dur_ms":6000,"ttft_ms":3000,"model":"agent","protocol":"openai","outcome":"ok","client":{"request":{"body":{"model":"agent","messages":[{"role":"system","content":"sys"},{"role":"user","content":"heartbeat B [OpenClaw heartbeat poll]"}]}},"response":{"status":200,"body":{"model":"agent","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":200,"completion_tokens":20}}}},"attempts":[{"endpoint":"openai/prov/real-1","response":{"status":200}}]}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2 (distinct content must not group together)", len(a.Sessions))
	}
	rep, err := Build([]string{path}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	rep.Sessions = a.SessionRows()
	md := Markdown(rep)

	row := ""
	for _, line := range strings.Split(md, "\n") {
		if strings.Contains(line, "heartbeat 单发会话") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("collapsed heartbeat row not found in:\n%s", md)
	}
	for _, want := range []string{
		"×2",            // both firings merged into one row
		"150 / 15",      // avg tokens in/out: (100+200)/2, (10+20)/2
		"| 2.0 |",       // avg messages: (2+2)/2 — system+user each record
		"1000ms / 3.0s", // TTFT p50/p95: raw values 1000,3000 (not "-/-")
		"4.0s / 6.0s",   // duration p50/p95: raw values 4000,6000 (not "-/-")
	} {
		if !strings.Contains(row, want) {
			t.Errorf("collapsed row missing %q:\n%s", want, row)
		}
	}
	if strings.Contains(row, "| - | - | -/- | -/- |") {
		t.Error("collapsed row still has the hardcoded placeholder dashes")
	}
}

func TestMarkdownToolsAndSessions(t *testing.T) {
	path, _ := fixture(t)
	a, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Build([]string{path}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	rep.Tools = a.ToolShapes()
	rep.Sessions = a.SessionRows()
	rep.Workloads = a.Workloads()
	md := Markdown(rep)
	for _, want := range []string{
		"## Agent 会话",
		"[s01]",
		"[s02](./details/", " ← s01",
		"Req/Fall/Trunc", "图片/压缩",
		"## 工具使用",
		"调用过的工具",
		"exec",
		"从未调用（1 个，",
		"1. write", // numbered list
		"## 工作负载",
		"Tool 调用",
		"| interactive |",
		"| compaction |",
		"## 每小时活跃度",
		"| 10:00 |",
		"**finish_reason 数量及占比**",
		"tool_calls×",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}

func TestBuildRowHealthFields(t *testing.T) {
	path, _ := fixture(t)
	rep, err := Build([]string{path}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var agent *Row
	for i := range rep.Rows {
		if rep.Rows[i].Model == "agent" {
			agent = &rep.Rows[i]
		}
	}
	if agent == nil {
		t.Fatal("no agent row")
	}
	// fixture: r1/r2/r4 finish tool_calls; r3/r5 stop; compaction stop.
	if agent.FinishReasons["tool_calls"] != 3 || agent.FinishReasons["stop"] != 3 {
		t.Errorf("finish reasons = %v", agent.FinishReasons)
	}
	if agent.Truncated != 1 {
		t.Errorf("truncated = %d, want 1", agent.Truncated)
	}
	// r1/r2/r4 carry reasoning_tokens=4 each in their usage chunk.
	if agent.TokensReasoning != 12 {
		t.Errorf("reasoning tokens = %d, want 12", agent.TokensReasoning)
	}
	if len(rep.Hours) == 0 {
		t.Fatal("no hours rows")
	}
	var reqs int
	for _, h := range rep.Hours {
		if h.Date != "2026-07-09" || h.Hour != 10 {
			t.Errorf("unexpected hour row %+v", h)
		}
		reqs += h.Requests
	}
	if reqs != 6 {
		t.Errorf("hourly requests = %d, want 6", reqs)
	}
}

func TestExtractFinish(t *testing.T) {
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"x\"},\"finish_reason\":null}]}\n" +
		"data: {\"choices\":[{\"finish_reason\":\"length\",\"delta\":{}}]}\n" +
		"data: [DONE]\n"
	if got := extractFinish(sse); got != "length" {
		t.Errorf("sse finish = %q", got)
	}
	anth := "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n"
	if got := extractFinish(anth); got != "end_turn" {
		t.Errorf("anthropic finish = %q", got)
	}
	jsonBody := map[string]any{"choices": []any{map[string]any{"finish_reason": "stop"}}}
	if got := extractFinish(jsonBody); got != "stop" {
		t.Errorf("json finish = %q", got)
	}
	if got := extractFinish("no finish here"); got != "" {
		t.Errorf("empty finish = %q", got)
	}
}

func TestIsRealUserScaffolding(t *testing.T) {
	for _, text := range []string{
		"OpenClaw runtime context for the immediately preceding user message.\nInternal.",
		"[Thu] Conversation info (untrusted metadata):\n```json\n{}\n```",
		"Attached image(s) from tool result:",
		"The conversation history before this point was compacted into the following summary:\n<summary>x</summary>",
	} {
		if isRealUser(chatMessage{Role: "user", Text: text}, nil, -1) {
			t.Errorf("scaffolding counted as real user: %q", text[:40])
		}
	}
	if !isRealUser(chatMessage{Role: "user", Text: "帮我修个 bug"}, nil, -1) {
		t.Error("real instruction not recognized")
	}
}

// TestRealUserTextStripsEnvelope locks in that a genuine user instruction
// survives OpenClaw's envelope: OpenClaw glues its "Conversation info
// (untrusted metadata)" / "Sender (untrusted metadata)" JSON routing blocks
// onto the FRONT of genuine asks, not just onto pure scaffolding pings, so
// the envelope must be stripped rather than the whole message discarded —
// discarding it would lose the actual instruction and make task titles fall
// back to an unrelated earlier message.
func TestRealUserTextStripsEnvelope(t *testing.T) {
	wrapped := "[Thu 2026-07-09 06:48 GMT+8] Conversation info (untrusted metadata):\n" +
		"```json\n{\"chat_id\":\"user:ou_x\"}\n```\n\n" +
		"Sender (untrusted metadata):\n```json\n{\"id\":\"ou_x\"}\n```\n\n" +
		"OK，基于你为每个风格设计的提示词，调用 ai-script 批量生成 logo 设计图。"
	text, ok := realUserText(chatMessage{Role: "user", Text: wrapped}, nil, -1)
	if !ok {
		t.Fatal("envelope-wrapped real instruction not recognized")
	}
	if !strings.Contains(text, "OK，基于你为每个风格设计的提示词") {
		t.Errorf("stripped text lost the real instruction: %q", text)
	}
	if strings.Contains(text, "chat_id") {
		t.Errorf("stripped text still carries the JSON envelope: %q", text)
	}
}

// TestNoReplyMergesRetryIntoSameTask covers OpenClaw's skip-on-memory-flush
// pattern: when a turn's assistant reply is empty or just "NO_REPLY", the
// LLM never actually acted on the user's instruction. The next turn (even
// though it adds a genuinely new real-user message near the end, which would
// normally open a new task) must stay in the SAME task — it's a continuation
// of the instruction the parent skipped, not a fresh ask.
func TestNoReplyMergesRetryIntoSameTask(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, zone) }
	sys := msg("system", "You are a personal assistant.")
	u1 := msg("user", "[Thu 2026-07-09 10:00 GMT+8] 任务开始：写日报")
	r1 := mkRec(at(0), "1111aaaa1111aaaa1111aaaa1111aaaa", []any{sys, u1}, nil, sseText("NO_REPLY"))
	u2 := msg("user", "[Thu 2026-07-09 10:06 GMT+8] 继续，把日报写完")
	r2 := mkRec(at(6), "1111aaaa1111aaaa1111aaaa1111aaaa", []any{sys, u1, u2}, nil, sseText("好的，日报如下"))

	path := writeJSONL(t, []audit.Record{r1, r2})
	a, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(a.Sessions))
	}
	s := a.Sessions[0]
	if len(s.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1 (NO_REPLY parent should keep the retry in the same task): %+v", len(s.Tasks), s.Tasks)
	}
	if len(s.Tasks[0].Recs) != 2 {
		t.Fatalf("task recs = %d, want 2", len(s.Tasks[0].Recs))
	}
	if !s.Tasks[0].Recs[0].NoReply {
		t.Error("r1 should be flagged NoReply")
	}
	if got := s.Tasks[0].Recs[1].TaskSeq; got != 2 {
		t.Errorf("r2 task seq = %d, want 2", got)
	}
}
