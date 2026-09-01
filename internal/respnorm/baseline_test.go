// Ver 2026-09-02 07:10, by pi-agent

package respnorm

import (
	"io"
	"os"
	"strings"
	"testing"

	"vmr/internal/chatmsg"
)

// TestBaseline_TruncatedAnthropicStream_OutEstimateIsContentScale is the
// response-side absolute magnitude baseline (R105). The parity tests pin the
// analytics estimate to the router estimate — and are blind to both being
// wrong together, because both are derived from the same basis. This test
// asserts the router's degraded output estimate directly against a
// hand-labeled fixture: a truncated Anthropic stream whose real content is a
// known amount of English text must estimate at the ~chars/4 scale of that
// text, NOT at the scale of the raw SSE envelope bytes (the R28 failure,
// where envelope bytes inflated the estimate ~70x on both sides at once).
//
// If this trips, do NOT widen the band: check what basis OutTokens (and its
// analytics twin chatmsg.EstimateResponseBodyTokens) are estimating over —
// they must measure the extracted assistant text, nothing else.
func TestBaseline_TruncatedAnthropicStream_OutEstimateIsContentScale(t *testing.T) {
	raw, err := os.ReadFile("testdata/anthropic_truncated_stream.sse")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	sse := string(raw)
	// Label: the fixture carries 911 chars of assistant text (20 sentences
	// + a cut-off tail); at ~4 chars/token that is ~230 tokens.
	const wantLo, wantHi = 115, 460

	rs := Wrap(strings.NewReader(sse), Options{
		ClientModel: "agent", UpstreamModel: "claude-x", IsSSE: true, Protocol: "anthropic-messages",
	})
	if _, err := io.Copy(io.Discard, rs); err != nil {
		t.Fatalf("drain: %v", err)
	}
	routerEst := rs.OutTokens()
	if routerEst < wantLo || routerEst > wantHi {
		t.Errorf("router OutTokens = %d, want within [%d, %d].\n"+
			"Far above means SSE/JSON envelope bytes are being counted instead of extracted text — "+
			"the pre-R28 failure mode, invisible to parity tests because the analytics half shared the basis.",
			routerEst, wantLo, wantHi)
	}

	analyticsEst := chatmsg.EstimateResponseBodyTokens(sse)
	if analyticsEst < wantLo || analyticsEst > wantHi {
		t.Errorf("analytics EstimateResponseBodyTokens = %d, want within [%d, %d] (same basis rule as the router side)", analyticsEst, wantLo, wantHi)
	}
	// Near-equality, not exact: the router extracts text per emitted block,
	// the analytics side over the whole body once, so a stream's truncated
	// final tail can classify slightly differently — legitimate. What must
	// not drift is the BASIS (extracted text vs. raw bytes), which the band
	// above pins on both sides; a gap far larger than this tolerance means
	// one side changed what it measures.
	if diff := routerEst - analyticsEst; diff > (wantHi-wantLo)/4 || diff < -(wantHi-wantLo)/4 {
		t.Errorf("basis drift: router OutTokens = %d, analytics estimate = %d — the two degraded-basis implementations no longer measure the same thing", routerEst, analyticsEst)
	}
}
