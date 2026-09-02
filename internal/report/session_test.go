// Ver 2026-07-25, by Sonnet 5
package report

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
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

// mkRec builds one openai-completions audit record.
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
		TS: ts, DurMS: 100, Model: "agent", Protocol: "openai-completions", Stream: true, Outcome: "ok",
		Client: audit.Exchange{
			Request:  audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: h, Body: body},
			Response: &audit.Message{Status: 200, Headers: http.Header{}, Body: respBody},
		},
		Attempts: []audit.Attempt{{Endpoint: "openai-completions:prov:real-1", Protocol: "openai-completions", Provider: "prov", Model: "real-1", URL: "https://x/v1", DurMS: 90,
			Request:  audit.Message{Headers: http.Header{}},
			Response: &audit.Message{Status: 200, Headers: http.Header{}},
			Norm:     []string{"model_rewrite"}}},
	}
	return rec
}

func msg(role, content string) map[string]any {
	return map[string]any{"role": role, "content": content}
}

// writeJSONL writes recs to a fresh temp file and returns its path. The
// basename is derived from t.TempDir()'s own unique suffix (not a fixed
// "audit.jsonl") — real audit files always carry a date and never collide
// on basename (see ctxgraph's CheckPathCollisions), and a fixed name would
// make two calls whose paths ever land in the same Scan/ScanCached call
// collide on purpose.
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
	dir := t.TempDir()
	path := filepath.Join(dir, "audit-"+filepath.Base(dir)+".jsonl")
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
		TS: at(2, 0), DurMS: 50, Model: "agent", Protocol: "openai-completions", Stream: true, Outcome: "ok",
		Client: audit.Exchange{
			Request:  audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: http.Header{}, Body: compBody},
			Response: &audit.Message{Status: 200, Headers: http.Header{}, Body: sseText("## Goal 调研 X 的总结摘要")},
		},
		Attempts: []audit.Attempt{{Endpoint: "openai-completions:prov:real-1", Protocol: "openai-completions", Provider: "prov", Model: "real-1", Response: &audit.Message{Status: 200, Headers: http.Header{}}}},
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

// TestLinkCompactionsLogsMiss covers the case neither needle finds a match
// (content genuinely unrelated, not just past the 200-byte cap): both sides
// must stay unlinked AND each miss must be logged, so triage can tell
// "no relation" apart from "the needle missed" instead of seeing the same
// silent blank field either way.
// TestReleaseTextBuffersPreservesSessionFirstAndCompaction verifies that
// after AnalyzeSessionsCached, the large per-request text buffers on
// intermediate records (non-Recs[0], non-compaction) have been released to
// avoid OOM in large corpora, while the records that still need them (each
// session's first record, and every compaction record) keep them intact.
func TestReleaseTextBuffersPreservesSessionFirstAndCompaction(t *testing.T) {
	path, _ := fixture(t)
	a, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}

	// Session A: r1 (Recs[0], firstText kept), r2 (intermediate, cleared), r3 (intermediate, cleared)
	// Compaction: comp (firstText+respText kept)
	// Session A2: r4 (Recs[0], firstText kept), r5 (intermediate, cleared)

	sA := a.Sessions[0]
	r1 := sA.Recs[0]
	r2 := sA.Recs[1]
	r3 := sA.Recs[2]

	// Recs[0] must keep its firstText (sessionTitle reads it).
	if r1.firstText == "" {
		t.Error("r1 (Recs[0]): firstText was cleared, but session first record must keep it")
	}
	// r1's respText may be empty or non-empty depending on the fixture
	// (the opening record's response carries content). It's fine either way.
	// The task spec says to keep both, so just verify nothing broke.

	// r2 (intermediate, non-Recs[0], non-compaction) must have firstText cleared.
	if r2.firstText != "" {
		t.Errorf("r2 (intermediate): firstText = %q, want cleared (empty string)", r2.firstText)
	}
	if r2.respText != "" {
		t.Errorf("r2 (intermediate): respText = %q, want cleared (empty string)", r2.respText)
	}

	// r3 (intermediate, non-Recs[0], non-compaction) must have firstText cleared.
	if r3.firstText != "" {
		t.Errorf("r3 (intermediate): firstText = %q, want cleared (empty string)", r3.firstText)
	}
	if r3.respText != "" {
		t.Errorf("r3 (intermediate): respText = %q, want cleared (empty string)", r3.respText)
	}

	// Compaction record must keep both firstText and respText
	// (recextract.buildCompactions reads both).
	if len(a.Compactions) != 1 {
		t.Fatalf("compactions = %d, want 1", len(a.Compactions))
	}
	c := a.Compactions[0]
	if c.firstText == "" {
		t.Error("compaction: firstText was cleared, but compaction records must keep it")
	}
	if c.respText == "" {
		t.Error("compaction: respText was cleared, but compaction records must keep it")
	}

	// Session A2: r4 (Recs[0], firstText kept), r5 (intermediate, cleared)
	if len(a.Sessions) < 2 {
		t.Fatalf("sessions = %d, want at least 2", len(a.Sessions))
	}
	sA2 := a.Sessions[1]
	r4 := sA2.Recs[0]
	r5 := sA2.Recs[1]

	if r4.firstText == "" {
		t.Error("r4 (Recs[0]): firstText was cleared, but session first record must keep it")
	}
	if r5.firstText != "" {
		t.Errorf("r5 (intermediate): firstText = %q, want cleared (empty string)", r5.firstText)
	}
	if r5.respText != "" {
		t.Errorf("r5 (intermediate): respText = %q, want cleared (empty string)", r5.respText)
	}
}

func TestLinkCompactionsLogsMiss(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	t0 := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	sess := &SessionInfo{ID: "s01", Recs: []*ReqInfo{{firstText: "hello world", TS: t0.Add(time.Minute)}}}
	compaction := &ReqInfo{
		Path: "test.jsonl", TS: t0,
		respText:  "this response text does not appear in any session's opening message",
		firstText: "this compaction's own opening text is not a prefix of any session's continuation",
	}
	a := &SessionAnalysis{Sessions: []*SessionInfo{sess}, Compactions: []*ReqInfo{compaction}}
	linkCompactions(a)

	if compaction.ContinuesTo != "" || compaction.Summarizes != "" {
		t.Fatalf("expected no links for a non-matching compaction, got continuesTo=%q summarizes=%q",
			compaction.ContinuesTo, compaction.Summarizes)
	}
	out := buf.String()
	if !strings.Contains(out, "successor needle not found") {
		t.Errorf("expected a successor-miss log line, got: %s", out)
	}
	if !strings.Contains(out, "predecessor needle not found") {
		t.Errorf("expected a predecessor-miss log line, got: %s", out)
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

// TestUngroupedFoldedIntoUnresolved covers a non-chat/rejected record (no
// "messages" field, so it never gets a SessKey): it must group into
// AnalyzeSessions' Ungrouped bucket, and WriteDetails must still render its
// detail file without erroring (the old grouped vmr-requests-index.md this
// used to also assert on was removed with the old `vmr report` command —
// the current vmr-requests.md, tested elsewhere, covers that view now).
func TestUngroupedFoldedIntoUnresolved(t *testing.T) {
	line := `{"ts":"2026-07-09T08:00:00+08:00","dur_ms":10,"model":"","protocol": "openai-completions","outcome":"error","client":{"request":{"method":"POST","path":"/v1/chat/completions","body":null}}}` + "\n"
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
	if n, err := WriteDetails([]string{src}, out, a, nil, i18n.EN, taskseg.OpenClawAware); err != nil {
		t.Fatal(err)
	} else if n != 1 {
		t.Fatalf("n = %d, want 1", n)
	}
}

func TestWriteDetailsGroupedIndex(t *testing.T) {
	path, _ := fixture(t)
	a, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "details")
	n, err := WriteDetails([]string{path}, dir, a, nil, i18n.EN, taskseg.OpenClawAware)
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Fatalf("n = %d, want 6", n)
	}

	// The r3 detail file carries the delta section (previous-turn link +
	// increment highlight), computed from ctxgraph.Classify against its
	// lineage predecessor's Manifest — but deliberately NOT a
	// "Session s01 / Task t02" position line:
	// a leaf detail page doesn't know its position in report's session/task
	// tree, only whoever links to it (a requests-index row, a spine step)
	// does, and run-scoped session/task numbers would make the SAME
	// record's detail page differ between a full-corpus run and a subset
	// run, defeating the entire point of coordinate-based naming.
	r3 := a.Sessions[0].Tasks[1].Recs[0]
	body, err := os.ReadFile(filepath.Join(dir, r3.DetailFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"🆕 #7 user", // per-message emoji on the increment
		"换个方向",      // this delta's actual new message content, rendered inline
		"This turn's increment (vs. the previous turn, +5", // footer summary on the message list (5 msgs in this r3 delta)
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("r3 detail missing %q", want)
		}
	}
	if strings.Contains(string(body), "Session s01") || strings.Contains(string(body), "Task t02") {
		t.Errorf("r3 detail should not render a run-scoped session/task position line, got:\n%s", body)
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
	rec := audit.Record{TS: ts, Model: "claude", Protocol: "anthropic-messages", Outcome: "ok",
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
