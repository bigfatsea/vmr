// Ver 2026-07-24 12:00, by Sonnet 5

// mockupstream stands in for a real LLM provider during vmr load testing
// (see docs/VirtualModelRouter_Design_v4_Core.md §12). It never talks to a real
// provider — the whole point is to measure vmr's own overhead, not a
// provider's response time. Response shape is chosen by the incoming
// request's "model" field (vmr has already rewritten it to whatever real
// upstream model the matching endpoint configures), prefixed "scenario:".
//
// Not part of the shipped vmr binary — `go build ./cmd/vmr` never touches
// this directory. Run directly: `go run ./loadtest/mockupstream`.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9900", "listen address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", handleScenario)
	mux.HandleFunc("/ok/v1/chat/completions", handleScenario)
	mux.HandleFunc("/fail1/v1/chat/completions", handleFail)
	mux.HandleFunc("/fail2/v1/chat/completions", handleFail)
	mux.HandleFunc("/v1/messages", handleAnthropicScenario) // Anthropic-protocol ingress (anthropic_baseline)

	log.Printf("mockupstream listening on %s (scenarios dispatch on the request's model field)", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

// handleFail always fails — used by the `failover` scenario's first two
// endpoints so vmr has to walk its candidate list before succeeding.
func handleFail(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body) // drain so vmr's write doesn't block on a closed pipe
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprint(w, `{"error":{"message":"mockupstream: simulated failure","type":"server_error"}}`)
}

type inboundReq struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

func handleScenario(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req inboundReq
	json.Unmarshal(body, &req) // best-effort: malformed body just falls through to baseline

	switch strings.TrimPrefix(req.Model, "scenario:") {
	case "thinking_leak":
		serveThinkingLeak(w)
	case "stream_normal":
		serveStreamNormal(w)
	case "think_tag":
		serveThinkTag(w)
	case "big_response":
		serveBigResponse(w)
	default: // "baseline" and anything else (big_image/long_history/multi_image/gif reuse it — only the request side matters there)
		serveBaseline(w, req.Stream)
	}
}

// handleAnthropicScenario is the /messages counterpart of handleScenario —
// only the anthropic_baseline scenario uses it, so it doesn't need the same
// dispatch table, just the one Anthropic-shaped response.
func handleAnthropicScenario(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id": "mock-anthropic-baseline", "type": "message", "role": "assistant",
		"content":     []map[string]string{{"type": "text", "text": "ok"}},
		"model":       "scenario:baseline",
		"stop_reason": "end_turn",
		"usage":       map[string]int{"input_tokens": 5, "output_tokens": 1},
	})
}

// --- response shapes -------------------------------------------------------
//
// All built via encoding/json rather than hand-formatted strings, so Go
// handles JSON escaping (the thinking scenario's embedded newlines in
// particular) instead of us getting it wrong by hand.

type delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}
type sseChoice struct {
	Index        int     `json:"index"`
	Delta        delta   `json:"delta"`
	FinishReason *string `json:"finish_reason,omitempty"`
}
type sseChunk struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Choices []sseChoice `json:"choices"`
}

// writeSSE marshals with HTML escaping off — json.Marshal's default would
// turn the think_tag scenario's literal "<think>"/"</think>" into
// "<think>", which never matches vmr's thinkOpenMarker/
// thinkCloseMarker byte literals (same reason vmr's own core.MarshalNoEscape
// exists).
func writeSSE(w http.ResponseWriter, fl http.Flusher, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
	fmt.Fprintf(w, "data: %s\n\n", bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
	fl.Flush()
}

func finishReason(s string) *string { return &s }

// serveBaseline answers with the smallest realistic shape — non-streaming
// JSON by default, or a tiny 2-chunk SSE stream if the request asked for
// one. This is the scenario every request that doesn't care about response
// shape (big_image, long_history, the failover scenario's `ok` endpoint)
// reuses — the interesting cost in those scenarios is on vmr's request
// side, not what comes back.
func serveBaseline(w http.ResponseWriter, stream bool) {
	if !stream {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "mock-baseline", "object": "chat.completion",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
		})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	fl := w.(http.Flusher)
	writeSSE(w, fl, sseChunk{ID: "mock-baseline", Object: "chat.completion.chunk", Choices: []sseChoice{{Delta: delta{Role: "assistant"}}}})
	writeSSE(w, fl, sseChunk{ID: "mock-baseline", Object: "chat.completion.chunk", Choices: []sseChoice{{Delta: delta{Content: "ok"}}}})
	writeSSE(w, fl, sseChunk{ID: "mock-baseline", Object: "chat.completion.chunk", Choices: []sseChoice{{FinishReason: finishReason("stop")}}})
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// serveStreamNormal is a genuine multi-chunk SSE stream with no pathology —
// exercises vmr's true-streaming passthrough path (modePassthrough).
func serveStreamNormal(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	fl := w.(http.Flusher)
	writeSSE(w, fl, sseChunk{ID: "mock-stream", Object: "chat.completion.chunk", Choices: []sseChoice{{Delta: delta{Role: "assistant"}}}})
	for _, word := range []string{"The", " quick", " brown", " fox", " jumps", " over", " the", " lazy", " dog", "."} {
		writeSSE(w, fl, sseChunk{ID: "mock-stream", Object: "chat.completion.chunk", Choices: []sseChoice{{Delta: delta{Content: word}}}})
	}
	writeSSE(w, fl, sseChunk{ID: "mock-stream", Object: "chat.completion.chunk", Choices: []sseChoice{{FinishReason: finishReason("stop")}}})
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// serveThinkingLeak reproduces MiniMax M3's thinking=medium shape (see
// internal/router/response.go::stripThinkingProcess) — a plain-text
// "Thinking Process:" section followed by a "Looks good. Pro(ceed)"
// self-endorsement marker, all inside one content value. This is the one
// scenario expected to force vmr's response normalizer into full-buffer
// mode (no incremental streaming) — the whole point of measuring it.
func serveThinkingLeak(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	fl := w.(http.Flusher)
	writeSSE(w, fl, sseChunk{ID: "mock-thinking", Object: "chat.completion.chunk", Choices: []sseChoice{{Delta: delta{Role: "assistant"}}}})
	// The exploratory "practice" thinking section, including a throwaway
	// self-endorsement, is its own SSE chunk — real MiniMax streams the
	// eventual, real endorsement as a SEPARATE later chunk (see
	// internal/router/response_test.go::TestStripThinkingProcess_MultipleEndorsements),
	// so stripThinkingProcess's regex (which only spans a single SSE data:
	// line — real "\n\n" event boundaries stop it) sees two distinct
	// matches and correctly keeps the LAST one, not this one.
	thinking := "Thinking Process:\n\n" +
		"1. Analyze the request\n2. Consider options\n3. Draft a response\n4. Review the draft\n" +
		"5. Final Polish:\n    draft one\n    Looks good. Pro draft two\n    draft three"
	writeSSE(w, fl, sseChunk{ID: "mock-thinking", Object: "chat.completion.chunk", Choices: []sseChoice{{Delta: delta{Content: thinking}}}})
	// The real self-endorsement — a separate chunk, so the "take the last
	// endorsement" rule lands here.
	writeSSE(w, fl, sseChunk{ID: "mock-thinking", Object: "chat.completion.chunk", Choices: []sseChoice{{Delta: delta{Content: "    Looks good. Proceed with the final answer below."}}}})
	writeSSE(w, fl, sseChunk{ID: "mock-thinking", Object: "chat.completion.chunk", Choices: []sseChoice{{Delta: delta{Content: " Here is the actual final answer."}}}})
	writeSSE(w, fl, sseChunk{ID: "mock-thinking", Object: "chat.completion.chunk", Choices: []sseChoice{{FinishReason: finishReason("stop")}}})
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// serveThinkTag reproduces MiniMax M3's OTHER thinking shape (see
// internal/router/response.go — the thinkPattern/think_strip path, distinct
// from thinking_leak's stripThinkingProcess path): inline <think>...</think>
// reasoning inside the content field. Unlike thinking_leak, vmr only
// buffers WHILE the think block is open — once </think> closes, it resumes
// live streaming for the rest of the response. Several post-think chunks
// below so that resumption is actually exercised, not just the transition.
func serveThinkTag(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	fl := w.(http.Flusher)
	writeSSE(w, fl, sseChunk{ID: "mock-think-tag", Object: "chat.completion.chunk", Choices: []sseChoice{{Delta: delta{Role: "assistant"}}}})
	writeSSE(w, fl, sseChunk{ID: "mock-think-tag", Object: "chat.completion.chunk", Choices: []sseChoice{{Delta: delta{
		Content: "<think>Let me consider the request and figure out the best approach here.</think>\n",
	}}}})
	for _, word := range []string{"Here", " is", " the", " answer", " after", " thinking", " it", " through", "."} {
		writeSSE(w, fl, sseChunk{ID: "mock-think-tag", Object: "chat.completion.chunk", Choices: []sseChoice{{Delta: delta{Content: word}}}})
	}
	writeSSE(w, fl, sseChunk{ID: "mock-think-tag", Object: "chat.completion.chunk", Choices: []sseChoice{{FinishReason: finishReason("stop")}}})
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// serveBigResponse answers with a large non-streaming body (a long
// assistant reply) — stresses the response side (audit full-body write,
// JSON encode/decode at scale) as opposed to long_history's request side.
func serveBigResponse(w http.ResponseWriter) {
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&sb, "Paragraph %d of a long generated answer, simulating a verbose agent response. ", i)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id": "mock-big-response", "object": "chat.completion",
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]string{"role": "assistant", "content": sb.String()},
			"finish_reason": "stop",
		}},
	})
}
