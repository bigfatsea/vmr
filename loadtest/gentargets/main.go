// Ver 2026-07-16 00:00, by Sonnet 5

// gentargets writes loadtest/targets.json — one Vegeta attack target per
// scenario (see docs/PerformanceTesting_Design_Sonnet5.md §4). Not checked
// in (it embeds several generated images, sized to be real payloads rather
// than repo-bloating fixtures) — regenerate on demand:
//
//	go run ./loadtest/gentargets
//
// Not part of the shipped vmr binary — `go build ./cmd/vmr` never touches
// this directory.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"os"
)

// vmrAddr must match loadtest/config.yaml's `listen`.
const vmrAddr = "http://127.0.0.1:8801"

type target struct {
	Method string              `json:"method"`
	URL    string              `json:"url"`
	Header map[string][]string `json:"header"`
	Body   string              `json:"body"` // base64 of the raw HTTP request body
}

type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}
type reqBody struct {
	Model     string    `json:"model"`
	Stream    bool      `json:"stream"`
	MaxTokens int       `json:"max_tokens,omitempty"` // Anthropic requires this; omitted (0) is fine for OpenAI-shaped scenarios
	Messages  []message `json:"messages"`
}

// scenario pairs a request body with the ingress path it must hit —
// everything is OpenAI-protocol (/v1/chat/completions) except
// anthropic_baseline.
type scenario struct {
	path string
	body reqBody
}

func main() {
	scenarios := map[string]scenario{
		"baseline": {"/v1/chat/completions", reqBody{
			Model: "baseline", Stream: false,
			Messages: []message{{Role: "user", Content: "hi"}},
		}},
		"stream_normal": {"/v1/chat/completions", reqBody{
			Model: "stream_normal", Stream: true,
			Messages: []message{{Role: "user", Content: "tell me a short story"}},
		}},
		"thinking_leak": {"/v1/chat/completions", reqBody{
			Model: "thinking_leak", Stream: true,
			Messages: []message{{Role: "user", Content: "think step by step, then answer"}},
		}},
		"think_tag": {"/v1/chat/completions", reqBody{
			Model: "think_tag", Stream: true,
			Messages: []message{{Role: "user", Content: "think it through, then answer"}},
		}},
		"big_response": {"/v1/chat/completions", reqBody{
			Model: "big_response", Stream: false,
			Messages: []message{{Role: "user", Content: "write a very long answer"}},
		}},
		"big_image": {"/v1/chat/completions", reqBody{
			Model: "big_image", Stream: false,
			Messages: []message{{Role: "user", Content: []map[string]any{
				{"type": "text", "text": "describe this screenshot"},
				{"type": "image_url", "image_url": map[string]string{"url": solidJPEGDataURI(3000, 2000)}},
			}}},
		}},
		"multi_image": {"/v1/chat/completions", reqBody{
			Model: "multi_image", Stream: false,
			Messages: []message{{Role: "user", Content: []map[string]any{
				{"type": "text", "text": "compare these screenshots"},
				{"type": "image_url", "image_url": map[string]string{"url": solidJPEGDataURI(400, 300)}},   // under any realistic cap: detection only
				{"type": "image_url", "image_url": map[string]string{"url": solidJPEGDataURI(2000, 1400)}}, // over cap: triggers downscale
				{"type": "image_url", "image_url": map[string]string{"url": solidJPEGDataURI(400, 300)}},   // under cap again
			}}},
		}},
		"gif": {"/v1/chat/completions", reqBody{
			Model: "gif", Stream: false,
			Messages: []message{{Role: "user", Content: []map[string]any{
				{"type": "text", "text": "what's in this gif"},
				{"type": "image_url", "image_url": map[string]string{"url": animatedGIFDataURI(800, 600)}},
			}}},
		}},
		"long_history": {"/v1/chat/completions", reqBody{
			Model: "long_history", Stream: false,
			Messages: longHistory(40),
		}},
		"failover": {"/v1/chat/completions", reqBody{
			Model: "failover", Stream: false,
			Messages: []message{{Role: "user", Content: "hi"}},
		}},
		"anthropic_baseline": {"/v1/messages", reqBody{
			Model: "anthropic_baseline", Stream: false, MaxTokens: 64,
			Messages: []message{{Role: "user", Content: "hi"}},
		}},
	}

	out, err := os.Create("loadtest/targets.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gentargets:", err)
		os.Exit(1)
	}
	defer out.Close()

	// Deterministic order so a diff of two generated files is meaningful.
	order := []string{
		"baseline", "stream_normal", "thinking_leak", "think_tag", "big_response",
		"big_image", "multi_image", "gif", "long_history", "failover", "anthropic_baseline",
	}
	for _, name := range order {
		s := scenarios[name]
		body, err := json.Marshal(s.body)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gentargets:", name, err)
			os.Exit(1)
		}
		t := target{
			Method: "POST",
			URL:    vmrAddr + s.path,
			Header: map[string][]string{"Content-Type": {"application/json"}},
			Body:   base64.StdEncoding.EncodeToString(body),
		}
		line, _ := json.Marshal(t)
		out.Write(line)
		out.Write([]byte("\n"))
	}
	fmt.Fprintf(os.Stderr, "wrote loadtest/targets.json (%d scenarios)\n", len(order))
}

// solidJPEGDataURI synthesizes a wxh JPEG. Used at 3000x2000 (well over any
// realistic image_downscale cap, for big_image) and at smaller under/over-cap
// sizes for multi_image.
func solidJPEGDataURI(w, h int) string {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 255), uint8(y % 255), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		panic(err)
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// animatedGIFDataURI synthesizes a wxh, 3-frame GIF — imgprep never
// rescales GIFs at all now (single-frame or animated, see
// internal/imgprep/imgprep.go), so this scenario is about confirming that
// fast skip path stays cheap under load, not about exercising a decode.
func animatedGIFDataURI(w, h int) string {
	pal := []color.Color{color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}, color.RGBA{0, 0, 255, 255}}
	frames := make([]*image.Paletted, 3)
	delays := make([]int, 3)
	for i := range frames {
		frames[i] = image.NewPaletted(image.Rect(0, 0, w, h), pal)
		delays[i] = 10
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{Image: frames, Delay: delays}); err != nil {
		panic(err)
	}
	return "data:image/gif;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// longHistory simulates an agent resending its full conversation every
// turn — the request-size regime the design doc's §4 "long_history" row
// targets (audit full-body write, model-field splice, image-marker scan,
// all at a bigger-than-baseline body size).
func longHistory(turns int) []message {
	msgs := make([]message, 0, turns*2)
	for i := 0; i < turns; i++ {
		msgs = append(msgs,
			message{Role: "user", Content: fmt.Sprintf("Turn %d: please continue working on the task, checking the previous steps and moving forward with the next action in the plan.", i)},
			message{Role: "assistant", Content: fmt.Sprintf("Turn %d: acknowledged, proceeding with the next step as described, no issues found in the prior output.", i)},
		)
	}
	return msgs
}
