// Ver 2026-07-30, by Sonnet 5
//
// Integration tests for the response-side normalizer: model-field
// rewrite + think-block stripping + [DONE] sentinel, exercised through
// the full VMR stack (real HTTP server, real streaming response, real
// HTTP client consuming the stream).
package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vmr/internal/config"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

// streamingUpstream is a mock that emits a MiniMax-shaped streaming
// response: many small content chunks followed by a tool_call chunk,
// then a finish_reason chunk, then a usage chunk. The first
// delta.content is the start of a <think> block, the last non-tool
// delta.content closes it with "</think>\n\n", the tool_call chunk
// has the upstream's model name "MiniMax-M3" in it.
type streamingUpstream struct {
	srv *httptest.Server
}

func newStreamingUpstream(t *testing.T) *streamingUpstream {
	t.Helper()
	u := &streamingUpstream{}
	u.srv = newJSONUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)

		// Chunk 0: role marker
		writeSSE(w, flusher, `{"id":"x","choices":[{"index":0,"delta":{"role":"assistant"}}],"created":1,"model":"MiniMax-M3","object":"chat.completion.chunk","usage":null,"service_tier":"standard"}`)
		// Chunk 1: thinking begins with the <think> opener (matches
		// MiniMax M3's real output where the first content chunk
		// includes the literal tag inline).
		writeSSE(w, flusher, `{"id":"x","choices":[{"index":0,"delta":{"content":`+quote("<think>Let me ")+`}}],"created":1,"model":"MiniMax-M3","object":"chat.completion.chunk","usage":null,"service_tier":"standard"}`)
		// Chunks 2-3: more thinking content (no marker, plain text)
		for _, c := range []string{"check the ", "memory file."} {
			writeSSE(w, flusher, `{"id":"x","choices":[{"index":0,"delta":{"content":`+quote(c)+`}}],"created":1,"model":"MiniMax-M3","object":"chat.completion.chunk","usage":null,"service_tier":"standard"}`)
		}
		// Chunk 4: close think, no tool call
		writeSSE(w, flusher, `{"id":"x","choices":[{"index":0,"delta":{"content":"</think>\n\nReal answer."}}],"created":1,"model":"MiniMax-M3","object":"chat.completion.chunk","usage":null,"service_tier":"standard"}`)
		// Chunk 5: finish
		writeSSE(w, flusher, `{"id":"x","choices":[{"finish_reason":"stop","index":0,"delta":{"role":"assistant"}}],"created":1,"model":"MiniMax-M3","object":"chat.completion.chunk","usage":null,"service_tier":"standard"}`)
		// Chunk 6: usage — note the model field is also "MiniMax-M3"
		writeSSE(w, flusher, `{"id":"x","choices":[],"created":1,"model":"MiniMax-M3","object":"chat.completion.chunk","usage":{"total_tokens":42,"prompt_tokens":30,"completion_tokens":12},"service_tier":"standard"}`)
		// Upstream closes WITHOUT data: [DONE]
	})
	return u
}

func writeSSE(w http.ResponseWriter, f http.Flusher, payload string) {
	io.WriteString(w, "data: "+payload+"\n\n")
	if f != nil {
		f.Flush()
	}
}

func quote(s string) string { return `"` + s + `"` }

func TestResponse_NonStreamingNoDoneSentinel(t *testing.T) {
	// Non-streaming responses are single JSON objects — the SSE
	// [DONE] sentinel must NOT be appended, or the response would
	// be invalid JSON.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"id":"x","model":"MiniMax-M3","choices":[{"finish_reason":"stop","index":0,"message":{"role":"assistant","content":"<think>reasoning</think>\n\nHi there!"}}],"created":1,"object":"chat.completion","usage":{"total_tokens":100,"prompt_tokens":50,"completion_tokens":50}}`)
	}))
	defer up.Close()
	cfg, _ := config.Parse([]byte(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: ` + up.URL + `}, api_key: k1}
models:
  agent:
    endpoints:
      - {protocol: openai, providers: [p1], models: [MiniMax-M3]}
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
	if strings.Contains(string(body), "[DONE]") {
		t.Errorf("non-streaming response contaminated with [DONE]: %s", body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Errorf("non-streaming response not valid JSON after normalization: %v\nbody=%s", err, body)
	}
}

func TestResponse_NormalizedThroughVMR(t *testing.T) {
	up := newStreamingUpstream(t)
	cfg, err := config.Parse([]byte(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: ` + up.srv.URL + `}, api_key: k1}
models:
  agent:
    endpoints:
      - {protocol: openai, providers: [p1], models: [MiniMax-M3]}
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

	// Client sends model="agent" — must see "agent" back, not "MiniMax-M3".
	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions",
		bytes.NewReader([]byte(`{"model":"agent","stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Stream into a buffer and parse each SSE data: line.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	// (a) Last data: line must be the [DONE] sentinel — MiniMax
	// didn't send one, VMR appended it.
	if len(dataLines) == 0 || dataLines[len(dataLines)-1] != "[DONE]" {
		t.Errorf("missing [DONE] sentinel; tail=%q", dataLines[len(dataLines)-1:])
	}

	// (b) Every model field must say "agent", never "MiniMax-M3".
	for i, l := range dataLines {
		if l == "[DONE]" {
			continue
		}
		if strings.Contains(l, "MiniMax-M3") {
			t.Errorf("data[%d] leaked upstream model name: %s", i, l)
		}
	}

	// (c) Think content must not appear in any data: line (it would
	// be inside delta.content as plain text).
	for i, l := range dataLines {
		if l == "[DONE]" {
			continue
		}
		if strings.Contains(l, "Let me") || strings.Contains(l, "check the") || strings.Contains(l, "memory file") {
			t.Errorf("data[%d] leaked think content: %s", i, l)
		}
	}

	// (d) The real answer text must still be present.
	found := false
	for _, l := range dataLines {
		if strings.Contains(l, "Real answer") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("post-think real answer missing; data=%v", dataLines)
	}
}
