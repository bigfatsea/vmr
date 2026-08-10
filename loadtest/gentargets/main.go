// Ver 2026-07-24 13:15, by Sonnet 5

// gentargets writes loadtest/targets.json — one Vegeta attack target per
// scenario (see docs/VirtualModelRouter_Design_v4_Core.md §12) — plus two
// subset files, targets-plain.json and targets-image.json, split by
// whether the scenario exercises image downscaling. Image decode/scale/
// encode is by far the most expensive code path vmr has;
// mixed into one combined percentile figure it silently drags up the
// p95/p99/max for every *other*, genuinely-cheap scenario. runner.go fires
// the two subsets as separate Vegeta attacks so the client-side report
// shows "plain request" and "image request" latency as what they actually
// are — two different cost regimes — instead of one blended number. Not
// checked in (embeds several generated images, sized to be real payloads
// rather than repo-bloating fixtures) — regenerate on demand:
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

	"vmr/loadtest/addr"
)

// vmrAddr must match loadtest/config.yaml's `listen`.
const vmrAddr = "http://" + addr.VMR

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
	Messages  []message `json:"messages,omitempty"`   // Chat Completions / Anthropic Messages shape
	Input     string    `json:"input,omitempty"`      // Responses shape (top-level "input", no "messages" at all) — responses_baseline only
}

// scenario pairs a request body with the ingress path it must hit —
// everything is OpenAI-protocol (/v1/chat/completions) except
// anthropic_baseline (/v1/messages) and responses_baseline (/v1/responses).
type scenario struct {
	path string
	body reqBody
}

// imageScenarios marks the scenarios that go in the "image" bucket rather
// than "plain" — the split is by cost regime (image processing vs not),
// not by request size, so large-but-non-image payloads like long_history/
// big_response stay in "plain". big_image/multi_image are handled as their
// own switch cases below (they need cacheBustVariants copies each, not one
// line) and never consult this map; only gif reaches it, one fixed line
// (see cacheBustVariants' doc comment for why gif doesn't need variants).
var imageScenarios = map[string]bool{
	"gif": true,
}

// cacheBustVariants is how many distinct copies of the over-cap image
// big_image/multi_image each generate, instead of one fixed image reused
// for the whole run. internal/imgprep's on-disk downscale cache is keyed
// by content hash + MaxPx, so a single fixed image is a cache MISS
// (real decode+scale+encode — the expensive work these two scenarios
// exist to measure) exactly once across the entire run,
// then a HIT for every request after that, for as long as this cache
// entry survives (it isn't cleared between runs the way logs/reports
// are). That's not a realistic hit rate, and it isn't just imprecise —
// it also made whichever load round happened to run first eat that one
// real miss inside a much smaller sample, dragging its own percentiles
// up relative to later rounds that only ever saw hits (an ordering
// artifact, not a real regression: see the git history around this
// comment for the load-test run that first surfaced it).
//
// A pool of distinct variants generated once here, up front — never
// during the attack itself, which would perturb request timing/density —
// fixes the worst of it: the light round (30 image-group requests split
// 3 ways, ~10 per scenario) never repeats a variant at all, so it can no
// longer be singled out to eat the one real miss inside a much smaller
// sample than moderate/heavy get. Every round also keeps a real, visible
// tail of genuine cache misses (not just a one-off): with 50 variants
// cycling in order, roughly the first 50 requests to any one scenario in
// a round are fresh, and only requests beyond that start repeating —
// enough that p95/p99/max (not just the mean) in every round's own
// report still reflect real decode+scale+encode cost, which is the
// entire reason these scenarios exist.
//
// 50 (not some far larger number closer to a full run's ~380 total
// big_image requests) is a deliberate size-vs-realism tradeoff: each
// 3000x2000 variant encodes to ~470KB, ~220KB for multi_image's
// over-cap image, written into both targets.json and targets-image.json
// — at 400 variants that was 528MB and a 60s gentargets run; at 50 it's
// under a tenth of that, seconds not a minute, for a one-off local tool
// that isn't wired into CI. This does mean the heavy round (the largest
// sample) still sees mostly-cached requests in aggregate — accepted,
// because catching a regression in the expensive path only needs it to
// show up somewhere in the tail of every round, not to dominate the
// median of the busiest one. Real traffic's hit rate sits somewhere
// between "fixed image, ~100% hit" and "unique image every time, ~0%
// hit" anyway (an agent conversation resending the same screenshot every
// turn is itself a real cache-hit pattern this cache is built for) — this
// isn't trying to model that distribution precisely, just to stop a
// single fixed image from hiding the expensive path almost entirely.
//
// gif's own IMAGE stays fixed (imgprep never rescales GIFs at all, animated
// or not, so there's no cache to defeat there — it's specifically testing
// that the never-rescale fast path stays cheap) — but it still gets
// written cacheBustVariants times over, see the "gif" case below for why:
// Vegeta round-robins a targets file by line count, not by scenario
// identity, so leaving gif at a single line while big_image/multi_image
// each get cacheBustVariants would starve it of its intended 1/3 share of
// the "image" attack's traffic.
const cacheBustVariants = 50

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
		"responses_baseline": {"/v1/responses", reqBody{
			Model: "responses_baseline", Stream: false,
			Input: "hi",
		}},
	}

	// big_image/multi_image are handled separately from the single-body
	// map above: each contributes cacheBustVariants distinct target lines
	// (same scenario/model name, different image bytes) instead of one —
	// see cacheBustVariants' doc comment for why.
	var bigImageVariants, multiImageVariants []reqBody
	for i := 0; i < cacheBustVariants; i++ {
		bigImageVariants = append(bigImageVariants, reqBody{
			Model: "big_image", Stream: false,
			Messages: []message{{Role: "user", Content: []map[string]any{
				{"type": "text", "text": "describe this screenshot"},
				{"type": "image_url", "image_url": map[string]string{"url": solidJPEGDataURI(3000, 2000, i)}},
			}}},
		})
		multiImageVariants = append(multiImageVariants, reqBody{
			Model: "multi_image", Stream: false,
			Messages: []message{{Role: "user", Content: []map[string]any{
				{"type": "text", "text": "compare these screenshots"},
				{"type": "image_url", "image_url": map[string]string{"url": solidJPEGDataURI(400, 300, 0)}},   // under any realistic cap: detection only, fixed (never cached either way)
				{"type": "image_url", "image_url": map[string]string{"url": solidJPEGDataURI(2000, 1400, i)}}, // over cap: triggers downscale — this is the one that needs variety
				{"type": "image_url", "image_url": map[string]string{"url": solidJPEGDataURI(400, 300, 0)}},   // under cap again, fixed
			}}},
		})
	}

	all, err := os.Create("loadtest/targets.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gentargets:", err)
		os.Exit(1)
	}
	defer all.Close()
	plain, err := os.Create("loadtest/targets-plain.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gentargets:", err)
		os.Exit(1)
	}
	defer plain.Close()
	image, err := os.Create("loadtest/targets-image.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gentargets:", err)
		os.Exit(1)
	}
	defer image.Close()

	// writeLine marshals one target line and appends it to every dst file —
	// computed once, written to as many destinations as apply (the combined
	// file plus its plain/image subset) so a scenario with many variants
	// (big_image/multi_image) never re-marshals the same body twice.
	writeLine := func(path string, body reqBody, dsts ...*os.File) {
		b, err := json.Marshal(body)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gentargets:", body.Model, err)
			os.Exit(1)
		}
		t := target{
			Method: "POST",
			URL:    vmrAddr + path,
			Header: map[string][]string{"Content-Type": {"application/json"}},
			Body:   base64.StdEncoding.EncodeToString(b),
		}
		line, _ := json.Marshal(t)
		line = append(line, '\n')
		for _, f := range dsts {
			f.Write(line)
		}
	}

	// Deterministic order so a diff of two generated files is meaningful.
	order := []string{
		"baseline", "stream_normal", "thinking_leak", "think_tag", "big_response",
		"big_image", "multi_image", "gif", "long_history", "failover", "anthropic_baseline",
		"responses_baseline",
	}
	var plainCount, imageCount, totalLines int
	for _, name := range order {
		switch name {
		case "big_image":
			for _, body := range bigImageVariants {
				writeLine("/v1/chat/completions", body, all, image)
			}
			imageCount++
			totalLines += len(bigImageVariants)
		case "multi_image":
			for _, body := range multiImageVariants {
				writeLine("/v1/chat/completions", body, all, image)
			}
			imageCount++
			totalLines += len(multiImageVariants)
		default:
			s := scenarios[name]
			if imageScenarios[name] { // gif
				// Vegeta round-robins through a targets file by LINE, not by
				// scenario identity — confirmed empirically, not assumed: an
				// earlier version of this file gave gif only 1 line against
				// big_image/multi_image's cacheBustVariants each, and a real
				// run showed gif getting ~1% of the image group's traffic
				// instead of its intended 1/3 share (big_image/multi_image
				// drowned it out purely by line count). gif doesn't need
				// distinct images — imgprep never rescales GIFs, so there's
				// no cache to defeat, and repeating the exact same one
				// cacheBustVariants times changes nothing about what this
				// scenario measures — but it does need the same LINE COUNT
				// as its two siblings to get its fair share of the "image"
				// attack's rate again.
				for i := 0; i < cacheBustVariants; i++ {
					writeLine(s.path, s.body, all, image)
				}
				imageCount++
				totalLines += cacheBustVariants
			} else {
				writeLine(s.path, s.body, all, plain)
				plainCount++
				totalLines++
			}
		}
	}
	fmt.Fprintf(os.Stderr, "wrote loadtest/targets.json (%d scenarios, %d target lines — big_image/multi_image expand to %d cache-busting variants each), targets-plain.json (%d), targets-image.json (%d scenarios)\n",
		len(order), totalLines, cacheBustVariants, plainCount, imageCount)
}

// solidJPEGDataURI synthesizes a wxh JPEG. Used at 3000x2000 (well over any
// realistic image_downscale cap, for big_image) and at smaller under/over-cap
// sizes for multi_image.
// solidJPEGDataURI synthesizes a wxh JPEG. seed shifts the color formula so
// different seeds encode to different bytes (hence different content
// hashes) — see cacheBustVariants' doc comment for why that matters. Two
// calls with the same (w,h,seed) are byte-identical (deterministic, no
// randomness) — that's what makes a diff of two generated target files
// meaningful, and it's fine here: seed only ever varies across a single
// gentargets run's own variant pool, not across separate runs.
func solidJPEGDataURI(w, h, seed int) string {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8((x + seed) % 255), uint8((y + seed) % 255), 128, 255})
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
// turn — the request-size regime the design doc's "long_history" row
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
