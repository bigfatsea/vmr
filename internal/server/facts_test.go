// Ver 2026-07-24 12:35, by Sonnet 5

package server

import "testing"

func TestComputeRequestFacts_PlainText(t *testing.T) {
	body := []byte(`{"model":"agent","messages":[{"role":"user","content":"hello there"}]}`)
	facts := computeRequestFacts(body, 0)
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

func TestComputeRequestFacts_ChineseCostsMoreThanEnglishPerByte(t *testing.T) {
	// Same byte count (roughly), but Chinese should estimate more tokens
	// than an equal-length ASCII string — the whole point of the split.
	en := []byte(`{"model":"agent","messages":[{"role":"user","content":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`)
	zh := []byte(`{"model":"agent","messages":[{"role":"user","content":"我我我我我我我我我我"}]}`) // 10 CJK chars = 30 bytes, similar payload size to the ascii run above
	enFacts := computeRequestFacts(en, 0)
	zhFacts := computeRequestFacts(zh, 0)
	if zhFacts.EstimatedTokens <= enFacts.EstimatedTokens {
		t.Errorf("expected Chinese text to estimate more tokens per byte than English: en=%d zh=%d", enFacts.EstimatedTokens, zhFacts.EstimatedTokens)
	}
}

func TestComputeRequestFacts_Tools(t *testing.T) {
	withTools := []byte(`{"model":"agent","tools":[{"name":"x"}],"messages":[]}`)
	emptyTools := []byte(`{"model":"agent","tools":[],"messages":[]}`)
	noTools := []byte(`{"model":"agent","messages":[]}`)

	if !computeRequestFacts(withTools, 0).HasTools {
		t.Errorf("non-empty tools array must set HasTools")
	}
	if computeRequestFacts(emptyTools, 0).HasTools {
		t.Errorf("empty tools array must not set HasTools")
	}
	if computeRequestFacts(noTools, 0).HasTools {
		t.Errorf("absent tools field must not set HasTools")
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

	if computeRequestFacts(body, 0).HasImage {
		t.Errorf("imageCount=0 must report HasImage=false")
	}
	if !computeRequestFacts(body, 1).HasImage {
		t.Errorf("imageCount=1 must report HasImage=true")
	}

	// Two images must estimate more tokens than one, scaling linearly with
	// imageCount — same body both times, only the count differs.
	oneImg := computeRequestFacts(body, 1).EstimatedTokens
	twoImg := computeRequestFacts(body, 2).EstimatedTokens
	if twoImg <= oneImg {
		t.Errorf("two images should estimate more tokens than one: one=%d two=%d", oneImg, twoImg)
	}
	if got, want := twoImg-oneImg, oneImg-computeRequestFacts(body, 0).EstimatedTokens; got != want {
		t.Errorf("each additional image should add a constant token amount: (2img-1img)=%d, (1img-0img)=%d", got, want)
	}
}

func TestComputeRequestFacts_DocumentEstimateOnlyWhenMarkerPresent(t *testing.T) {
	// A long base64-looking "data" field with no document marker anywhere
	// must NOT trigger the document estimate (avoids false positives on
	// arbitrary large string fields).
	noMarker := []byte(`{"model":"agent","messages":[{"role":"user","content":"data:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}]}`)
	base := computeRequestFacts(noMarker, 0).EstimatedTokens

	withDoc := []byte(`{"model":"agent","messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` +
		string(make([]byte, 400)) + `"}}]}]}`)
	// Replace the zero-bytes with a repeated printable char so it's a
	// realistic-looking (if fake) base64 payload, not embedded NULs.
	for i := range withDoc {
		if withDoc[i] == 0 {
			withDoc[i] = 'A'
		}
	}
	docFacts := computeRequestFacts(withDoc, 0)
	if docFacts.EstimatedTokens <= base {
		t.Errorf("a request with a document marker + large data field should estimate more tokens than one without any marker: base=%d withDoc=%d", base, docFacts.EstimatedTokens)
	}
}
