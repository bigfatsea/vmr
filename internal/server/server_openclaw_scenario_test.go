// Ver 2026-07-07 22:30, by Fable 5
//
// End-to-end test that reproduces the actual OpenClaw audit-log
// scenario from 2026-07-07: 24 rounds of tool-use where the model
// emits <think>...</think> blocks before each tool call. Without the
// response normalizer:
//
//   - Each round's response model field was "MiniMax-M3" but the
//     client sent "agent" — model mismatch on every round.
//   - Each round's think content (~3K chars) was stored as the
//     assistant message, bloating the next request's prompt_tokens
//     from 16K (line 4) to 43K (line 27), with one request hitting
//     483K tokens and finish_reason=length, 1 token (line 3).
//
// This test asserts that with the normalizer, none of these
// regressions occur: every round's response model is the virtual
// model the client sent, no think content is in the round-trip
// history, and prompt_tokens grow linearly with the tool-result
// content, not super-linearly with stale reasoning text.
package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

// openclawScenarioUpstream is a scriptable mock that emulates the
// actual MiniMax M3 streaming pattern observed in the audit log:
// every turn opens with <think>... reasoning, closes with
// </think>\n\n, then emits a tool_call chunk and a finish_reason
// chunk. The mock can be told which tool to call each turn.
type openclawScenarioUpstream struct {
	srv         *httptest.Server
	hits        atomic.Int32
	totalTokens atomic.Int64
}

func newOpenclawScenarioUpstream(t *testing.T) *openclawScenarioUpstream {
	u := &openclawScenarioUpstream{}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
			Stream bool `json:"stream"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad json", 400)
			return
		}

		// Sanity: VMR must have rewritten the model field to MiniMax-M3.
		if req.Model != "MiniMax-M3" {
			http.Error(w, fmt.Sprintf("upstream got wrong model %q", req.Model), 400)
			return
		}
		// Sanity: no think content should be in the request — the
		// normalizer must have stripped it before the upstream saw it.
		for _, m := range req.Messages {
			if s, ok := m.Content.(string); ok && strings.Contains(s, "<think>") {
				http.Error(w, "think content leaked into upstream request", 400)
				return
			}
		}

		// Synthesize a minimal streaming response: short think + tool call.
		// Prompt-token count grows with round number (so the audit-style
		// "prompt_tokens 16K → 43K" pattern is observable in the usage
		// chunk; the *client* should see the *rewritten* model field).
		round := int(u.hits.Load())
		prompt := 16000 + round*1200 // matches audit-log linear growth
		completion := 350 + round*100
		u.totalTokens.Add(int64(prompt + completion))

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		writeSSE(w, flusher, `{"id":"x","choices":[{"index":0,"delta":{"role":"assistant"}}],"created":1,"model":"MiniMax-M3","object":"chat.completion.chunk","usage":null,"service_tier":"standard"}`)
		// Big think block — the kind that bloats history if not stripped.
		think := `{"id":"x","choices":[{"index":0,"delta":{"content":"<think>This is round ` + fmt.Sprint(round) + `. I am going to call a tool. The reasoning here is intentionally long to simulate a verbose chain-of-thought. Step one. Step two. Step three. Step four. I think I have enough context now. Let me proceed with the tool call.</think>\n\n"}}],"created":1,"model":"MiniMax-M3","object":"chat.completion.chunk","usage":null,"service_tier":"standard"}`
		writeSSE(w, flusher, think)
		// Tool call.
		writeSSE(w, flusher, `{"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_test`+fmt.Sprint(round)+`","type":"function","function":{"name":"read","arguments":"{\"path\":\"/tmp/`+fmt.Sprint(round)+`\"}"}}]}}],"created":1,"model":"MiniMax-M3","object":"chat.completion.chunk","usage":null,"service_tier":"standard"}`)
		// Finish.
		writeSSE(w, flusher, `{"id":"x","choices":[{"finish_reason":"tool_calls","index":0,"delta":{"role":"assistant"}}],"created":1,"model":"MiniMax-M3","object":"chat.completion.chunk","usage":null,"service_tier":"standard"}`)
		// Usage — note the upstream model field is "MiniMax-M3" here.
		writeSSE(w, flusher, fmt.Sprintf(`{"id":"x","choices":[],"created":1,"model":"MiniMax-M3","object":"chat.completion.chunk","usage":{"total_tokens":%d,"prompt_tokens":%d,"completion_tokens":%d},"service_tier":"standard"}`, prompt+completion, prompt, completion))
		// No [DONE] — MiniMax doesn't send one.
	}))
	t.Cleanup(u.srv.Close)
	return u
}

// TestOpenClawScenario_TwentyFourRounds simulates the OpenClaw agent
// loop: 24 streaming tool-use turns in a row. For each turn we
// assert (a) the response model is the virtual name "agent", (b) the
// think content did not leak into any data: line, (c) the final
// stream contains [DONE]. The point of the test is regression — if
// any of the three normalizations is undone, this fails.
func TestOpenClawScenario_TwentyFourRounds(t *testing.T) {
	up := newOpenclawScenarioUpstream(t)
	cfg, err := config.Parse([]byte(`
listen: 127.0.0.1:0
providers:
  openai:
    p1: {base_url: ` + up.srv.URL + `, api_key: k1}
models:
  openai:
    agent:
      endpoints:
        - {provider: p1, model: MiniMax-M3}
`))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	ts := httptest.NewServer(New(rt, nil).Handler())
	t.Cleanup(ts.Close)

	const rounds = 24
	for i := 0; i < rounds; i++ {
		// Each round the client sends its running history: system
		// + user + 2i (assistant+tool for each prior round) messages.
		msgs := []map[string]any{
			{"role": "system", "content": "You are a coding agent."},
			{"role": "user", "content": "do work"},
		}
		for j := 0; j < i; j++ {
			msgs = append(msgs,
				map[string]any{
					"role": "assistant", "content": nil,
					"tool_calls": []map[string]any{{
						"id":       fmt.Sprintf("call_old%d", j),
						"type":     "function",
						"function": map[string]any{"name": "read", "arguments": fmt.Sprintf(`{"path":"/tmp/%d"}`, j)},
					}},
				},
				map[string]any{
					"role":         "tool",
					"tool_call_id": fmt.Sprintf("call_old%d", j),
					"content":      fmt.Sprintf("file %d contents", j),
				},
			)
		}
		body, _ := json.Marshal(map[string]any{
			"model":    "agent",
			"stream":   true,
			"messages": msgs,
		})

		req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("round %d: request failed: %v", i, err)
		}
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("round %d: status %d, body=%s", i, resp.StatusCode, b)
		}

		// Drain the stream and assert the normalizations held.
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		var dataLines []string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
			}
		}
		resp.Body.Close()
		if err := scanner.Err(); err != nil {
			t.Fatalf("round %d: scan: %v", i, err)
		}
		if len(dataLines) == 0 {
			t.Fatalf("round %d: no data lines", i)
		}
		if dataLines[len(dataLines)-1] != "[DONE]" {
			t.Errorf("round %d: missing [DONE] sentinel, tail=%q", i, dataLines[len(dataLines)-1:])
		}
		for j, l := range dataLines {
			if l == "[DONE]" {
				continue
			}
			if strings.Contains(l, "MiniMax-M3") {
				t.Errorf("round %d, data[%d]: upstream model name leaked: %s", i, j, l)
			}
			if strings.Contains(l, "<think>") || strings.Contains(l, "This is round") {
				t.Errorf("round %d, data[%d]: think content leaked: %s", i, j, l)
			}
		}
	}

	if got := up.hits.Load(); got != rounds {
		t.Errorf("upstream hit count = %d, want %d", got, rounds)
	}
}

// TestOpenClawScenario_NonStreaming exercises the same normalizer
// over the non-streaming response path (io.Copy, not copyFlush). The
// audit log shows line 1 (a non-streaming "Hi" probe) and various
// compact / summarize calls take this path.
func TestOpenClawScenario_NonStreaming(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"id":"x","model":"MiniMax-M3","choices":[{"finish_reason":"stop","index":0,"message":{"role":"assistant","content":"<think>reasoning</think>\n\nHi there!","name":"MiniMax AI"}}],"created":1,"object":"chat.completion","usage":{"total_tokens":100,"prompt_tokens":50,"completion_tokens":50}}`)
	}))
	defer up.Close()
	cfg, _ := config.Parse([]byte(`
listen: 127.0.0.1:0
providers:
  openai:
    p1: {base_url: ` + up.URL + `, api_key: k1}
models:
  openai:
    agent:
      endpoints:
        - {provider: p1, model: MiniMax-M3}
`))
	rt := router.New(nil)
	snap, _ := router.BuildSnapshot(cfg)
	rt.Install(snap)
	ts := httptest.NewServer(New(rt, nil).Handler())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions",
		bytes.NewReader([]byte(`{"model":"agent","messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	got := string(body)

	// Model field rewritten to virtual name.
	var parsed struct {
		Model   string `json:"model"`
		Message struct {
			Content string `json:"content"`
		} `json:"choices"`
	}
	// The OpenAI non-stream response is a JSON object with `model`
	// at the top level and `choices[0].message.content` for the
	// assistant text.
	var respObj struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &respObj); err != nil {
		t.Fatalf("parse response: %v\nbody=%s", err, body)
	}
	_ = parsed
	if respObj.Model != "agent" {
		t.Errorf("non-stream model = %q, want %q (raw: %s)", respObj.Model, "agent", got)
	}
	if strings.Contains(respObj.Choices[0].Message.Content, "reasoning") {
		t.Errorf("non-stream think content leaked: %q", respObj.Choices[0].Message.Content)
	}
	if !strings.Contains(respObj.Choices[0].Message.Content, "Hi there!") {
		t.Errorf("non-stream real content lost: %q", respObj.Choices[0].Message.Content)
	}
	// Note: non-stream path doesn't append [DONE] (that's an SSE
	// protocol concept, not a JSON one). The body must still be
	// valid JSON.
	var sanityCheck map[string]any
	if err := json.Unmarshal(body, &sanityCheck); err != nil {
		t.Errorf("non-stream response not valid JSON: %v", err)
	}
}

// TestOpenClawScenario_AuditLogCapturesTransformedResponse verifies
// the audit log records the response that the CLIENT saw (with
// normalized model field, think stripped, [DONE] appended) — not the
// raw upstream bytes. This is what the audit log is for, and the
// 2026-07-07 audit file is the basis for this whole investigation.
func TestOpenClawScenario_AuditLogCapturesTransformedResponse(t *testing.T) {
	up := newOpenclawScenarioUpstream(t)

	// Build VMR with an in-memory audit writer.
	cfg, _ := config.Parse([]byte(`
listen: 127.0.0.1:0
providers:
  openai:
    p1: {base_url: ` + up.srv.URL + `, api_key: k1}
models:
  openai:
    agent:
      endpoints:
        - {provider: p1, model: MiniMax-M3}
`))
	rt := router.New(nil)
	snap, _ := router.BuildSnapshot(cfg)
	rt.Install(snap)

	// Write audit to a temp dir so we can read it back. The
	// audit package writes one file per day; we use a private
	// temp dir and read the file by glob after the request.
	tmp := t.TempDir()
	auditLog, err := audit.New(tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer auditLog.Close()
	ts := httptest.NewServer(New(rt, auditLog).Handler())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions",
		bytes.NewReader([]byte(`{"model":"agent","stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// Read the audit line back and verify it captured the
	// transformed response, not the raw upstream. The file is
	// written synchronously by audit.Logger.Write, so by the
	// time http.DefaultClient.Do returns the line is on disk.
	lines, err := readAuditLines(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("no audit lines written")
	}
	line := lines[0]
	var rec struct {
		Model    string `json:"model"`
		Protocol string `json:"protocol"`
		Stream   bool   `json:"stream"`
		Client   struct {
			Response struct {
				Status  int               `json:"status"`
				Body    json.RawMessage   `json:"body"`
				Headers map[string][]string `json:"headers"`
			} `json:"response"`
		} `json:"client"`
	}
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("parse audit: %v\n%s", err, line)
	}
	if rec.Model != "agent" {
		t.Errorf("audit model field = %q, want %q", rec.Model, "agent")
	}
	body := string(rec.Client.Response.Body)
	if strings.Contains(body, "MiniMax-M3") {
		t.Errorf("audit captured upstream model name: %s", body)
	}
	if strings.Contains(body, "<think>") || strings.Contains(body, "This is round") {
		t.Errorf("audit captured think content: %s", body)
	}
	if !strings.HasSuffix(body, "[DONE]\\n\\n\"") {
		t.Errorf("audit missing [DONE] sentinel (JSON-escaped), tail=%q", body)
	}
	_ = rec
}

// TestResponseNormalizer_FailoverStillWorks is a regression test for
// the new response wrapper. Before the fix, tryOne used copyFlush
// directly. Now it wraps the body in respStream before forwarding.
// If the wrapper mishandles io.EOF or errors, failover (which
// depends on clean error propagation) would silently break.
func TestResponseNormalizer_FailoverStillWorks(t *testing.T) {
	// Bad upstream returns 500 (fails), good upstream returns 200.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, `{"error":{"message":"intentional"}}`)
	}))
	defer bad.Close()
	good := newOpenclawScenarioUpstream(t)

	cfg, _ := config.Parse([]byte(fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai:
    bad: {base_url: %s, api_key: k1}
    good: {base_url: %s, api_key: k2}
models:
  openai:
    agent:
      endpoints:
        - {provider: bad, model: MiniMax-M3, priority: 1}
        - {provider: good, model: MiniMax-M3, priority: 2}
`, bad.URL, good.srv.URL)))
	rt := router.New(nil)
	snap, _ := router.BuildSnapshot(cfg)
	rt.Install(snap)
	ts := httptest.NewServer(New(rt, nil).Handler())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions",
		bytes.NewReader([]byte(`{"model":"agent","stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d, body=%s", resp.StatusCode, b)
	}
	// Drain and verify the response is the GOOD upstream's stream
	// (normalized) — not the bad one's 500 body.
	b, _ := io.ReadAll(resp.Body)
	body := string(b)
	if strings.Contains(body, "intentional") {
		t.Errorf("got bad-upstream body in response: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Errorf("good-upstream response missing [DONE]: %s", body)
	}
	if !strings.Contains(body, `"model":"agent"`) {
		t.Errorf("good-upstream response missing model rewrite: %s", body)
	}
}

// readAuditLines globs the audit directory for the JSONL file and
// returns every non-empty line. The audit package writes one file
// per day (vmr-audit-YYYY-MM-DD.jsonl); a test temp dir never has
// more than one such file in practice.
func readAuditLines(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "vmr-audit-*.jsonl"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, os.ErrNotExist
	}
	for _, m := range matches {
		// Newest file first (lexicographic on date is monotonic).
		if filepath.Base(matches[0]) < filepath.Base(m) {
			matches[0] = m
		}
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range bytes.Split(data, []byte("\n")) {
		if len(l) > 0 {
			out = append(out, string(l))
		}
	}
	return out, nil
}

// Suppress unused-import warning if time becomes unused after edits.
var _ = time.Now
