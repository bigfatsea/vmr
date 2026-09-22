// Ver 2026-09-17, by Sonnet 5

// Inbound: the online read-chain mount point (the Agent Guard spec
// ADR-15, M4). Sits downstream of respnorm in the read chain (resp.Body ->
// respnorm.Wrap -> guard.Inbound -> copyFlush) and independently of
// respnorm's own mode selection: whether respnorm ends up streaming,
// buffering, or degrading to raw opaque passthrough (overflow), the bytes
// guard.Inbound receives are still the same text stream it can inspect.
// The one case guard.Inbound genuinely cannot inspect is a truly
// compressed body (Content-Encoding present and not already transparently
// decompressed by Go's own Transport) -- that is exactly what
// InboundOpts.Opaque means, decided from response headers before a single
// body byte is read.
//
// ADR-15 (2026-09-16) removed the online Tool Call double gate, the
// three-protocol circuit-breaker frames, and the non-streaming one-shot
// tool-call scan that used to live alongside this file: a client's own
// approval gate and sandbox are the correct place to judge whether a
// command should run, not a gateway with strictly less context, and two
// independent reviews found nine of their ~14 findings concentrated in
// that machinery. What remains is exactly one online intervention --
// Unicode-steganography sanitization -- and it never blocks, never
// synthesizes a protocol frame, never changes the HTTP status code:
// Read is either a pure byte passthrough (sanitization off, or the
// response is genuinely opaque) or a re-framed, per-event rewrite that
// always forwards every byte it sees, sanitized or not.
package guard

import (
	"bytes"
	"io"
	"sync"
)

// inboundSanitizeMaxHoldBytes bounds how many not-yet-framed bytes Read
// will hold while waiting for the next SSE blank-line delimiter. A
// pathological or malicious upstream that never sends one could otherwise
// make this hold grow without bound -- a memory blowup AND a response
// that never gets flushed to the client, exactly the kind of "unnecessary
// factor breaking the pipe" the inbound side exists to avoid (ADR-15).
// Past this cap, fill gives up on re-framing and relays every remaining
// byte raw, unsanitized, for the rest of this response -- not a config
// knob, a robustness floor "sanitization must never become an
// obstruction" itself requires.
//
// Deliberately much smaller than nonStreamGuardCap (router/guard.go,
// 8 MiB): that constant bounds a whole non-streaming response body, while
// this one bounds a SINGLE re-framing wait between two SSE delimiters -- a
// real upstream's individual event is at most a few KiB. Sizing this one
// off the whole-body constant would mean a malformed or stalled upstream
// (no blank-line delimiter ever arriving) holds the client's stream dark
// for up to 8 MiB of buffering before giving up and flushing anything --
// TTFT effectively infinite on exactly the kind of broken stream this
// cap exists to bound. 256 KiB gives an ordinary event roughly two orders
// of magnitude of headroom while keeping the worst-case hold small.
//
// That cap still leaves one gap looksLikeNonSSE (below) closes: router.go's
// isSSE guess is header-only (text/event-stream, or an empty Content-Type
// alongside a client stream:true request -- some upstreams omit the header
// on real SSE, hence the fallback). When that guess is wrong -- a broken
// relay's plain JSON error body, empty Content-Type, on a request the
// client asked to stream -- this stage would otherwise hold every byte
// until EOF or this cap: an all-or-nothing wait instead of the streaming
// response the client expected. Deliberately not "wait out the cap first,
// then check the shape": the point is catching this on the earliest bytes,
// before most non-SSE bodies have even finished arriving.
const inboundSanitizeMaxHoldBytes = 256 << 10

// InboundOpts configures one response's Inbound call (ADR-15: sanitization
// only).
type InboundOpts struct {
	// Opaque is true when the upstream response carries a non-empty
	// Content-Encoding header that Go's Transport did not already
	// transparently decompress -- guard cannot parse SSE structure inside
	// genuinely compressed bytes, so Inbound skips sanitization entirely
	// and returns a pure passthrough reader. There is nothing to strip and
	// (post-ADR-15) nothing to gate either way.
	Opaque bool
	// SanitizeInvisibleRunes mirrors config.GuardInbound.SanitizeRunes()
	// -- the caller resolves the *bool-vs-default-true indirection before
	// this point. false (or Opaque true) keeps Read a pure byte
	// passthrough -- no re-framing, no allocation.
	SanitizeInvisibleRunes bool
}

// InboundStream wraps the upstream response body for the read chain. Read
// is always a full relay of every byte the upstream sent -- sanitized
// where sanitization changed something, byte-identical where it didn't or
// wasn't attempted -- never a partial or truncated stream (ADR-15: no
// blocking path exists here at all).
type InboundStream interface {
	io.Reader
	// Applied lists Attempt.Norm markers this stream's sanitization
	// produced (e.g. "guard_runes_sanitized") -- merged into Attempt.Norm
	// by the caller alongside respnorm.Applied().
	Applied() []string
	// RuneCounts returns the count of sanitized invisible runes by
	// category, for Record.Guard.SanitizedRunes.
	RuneCounts() map[string]int
}

type inboundStream struct {
	src io.Reader

	// mu guards runeCounts and applied below: Read (and everything it
	// calls -- fill, sanitizeEvent, sanitizeEventSafe) runs on copyFlush's
	// own reader goroutine (transport.go), but RuneCounts/Applied are read
	// from relayResponseBody's goroutine after copyFlush returns. On early
	// return (idle timeout, client disconnect, write error), the reader
	// goroutine can still be mid-Read -- exactly the race
	// respnorm.stream's own mu (same read chain, same copyFlush) exists to
	// close; inboundStream sits downstream of it in that same chain and
	// needs the identical guard.
	mu          sync.Mutex
	runeCounts  map[string]int
	runesMarked bool // true once the one-time "guard_runes_sanitized" marker has been appended

	// reframe is true when sanitize is on -- re-framing the stream into
	// whole SSE events instead of the zero-buffering byte passthrough (see
	// Read's doc comment).
	reframe bool

	inbuf  []byte // raw bytes read from src, not yet split into a complete SSE event
	outbuf []byte // processed bytes ready to hand to the caller's Read
	rbuf   []byte // scratch read buffer, reused across src.Read calls
	srcEOF bool

	// pendingErr holds a non-EOF src.Read error until outbuf has been
	// drained: fill flushes whatever inbuf still held (sanitized, same as
	// the EOF trailing-flush below) into outbuf immediately so those bytes
	// still reach the client, then stashes the error here instead of
	// returning it on the same call -- returning it immediately would
	// discard the just-flushed outbuf (Read's loop only checks err once
	// outbuf comes back empty).
	pendingErr error

	// giveUp is set once inbuf grows past inboundSanitizeMaxHoldBytes
	// without finding a delimiter -- see that constant's doc comment.
	// Once set, fill stops looking for delimiters and relays bytes as they
	// arrive, unsanitized, for the rest of this response.
	giveUp       bool
	giveUpMarked bool

	// shapeChecked is set after the first non-empty src.Read, gating the
	// one-time looksLikeNonSSE check in fill -- see that function's own
	// comment for why this needs to run only once, on the earliest bytes.
	shapeChecked bool

	// errMarked is true once the one-time "guard_inbound_error" marker has been
	// appended for a recovered sanitizeEventSafe panic -- see its own doc
	// comment. Independent of giveUpMarked: a panic mid-sanitization
	// doesn't imply the hold cap was ever exceeded, and vice versa.
	errMarked bool

	applied []string // guarded by mu above, same as runeCounts
}

// Inbound wraps src per opts. Opaque bytes and sanitize-off both take the
// zero-cost pure-passthrough path -- there is no partial-inspection
// option, and (post-ADR-15) no reason to want one: sanitization either
// runs on readable bytes or it doesn't run at all.
func (g *Guard) Inbound(src io.Reader, opts InboundOpts) InboundStream {
	s := &inboundStream{src: src}
	if opts.Opaque || !opts.SanitizeInvisibleRunes {
		return s
	}
	s.reframe = true
	s.rbuf = make([]byte, 32<<10)
	return s
}

func (s *inboundStream) Read(p []byte) (int, error) {
	if !s.reframe {
		return s.src.Read(p) // pure passthrough -- see package doc comment
	}
	for len(s.outbuf) == 0 {
		err := s.fill()
		if len(s.outbuf) > 0 {
			break
		}
		if err != nil {
			return 0, err
		}
	}
	n := copy(p, s.outbuf)
	s.outbuf = s.outbuf[n:]
	return n, nil
}

// fill ensures forward progress on one Read call's behalf: it either
// extracts and processes exactly one already-buffered complete SSE event
// (appending its sanitized bytes to outbuf), or reads more bytes from src
// into inbuf when no complete event is available yet. A non-nil error --
// clean EOF or a src.Read failure -- never comes back on the same call
// that still has trailing inbuf bytes to deliver: those are flushed
// (sanitizeEventSafe'd, same as any other event) into outbuf first, and a
// non-EOF error is held in pendingErr until the next fill() call once
// outbuf is drained, so Read never reports a short read that silently
// drops bytes the caller already had buffered.
func (s *inboundStream) fill() error {
	if s.pendingErr != nil {
		err := s.pendingErr
		s.pendingErr = nil
		return err
	}
	if s.giveUp {
		if len(s.inbuf) > 0 {
			s.outbuf = append(s.outbuf, s.inbuf...)
			s.inbuf = nil
			return nil
		}
		if s.srcEOF {
			return io.EOF
		}
		n, err := s.src.Read(s.rbuf)
		if n > 0 {
			s.outbuf = append(s.outbuf, s.rbuf[:n]...)
		}
		if err != nil {
			if err != io.EOF {
				// Hold the error until outbuf (just appended above) is
				// drained, mirroring the pendingErr handling below -- a bare
				// `return err` here would be silently dropped by Read's
				// "outbuf non-empty wins" check, then re-issued against a
				// src that already errored once.
				s.pendingErr = err
				return nil
			}
			s.srcEOF = true
		}
		return nil
	}
	if i, sepLen := findSSEDelimiter(s.inbuf); i >= 0 {
		event := s.inbuf[:i+sepLen]
		s.inbuf = s.inbuf[i+sepLen:]
		s.outbuf = append(s.outbuf, s.sanitizeEventSafe(event)...)
		return nil
	}
	if s.srcEOF {
		if len(s.inbuf) > 0 {
			trailing := s.inbuf
			s.inbuf = nil
			s.outbuf = append(s.outbuf, s.sanitizeEventSafe(trailing)...)
		}
		return io.EOF
	}
	n, err := s.src.Read(s.rbuf)
	if n > 0 {
		s.inbuf = append(s.inbuf, s.rbuf[:n]...)
		if !s.shapeChecked {
			s.shapeChecked = true
			if looksLikeNonSSE(s.inbuf) {
				s.giveUp = true
				if !s.giveUpMarked {
					s.mu.Lock()
					s.applied = append(s.applied, "guard_sanitize_non_sse")
					s.mu.Unlock()
					s.giveUpMarked = true
				}
			}
		}
		if !s.giveUp && len(s.inbuf) > inboundSanitizeMaxHoldBytes {
			s.giveUp = true
			if !s.giveUpMarked {
				s.mu.Lock()
				s.applied = append(s.applied, "guard_sanitize_hold_exceeded")
				s.mu.Unlock()
				s.giveUpMarked = true
			}
		}
	}
	if err != nil {
		if err != io.EOF {
			if len(s.inbuf) > 0 {
				trailing := s.inbuf
				s.inbuf = nil
				if s.giveUp {
					// The append just above pushed inbuf past the hold cap in
					// this same call, which already gave up on re-framing --
					// running sanitizeEventSafe on a blob that was just
					// judged too large to safely process would contradict
					// giveUp's own reason to exist (bounding per-event
					// sanitization work); relay it raw instead, exactly like
					// the giveUp branch at the top of this function does.
					s.outbuf = append(s.outbuf, trailing...)
				} else {
					s.outbuf = append(s.outbuf, s.sanitizeEventSafe(trailing)...)
				}
			}
			s.pendingErr = err
			return nil
		}
		s.srcEOF = true
	}
	return nil
}

func (s *inboundStream) RuneCounts() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.runeCounts) == 0 {
		return nil
	}
	out := make(map[string]int, len(s.runeCounts))
	for k, v := range s.runeCounts {
		out[k] = v
	}
	return out
}

// findSSEDelimiter locates the earliest SSE delimiter: CRLFCRLF, LFLF, or CRCR (R-2).
func findSSEDelimiter(buf []byte) (int, int) {
	idxNN := bytes.Index(buf, []byte("\n\n"))
	idxCRLF := bytes.Index(buf, []byte("\r\n\r\n"))
	idxRR := bytes.Index(buf, []byte("\r\r"))

	bestIdx := -1
	bestLen := 0

	update := func(idx, l int) {
		if idx >= 0 && (bestIdx < 0 || idx < bestIdx) {
			bestIdx = idx
			bestLen = l
		}
	}

	update(idxNN, 2)
	update(idxCRLF, 4)
	update(idxRR, 2)

	return bestIdx, bestLen
}

// looksLikeNonSSE reports whether buf's first significant byte rules out
// SSE framing outright. Every real SSE line starts with a field name
// (data:/event:/id:/retry:) or a bare ':' comment; a raw '{' or '[' can
// only be a JSON document handed to fill() under a wrong isSSE guess
// (inboundSanitizeMaxHoldBytes's own doc comment). The check is
// conservative by construction -- only positive JSON evidence trips it,
// never the mere absence of SSE evidence -- so a genuine SSE stream, whose
// first byte is never '{' or '[', can never be misjudged by it. Leading
// ASCII whitespace is skipped rather than treated as a verdict either way:
// SSE tolerates a blank line before the first event, and a pretty-printed
// JSON body may lead with whitespace too.
func looksLikeNonSSE(buf []byte) bool {
	i := 0
	for i < len(buf) {
		switch buf[i] {
		case ' ', '\t', '\r', '\n':
			i++
			continue
		}
		break
	}
	if i >= len(buf) {
		return false
	}
	return buf[i] == '{' || buf[i] == '['
}

// sanitizeEventSafe wraps sanitizeEvent with a panic recovery that
// Fail-Opens to the event's original, unsanitized bytes (spec §4.9: an
// in-process guard bug must never become a client-visible failure). This
// matters specifically because fill() runs on copyFlush's own reader
// goroutine (transport.go): that goroutine's own recover() converts ANY
// panic -- including one from deep inside SanitizeEventJSON -- into a
// generic "upstream stream panic" error, which the caller then classifies
// as TRUNCATED and reports as a health failure against the endpoint
// (reportStreamOutcome) -- misattributing a bug in this package's own code
// to the upstream provider. Recovering here, one event at a time, keeps
// that failure at its true source: the client still gets this event's
// bytes (unsanitized, since sanitization is what broke), the stream
// continues normally, and guard_inbound_error is recorded once.
func (s *inboundStream) sanitizeEventSafe(event []byte) (out []byte) {
	defer func() {
		if recover() != nil {
			out = event
			if !s.errMarked {
				s.mu.Lock()
				s.applied = append(s.applied, "guard_inbound_error")
				s.mu.Unlock()
				s.errMarked = true
			}
		}
	}()
	return s.sanitizeEvent(event)
}

// sanitizeEvent runs SanitizeEventJSON over event's data payload and
// rebuilds the physical SSE event bytes only when something actually
// changed -- the common (unmodified) case returns event unchanged with no
// new allocation beyond SanitizeEventJSON's own fast path.
func (s *inboundStream) sanitizeEvent(event []byte) []byte {
	eventName, id, hasID, retry, data := splitSSEEvent(event)
	if len(data) == 0 || string(data) == "[DONE]" {
		return event
	}
	// Locked for the whole call, not just the reassignment below:
	// SanitizeEventJSON mutates the SAME underlying map in place
	// (dst[cat]++ on every call after the first allocates it) rather than
	// building a fresh one each time, so RuneCounts' concurrent range over
	// s.runeCounts races with this map write, not just with the s.runeCounts
	// field assignment.
	s.mu.Lock()
	out, changed, dst := SanitizeEventJSON(data, s.runeCounts)
	s.runeCounts = dst
	s.mu.Unlock()
	if !changed {
		return event
	}
	if !s.runesMarked {
		s.mu.Lock()
		s.applied = append(s.applied, "guard_runes_sanitized")
		s.mu.Unlock()
		s.runesMarked = true
	}
	return rebuildSSEEvent(eventName, id, hasID, retry, out, bytes.Contains(event, []byte("\r\n")))
}

// splitSSEEvent decomposes one raw SSE event (already delimited by fill's
// blank-line splitter) into its event/id/retry fields and concatenated
// data payload, so rebuildSSEEvent can round-trip every field a sanitized
// event carries -- not just data. id: carries the client's reconnection
// Last-Event-ID and retry: its reconnect backoff; dropping either on a
// sanitized event is a real byte-fidelity defect, not a harmless
// simplification. Comment lines (a bare ":" prefix) carry no client-
// visible meaning per the SSE spec and are dropped, matching the wire's
// own semantics. hasID is reported separately from id == "": the W3C spec
// gives an explicit empty id: line its own meaning (reset the client's
// Last-Event-ID buffer to empty), distinct from no id: field at all, and
// id alone can't tell the two apart.
func splitSSEEvent(raw []byte) (event, id string, hasID bool, retry string, data []byte) {
	for _, line := range splitSSELines(raw) {
		switch {
		case bytes.HasPrefix(line, []byte("event:")):
			event = string(stripLeadingSSESpace(line[len("event:"):]))
		case bytes.HasPrefix(line, []byte("id:")):
			id = string(stripLeadingSSESpace(line[len("id:"):]))
			hasID = true
		case bytes.HasPrefix(line, []byte("retry:")):
			retry = string(stripLeadingSSESpace(line[len("retry:"):]))
		case bytes.HasPrefix(line, []byte("data:")):
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, stripLeadingSSESpace(line[len("data:"):])...)
		}
	}
	return event, id, hasID, retry, data
}

// splitSSELines splits raw on any SSE line terminator (LF, CRLF, or a lone
// CR -- W3C SSE spec sanctions all three) so a mixed or classic-Mac-style
// event round-trips through splitSSEEvent/rebuildSSEEvent correctly,
// matching findSSEDelimiter's own three-form delimiter recognition: a raw
// event that arrived because a \r\r delimiter closed it must still have
// its interior \r-only lines parsed as separate lines, not folded into one
// unbroken blob mistaken for a single field.
func splitSSELines(raw []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '\n':
			lines = append(lines, raw[start:i])
			start = i + 1
		case '\r':
			lines = append(lines, raw[start:i])
			if i+1 < len(raw) && raw[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(raw) {
		lines = append(lines, raw[start:])
	}
	return lines
}

// stripLeadingSSESpace removes at most the single leading U+0020 SPACE the
// W3C SSE spec allows a field value to carry right after its colon ("If
// value starts with a single U+0020 SPACE character, remove it") --
// unlike bytes.TrimSpace, it never touches trailing whitespace or any
// space beyond the first, so a payload's own leading/trailing whitespace
// (e.g. inside a multi-line data: block) survives byte-for-byte through
// an event the sanitizer rebuilds.
func stripLeadingSSESpace(b []byte) []byte {
	if len(b) > 0 && b[0] == ' ' {
		return b[1:]
	}
	return b
}

// rebuildSSEEvent re-serializes an SSE event from its (possibly absent)
// id/event/retry fields and data payload, matching the exact shape
// splitSSEEvent parses back out of it. Field order (id, event, retry,
// then data lines) follows the W3C SSE spec's own informative example
// ordering; a client's line-by-line field parser does not care about
// order, so this is a readability choice, not a correctness one. It
// formats multi-line data with a "data: " prefix on each line per spec,
// joined with the original event's own line terminator (crlf true when
// the upstream used "\r\n") so sanitizing an event never downgrades the
// transport's line-ending convention on its own. hasID (not id != "")
// decides whether to emit id: at all: an explicit empty id: line is a
// real W3C reset instruction (clears the client's Last-Event-ID buffer),
// and dropping it because id happens to be "" would silently defeat that
// reset on any event this function has to rebuild.
func rebuildSSEEvent(eventName, id string, hasID bool, retry string, data []byte, crlf bool) []byte {
	nl := "\n"
	if crlf {
		nl = "\r\n"
	}
	var b bytes.Buffer
	if hasID {
		b.WriteString("id: " + id + nl)
	}
	if eventName != "" {
		b.WriteString("event: " + eventName + nl)
	}
	if retry != "" {
		b.WriteString("retry: " + retry + nl)
	}
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		b.WriteString("data: ")
		b.Write(line)
		b.WriteString(nl)
	}
	b.WriteString(nl)
	return b.Bytes()
}

func (s *inboundStream) Applied() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applied
}
