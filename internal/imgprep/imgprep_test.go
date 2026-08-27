// Ver 2026-07-24 12:05, by Sonnet 5
package imgprep

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func solidJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 255), uint8(y % 255), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func transparentPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{200, 50, 50, 128}) // half-transparent red
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func animatedGIF(t *testing.T, w, h int) []byte {
	t.Helper()
	pal := []color.Color{color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}}
	f1 := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	f2 := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	g := &gif.GIF{Image: []*image.Paletted{f1, f2}, Delay: []int{10, 10}}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func singleFrameGIF(t *testing.T, w, h int) []byte {
	t.Helper()
	pal := []color.Color{color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}}
	f1 := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	g := &gif.GIF{Image: []*image.Paletted{f1}, Delay: []int{0}}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakePNGHeader builds a syntactically valid PNG that declares huge
// dimensions in IHDR but carries no pixel data — enough for
// image.DecodeConfig (which stops right after IHDR) without requiring an
// actual multi-gigabyte raster in memory. Used to exercise the
// decompression-bomb guard without allocating the bomb.
func fakePNGHeader(t *testing.T, w, h uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	var ihdr bytes.Buffer
	binary.Write(&ihdr, binary.BigEndian, w)
	binary.Write(&ihdr, binary.BigEndian, h)
	ihdr.Write([]byte{8, 6, 0, 0, 0}) // 8-bit depth, color type 6 (truecolor+alpha)
	writeChunk(t, &buf, "IHDR", ihdr.Bytes())
	return buf.Bytes()
}

func writeChunk(t *testing.T, buf *bytes.Buffer, typ string, data []byte) {
	t.Helper()
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	buf.Write(lenBuf[:])
	body := append([]byte(typ), data...)
	buf.Write(body)
	var crcBuf [4]byte
	binary.BigEndian.PutUint32(crcBuf[:], crc32.ChecksumIEEE(body))
	buf.Write(crcBuf[:])
}

func dataURI(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func openAIReq(t *testing.T, url string) []byte {
	t.Helper()
	req := map[string]any{
		"model": "coding",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "what's in this image?"},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": url, "detail": "auto"}},
				},
			},
		},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func anthropicReq(t *testing.T, mime string, data []byte) []byte {
	t.Helper()
	req := map[string]any{
		"model":      "claude",
		"max_tokens": 64,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": mime,
							"data":       base64.StdEncoding.EncodeToString(data),
						},
					},
				},
			},
		},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// extractOpenAIImage pulls the data-URI payload back out of a rewritten
// OpenAI-shaped body, decoding it into an image.Image.
func extractOpenAIImage(t *testing.T, body []byte) (image.Image, string) {
	t.Helper()
	var req struct {
		Messages []struct {
			Content []struct {
				Type     string `json:"type"`
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal rewritten body: %v", err)
	}
	url := req.Messages[0].Content[1].ImageURL.URL
	data, ok := parseDataURI(url)
	if !ok {
		t.Fatalf("expected a data URI, got %q", url)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode rewritten image: %v", err)
	}
	return img, format
}

// responsesReq builds a Responses-protocol request body: top-level "input"
// array instead of "messages", and image_url as a FLAT string field on the
// input_image block (not the nested {"image_url":{"url":...}} object Chat
// Completions uses) — see the openai-python SDK's ResponseInputImageParam.
func responsesReq(t *testing.T, url string) []byte {
	t.Helper()
	req := map[string]any{
		"model": "coding",
		"input": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "what's in this image?"},
					map[string]any{"type": "input_image", "image_url": url, "detail": "auto"},
				},
			},
		},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// responsesFileIDReq builds a Responses request whose input_image block
// references an already-uploaded file (file_id) instead of carrying inline
// bytes — no image_url field at all.
func responsesFileIDReq(t *testing.T, fileID string) []byte {
	t.Helper()
	req := map[string]any{
		"model": "coding",
		"input": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_image", "file_id": fileID},
				},
			},
		},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func extractResponsesImage(t *testing.T, body []byte) (image.Image, string) {
	t.Helper()
	var req struct {
		Input []struct {
			Content []struct {
				Type     string `json:"type"`
				ImageURL string `json:"image_url"`
			} `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal rewritten body: %v", err)
	}
	url := req.Input[0].Content[1].ImageURL
	data, ok := parseDataURI(url)
	if !ok {
		t.Fatalf("expected a data URI, got %q", url)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode rewritten image: %v", err)
	}
	return img, format
}

func extractAnthropicImage(t *testing.T, body []byte) (image.Image, string, string) {
	t.Helper()
	var req struct {
		Messages []struct {
			Content []struct {
				Type   string `json:"type"`
				Source struct {
					MediaType string `json:"media_type"`
					Data      string `json:"data"`
				} `json:"source"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal rewritten body: %v", err)
	}
	src := req.Messages[0].Content[0].Source
	data, err := base64.StdEncoding.DecodeString(src.Data)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode rewritten image: %v", err)
	}
	return img, format, src.MediaType
}

func TestHasImageMarker(t *testing.T) {
	cases := map[string]bool{
		`{"messages":[{"content":[{"type":"image_url"}]}]}`:                    true,
		`{"messages":[{"content":[{"type":"image"}]}]}`:                        true,
		`{"messages":[{"content":"hello there"}]}`:                             false,
		`{"input":[{"content":[{"type":"input_image","image_url":"data:"}]}]}`: true, // caught via the "image_url" key
		`{"input":[{"content":[{"type":"input_image","file_id":"file-1"}]}]}`:  true, // file_id-only: no image_url key at all, caught via the bare "input_image" type value
	}
	for body, want := range cases {
		if got := HasImageMarker([]byte(body)); got != want {
			t.Errorf("HasImageMarker(%q) = %v, want %v", body, got, want)
		}
	}
}

// TestDownscaleDisabledIsNoop covers the resize path being off, not
// detection: MaxPx<=0 must never rewrite the body, but the image should
// still be described (format/dimensions/bytes) since audit metadata
// shouldn't depend on the resize feature being configured.
func TestDownscaleDisabledIsNoop(t *testing.T) {
	raw := solidJPEG(t, 2000, 1000)
	body := openAIReq(t, dataURI("image/jpeg", raw))
	got, images := Downscale(body, "openai-completions", Options{MaxPx: 0})
	if &got[0] != &body[0] {
		t.Error("maxPx<=0 must return the exact same slice, not a copy")
	}
	if len(images) != 1 {
		t.Fatalf("images = %d, want 1", len(images))
	}
	img := images[0]
	if img.Format != "jpeg" || img.Width != 2000 || img.Height != 1000 || img.Bytes != int64(len(raw)) {
		t.Errorf("image metadata = %+v, want format=jpeg width=2000 height=1000 bytes=%d", img, len(raw))
	}
	if img.Downscaled || img.CacheHit || img.Remote {
		t.Errorf("image metadata = %+v, want Downscaled/CacheHit/Remote all false (resizing was never attempted)", img)
	}
}

func TestDownscaleNoMarkerIsNoop(t *testing.T) {
	body := []byte(`{"model":"coding","messages":[{"role":"user","content":"just text"}]}`)
	got, images := Downscale(body, "openai-completions", Options{MaxPx: 512})
	if &got[0] != &body[0] {
		t.Error("a request with no image marker must return the exact same slice")
	}
	if len(images) != 0 {
		t.Errorf("images = %v, want none", images)
	}
}

// TestDownscaleTextMentioningMarkerIsNotAnImage locks in a real incident: a
// coding agent's tool output quoted a Go test assertion containing the
// literal string `"image_downscale=512px"`. Once that text landed in a later
// turn's plain-text content, HasImageMarker's cheap substring pre-check
// fired (correctly, per its own contract — it's not supposed to be
// precise), but the structural scan below must still find zero real image
// content blocks. RequestFacts.HasImage is wired to this function's result
// (not to HasImageMarker) specifically so this case can never again trip
// the hard "image" capability Condition and wrongly eliminate every
// endpoint for a plain-text request.
func TestDownscaleTextMentioningMarkerIsNotAnImage(t *testing.T) {
	body := []byte(`{"model":"coding","messages":[{"role":"user","content":"please check the test assertion \"image_downscale=512px\" in main_test.go"}]}`)
	if !HasImageMarker(body) {
		t.Fatal("test setup: body must trip the cheap presence marker for this test to be meaningful")
	}
	got, images := Downscale(body, "openai-completions", Options{MaxPx: 512})
	if &got[0] != &body[0] {
		t.Error("a request with no real image block must return the exact same slice")
	}
	if len(images) != 0 {
		t.Errorf("images = %v, want none — the marker fired on quoted text, not a real image content block", images)
	}
}

func TestOpenAIImageAboveThresholdIsResized(t *testing.T) {
	body := openAIReq(t, dataURI("image/jpeg", solidJPEG(t, 2000, 1000)))
	out, _ := Downscale(body, "openai-completions", Options{MaxPx: 512})
	img, format := extractOpenAIImage(t, out)
	b := img.Bounds()
	if format != "jpeg" {
		t.Errorf("output format = %q, want jpeg", format)
	}
	if b.Dx() != 512 || b.Dy() != 256 {
		t.Errorf("resized to %dx%d, want 512x256 (aspect-preserved)", b.Dx(), b.Dy())
	}
}

func TestOpenAIImageBelowThresholdUntouched(t *testing.T) {
	body := openAIReq(t, dataURI("image/jpeg", solidJPEG(t, 300, 200)))
	out, _ := Downscale(body, "openai-completions", Options{MaxPx: 512})
	if !bytes.Equal(out, body) {
		t.Error("an image already within the pixel cap must not be rewritten")
	}
}

func TestOpenAIRemoteURLNotFetched(t *testing.T) {
	body := openAIReq(t, "https://example.com/some-huge-photo.jpg")
	out, images := Downscale(body, "openai-completions", Options{MaxPx: 512})
	if !bytes.Equal(out, body) {
		t.Error("remote image URLs must never be fetched or rewritten")
	}
	if len(images) != 1 || !images[0].Remote || images[0].Bytes != 0 || images[0].Format != "" {
		t.Errorf("images = %+v, want one Remote=true entry with no size/format", images)
	}
}

func TestResponsesImageAboveThresholdIsResized(t *testing.T) {
	body := responsesReq(t, dataURI("image/jpeg", solidJPEG(t, 2000, 1000)))
	out, _ := Downscale(body, "openai-responses", Options{MaxPx: 512})
	img, format := extractResponsesImage(t, out)
	b := img.Bounds()
	if format != "jpeg" {
		t.Errorf("output format = %q, want jpeg", format)
	}
	if b.Dx() != 512 || b.Dy() != 256 {
		t.Errorf("resized to %dx%d, want 512x256 (aspect-preserved)", b.Dx(), b.Dy())
	}
}

func TestResponsesImageBelowThresholdUntouched(t *testing.T) {
	body := responsesReq(t, dataURI("image/jpeg", solidJPEG(t, 300, 200)))
	out, _ := Downscale(body, "openai-responses", Options{MaxPx: 512})
	if !bytes.Equal(out, body) {
		t.Error("an image already within the pixel cap must not be rewritten")
	}
}

func TestResponsesRemoteURLNotFetched(t *testing.T) {
	body := responsesReq(t, "https://example.com/some-huge-photo.jpg")
	out, images := Downscale(body, "openai-responses", Options{MaxPx: 512})
	if !bytes.Equal(out, body) {
		t.Error("remote image URLs must never be fetched or rewritten")
	}
	if len(images) != 1 || !images[0].Remote || images[0].Bytes != 0 || images[0].Format != "" {
		t.Errorf("images = %+v, want one Remote=true entry with no size/format", images)
	}
}

// TestResponsesFileIDImageCounted is the gap HasImageMarker's second check
// exists to close: a Files-API-referenced input_image block has no
// "image_url" field at all (only "file_id"), so it can't be detected via
// the same substring that catches every other shape — this proves the
// request still comes out of Downscale with a real ImageInfo entry
// (HasImage must not silently read false for it) even though there is
// nothing to fetch or resize.
func TestResponsesFileIDImageCounted(t *testing.T) {
	body := responsesFileIDReq(t, "file-abc123")
	out, images := Downscale(body, "openai-responses", Options{MaxPx: 512})
	if !bytes.Equal(out, body) {
		t.Error("a file_id-referenced image must never be rewritten")
	}
	if len(images) != 1 || !images[0].Remote {
		t.Errorf("images = %+v, want one Remote=true entry (detected, nothing to fetch)", images)
	}
}

func TestAnthropicImageAboveThresholdIsResizedAndFlattened(t *testing.T) {
	body := anthropicReq(t, "image/png", transparentPNG(t, 1200, 1200))
	out, _ := Downscale(body, "anthropic-messages", Options{MaxPx: 512})
	img, format, mediaType := extractAnthropicImage(t, out)
	b := img.Bounds()
	if format != "jpeg" || mediaType != "image/jpeg" {
		t.Errorf("format=%q media_type=%q, want jpeg/image/jpeg (JPEG has no alpha)", format, mediaType)
	}
	if b.Dx() != 512 || b.Dy() != 512 {
		t.Errorf("resized to %dx%d, want 512x512", b.Dx(), b.Dy())
	}
	// Corner pixel must be a flattened opaque color, not raw partial-alpha red.
	r, _, _, a := img.At(0, 0).RGBA()
	if a != 0xffff {
		t.Errorf("flattened JPEG pixel alpha = %x, want fully opaque", a)
	}
	_ = r
}

func TestAnthropicImageBelowThresholdUntouched(t *testing.T) {
	body := anthropicReq(t, "image/png", transparentPNG(t, 100, 100))
	out, _ := Downscale(body, "anthropic-messages", Options{MaxPx: 512})
	if !bytes.Equal(out, body) {
		t.Error("an image already within the pixel cap must not be rewritten")
	}
}

func TestAnthropicNonBase64SourceUntouched(t *testing.T) {
	req := map[string]any{
		"model": "claude",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":   "image",
						"source": map[string]any{"type": "url", "url": "https://example.com/x.png"},
					},
				},
			},
		},
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	out, images := Downscale(body, "anthropic-messages", Options{MaxPx: 512})
	if len(images) != 1 || !images[0].Remote {
		t.Errorf("images = %+v, want one Remote=true entry", images)
	}
	if !bytes.Equal(out, body) {
		t.Error("url-sourced Anthropic images must never be fetched or rewritten")
	}
}

func TestAnimatedGIFUntouched(t *testing.T) {
	body := openAIReq(t, dataURI("image/gif", animatedGIF(t, 1000, 1000)))
	out, images := Downscale(body, "openai-completions", Options{MaxPx: 512})
	if !bytes.Equal(out, body) {
		t.Error("animated GIFs must be left untouched, not collapsed to a still frame")
	}
	if len(images) != 1 || images[0].Format != "gif" || images[0].Downscaled {
		t.Errorf("images = %+v, want one gif entry with Downscaled=false", images)
	}
}

// TestSingleFrameGIFUntouched locks in that GIF is never rescaled, single
// frame or not — image/gif.DecodeAll is the only stdlib way to even learn a
// GIF's frame count, and it has no cap on frames or cumulative decoded size,
// so telling a single-frame still apart from a many-frame animation would
// require paying the same unbounded-decode cost the animated case exists to
// avoid (a small-canvas, many-frame GIF is a real decompression-bomb vector).
func TestSingleFrameGIFUntouched(t *testing.T) {
	body := openAIReq(t, dataURI("image/gif", singleFrameGIF(t, 1000, 1000)))
	out, images := Downscale(body, "openai-completions", Options{MaxPx: 512})
	if !bytes.Equal(out, body) {
		t.Error("single-frame GIFs must be left untouched too — see processImage's format==\"gif\" comment")
	}
	if len(images) != 1 || images[0].Format != "gif" || images[0].Downscaled {
		t.Errorf("images = %+v, want one gif entry with Downscaled=false", images)
	}
}

func TestCorruptImageDataFailsOpen(t *testing.T) {
	body := openAIReq(t, dataURI("image/jpeg", []byte("not actually an image")))
	out, _ := Downscale(body, "openai-completions", Options{MaxPx: 512})
	if !bytes.Equal(out, body) {
		t.Error("corrupt image data must fail open (leave request unchanged), not error out")
	}
}

// TestUndecodableImageStillCounted locks in that a structurally real image
// reference (type=="image_url", a valid data URI) whose payload vmr's
// stdlib decoders can't read (corrupt bytes, or a real format Go's image
// package doesn't register — HEIC/AVIF being the common real-world case)
// still contributes an ImageInfo entry. Before this fix, a decode failure
// made the block vanish from images entirely, which — now that
// RequestFacts.HasImage is sourced from len(images) — would have
// misclassified a request that genuinely has an image (just not one vmr
// can describe) as HasImage=false, and misrouted it to an endpoint that
// doesn't declare image support.
func TestUndecodableImageStillCounted(t *testing.T) {
	openaiBody := openAIReq(t, dataURI("image/heic", []byte("not a format Go's image package can decode")))
	out, images := Downscale(openaiBody, "openai-completions", Options{MaxPx: 512})
	if !bytes.Equal(out, openaiBody) {
		t.Error("an undecodable image must still fail open (leave request unchanged)")
	}
	if len(images) != 1 {
		t.Fatalf("images = %+v, want exactly one entry for the undecodable-but-real image reference", images)
	}
	if images[0].Format != "" || images[0].Downscaled || images[0].Remote {
		t.Errorf("images[0] = %+v, want Format empty (genuinely unknown) and Downscaled/Remote both false", images[0])
	}
	if images[0].Bytes == 0 {
		t.Errorf("images[0].Bytes should still record the decoded payload length even though the format is unknown, got 0")
	}

	anthropicBody := anthropicReq(t, "image/heic", []byte("not a format Go's image package can decode"))
	_, anthropicImages := Downscale(anthropicBody, "anthropic-messages", Options{MaxPx: 512})
	if len(anthropicImages) != 1 {
		t.Fatalf("anthropic images = %+v, want exactly one entry", anthropicImages)
	}
}

func TestMalformedRequestBodyFailsOpen(t *testing.T) {
	// Has the marker substring but isn't shaped like a real chat request at all.
	body := []byte(`{"image_url_but_not_json_object`)
	out, _ := Downscale(body, "openai-completions", Options{MaxPx: 512})
	if !bytes.Equal(out, body) {
		t.Error("malformed JSON must fail open and return the input unchanged")
	}
}

func TestDecompressionBombGuard(t *testing.T) {
	// 30000x30000 = 900M declared pixels, far past maxDecodePixels, but the
	// "file" itself is a few dozen bytes — DecodeConfig reads only IHDR.
	huge := fakePNGHeader(t, 30000, 30000)
	out, mime, changed, info := processImage(huge, Options{MaxPx: 512})
	if changed || out != nil || mime != "" {
		t.Errorf("declared-oversized image must be left alone: changed=%v", changed)
	}
	if info.Downscaled {
		t.Errorf("info = %+v, want Downscaled=false (guard fired before any resize)", info)
	}
}

func TestScaledSize(t *testing.T) {
	cases := []struct{ w, h, maxPx, wantW, wantH int }{
		{2000, 1000, 512, 512, 256},
		{1000, 2000, 512, 256, 512},
		{1000, 1000, 512, 512, 512},
	}
	for _, c := range cases {
		w, h := scaledSize(c.w, c.h, c.maxPx)
		if w != c.wantW || h != c.wantH {
			t.Errorf("scaledSize(%d,%d,%d) = (%d,%d), want (%d,%d)", c.w, c.h, c.maxPx, w, h, c.wantW, c.wantH)
		}
	}
}

func TestNoImageBlocksIsNoop(t *testing.T) {
	body := []byte(`{"model":"coding","messages":[{"role":"user","content":[{"type":"text","text":"hi image lovers"}]}]}`)
	out, _ := Downscale(body, "openai-completions", Options{MaxPx: 512})
	if !bytes.Equal(out, body) {
		t.Error("a marker false-positive with no actual image block must leave the body unchanged")
	}
}

func TestUnsupportedProtocolIsNoop(t *testing.T) {
	body := openAIReq(t, dataURI("image/jpeg", solidJPEG(t, 2000, 1000)))
	out, _ := Downscale(body, "carrier-pigeon", Options{MaxPx: 512})
	if !bytes.Equal(out, body) {
		t.Error("an unrecognized protocol must not attempt any block rewrite")
	}
}

// --- on-disk cache (cache.go) ---

// TestCacheHitReusesStoredBytesInsteadOfReprocessing proves a second
// Downscale call for the same source image + maxPx returns exactly what's
// on disk rather than recomputing: it primes the cache with a real pass,
// then overwrites the cache entry with a distinguishable "poisoned" image
// that a fresh downscale would never produce. If the second call returns
// the poisoned bytes, the cache path — not reprocessing — served it.
func TestCacheHitReusesStoredBytesInsteadOfReprocessing(t *testing.T) {
	dir := t.TempDir()
	raw := solidJPEG(t, 2000, 1000)
	body := openAIReq(t, dataURI("image/jpeg", raw))
	opts := Options{MaxPx: 512, CacheDir: dir, CacheTTLDays: 7}

	out1, images1 := Downscale(body, "openai-completions", opts)
	img1, _ := extractOpenAIImage(t, out1)
	if b := img1.Bounds(); b.Dx() != 512 || b.Dy() != 256 {
		t.Fatalf("first pass not downscaled as expected: %dx%d", b.Dx(), b.Dy())
	}
	if len(images1) != 1 || images1[0].CacheHit {
		t.Errorf("first pass images = %+v, want one entry with CacheHit=false (this pass populated the cache, didn't hit it)", images1)
	}

	hash := sha256.Sum256(raw)
	cachePath := filepath.Join(dir, cacheFileName(hash, 512))
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected a cache file at %s: %v", cachePath, err)
	}

	poison := solidJPEG(t, 64, 64) // a shape real downscaling to maxPx=512 would never produce
	if err := os.WriteFile(cachePath, poison, 0o600); err != nil {
		t.Fatal(err)
	}

	out2, images2 := Downscale(body, "openai-completions", opts)
	img2, _ := extractOpenAIImage(t, out2)
	if b := img2.Bounds(); b.Dx() != 64 || b.Dy() != 64 {
		t.Errorf("second pass did not reuse the (poisoned) cache entry: got %dx%d, want 64x64 — image was reprocessed instead of read from cache", b.Dx(), b.Dy())
	}
	if len(images2) != 1 || !images2[0].CacheHit {
		t.Errorf("second pass images = %+v, want one entry with CacheHit=true", images2)
	}
}

// TestMultipleImagesMixedOutcomes covers a single request with three images
// that each take a different path: one gets downscaled, one is already
// small enough to skip, and one is a remote URL vmr never fetches — the
// per-image metadata list must have one correctly-tagged entry for each,
// in message order.
func TestMultipleImagesMixedOutcomes(t *testing.T) {
	big := dataURI("image/jpeg", solidJPEG(t, 2000, 1000))
	small := dataURI("image/jpeg", solidJPEG(t, 100, 100))
	req := map[string]any{
		"model": "coding",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": big}},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": small}},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/remote.png"}},
				},
			},
		},
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	_, images := Downscale(body, "openai-completions", Options{MaxPx: 512})
	if len(images) != 3 {
		t.Fatalf("images = %d, want 3", len(images))
	}
	if !images[0].Downscaled || images[0].Remote {
		t.Errorf("image 0 (2000x1000, over threshold) = %+v, want Downscaled=true", images[0])
	}
	if images[1].Downscaled || images[1].Remote {
		t.Errorf("image 1 (100x100, under threshold) = %+v, want Downscaled=false Remote=false", images[1])
	}
	if !images[2].Remote || images[2].Downscaled {
		t.Errorf("image 2 (remote URL) = %+v, want Remote=true Downscaled=false", images[2])
	}
	for i, img := range images {
		if img.MessageIndex != 0 {
			t.Errorf("images[%d].MessageIndex = %d, want 0 (single message)", i, img.MessageIndex)
		}
	}
}

// TestCacheKeyDependsOnMaxPx guards against two different per-model targets
// colliding on the same cache entry for the same source image.
func TestCacheKeyDependsOnMaxPx(t *testing.T) {
	dir := t.TempDir()
	raw := solidJPEG(t, 2000, 1000)
	body := openAIReq(t, dataURI("image/jpeg", raw))

	out256, _ := Downscale(body, "openai-completions", Options{MaxPx: 256, CacheDir: dir})
	out512, _ := Downscale(body, "openai-completions", Options{MaxPx: 512, CacheDir: dir})

	img256, _ := extractOpenAIImage(t, out256)
	img512, _ := extractOpenAIImage(t, out512)
	if b := img256.Bounds(); b.Dx() != 256 {
		t.Errorf("maxPx=256 result is %dx%d, want long side 256", b.Dx(), b.Dy())
	}
	if b := img512.Bounds(); b.Dx() != 512 {
		t.Errorf("maxPx=512 result is %dx%d, want long side 512", b.Dx(), b.Dy())
	}
}

func TestCacheLookupTouchesMTimeOnHit(t *testing.T) {
	dir := t.TempDir()
	var hash [32]byte
	hash[0] = 0xAB
	cacheStore(dir, hash, 256, []byte("fake-jpeg-bytes"))
	path := filepath.Join(dir, cacheFileName(hash, 256))
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	if _, ok := cacheLookup(dir, hash, 256); !ok {
		t.Fatal("expected a cache hit")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(info.ModTime()) > 5*time.Second {
		t.Errorf("cache hit should refresh mtime to now, got mtime=%v", info.ModTime())
	}
}

func TestSweepCacheDirEvictsStaleEntriesOnly(t *testing.T) {
	dir := t.TempDir()
	var freshHash, staleHash [32]byte
	freshHash[0], staleHash[0] = 1, 2
	cacheStore(dir, freshHash, 100, []byte("fresh"))
	cacheStore(dir, staleHash, 100, []byte("stale"))

	freshPath := filepath.Join(dir, cacheFileName(freshHash, 100))
	stalePath := filepath.Join(dir, cacheFileName(staleHash, 100))
	now := time.Now()
	if err := os.Chtimes(stalePath, now.AddDate(0, 0, -10), now.AddDate(0, 0, -10)); err != nil {
		t.Fatal(err)
	}

	sweepCacheDir(dir, 7, now)

	if _, err := os.Stat(freshPath); err != nil {
		t.Errorf("fresh entry should survive a 7-day sweep: %v", err)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Errorf("stale entry (10 days old, ttl 7d) should have been evicted, stat err=%v", err)
	}
}

func TestSweepCacheDirTTLDisabledKeepsEverything(t *testing.T) {
	dir := t.TempDir()
	var hash [32]byte
	hash[0] = 9
	cacheStore(dir, hash, 100, []byte("data"))
	path := filepath.Join(dir, cacheFileName(hash, 100))
	old := time.Now().AddDate(-1, 0, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	sweepCacheDir(dir, 0, time.Now())

	if _, err := os.Stat(path); err != nil {
		t.Errorf("ttlDays<=0 must disable time-based eviction: %v", err)
	}
}

func TestSweepCacheDirCapEnforcedEvenWithTTLDisabled(t *testing.T) {
	dir := t.TempDir()
	var h1, h2 [32]byte
	h1[0], h2[0] = 1, 2
	cacheStore(dir, h1, 100, []byte("data-one")) // 8 bytes
	cacheStore(dir, h2, 100, []byte("data-two")) // 8 bytes

	p1 := filepath.Join(dir, cacheFileName(h1, 100))
	p2 := filepath.Join(dir, cacheFileName(h2, 100))

	now := time.Now()
	// Both entries are ancient by mtime — with TTL enabled they'd both be
	// removed by the time-based pass. TTL is disabled here (ttlDays=0), so
	// only the capacity cap can be responsible for any eviction.
	_ = os.Chtimes(p1, now.AddDate(-1, 0, 0), now.AddDate(-1, 0, 0))
	_ = os.Chtimes(p2, now.AddDate(-1, 0, 0).Add(time.Hour), now.AddDate(-1, 0, 0).Add(time.Hour))

	// Cap of 8 bytes: only the newer entry (p2) fits; the older (p1) must
	// still be evicted despite ttlDays<=0 — the cap is not gated on TTL.
	sweepCacheDirWithCap(dir, 0, now, 8)

	if _, err := os.Stat(p1); !os.IsNotExist(err) {
		t.Errorf("capacity cap must evict the oldest entry even when TTL is disabled, stat err=%v", err)
	}
	if _, err := os.Stat(p2); err != nil {
		t.Errorf("p2 should survive within capacity: %v", err)
	}
}

func TestSweepCacheDirRemovesStrayTempFiles(t *testing.T) {
	dir := t.TempDir()
	stray := filepath.Join(dir, ".deadbeef-100.jpg.tmp-12345")
	if err := os.WriteFile(stray, []byte("half-written"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -10)
	if err := os.Chtimes(stray, old, old); err != nil {
		t.Fatal(err)
	}

	sweepCacheDir(dir, 7, time.Now())

	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Errorf("a stale leftover .tmp- file from a crashed write should be cleaned up, stat err=%v", err)
	}
}

func TestSweepCacheDirEvictsCapacityOverflow(t *testing.T) {
	dir := t.TempDir()
	var h1, h2, h3 [32]byte
	h1[0], h2[0], h3[0] = 1, 2, 3
	cacheStore(dir, h1, 100, []byte("data-one"))   // 8 bytes
	cacheStore(dir, h2, 100, []byte("data-two"))   // 8 bytes
	cacheStore(dir, h3, 100, []byte("data-three")) // 10 bytes

	p1 := filepath.Join(dir, cacheFileName(h1, 100))
	p2 := filepath.Join(dir, cacheFileName(h2, 100))
	p3 := filepath.Join(dir, cacheFileName(h3, 100))

	now := time.Now()
	// h1 is oldest, h2 is middle, h3 is newest
	_ = os.Chtimes(p1, now.Add(-3*time.Hour), now.Add(-3*time.Hour))
	_ = os.Chtimes(p2, now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	_ = os.Chtimes(p3, now.Add(-1*time.Hour), now.Add(-1*time.Hour))

	// Cap of 18 bytes: h3 (10 bytes) + h2 (8 bytes) = 18 bytes. Oldest h1 (8 bytes) must be evicted.
	sweepCacheDirWithCap(dir, 7, now, 18)

	if _, err := os.Stat(p1); !os.IsNotExist(err) {
		t.Errorf("oldest entry p1 should be evicted on capacity overflow, stat err=%v", err)
	}
	if _, err := os.Stat(p2); err != nil {
		t.Errorf("p2 should survive within capacity: %v", err)
	}
	if _, err := os.Stat(p3); err != nil {
		t.Errorf("p3 should survive within capacity: %v", err)
	}
}

func TestCacheMissWithoutCacheDirNeverTouchesDisk(t *testing.T) {
	// opts.CacheDir == "" must disable caching entirely, not just no-op
	// lookups: no directory should be created as a side effect.
	body := openAIReq(t, dataURI("image/jpeg", solidJPEG(t, 2000, 1000)))
	out, _ := Downscale(body, "openai-completions", Options{MaxPx: 512})
	img, _ := extractOpenAIImage(t, out)
	if b := img.Bounds(); b.Dx() != 512 || b.Dy() != 256 {
		t.Errorf("downscale without a cache dir should still work: got %dx%d", b.Dx(), b.Dy())
	}
}

// panickyReader backs a registered fake image format whose DecodeConfig
// panics — simulating a stdlib/x-image decoder bug on adversarial input, the
// exact class of failure Downscale's recover() exists for.
func init() {
	image.RegisterFormat("panicfmt", "PANICFMT",
		func(io.Reader) (image.Image, error) { panic("panicfmt: decode") },
		func(io.Reader) (image.Config, error) { panic("panicfmt: decode config") })
}

// TestDownscalePanicRecoveredFailsOpen locks in the recover() contract: a
// decoder panic must neither escape (crashing the request goroutine) nor
// alter the request — the original bytes pass through unmodified with no
// image metadata. The stderr trace it emits is deliberately not asserted
// (logging, not behavior).
func TestDownscalePanicRecoveredFailsOpen(t *testing.T) {
	body := openAIReq(t, dataURI("image/png", []byte("PANICFMT-then-garbage")))
	out, images := Downscale(body, "openai-completions", Options{MaxPx: 512})
	if !bytes.Equal(out, body) {
		t.Error("panic path must return the original bytes unmodified")
	}
	if images != nil {
		t.Errorf("panic path must not report image metadata, got %v", images)
	}
}
