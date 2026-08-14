// Ver 2026-08-14, by Sonnet 5
package adapter

import (
	"strings"
	"testing"

	"vmr/internal/core"
)

func TestDefaultClassify_MarkerDeepInBody(t *testing.T) {
	t.Parallel()
	// Vendors may attach verbose debug payloads before the actual error
	// message; a marker several KB into the body must still be sniffed
	// within the snippet cutoff — a miss classifies as ErrClient, which
	// never fails over.
	padding := strings.Repeat(`{"debug":"xxxxxxxxxxxxxxxx"},`, 200) // ~5.6 KB
	cases := []struct {
		name string
		body string
		want core.ErrorClass
	}{
		{"model not found late", `{"trace":[` + padding + `],"error":{"message":"model gpt-x not found"}}`, core.ErrEndpoint},
		{"content flag late", `{"trace":[` + padding + `],"error":{"message":"output data may contain inappropriate content (1027)"}}`, core.ErrContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DefaultClassify(400, []byte(tc.body)); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
	// Beyond the 32 KB bound the marker is invisible by design.
	huge := strings.Repeat("x", classifySnippetBytes) + "model not found"
	if got := DefaultClassify(400, []byte(huge)); got != core.ErrClient {
		t.Errorf("marker past bound: got %v, want %v", got, core.ErrClient)
	}
}

// TestDefaultClassify_UpstreamGatewayFailure locks in the fix for the
// incident documented in reports/incident-20260718-console-go-400-failover_
// Sonnet5.md: a relay hop reporting its own forwarding failure must not
// dead-end the failover walk the way a genuine bad-request 400 correctly
// does.
func TestDefaultClassify_UpstreamGatewayFailure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want core.ErrorClass
	}{
		{
			"opencode Console Go relay failure (the actual incident body)",
			`{"message":"Error from provider (Console Go): Upstream request failed","type":"invalid_request_error","param":null,"code":"invalid_request_error"}`,
			core.ErrEndpoint,
		},
		{"bad gateway wording", `{"error":{"message":"502 Bad Gateway from upstream"}}`, core.ErrEndpoint},
		{"gateway timeout wording", `{"error":{"message":"Gateway Timeout while contacting upstream"}}`, core.ErrEndpoint},
		// Genuine request-content errors must still classify as ErrClient —
		// upstreamHint must not swallow these just because "model" or generic
		// wording appears nearby.
		{"missing field is still ErrClient", `{"error":{"message":"missing required field: messages"}}`, core.ErrClient},
		{"malformed json is still ErrClient", `{"error":{"message":"invalid JSON payload"}}`, core.ErrClient},
		// contentHint and the model-not-found rule still take priority over
		// upstreamHint when both could apply.
		{"content flag beats upstream wording", `{"error":{"message":"upstream request failed: content_policy violation"}}`, core.ErrContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DefaultClassify(400, []byte(tc.body)); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestDefaultClassify_ContextLimit locks in P0-A's fix for a real
// robustness gap: a long-context request that overflows an endpoint's
// context window must classify as ErrContextLimit (switch, no health
// penalty) instead of ErrClient (return to client, failover walk stops
// dead) — see the architecture review's P0-B finding. Distinguishes
// "conversation history exceeds the window" (failover-eligible) from "the
// request's own max_tokens/output-length parameter is too large" (switching
// endpoints can't fix a client-supplied number, stays ErrClient).
func TestDefaultClassify_ContextLimit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want core.ErrorClass
	}{
		{
			"OpenAI context_length_exceeded (the classic shape)",
			`{"error":{"message":"This model's maximum context length is 8192 tokens. However, you requested 10000 tokens (9000 in the messages, 1000 in the completion). Please reduce the length of the messages or completion.","type":"invalid_request_error","param":"messages","code":"context_length_exceeded"}}`,
			core.ErrContextLimit,
		},
		{
			"Anthropic prompt-too-long wording",
			`{"error":{"type":"invalid_request_error","message":"prompt is too long: 220000 tokens > 200000 maximum"}}`,
			core.ErrContextLimit,
		},
		{
			"generic context window wording",
			`{"error":"input exceeds the model's context window"}`,
			core.ErrContextLimit,
		},
		{
			"Chinese vendor wording",
			`{"error":{"message":"输入内容超出上下文长度限制"}}`,
			core.ErrContextLimit,
		},
		{
			"Anthropic max_tokens output-param rejection stays ErrClient",
			`{"error":{"type":"invalid_request_error","message":"max_tokens: 100000 > 64000, which is the maximum allowed number of output tokens"}}`,
			core.ErrClient,
		},
		{
			"OpenAI max_tokens output-param rejection stays ErrClient",
			`{"error":{"message":"max_tokens is too large: 100000. This model supports at most 16384 completion tokens, whereas you provided 100000.","type":"invalid_request_error"}}`,
			core.ErrClient,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DefaultClassify(400, []byte(tc.body)); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
