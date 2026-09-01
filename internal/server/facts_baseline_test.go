// Ver 2026-09-02 07:05, by pi-agent

package server

import (
	"os"
	"strings"
	"testing"
)

// Absolute magnitude baselines (R105).
//
// The differential/parity tests (cmd/vmr/quota_parity_test.go) pin the
// analytics side to the router side — and are structurally blind to the two
// sides being wrong TOGETHER: they share the same basis, so a wrong basis
// passes them green (R60 lived through exactly that: both sides counted
// inline base64 payload as text, parity stayed green, the ledger was wrong).
// These tests are the orthogonal check: hand-labeled fixtures with the
// expected estimate MAGNITUDE asserted directly, no cross-side agreement
// involved. When one of them trips, do NOT widen the band to get green —
// first check whether the estimate basis itself is wrong.
//
// The fixtures in testdata/ carry a __TEXT__ / __B64__ placeholder; the
// placeholder is substituted with a deterministic payload of the size the
// test's label states. Payload sizes stay in the test (not the fixture) so
// the labels and the assertions can never drift apart.

// baselineFixture reads one testdata template and substitutes placeholders.
func baselineFixture(t *testing.T, name string, subs ...string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	body := string(raw)
	if len(subs)%2 != 0 {
		t.Fatalf("fixture %s: odd substitution count", name)
	}
	for i := 0; i < len(subs); i += 2 {
		body = strings.ReplaceAll(body, subs[i], subs[i+1])
	}
	return []byte(body)
}

// b64Of returns n bytes of deterministic base64-alphabet payload.
func b64Of(n int) string {
	p := make([]byte, n)
	for i := range p {
		p[i] = "QUJD"[i%4]
	}
	return string(p)
}

// English prose runs roughly 4 characters per token; the bands below are
// half-to-double-ish around that hand label, not around tokenutil's own
// formula — asserting the formula's own output against itself would make
// this a tautology, not a baseline.

// TestBaseline_PlainTextRequest pins the no-attachment floor: a request
// whose only content is a known amount of English text must estimate at the
// ~chars/4 scale. Guards against the text estimate ever regressing to a
// raw-byte basis (JSON envelope counted as content) or to zero.
func TestBaseline_PlainTextRequest(t *testing.T) {
	// 4400 ASCII letters+spaces ≈ 1100 tokens at the hand label of 4 chars/token.
	text := strings.Repeat("abcdefghij ", 400)
	if len(text) != 4400 {
		t.Fatalf("label drift: text length = %d, want 4400", len(text))
	}
	body := baselineFixture(t, "baseline_plaintext.json", "__TEXT__", text)
	facts := computeRequestFacts(body, 0, false)
	if facts.EstimatedTokens < 550 || facts.EstimatedTokens > 2200 {
		t.Errorf("plaintext estimate = %d, want within [550, 2200] (~4400 chars at ~4 chars/token).\n"+
			"Outside the band means the text basis is wrong: near-zero suggests content is not being extracted; "+
			"far above suggests JSON envelope/structural bytes are being counted as message text.\n"+
			"This is an ABSOLUTE baseline: parity tests cannot catch the basis being wrong on both sides at once, only this can.",
			facts.EstimatedTokens)
	}
}

// TestBaseline_ImageRequestEstimateIsImageScale is the R60 gate: a request
// carrying a 500KB inline image must estimate at (text + one flat per-image
// estimate), NOT at (text + ~100K tokens of base64 counted as text). If this
// trips, the attachment payload bytes have leaked back into the text
// estimate — the exact failure that made the quota ledger over-charge by
// two orders of magnitude while every parity test stayed green.
func TestBaseline_ImageRequestEstimateIsImageScale(t *testing.T) {
	const payloadBytes = 500_000
	for _, tc := range []struct{ name, fixture string }{
		{"anthropic", "baseline_image_anthropic.json"},
		{"openai", "baseline_image_openai.json"},
	} {
		body := baselineFixture(t, tc.fixture, "__B64__", b64Of(payloadBytes))
		withImg := computeRequestFacts(body, 1, false)
		// The baseline must be a genuinely image-free body — computing it from
		// the same body with imageCount=0 would cancel the shared error (the
		// payload bytes counted as text appear in BOTH terms), recreating
		// inside this test the very both-sides-wrong-together blindness the
		// absolute baselines exist to break.
		textOnly := computeRequestFacts(baselineFixture(t, tc.fixture, "__B64__", ""), 0, false)
		diff := withImg.EstimatedTokens - textOnly.EstimatedTokens
		// Label: one image ≈ imageTokenEstimate (flat, per-image); allow
		// 2x for calibration movement. The base64 payload counted as text
		// would add ~payloadBytes/5 more — two orders of magnitude away.
		if diff <= 0 || diff > 2*imageTokenEstimate {
			t.Errorf("%s: image delta = %d, want within (0, %d].\n"+
				"A value near %d (payload bytes as text) means attachment base64 is being counted as message text again — "+
				"the request-side twin of the R60 mis-accounting that parity tests could not see because the "+
				"router and the analytics half shared the same wrong basis.",
				tc.name, diff, 2*imageTokenEstimate, payloadBytes/5)
		}
	}
}

// TestBaseline_DocumentRequestEstimateIsDocumentScale is the document twin:
// a 300KB PDF payload must contribute ~bytes/documentBytesPerToken and
// nothing else.
func TestBaseline_DocumentRequestEstimateIsDocumentScale(t *testing.T) {
	const payloadBytes = 300_000
	body := baselineFixture(t, "baseline_document_pdf.json", "__B64__", b64Of(payloadBytes))
	withDoc := computeRequestFacts(body, 0, false)
	textOnly := computeRequestFacts([]byte(`{"model":"agent","messages":[{"role":"user","content":"Summarize this document."}]}`), 0, false)
	diff := withDoc.EstimatedTokens - textOnly.EstimatedTokens
	want := int64(payloadBytes / documentBytesPerToken) // 15000
	if diff < want/2 || diff > 2*want {
		t.Errorf("document delta = %d, want within [%d, %d].\n"+
			"Far above means payload bytes leaked into the text estimate; far below means the document estimate was lost. "+
			"Absolute baseline — parity cannot catch a shared wrong basis.",
			diff, want/2, 2*want)
	}
}
