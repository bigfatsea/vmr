// Ver 2026-07-07 22:30, by Fable 5 (post-audit 2026-07-07 fixes)
//
// Response stream processor: the three transformations VMR applies to
// the upstream response body before forwarding to the client.
//
//   1. Rewrite the "model" field in each SSE chunk from the upstream's
//      actual model name (e.g. "MiniMax-M3") back to the virtual model
//      name the client sent (e.g. "agent"). Without this, clients that
//      track the model per-message (OpenAI SDK does — see
//      ChatCompletionStream.accumulateChatCompletion, line 250) see a
//      mismatch with their request and either fail cache lookups, route
//      hooks off the wrong branch, or — in OpenClaw's case — silently
//      drop tool-call roundtrips.
//
//   2. Strip <think>...</think> blocks from the content. MiniMax M3
//      emits its reasoning as inline <think>...</think> text inside
//      delta.content (not in a separate reasoning_content field like
//      DeepSeek/Anthropic). When this gets persisted into the
//      assistant message history and sent back on the next request,
//      the model sees its own previous reasoning and locks into a
//      feedback loop (the audit log's lines 4-27 show this exact
//      pattern — repeated read() on non-existent files, prompt_tokens
//      growing 16K → 43K across 24 turns, line 3 hitting 483K tokens
//      and finish_reason=length). Stripping the think blocks at the
//      response boundary breaks the loop without forcing clients to
//      change.
//
//      The think block can be thousands of bytes long, so a
//      streaming carry-based approach is unsafe — a 200-byte carry
//      is smaller than most think blocks, and the state machine
//      hits corner cases (bytes inside an open think block get
//      "lost" when the output is empty). Instead, we buffer the
//      entire response in memory, apply the transformations as a
//      single regex pass, then stream the result. Chat completions
//      are bounded (a few hundred KB at most) so the memory cost
//      is acceptable, and the simpler logic eliminates a class of
//      bugs. Streaming was nice-to-have, correctness is must-have.
//
//   3. Append `data: [DONE]\n\n` on EOF for streaming responses
//      only. MiniMax closes the SSE stream without sending the
//      OpenAI-spec-required [DONE] sentinel. The OpenAI JS SDK's
//      fromSSEResponse checks for [DONE] to set done=true
//      (streaming.mjs:31); without it, the SDK relies on TCP EOF,
//      which is fine in the happy path but races with the
//      stream_idle watchdog and X-Stainless-Timeout under load.
//      Non-streaming responses are single JSON objects, not SSE
//      streams, so [DONE] is suppressed there.
package router

import (
	"bytes"
	"io"
	"regexp"
)

// modelFieldPattern matches the SSE chunk's top-level "model" field
// value. The capture groups preserve the `"model":"` opener and the
// closing `"` so the value between can be replaced without disturbing
// the surrounding JSON. `[^"]*` is greedy by Go regex semantics, but
// the surrounding `"` anchors force it to stop at the next quote.
var modelFieldPattern = regexp.MustCompile(`("model":\s*")[^"]*"`)

// thinkPattern matches a complete <think>...</think> block, non-greedy
// so the first closing </think> ends the block. (?s) makes `.` match
// newlines so the regex works across SSE message boundaries (each
// data: line is one JSON message, but the regex sees the whole
// buffered response as a single byte stream).
var thinkPattern = regexp.MustCompile(`(?s)<think>.*?</think>`)

// thinkTrailingNewline trims the literal "\n\n" that MiniMax appends
// after every </think> close. Without this, the assistant content
// starts with two stray newlines on every turn.
var thinkTrailingNewline = regexp.MustCompile(`</think>\n\n`)

// thinkingProcessEndorsement matches the self-endorsement markers
// the model uses to signal "end of thinking, start of final
// response". The model emits these at the end of a "Final Polish"
// subsection, immediately followed by the actual response text
// on the same line.
var thinkingProcessEndorsement = regexp.MustCompile(`Looks good\.[ \t]+Pro(?:ceed|ceed)?`)

// thinkingProcessFullMatch captures the entire self-endorsement
// line so the final response (which starts on the same line right
// after the marker) is preserved. The regex does NOT anchor at
// `^` (line start) because the buffer is the raw upstream body
// including SSE framing — the "Looks good. Pro" marker lives
// INSIDE the content of one of the data: lines (i.e., after
// several hundred bytes of JSON escaping), never at a line
// start. Optional leading whitespace is allowed to handle the
// model's indentation of "Final Polish" draft content.
var thinkingProcessFullMatch = regexp.MustCompile(`[ \t]*Looks good\.[ \t]+Pro(?:ceed|ceed)?[^\n]*`)

// respStream is an io.Reader wrapper that buffers the upstream
// response in memory, applies the three transformations as a
// single regex pass, and streams the result to the client. The
// buffer size is bounded by the upstream's own response size — a
// chat completion is at most a few hundred KB.
type respStream struct {
	src         io.Reader
	clientModel string
	isSSE       bool

	buf  []byte // accumulated raw bytes from src
	done bool   // src has returned io.EOF and the buffer is finalized
	out  []byte // pending processed bytes to deliver to the client
}

func newRespStream(src io.Reader, clientModel string, isSSE bool) *respStream {
	return &respStream{src: src, clientModel: clientModel, isSSE: isSSE}
}

// Read implements io.Reader. It drains any pending processed
// bytes, then pulls more from src. On the first io.EOF from src,
// it finalizes the buffer (runs both regex passes), appends
// "data: [DONE]\n\n" for streaming, and signals EOF on the
// next call after draining.
func (s *respStream) Read(out []byte) (int, error) {
	// 1. Drain any pending processed bytes.
	if len(s.out) > 0 {
		n := copy(out, s.out)
		s.out = s.out[n:]
		return n, nil
	}

	// 2. If we're done, return EOF.
	if s.done {
		return 0, io.EOF
	}

	// 3. Pull more from src.
	var scratch [4096]byte
	n, err := s.src.Read(scratch[:])
	if n > 0 {
		s.buf = append(s.buf, scratch[:n]...)
	}
	if err == io.EOF {
		s.done = true
		// Finalize: apply transformations to the whole buffer.
		// Model field rewrite first (it's a literal replace, cheap).
		s.buf = modelFieldPattern.ReplaceAll(s.buf, []byte(`${1}`+s.clientModel+`"`))
		// Think-strip second (regex scan over the full buffer).
		s.buf = thinkPattern.ReplaceAll(s.buf, nil)
		// Trim the trailing "\n\n" MiniMax appends after every
		// </think> so the assistant content doesn't start with
		// two stray newlines.
		if bytes.Contains(s.buf, []byte("</think>\n\n")) {
			s.buf = thinkTrailingNewline.ReplaceAll(s.buf, []byte("</think>"))
		}
		// Thinking Process: heuristic — MiniMax M3 with
		// thinking=medium emits a structured plain-text thinking
		// section before the actual final response. The
		// self-endorsement marker "Looks good. Pro" / "Proceed"
		// is the boundary between the thinking and the final
		// response. Without this strip the user sees the
		// model's chain-of-thought inline.
		s.buf = stripThinkingProcess(s.buf)
		// Append [DONE] for streaming responses.
		if s.isSSE {
			s.buf = append(s.buf, []byte("data: [DONE]\n\n")...)
		}
		s.out = s.buf
		s.buf = nil
	} else if err != nil {
		return 0, err
	}

	// 4. Deliver pending or signal EOF.
	if len(s.out) > 0 {
		n := copy(out, s.out)
		s.out = s.out[n:]
		return n, nil
	}
	return 0, nil
}

// stripThinkingProcess removes MiniMax M3's "Thinking Process:"
// structured thinking section from the response, preserving the
// SSE framing of every other data: line. Returns b unchanged if
// the pattern is not detected — better to pass through than to
// drop the actual response.
//
// The pattern MiniMax emits under thinking=medium is:
//
//   Thinking Process:
//
//   1. **Analyze the Request:**
//      *   ...
//   2. **Examine the Runtime Context:**
//      *   ...
//   ...
//   N. **Final Polish:**
//      <draft 1>
//      <blank>
//      <draft 2>
//      <blank>
//      ...
//      Looks good. Pro<final response line 1>
//
//   <final response paragraphs>
//
// The boundary between the thinking and the final response is
// the "Looks good. Pro" (or "Looks good. Proceed") self-endorsement
// marker. The actual final response starts on the SAME line,
// immediately after "Pro".
//
// Important: the thinking content and the final response are in
// DIFFERENT data: lines (the second and third respectively in
// the audit log). The strip must (a) drop the data: line(s)
// between the role marker and the marker line, AND (b) trim
// the "Looks good. Pro" prefix from the content of the marker
// line. Just trimming the latter — as an earlier version did
// — leaves the thinking in place because the model wrote it as
// a separate content chunk.
//
// Strategy: split the buffer into data: lines on "\n\n" (safe
// because the content is JSON-escaped so embedded newlines are
// "\\n" not "\n"). Find the line containing the LAST
// self-endorsement marker. Drop every line between the first
// (role) line and the marker line. Then trim the marker's
// content to start after the "Pro" / "Proceed" word.
func stripThinkingProcess(b []byte) []byte {
	// Find the LAST self-endorsement marker.
	loc := thinkingProcessFullMatch.FindAllIndex(b, -1)
	if len(loc) == 0 {
		return b
	}
	last := loc[len(loc)-1]
	endorseLoc := thinkingProcessEndorsement.FindIndex(b[last[0]:last[1]])
	if endorseLoc == nil {
		return b
	}
	// cut is the byte offset immediately after "Pro" /
	// "Proceed" — the start of the actual final response
	// text within the marker's data: line.
	cut := last[0] + endorseLoc[1]

	// Split into data: lines on "\n\n" (the SSE separator).
	// Each line is a "data: <json>".
	lines := bytes.Split(b, []byte("\n\n"))
	// Find the LAST data: line that contains the marker. The
	// model may emit "Looks good. Pro" inside the thinking
	// section too (e.g., as a self-endorsement of an early
	// draft), so we need the actual final-response line, not
	// any of the thinking lines. The global regex match
	// position in `last` is in the buffer; map it back to
	// the line index.
	markerLineIdx := -1
	for i, line := range lines {
		if thinkingProcessFullMatch.FindIndex(line) != nil {
			markerLineIdx = i // overwrite so we end up at the LAST one
		}
	}
	if markerLineIdx < 0 || markerLineIdx >= len(lines) {
		// Shouldn't happen — the global regex already found
		// the marker. Pass through.
		return b
	}

	// Drop lines between the first line and the marker line
	// (inclusive of the thinking-only content). The first
	// line is always kept (it's the role marker).
	keep := make([][]byte, 0, len(lines)-markerLineIdx+1)
	keep = append(keep, lines[0])
	// Trim the marker's content to start after the "Pro" /
	// "Proceed" word. The cut offset is relative to the
	// whole buffer; compute the offset within the marker's
	// line.
	lineStart := 0
	for i := 0; i < markerLineIdx; i++ {
		lineStart += len(lines[i]) + 2 // +2 for the "\n\n" separator
	}
	cutInLine := cut - lineStart
	markerLine := lines[markerLineIdx]
	if cutInLine > 0 && cutInLine < len(markerLine) {
		// Find the content field start within the marker
		// line. We trim everything between the start of
		// the content VALUE and `cutInLine` — that range
		// is the thinking prefix and the "Looks good.
		// Pro" marker.
		contentField := []byte(`"content":"`)
		cfIdx := bytes.Index(markerLine, contentField)
		if cfIdx >= 0 {
			contentValueStart := cfIdx + len(contentField)
			if contentValueStart <= cutInLine {
				newLine := make([]byte, 0, len(markerLine))
				newLine = append(newLine, markerLine[:contentValueStart]...)
				newLine = append(newLine, markerLine[cutInLine:]...)
				markerLine = newLine
			}
		}
	}
	keep = append(keep, markerLine)
	// Keep all lines after the marker line (finish_reason,
	// usage, [DONE] sentinel — everything VMR appends after
	// the data: lines).
	for i := markerLineIdx + 1; i < len(lines); i++ {
		// Skip the trailing empty element from a final "\n\n"
		// split.
		if len(lines[i]) == 0 {
			continue
		}
		keep = append(keep, lines[i])
	}
	// Re-join. The trailing "\n\n" is preserved by joining
	// with the separator, so the client gets a well-formed
	// SSE stream.
	return bytes.Join(keep, []byte("\n\n"))
}
