// Ver 2026-09-17, by Sonnet 5

package guard

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestInbound_NonOpaquePassesThroughUnchanged(t *testing.T) {
	g := NewGuard(testEngine(t))
	src := bytes.NewReader([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
	s := g.Inbound(src, InboundOpts{})
	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" {
		t.Errorf("got %q, want the input unchanged", got)
	}
	if len(s.Applied()) != 0 {
		t.Error("Applied() should be empty for a normal stream")
	}
}

// TestInbound_OpaqueSkipsSanitizationAndPassesThrough covers ADR-15's
// post-removal opaque handling: there is no block/passthrough policy
// anymore, opaque bytes just skip sanitization unconditionally (nothing
// readable to strip, nothing to gate).
func TestInbound_OpaqueSkipsSanitizationAndPassesThrough(t *testing.T) {
	g := NewGuard(testEngine(t))
	payload := []byte("raw opaque-ish bytes -- must not be touched or held up")
	src := bytes.NewReader(payload)
	s := g.Inbound(src, InboundOpts{Opaque: true, SanitizeInvisibleRunes: true})
	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("got %q, want the payload unchanged", got)
	}
	if len(s.Applied()) != 0 {
		t.Error("Applied() should be empty when Opaque skips sanitization")
	}
}

// BenchmarkInbound_PassthroughOverhead records the sanitize-off Read
// path's per-call cost -- InboundStream.Read on the pass-through path is
// one extra method call plus a bool check per Read, so this should be
// close to the cost of reading from src directly.
func BenchmarkInbound_PassthroughOverhead(b *testing.B) {
	g := NewGuard(&Engine{}) // engine unused on the pass-through path
	payload := make([]byte, 4096)
	b.ReportAllocs()
	b.ResetTimer()
	buf := make([]byte, len(payload))
	for i := 0; i < b.N; i++ {
		s := g.Inbound(bytes.NewReader(payload), InboundOpts{})
		for {
			_, err := s.Read(buf)
			if err != nil {
				break
			}
		}
	}
}

func TestInbound_DelimiterCRLF(t *testing.T) {
	g := NewGuard(testEngine(t))
	// Build an SSE event using \r\n\r\n as delimiter.
	event := "data: {\"choices\":[{\"delta\":{\"content\":\"hello crlf\"}}]}\r\n\r\n"
	s := g.Inbound(bytes.NewReader([]byte(event)), InboundOpts{
		SanitizeInvisibleRunes: true,
	})
	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != event {
		t.Errorf("got %q, want %q", got, event)
	}
}

// TestInbound_SanitizedEventPreservesCRLF confirms that rewriting a CRLF-
// delimited event (rune sanitization actually changed its bytes) keeps
// using CRLF line endings, rather than silently downgrading to bare LF.
func TestInbound_SanitizedEventPreservesCRLF(t *testing.T) {
	g := NewGuard(testEngine(t))
	// U+200B (ZWSP, B-tier) inside the content string forces sanitizeEvent
	// to actually rewrite this event's bytes.
	event := "data: {\"choices\":[{\"delta\":{\"content\":\"hello\\u200Bworld\"}}]}\r\n\r\n"
	s := g.Inbound(bytes.NewReader([]byte(event)), InboundOpts{
		SanitizeInvisibleRunes: true,
	})
	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if strings.Contains(string(got), "​") {
		t.Errorf("got %q, want the ZWSP stripped", got)
	}
	if !strings.Contains(string(got), "\r\n") {
		t.Errorf("got %q, want CRLF line endings preserved after rewriting", got)
	}
	if strings.Contains(strings.ReplaceAll(string(got), "\r\n", ""), "\n") {
		t.Errorf("got %q, want no bare LF once CRLF is stripped out, i.e. no mixed line endings", got)
	}
}

// TestInbound_TrailingEventFlushedOnEOF: a stream that ends without a
// trailing blank line (an incomplete final SSE event) must still be
// flushed to the client verbatim -- there is nothing left to complete it
// with, and (post-ADR-15) nothing to gate even if there were.
func TestInbound_TrailingEventFlushedOnEOF(t *testing.T) {
	g := NewGuard(testEngine(t))
	trailing := "data: {\"choices\":[{\"delta\":{\"content\":\"no trailing blank line\"}}]}"
	s := g.Inbound(strings.NewReader(trailing), InboundOpts{SanitizeInvisibleRunes: true})
	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != trailing {
		t.Errorf("got %q, want the incomplete trailing event flushed unchanged: %q", got, trailing)
	}
}

// errAfterReader returns data once, then err on every subsequent Read --
// simulating a connection that delivers a partial event and then breaks.
type errAfterReader struct {
	data []byte
	err  error
	done bool
}

func (r *errAfterReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		return copy(p, r.data), nil
	}
	return 0, r.err
}

// TestInbound_NonEOFReadErrorFlushesBufferedBytes covers the independent
// review's finding: fill used to return a non-EOF src.Read error straight
// away, discarding whatever inbuf had already buffered while waiting for
// the next SSE delimiter -- bytes a plain (unguarded) passthrough would
// still have delivered to the client before the connection broke.
func TestInbound_NonEOFReadErrorFlushesBufferedBytes(t *testing.T) {
	g := NewGuard(testEngine(t))
	boom := errors.New("boom")
	partial := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial, no delimiter yet\"}}]}")
	s := g.Inbound(&errAfterReader{data: partial, err: boom}, InboundOpts{SanitizeInvisibleRunes: true})
	got, err := io.ReadAll(s)
	if !errors.Is(err, boom) {
		t.Fatalf("ReadAll err = %v, want %v", err, boom)
	}
	if !bytes.Equal(got, partial) {
		t.Errorf("got %q, want the buffered bytes flushed before the error: %q", got, partial)
	}
}

// TestInbound_HoldCapExceeded_FallsBackToRawPassthrough: a pathological
// upstream that never sends a blank-line delimiter must not make Read
// hold an unbounded buffer -- past inboundSanitizeMaxHoldBytes, fill gives
// up on re-framing and relays bytes as they arrive, unsanitized, marking
// guard_sanitize_hold_exceeded (ADR-15's safety valve).
func TestInbound_HoldCapExceeded_FallsBackToRawPassthrough(t *testing.T) {
	g := NewGuard(testEngine(t))
	// One giant "event" with no blank-line delimiter anywhere, larger than
	// the hold cap -- content deliberately includes a ZWSP that would have
	// been stripped had sanitization actually run on it.
	payload := append([]byte("data: "), bytes.Repeat([]byte("x"), inboundSanitizeMaxHoldBytes+1)...)
	payload = append(payload, []byte("​-tail-no-delimiter-ever")...)
	s := g.Inbound(bytes.NewReader(payload), InboundOpts{SanitizeInvisibleRunes: true})
	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("got %d bytes, want the %d-byte payload relayed byte-for-byte once the hold cap is exceeded", len(got), len(payload))
	}
	found := false
	for _, a := range s.Applied() {
		if a == "guard_sanitize_hold_exceeded" {
			found = true
		}
	}
	if !found {
		t.Errorf("Applied() = %v, want guard_sanitize_hold_exceeded", s.Applied())
	}
}

// TestInbound_NonSSEShapeGivesUpOnFirstChunk covers router.go's isSSE
// guess getting it wrong: an upstream that sent a one-shot JSON body (no
// SSE delimiter anywhere) on a connection the router classified as SSE
// (e.g. empty Content-Type alongside a client stream:true request). Before
// looksLikeNonSSE, fill() would hold every byte until EOF or the 256 KiB
// cap; this proves the fix releases the first chunk without waiting for a
// second chunk that has not arrived yet -- an io.Pipe write blocks until
// read, so a successful ReadFull here is only possible if fill() gave up
// and flushed on the first chunk alone.
func TestInbound_NonSSEShapeGivesUpOnFirstChunk(t *testing.T) {
	g := NewGuard(testEngine(t))
	pr, pw := io.Pipe()
	s := g.Inbound(pr, InboundOpts{SanitizeInvisibleRunes: true})

	first := []byte(`{"error":"bad gateway"`)
	go func() { _, _ = pw.Write(first) }()

	buf := make([]byte, len(first))
	if _, err := io.ReadFull(s, buf); err != nil {
		t.Fatalf("Read first chunk: %v", err)
	}
	if !bytes.Equal(buf, first) {
		t.Errorf("got %q, want first chunk %q relayed without waiting for the rest", buf, first)
	}

	second := []byte(`,"code":502}`)
	go func() {
		_, _ = pw.Write(second)
		pw.Close()
	}()
	rest, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("ReadAll rest: %v", err)
	}
	if !bytes.Equal(rest, second) {
		t.Errorf("got %q, want second chunk %q", rest, second)
	}

	found := false
	for _, a := range s.Applied() {
		if a == "guard_sanitize_non_sse" {
			found = true
		}
	}
	if !found {
		t.Errorf("Applied() = %v, want guard_sanitize_non_sse", s.Applied())
	}
}

// TestInbound_LeadingWhitespaceBeforeJSONStillGivesUp covers
// looksLikeNonSSE's whitespace-skip: a pretty-printed or padded JSON body
// starting with a blank line or indentation must still be recognized as
// non-SSE, not mistaken for an SSE blank-line lead-in.
func TestInbound_LeadingWhitespaceBeforeJSONStillGivesUp(t *testing.T) {
	g := NewGuard(testEngine(t))
	payload := []byte("  \n{\"ok\":false}")
	s := g.Inbound(bytes.NewReader(payload), InboundOpts{SanitizeInvisibleRunes: true})
	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("got %q, want %q relayed unchanged", got, payload)
	}
	found := false
	for _, a := range s.Applied() {
		if a == "guard_sanitize_non_sse" {
			found = true
		}
	}
	if !found {
		t.Errorf("Applied() = %v, want guard_sanitize_non_sse", s.Applied())
	}
}

// TestInbound_RealSSENeverFlaggedNonSSE guards looksLikeNonSSE's
// conservatism the other way: a genuine SSE stream (leading comment line,
// then a data: event) must never trip the early give-up, regardless of
// what its data payload itself looks like.
func TestInbound_RealSSENeverFlaggedNonSSE(t *testing.T) {
	g := NewGuard(testEngine(t))
	payload := []byte(": keepalive\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
	s := g.Inbound(bytes.NewReader(payload), InboundOpts{SanitizeInvisibleRunes: true})
	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("got %q, want %q relayed unchanged", got, payload)
	}
	for _, a := range s.Applied() {
		if a == "guard_sanitize_non_sse" || a == "guard_sanitize_hold_exceeded" {
			t.Errorf("Applied() = %v, a real SSE stream must never give up re-framing", s.Applied())
		}
	}
}

// chunkThenErrReader returns n full chunks of data (nil error), then one
// final chunk delivered together WITH a non-EOF error on the same call
// (some real io.Reader implementations, e.g. net.Conn after a partial
// read on a reset connection, do exactly this), then err alone forever --
// used to drive fill() into giveUp mode (via repeated appends past
// inboundSanitizeMaxHoldBytes) and then exercise that same-call
// data+error combination in the giveUp branch itself, distinct from
// errAfterReader's single-chunk, separate-call setup.
type chunkThenErrReader struct {
	chunk []byte
	n     int
	err   error
	fired bool
}

func (r *chunkThenErrReader) Read(p []byte) (int, error) {
	if r.fired {
		// The fix must hand the already-received error back via pendingErr
		// on the very next fill() call, never call src.Read again to get
		// it a second time -- a real broken connection has no obligation
		// to keep returning the same sentinel on retry.
		panic("chunkThenErrReader: Read called again after the data+error call -- the error was dropped instead of held in pendingErr")
	}
	if r.n > 0 {
		r.n--
		if r.n == 0 {
			r.fired = true
			return copy(p, r.chunk), r.err
		}
		return copy(p, r.chunk), nil
	}
	return 0, r.err
}

// TestInbound_GiveUpModeSurfacesReadError covers the independent review's
// finding: once fill() has given up re-framing (past
// inboundSanitizeMaxHoldBytes), a non-EOF src.Read error returned together
// with data on the same call used to be handed back from fill() directly
// -- which Read()'s "outbuf non-empty wins" check then silently discards,
// since the data from that same call already made outbuf non-empty. The
// error must survive via pendingErr and surface on the next Read once
// that data is drained, not vanish.
func TestInbound_GiveUpModeSurfacesReadError(t *testing.T) {
	g := NewGuard(testEngine(t))
	boom := errors.New("boom")
	chunk := bytes.Repeat([]byte("x"), 32<<10) // matches inbound.go's rbuf size
	nChunks := inboundSanitizeMaxHoldBytes/len(chunk) + 2
	r := &chunkThenErrReader{chunk: chunk, n: nChunks, err: boom}
	s := g.Inbound(r, InboundOpts{SanitizeInvisibleRunes: true})
	got, err := io.ReadAll(s)
	if !r.fired {
		t.Fatal("test setup bug: the data+error combined call never happened")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("ReadAll err = %v, want %v (must not be silently dropped once giveUp is active)", err, boom)
	}
	if want := nChunks * len(chunk); len(got) != want {
		t.Errorf("got %d bytes, want %d (all chunks, including the one delivered alongside the error, must reach the caller)", len(got), want)
	}
}

// payloadThenErrReader serves data in whatever chunk size the caller's
// buffer allows, and returns a non-EOF error together with the read that
// first pushes cumulative bytes served past inboundSanitizeMaxHoldBytes --
// the exact "giveUp becomes true and a read error arrive on the same
// fill() call" scenario ISSUE-5 is about, distinct from
// chunkThenErrReader's fixed-chunk-count setup above (which fires the
// error on a LATER call, once giveUp is already active).
type payloadThenErrReader struct {
	data  []byte
	err   error
	sent  int
	fired bool
}

func (r *payloadThenErrReader) Read(p []byte) (int, error) {
	if r.fired {
		panic("payloadThenErrReader: Read called again after the data+error call")
	}
	n := copy(p, r.data[r.sent:])
	r.sent += n
	if r.sent > inboundSanitizeMaxHoldBytes {
		r.fired = true
		return n, r.err
	}
	return n, nil
}

// TestInbound_GiveUpAndErrorSameCall_NoSanitizationOnOversizedTrailing
// covers the independent review's finding: when a single fill() call both
// pushes inbuf past the hold cap (setting giveUp) AND receives a non-EOF
// read error, the trailing-flush branch called sanitizeEventSafe on that
// now-oversized blob instead of relaying it raw -- contradicting giveUp's
// own purpose (bounding per-event sanitization work) the same way the
// giveUp branch at the top of fill() already correctly avoids it. Proven
// by an invisible rune embedded in a quoted JSON string early in the
// payload: sanitization WOULD strip it if it ran (verified directly
// against SanitizeEventJSON), so it surviving in the output proves the
// fix took the raw-passthrough path instead.
func TestInbound_GiveUpAndErrorSameCall_NoSanitizationOnOversizedTrailing(t *testing.T) {
	g := NewGuard(testEngine(t))
	boom := errors.New("boom")
	var payload []byte
	payload = append(payload, []byte(`data: "`)...)
	payload = append(payload, bytes.Repeat([]byte("x"), 260000)...)
	payload = append(payload, []byte("​")...)
	payload = append(payload, bytes.Repeat([]byte("y"), 10000)...)
	payload = append(payload, []byte(`"`)...)
	if len(payload) <= inboundSanitizeMaxHoldBytes {
		t.Fatalf("test setup bug: payload (%d bytes) must exceed the hold cap (%d)", len(payload), inboundSanitizeMaxHoldBytes)
	}
	r := &payloadThenErrReader{data: payload, err: boom}
	s := g.Inbound(r, InboundOpts{SanitizeInvisibleRunes: true})
	got, err := io.ReadAll(s)
	if !r.fired {
		t.Fatal("test setup bug: the data+error combined call never happened")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("ReadAll err = %v, want %v", err, boom)
	}
	if !bytes.Contains(got, []byte("​")) {
		t.Error("invisible rune was stripped -- the oversized trailing blob went through sanitization instead of raw passthrough")
	}
	if !bytes.Equal(got, payload[:len(got)]) {
		t.Error("output diverges from the raw input prefix -- expected untouched raw passthrough")
	}
}

// TestInbound_ConcurrentReadAndInspection covers the independent review's
// finding: Read (and everything it calls -- fill, sanitizeEvent,
// sanitizeEventSafe) runs on copyFlush's own background reader goroutine
// (transport.go), while RuneCounts/Applied are called from
// relayResponseBody's goroutine AFTER copyFlush has already returned --
// which it can do while that reader goroutine is still mid-Read (client
// disconnect or idle timeout both make copyFlush return without waiting
// for an in-flight upstream read to finish). Without inboundStream's own
// mutex (mirroring respnorm.stream's mu in the same read chain, added for
// the identical reason), this is an unsynchronized map/slice
// read-vs-write -- go test -race must catch it if the locking above is
// ever removed.
func TestInbound_ConcurrentReadAndInspection(t *testing.T) {
	g := NewGuard(testEngine(t))
	event := "data: " + string(mustSanitizeJSON(t, map[string]any{
		"choices": []map[string]any{{"delta": map[string]string{"content": "safe" + string(rune(0x202E)) + "text"}}},
	})) + "\n\n"
	const n = 2000
	var buf bytes.Buffer
	for i := 0; i < n; i++ {
		buf.WriteString(event)
	}
	s := g.Inbound(&buf, InboundOpts{SanitizeInvisibleRunes: true})

	var wg sync.WaitGroup
	wg.Add(2)
	stop := make(chan struct{})
	go func() {
		defer wg.Done()
		p := make([]byte, 4096)
		for {
			if _, err := s.Read(p); err != nil {
				close(stop)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				s.RuneCounts()
				s.Applied()
			}
		}
	}()
	wg.Wait()
}

func TestRebuildSSEEvent_MultiLine(t *testing.T) {
	// Single line data
	single := rebuildSSEEvent("", "", false, "", []byte("hello"), false)
	if string(single) != "data: hello\n\n" {
		t.Errorf("single = %q, want %q", single, "data: hello\n\n")
	}

	// Multi-line data (W3C standard)
	multi := rebuildSSEEvent("update", "", false, "", []byte("line1\nline2\nline3"), false)
	want := "event: update\ndata: line1\ndata: line2\ndata: line3\n\n"
	if string(multi) != want {
		t.Errorf("multi = %q, want %q", multi, want)
	}

	// CRLF-terminated original event must stay CRLF after rebuilding.
	crlf := rebuildSSEEvent("update", "", false, "", []byte("line1\nline2"), true)
	wantCRLF := "event: update\r\ndata: line1\r\ndata: line2\r\n\r\n"
	if string(crlf) != wantCRLF {
		t.Errorf("crlf = %q, want %q", crlf, wantCRLF)
	}

	// id: and retry: must round-trip -- the independent review's finding:
	// a naive rewrite that only reads/writes event:/data: silently drops
	// the client's reconnection Last-Event-ID and backoff hint.
	full := rebuildSSEEvent("update", "42", true, "3000", []byte("payload"), false)
	wantFull := "id: 42\nevent: update\nretry: 3000\ndata: payload\n\n"
	if string(full) != wantFull {
		t.Errorf("full = %q, want %q", full, wantFull)
	}

	// An explicit empty id: (W3C's Last-Event-ID reset instruction) must
	// still be emitted -- hasID, not id != "", gates the line. A second
	// independent review's finding: id != "" used to drop this exact case.
	reset := rebuildSSEEvent("", "", true, "", []byte("hello"), false)
	wantReset := "id: \ndata: hello\n\n"
	if string(reset) != wantReset {
		t.Errorf("reset = %q, want %q", reset, wantReset)
	}
}

func TestSplitSSEEvent_RoundTripsIDAndRetry(t *testing.T) {
	raw := []byte("id: 7\nevent: update\nretry: 1500\ndata: {\"x\":1}\n\n")
	event, id, hasID, retry, data := splitSSEEvent(raw)
	if event != "update" || id != "7" || !hasID || retry != "1500" || string(data) != `{"x":1}` {
		t.Errorf("splitSSEEvent(%q) = (event=%q, id=%q, hasID=%v, retry=%q, data=%q)", raw, event, id, hasID, retry, data)
	}
	rebuilt := rebuildSSEEvent(event, id, hasID, retry, data, false)
	if string(rebuilt) != string(raw) {
		t.Errorf("round trip = %q, want %q", rebuilt, raw)
	}
}

// TestSplitSSEEvent_ExplicitEmptyIDRoundTrips covers the independent
// review's finding: an event whose id: field is present but empty (the
// W3C's explicit Last-Event-ID reset) must round-trip as hasID == true,
// not collapse into "no id: field at all" the way a bare id string can't
// help but do.
func TestSplitSSEEvent_ExplicitEmptyIDRoundTrips(t *testing.T) {
	raw := []byte("id:\ndata: {\"x\":1}\n\n")
	event, id, hasID, retry, data := splitSSEEvent(raw)
	if id != "" || !hasID || event != "" || retry != "" || string(data) != `{"x":1}` {
		t.Errorf("splitSSEEvent(%q) = (event=%q, id=%q, hasID=%v, retry=%q, data=%q), want hasID=true", raw, event, id, hasID, retry, data)
	}
	rebuilt := rebuildSSEEvent(event, id, hasID, retry, data, false)
	wantRebuilt := "id: \ndata: {\"x\":1}\n\n"
	if string(rebuilt) != wantRebuilt {
		t.Errorf("rebuilt = %q, want %q", rebuilt, wantRebuilt)
	}

	noID, _, hasIDAbsent, _, _ := splitSSEEvent([]byte("data: {\"x\":1}\n\n"))
	_ = noID
	if hasIDAbsent {
		t.Error("hasID = true for an event with no id: field at all, want false")
	}
}
