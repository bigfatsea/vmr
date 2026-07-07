// Ver 2026-07-07 15:30, by Fable 5
package imgprep

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
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
		`{"messages":[{"content":[{"type":"image_url"}]}]}`: true,
		`{"messages":[{"content":[{"type":"image"}]}]}`:     true,
		`{"messages":[{"content":"hello there"}]}`:          false,
	}
	for body, want := range cases {
		if got := HasImageMarker([]byte(body)); got != want {
			t.Errorf("HasImageMarker(%q) = %v, want %v", body, got, want)
		}
	}
}

func TestDownscaleDisabledIsNoop(t *testing.T) {
	body := openAIReq(t, dataURI("image/jpeg", solidJPEG(t, 2000, 1000)))
	got := Downscale(body, "openai", 0)
	if &got[0] != &body[0] {
		t.Error("maxPx<=0 must return the exact same slice, not a copy")
	}
}

func TestDownscaleNoMarkerIsNoop(t *testing.T) {
	body := []byte(`{"model":"coding","messages":[{"role":"user","content":"just text"}]}`)
	got := Downscale(body, "openai", 512)
	if &got[0] != &body[0] {
		t.Error("a request with no image marker must return the exact same slice")
	}
}

func TestOpenAIImageAboveThresholdIsResized(t *testing.T) {
	body := openAIReq(t, dataURI("image/jpeg", solidJPEG(t, 2000, 1000)))
	out := Downscale(body, "openai", 512)
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
	out := Downscale(body, "openai", 512)
	if !bytes.Equal(out, body) {
		t.Error("an image already within the pixel cap must not be rewritten")
	}
}

func TestOpenAIRemoteURLNotFetched(t *testing.T) {
	body := openAIReq(t, "https://example.com/some-huge-photo.jpg")
	out := Downscale(body, "openai", 512)
	if !bytes.Equal(out, body) {
		t.Error("remote image URLs must never be fetched or rewritten")
	}
}

func TestAnthropicImageAboveThresholdIsResizedAndFlattened(t *testing.T) {
	body := anthropicReq(t, "image/png", transparentPNG(t, 1200, 1200))
	out := Downscale(body, "anthropic", 512)
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
	out := Downscale(body, "anthropic", 512)
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
	out := Downscale(body, "anthropic", 512)
	if !bytes.Equal(out, body) {
		t.Error("url-sourced Anthropic images must never be fetched or rewritten")
	}
}

func TestAnimatedGIFUntouched(t *testing.T) {
	body := openAIReq(t, dataURI("image/gif", animatedGIF(t, 1000, 1000)))
	out := Downscale(body, "openai", 512)
	if !bytes.Equal(out, body) {
		t.Error("animated GIFs must be left untouched, not collapsed to a still frame")
	}
}

func TestCorruptImageDataFailsOpen(t *testing.T) {
	body := openAIReq(t, dataURI("image/jpeg", []byte("not actually an image")))
	out := Downscale(body, "openai", 512)
	if !bytes.Equal(out, body) {
		t.Error("corrupt image data must fail open (leave request unchanged), not error out")
	}
}

func TestMalformedRequestBodyFailsOpen(t *testing.T) {
	// Has the marker substring but isn't shaped like a real chat request at all.
	body := []byte(`{"image_url_but_not_json_object`)
	out := Downscale(body, "openai", 512)
	if !bytes.Equal(out, body) {
		t.Error("malformed JSON must fail open and return the input unchanged")
	}
}

func TestDecompressionBombGuard(t *testing.T) {
	// 30000x30000 = 900M declared pixels, far past maxDecodePixels, but the
	// "file" itself is a few dozen bytes — DecodeConfig reads only IHDR.
	huge := fakePNGHeader(t, 30000, 30000)
	out, mime, changed, err := processImage(huge, 512)
	if err != nil || changed || out != nil || mime != "" {
		t.Errorf("declared-oversized image must be left alone: changed=%v err=%v", changed, err)
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
	out := Downscale(body, "openai", 512)
	if !bytes.Equal(out, body) {
		t.Error("a marker false-positive with no actual image block must leave the body unchanged")
	}
}

func TestUnsupportedProtocolIsNoop(t *testing.T) {
	body := openAIReq(t, dataURI("image/jpeg", solidJPEG(t, 2000, 1000)))
	out := Downscale(body, "carrier-pigeon", 512)
	if !bytes.Equal(out, body) {
		t.Error("an unrecognized protocol must not attempt any block rewrite")
	}
}
