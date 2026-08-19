// Ver 2026-07-30, by Sonnet 5

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
	"os"
	"path/filepath"
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
  - {name: p1, base_url: {openai: %s}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai, providers: [p1], models: [model-one]}
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

// outboundImageDims sends reqBody through ts and decodes the single
// image_url block the mock upstream actually received, returning its pixel
// dimensions. Shared by the per-model-override and cache wiring tests below.
func outboundImageDims(t *testing.T, ts *httptest.Server, gotBody *atomic.Value, reqBody string) (w, h int) {
	t.Helper()
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
	return b.Dx(), b.Dy()
}

func mockUpstream(t *testing.T, gotBody *atomic.Value) *httptest.Server {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody.Store(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","choices":[]}`)
	}))
	t.Cleanup(up.Close)
	return up
}

// TestImageDownscalePerModelOverrideWinsOverGlobal: "vm" has no override and
// inherits the 1024px global cap; "vm-small" overrides to 256px. Both share
// the same upstream and source image — only the virtual model differs.
func TestImageDownscalePerModelOverrideWinsOverGlobal(t *testing.T) {
	var gotBody atomic.Value
	up := mockUpstream(t, &gotBody)

	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
image_downscale: 1024
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
models:
  vm:
    endpoints: [{protocol: openai, providers: [p1], models: [model-one]}]
  vm-small:
    image_downscale: 256
    endpoints: [{protocol: openai, providers: [p1], models: [model-one]}]
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

	uri := bigJPEGDataURI(t) // 1600x900
	reqFor := func(model string) string {
		return fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":[
			{"type":"text","text":"describe this"},
			{"type":"image_url","image_url":{"url":%q}}
		]}]}`, model, uri)
	}

	w, h := outboundImageDims(t, ts, &gotBody, reqFor("vm"))
	if w != 1024 || h != 576 {
		t.Errorf("vm (no override, inherits global 1024px): got %dx%d, want 1024x576", w, h)
	}

	w, h = outboundImageDims(t, ts, &gotBody, reqFor("vm-small"))
	if w != 256 || h != 144 {
		t.Errorf("vm-small (override 256px): got %dx%d, want 256x144", w, h)
	}
}

// TestImageDownscaleModelOverrideCanForceDisable: a model can set
// image_downscale: 0 to opt out even though the global default is on.
func TestImageDownscaleModelOverrideCanForceDisable(t *testing.T) {
	var gotBody atomic.Value
	up := mockUpstream(t, &gotBody)

	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
image_downscale: 512
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
models:
  vm-off:
    image_downscale: 0
    endpoints: [{protocol: openai, providers: [p1], models: [model-one]}]
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

	uri := bigJPEGDataURI(t) // 1600x900
	reqBody := fmt.Sprintf(`{"model":"vm-off","messages":[{"role":"user","content":[
		{"type":"text","text":"describe this"},
		{"type":"image_url","image_url":{"url":%q}}
	]}]}`, uri)

	w, h := outboundImageDims(t, ts, &gotBody, reqBody)
	if w != 1600 || h != 900 {
		t.Errorf("vm-off (image_downscale: 0 overrides global 512): got %dx%d, want untouched 1600x900", w, h)
	}
}

// TestImageDownscaleCacheWiredEndToEnd confirms the full path — config ->
// snapshot -> chatHandler -> imgprep.Downscale — actually uses the on-disk
// cache: it primes the cache with a first request, replaces the resulting
// cache file with a distinguishable "poisoned" image, then confirms a
// second identical request returns the poisoned bytes instead of a freshly
// recomputed downscale. imgprep's own tests already cover cache correctness
// in isolation; this test only proves the wiring (CacheDir/CacheTTLDays
// reaching imgprep.Options from server.go) is not silently broken.
func TestImageDownscaleCacheWiredEndToEnd(t *testing.T) {
	var gotBody atomic.Value
	up := mockUpstream(t, &gotBody)

	// An explicit image_cache_dir is used exactly as given, no subdir
	// appended — unlike the unset-default, which does add one.
	cacheDir := t.TempDir()

	ts := newRouterServer(t, fmt.Sprintf(`
listen: 127.0.0.1:0
image_downscale: 512
image_cache_dir: %s
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
models:
  vm:
    endpoints: [{protocol: openai, providers: [p1], models: [model-one]}]
`, cacheDir, up.URL))

	uri := bigJPEGDataURI(t) // 1600x900
	reqBody := fmt.Sprintf(`{"model":"vm","messages":[{"role":"user","content":[
		{"type":"text","text":"describe this"},
		{"type":"image_url","image_url":{"url":%q}}
	]}]}`, uri)

	w, h := outboundImageDims(t, ts, &gotBody, reqBody)
	if w != 512 || h != 288 {
		t.Fatalf("first request not downscaled as expected: %dx%d", w, h)
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one cache file under %s, got entries=%v err=%v", cacheDir, entries, err)
	}
	cachePath := filepath.Join(cacheDir, entries[0].Name())

	poison := new(bytes.Buffer)
	poisonImg := image.NewRGBA(image.Rect(0, 0, 32, 32))
	if err := jpeg.Encode(poison, poisonImg, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, poison.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	w, h = outboundImageDims(t, ts, &gotBody, reqBody)
	if w != 32 || h != 32 {
		t.Errorf("second identical request should have served the poisoned cache entry: got %dx%d, want 32x32 (proves the cache wiring reused the on-disk entry instead of reprocessing)", w, h)
	}
}
