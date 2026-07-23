// Ver 2026-07-23 10:00, by Sonnet 5

package server

import "testing"

func TestComputeRequestFacts_PlainText(t *testing.T) {
	body := []byte(`{"model":"agent","messages":[{"role":"user","content":"hello there"}]}`)
	facts := computeRequestFacts(body, "openai")
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
	enFacts := computeRequestFacts(en, "openai")
	zhFacts := computeRequestFacts(zh, "openai")
	if zhFacts.EstimatedTokens <= enFacts.EstimatedTokens {
		t.Errorf("expected Chinese text to estimate more tokens per byte than English: en=%d zh=%d", enFacts.EstimatedTokens, zhFacts.EstimatedTokens)
	}
}

func TestComputeRequestFacts_Tools(t *testing.T) {
	withTools := []byte(`{"model":"agent","tools":[{"name":"x"}],"messages":[]}`)
	emptyTools := []byte(`{"model":"agent","tools":[],"messages":[]}`)
	noTools := []byte(`{"model":"agent","messages":[]}`)

	if !computeRequestFacts(withTools, "openai").HasTools {
		t.Errorf("non-empty tools array must set HasTools")
	}
	if computeRequestFacts(emptyTools, "openai").HasTools {
		t.Errorf("empty tools array must not set HasTools")
	}
	if computeRequestFacts(noTools, "openai").HasTools {
		t.Errorf("absent tools field must not set HasTools")
	}
}

func TestComputeRequestFacts_Image(t *testing.T) {
	openaiImg := []byte(`{"model":"agent","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`)
	facts := computeRequestFacts(openaiImg, "openai")
	if !facts.HasImage {
		t.Errorf("expected HasImage=true")
	}
	textOnly := []byte(`{"model":"agent","messages":[{"role":"user","content":"no images here"}]}`)
	if computeRequestFacts(textOnly, "openai").HasImage {
		t.Errorf("plain text must not set HasImage")
	}

	// Two images should estimate roughly double the tokens of one.
	oneImg := computeRequestFacts(openaiImg, "openai").EstimatedTokens
	twoImgBody := []byte(`{"model":"agent","messages":[{"role":"user","content":[` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}},` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,BBBB"}}]}]}`)
	twoImg := computeRequestFacts(twoImgBody, "openai").EstimatedTokens
	if twoImg <= oneImg {
		t.Errorf("two images should estimate more tokens than one: one=%d two=%d", oneImg, twoImg)
	}
}

func TestComputeRequestFacts_DocumentEstimateOnlyWhenMarkerPresent(t *testing.T) {
	// A long base64-looking "data" field with no document marker anywhere
	// must NOT trigger the document estimate (avoids false positives on
	// arbitrary large string fields).
	noMarker := []byte(`{"model":"agent","messages":[{"role":"user","content":"data:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}]}`)
	base := computeRequestFacts(noMarker, "openai").EstimatedTokens

	withDoc := []byte(`{"model":"agent","messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"` +
		string(make([]byte, 400)) + `"}}]}]}`)
	// Replace the zero-bytes with a repeated printable char so it's a
	// realistic-looking (if fake) base64 payload, not embedded NULs.
	for i := range withDoc {
		if withDoc[i] == 0 {
			withDoc[i] = 'A'
		}
	}
	docFacts := computeRequestFacts(withDoc, "anthropic")
	if docFacts.EstimatedTokens <= base {
		t.Errorf("a request with a document marker + large data field should estimate more tokens than one without any marker: base=%d withDoc=%d", base, docFacts.EstimatedTokens)
	}
}
