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

func TestDefaultClassify_StatusCodesAndVendors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		body   string
		want   core.ErrorClass
	}{
		{"400 invalid parameter", 400, `{"error":{"message":"invalid temperature"}}`, core.ErrClient},
		// The literal body from audit line 455 of the 2026-08-25 incident: bai
		// words a context-window overflow as "Input token exceed the limit"
		// alongside its own quota_limit_reached code — the context wording must
		// win (failover-eligible, zero cooldown) over the ErrClient fallback.
		{"400 input token exceed (bai)", 400, `{"error":{"code":"1210","message":"Input token exceed the limit","data":{"quota_limit_reached":true}}}`, core.ErrContextLimit},
		// The literal body from audit line 109 of the same incident: cliproxy
		// answering its own token-refresh failure with 400 invalid_grant — an
		// auth-layer problem on a non-401 status (long cooldown + switch).
		{"400 invalid_grant (OAuth refresh failure)", 400, `{"error":"invalid_grant","error_description":"Bad Request"}`, core.ErrAuth},
		{"400 unknown model (MiniMax)", 400, `{"error":{"message":"invalid params, unknown model 'x' (2013)"}}`, core.ErrEndpoint},
		{"400 google missing thought_signature", 400, `{"error":{"code":400,"message":"Function call is missing a thought_signature in functionCall parts. This is required for tools to work correctly, and missing thought_signature may lead to degraded model performance. Additional data, function call default_api:exec , position 2. Please refer to https://ai.google.dev/gemini-api/docs/thought-signatures for more details.","status":"INVALID_ARGUMENT"}}`, core.ErrQuirk},
		{"401 unauthorized", 401, `{}`, core.ErrAuth},
		{"402 insufficient credits (OpenRouter)", 402, `{"error":{"message":"Insufficient credits"}}`, core.ErrEndpoint},
		{"403 forbidden", 403, `{}`, core.ErrAuth},
		{"404 model not found", 404, `{"error":{"message":"model not found"}}`, core.ErrEndpoint},
		{"408 request timeout", 408, `{}`, core.ErrTransient},
		{"413 payload too large", 413, `{"error":{"message":"payload too large"}}`, core.ErrClient},
		{"422 model does not exist", 422, `{"error":{"message":"model does not exist"}}`, core.ErrEndpoint},
		{"429 rate limit exceeded", 429, `{"error":{"message":"rate limit exceeded, slow down"}}`, core.ErrRateLimit},
		// Content-policy flags: switch endpoints but never punish health.
		{"403 moderation flagged (OpenRouter)", 403, `{"error":{"message":"Your input was flagged","metadata":{"reasons":["hate"],"flagged_input":"..."}}}`, core.ErrContent},
		{"403 guardrail blocked (OpenRouter)", 403, `{"error":{"message":"Request blocked by guardrail: prompt-injection"}}`, core.ErrContent},
		{"400 content exists risk (DeepSeek)", 400, `{"error":{"message":"Content Exists Risk"}}`, core.ErrContent},
		{"400 output content violation (MiniMax)", 400, `{"type":"error","error":{"message":"invalid params, output content violation (1027)"}}`, core.ErrContent},
		{"400 sensitive content in Chinese", 400, `{"error":{"message":"输入包含敏感内容，请修改后重试"}}`, core.ErrContent},
		{"402 content moderation wins over 402 endpoint rule", 402, `{"error":{"message":"request rejected by content moderation"}}`, core.ErrContent},
		{"404 content policy wins over 404 endpoint rule", 404, `{"error":{"message":"resource not found due to content policy violation"}}`, core.ErrContent},
		{"451 unavailable for legal reasons", 451, `{}`, core.ErrContent},
		{"429 exceeded quota treated as ErrEndpoint", 429, `{"error":{"message":"you have exceeded your quota"}}`, core.ErrEndpoint},
		{"429 insufficient balance treated as ErrEndpoint", 429, `{"error":{"message":"insufficient balance"}}`, core.ErrEndpoint},
		{"400 insufficient balance (relay)", 400, `{"error":{"message":"Insufficient balance"}}`, core.ErrEndpoint},
		{"400 insufficient credit (relay)", 400, `{"error":{"message":"insufficient credit"}}`, core.ErrEndpoint},
		{"400 insufficient quota (relay)", 400, `{"error":{"message":"insufficient quota"}}`, core.ErrEndpoint},
		{"400 quota exhausted (relay)", 400, `{"error":{"message":"quota exhausted"}}`, core.ErrEndpoint},
		{"400 out of credit (relay)", 400, `{"error":{"message":"out of credit"}}`, core.ErrEndpoint},
		{"400 balance exhausted Chinese 余额不足", 400, `{"error":{"message":"账户余额不足，请充值"}}`, core.ErrEndpoint},
		{"400 quota exhausted Chinese 额度不足", 400, `{"error":{"message":"当前额度不足"}}`, core.ErrEndpoint},
		{"400 account in arrears Chinese 账户欠费", 400, `{"error":{"message":"您的账户欠费，无法继续调用"}}`, core.ErrEndpoint},
		{"403 insufficient balance", 403, `{"error":{"message":"Insufficient balance"}}`, core.ErrEndpoint},
		{"500 internal server error", 500, `{}`, core.ErrTransient},
		{"502 bad gateway", 502, `{}`, core.ErrTransient},
		{"503 service unavailable", 503, `{}`, core.ErrTransient},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DefaultClassify(tc.status, []byte(tc.body)); got != tc.want {
				t.Errorf("status=%d body=%s: got %v want %v", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

func TestDefaultClassify_VendorQuirks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want core.ErrorClass
	}{
		{
			"Google Gemini thought_signature missing in tool call (live shape)",
			`{"error":{"code":400,"message":"Function call is missing a thought_signature in functionCall parts. This is required for tools to work correctly, and missing thought_signature may lead to degraded model performance. Additional data, function call default_api:exec , position 2. Please refer to https://ai.google.dev/gemini-api/docs/thought-signatures for more details.","status":"INVALID_ARGUMENT"}}`,
			core.ErrQuirk,
		},
		{
			"Google Gemini thought-signature hyphenated variant",
			`{"error":{"message":"missing thought-signature in functionCall"}}`,
			core.ErrQuirk,
		},
		{
			"Google Gemini thought signature spaced variant",
			`{"error":{"message":"invalid tool call: thought signature mismatch"}}`,
			core.ErrQuirk,
		},
		{
			// The literal body from audit line 513 of the 2026-08-25 incident: a
			// healthy endpoint rejecting a conversation whose history lacks the
			// previous turn's reasoning_content — the request/history shape is
			// wrong for THIS vendor, not evidence of an unhealthy endpoint. Must
			// fail over with zero cooldown (ErrQuirk), not dead-end (ErrClient)
			// or cool down (ErrEndpoint).
			"DeepSeek thinking-mode reasoning_content pass-back requirement",
			"The `reasoning_content` in the thinking mode must be passed back to the API.",
			core.ErrQuirk,
		},
		{
			"Generic invalid argument stays ErrClient",
			`{"error":{"code":400,"message":"invalid argument: temperature must be between 0 and 2","status":"INVALID_ARGUMENT"}}`,
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

func TestDefaultClassify_BalanceExhausted(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		body   string
		want   core.ErrorClass
	}{
		{"400 Insufficient balance standard JSON", 400, `{"error":{"message":"Insufficient balance"}}`, core.ErrEndpoint},
		{"400 insufficient credit", 400, `{"error":{"message":"insufficient credit"}}`, core.ErrEndpoint},
		{"400 insufficient quota", 400, `{"error":{"message":"insufficient quota"}}`, core.ErrEndpoint},
		{"400 quota exhausted", 400, `{"error":{"message":"quota exhausted"}}`, core.ErrEndpoint},
		{"400 out of credit", 400, `{"error":{"message":"out of credit"}}`, core.ErrEndpoint},
		{"400 余额不足", 400, `{"error":{"message":"当前账户余额不足"}}`, core.ErrEndpoint},
		{"400 额度不足", 400, `{"error":{"message":"接口调用额度不足"}}`, core.ErrEndpoint},
		{"400 账户欠费", 400, `{"error":{"message":"账户欠费，服务已暂停"}}`, core.ErrEndpoint},
		{"403 余额不足", 403, `{"error":{"message":"insufficient balance"}}`, core.ErrEndpoint},
		{"403 content flag beats balance", 403, `{"error":{"message":"content policy violation: insufficient balance"}}`, core.ErrContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DefaultClassify(tc.status, []byte(tc.body)); got != tc.want {
				t.Errorf("status=%d body=%s: got %v, want %v", tc.status, tc.body, got, tc.want)
			}
		})
	}
}
