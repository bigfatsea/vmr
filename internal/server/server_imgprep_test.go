// Ver 2026-07-07 15:30, by Fable 5

// End-to-end check that image downscaling (internal/imgprep) is wired into
// the real request path: client -> server -> router -> adapter -> upstream.
package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"vmr/internal/config"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

func bigJPEGDataURI(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	for y := 0; y < 900; y++ {
		for x := 0; x < 1600; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 255), uint8(y % 255), 60, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestImageDownscaleAppliedBeforeUpstream(t *testing.T) {
	var gotBody atomic.Value // []byte, the body the mock upstream actually received
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody.Store(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","choices":[]}`)
	}))
	defer up.Close()

	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
image_downscale: 512
providers:
  p1: {type: openai, base_url: %s, api_key: k1}
models:
  vm:
    endpoints:
      - {provider: p1, model: model-one, priority: 1}
`, up.URL)
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	ts := httptest.NewServer(New(rt, nil).Handler())
	defer ts.Close()

	uri := bigJPEGDataURI(t)
	reqBody := fmt.Sprintf(`{"model":"vm","messages":[{"role":"user","content":[
		{"type":"text","text":"describe this"},
		{"type":"image_url","image_url":{"url":%q}}
	]}]}`, uri)

	resp, body := chat(t, ts, reqBody, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}

	sentRaw, _ := gotBody.Load().([]byte)
	if len(sentRaw) == 0 {
		t.Fatal("upstream never received a request")
	}
	if len(sentRaw) >= len(reqBody) {
		t.Fatalf("expected downscaled outbound body to shrink: sent=%dB original=%dB", len(sentRaw), len(reqBody))
	}

	var sent struct {
		Messages []struct {
			Content []struct {
				Type     string `json:"type"`
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(sentRaw, &sent); err != nil {
		t.Fatalf("upstream body not valid JSON: %v", err)
	}
	outURL := sent.Messages[0].Content[1].ImageURL.URL
	const marker = ";base64,"
	idx := bytes.Index([]byte(outURL), []byte(marker))
	if idx < 0 {
		t.Fatalf("no base64 payload in outbound image_url: %s", outURL)
	}
	raw, err := base64.StdEncoding.DecodeString(outURL[idx+len(marker):])
	if err != nil {
		t.Fatalf("decode outbound base64: %v", err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode outbound image: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 512 || b.Dy() != 288 {
		t.Errorf("outbound image is %dx%d, want 512x288 (1600x900 scaled to 512 long side)", b.Dx(), b.Dy())
	}
}

func TestImageDownscaleDisabledByDefaultLeavesImageIntact(t *testing.T) {
	var gotBody atomic.Value
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody.Store(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","choices":[]}`)
	}))
	defer up.Close()

	// No image_downscale key at all: feature must be off (default 0).
	ts := newRouterServer(t, twoEndpointYAML(up.URL, up.URL, ""))

	uri := bigJPEGDataURI(t)
	reqBody := fmt.Sprintf(`{"model":"vm","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":%q}}]}]}`, uri)
	resp, body := chat(t, ts, reqBody, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	sentRaw, _ := gotBody.Load().([]byte)
	if len(sentRaw) == 0 {
		t.Fatal("upstream never received a request")
	}
	var sent struct {
		Messages []struct {
			Content []struct {
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(sentRaw, &sent); err != nil {
		t.Fatalf("upstream body not valid JSON: %v", err)
	}
	outURL := sent.Messages[0].Content[0].ImageURL.URL
	const marker = ";base64,"
	idx := bytes.Index([]byte(outURL), []byte(marker))
	if idx < 0 {
		t.Fatalf("no base64 payload in outbound image_url: %s", outURL)
	}
	raw, err := base64.StdEncoding.DecodeString(outURL[idx+len(marker):])
	if err != nil {
		t.Fatalf("decode outbound base64: %v", err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode outbound image: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 1600 || b.Dy() != 900 {
		t.Errorf("outbound image is %dx%d, want the untouched original 1600x900 (image_downscale unset)", b.Dx(), b.Dy())
	}
}
