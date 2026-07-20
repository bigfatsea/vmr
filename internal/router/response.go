// Ver 2026-07-17 02:00, by Sonnet 5
//
// Response normalizer. Guiding principle: what the client receives through
// VMR must match what it would receive calling the provider directly, except
// for the virtual-model abstraction (the "model" field is rewritten back to
// the client's virtual name) and two MiniMax-M3-specific repairs that only
// engage when their exact upstream shape is detected:
//
//   - inline <think>...</think> reasoning inside content (thinking mode):
//     if persisted into the assistant history it feeds the model its own
//     reasoning next turn and locks it into a feedback loop;
//   - plain-text "Thinking Process:" + numbered sections + "Final Polish"
//     drafts (thinking=medium): chain-of-thought shown to the user.
//
// Two transport modes, chosen per response:
//
//	passthrough (default for SSE): events are forwarded to the client as
//	they arrive — true streaming, byte-identical except the model-field
//	rewrite. The mode is chosen as soon as the first payload-bearing SSE
//	event proves the response is NOT one of the MiniMax thinking shapes.
//
//	buffered: the whole body is accumulated and normalized in one regex
//	pass at EOF. Used for (a) non-SSE responses (single JSON object — the
//	client waits for the full body either way), (b) SSE responses whose
//	first payload event starts with <think> or "Thinking Process:" (the
//	client would see nothing during the thinking phase anyway, so buffering
//	costs no perceived latency), and (c) SSE responses that hit EOF before
//	any payload event (tiny or malformed streams).
//
// A response with a Content-Encoding header (upstream compressed it in a
// coding Go's Transport didn't transparently decode) is opaque: forwarded
// raw, no transforms — running regexes over compressed bytes can only
// corrupt them.
//
// The [DONE] sentinel is appended only for openai-protocol SSE responses
// where the upstream didn't send one (MiniMax closes the TCP stream
// without it; the OpenAI SDK's terminator logic wants it). Anthropic SSE
// has no [DONE] concept — never appended there. An upstream that already
// sent [DONE] never gets a second one.
//
// Every transform actually applied is recorded (applied list) so the audit
// log can explain any byte difference between upstream and client.
//
// One entry in that list is detection-only, not a transform: a MiniMax 2xx
// response can embed a compliance flag (input_sensitive/output_sensitive)
// instead of erroring — a "soft block" the failover loop cannot see because
// it never sees a non-2xx status. When spotted, "soft_block_detected" is
// added to the audit norm trail; the bytes reaching the client are
// unchanged and health/failover are untouched.
package router

import (
	"bytes"
	"io"
	"regexp"
)

// Transport modes. A stream starts undecided (SSE) or buffered (non-SSE)
// and settles once; the only later transition is buffered→passthrough when
// an inline <think> block closes and streaming can safely resume.
const (
	modeUndecided = iota
	modeBuffered
	modePassthrough
)

// bufferedCap bounds the buffered mode's memory: a runaway upstream that
// keeps sending forever would otherwise grow the buffer without limit
// (the stream-idle watchdog only catches silence, not endless data).
// Past the cap the accumulated bytes are flushed raw and the response
// degrades to opaque passthrough — direct-connection behavior.
const bufferedCap = 32 << 20

// modelFieldPattern matches every unescaped "model" field value in the
// block — not just the top-level one; it has no JSON-depth tracking, so a
// genuinely nested "model" key (not inside an escaped string) would be
// rewritten too (see TestRespStream_NestedModelInDelta). This differs from
// the request-side RewriteModel, which is a structural scanner limited to
// the top-level key. Harmless in practice: every provider shape this
// package targets only ever carries "model" at the top level. The capture
// group preserves the `"model":"` opener; `[^"]*` stops at the closing
// quote. JSON-escaped quotes inside string values (\") never match the
// bare `"` the pattern requires, so content that merely *mentions* a
// model field is not rewritten.
var modelFieldPattern = regexp.MustCompile(`("model":\s*")[^"]*"`)

// thinkPattern matches a complete <think>...</think> block plus the
// newline padding MiniMax appends after the closer. Content inside SSE
// data lines is JSON-escaped, so the padding is the two-character
// sequence `\n` (backslash-n, matched by `\\n`); real newlines (`\n`)
// are also accepted for non-escaped contexts. Without eating the
// padding, the assistant message starts with two blank lines every turn.
var thinkPattern = regexp.MustCompile(`(?s)<think>.*?</think>(?:\\n|\n)*`)

// thinkingProcessEndorsement matches the self-endorsement marker the
// model uses to signal "end of thinking, start of final response".
var thinkingProcessEndorsement = regexp.MustCompile(`Looks good\.[ \t]+Pro(?:ceed)?`)

// thinkingProcessFullMatch captures the endorsement and the rest of its
// line; the final response starts on the same line right after the
// marker.
var thinkingProcessFullMatch = regexp.MustCompile(`[ \t]*Looks good\.[ \t]+Pro(?:ceed)?[^\n]*`)

var (
	contentFieldMarker    = []byte(`"content":"`)
	textFieldMarker       = []byte(`"text":"`)
	thinkingProcessPrefix = []byte("Thinking Process:")
	thinkOpenMarker       = []byte("<think>")
	thinkCloseMarker      = []byte("</think>")
	doneSentinel          = []byte("data: [DONE]")
	eventSep              = []byte("\n\n")
	crlfEventSepHint      = []byte("\r\n\r\n")
)

// thinkGuardMarkers are the payload fields whose value can carry MiniMax's
// inline-think pathology: openai delta/message content and anthropic text.
// Dedicated reasoning fields (reasoning_content, thinking) are well-behaved
// by construction and never guard-checked.
var thinkGuardMarkers = [][]byte{contentFieldMarker, textFieldMarker}

// thinkShapeGuard reports whether the FIRST non-empty content/text value in b
// starts with <think> — the only shape the think-strip repair targets
// (MiniMax-M3 thinking mode opens the reply with the tag). Without this
// guard, a reply that merely QUOTES a <think>…</think> block mid-text (a
// user asking about the tag format, a code sample echoing it) would have the
// quoted span silently deleted. Symmetric with stripThinkingProcess's own
// first-content-prefix guard; kept as cheap byte scans, same as the rest of
// this file.
func thinkShapeGuard(b []byte) bool {
	for off := 0; off < len(b); {
		idx, mlen := -1, 0
		for _, m := range thinkGuardMarkers {
			if j := bytes.Index(b[off:], m); j >= 0 && (idx < 0 || off+j < idx) {
				idx, mlen = off+j, len(m)
			}
		}
		if idx < 0 {
			return false
		}
		v := trimEscapedWS(b[idx+mlen:])
		if len(v) > 0 && v[0] != '"' { // first non-empty value decides
			return bytes.HasPrefix(v, thinkOpenMarker)
		}
		off = idx + mlen
	}
	return false
}

// softBlockMarkers flag MiniMax's "soft" content-policy block: a 2xx
// response that embeds a compliance flag instead of erroring. Detection is
// observation-only (see package doc) — it only adds "soft_block_detected"
// to the audit norm trail so these otherwise-invisible blocks become
// greppable instead of silently reaching the client as an empty or
// substituted response.
var softBlockMarkers = [][]byte{
	[]byte(`"input_sensitive":true`),
	[]byte(`"output_sensitive":true`),
}

func containsSoftBlockMarker(b []byte) bool {
	for _, m := range softBlockMarkers {
		if bytes.Contains(b, m) {
			return true
		}
	}
	return false
}

// passthroughStringMarkers / passthroughTokenMarkers are event contents
// that prove the response carries well-behaved payload: reasoning in a
// dedicated field, anthropic text/thinking deltas, or tool-call
// arguments. Any of these settles the stream into passthrough mode.
var passthroughStringMarkers = [][]byte{
	textFieldMarker, // same literal as thinkGuardMarkers' entry — reused, not retyped
	[]byte(`"reasoning_content":"`),
	[]byte(`"thinking":"`),
}

var passthroughTokenMarkers = [][]byte{
	[]byte(`"tool_calls"`),
	[]byte(`"partial_json"`),
}

type respStream struct {
	src         io.Reader
	clientModel string
	isSSE       bool
	protocol    string // ingress protocol: decides [DONE] policy
	opaque      bool   // Content-Encoding present: no transforms at all

	mode           int
	pending        []byte // undecided: withheld bytes; passthrough: partial-event tail
	scanned        int    // undecided only: pending[:scanned] holds events already classified undecided — decide resumes after them instead of rescanning (keeps a ping-heavy stream linear, not quadratic)
	buf            []byte // buffered-mode accumulation
	out            []byte // processed bytes ready for the client
	srcErr         error  // non-EOF src error, surfaced after out drains
	done           bool   // src hit EOF and out holds the final bytes
	sawDone        bool   // upstream emitted its own data: [DONE]
	tailNL         bool   // last emitted bytes ended with the SSE separator
	thinkTriggered bool   // buffering was caused by an inline <think> block
	applied        []string
	rawPreStrip    []byte // upstream bytes exactly as received, captured right before think_strip/thinking_process_strip rewrote them — nil unless one of those fired
	scratch        []byte // reused read buffer; lazily allocated once per response (a stack array here would be re-zeroed on every Read call)
}

func newRespStream(src io.Reader, clientModel string, isSSE bool, protocol string, opaque bool) *respStream {
	rs := &respStream{src: src, clientModel: clientModel, isSSE: isSSE, protocol: protocol, opaque: opaque, tailNL: true}
	switch {
	case opaque:
		rs.applied = append(rs.applied, "opaque")
	case !isSSE:
		rs.mode = modeBuffered
	}
	return rs
}

// Applied reports which transforms ran, for the audit trail. Valid after
// the stream has been fully copied.
func (s *respStream) Applied() []string { return s.applied }

// RawPreStrip returns the upstream bytes exactly as received, from the point
// just before a think_strip/thinking_process_strip rewrite ran — nil when
// neither fired. It is the buffered segment only (whatever was accumulated
// at that moment), not a second copy of the whole response: the buffered
// mode already holds these bytes in memory for the regex pass, so this just
// keeps that reference alive instead of discarding it. Valid after the
// stream has been fully copied.
func (s *respStream) RawPreStrip() []byte { return s.rawPreStrip }

func (s *respStream) Read(p []byte) (int, error) {
	if len(s.out) > 0 {
		n := copy(p, s.out)
		s.out = s.out[n:]
		return n, nil
	}
	if s.srcErr != nil {
		return 0, s.srcErr
	}
	if s.done {
		return 0, io.EOF
	}

	if s.scratch == nil {
		s.scratch = make([]byte, 32<<10)
	}
	n, err := s.src.Read(s.scratch)
	if n > 0 {
		s.ingest(s.scratch[:n])
	}
	if err == io.EOF {
		s.finish()
		s.done = true
	} else if err != nil {
		// Deliver whatever the ingest above made deliverable before
		// surfacing the error (a direct connection would have handed the
		// client those TCP-delivered bytes too); the error is returned on
		// the next call once out is drained.
		s.srcErr = err
	}

	if len(s.out) > 0 {
		n := copy(p, s.out)
		s.out = s.out[n:]
		return n, nil
	}
	if s.srcErr != nil {
		return 0, s.srcErr
	}
	// Nothing deliverable yet (mid-event, or withheld pending a mode
	// decision). Zero-length reads let the caller's idle watchdog tick.
	return 0, nil
}

func (s *respStream) ingest(b []byte) {
	if s.opaque {
		s.out = append(s.out, b...)
		return
	}
	switch s.mode {
	case modeBuffered:
		s.buf = append(s.buf, b...)
		// <think>-triggered buffering can resume streaming: once the
		// closing tag is inside complete events, strip the block and
		// stream the rest live — the client waited only through the
		// thinking phase, during which there was nothing to show anyway.
		// (Thinking Process: buffering can't resume — its end marker is
		// only recognizable at the very end of the response.)
		if s.isSSE && s.thinkTriggered {
			if i := bytes.LastIndex(s.buf, eventSep); i >= 0 {
				block := s.buf[:i+len(eventSep)]
				if bytes.Contains(block, thinkCloseMarker) {
					tail := append([]byte(nil), s.buf[i+len(eventSep):]...)
					s.buf = nil
					if thinkPattern.Match(block) {
						s.rawPreStrip = block
						block = thinkPattern.ReplaceAll(block, nil)
						s.noteApplied("think_strip")
					}
					s.mode = modePassthrough
					s.noteApplied("resumed_stream")
					s.emitBlock(block)
					s.pending = tail
					return
				}
			}
		}
		if len(s.buf) > bufferedCap {
			// Runaway stream: give up on normalization, flush raw and
			// degrade to opaque passthrough (= direct behavior).
			s.opaque = true
			s.noteApplied("overflow_raw_passthrough")
			s.out = append(s.out, s.buf...)
			s.buf = nil
		}
	case modeUndecided:
		s.pending = append(s.pending, b...)
		s.decide()
		// A stream that never produces a decisive event (e.g. Anthropic's
		// periodic keepalive "ping" events during a long wait) would
		// otherwise grow s.pending without limit — the bufferedCap guard
		// below only covered modeBuffered. Same degrade-to-opaque response
		// as that overflow path: give up on normalization, flush raw.
		if s.mode == modeUndecided && len(s.pending) > bufferedCap {
			s.noteCRLFFramingIfSuspected(s.pending)
			s.opaque = true
			s.noteApplied("overflow_raw_passthrough")
			s.out = append(s.out, s.pending...)
			s.pending = nil
			s.scanned = 0
		}
	case modePassthrough:
		s.pending = append(s.pending, b...)
		s.emitComplete()
	}
}

// noteCRLFFramingIfSuspected is called only where s.mode is still
// modeUndecided despite s.isSSE (guaranteed here: a non-SSE response is
// forced into modeBuffered at construction, so reaching modeUndecided means
// isSSE was true) — i.e. every "\n\n" this package looks for was scanned for
// and never found. eventSep only recognizes bare-LF event boundaries; an
// upstream framing SSE with "\r\n\r\n" (spec-legal, but unobserved from any
// currently integrated vendor) would land here too. This doesn't change
// behavior — the whole-body fallback already handles it correctly, just
// without incremental streaming — it only leaves a trail so `vmr report`
// can tell a genuinely tiny/malformed response apart from CRLF framing.
func (s *respStream) noteCRLFFramingIfSuspected(b []byte) {
	if bytes.Contains(b, crlfEventSepHint) {
		s.noteApplied("crlf_framing_suspected")
	}
}

// decide scans the complete events withheld so far and settles the mode
// on the first payload-bearing one. Role markers, pings, message_start
// and empty-content chunks don't decide; they stay withheld (a few tiny
// events at most) and are released or buffered with everything else.
func (s *respStream) decide() {
	rest := s.pending[s.scanned:]
	for {
		i := bytes.Index(rest, eventSep)
		if i < 0 {
			// No more complete events; remember how far classification got
			// so the next chunk resumes here instead of rescanning every
			// already-undecided event from the start of pending.
			s.scanned = len(s.pending) - len(rest)
			return
		}
		ev := rest[:i]
		rest = rest[i+len(eventSep):]
		switch classifyEvent(ev) {
		case verdictBuffered:
			s.mode = modeBuffered
			s.scanned = 0
			if s.isSSE {
				s.noteApplied("buffered")
			}
			s.buf = s.pending
			s.pending = nil
			s.thinkTriggered = thinkShapeGuard(s.buf)
			return
		case verdictPassthrough:
			s.mode = modePassthrough
			s.scanned = 0
			s.emitComplete()
			return
		}
	}
}

// emitComplete releases every complete event in pending, keeping only the
// partial tail. The block boundary is always an event separator, so the
// model-field rewrite never straddles a JSON string.
func (s *respStream) emitComplete() {
	i := bytes.LastIndex(s.pending, eventSep)
	if i < 0 {
		return
	}
	block := s.pending[:i+len(eventSep)]
	tail := append([]byte(nil), s.pending[i+len(eventSep):]...)
	s.emitBlock(block)
	s.pending = tail
}

func (s *respStream) emitBlock(block []byte) {
	if len(block) == 0 {
		return
	}
	if !s.sawDone && bytes.Contains(block, doneSentinel) {
		s.sawDone = true
	}
	if containsSoftBlockMarker(block) {
		s.noteApplied("soft_block_detected")
	}
	if modelFieldPattern.Match(block) {
		block = modelFieldPattern.ReplaceAll(block, []byte(`${1}`+s.clientModel+`"`))
		s.noteApplied("model_rewrite")
	}
	s.tailNL = bytes.HasSuffix(block, eventSep)
	s.out = append(s.out, block...)
}

func (s *respStream) finish() {
	if s.opaque {
		return
	}
	switch s.mode {
	case modeUndecided:
		// EOF before any payload event: tiny stream, non-\n\n framing,
		// or not SSE-shaped at all. Whole-body pass, same as buffered.
		s.noteCRLFFramingIfSuspected(s.pending)
		s.buf = s.pending
		s.pending = nil
		s.finalizeBuffered()
	case modeBuffered:
		s.finalizeBuffered()
	case modePassthrough:
		s.emitBlock(s.pending) // partial tail: stream cut mid-event
		s.pending = nil
		s.appendDone()
	}
}

func (s *respStream) finalizeBuffered() {
	b := s.buf
	s.buf = nil
	raw := b // pre-strip snapshot; only kept (below) if a strip actually fires
	if modelFieldPattern.Match(b) {
		b = modelFieldPattern.ReplaceAll(b, []byte(`${1}`+s.clientModel+`"`))
		s.noteApplied("model_rewrite")
	}
	// Guarded like the streaming path: only a response whose first non-empty
	// content/text value STARTS with <think> is the MiniMax thinking shape;
	// a body that merely quotes the tag mid-text passes through untouched.
	if thinkShapeGuard(b) && thinkPattern.Match(b) {
		s.rawPreStrip = raw
		b = thinkPattern.ReplaceAll(b, nil)
		s.noteApplied("think_strip")
	}
	if stripped := stripThinkingProcess(b); !bytes.Equal(stripped, b) {
		if s.rawPreStrip == nil {
			s.rawPreStrip = raw
		}
		b = stripped
		s.noteApplied("thinking_process_strip")
	}
	if bytes.Contains(b, doneSentinel) {
		s.sawDone = true
	}
	if containsSoftBlockMarker(b) {
		s.noteApplied("soft_block_detected")
	}
	s.tailNL = len(b) == 0 || bytes.HasSuffix(b, eventSep)
	s.out = append(s.out, b...)
	s.appendDone()
}

// appendDone adds the [DONE] sentinel when — and only when — the client
// speaks the OpenAI protocol, the response is SSE, and the upstream
// didn't send its own. Anthropic streams have no [DONE] concept.
func (s *respStream) appendDone() {
	if !s.isSSE || s.protocol != "openai" || s.sawDone {
		return
	}
	if !s.tailNL && len(s.out) > 0 {
		s.out = append(s.out, eventSep...)
	}
	s.out = append(s.out, doneSentinel...)
	s.out = append(s.out, eventSep...)
	s.noteApplied("done_appended")
}

func (s *respStream) noteApplied(step string) {
	for _, a := range s.applied {
		if a == step {
			return
		}
	}
	s.applied = append(s.applied, step)
}

type verdict int

const (
	verdictUndecided verdict = iota
	verdictBuffered
	verdictPassthrough
)

// classifyEvent inspects one complete SSE event and reports whether it
// proves the response needs buffering (MiniMax thinking shapes), proves
// it can stream through untouched, or proves nothing yet. Both thinking
// shapes are detected by the same rule — the first non-empty content/text
// value STARTS with the shape's marker — so a reply that merely mentions
// <think> or "Thinking Process:" mid-text streams through untouched.
func classifyEvent(ev []byte) verdict {
	if v, ok := afterMarker(ev, contentFieldMarker); ok {
		v = trimEscapedWS(v)
		if len(v) > 0 && v[0] != '"' { // non-empty content value
			if bytes.HasPrefix(v, thinkOpenMarker) || bytes.HasPrefix(v, thinkingProcessPrefix) {
				return verdictBuffered
			}
			return verdictPassthrough
		}
	}
	for _, m := range passthroughStringMarkers {
		if v, ok := afterMarker(ev, m); ok {
			if v = trimEscapedWS(v); len(v) > 0 && v[0] != '"' {
				// anthropic text deltas can carry the same inline-think
				// pathology as openai content; the dedicated reasoning
				// fields (reasoning_content, thinking) cannot.
				if bytes.Equal(m, textFieldMarker) && bytes.HasPrefix(v, thinkOpenMarker) {
					return verdictBuffered
				}
				return verdictPassthrough
			}
		}
	}
	for _, m := range passthroughTokenMarkers {
		if bytes.Contains(ev, m) {
			return verdictPassthrough
		}
	}
	return verdictUndecided
}

// afterMarker returns the bytes following the first occurrence of marker.
func afterMarker(ev, marker []byte) ([]byte, bool) {
	i := bytes.Index(ev, marker)
	if i < 0 {
		return nil, false
	}
	return ev[i+len(marker):], true
}

// trimEscapedWS strips leading spaces and JSON-escaped whitespace
// sequences (the two-character forms \n, \t, \r) so prefix checks see
// the first meaningful character of a string value.
func trimEscapedWS(v []byte) []byte {
	for {
		switch {
		case len(v) > 0 && v[0] == ' ':
			v = v[1:]
		case len(v) >= 2 && v[0] == '\\' && (v[1] == 'n' || v[1] == 't' || v[1] == 'r'):
			v = v[2:]
		default:
			return v
		}
	}
}

// stripThinkingProcess removes MiniMax M3's "Thinking Process:"
// structured thinking section from a fully buffered response, preserving
// the SSE framing of every other data: line. Returns b unchanged if the
// pattern is not detected — better to pass through than to drop the
// actual response.
//
// The shape MiniMax emits under thinking=medium is a plain-text thinking
// section starting with "Thinking Process:", numbered subsections, a
// "Final Polish" draft area, and a "Looks good. Pro"/"Looks good.
// Proceed" self-endorsement marker; the final response starts on the
// marker's own line, right after the marker. Thinking and final response
// usually live in different data: lines, so the strip drops the lines
// between the role marker and the marker line, then trims the marker
// line's content up to the end of the endorsement.
func stripThinkingProcess(b []byte) []byte {
	// Trigger guard: only fire when the response's FIRST content value
	// begins with the literal "Thinking Process:". Without this, any
	// legitimate reply containing "Looks good. Pro…" (a code review
	// saying "Looks good. Proceed with the merge") would have the
	// chunks before the marker silently dropped.
	cf := bytes.Index(b, contentFieldMarker)
	if cf < 0 || !bytes.HasPrefix(trimEscapedWS(b[cf+len(contentFieldMarker):]), thinkingProcessPrefix) {
		return b
	}
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
	// cut is the byte offset immediately after "Pro" / "Proceed" — the
	// start of the actual final response text within the marker's line.
	cut := last[0] + endorseLoc[1]

	// Split into data: lines on the SSE separator (JSON-escaped content
	// contains no real "\n\n", so the split is safe).
	lines := bytes.Split(b, eventSep)
	// Locate the LAST line containing the marker — the model may emit
	// "Looks good. Pro" inside the thinking section too.
	markerLineIdx := -1
	for i, line := range lines {
		if thinkingProcessFullMatch.FindIndex(line) != nil {
			markerLineIdx = i
		}
	}
	if markerLineIdx < 0 || markerLineIdx >= len(lines) {
		return b
	}

	// Drop lines between the first line and the marker line (the
	// thinking-only content). The first line (role marker) is kept —
	// unless the marker IS the first line (non-streaming JSON is a
	// single "line"), where keeping it would duplicate the whole body.
	keep := make([][]byte, 0, len(lines)-markerLineIdx+1)
	if markerLineIdx > 0 {
		keep = append(keep, lines[0])
	}
	// Trim the marker line's content to start after "Pro"/"Proceed".
	lineStart := 0
	for i := 0; i < markerLineIdx; i++ {
		lineStart += len(lines[i]) + len(eventSep)
	}
	cutInLine := cut - lineStart
	markerLine := lines[markerLineIdx]
	if cutInLine > 0 && cutInLine < len(markerLine) {
		cfIdx := bytes.Index(markerLine, contentFieldMarker)
		if cfIdx >= 0 {
			contentValueStart := cfIdx + len(contentFieldMarker)
			if contentValueStart <= cutInLine {
				newLine := make([]byte, 0, len(markerLine))
				newLine = append(newLine, markerLine[:contentValueStart]...)
				newLine = append(newLine, markerLine[cutInLine:]...)
				markerLine = newLine
			}
		}
	}
	keep = append(keep, markerLine)
	// Keep all lines after the marker line verbatim — including a
	// trailing empty element from splitting a body that ends in "\n\n",
	// so the final SSE separator survives the re-join.
	keep = append(keep, lines[markerLineIdx+1:]...)
	return bytes.Join(keep, eventSep)
}
