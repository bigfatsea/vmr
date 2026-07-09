// Ver 2026-07-09 00:00, by Sonnet 5

// Package imgprep optionally downscales inline base64 image attachments in
// request bodies before they reach the router, to cut vision-token cost on
// oversized screenshots/photos. It never fetches remote image URLs and never
// touches response bodies — only inline (data-URI / base64-source) images in
// the request are candidates.
//
// Disabled unless a positive max pixel side is configured. Detection is a
// cheap substring scan so text-only requests (the overwhelming majority)
// pay no JSON-parsing cost. Any parse/decode failure anywhere in the path
// falls back to the original bytes unchanged (fail-open): a bug here must
// never turn an otherwise-good request into a failure.
//
// An optional on-disk cache (cache.go) lets a source image that has already
// been downscaled to a given target size be reused byte-for-byte on a later
// request, instead of being re-decoded/re-scaled/re-encoded. This matters
// for two reasons: it avoids repeating CPU work when an agent conversation
// resends the same screenshot every turn, and it keeps the re-encoded bytes
// identical across requests — upstream prompt caches are keyed on exact
// byte/token match, and a general-purpose JPEG encoder is not guaranteed to
// produce identical output on every run, so skipping re-encoding entirely is
// the only way to guarantee the upstream cache never silently misses.
package imgprep

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	_ "image/png"
	"strings"
	"time"

	"vmr/internal/core"

	"golang.org/x/image/draw"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

// maxDecodePixels guards against decompression bombs: a malformed or
// adversarial image can declare huge dimensions while its encoded bytes stay
// tiny (e.g. a flat-color PNG). Anything above this is left untouched rather
// than decoded into memory.
const maxDecodePixels = 64_000_000 // ~64MP, comfortably above any real screenshot/photo

// jpegQuality is fixed rather than configurable: the feature's whole point is
// "good enough to read, small enough to be cheap" — a quality knob would be a
// second parameter nobody asked for.
const jpegQuality = 85

// Options bundles the per-request knobs Downscale needs. CacheDir/CacheTTLDays
// are process-wide config values (from config.Config), threaded through
// explicitly rather than held as package-level mutable state: Downscale is
// called once per chat request, and the caller (server.chatHandler) already
// has the resolved config snapshot in hand, so there's no singleton to wire
// up and tests can point CacheDir at a t.TempDir() without touching global
// state shared across other tests.
type Options struct {
	MaxPx int // longer-side pixel cap; <=0 disables downscaling entirely

	// CacheDir, if non-empty, enables the on-disk downscale cache (cache.go):
	// a source image already downscaled to MaxPx is reused instead of being
	// reprocessed. Empty disables caching — every request is processed fresh.
	CacheDir string
	// CacheTTLDays bounds how long an unused cache entry survives (evicted by
	// last-use mtime, refreshed on every hit). Only consulted when CacheDir
	// is set; <=0 disables the eviction sweep (entries are kept forever).
	CacheTTLDays int
}

// HasImageMarker is a cheap pre-check so non-image requests (the large
// majority) never pay for JSON parsing. False positives (matching "images"
// or similar) just fall through to a parse that finds nothing to rewrite.
func HasImageMarker(body []byte) bool {
	return bytes.Contains(body, []byte(`"image`))
}

// Downscale rewrites inline base64 images in body whose longer side exceeds
// opts.MaxPx, re-encoding them as JPEG scaled to fit within opts.MaxPx.
// protocol selects which content-block shape to look for ("openai":
// content[].image_url.url data URI; "anthropic": content[].source with type
// "base64"). opts.MaxPx<=0 disables the feature. On any failure, or when
// nothing needed resizing, the original body is returned unchanged (same
// backing array).
func Downscale(body []byte, protocol string, opts Options) (result []byte) {
	result = body
	if opts.MaxPx <= 0 || !HasImageMarker(body) {
		return
	}
	// Defensive belt on top of careful error handling below: this function
	// processes attacker-influenced image bytes through decoders that were
	// not written with adversarial input in mind. A panic here must not take
	// down the request.
	defer func() {
		if recover() != nil {
			result = body
		}
	}()
	rewritten, changed, err := rewriteBody(body, protocol, opts)
	if err != nil || !changed {
		return
	}
	result = rewritten
	return
}

func rewriteBody(body []byte, protocol string, opts Options) ([]byte, bool, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, false, nil
	}
	rawMsgs, ok := top["messages"]
	if !ok {
		return nil, false, nil
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(rawMsgs, &msgs); err != nil {
		return nil, false, nil
	}
	changed := false
	for i, m := range msgs {
		nm, mChanged, err := rewriteMessage(m, protocol, opts)
		if err != nil || !mChanged {
			continue
		}
		msgs[i] = nm
		changed = true
	}
	if !changed {
		return nil, false, nil
	}
	newMsgs, err := core.MarshalNoEscape(msgs)
	if err != nil {
		return nil, false, err
	}
	top["messages"] = newMsgs
	out, err := core.MarshalNoEscape(top)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func rewriteMessage(raw json.RawMessage, protocol string, opts Options) (json.RawMessage, bool, error) {
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return raw, false, nil
	}
	rawContent, ok := msg["content"]
	if !ok {
		return raw, false, nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(rawContent, &blocks); err != nil {
		return raw, false, nil // content is a plain string: no image blocks possible
	}
	changed := false
	for i, b := range blocks {
		nb, bChanged, err := rewriteBlock(b, protocol, opts)
		if err != nil || !bChanged {
			continue
		}
		blocks[i] = nb
		changed = true
	}
	if !changed {
		return raw, false, nil
	}
	newContent, err := core.MarshalNoEscape(blocks)
	if err != nil {
		return raw, false, err
	}
	msg["content"] = newContent
	out, err := core.MarshalNoEscape(msg)
	if err != nil {
		return raw, false, err
	}
	return out, true, nil
}

func rewriteBlock(raw json.RawMessage, protocol string, opts Options) (json.RawMessage, bool, error) {
	var block map[string]json.RawMessage
	if err := json.Unmarshal(raw, &block); err != nil {
		return raw, false, nil
	}
	var typ string
	if err := json.Unmarshal(block["type"], &typ); err != nil {
		return raw, false, nil
	}
	switch {
	case protocol == "openai" && typ == "image_url":
		return rewriteOpenAIImage(raw, block, opts)
	case protocol == "anthropic" && typ == "image":
		return rewriteAnthropicImage(raw, block, opts)
	default:
		return raw, false, nil
	}
}

func rewriteOpenAIImage(raw json.RawMessage, block map[string]json.RawMessage, opts Options) (json.RawMessage, bool, error) {
	var iu map[string]json.RawMessage
	if err := json.Unmarshal(block["image_url"], &iu); err != nil {
		return raw, false, nil
	}
	var url string
	if err := json.Unmarshal(iu["url"], &url); err != nil {
		return raw, false, nil
	}
	data, ok := parseDataURI(url)
	if !ok {
		return raw, false, nil // remote URL: vmr never fetches it
	}
	newData, newMime, changed, err := processImage(data, opts)
	if err != nil || !changed {
		return raw, false, nil
	}
	newURL := "data:" + newMime + ";base64," + base64.StdEncoding.EncodeToString(newData)
	uv, err := core.MarshalNoEscape(newURL)
	if err != nil {
		return raw, false, err
	}
	iu["url"] = uv
	ib, err := core.MarshalNoEscape(iu)
	if err != nil {
		return raw, false, err
	}
	block["image_url"] = ib
	out, err := core.MarshalNoEscape(block)
	if err != nil {
		return raw, false, err
	}
	return out, true, nil
}

func rewriteAnthropicImage(raw json.RawMessage, block map[string]json.RawMessage, opts Options) (json.RawMessage, bool, error) {
	var src map[string]json.RawMessage
	if err := json.Unmarshal(block["source"], &src); err != nil {
		return raw, false, nil
	}
	var srcType string
	if err := json.Unmarshal(src["type"], &srcType); err != nil || srcType != "base64" {
		return raw, false, nil // url-sourced image, or shape we don't recognize
	}
	var b64 string
	if err := json.Unmarshal(src["data"], &b64); err != nil {
		return raw, false, nil
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return raw, false, nil
	}
	newData, newMime, changed, err := processImage(data, opts)
	if err != nil || !changed {
		return raw, false, nil
	}
	mv, err := core.MarshalNoEscape(newMime)
	if err != nil {
		return raw, false, err
	}
	dv, err := core.MarshalNoEscape(base64.StdEncoding.EncodeToString(newData))
	if err != nil {
		return raw, false, err
	}
	src["media_type"] = mv
	src["data"] = dv
	sb, err := core.MarshalNoEscape(src)
	if err != nil {
		return raw, false, err
	}
	block["source"] = sb
	out, err := core.MarshalNoEscape(block)
	if err != nil {
		return raw, false, err
	}
	return out, true, nil
}

// parseDataURI extracts the decoded payload of a "data:<mime>;base64,<data>"
// URI. Any other scheme (http/https, or malformed data URIs) is reported as
// not-ok — vmr only ever touches bytes the client already sent it.
func parseDataURI(u string) ([]byte, bool) {
	if !strings.HasPrefix(u, "data:") {
		return nil, false
	}
	const marker = ";base64,"
	idx := strings.Index(u, marker)
	if idx < 0 {
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(u[idx+len(marker):])
	if err != nil {
		return nil, false
	}
	return data, true
}

// processImage decodes data (format auto-detected from content, not from any
// caller-supplied mime type), and if its longer side exceeds opts.MaxPx,
// resizes it to fit and re-encodes as JPEG. changed=false (with a nil error)
// covers every "leave it alone" case: already small enough, animated (GIF
// with more than one frame — resizing would collapse it to a still),
// unrecognized format, corrupt data, or oversized declared dimensions
// (decompression-bomb guard). Output is always JPEG; alpha is flattened onto
// white first since JPEG has no transparency.
//
// When opts.CacheDir is set, a cache lookup happens only after the
// need-to-process decision is made (longSide > MaxPx, not a decompression
// bomb) — most requests don't need any processing at all, and hashing the
// full image on every request would tax exactly the common case caching is
// meant to help least. A hit skips decode/scale/encode entirely; a miss
// falls through to full processing and stores the result before returning.
func processImage(data []byte, opts Options) (out []byte, mime string, changed bool, err error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", false, nil
	}
	longSide := cfg.Width
	if cfg.Height > longSide {
		longSide = cfg.Height
	}
	if longSide <= opts.MaxPx {
		return nil, "", false, nil
	}
	if cfg.Width*cfg.Height > maxDecodePixels {
		return nil, "", false, nil
	}

	var hash [32]byte
	if opts.CacheDir != "" {
		hash = sha256.Sum256(data)
		maybeSweepCache(opts.CacheDir, opts.CacheTTLDays, time.Now())
		if cached, ok := cacheLookup(opts.CacheDir, hash, opts.MaxPx); ok {
			return cached, "image/jpeg", true, nil
		}
	}

	var src image.Image
	if format == "gif" {
		g, gerr := gif.DecodeAll(bytes.NewReader(data))
		if gerr != nil {
			return nil, "", false, nil
		}
		if len(g.Image) != 1 {
			return nil, "", false, nil // animated: resizing would destroy the animation
		}
		src = g.Image[0]
	} else {
		src, _, err = image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, "", false, nil
		}
	}

	sb := src.Bounds()
	newW, newH := scaledSize(sb.Dx(), sb.Dy(), opts.MaxPx)

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.BiLinear.Scale(dst, dst.Bounds(), src, sb, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, "", false, nil
	}
	result := buf.Bytes()
	if opts.CacheDir != "" {
		cacheStore(opts.CacheDir, hash, opts.MaxPx, result)
	}
	return result, "image/jpeg", true, nil
}

// scaledSize returns the largest w×h with the same aspect ratio as the
// source such that the longer side equals maxPx.
func scaledSize(w, h, maxPx int) (int, int) {
	if w >= h {
		newH := h * maxPx / w
		if newH < 1 {
			newH = 1
		}
		return maxPx, newH
	}
	newW := w * maxPx / h
	if newW < 1 {
		newW = 1
	}
	return newW, maxPx
}
