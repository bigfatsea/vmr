// Ver 2026-07-17 02:00, by Sonnet 5
//
// Unit tests for the response stream processor: model-field rewrite,
// think-block stripping, [DONE] sentinel, and cross-chunk regex
// safety.
package router

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// readAll drains the processor into a string. Reads in a loop because
// the processor may legitimately return (0, nil) while waiting for
// the source to deliver more bytes.
func readAll(t *testing.T, rs io.Reader) string {
	t.Helper()
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	for {
		n, err := rs.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err == io.EOF {
			return buf.String()
		}
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
	}
}

func TestRespStream_ModelFieldRewrite(t *testing.T) {
	// Each chunk: standard SSE message with model field. The processor
	// should rewrite "MiniMax-M3" → "agent" in every chunk.
	src := strings.NewReader(
		`data: {"id":"a","model":"MiniMax-M3","object":"chunk"}` + "\n\n" +
			`data: {"id":"b","model":"MiniMax-M3","object":"chunk"}` + "\n\n",
	)
	out := readAll(t, newRespStream(src, "agent", true, "openai", false))

	if strings.Contains(out, "MiniMax-M3") {
		t.Errorf("upstream model name leaked: %q", out)
	}
	if c := strings.Count(out, `"model":"agent"`); c != 2 {
		t.Errorf("expected 2 occurrences of \"model\":\"agent\", got %d in %q", c, out)
	}
}

func TestRespStream_ThinkBlockStripped(t *testing.T) {
	// MiniMax-style: thinking is inline as <think>...</think> inside
	// delta.content, with literal "\n\n" trailing the close tag.
	src := strings.NewReader(
		`data: {"choices":[{"index":0,"delta":{"content":"` +
			`<think>The user said hi, so I will respond with a greeting.</think>\n\n` +
			`Hi there!"}}]}` + "\n\n",
	)
	out := readAll(t, newRespStream(src, "agent", true, "openai", false))

	if strings.Contains(out, "<think>") {
		t.Errorf("think block start leaked: %q", out)
	}
	if strings.Contains(out, "</think>") {
		t.Errorf("think block end leaked: %q", out)
	}
	if !strings.Contains(out, "Hi there!") {
		t.Errorf("post-think content missing: %q", out)
	}
}

func TestRespStream_ThinkBlockCrossChunk(t *testing.T) {
	// Realistic cross-chunk case: the opener <think> and the closer
	// </think> are in different SSE messages, and the body of the
	// think block spans the boundary. This is the typical pattern in
	// MiniMax streaming output (markers are small, content is large).
	src := strings.NewReader(
		`data: {"choices":[{"delta":{"content":"<think>step 1. step 2. step` + "\n\n" +
			`data: {"choices":[{"delta":{"content":" 3. step 4.</think>real"}}]}` + "\n\n",
	)
	out := readAll(t, newRespStream(src, "agent", true, "openai", false))

	if strings.Contains(out, "step 1") {
		t.Errorf("think content not stripped across SSE messages: %q", out)
	}
	if strings.Contains(out, "step 3") {
		t.Errorf("think content not stripped (latter half): %q", out)
	}
	if !strings.Contains(out, "real") {
		t.Errorf("post-think content missing: %q", out)
	}
}

func TestRespStream_DoneSentinel(t *testing.T) {
	// Upstream closes without [DONE]. The processor must append one
	// before returning EOF so SSE parsers get a clean termination.
	src := strings.NewReader(
		`data: {"id":"x","choices":[],"model":"MiniMax-M3","object":"chunk"}` + "\n\n",
	)
	out := readAll(t, newRespStream(src, "agent", true, "openai", false))

	if !strings.HasSuffix(out, "data: [DONE]\n\n") {
		t.Errorf("missing [DONE] sentinel, tail=%q", out)
	}
}

func TestRespStream_NonStreamSingleObject(t *testing.T) {
	// A single JSON object with no SSE framing: no event separator ever
	// appears, so the stream stays undecided until EOF and goes through
	// the buffered whole-body pass (model rewrite + think strip). isSSE
	// is passed as true here to also exercise the [DONE] append.
	src := strings.NewReader(
		`{"id":"x","model":"MiniMax-M3","choices":[{"message":{"role":"assistant","content":"<think>hello</think>\n\nHi"}}]}`,
	)
	out := readAll(t, newRespStream(src, "agent", true, "openai", false))

	if !strings.Contains(out, `"model":"agent"`) {
		t.Errorf("non-stream model rewrite failed: %q", out)
	}
	if strings.Contains(out, "hello") {
		t.Errorf("non-stream think strip failed: %q", out)
	}
	if !strings.Contains(out, "Hi") {
		t.Errorf("non-stream content lost: %q", out)
	}
	if !strings.HasSuffix(out, "data: [DONE]\n\n") {
		t.Errorf("non-stream [DONE] missing: %q", out)
	}
}

func TestRespStream_NoThinkBlockPassthrough(t *testing.T) {
	// Normal content without any think markers should be untouched.
	// The trailing newlines in delta.content are preserved.
	in := `data: {"choices":[{"delta":{"content":"plain reply"}}]}` + "\n\n"
	out := readAll(t, newRespStream(strings.NewReader(in), "agent", true, "openai", false))
	if !strings.Contains(out, "plain reply") {
		t.Errorf("plain content lost: %q", out)
	}
}

func TestRespStream_OneByteReads(t *testing.T) {
	// Pathological: read one byte at a time from the source. Event
	// assembly and the mode decision must be unaffected by how the
	// bytes are chunked on the wire.
	srcStr := `data: {"model":"MiniMax-M3","choices":[{"delta":{"content":"` +
		`<think>step 1. step 2. step 3.</think>result"}}]}` + "\n\n"
	out := readAll(t, newRespStream(oneByteReader{src: strings.NewReader(srcStr)}, "agent", true, "openai", false))

	if strings.Contains(out, "MiniMax-M3") {
		t.Errorf("upstream model name leaked under 1-byte reads: %q", out)
	}
	if strings.Contains(out, "step 1") {
		t.Errorf("think content leaked under 1-byte reads: %q", out)
	}
	if !strings.Contains(out, `"model":"agent"`) {
		t.Errorf("model rewrite failed under 1-byte reads: %q", out)
	}
	if !strings.Contains(out, "result") {
		t.Errorf("post-think content missing under 1-byte reads: %q", out)
	}
}

// oneByteReader is a test helper: yields one byte per Read call,
// exercising the worst case for event assembly.
type oneByteReader struct{ src io.Reader }

func (r oneByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return r.src.Read(p[:1])
}

func TestStripThinkingProcess_FullPattern(t *testing.T) {
	// The full MiniMax M3 thinking=medium shape: the buffer is the raw
	// upstream body with SSE framing, and the thinking lives in a separate
	// data: line from the final response — the strip must drop the
	// thinking line and trim the marker's line content.
	roleLine := "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n"
	thinkingLine := "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Thinking Process:\\n\\n1.  **Analyze the Request:**\\n    *   **User Message:** \\\"hi\\\"\\n2.  **Examine:**\\n    *   ...\\n3.  **Drafting:**\\n    *   ...\\n4.  **Review:**\\n    *   No emojis? Checked.\\n5.  **Final Polish:**\\n    draft 1\\n    Looks good. Pro draft 2\\n    draft 3\\n    Looks good. Proceed final answer here\"}}]}\n\n"
	finalLine := "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\\n5. **Final Polish:**\\n    draft 1\\n    Looks good. Proactual reply here\"}}]}\n\n"
	finishLine := "data: {\"id\":\"x\",\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"

	in := roleLine + thinkingLine + finalLine + finishLine

	out := stripThinkingProcess([]byte(in))
	got := string(out)

	// The thinking line must be dropped.
	if strings.Contains(got, "Thinking Process:") {
		t.Errorf("Thinking Process: header should be stripped, got %q", got)
	}
	if strings.Contains(got, "Analyze the Request") {
		t.Errorf("numbered thinking section should be stripped, got %q", got)
	}
	// The marker ("Looks good. Pro" / "Proceed") must be gone.
	if strings.Contains(got, "Looks good") {
		t.Errorf("self-endorsement marker should be stripped, got %q", got)
	}
	// The actual final response must survive.
	if !strings.Contains(got, "actual reply here") {
		t.Errorf("final response missing: %q", got)
	}
	// The role line and finish line must be preserved.
	if !strings.Contains(got, "\"role\":\"assistant\"") {
		t.Errorf("role line should be preserved: %q", got)
	}
	if !strings.Contains(got, "\"finish_reason\":\"stop\"") {
		t.Errorf("finish line should be preserved: %q", got)
	}
}

func TestStripThinkingProcess_MultipleEndorsements(t *testing.T) {
	// The model iterates — emits "Looks good. Pro" once in
	// the thinking line, then "Looks good. Proceed" at the
	// actual transition in the final response line. We must
	// take the LAST one in the LAST data: line.
	roleLine := "data: {\"delta\":{\"role\":\"assistant\"}}\n\n"
	thinkingLine := "data: {\"delta\":{\"content\":\"Thinking Process:\\n\\n1. **Drafting**\\n2. **Final Polish:**\\n    draft 1\\n    Looks good. Pro draft 2\\n    draft 3\"}}\n\n"
	finalLine := "data: {\"delta\":{\"content\":\"    Looks good. Proceed final response here\"}}\n\n"

	in := roleLine + thinkingLine + finalLine

	out := stripThinkingProcess([]byte(in))
	got := string(out)

	if !strings.Contains(got, "final response here") {
		t.Errorf("expected final response to be preserved, got %q", got)
	}
	if strings.Contains(got, "draft 3") {
		t.Errorf("earlier draft should be stripped, got %q", got)
	}
	if strings.Contains(got, "Looks good") {
		t.Errorf("self-endorsement marker should be stripped, got %q", got)
	}
}

func TestStripThinkingProcess_NoEndorsement(t *testing.T) {
	// Content mentions "Thinking Process" but lacks the
	// "Looks good. Pro" self-endorsement marker. Pass through
	// rather than drop the response.
	in := "data: {\"delta\":{\"content\":\"Thinking Process: 1. step one 2. step two. Final response here.\"}}\n\n"
	out := stripThinkingProcess([]byte(in))
	if string(out) != in {
		t.Errorf("no-endorsement case: should pass through, got %q", string(out))
	}
}

func TestStripThinkingProcess_NotThinkingProcess(t *testing.T) {
	// Content has no "Looks good. Pro" self-endorsement marker —
	// pass through.
	in := "data: {\"id\":\"x\",\"choices\":[{\"delta\":{\"content\":\"Hello world\"}}]}\n\n"
	out := stripThinkingProcess([]byte(in))
	if string(out) != in {
		t.Errorf("no-marker case: should pass through unchanged, got %q", string(out))
	}
}

func TestStripThinkingProcess_ChineseEndorsement(t *testing.T) {
	// Future-proofing: the model might use Chinese self-endorsement
	// markers. For now we only support the English ones, so this
	// should pass through.
	in := "Thinking Process:\n\n1. 思考\n\n好。\n\n收到。"
	out := stripThinkingProcess([]byte(in))
	// No "Looks good. Pro" — pass through.
	if string(out) != in {
		t.Errorf("Chinese endorsement (unsupported): should pass through, got %q", string(out))
	}
}

func TestStripThinkingProcess_LeadingWhitespace(t *testing.T) {
	// The self-endorsement line is indented (Final Polish
	// drafts are indented). The regex must eat the leading
	// whitespace. The first content line carries the
	// "Thinking Process:" prefix that arms the trigger guard.
	roleLine := "data: {\"delta\":{\"role\":\"assistant\"}}\n\n"
	thinkingLine := "data: {\"delta\":{\"content\":\"Thinking Process:\\n\\n1. **Drafting**\"}}\n\n"
	finalLine := "data: {\"delta\":{\"content\":\"...\\n    Looks good. Profinal answer here\\n\"}}\n\n"

	in := roleLine + thinkingLine + finalLine

	out := stripThinkingProcess([]byte(in))
	got := string(out)
	if !strings.Contains(got, "final answer here") {
		t.Errorf("expected 'final answer here' to be preserved, got %q", got)
	}
	if strings.Contains(got, "Looks good") {
		t.Errorf("self-endorsement marker should be stripped, got %q", got)
	}
	if strings.Contains(got, "Thinking Process") {
		t.Errorf("thinking line should be dropped, got %q", got)
	}
}

func TestStripThinkingProcess_LooksGoodInNormalStream(t *testing.T) {
	// A NORMAL reply whose text legitimately contains "Looks good. Proceed"
	// — e.g. a code review verdict — must pass through untouched. Without
	// the trigger guard (first content value must start with "Thinking
	// Process:"), every chunk before the marker would be silently dropped.
	in := `data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant"}}]}` + "\n\n" +
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"I reviewed the PR carefully."}}]}` + "\n\n" +
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"Everything checks out."}}]}` + "\n\n" +
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"Looks good. Proceed with the merge."}}]}` + "\n\n" +
		`data: {"id":"1","choices":[{"finish_reason":"stop","index":0,"delta":{}}]}` + "\n\n"
	out := stripThinkingProcess([]byte(in))
	if string(out) != in {
		t.Errorf("normal reply with endorsement phrase must pass through, got %q", string(out))
	}
}

func TestStripThinkingProcess_LooksGoodInNormalNonStream(t *testing.T) {
	// Non-streaming JSON whose content contains the endorsement phrase as
	// part of a normal reply must pass through unchanged — the single-line
	// body must never get duplicated (marker line == first line), which
	// would corrupt the JSON the client parses.
	in := `{"id":"1","choices":[{"message":{"role":"assistant","content":"Looks good. Proceed with step 2."}}],"model":"m"}`
	out := stripThinkingProcess([]byte(in))
	if string(out) != in {
		t.Errorf("normal non-stream reply must pass through, got %q", string(out))
	}
}

func TestStripThinkingProcess_MarkerInFirstLine(t *testing.T) {
	// The real thinking=medium shape in a NON-STREAMING response:
	// thinking and final response share one content value in one JSON
	// object — the marker line IS the first line. The strip must trim
	// in place without duplicating the body.
	in := `{"id":"1","choices":[{"message":{"role":"assistant","content":"Thinking Process:\n\n1. **Drafting**\n2. **Final Polish:**\n    draft\n    Looks good. Proceed the actual answer."}}],"model":"m"}`
	out := stripThinkingProcess([]byte(in))
	got := string(out)
	if c := strings.Count(got, `"id":"1"`); c != 1 {
		t.Errorf("body duplicated: id appears %d times in %q", c, got)
	}
	if strings.Contains(got, "Thinking Process") || strings.Contains(got, "Looks good") {
		t.Errorf("thinking prefix should be trimmed, got %q", got)
	}
	if !strings.Contains(got, "the actual answer.") {
		t.Errorf("final response missing: %q", got)
	}
}

func TestRespStream_StripKeepsSSEFraming(t *testing.T) {
	// When the strip fires, the surviving lines must stay separated by
	// "\n\n" — including the boundary before the appended [DONE] sentinel.
	// The trailing empty element from splitting a body that ends in "\n\n"
	// must be kept, or the last data: line glues onto "data: [DONE]".
	roleLine := "data: {\"delta\":{\"role\":\"assistant\"}}\n\n"
	thinkingLine := "data: {\"delta\":{\"content\":\"Thinking Process:\\n\\n1. **Drafting**\"}}\n\n"
	finalLine := "data: {\"delta\":{\"content\":\"    Looks good. Proceed the reply\"}}\n\n"
	out := readAll(t, newRespStream(strings.NewReader(roleLine+thinkingLine+finalLine), "agent", true, "openai", false))

	if !strings.HasSuffix(out, "\n\ndata: [DONE]\n\n") {
		t.Errorf("[DONE] not separated from the last data line, tail=%q", out)
	}
	if strings.Contains(out, "}data:") {
		t.Errorf("SSE framing corrupted (glued lines): %q", out)
	}
}

func TestRespStream_EmptySource(t *testing.T) {
	// Empty source: just the [DONE] sentinel should be emitted.
	out := readAll(t, newRespStream(strings.NewReader(""), "agent", true, "openai", false))
	if out != "data: [DONE]\n\n" {
		t.Errorf("empty source produced %q, want only [DONE]", out)
	}
}

func TestRespStream_NestedModelInDelta(t *testing.T) {
	// modelFieldPattern is a plain regex with no JSON-depth tracking: it
	// rewrites every unescaped `"model":"..."` occurrence in the block,
	// nested or not — unlike the request-side RewriteModel, which is a
	// structural scanner limited to the top-level key. In practice this is
	// harmless (OpenAI/Anthropic-shaped responses only ever carry "model"
	// at the top level), but a chunk with a genuinely nested "model" field
	// — e.g. a vendor extension echoed inside a tool call — has that value
	// rewritten too. This test documents the actual (not the hoped-for)
	// behavior; it previously claimed "top level only" without ever
	// constructing a nested field to check it.
	src := strings.NewReader(
		`data: {"id":"x","model":"MiniMax-M3","choices":[{"index":0,"delta":{"role":"assistant",` +
			`"tool_calls":[{"function":{"name":"x","model":"nested-value"}}]}}]}` + "\n\n",
	)
	out := readAll(t, newRespStream(src, "agent", true, "openai", false))
	if got := strings.Count(out, `"model":"agent"`); got != 2 {
		t.Errorf("expected both the top-level AND the nested model field rewritten to the client model (regex has no depth awareness), got %d rewrites in: %q", got, out)
	}
	if strings.Contains(out, `"model":"nested-value"`) {
		t.Errorf("nested model field survived unrewritten — modelFieldPattern's depth-blindness changed, update this test's assumption: %q", out)
	}
}

// scriptReader delivers one scripted chunk per Read call, then EOF. It
// lets tests observe output produced BEFORE the source is exhausted —
// the definition of true streaming.
type scriptReader struct {
	chunks [][]byte
	i      int
}

func (r *scriptReader) Read(p []byte) (int, error) {
	if r.i >= len(r.chunks) {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[r.i])
	r.i++
	return n, nil
}

func TestRespStream_TrueStreamingPassthrough(t *testing.T) {
	// A normal content event must be forwarded as soon as it arrives —
	// after a single source read, before the upstream stream ends.
	ev1 := `data: {"id":"1","model":"MiniMax-M3","choices":[{"delta":{"content":"hello"}}]}` + "\n\n"
	ev2 := `data: {"id":"1","model":"MiniMax-M3","choices":[{"finish_reason":"stop","delta":{}}]}` + "\n\n"
	sr := &scriptReader{chunks: [][]byte{[]byte(ev1), []byte(ev2)}}
	rs := newRespStream(sr, "agent", true, "openai", false)

	buf := make([]byte, 64<<10)
	n, err := rs.Read(buf)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	first := string(buf[:n])
	if sr.i != 1 {
		t.Fatalf("expected exactly 1 source read before first output, got %d", sr.i)
	}
	if !strings.Contains(first, "hello") {
		t.Errorf("first event not forwarded immediately: %q", first)
	}
	if !strings.Contains(first, `"model":"agent"`) {
		t.Errorf("model not rewritten in streamed event: %q", first)
	}

	rest := readAll(t, rs)
	full := first + rest
	if c := strings.Count(full, "data: [DONE]"); c != 1 {
		t.Errorf("expected exactly one [DONE], got %d in %q", c, full)
	}
	if !strings.HasSuffix(full, "data: [DONE]\n\n") {
		t.Errorf("missing [DONE] terminator: tail=%q", full)
	}
}

func TestRespStream_NoDoubleDone(t *testing.T) {
	// Upstream already sends [DONE] (DeepSeek, OpenRouter): VMR must
	// not append a second one.
	in := `data: {"id":"1","model":"M","choices":[{"delta":{"content":"hi"}}]}` + "\n\n" +
		"data: [DONE]\n\n"
	out := readAll(t, newRespStream(strings.NewReader(in), "agent", true, "openai", false))
	if c := strings.Count(out, "data: [DONE]"); c != 1 {
		t.Errorf("expected exactly one [DONE], got %d in %q", c, out)
	}
}

func TestRespStream_AnthropicNoDoneAppended(t *testing.T) {
	// Anthropic SSE has no [DONE] concept — appending one is protocol
	// pollution. The model field (message_start) is still rewritten.
	in := `event: message_start` + "\n" +
		`data: {"type":"message_start","message":{"id":"m1","model":"MiniMax-M3","content":[]}}` + "\n\n" +
		`event: content_block_delta` + "\n" +
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}` + "\n\n" +
		`event: message_stop` + "\n" +
		`data: {"type":"message_stop"}` + "\n\n"
	out := readAll(t, newRespStream(strings.NewReader(in), "claude", true, "anthropic", false))
	if strings.Contains(out, "[DONE]") {
		t.Errorf("anthropic stream polluted with [DONE]: %q", out)
	}
	if !strings.Contains(out, `"model":"claude"`) {
		t.Errorf("model not rewritten in anthropic stream: %q", out)
	}
	if !strings.Contains(out, "Hello") {
		t.Errorf("anthropic content lost: %q", out)
	}
}

func TestRespStream_OpaquePassthrough(t *testing.T) {
	// Content-Encoding responses are forwarded raw: no rewrite, no
	// strip, no [DONE] — running transforms over compressed bytes can
	// only corrupt them.
	in := `data: {"model":"MiniMax-M3","choices":[{"delta":{"content":"<think>x</think>hi"}}]}` + "\n\n"
	rs := newRespStream(strings.NewReader(in), "agent", true, "openai", true)
	out := readAll(t, rs)
	if out != in {
		t.Errorf("opaque body modified:\n got %q\nwant %q", out, in)
	}
	if a := rs.Applied(); len(a) != 1 || a[0] != "opaque" {
		t.Errorf("applied = %v, want [opaque]", a)
	}
}

func TestRespStream_AppliedTracking(t *testing.T) {
	// The applied list must name every transform that ran, so the audit
	// log can explain any upstream/client byte difference.
	in := `data: {"model":"MiniMax-M3","choices":[{"delta":{"content":"<think>reason</think>\n\nreply"}}]}` + "\n\n"
	rs := newRespStream(strings.NewReader(in), "agent", true, "openai", false)
	out := readAll(t, rs)
	if !strings.Contains(out, "reply") {
		t.Fatalf("content lost: %q", out)
	}
	want := map[string]bool{"buffered": true, "model_rewrite": true, "think_strip": true, "done_appended": true}
	got := map[string]bool{}
	for _, a := range rs.Applied() {
		got[a] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("applied missing %q: %v", k, rs.Applied())
		}
	}
	if raw := string(rs.RawPreStrip()); !strings.Contains(raw, "<think>reason</think>") {
		t.Errorf("RawPreStrip lost the think block: %q", raw)
	}
}

func TestRespStream_ReasoningContentStreams(t *testing.T) {
	// DeepSeek-style reasoning in a dedicated field is well-behaved:
	// the stream must settle into passthrough on the first
	// reasoning_content delta, not wait for regular content.
	ev1 := `data: {"model":"deepseek-v4","choices":[{"delta":{"reasoning_content":"thinking..."}}]}` + "\n\n"
	ev2 := `data: {"model":"deepseek-v4","choices":[{"delta":{"content":"answer"}}]}` + "\n\n"
	sr := &scriptReader{chunks: [][]byte{[]byte(ev1), []byte(ev2)}}
	rs := newRespStream(sr, "agent", true, "openai", false)

	buf := make([]byte, 64<<10)
	n, err := rs.Read(buf)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if sr.i != 1 || !strings.Contains(string(buf[:n]), "thinking...") {
		t.Errorf("reasoning_content delta not streamed immediately (reads=%d, out=%q)", sr.i, buf[:n])
	}
	rest := readAll(t, rs)
	if !strings.Contains(rest, "answer") {
		t.Errorf("content lost: %q", rest)
	}
}

func TestRespStream_ToolCallOnlyStreams(t *testing.T) {
	// Pure tool-call responses (no text content at all) are the bread
	// and butter of agent loops — they must stream, not buffer.
	ev1 := `data: {"model":"M","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"read","arguments":""}}]}}]}` + "\n\n"
	ev2 := `data: {"model":"M","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"x\"}"}}]}}]}` + "\n\n"
	sr := &scriptReader{chunks: [][]byte{[]byte(ev1), []byte(ev2)}}
	rs := newRespStream(sr, "agent", true, "openai", false)

	buf := make([]byte, 64<<10)
	n, err := rs.Read(buf)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if sr.i != 1 || !strings.Contains(string(buf[:n]), `"name":"read"`) {
		t.Errorf("tool-call delta not streamed immediately (reads=%d, out=%q)", sr.i, buf[:n])
	}
}

func TestRespStream_ThinkingProcessBuffered(t *testing.T) {
	// thinking=medium shape through the full pipeline: detected on the
	// first content event, buffered, stripped, [DONE] appended with
	// intact SSE framing.
	roleLine := "data: {\"delta\":{\"role\":\"assistant\"}}\n\n"
	thinkingLine := "data: {\"delta\":{\"content\":\"Thinking Process:\\n\\n1. **Drafting**\"}}\n\n"
	finalLine := "data: {\"delta\":{\"content\":\"    Looks good. Proceed the reply\"}}\n\n"
	rs := newRespStream(strings.NewReader(roleLine+thinkingLine+finalLine), "agent", true, "openai", false)
	out := readAll(t, rs)
	if strings.Contains(out, "Thinking Process") || strings.Contains(out, "Looks good") {
		t.Errorf("thinking leaked: %q", out)
	}
	if !strings.Contains(out, "the reply") {
		t.Errorf("final response lost: %q", out)
	}
	got := map[string]bool{}
	for _, a := range rs.Applied() {
		got[a] = true
	}
	if !got["buffered"] || !got["thinking_process_strip"] {
		t.Errorf("applied = %v, want buffered + thinking_process_strip", rs.Applied())
	}
}

func TestRespStream_ResumesStreamingAfterThinkCloses(t *testing.T) {
	// M3 thinking mode: buffering must last only through the think
	// block. Once </think> arrives, the stripped prefix is emitted and
	// the remaining events stream live — before the source is drained.
	ev1 := `data: {"model":"M","choices":[{"delta":{"role":"assistant"}}]}` + "\n\n"
	ev2 := `data: {"model":"M","choices":[{"delta":{"content":"<think>step 1."}}]}` + "\n\n"
	ev3 := `data: {"model":"M","choices":[{"delta":{"content":" step 2.</think>\n\nanswer starts"}}]}` + "\n\n"
	ev4 := `data: {"model":"M","choices":[{"delta":{"content":" and continues"}}]}` + "\n\n"
	sr := &scriptReader{chunks: [][]byte{[]byte(ev1 + ev2), []byte(ev3), []byte(ev4)}}
	rs := newRespStream(sr, "agent", true, "openai", false)

	// Drive reads until the first output appears; remember how many
	// source chunks had been consumed at that moment.
	buf := make([]byte, 64<<10)
	var first string
	for i := 0; i < 10 && first == ""; i++ {
		n, err := rs.Read(buf)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if n > 0 {
			first = string(buf[:n])
		}
	}
	if first == "" {
		t.Fatal("no output produced")
	}
	if sr.i > 2 {
		t.Errorf("output only appeared after %d source chunks; want resume right after </think> (2)", sr.i)
	}
	if strings.Contains(first, "step 1") || strings.Contains(first, "<think>") {
		t.Errorf("think content leaked on resume: %q", first)
	}
	if !strings.Contains(first, "answer starts") {
		t.Errorf("post-think content missing from resumed stream: %q", first)
	}

	rest := readAll(t, rs)
	full := first + rest
	if !strings.Contains(full, "and continues") {
		t.Errorf("streamed continuation lost: %q", full)
	}
	if c := strings.Count(full, "data: [DONE]"); c != 1 {
		t.Errorf("expected exactly one [DONE], got %d", c)
	}
	got := map[string]bool{}
	for _, a := range rs.Applied() {
		got[a] = true
	}
	if !got["think_strip"] || !got["resumed_stream"] {
		t.Errorf("applied = %v, want think_strip + resumed_stream", rs.Applied())
	}
	if raw := string(rs.RawPreStrip()); !strings.Contains(raw, "<think>step 1.") || !strings.Contains(raw, "step 2.</think>") {
		t.Errorf("RawPreStrip lost the think block: %q", raw)
	}
}

// TestRespStream_UndecidedOverflowDegradesToOpaque locks in that bufferedCap
// also bounds modeUndecided, not just modeBuffered: a stream that never
// produces a decisive event — e.g. Anthropic's periodic "ping" keepalive
// events sent during a long wait before the first real content — must not
// grow s.pending without bound while stuck in modeUndecided. Large chunks
// (rather than many tiny simulated pings) keep this test fast: what's under
// test is the byte count crossing bufferedCap, not the shape of any one
// event.
func TestRespStream_UndecidedOverflowDegradesToOpaque(t *testing.T) {
	rs := newRespStream(strings.NewReader(""), "agent", true, "openai", false)
	filler := bytes.Repeat([]byte("x"), 1<<20) // 1MB, contains no "\n\n"
	for i := 0; i < 40 && !rs.opaque; i++ {    // 40MB > bufferedCap (32MB)
		rs.ingest(filler)
	}
	if !rs.opaque {
		t.Fatal("expected the stream to degrade to opaque after crossing bufferedCap while undecided")
	}
	if rs.pending != nil {
		t.Errorf("pending should be drained into out on overflow, got %d bytes left", len(rs.pending))
	}
	if len(rs.out) < bufferedCap {
		t.Errorf("accumulated bytes should have been flushed to out, got %d", len(rs.out))
	}
	found := false
	for _, a := range rs.Applied() {
		if a == "overflow_raw_passthrough" {
			found = true
		}
	}
	if !found {
		t.Errorf("applied should record overflow_raw_passthrough, got %v", rs.Applied())
	}

	// Once opaque, further bytes pass raw with no attempted normalization —
	// direct-connection behavior, same contract as the modeBuffered overflow path.
	rs.ingest([]byte(`data: {"model":"MiniMax-M3","choices":[{"delta":{"content":"hi"}}]}` + "\n\n"))
	if !strings.Contains(string(rs.out), `"model":"MiniMax-M3"`) {
		t.Errorf("post-overflow bytes must pass through raw (no model rewrite), got tail=%q",
			string(rs.out[len(rs.out)-200:]))
	}
}

// TestRespStream_CRLFFramingSuspectedAtEOF covers an upstream that frames
// SSE events with "\r\n\r\n" instead of "\n\n" (SSE-spec-legal, but unlike
// anything any currently integrated vendor sends). eventSep never finds a
// boundary, so the stream stays modeUndecided until EOF and falls back to
// the whole-body path — content still comes through correct and complete
// (model rewritten, no corruption), just without incremental streaming.
// The only observable difference from a plain buffered response should be
// the crlf_framing_suspected marker, added purely for `vmr report`
// visibility into why this response never streamed.
func TestRespStream_CRLFFramingSuspectedAtEOF(t *testing.T) {
	in := "data: {\"model\":\"MiniMax-M3\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\r\n\r\n"
	rs := newRespStream(strings.NewReader(in), "agent", true, "openai", false)
	out := readAll(t, rs)
	if !strings.Contains(out, `"model":"agent"`) {
		t.Errorf("model not rewritten despite CRLF framing: %q", out)
	}
	if !strings.Contains(out, `"content":"hi"`) {
		t.Errorf("content lost despite CRLF framing: %q", out)
	}
	found := false
	for _, a := range rs.Applied() {
		if a == "crlf_framing_suspected" {
			found = true
		}
	}
	if !found {
		t.Errorf("applied should record crlf_framing_suspected, got %v", rs.Applied())
	}
}

// TestRespStream_CRLFFramingSuspectedOnOverflow is the CRLF-framing
// counterpart to TestRespStream_UndecidedOverflowDegradesToOpaque: a
// response that never resolves out of modeUndecided (no "\n\n" anywhere)
// AND crosses bufferedCap should carry both overflow_raw_passthrough and
// crlf_framing_suspected — the two conditions are independent and can
// co-occur.
func TestRespStream_CRLFFramingSuspectedOnOverflow(t *testing.T) {
	rs := newRespStream(strings.NewReader(""), "agent", true, "openai", false)
	rs.ingest([]byte("data: {\"model\":\"MiniMax-M3\"}\r\n\r\n")) // the only CRLF boundary in the whole stream
	filler := bytes.Repeat([]byte("x"), 1<<20)                  // 1MB, contains no "\n\n" or "\r\n\r\n"
	for i := 0; i < 40 && !rs.opaque; i++ {                     // 40MB > bufferedCap (32MB)
		rs.ingest(filler)
	}
	if !rs.opaque {
		t.Fatal("expected the stream to degrade to opaque after crossing bufferedCap while undecided")
	}
	var sawOverflow, sawCRLF bool
	for _, a := range rs.Applied() {
		sawOverflow = sawOverflow || a == "overflow_raw_passthrough"
		sawCRLF = sawCRLF || a == "crlf_framing_suspected"
	}
	if !sawOverflow || !sawCRLF {
		t.Errorf("applied should record both overflow_raw_passthrough and crlf_framing_suspected, got %v", rs.Applied())
	}
}

// TestRespStream_ManyPingsThenContent exercises the decide() scan-offset:
// a long run of non-decisive events (Anthropic-style pings) followed by a
// decisive content event must settle into passthrough with every withheld
// ping released in order — the offset must not skip or duplicate events.
func TestRespStream_ManyPingsThenContent(t *testing.T) {
	var in strings.Builder
	for i := 0; i < 500; i++ {
		in.WriteString(`event: ping` + "\n" + `data: {"type":"ping"}` + "\n\n")
	}
	in.WriteString(`data: {"delta":{"content":"hello"}}` + "\n\n")
	rs := newRespStream(oneByteReader{strings.NewReader(in.String())}, "agent", true, "openai", false)
	out := readAll(t, rs)
	if c := strings.Count(out, `{"type":"ping"}`); c != 500 {
		t.Errorf("expected all 500 withheld pings released exactly once, got %d", c)
	}
	if !strings.Contains(out, `"content":"hello"`) {
		t.Errorf("decisive content event missing: tail=%q", out[len(out)-120:])
	}
	if strings.Contains(out, "}data:") || strings.Contains(out, "}event:") {
		t.Errorf("SSE framing corrupted around the release boundary")
	}
}

// dataThenErrReader returns its payload together with a non-EOF error in the
// first Read — the "TCP delivered final bytes along with the reset" shape.
type dataThenErrReader struct {
	data   []byte
	err    error
	served bool
}

func (r *dataThenErrReader) Read(p []byte) (int, error) {
	if !r.served {
		r.served = true
		return copy(p, r.data), r.err
	}
	return 0, r.err
}

// TestRespStream_DeliversBytesBeforeSrcError: bytes made deliverable by the
// same Read that produced a mid-stream error must reach the client before
// the error surfaces — a direct connection would have handed them over too.
func TestRespStream_DeliversBytesBeforeSrcError(t *testing.T) {
	ev := "data: {\"delta\":{\"content\":\"hi\"}}\n\n"
	rs := newRespStream(&dataThenErrReader{data: []byte(ev), err: errors.New("conn reset")}, "agent", true, "openai", false)

	buf := make([]byte, 4096)
	n, err := rs.Read(buf)
	if err != nil || n == 0 {
		t.Fatalf("first read must deliver the ingested bytes, got n=%d err=%v", n, err)
	}
	if !strings.Contains(string(buf[:n]), `"content":"hi"`) {
		t.Errorf("delivered bytes wrong: %q", buf[:n])
	}
	if _, err := rs.Read(buf); err == nil || err.Error() != "conn reset" {
		t.Errorf("second read must surface the source error, got %v", err)
	}
}

// TestSoftBlockDetected_NonStreaming covers the buffered path (finalizeBuffered):
// a MiniMax 2xx body embedding input_sensitive must be recorded in Applied()
// as an observation, with the client-visible bytes left untouched.
func TestSoftBlockDetected_NonStreaming(t *testing.T) {
	in := `{"id":"1","input_sensitive":true,"choices":[{"message":{"role":"assistant","content":""}}],"model":"MiniMax-M3"}`
	rs := newRespStream(strings.NewReader(in), "agent", false, "openai", false)
	out := readAll(t, rs)
	if !strings.Contains(out, `"input_sensitive":true`) {
		t.Errorf("soft-block marker must reach the client unchanged (detection-only), got %q", out)
	}
	if !strings.Contains(out, `"model":"agent"`) {
		t.Errorf("model rewrite must still apply alongside detection, got %q", out)
	}
	found := false
	for _, a := range rs.Applied() {
		if a == "soft_block_detected" {
			found = true
		}
	}
	if !found {
		t.Errorf("applied should record soft_block_detected, got %v", rs.Applied())
	}
}

// TestSoftBlockDetected_Streaming covers the passthrough path (emitBlock):
// the marker can arrive inside an ordinary SSE data: line and must still be
// flagged without altering transport mode or bytes.
func TestSoftBlockDetected_Streaming(t *testing.T) {
	roleLine := `data: {"delta":{"role":"assistant"}}` + "\n\n"
	textLine := `data: {"delta":{"content":"hi"},"output_sensitive":true}` + "\n\n"
	rs := newRespStream(strings.NewReader(roleLine+textLine), "agent", true, "openai", false)
	out := readAll(t, rs)
	if !strings.Contains(out, `"output_sensitive":true`) {
		t.Errorf("soft-block marker must reach the client unchanged, got %q", out)
	}
	found := false
	for _, a := range rs.Applied() {
		if a == "soft_block_detected" {
			found = true
		}
	}
	if !found {
		t.Errorf("applied should record soft_block_detected, got %v", rs.Applied())
	}
}

// TestSoftBlockDetected_NoFalsePositive is a control: an ordinary response
// with neither marker must never gain the norm entry.
func TestSoftBlockDetected_NoFalsePositive(t *testing.T) {
	in := `{"id":"1","choices":[{"message":{"content":"all good"}}],"model":"m"}`
	rs := newRespStream(strings.NewReader(in), "agent", false, "openai", false)
	readAll(t, rs)
	for _, a := range rs.Applied() {
		if a == "soft_block_detected" {
			t.Errorf("false positive: applied=%v for a clean response", rs.Applied())
		}
	}
}

// TestRespStream_ThinkQuotedMidTextUntouched locks in the strip guard: a
// reply that merely QUOTES a <think>…</think> block mid-text (user asking
// about the tag format, a code sample echoing it) is NOT the MiniMax
// thinking shape — the quoted span must reach the client intact, streamed.
func TestRespStream_ThinkQuotedMidTextUntouched(t *testing.T) {
	in := `data: {"choices":[{"delta":{"content":"The tag format is <think>reasoning goes here</think> followed by the reply."}}]}` + "\n\n"
	rs := newRespStream(strings.NewReader(in), "agent", true, "openai", false)
	out := readAll(t, rs)
	if !strings.Contains(out, "<think>reasoning goes here</think>") {
		t.Errorf("quoted think block was stripped from a normal reply: %q", out)
	}
	for _, a := range rs.Applied() {
		if a == "think_strip" || a == "buffered" {
			t.Errorf("normal reply must stream untouched, applied = %v", rs.Applied())
		}
	}
}

// TestRespStream_ThinkQuotedMidTextNonStreamUntouched is the buffered-path
// counterpart: a single JSON body quoting <think> mid-content must survive
// finalizeBuffered's guarded strip.
func TestRespStream_ThinkQuotedMidTextNonStreamUntouched(t *testing.T) {
	in := `{"model":"M","choices":[{"message":{"role":"assistant","content":"Explanation: models emit <think>steps</think> before answering."}}]}`
	out := readAll(t, newRespStream(strings.NewReader(in), "agent", false, "openai", false))
	if !strings.Contains(out, "<think>steps</think>") {
		t.Errorf("quoted think block stripped from JSON body: %q", out)
	}
}

// TestRespStream_AnthropicTextThinkStripped keeps the anthropic-face
// coverage the guard refactor must not lose: a text delta OPENING with
// <think> is the same MiniMax pathology and is still buffered + stripped.
func TestRespStream_AnthropicTextThinkStripped(t *testing.T) {
	in := `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"<think>internal</think>\n\nvisible"}}` + "\n\n"
	rs := newRespStream(strings.NewReader(in), "agent", true, "anthropic", false)
	out := readAll(t, rs)
	if strings.Contains(out, "internal") {
		t.Errorf("anthropic-face think block leaked: %q", out)
	}
	if !strings.Contains(out, "visible") {
		t.Errorf("post-think content lost: %q", out)
	}
}
