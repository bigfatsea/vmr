// Ver 2026-08-15, by Sonnet 5

// Fuzz coverage for the transport state machine (Wrap/stream): the one
// hand-written byte-level state machine in the routing half with no fuzz
// coverage until this batch (architecture review's Part 8 batch B7) — the
// real payoff of extracting this package, not a line-count reduction (see
// respnorm.go's own package doc comment). Runs every fuzzed byte string
// through every {isSSE, protocol, opaque} combination this package's
// behavior actually branches on, at the pure io.Reader level, independent
// of Router/Snapshot — the same "expensive-to-hit shapes in seconds instead
// of hand-enumerated cases" payoff internal/jsonscan's own fuzz tests
// document for the request-side byte scanners (see rewrite_fuzz_test.go).
//
// Goes through the exported Wrap/Options surface, not newStream directly
// (a B7 follow-up review's finding: the package's own tests never actually
// exercised Wrap's Options field mapping, only router's downstream
// integration tests did) — a type assertion back to *stream right after
// gets the internal state (mode, stripFired) the invariant checks below
// need, without that assertion itself being the thing under test.
package respnorm

import (
	"bytes"
	"io"
	"testing"
)

// readAllBounded drains rs the same way respnorm_test.go's own readAll
// does (a Read returning (0, nil) is a legitimate "nothing deliverable yet"
// per Read's doc comment, not a terminal condition), but with a hard
// iteration cap: a genuine infinite loop inside the state machine should
// fail this specific fuzz case fast and legibly, rather than only ever
// surfacing as an opaque "go test -fuzz" worker timeout.
func readAllBounded(t *testing.T, rs io.Reader) []byte {
	t.Helper()
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	for i := 0; i < 1_000_000; i++ {
		n, err := rs.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err == io.EOF {
			return buf.Bytes()
		}
		if err != nil {
			t.Fatalf("Read returned a non-EOF error over a finite in-memory source: %v", err)
		}
	}
	t.Fatal("Read never reached EOF after 1,000,000 calls — likely an infinite loop")
	return nil
}

// chunkedReader hands data back in size-byte pieces instead of all at once.
// bytes.NewReader alone would let any fuzzed input smaller than the 32KB
// scratch buffer (respnorm.go's Read) reach ingest() in a single call,
// never exercising the state machine's cross-chunk logic: an event
// separator split across two Read calls, decide()'s scanned cursor
// resuming past a prior partial scan instead of rescanning from zero, a
// <think> closer arriving in a later chunk than its opener. size <= 0 is
// treated as 1 — the caller clamps to >=1 already, but a zero-size reader
// would otherwise hand back (0, nil) forever, which is exactly the
// "genuine infinite loop" readAllBounded exists to catch, not something
// this helper should be able to trigger by construction.
type chunkedReader struct {
	data []byte
	size int
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := c.size
	if n <= 0 {
		n = 1
	}
	if n > len(c.data) {
		n = len(c.data)
	}
	if n > len(p) {
		n = len(p)
	}
	copy(p, c.data[:n])
	c.data = c.data[n:]
	return n, nil
}

// asStream unwraps Wrap's return value back to the concrete type — the
// only implementation NormalizerStream has, so this can never fail outside
// a future second implementation deliberately choosing not to satisfy it
// (which would itself be worth knowing about, hence Fatalf rather than a
// silent skip).
func asStream(t *testing.T, ns NormalizerStream) *stream {
	t.Helper()
	s, ok := ns.(*stream)
	if !ok {
		t.Fatalf("Wrap returned %T, not *stream", ns)
	}
	return s
}

// appliedChangesContent reports whether applied (a stream's Applied()
// result) contains any transform that can change the client-visible bytes.
// "buffered"/"resumed_stream"/"crlf_framing_suspected"/
// "soft_block_detected"/"thinking_process_pattern_detected"/
// "overflow_raw_passthrough" are bookkeeping/mode markers, not byte
// transforms in themselves (a response can be marked "buffered" and still
// come out byte-identical if nothing inside it actually needed rewriting).
func appliedChangesContent(applied []string) bool {
	for _, a := range applied {
		switch a {
		case "model_rewrite", "think_strip", "thinking_process_strip", "done_appended":
			return true
		}
	}
	return false
}

// FuzzStream exercises the transport state machine's invariants (see the
// architecture review's Part 8 batch B7 entry and its follow-up review):
// no panic/hang on any byte stream; fragmentation-invariance (the SAME
// bytes delivered in one Read vs. many small Reads must produce
// byte-identical output — this is what actually exercises the cross-chunk
// state transitions, not just "fuzzing the whole-body parse"), with one
// narrow, documented exception (modelFieldSpansEventSep below); opaque mode
// never transforms a single byte; the think-strip repair never leaves a
// complete <think>...</think> pair behind once it has fired; and — checked
// against Applied(), the authoritative transform record, not a guess from
// raw input content — no output differs from input unless Applied() says
// something actually changed it.
func FuzzStream(f *testing.F) {
	seeds := []string{
		// Well-formed SSE, one payload event.
		`data: {"choices":[{"delta":{"content":"hello"}}]}` + "\n\n",
		// Anthropic-shaped text delta.
		`data: {"type":"content_block_delta","delta":{"text":"hi"}}` + "\n\n",
		// Dedicated reasoning fields — well-behaved, never guard-checked.
		`data: {"choices":[{"delta":{"reasoning_content":"r"}}]}` + "\n\n" +
			`data: {"choices":[{"delta":{"content":"hi"}}]}` + "\n\n",
		// Tool-call / partial_json passthrough markers.
		`data: {"choices":[{"delta":{"tool_calls":[{"id":"1"}]}}]}` + "\n\n",
		`data: {"delta":{"partial_json":"{\"a\":"}}` + "\n\n",
		// Complete inline <think> block — should get stripped.
		`data: {"choices":[{"delta":{"content":"<think>reasoning</think>done"}}]}` + "\n\n",
		// Unclosed <think> — legitimately can't be stripped (nothing closes
		// it yet); must pass through as literal text, not get mangled.
		`data: {"choices":[{"delta":{"content":"<think>never closes`,
		// <think> mentioned mid-text, not at the start — must NOT trigger
		// the strip guard (thinkShapeGuard requires it as the FIRST value).
		`data: {"choices":[{"delta":{"content":"see the <think> tag docs"}}]}` + "\n\n",
		// MiniMax "Thinking Process:" shape, non-streaming.
		`{"choices":[{"message":{"content":"Thinking Process:\n1. step one\n2. step two\nLooks good. Proceed\nfinal answer"}}]}`,
		// Soft-block markers.
		`data: {"input_sensitive":true}` + "\n\n",
		`{"output_sensitive":true}`,
		// model field rewrite target, including a "$" that would be
		// misread as a regexp.ReplaceAll submatch reference if unescaped.
		`data: {"model":"MiniMax-M3","choices":[{"delta":{"content":"hi"}}]}` + "\n\n",
		// Upstream already sent its own [DONE].
		`data: {"choices":[{"delta":{"content":"hi"}}]}` + "\n\n" + `data: [DONE]` + "\n\n",
		// Role markers / pings that don't decide the mode by themselves.
		`data: {"type":"ping"}` + "\n\n" + `data: {"choices":[{"delta":{"role":"assistant"}}]}` + "\n\n",
		// Empty / degenerate.
		``,
		"\n\n",
		// Non-JSON garbage, invalid UTF-8, and CRLF framing.
		`not json at all`,
		string([]byte{0xff, 0xfe, '<', 't', 'h', 'i', 'n', 'k', '>'}),
		"data: {}\r\n\r\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s), uint8(0))  // chunkSize=1: maximum fragmentation
		f.Add([]byte(s), uint8(63)) // chunkSize=64: a few bytes per event
	}

	f.Fuzz(func(t *testing.T, data []byte, chunkSizeByte uint8) {
		wantThinkOpens := bytes.Count(data, thinkOpenMarker)
		chunkSize := int(chunkSizeByte) + 1 // clamp to [1,256]

		// modelFieldSpansEventSep is the one known, accepted exception to
		// fragmentation-invariance below: the byte-splice model rewrite can
		// only fire when the WHOLE "model":\s*"..." match (modelFieldPattern
		// itself allows whitespace — including a raw newline — between the
		// colon and the opening quote, not just inside the value) lands
		// inside a single emitted block. A raw, un-escaped eventSep
		// ("\n\n") anywhere in that match — in the \s* gap or in the value
		// — can fool the event-boundary scanner into emitting a block that
		// ends mid-field, something a fragmented delivery can expose in a
		// way a single whole-body Read never does (see respnorm.go's
		// emitBlock). This requires a literal, unescaped newline byte
		// somewhere a compliant JSON encoder would never put one raw
		// (either inside a string value, or as insignificant whitespace a
		// real upstream has no reason to emit mid-field) — invalid-shaped
		// input no real upstream produces, so it's a real gap only against
		// malformed input; the other invariants below still hold,
		// whole-shot and chunked are just allowed to disagree on this one
		// specific output.
		modelFieldSpansEventSep := false
		if m := modelFieldPattern.FindSubmatch(data); m != nil && bytes.Contains(m[0], eventSep) {
			modelFieldSpansEventSep = true
		}

		// checkInvariants applies every invariant except fragmentation-
		// equality to one (out, s) pair — called once per delivery mode
		// (whole-shot, chunked) so a divergence in the accepted exception
		// above doesn't skip validating the rest.
		checkInvariants := func(out []byte, s *stream, label string) {
			if s.opaque {
				// Opaque skips every transform unconditionally — the
				// strongest, zero-exception version of the package's
				// byte-faithful-passthrough promise.
				if !bytes.Equal(out, data) {
					t.Fatalf("opaque=true must never transform bytes [%s]: isSSE=%v protocol=%s\nin=%q\nout=%q", label, s.isSSE, s.protocol, data, out)
				}
				return
			}

			if got := bytes.Count(out, thinkOpenMarker); got > wantThinkOpens {
				t.Fatalf("<think> occurrence count increased (stripping can only remove, never add) [%s]: isSSE=%v protocol=%s in=%d out=%d\nin=%q\nout=%q",
					label, s.isSSE, s.protocol, wantThinkOpens, got, data, out)
			}

			if s.stripFired() && thinkPattern.Match(out) {
				t.Fatalf("think_strip fired but a complete <think>...</think> pair still matches the output [%s]: isSSE=%v protocol=%s\nin=%q\nout=%q",
					label, s.isSSE, s.protocol, data, out)
			}

			// Byte-identity: only asserted when Applied() (the authoritative
			// transform record — see respnorm.go's package doc on "every
			// transform actually applied is recorded") reports NO
			// content-changing transform. Checking Applied() directly
			// rather than guessing from raw input content (e.g. "does data
			// contain a model field") is what makes this correct across
			// mode transitions: a response that starts buffered because of
			// a <think> block, then RESUMES as modePassthrough once the
			// block closes (see ingest's "resumed_stream" transition in
			// respnorm.go), still has think_strip recorded in Applied()
			// even though the FINAL settled mode is passthrough — a
			// heuristic keyed only on "final mode == passthrough" would
			// miss that a transform already fired earlier in the response.
			if !appliedChangesContent(s.Applied()) {
				if !bytes.Equal(out, data) {
					t.Fatalf("no content-changing transform recorded in Applied()=%v but output differs from input [%s]: isSSE=%v protocol=%s\nin=%q\nout=%q",
						s.Applied(), label, s.isSSE, s.protocol, data, out)
				}
			}
		}

		for _, isSSE := range []bool{true, false} {
			for _, protocol := range []string{"openai", "anthropic", "openai-responses"} {
				for _, opaque := range []bool{true, false} {
					opts := Options{
						ClientModel:   "agent",
						UpstreamModel: "upstream-model",
						IsSSE:         isSSE,
						Protocol:      protocol,
						Opaque:        opaque,
					}

					wholeShot := asStream(t, Wrap(bytes.NewReader(data), opts))
					wholeOut := readAllBounded(t, wholeShot)

					chunked := asStream(t, Wrap(&chunkedReader{data: data, size: chunkSize}, opts))
					chunkedOut := readAllBounded(t, chunked)

					if !bytes.Equal(wholeOut, chunkedOut) && !modelFieldSpansEventSep {
						t.Fatalf("output depends on fragmentation alone: isSSE=%v protocol=%s opaque=%v chunkSize=%d\nin=%q\nwhole-shot=%q\nchunked=%q",
							isSSE, protocol, opaque, chunkSize, data, wholeOut, chunkedOut)
					}

					checkInvariants(wholeOut, wholeShot, "whole-shot")
					checkInvariants(chunkedOut, chunked, "chunked")
				}
			}
		}
	})
}
