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
func readAllBounded(t *testing.T, s *stream) []byte {
	t.Helper()
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	for i := 0; i < 1_000_000; i++ {
		n, err := s.Read(tmp)
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

// FuzzStream exercises the transport state machine's three documented
// invariants (see the architecture review's Part 8 batch B7 entry): no
// panic/hang on any byte stream, opaque mode never transforms a single
// byte, and the think-strip repair never leaves a complete <think>...
// </think> pair behind once it has fired. A fourth, narrower invariant
// (passthrough is byte-identical to input) is checked only when the input
// contains none of the three things that legitimately make passthrough NOT
// byte-identical — a "model" field value, an existing [DONE] sentinel, or
// the openai protocol's own DONE-append policy — since those are documented,
// deliberate transforms, not something a passthrough fuzz check should
// treat as a bug.
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
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		hasModelField := bytes.Contains(data, []byte(`"model":`))
		hasDone := bytes.Contains(data, doneSentinel)
		wantThinkOpens := bytes.Count(data, thinkOpenMarker)

		for _, isSSE := range []bool{true, false} {
			for _, protocol := range []string{"openai", "anthropic"} {
				for _, opaque := range []bool{true, false} {
					s := newStream(bytes.NewReader(data), "agent", "upstream-model", isSSE, protocol, opaque)
					out := readAllBounded(t, s)

					if opaque {
						// Opaque skips every transform unconditionally —
						// the strongest, zero-exception version of the
						// package's byte-faithful-passthrough promise.
						if !bytes.Equal(out, data) {
							t.Fatalf("opaque=true must never transform bytes: isSSE=%v protocol=%s\nin=%q\nout=%q", isSSE, protocol, data, out)
						}
						continue
					}

					if got := bytes.Count(out, thinkOpenMarker); got > wantThinkOpens {
						t.Fatalf("<think> occurrence count increased (stripping can only remove, never add): isSSE=%v protocol=%s in=%d out=%d\nin=%q\nout=%q",
							isSSE, protocol, wantThinkOpens, got, data, out)
					}

					if s.stripFired() && thinkPattern.Match(out) {
						t.Fatalf("think_strip fired but a complete <think>...</think> pair still matches the output: isSSE=%v protocol=%s\nin=%q\nout=%q",
							isSSE, protocol, data, out)
					}

					// Passthrough byte-identity: only asserted when none of
					// the three sanctioned transforms could legitimately
					// have fired, so this never false-positives on a
					// deliberate rewrite.
					if s.mode == modePassthrough && !hasModelField && !hasDone && protocol != "openai" {
						if !bytes.Equal(out, data) {
							t.Fatalf("passthrough with no model field/DONE/openai-DONE-append trigger must be byte-identical: isSSE=%v protocol=%s\nin=%q\nout=%q",
								isSSE, protocol, data, out)
						}
					}
				}
			}
		}
	})
}
