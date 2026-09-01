// Ver 2026-07-24 12:35, by Sonnet 5

package server

import (
	"testing"

	"vmr/internal/tokenutil"
)

func TestComputeRequestFacts_PlainText(t *testing.T) {
	body := []byte(`{"model":"agent","messages":[{"role":"user","content":"hello there"}]}`)
	facts := computeRequestFacts(body, 0, false)
	if facts.HasImage {
		t.Errorf("plain text request must not report HasImage")
	}
	if facts.HasTools {
		t.Errorf("plain text request must not report HasTools")
	}
	if facts.EstimatedTokens <= 0 {
		t.Errorf("expected a positive token estimate for non-empty text, got %d", facts.EstimatedTokens)
	}
}

func TestComputeRequestFacts_ChineseCostsMoreThanEnglishPerChar(t *testing.T) {
	// For equal character count, Chinese estimates more tokens than English letters (0.507 vs 0.206).
	en := []byte(`{"model":"agent","messages":[{"role":"user","content":"aaaaaaaaaa"}]}`)
	zh := []byte(`{"model":"agent","messages":[{"role":"user","content":"我我我我我我我我我我"}]}`)
	enFacts := computeRequestFacts(en, 0, false)
	zhFacts := computeRequestFacts(zh, 0, false)
	if zhFacts.EstimatedTokens <= enFacts.EstimatedTokens {
		t.Errorf("expected Chinese text to estimate more tokens per character than English: en=%d zh=%d", enFacts.EstimatedTokens, zhFacts.EstimatedTokens)
	}
}

// TestComputeRequestFacts_Tools locks in that HasTools is a straight
// pass-through of the caller-supplied hasTools argument — computeRequestFacts
// does no tools-array scanning of its own anymore (see the function's doc
// comment: the caller's single adapter.TopLevelProbe call already did that
// detection, folded into the same structural pass as model/stream). The
// detection accuracy itself (empty vs non-empty vs absent "tools") is
// adapter.TopLevelProbe's own test responsibility
// (internal/adapter/fingerprint_test.go).
func TestComputeRequestFacts_Tools(t *testing.T) {
	body := []byte(`{"model":"agent","messages":[]}`)

	if !computeRequestFacts(body, 0, true).HasTools {
		t.Errorf("hasTools=true must set facts.HasTools")
	}
	if computeRequestFacts(body, 0, false).HasTools {
		t.Errorf("hasTools=false must not set facts.HasTools")
	}
}

// TestComputeRequestFacts_Image locks in that HasImage and the image portion
// of EstimatedTokens are a straight function of the caller-supplied
// imageCount — computeRequestFacts does no image detection or marker
// scanning of its own (see the function's doc comment for why: the caller's
// single imgprep.Downscale call already did the real, structural detection,
// and re-scanning here would both risk the exact false-positive class this
// replaced a naive imgprep.HasImageMarker byte-scan to fix, and cost a
// second body scan for every request). The accuracy of that upstream
// detection is imgprep's own test responsibility
// (internal/imgprep/imgprep_test.go).
func TestComputeRequestFacts_Image(t *testing.T) {
	body := []byte(`{"model":"agent","messages":[{"role":"user","content":"hello"}]}`)

	if computeRequestFacts(body, 0, false).HasImage {
		t.Errorf("imageCount=0 must report HasImage=false")
	}
	if !computeRequestFacts(body, 1, false).HasImage {
		t.Errorf("imageCount=1 must report HasImage=true")
	}

	// Two images must estimate more tokens than one, scaling linearly with
	// imageCount — same body both times, only the count differs.
	oneImg := computeRequestFacts(body, 1, false).EstimatedTokens
	twoImg := computeRequestFacts(body, 2, false).EstimatedTokens
	if twoImg <= oneImg {
		t.Errorf("two images should estimate more tokens than one: one=%d two=%d", oneImg, twoImg)
	}
	if got, want := twoImg-oneImg, oneImg-computeRequestFacts(body, 0, false).EstimatedTokens; got != want {
		t.Errorf("each additional image should add a constant token amount: (2img-1img)=%d, (1img-0img)=%d", got, want)
	}
}

func TestComputeRequestFacts_DocumentEstimateOnlyWhenMarkerPresent(t *testing.T) {
	// A long base64-looking "data" field with no document marker anywhere
	// must NOT trigger the document estimate (avoids false positives on
	// arbitrary large string fields).
	noMarker := []byte(`{"model":"agent","messages":[{"role":"user","content":"data:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}]}`)
	base := computeRequestFacts(noMarker, 0, false).EstimatedTokens

	withDoc := []byte(`{"model":"agent","messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` +
		string(make([]byte, 400)) + `"}}]}]}`)
	// Replace the zero-bytes with a repeated printable char so it's a
	// realistic-looking (if fake) base64 payload, not embedded NULs.
	for i := range withDoc {
		if withDoc[i] == 0 {
			withDoc[i] = 'A'
		}
	}
	docFacts := computeRequestFacts(withDoc, 0, false)
	if docFacts.EstimatedTokens <= base {
		t.Errorf("a request with a document marker + large data field should estimate more tokens than one without any marker: base=%d withDoc=%d", base, docFacts.EstimatedTokens)
	}
}

// TestComputeRequestFacts_ResponsesInputFile locks in that a Responses-
// protocol input_file block's inline payload (field name "file_data", not
// Anthropic's "data") is actually counted — the field name is genuinely
// different, not just differently nested (see the openai-python SDK's
// ResponseInputFileParam), so estimateDocumentTokens needs its own marker
// for it or this silently undercounts to zero.
func TestComputeRequestFacts_ResponsesInputFile(t *testing.T) {
	noMarker := []byte(`{"model":"agent","input":[{"role":"user","content":"hello there"}]}`)
	base := computeRequestFacts(noMarker, 0, false).EstimatedTokens

	withDoc := []byte(`{"model":"agent","input":[{"role":"user","content":[{"type":"input_file","filename":"a.pdf","file_data":"` +
		string(make([]byte, 400)) + `"}]}]}`)
	for i := range withDoc {
		if withDoc[i] == 0 {
			withDoc[i] = 'A'
		}
	}
	docFacts := computeRequestFacts(withDoc, 0, false)
	if docFacts.EstimatedTokens <= base {
		t.Errorf("a Responses input_file block's file_data payload should raise the token estimate: base=%d withDoc=%d", base, docFacts.EstimatedTokens)
	}
}

// b64Payload returns n bytes of deterministic base64-alphabet payload — the
// stand-in for a real inline image/document body in the attachment tests.
func b64Payload(n int) []byte {
	p := make([]byte, n)
	for i := range p {
		p[i] = "QUJD"[i%4] // Q,U,J,D repeat — valid base64 alphabet bytes
	}
	return p
}

// TestComputeRequestFacts_ImageBase64NotCountedAsText pins R60: an inline
// image's base64 payload must NOT also be counted as message text on top of
// imageCount*imageTokenEstimate. Before the fix, tokenutil.Estimate ran over
// the whole (imgprep-rewritten) body, so a 500KB inline JPEG contributed
// ~100K phantom "text" tokens — and router/quota.go's degraded In-side
// charge reused that inflated number as-is, writing it to the quota ledger.
// The image's own contribution must stay at the imageTokenEstimate scale.
func TestComputeRequestFacts_ImageBase64NotCountedAsText(t *testing.T) {
	payload := b64Payload(500_000)
	anthropicImg := []byte(`{"model":"agent","messages":[{"role":"user","content":[{"type":"text","text":"Describe this image."},{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"` + string(payload) + `"}}]}]}`)
	openAIImg := []byte(`{"model":"agent","messages":[{"role":"user","content":[{"type":"text","text":"Describe this image."},{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,` + string(payload) + `"}}]}]}`)
	responsesImg := []byte(`{"model":"agent","input":[{"role":"user","content":[{"type":"input_text","text":"Describe this image."},{"type":"input_image","image_url":"data:image/jpeg;base64,` + string(payload) + `"}]}]}`)

	for name, body := range map[string][]byte{
		"anthropic": anthropicImg,
		"openai":    openAIImg,
		"responses": responsesImg,
	} {
		withImg := computeRequestFacts(body, 1, false)
		textOnly := computeRequestFacts(body, 0, false)
		diff := withImg.EstimatedTokens - textOnly.EstimatedTokens
		// The delta must be the flat per-image estimate (plus rounding
		// noise from excluding the payload bytes), NOT the ~100K-token
		// scale of the base64 bytes counted as text.
		if diff <= 0 || diff > 2*imageTokenEstimate {
			t.Errorf("%s: image delta = %d, want within (0, %d] — a value near the payload's text-token scale (~100K) means attachment bytes are being double-counted (R60)", name, diff, 2*imageTokenEstimate)
		}
	}
}

// TestComputeRequestFacts_PDFBase64NotCountedAsText is the document twin of
// the image test: a PDF payload's bytes are accounted by
// estimateDocumentTokens (bytes/documentBytesPerToken); they must not ALSO
// enter the text estimate.
func TestComputeRequestFacts_PDFBase64NotCountedAsText(t *testing.T) {
	payload := b64Payload(300_000)
	withDoc := []byte(`{"model":"agent","messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` + string(payload) + `"}}]}]}`)
	facts := computeRequestFacts(withDoc, 0, false)

	wantDoc := int64(300_000 / documentBytesPerToken) // 15000
	textOnly := computeRequestFacts([]byte(`{"model":"agent","messages":[{"role":"user","content":"Summarize this document."}]}`), 0, false)
	diff := facts.EstimatedTokens - textOnly.EstimatedTokens
	if diff < wantDoc/2 || diff > 2*wantDoc {
		t.Errorf("document delta = %d, want within [%d, %d] — outside means the payload bytes leaked into (or vanished from) the estimate", diff, wantDoc/2, 2*wantDoc)
	}
}

// TestComputeRequestFacts_UnterminatedPayloadNotCountedAsText covers the
// truncated-request edge: a payload value with no closing quote is spanned
// to end-of-body (still attachment bytes), not estimated as text.
func TestComputeRequestFacts_UnterminatedPayloadNotCountedAsText(t *testing.T) {
	payload := b64Payload(100_000)
	truncated := []byte(`{"model":"agent","messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` + string(payload))
	facts := computeRequestFacts(truncated, 0, false)
	if facts.EstimatedTokens > 4*int64(100_000/documentBytesPerToken) {
		t.Errorf("truncated payload estimate = %d, want at the document-token scale (~%d), not the ~%d text-token scale of the raw bytes",
			facts.EstimatedTokens, 100_000/documentBytesPerToken, tokenutil.Estimate(truncated))
	}
}
