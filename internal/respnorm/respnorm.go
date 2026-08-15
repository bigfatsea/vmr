// Ver 2026-07-17 02:00, by Sonnet 5
//
// Package respnorm is the response normalizer. Guiding principle: what the
// client receives through VMR must match what it would receive calling the
// provider directly, except for the virtual-model abstraction (the "model"
// field is rewritten back to the client's virtual name) and two
// MiniMax-M3-specific repairs that only engage when their exact upstream
// shape is detected:
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
//
// This file holds the generic transport mechanics — event splitting, the
// buffered/passthrough decision, model-field rewrite, [DONE] policy. The
// MiniMax-specific pattern knowledge behind the two thinking-mode repairs
// and the soft-block markers lives in minimax.go; this file calls into it
// (thinkShapeGuard, stripThinkingProcess, containsSoftBlockMarker, and the
// thinkOpenMarker/thinkCloseMarker/thinkPattern constants) at the few points
// where the state machine needs to know "does this look like a MiniMax
// quirk".
//
// This is a separate package from internal/router so the state machine can be
// fuzzed at the pure io.Reader level, independent of Router/Snapshot (see
// respnorm_test.go's FuzzStream). router.go/quota.go call only Wrap and
// NormalizerStream, never a stream field directly.
//
// Usage-sniffing placement is an acknowledged tradeoff, not an oversight:
// Quota-Aware Routing's accumulators (noteUsage/countBytes, exposed as
// Usage()/OutBytes()) live on stream, mixing "response normalization" with
// "billing sniffing" in one package. The alternative — a decorator in
// internal/router layered over Wrap's output — costs an interface call and
// boundary check per streamed chunk on the hot forward path, which this
// project does not spend (see CLAUDE.md's Invariants). Sniffing piggybacks on
// ingest's existing per-chunk loop at zero added cost.
package respnorm

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"sync"

	"vmr/internal/chatmsg"
)

// Options configures Wrap. ClientModel is the virtual model name the
// client asked for (rewritten into every "model" field in the response);
// UpstreamModel is the real model name vmr requested from this endpoint,
// used only to detect when the upstream answered with something else (see
// NormalizerStream.ObservedModel). IsSSE and Protocol decide framing/[DONE]
// policy; Opaque (Content-Encoding present) disables every transform.
type Options struct {
	ClientModel   string
	UpstreamModel string
	IsSSE         bool
	Protocol      string // ingress protocol: decides [DONE] policy
	Opaque        bool   // Content-Encoding present: no transforms at all
}

// NormalizerStream is what Wrap returns: an io.Reader over the normalized
// response body, plus the audit-trail/quota-metering facts accumulated
// while it was read. Applied/RawPreStrip/ObservedModel are valid only after
// the stream has been fully copied (EOF reached); Usage/OutBytes update
// incrementally and are safe to read concurrently with an in-flight Read
// (see the qmu field on stream for why only those two need that guarantee).
type NormalizerStream interface {
	io.Reader

	// Applied reports which transforms ran, for the audit trail.
	Applied() []string
	// RawPreStrip returns the upstream bytes exactly as received, from the
	// point just before a think_strip/thinking_process_strip rewrite ran —
	// nil when neither fired.
	RawPreStrip() []byte
	// ObservedModel is the upstream's own model value when it differed from
	// the one vmr requested, else "".
	ObservedModel() string
	// Usage returns the usage sniffed from this response so far; ok is true
	// only once at least one usage-bearing block has actually been parsed.
	Usage() (chatmsg.Usage, bool)
	// OutBytes returns the ASCII/wide byte counts classified so far — the
	// degraded-estimate input when Usage() has nothing.
	OutBytes() (ascii, wide int64)
}

// Wrap normalizes src (an upstream response body) per opts. See the package
// doc comment for the transport-mode decision and the usage-sniffing
// placement tradeoff.
func Wrap(src io.Reader, opts Options) NormalizerStream {
	return newStream(src, opts.ClientModel, opts.UpstreamModel, opts.IsSSE, opts.Protocol, opts.Opaque)
}

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
// degrades to opaque passthrough — direct-connection behavior. Sized off
// today's ~1M-token context windows (~3-4MB of bytes) with about 2x
// headroom, matching config.DefaultMaxRequestBodyMB's reasoning rather than
// an arbitrary round number — legitimate responses have no reason to need
// more than a request does, and a lower cap also means a genuinely runaway
// upstream degrades to passthrough sooner instead of buffering longer.
const bufferedCap = 8 << 20

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
var modelFieldPattern = regexp.MustCompile(`("model":\s*")([^"]*)"`)

// Group 2 is the upstream's own model value, read once per response before
// the rewrite overwrites it — see stream.noteUpstreamModel. Adding the
// group is free for the rewrite itself: ReplaceAll's `${1}` still names the
// same opener it always did.

var (
	contentFieldMarker = []byte(`"content":"`)
	textFieldMarker    = []byte(`"text":"`)
	doneSentinel       = []byte("data: [DONE]")
	eventSep           = []byte("\n\n")
	crlfEventSepHint   = []byte("\r\n\r\n")
	// usageFieldMarker is the cheap gate for Quota-Aware Routing's usage
	// sniffing (see noteUsage below): almost every SSE token-delta event
	// carries no "usage" key at all, so this bytes.Contains check skips a
	// full JSON parse for the overwhelming majority of events — the same
	// "cheap substring gate before an expensive parse" idiom modelFieldPattern
	// and the other markers in this file already use.
	usageFieldMarker = []byte(`"usage"`)
)

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

type stream struct {
	src         io.Reader
	clientModel string
	// modelRewriteRepl is the regexp.ReplaceAll template used to rewrite the
	// "model" field back to clientModel — precomputed once so a virtual
	// model name containing "$" (a legal YAML `models:` key) can't be
	// misread as a submatch reference (see newStream).
	modelRewriteRepl []byte
	isSSE            bool
	protocol         string // ingress protocol: decides [DONE] policy
	opaque           bool   // Content-Encoding present: no transforms at all

	mode    int
	pending []byte // undecided: withheld bytes; passthrough: partial-event tail
	scanned int    // undecided only: pending[:scanned] holds events already classified undecided — decide resumes after them instead of rescanning (keeps a ping-heavy stream linear, not quadratic)
	buf     []byte // buffered-mode accumulation
	out     []byte // processed bytes ready for the client
	srcErr  error  // non-EOF src error, surfaced after out drains
	done    bool   // src hit EOF and out holds the final bytes
	sawDone bool   // upstream emitted its own data: [DONE]
	tailNL  bool   // last emitted bytes ended with the SSE separator
	// emittedTail holds the last len(eventSep) bytes actually emitted by
	// emitBlock, across ALL calls — NOT just the most recent block. Needed
	// because s.out is drained (returned to the caller and sliced away) by
	// Read between emitBlock calls, so a block-local check can't see a
	// separator a fragmented delivery happened to split across two emits
	// (see emitBlock's own comment on tailNL for the concrete case this
	// fixed).
	emittedTail       []byte
	upstreamModel     string // the real model name vmr ASKED this endpoint for (ep.Model)
	observedModel     string // what the upstream actually answered with, recorded only when it differs from upstreamModel
	modelSeen         bool   // the observed value is captured once per response, not once per SSE chunk
	thinkTriggered    bool   // buffering was caused by an inline <think> block
	thinkPatternBytes int    // observation only: cumulative content bytes scanned for the leaked-thinking-process shape
	thinkPatternHits  int    // cumulative "\n<N>." numbered-marker hits found in those bytes
	applied           []string
	rawPreStrip       []byte // upstream bytes exactly as received, captured right before think_strip/thinking_process_strip rewrote them — nil unless one of those fired
	scratch           []byte // reused read buffer; lazily allocated once per response (a stack array here would be re-zeroed on every Read call)

	// qmu guards ONLY the four fields below — Quota-Aware Routing's usage/
	// byte-count accumulators — not the rest of stream's fields. Those
	// stay unsynchronized on purpose (Read is only ever called serially by
	// transport.go's copyFlush reader goroutine), but Usage()/OutBytes()
	// are read from forwardSuccess's own goroutine, AFTER copyFlush
	// returns — and on two of copyFlush's return paths (idle timeout,
	// write error) the reader goroutine is not guaranteed to have exited
	// yet (see transport.go's copyFlush doc comment and
	// docs/KNOWN_ISSUES_sonnet-5.md's entry on copyFlush returning before
	// its reader goroutine has stopped touching the body). Rather
	// than fixing that pre-existing race (a hot-path change out of scope
	// for this feature), these four fields get their own lock so the NEW code
	// this feature adds is race-clean without touching the old fields at
	// all. Worst case for THESE four, precisely because of this lock: this
	// response's very last chunk of usage/bytes is missed — a benign
	// undercount, not undefined behavior. That bound does NOT extend to the
	// unsynchronized fields the same race also touches (applied,
	// rawPreStrip, observedModel, read via Applied()/RawPreStrip()/
	// ObservedModel() on the same post-copyFlush path): those are a genuine
	// data race on slice/string headers. See the KNOWN_ISSUES entry.
	qmu        sync.Mutex
	asciiBytes int64
	wideBytes  int64
	usage      chatmsg.Usage
	usageSeen  bool
}

func newStream(src io.Reader, clientModel, upstreamModel string, isSSE bool, protocol string, opaque bool) *stream {
	// "$" is a template metacharacter to regexp.ReplaceAll (see
	// modelRewriteRepl's use in emitBlock/finalizeBuffered) — escape it so a
	// virtual model name like "gpt$4" is emitted verbatim instead of being
	// read as a (nonexistent) submatch reference and silently truncated.
	escapedClientModel := strings.ReplaceAll(clientModel, "$", "$$")
	rs := &stream{
		src: src, clientModel: clientModel,
		modelRewriteRepl: []byte(`${1}` + escapedClientModel + `"`),
		upstreamModel:    upstreamModel, isSSE: isSSE, protocol: protocol, opaque: opaque, tailNL: true,
	}
	switch {
	case opaque:
		rs.applied = append(rs.applied, "opaque")
	case !isSSE:
		rs.mode = modeBuffered
	case protocol == "openai-responses":
		// The modeUndecided/classifyEvent machinery below exists purely to
		// detect MiniMax's two thinking-mode shapes, both defined by
		// inline text inside "content"/"text" fields. The Responses
		// protocol has no such shape to detect: reasoning is always a
		// separate typed Item (response.output[].type == "reasoning"),
		// never mixed into text content — and its SSE events don't carry
		// the "content"/"text" field markers classifyEvent looks for in
		// the first place, so every response on this protocol would
		// otherwise sit in modeUndecided until EOF and silently degrade
		// from true streaming to full buffering. Going straight to
		// modePassthrough is the same "no known quirk shape, so don't
		// wait for one" decision !isSSE already makes for non-SSE bodies —
		// not a guess at Responses' event field names.
		rs.mode = modePassthrough
	}
	return rs
}

// Applied reports which transforms ran, for the audit trail. Valid after
// the stream has been fully copied.
func (s *stream) Applied() []string { return s.applied }

// RawPreStrip returns the upstream bytes exactly as received, from the point
// just before a think_strip/thinking_process_strip rewrite ran — nil when
// neither fired. It is the buffered segment only (whatever was accumulated
// at that moment), not a second copy of the whole response: the buffered
// mode already holds these bytes in memory for the regex pass, so this just
// keeps that reference alive instead of discarding it. Valid after the
// stream has been fully copied.
func (s *stream) RawPreStrip() []byte { return s.rawPreStrip }

// Read does not satisfy the general io.Reader contract: it can return
// (0, nil) — see the comment above the return statement below — which
// io.Reader's own doc comment explicitly discourages ("Implementations of
// Read are discouraged from returning a zero byte count with a nil error").
// This type is meant for exactly one consumer, transport.go's copyFlush,
// which already treats a zero-byte/nil-error read as "nothing to flush yet,
// call again" rather than a suspicious/terminal condition. It happens to
// stay safe even for a generic io.Copy-style caller too, since the (0,nil)
// only ever follows a real blocking read on s.src that already consumed
// some upstream bytes — but that's incidental to this type's contract, not
// a promise: don't rely on it, route new consumers through copyFlush.
func (s *stream) Read(p []byte) (int, error) {
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
	if s.done {
		return 0, io.EOF
	}
	// Nothing deliverable yet (mid-event, or withheld pending a mode
	// decision). Zero-length reads let the caller's idle watchdog tick.
	return 0, nil
}

func (s *stream) ingest(b []byte) {
	// Every source byte flows through here exactly once, in every mode
	// including opaque (see the P1 dev plan's baseline-facts table) — the
	// one hook that
	// can classify 100% of a response's bytes for the degraded token
	// estimate (see OutBytes/tokenCharge in quota.go), unlike emitBlock/
	// finalizeBuffered below which only ever see the non-opaque paths.
	s.countBytes(b)
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
func (s *stream) noteCRLFFramingIfSuspected(b []byte) {
	if bytes.Contains(b, crlfEventSepHint) {
		s.noteApplied("crlf_framing_suspected")
	}
}

// decide scans the complete events withheld so far and settles the mode
// on the first payload-bearing one. Role markers, pings, message_start
// and empty-content chunks don't decide; they stay withheld (a few tiny
// events at most) and are released or buffered with everything else.
func (s *stream) decide() {
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
func (s *stream) emitComplete() {
	i := bytes.LastIndex(s.pending, eventSep)
	if i < 0 {
		return
	}
	block := s.pending[:i+len(eventSep)]
	tail := append([]byte(nil), s.pending[i+len(eventSep):]...)
	s.emitBlock(block)
	s.pending = tail
}

func (s *stream) emitBlock(block []byte) {
	if len(block) == 0 {
		return
	}
	if !s.sawDone && bytes.Contains(block, doneSentinel) {
		s.sawDone = true
	}
	if containsSoftBlockMarker(block) {
		s.noteApplied("soft_block_detected")
	}
	s.noteThinkingPatternIfSuspected(block)
	s.noteUsage(block)
	if modelFieldPattern.Match(block) {
		s.noteUpstreamModel(block)
		block = modelFieldPattern.ReplaceAll(block, s.modelRewriteRepl)
		s.noteApplied("model_rewrite")
	}
	s.out = append(s.out, block...)
	// tailNL must reflect the trailing bytes of everything emitted SO FAR,
	// not just this block — s.out is drained (returned to the caller and
	// sliced away) by Read between emitBlock calls, so a block-local check
	// misses a separator a fragmented delivery happened to split across two
	// emits: one block ends "...\n\n" (tailNL correctly true), the next is
	// a lone trailing "\n" too short to contain eventSep on its own
	// (block-local check wrongly concludes false) — appendDone then splices
	// in a spurious extra blank line before [DONE] that whole-shot delivery
	// of the identical bytes never would have. Found by FuzzStream's
	// fragmentation-invariance check (see respnorm_test.go's FuzzStream
	// follow-up): whole-shot delivery happened to always emit such a split
	// as one single block, which is why this went unnoticed until chunked
	// ingestion was fuzzed. emittedTail (bounded to len(eventSep) bytes)
	// carries the needed trailing context across the drain.
	combined := append(s.emittedTail, block...)
	s.tailNL = bytes.HasSuffix(combined, eventSep)
	if len(combined) > len(eventSep) {
		combined = combined[len(combined)-len(eventSep):]
	}
	s.emittedTail = append([]byte(nil), combined...)
}

// noteUpstreamModel records what the upstream actually said its model was,
// captured from the block about to have that value overwritten with the
// virtual name. This is the only moment the information exists: the audit
// trail deliberately does not store a successful attempt's response body
// (it is byte-identical to the client's, minus the steps in Norm), and the
// client's copy has already been rewritten — so a value not read here is
// gone for good.
//
// Deliberately records the RAW value and no verdict. "Asked for X, got Y"
// is not evidence of anything by itself: version pinning (gpt-4o ->
// gpt-4o-2024-08-06), vendor prefixes (z-ai/glm-5.2 -> glm-5.2) and plan
// aliases (ark-code-latest -> doubao-seed-code-251015) all produce a
// legitimate mismatch on every single request. What separates an alias from
// a silent downgrade is only visible in aggregate — a *consistent* mapping
// is an alias, an inconsistent one is worth looking at — and that judgment
// belongs in vmr report, offline, over many requests, not in a per-request
// heuristic on the streaming path.
//
// Costs one extra regex scan per response, not per SSE chunk: the model
// field repeats in every chunk and modelSeen latches after the first.
// Identical values record nothing at all, so the common case adds no bytes
// to the audit record.
func (s *stream) noteUpstreamModel(block []byte) {
	if s.modelSeen || s.upstreamModel == "" {
		return
	}
	m := modelFieldPattern.FindSubmatch(block)
	if len(m) < 3 {
		return
	}
	s.modelSeen = true
	if got := string(m[2]); got != s.upstreamModel {
		s.observedModel = got
	}
}

// ObservedModel is the upstream's own model value when it differed from the
// one vmr requested, else "" — see noteUpstreamModel for why there is no
// verdict attached.
func (s *stream) ObservedModel() string { return s.observedModel }

func (s *stream) finish() {
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
	s.notePatternDetectedIfSuspected()
}

func (s *stream) finalizeBuffered() {
	b := s.buf
	s.buf = nil
	raw := b // pre-strip snapshot; only kept (below) if a strip actually fires
	if modelFieldPattern.Match(b) {
		s.noteUpstreamModel(b)
		b = modelFieldPattern.ReplaceAll(b, s.modelRewriteRepl)
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
	s.noteThinkingPatternIfSuspected(b)
	s.noteUsage(b)
	s.tailNL = len(b) == 0 || bytes.HasSuffix(b, eventSep)
	s.out = append(s.out, b...)
	s.appendDone()
}

// appendDone adds the [DONE] sentinel when — and only when — the client
// speaks the OpenAI protocol, the response is SSE, and the upstream
// didn't send its own. Anthropic streams have no [DONE] concept.
//
// Options.Protocol is the INGRESS adapter's registered type string, not a
// family name: the three values that reach here are "openai", "anthropic",
// and "openai-responses" (newStream switches on that last one too). The
// comparison is deliberately exact rather than a prefix or a family
// predicate — "openai-responses" starts with "openai" and must NOT get a
// [DONE]. A future OpenAI-derived ingress (an azure-openai adapter, say)
// therefore needs its own decision here rather than inheriting one: it is a
// new registered protocol string, so it falls through to "no [DONE]" until
// someone states otherwise, which is the safe default (adding a sentinel a
// client doesn't expect corrupts a stream; omitting one only reaches a
// client that already tolerates upstreams which never send it). Kept as a
// literal comparison in the one place the decision is made rather than an
// isProtocolRequiringDone helper — with three protocols and no translation
// between them (CLAUDE.md's headline invariant), a predicate function would
// add a layer without adding a decision.
func (s *stream) appendDone() {
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

func (s *stream) noteApplied(step string) {
	for _, a := range s.applied {
		if a == step {
			return
		}
	}
	s.applied = append(s.applied, step)
}

// noteThinkingPatternIfSuspected accumulates the leaked-thinking-process
// observation signal for one block/whole-body b: total bytes scanned and how
// many numbered-subsection markers ("\n1.", "\n2.", …) they contain. Called
// on every buffered and passthrough emission regardless of whether an actual
// strip already fired for this response — cheap two-int bookkeeping, no
// extra buffering — because the strip may fire later in the same response
// (buffered mode) or not fire at all despite this shape appearing (exactly
// the failure this exists to catch); the pass/fail decision is deferred to
// finish()'s notePatternDetectedIfSuspected once every byte has been seen.
func (s *stream) noteThinkingPatternIfSuspected(b []byte) {
	s.thinkPatternBytes += len(b)
	s.thinkPatternHits += len(thinkingProcessNumberedMarker.FindAll(b, -1))
}

// stripFired reports whether either MiniMax thinking-mode repair actually
// rewrote this response's bytes.
func (s *stream) stripFired() bool {
	for _, a := range s.applied {
		if a == "think_strip" || a == "thinking_process_strip" {
			return true
		}
	}
	return false
}

// notePatternDetectedIfSuspected makes the final call at EOF: this response
// looked like MiniMax's leaked thinking-mode outline (enough numbered-marker
// hits over enough content bytes) but neither actual strip fired for it —
// stripThinkingProcess's literal "Thinking Process:" prefix guard (which
// also decides passthrough vs. buffered mode up in decide()) may have gone
// blind to a wording change. Tags the audit trail only; never touches a byte
// of the response and never affects failover/health.
func (s *stream) notePatternDetectedIfSuspected() {
	if s.stripFired() {
		return
	}
	if s.thinkPatternBytes > 1024 && s.thinkPatternHits >= 3 {
		s.noteApplied("thinking_process_pattern_detected")
	}
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
//
// Why only <think> is checked in BOTH "content" (openai) and "text"
// (anthropic), while "Thinking Process:" is checked in "content" alone: the
// <think> repair (thinkPattern) is a field-agnostic regexp that works on
// either event shape, but stripThinkingProcess is structurally openai-shaped —
// its own trigger guard and its surviving-content splice both key off
// contentFieldMarker. Buffering an anthropic response on a "text" value that
// opened with "Thinking Process:" would cost the whole stream's latency and
// then strip nothing. Widening this needs the repair rewritten for the
// anthropic event shape first, and evidence that MiniMax thinking mode is
// actually reachable over /v1/messages — neither exists today.
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

// countBytes classifies every byte of b by UTF-8 lead byte (ASCII vs. wide),
// the same split core.EstimateTextTokens uses — feeding Quota-Aware
// Routing's degraded token estimate (see quota.go's tokenCharge) without
// buffering a whole response body just to hand it to that function: this
// tallies incrementally, once per Read, and the result is byte-for-byte
// equivalent to running EstimateTextTokens over the full concatenated body
// (see core.EstimateTokensFromCounts's doc comment).
func (s *stream) countBytes(b []byte) {
	var ascii, wide int64
	for _, c := range b {
		if c < 0x80 {
			ascii++
		} else {
			wide++
		}
	}
	s.qmu.Lock()
	s.asciiBytes += ascii
	s.wideBytes += wide
	s.qmu.Unlock()
}

// noteUsage looks for a "usage" object in b and folds it into the running
// total. Called from both emitBlock (streaming) and finalizeBuffered
// (buffered) — the two paths never overlap for one response (see
// response.go's package doc comment on transport modes), so this never
// double-counts. The bytes.Contains gate means the overwhelming majority of
// streamed events (plain content/tool-call deltas) cost one substring scan
// and nothing else; only an event that actually mentions "usage" pays for a
// JSON parse.
func (s *stream) noteUsage(b []byte) {
	if !bytes.Contains(b, usageFieldMarker) {
		return
	}
	s.qmu.Lock()
	defer s.qmu.Unlock()
	u := chatmsg.MergeUsageBytes(b, s.usage)
	s.usage = u
	if u.In > 0 || u.Out > 0 {
		s.usageSeen = true
	}
}

// Usage returns the usage sniffed from this response so far; ok is true
// only once at least one usage-bearing block has actually been parsed —
// see quota.go's tokenCharge for the fallback when ok is false. Safe to
// call concurrently with an in-flight Read/ingest — see qmu's doc comment
// on stream for why this specific pair of fields needs that guarantee
// when the rest of the type doesn't.
func (s *stream) Usage() (chatmsg.Usage, bool) {
	s.qmu.Lock()
	defer s.qmu.Unlock()
	return s.usage, s.usageSeen
}

// OutBytes returns the ASCII/wide byte counts countBytes has classified so
// far — the degraded-estimate input when Usage() has nothing. Same
// concurrency contract as Usage().
func (s *stream) OutBytes() (ascii, wide int64) {
	s.qmu.Lock()
	defer s.qmu.Unlock()
	return s.asciiBytes, s.wideBytes
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
