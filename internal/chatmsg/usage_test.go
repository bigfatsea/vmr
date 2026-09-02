// Ver 2026-07-28 22:10, by Sonnet 5

package chatmsg

import (
	"testing"
	"unicode/utf8"

	"vmr/internal/core"
	"vmr/internal/tokenutil"
)

func TestExtractUsage_OpenAIJSON(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     float64(1000),
			"completion_tokens": float64(200),
			"prompt_tokens_details": map[string]any{
				"cached_tokens": float64(800),
			},
		},
	}
	u, ok := ExtractUsage(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if u.In != 1000 || u.Out != 200 || u.CacheRead != 800 {
		t.Errorf("usage = %+v", u)
	}
}

func TestExtractUsage_AnthropicJSON(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"usage": map[string]any{
			"input_tokens":                float64(500),
			"output_tokens":               float64(50),
			"cache_read_input_tokens":     float64(300),
			"cache_creation_input_tokens": float64(20),
		},
	}
	u, ok := ExtractUsage(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// Anthropic: In excludes cache by default, so total = input_tokens + cacheRead + cacheWrite.
	if u.In != 500+300+20 || u.Out != 50 || u.CacheRead != 300 || u.CacheWrite != 20 {
		t.Errorf("usage = %+v", u)
	}
}

// TestExtractUsage_ResponsesJSON proves openai-responses' usage object —
// Anthropic's field names (input_tokens/output_tokens) but Chat Completions'
// "cached tokens already included in the total" semantics — is read
// correctly rather than falling into either sibling protocol's rule
// verbatim: input_tokens_details.cached_tokens is the tell that selects the
// no-double-add path (see usageFromObj's doc comment).
func TestExtractUsage_ResponsesJSON(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"usage": map[string]any{
			"input_tokens":  float64(1000),
			"output_tokens": float64(200),
			"input_tokens_details": map[string]any{
				"cached_tokens": float64(800),
			},
			"output_tokens_details": map[string]any{
				"reasoning_tokens": float64(50),
			},
		},
	}
	u, ok := ExtractUsage(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if u.In != 1000 || u.Out != 200 || u.CacheRead != 800 || u.Reasoning != 50 {
		t.Errorf("usage = %+v, want In=1000 (not 1000+800 — already inclusive like Chat Completions)", u)
	}
}

// TestExtractUsage_ResponsesJSONNoCacheDetails covers a Responses usage
// object with no input_tokens_details at all (the shape my mockupstream
// loadtest scenario and a plain non-caching upstream both send) — must
// still take the "already inclusive" branch, not silently fall through to
// Anthropic's additive one just because input_tokens_details is absent.
func TestExtractUsage_ResponsesJSONNoCacheDetails(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"usage": map[string]any{
			"input_tokens":  float64(5),
			"output_tokens": float64(1),
		},
	}
	u, ok := ExtractUsage(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if u.In != 5 || u.Out != 1 {
		t.Errorf("usage = %+v, want In=5 Out=1", u)
	}
}

// TestExtractUsage_OpenAICompletionsGatewayDoubleCount pins the explicit
// openai-completions protocol branch: an aggregated gateway (Cloudflare AI
// Gateway, LiteLLM, a relay) can answer an OpenAI-protocol request with an
// Anthropic-shaped usage object — input_tokens present, input_tokens_details
// absent — and prompt_tokens is ALREADY the total including cache hits.
// Without the protocol branch the code fell into the "anthropic-shaped,
// protocol unknown" case and added cacheRead+cacheWrite on top, billing
// 1800 for a 1000-token input (the field-presence guess has no
// input_tokens_details to save it). The explicit branch takes
// max(prompt_tokens, input_tokens) and never adds the cache components.
func TestExtractUsage_OpenAICompletionsGatewayDoubleCount(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"usage": map[string]any{
			"input_tokens":            float64(1000),
			"output_tokens":           float64(200),
			"prompt_cache_hit_tokens": float64(800),
		},
	}
	u, ok := ExtractUsageWithProtocol(body, core.ProtocolOpenAICompletions)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if u.In != 1000 || u.CacheRead != 800 {
		t.Errorf("usage = %+v, want In=1000 (not 1000+800 — cache already included) CacheRead=800", u)
	}
}

// TestExtractUsage_OpenAICompletionsPrompTokens pins that the same explicit
// branch also reads a normal OpenAI-shaped object (prompt_tokens only, no
// input_tokens alias) without inventing a second count.
func TestExtractUsage_OpenAICompletionsPrompTokens(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     float64(500),
			"completion_tokens": float64(50),
			"prompt_tokens_details": map[string]any{
				"cached_tokens": float64(100),
			},
		},
	}
	u, ok := ExtractUsageWithProtocol(body, core.ProtocolOpenAICompletions)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if u.In != 500 || u.CacheRead != 100 {
		t.Errorf("usage = %+v, want In=500 CacheRead=100 (no double-add of the cached subset)", u)
	}
}

func TestExtractUsage_SSEStream(t *testing.T) {
	t.Parallel()
	raw := sseToolCall("exec")
	u, ok := ExtractUsage(raw)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if u.In != 100 || u.Out != 10 || u.CacheRead != 80 || u.Reasoning != 4 {
		t.Errorf("usage = %+v", u)
	}
}

func TestExtractUsage_NoUsage(t *testing.T) {
	t.Parallel()
	if _, ok := ExtractUsage(map[string]any{"foo": "bar"}); ok {
		t.Error("expected ok=false when no usage present")
	}
}

// TestUsage_Fresh pins the one formula this type's own doc comment states
// ("In - CacheRead - CacheWrite is the fresh portion") — previously
// hand-written at four independent call sites (internal/router/quota.go,
// internal/report/{cost,sticky}.go, internal/story/render_md.go) with no
// single test covering any of them directly; each site's own behavior was
// only ever pinned indirectly through that package's higher-level tests.
func TestUsage_Fresh(t *testing.T) {
	for _, tc := range []struct {
		name string
		u    Usage
		want int64
	}{
		{"no cache", Usage{In: 100, Out: 20}, 100},
		{"cache read only", Usage{In: 100, CacheRead: 40, Out: 20}, 60},
		{"cache read and write", Usage{In: 100, CacheRead: 30, CacheWrite: 10, Out: 20}, 60},
		{"pure cache hit, no fresh", Usage{In: 100, CacheRead: 100, Out: 20}, 0},
		{
			"defensive: reported cache exceeds reported total floors at 0, not negative",
			Usage{In: 100, CacheRead: 80, CacheWrite: 50, Out: 20}, 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.u.Fresh(); got != tc.want {
				t.Errorf("Fresh() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestExtractResponseText_SSEAndJSON(t *testing.T) {
	t.Parallel()
	// OpenAI completions SSE
	sseOpenAI := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello from OpenAI\"}}]}\n\ndata: [DONE]\n\n"
	if got := ExtractResponseText(sseOpenAI); got != "Hello from OpenAI" {
		t.Errorf("ExtractResponseText(sseOpenAI) = %q, want %q", got, "Hello from OpenAI")
	}

	// Anthropic SSE
	sseAnthropic := "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello from Anthropic\"}}\n\n"
	if got := ExtractResponseText(sseAnthropic); got != "Hello from Anthropic" {
		t.Errorf("ExtractResponseText(sseAnthropic) = %q, want %q", got, "Hello from Anthropic")
	}

	// OpenAI completions JSON
	jsonOpenAI := map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"content": "Hello JSON",
				},
			},
		},
	}
	if got := ExtractResponseText(jsonOpenAI); got != "Hello JSON" {
		t.Errorf("ExtractResponseText(jsonOpenAI) = %q, want %q", got, "Hello JSON")
	}

	// Plain text
	plain := "Just plain text"
	if got := ExtractResponseText(plain); got != "Just plain text" {
		t.Errorf("ExtractResponseText(plain) = %q, want %q", got, "Just plain text")
	}

	// Nil / Empty
	if got := ExtractResponseText(nil); got != "" {
		t.Errorf("ExtractResponseText(nil) = %q, want empty", got)
	}
}

func TestEstimateResponseBodyTokens_ExcludesSSEEnvelopes(t *testing.T) {
	t.Parallel()
	content := "This is the actual output message from the assistant."
	sseStream := "data: {\"id\":\"chatcmpl-999\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"" + content + "\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"

	got := EstimateResponseBodyTokens(sseStream)
	rawEstimate := EstimateResponseBodyTokens([]byte(sseStream))
	if got != rawEstimate {
		t.Errorf("EstimateResponseBodyTokens string vs []byte mismatch: %d vs %d", got, rawEstimate)
	}

	// Should match token estimation of content directly
	if got <= 0 {
		t.Fatalf("got = %d, want > 0", got)
	}
}

// EstimateResponseBodyTokens mirrors the router side's metering basis: text
// only, 0 for binary/opaque bodies, and never a raw-byte fallback.
func TestEstimateResponseBodyTokens_OpaqueBinaryIsZero(t *testing.T) {
	t.Parallel()
	// A compressed passthrough body as the audit JSONL round-trip delivers it:
	// invalid bytes already replaced with U+FFFD, so the string is valid UTF-8
	// but the content is mangled binary.
	opaque := string([]byte{0x1f, 0x8b}) + "\ufffd\ufffd binary garbage \ufffd"
	if got := EstimateResponseBodyTokens(opaque); got != 0 {
		t.Errorf("EstimateResponseBodyTokens(opaque string) = %d, want 0", got)
	}
	raw := []byte{0x1f, 0x8b, 0x00, 0xff, 0xfe}
	if utf8.Valid(raw) {
		t.Fatalf("test setup: raw bytes must be invalid UTF-8")
	}
	if got := EstimateResponseBodyTokens(raw); got != 0 {
		t.Errorf("EstimateResponseBodyTokens(opaque bytes) = %d, want 0", got)
	}
}

func TestEstimateResponseBodyTokens_PlainTextErrorBody(t *testing.T) {
	t.Parallel()
	// Non-JSON, non-SSE plain text still estimates from the text itself —
	// there is no envelope to strip.
	body := "Upstream is temporarily unavailable, please retry later."
	got := EstimateResponseBodyTokens(body)
	if got <= 0 {
		t.Errorf("EstimateResponseBodyTokens(plain text) = %d, want > 0", got)
	}
}

func TestEstimateResponseBodyTokens_EmptyAndNil(t *testing.T) {
	t.Parallel()
	if got := EstimateResponseBodyTokens(nil); got != 0 {
		t.Errorf("EstimateResponseBodyTokens(nil) = %d, want 0", got)
	}
	if got := EstimateResponseBodyTokens(""); got != 0 {
		t.Errorf("EstimateResponseBodyTokens(empty) = %d, want 0", got)
	}
}

// TestEstimateDegradedBasis_FallbackAsymmetry pins the deliberate fallback
// asymmetry of the degraded basis rule (see EstimateDegradedTokens): when
// text extraction yields nothing, the request side falls back to the raw
// request bytes (mirroring Facts.EstimatedTokens' raw basis) while the
// response side returns 0 (raw response bytes measure transport, not
// generation — the Q04 inflation). Unifying either side breaks parity with
// what the router charged; this test makes such a unification fail loudly.
func TestEstimateDegradedBasis_FallbackAsymmetry(t *testing.T) {
	t.Parallel()

	// Request side: valid JSON, but not a chat-request shape — extraction
	// must fail, forcing the raw-byte fallback with the raw basis.
	reqBody := map[string]any{"foo": "bar"}
	inEst := EstimateRequestBodyTokens(reqBody)
	wantIn := tokenutil.Estimate([]byte(`{"foo":"bar"}`))
	if wantIn <= 0 {
		t.Fatalf("wantIn = %d, want > 0", wantIn)
	}
	if inEst != wantIn {
		t.Errorf("inEst = %d, want raw-byte estimate %d (fallback must mirror Facts.EstimatedTokens' raw basis)", inEst, wantIn)
	}

	// Response side: opaque after the audit JSONL round-trip — mangled
	// bytes carry no usable content representation, must estimate 0.
	respBody := "data: \ufffd\ufffd\ufffd"
	if outEst := EstimateResponseBodyTokens(respBody); outEst != 0 {
		t.Errorf("outEst = %d, want 0 (no raw-byte fallback on the response side)", outEst)
	}

	// Both through the shared degraded entry point.
	inDeg, outDeg := EstimateDegradedTokens(nil, reqBody, respBody)
	if inDeg != wantIn || outDeg != 0 {
		t.Errorf("EstimateDegradedTokens = (%d, %d), want (%d, 0)", inDeg, outDeg, wantIn)
	}
}

func TestExtractTruncatedText_EscapedQuotes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "truncated JSON content with escaped quotes",
			input: `{"choices":[{"message":{"content":"say \"hello world\" to everyone`,
			want:  `say \"hello world\" to everyone`,
		},
		{
			name:  "truncated JSON text with unescaped closing quote",
			input: `{"text":"echo \"hello world\" completed"`,
			want:  `echo \"hello world\" completed`,
		},
		{
			name:  "truncated SSE data with escaped quotes",
			input: `data: {"delta":{"content":"code block: \"hello world\" truncated`,
			want:  `code block: \"hello world\" truncated`,
		},
		{
			name:  "truncated reasoning_content with escaped quotes",
			input: `{"choices":[{"delta":{"reasoning_content":"reasoning with \"quoted phrase\" here`,
			want:  `reasoning with \"quoted phrase\" here`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractResponseText(tc.input)
			if got != tc.want {
				t.Errorf("ExtractResponseText() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMergeUsageWithProtocol_CursorScanMultiLine(t *testing.T) {
	t.Parallel()
	raw := "data: {\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n" +
		": heartbeat\n" +
		"data: {\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":15}}\n" +
		"data: [DONE]\n"
	u := MergeUsageWithProtocol([]byte(raw), Usage{}, "")
	if u.In != 20 || u.Out != 15 {
		t.Errorf("MergeUsageWithProtocol = %+v, want In=20 Out=15", u)
	}
}
