// Ver 2026-08-15, by Sonnet 5

// Baseline benchmarks for the hot streaming-forward path — this package had
// none until a B7 follow-up review flagged it: CLAUDE.md's "performance
// must never regress" invariant has nothing to check itself against without
// one. Not a before/after comparison against the pre-B7 internal/router
// code (that code no longer exists to benchmark) — a going-forward snapshot
// so a future change to this file has something to run `benchstat` against.
package respnorm

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// benchDrain reads s to EOF the same way readAll does (a (0, nil) Read is
// legitimate mid-stream, not an error), without the *testing.T dependency
// readAll has.
func benchDrain(b *testing.B, s NormalizerStream) {
	b.Helper()
	buf := make([]byte, 4096)
	for {
		_, err := s.Read(buf)
		if err == io.EOF {
			return
		}
		if err != nil {
			b.Fatalf("read error: %v", err)
		}
	}
}

// benchSSEPayload simulates a realistic streamed chat completion: n small
// token-delta events, the common case on the hot forward path (passthrough
// mode, no MiniMax quirk shape).
func benchSSEPayload(n int) []byte {
	var buf bytes.Buffer
	for i := 0; i < n; i++ {
		buf.WriteString(`data: {"choices":[{"delta":{"content":"tok"}}]}` + "\n\n")
	}
	return buf.Bytes()
}

// BenchmarkStream_PassthroughStreaming is the hot-path default: SSE,
// openai, no quirk shape detected — settles into passthrough after the
// first event and streams the rest through with a per-block regex scan,
// never buffering the whole body.
func BenchmarkStream_PassthroughStreaming(b *testing.B) {
	payload := benchSSEPayload(200) // ~200 token deltas, a realistic reply length
	opts := Options{ClientModel: "agent", UpstreamModel: "upstream-model", IsSSE: true, Protocol: "openai-completions"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchDrain(b, Wrap(bytes.NewReader(payload), opts))
	}
}

// BenchmarkStream_Buffered is the non-SSE path: a single JSON object,
// normalized in one regex pass at EOF instead of per-event.
func BenchmarkStream_Buffered(b *testing.B) {
	payload := []byte(`{"choices":[{"message":{"content":"` + strings.Repeat("token ", 500) + `"}}]}`)
	opts := Options{ClientModel: "agent", UpstreamModel: "upstream-model", IsSSE: false, Protocol: "openai-completions"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchDrain(b, Wrap(bytes.NewReader(payload), opts))
	}
}

// BenchmarkStream_OpaquePassthrough is the zero-transform path (compressed
// response body) — a pure byte copy with usage/byte-count sniffing but no
// regex work at all, the cheapest of the three.
func BenchmarkStream_OpaquePassthrough(b *testing.B) {
	payload := benchSSEPayload(200)
	opts := Options{ClientModel: "agent", UpstreamModel: "upstream-model", IsSSE: true, Protocol: "openai-completions", Opaque: true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchDrain(b, Wrap(bytes.NewReader(payload), opts))
	}
}

// BenchmarkStream_ChunkedReadPartial exercises partial drains on s.out
// where the caller's read buffer is smaller than the SSE event size,
// ensuring capacity preservation avoids repeated slice reallocation.
func BenchmarkStream_ChunkedReadPartial(b *testing.B) {
	payload := benchSSEPayload(200)
	opts := Options{ClientModel: "agent", UpstreamModel: "upstream-model", IsSSE: true, Protocol: "openai-completions"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := Wrap(bytes.NewReader(payload), opts)
		buf := make([]byte, 32)
		for {
			_, err := s.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				b.Fatalf("read error: %v", err)
			}
		}
	}
}
