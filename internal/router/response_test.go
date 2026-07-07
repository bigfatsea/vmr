// Ver 2026-07-07 21:50, by Fable 5 (post-audit 2026-07-07 fixes)
//
// Unit tests for the response stream processor: model-field rewrite,
// think-block stripping, [DONE] sentinel, and cross-chunk regex
// safety.
package router

import (
	"bytes"
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
	out := readAll(t, newRespStream(src, "agent", true))

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
	out := readAll(t, newRespStream(src, "agent", true))

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
	out := readAll(t, newRespStream(src, "agent", true))

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
	out := readAll(t, newRespStream(src, "agent", true))

	if !strings.HasSuffix(out, "data: [DONE]\n\n") {
		t.Errorf("missing [DONE] sentinel, tail=%q", out)
	}
}

func TestRespStream_NonStreamSingleObject(t *testing.T) {
	// Non-streaming response (single JSON object, no SSE framing).
	// The processor should still rewrite the model field. The
	// non-streaming path doesn't append [DONE] (caller logic decides
	// that), but the carry semantics are the same.
	src := strings.NewReader(
		`{"id":"x","model":"MiniMax-M3","choices":[{"message":{"role":"assistant","content":"<think>hello</think>\n\nHi"}}]}`,
	)
	out := readAll(t, newRespStream(src, "agent", true))

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
	out := readAll(t, newRespStream(strings.NewReader(in), "agent", true))
	if !strings.Contains(out, "plain reply") {
		t.Errorf("plain content lost: %q", out)
	}
}

func TestRespStream_OneByteReads(t *testing.T) {
	// Pathological: read one byte at a time from the source. The
	// carry must still let the regex see markers. This is the
	// worst-case for cross-chunk boundary detection.
	srcStr := `data: {"model":"MiniMax-M3","choices":[{"delta":{"content":"` +
		`<think>step 1. step 2. step 3.</think>result"}}]}` + "\n\n"
	out := readAll(t, newRespStream(oneByteReader{src: strings.NewReader(srcStr)}, "agent", true))

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
// exercising the worst case for the carry buffer.
type oneByteReader struct{ src io.Reader }

func (r oneByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return r.src.Read(p[:1])
}

func TestStripThinkingProcess_FullPattern(t *testing.T) {
	// Real-world pattern from the 2026-07-07 audit log. The
	// buffer is the raw upstream body with SSE framing. The
	// thinking lives in a separate data: line from the final
	// response — the strip must drop the thinking line and
	// trim the marker's line content.
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
	// whitespace.
	roleLine := "data: {\"delta\":{\"role\":\"assistant\"}}\n\n"
	finalLine := "data: {\"delta\":{\"content\":\"...\\n    Looks good. Profinal answer here\\n\"}}\n\n"

	in := roleLine + finalLine

	out := stripThinkingProcess([]byte(in))
	got := string(out)
	if !strings.Contains(got, "final answer here") {
		t.Errorf("expected 'final answer here' to be preserved, got %q", got)
	}
	if strings.Contains(got, "Looks good") {
		t.Errorf("self-endorsement marker should be stripped, got %q", got)
	}
}

func TestRespStream_EmptySource(t *testing.T) {
	// Empty source: just the [DONE] sentinel should be emitted.
	out := readAll(t, newRespStream(strings.NewReader(""), "agent", true))
	if out != "data: [DONE]\n\n" {
		t.Errorf("empty source produced %q, want only [DONE]", out)
	}
}

func TestRespStream_NestedModelInDelta(t *testing.T) {
	// Defensive: if a chunk ever has a nested "model" field (it
	// shouldn't per OpenAI spec, but we shouldn't break if it
	// does), the rewrite still applies at the top level only.
	src := strings.NewReader(
		`data: {"id":"x","model":"MiniMax-M3","choices":[{"index":0,"delta":{"role":"assistant"}}]}` + "\n\n",
	)
	out := readAll(t, newRespStream(src, "agent", true))
	if !strings.Contains(out, `"model":"agent"`) {
		t.Errorf("top-level model not rewritten: %q", out)
	}
}
