// Ver 2026-07-24 12:05, by Sonnet 5

// Package imgprep detects inline image attachments in request bodies and,
// when configured, downscales the oversized ones before they reach the
// router — to cut vision-token cost on large screenshots/photos. It never
// fetches remote image URLs and never touches response bodies — only inline
// (data-URI / base64-source) images in the request are candidates.
//
// Detection (format/dimensions/byte size) always runs when the request has
// an image, regardless of whether downscaling is configured — audit metadata
// shouldn't depend on a resize feature being turned on. Only the actual
// decode/scale/re-encode/cache path is gated on a positive max pixel side;
// detection itself is a cheap header-only image.DecodeConfig read (no pixel
// decode), and requests with no image marker at all (the overwhelming
// majority) pay no JSON-parsing cost either way. Any parse/decode failure
// anywhere in the path falls back to the original bytes unchanged
// (fail-open): a bug here must never turn an otherwise-good request into a
// failure.
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
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
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
	MaxPx int // longer-side pixel cap; <=0 disables resizing (images are still detected and described, just never rewritten)

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
//
// The second check exists for Responses' input_image blocks specifically:
// the common inline-data shape ({"type":"input_image","image_url":"data:..."})
// is already caught by the first check via the "image_url" key itself, but a
// Files-API-referenced block ({"type":"input_image","file_id":"..."}, no
// image_url field at all) has no "image_url` substring anywhere — only the
// type value "input_image" names it, and that value's leading '"' is
// preceded by "input_" rather than immediately followed by "image", so it
// doesn't match the first check either. Checking for the bare type-value
// substring closes that gap without a false-positive risk worth worrying
// about (it's a specific enough token).
func HasImageMarker(body []byte) bool {
	return bytes.Contains(body, []byte(`"image`)) || bytes.Contains(body, []byte(`input_image`))
}

// ImageInfo describes one image block found in a request — mirrors
// audit.ImageInfo field-for-field. Kept as a separate type so this package
// (a self-contained image utility) doesn't need to import internal/audit
// just for a struct shape; the caller (internal/server) converts.
type ImageInfo struct {
	MessageIndex     int    // which message this image's content block was in (0-based)
	Format           string // jpeg/png/gif/webp/bmp; empty for a remote URL never fetched
	Bytes            int64  // original (pre-downscale) byte count; 0 for a remote URL
	Width            int
	Height           int
	Remote           bool // http(s) image_url/url-sourced image vmr never fetched; every other field stays zero
	Downscaled       bool
	DownscaledWidth  int
	DownscaledHeight int
	DownscaledBytes  int64
	CacheHit         bool // downscaled bytes reused byte-for-byte from the on-disk cache
}

// Downscale rewrites inline base64 images in body whose longer side exceeds
// opts.MaxPx, re-encoding them as JPEG scaled to fit within opts.MaxPx, and
// always returns a description of every image it found (see ImageInfo) —
// detection doesn't depend on opts.MaxPx being positive. protocol selects
// which content-block shape to look for ("openai": content[].image_url.url
// data URI; "anthropic": content[].source with type "base64"; "openai-
// responses": content[].image_url as a flat data URI string, see
// rewriteResponsesImage). On any rewrite failure, or when nothing needed
// resizing, the returned body is the original unchanged (same backing
// array) — images is still populated.
//
// "Detected" and "decodable" are deliberately independent: len(images) is
// the count of content blocks that are structurally image references (the
// type field matched), full stop — it does not depend on whether this
// package's decoder could actually read the pixel header. A block whose
// type matches but whose payload fails to decode (unrecognized/corrupt
// format) still gets an ImageInfo entry (Format/Width/Height left zero —
// genuinely unknown — but MessageIndex/Bytes set), and is passed through
// byte-for-byte unchanged, never dropped. This matters because callers use
// len(images) as an authoritative "does this request have an image"
// signal (RequestFacts.HasImage feeds a hard routing Condition with no
// fallback) — a format vmr's stdlib decoders don't recognize is still a
// real image as far as the upstream provider is concerned, and must not be
// misclassified as "no image" just because vmr itself can't describe it.
func Downscale(body []byte, protocol string, opts Options) (result []byte, images []ImageInfo) {
	result = body
	if !HasImageMarker(body) {
		return
	}
	// Defensive belt on top of careful error handling below: this function
	// processes attacker-influenced image bytes through decoders that were
	// not written with adversarial input in mind. A panic here must not take
	// down the request — but it must not vanish either: every other fail-open
	// path in this package leaves a trace (audit metadata, skipped-image
	// info), so a recovered panic logs one stderr line. Without it, a decoder
	// bug (or adversarial input) would permanently disable downscaling for an
	// image with zero operator-visible signal.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "imgprep: panic recovered, request passed through unmodified: %v\n", r)
			result, images = body, nil
		}
	}()
	rewritten, changed, imgs, err := rewriteBody(body, protocol, opts)
	images = imgs
	if err != nil || !changed {
		return
	}
	result = rewritten
	return
}

// containerKey is the top-level key holding the array of message-shaped
// elements to walk: "messages" for Chat Completions/Anthropic Messages,
// "input" for Responses. Responses' "input" can also be a bare string
// (no array at all) — the json.Unmarshal into []json.RawMessage below
// fails for that shape exactly the way it already does for Chat
// Completions' "content is a plain string" case, and rewriteBody correctly
// no-ops rather than erroring.
func containerKey(protocol string) string {
	if protocol == "openai-responses" {
		return "input"
	}
	return "messages"
}

func rewriteBody(body []byte, protocol string, opts Options) ([]byte, bool, []ImageInfo, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, false, nil, nil
	}
	key := containerKey(protocol)
	rawMsgs, ok := top[key]
	if !ok {
		return nil, false, nil, nil
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(rawMsgs, &msgs); err != nil {
		return nil, false, nil, nil
	}
	changed := false
	var images []ImageInfo
	for i, m := range msgs {
		nm, mChanged, imgs, err := rewriteMessage(i, m, protocol, opts)
		images = append(images, imgs...)
		if err != nil || !mChanged {
			continue
		}
		msgs[i] = nm
		changed = true
	}
	if !changed {
		return nil, false, images, nil
	}
	newMsgs, err := core.MarshalNoEscape(msgs)
	if err != nil {
		return nil, false, images, err
	}
	top[key] = newMsgs
	out, err := core.MarshalNoEscape(top)
	if err != nil {
		return nil, false, images, err
	}
	return out, true, images, nil
}

func rewriteMessage(msgIndex int, raw json.RawMessage, protocol string, opts Options) (json.RawMessage, bool, []ImageInfo, error) {
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return raw, false, nil, nil
	}
	rawContent, ok := msg["content"]
	if !ok {
		return raw, false, nil, nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(rawContent, &blocks); err != nil {
		return raw, false, nil, nil // content is a plain string: no image blocks possible
	}
	changed := false
	var images []ImageInfo
	for i, b := range blocks {
		nb, bChanged, info, err := rewriteBlock(msgIndex, b, protocol, opts)
		if info != nil {
			images = append(images, *info)
		}
		if err != nil || !bChanged {
			continue
		}
		blocks[i] = nb
		changed = true
	}
	if !changed {
		return raw, false, images, nil
	}
	newContent, err := core.MarshalNoEscape(blocks)
	if err != nil {
		return raw, false, images, err
	}
	msg["content"] = newContent
	out, err := core.MarshalNoEscape(msg)
	if err != nil {
		return raw, false, images, err
	}
	return out, true, images, nil
}

func rewriteBlock(msgIndex int, raw json.RawMessage, protocol string, opts Options) (json.RawMessage, bool, *ImageInfo, error) {
	var block map[string]json.RawMessage
	if err := json.Unmarshal(raw, &block); err != nil {
		return raw, false, nil, nil
	}
	var typ string
	if err := json.Unmarshal(block["type"], &typ); err != nil {
		return raw, false, nil, nil
	}
	switch {
	case protocol == "openai" && typ == "image_url":
		return rewriteOpenAIImage(msgIndex, raw, block, opts)
	case protocol == "anthropic" && typ == "image":
		return rewriteAnthropicImage(msgIndex, raw, block, opts)
	case protocol == "openai-responses" && typ == "input_image":
		return rewriteResponsesImage(msgIndex, raw, block, opts)
	default:
		return raw, false, nil, nil
	}
}

func rewriteOpenAIImage(msgIndex int, raw json.RawMessage, block map[string]json.RawMessage, opts Options) (json.RawMessage, bool, *ImageInfo, error) {
	var iu map[string]json.RawMessage
	if err := json.Unmarshal(block["image_url"], &iu); err != nil {
		return raw, false, nil, nil
	}
	var url string
	if err := json.Unmarshal(iu["url"], &url); err != nil {
		return raw, false, nil, nil
	}
	data, ok := parseDataURI(url)
	if !ok {
		// Remote URL: vmr never fetches it, but it's still an image
		// reference worth recording.
		return raw, false, &ImageInfo{MessageIndex: msgIndex, Remote: true}, nil
	}
	newData, newMime, changed, info := processImage(data, opts)
	if info.Format == "" {
		// Header decode failed (unrecognized/corrupt format) — but this is
		// still structurally a real image reference (we got past
		// json.Unmarshal + parseDataURI above), so it must still count
		// toward HasImage/the image tally. Only Format/Width/Height are
		// genuinely unknown; Bytes is not — record that much rather than
		// dropping the reference entirely. See design note on Downscale
		// for why "detected" must never depend on "decodable".
		return raw, false, &ImageInfo{MessageIndex: msgIndex, Bytes: int64(len(data))}, nil
	}
	info.MessageIndex = msgIndex
	if !changed {
		return raw, false, &info, nil
	}
	newURL := "data:" + newMime + ";base64," + base64.StdEncoding.EncodeToString(newData)
	uv, err := core.MarshalNoEscape(newURL)
	if err != nil {
		return raw, false, &info, err
	}
	iu["url"] = uv
	ib, err := core.MarshalNoEscape(iu)
	if err != nil {
		return raw, false, &info, err
	}
	block["image_url"] = ib
	out, err := core.MarshalNoEscape(block)
	if err != nil {
		return raw, false, &info, err
	}
	return out, true, &info, nil
}

// rewriteResponsesImage handles a Responses-protocol input_image block:
// {"type":"input_image","image_url":"data:...","detail":"...","file_id":"..."}.
// Unlike rewriteOpenAIImage's Chat Completions shape, image_url here is a
// FLAT string field directly on the block, not a nested {"url":...} object —
// per the official openai-python SDK's ResponseInputImageParam (the
// OpenAPI-generated, authoritative field shape). A block that references an
// already-uploaded file (file_id, no image_url at all) has no bytes vmr can
// reach — recorded the same way a remote URL is: a real image reference
// that HasImage/EstimatedTokens must still count, just with nothing to
// downscale.
func rewriteResponsesImage(msgIndex int, raw json.RawMessage, block map[string]json.RawMessage, opts Options) (json.RawMessage, bool, *ImageInfo, error) {
	rawURL, hasURL := block["image_url"]
	if !hasURL {
		return raw, false, &ImageInfo{MessageIndex: msgIndex, Remote: true}, nil
	}
	var url string
	if err := json.Unmarshal(rawURL, &url); err != nil {
		return raw, false, nil, nil
	}
	data, ok := parseDataURI(url)
	if !ok {
		// A real http(s) URL (or an unrecognized scheme): vmr never fetches
		// it, but it's still an image reference worth recording — same
		// treatment as rewriteOpenAIImage/rewriteAnthropicImage's remote case.
		return raw, false, &ImageInfo{MessageIndex: msgIndex, Remote: true}, nil
	}
	newData, newMime, changed, info := processImage(data, opts)
	if info.Format == "" {
		// Header decode failed, but this is still a structurally-confirmed
		// image reference — see Downscale's doc comment on why "detected"
		// must never depend on "decodable".
		return raw, false, &ImageInfo{MessageIndex: msgIndex, Bytes: int64(len(data))}, nil
	}
	info.MessageIndex = msgIndex
	if !changed {
		return raw, false, &info, nil
	}
	newURL := "data:" + newMime + ";base64," + base64.StdEncoding.EncodeToString(newData)
	uv, err := core.MarshalNoEscape(newURL)
	if err != nil {
		return raw, false, &info, err
	}
	block["image_url"] = uv
	out, err := core.MarshalNoEscape(block)
	if err != nil {
		return raw, false, &info, err
	}
	return out, true, &info, nil
}

func rewriteAnthropicImage(msgIndex int, raw json.RawMessage, block map[string]json.RawMessage, opts Options) (json.RawMessage, bool, *ImageInfo, error) {
	var src map[string]json.RawMessage
	if err := json.Unmarshal(block["source"], &src); err != nil {
		return raw, false, nil, nil
	}
	var srcType string
	if err := json.Unmarshal(src["type"], &srcType); err != nil {
		return raw, false, nil, nil
	}
	if srcType != "base64" {
		// url-sourced image, or a shape we don't recognize: vmr never
		// fetches/decodes it, but it's still an image reference worth
		// recording.
		return raw, false, &ImageInfo{MessageIndex: msgIndex, Remote: true}, nil
	}
	var b64 string
	if err := json.Unmarshal(src["data"], &b64); err != nil {
		return raw, false, nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return raw, false, nil, nil
	}
	newData, newMime, changed, info := processImage(data, opts)
	if info.Format == "" {
		// Same reasoning as rewriteOpenAIImage's identical branch: header
		// decode failed, but this is still a real, structurally-confirmed
		// image reference — must still count, just without Format/Width/
		// Height (genuinely unknown).
		return raw, false, &ImageInfo{MessageIndex: msgIndex, Bytes: int64(len(data))}, nil
	}
	info.MessageIndex = msgIndex
	if !changed {
		return raw, false, &info, nil
	}
	mv, err := core.MarshalNoEscape(newMime)
	if err != nil {
		return raw, false, &info, err
	}
	dv, err := core.MarshalNoEscape(base64.StdEncoding.EncodeToString(newData))
	if err != nil {
		return raw, false, &info, err
	}
	src["media_type"] = mv
	src["data"] = dv
	sb, err := core.MarshalNoEscape(src)
	if err != nil {
		return raw, false, &info, err
	}
	block["source"] = sb
	out, err := core.MarshalNoEscape(block)
	if err != nil {
		return raw, false, &info, err
	}
	return out, true, &info, nil
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

// processImage decodes data's header (format auto-detected from content, not
// from any caller-supplied mime type) to describe the image — this much
// always runs, independent of opts.MaxPx. If opts.MaxPx is positive and the
// longer side exceeds it, the image is fully decoded, resized to fit, and
// re-encoded as JPEG. changed=false covers every "leave it alone" case:
// resizing disabled, already small enough, GIF (never rescaled — see the
// format=="gif" branch below for why), unrecognized format, corrupt data, or
// oversized declared dimensions (decompression-bomb guard) — info is still
// populated in all of these except a header-decode failure. There is no
// error return: every failure mode here is deliberately absorbed into
// changed=false (fail-open, per the package doc comment) rather than
// propagated, so a caller has nothing to check beyond changed/info.Format.
// Output is always JPEG; alpha is flattened onto white first since JPEG has
// no transparency.
//
// When opts.CacheDir is set, a cache lookup happens only after the
// need-to-process decision is made (longSide > MaxPx, not a decompression
// bomb) — most requests don't need any processing at all, and hashing the
// full image on every request would tax exactly the common case caching is
// meant to help least. A hit skips decode/scale/encode entirely; a miss
// falls through to full processing and stores the result before returning.
func processImage(data []byte, opts Options) (out []byte, mime string, changed bool, info ImageInfo) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", false, ImageInfo{}
	}
	info = ImageInfo{Format: format, Bytes: int64(len(data)), Width: cfg.Width, Height: cfg.Height}

	longSide := cfg.Width
	if cfg.Height > longSide {
		longSide = cfg.Height
	}
	if opts.MaxPx <= 0 || longSide <= opts.MaxPx {
		return nil, "", false, info
	}
	if cfg.Width*cfg.Height > maxDecodePixels {
		return nil, "", false, info
	}

	var hash [32]byte
	if opts.CacheDir != "" {
		hash = sha256.Sum256(data)
		maybeSweepCache(opts.CacheDir, opts.CacheTTLDays, time.Now())
		if cached, ok := cacheLookup(opts.CacheDir, hash, opts.MaxPx); ok {
			newW, newH := scaledSize(cfg.Width, cfg.Height, opts.MaxPx)
			info.Downscaled, info.DownscaledWidth, info.DownscaledHeight = true, newW, newH
			info.DownscaledBytes, info.CacheHit = int64(len(cached)), true
			return cached, "image/jpeg", true, info
		}
	}

	if format == "gif" {
		// Never rescaled, animated or not: an animated GIF would collapse
		// to a still (semantic change), and a single-frame GIF isn't worth
		// special-casing to still frame-decode it — image/gif.DecodeAll is
		// the only stdlib way to even learn the frame count, and it has no
		// cap on frames or cumulative decoded size, so distinguishing
		// "single frame, safe to scale" from "many frames" would require
		// paying the same unbounded decode cost this branch exists to
		// avoid. Detection (Format/Width/Height above) already ran either way.
		return nil, "", false, info
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", false, info
	}

	sb := src.Bounds()
	newW, newH := scaledSize(sb.Dx(), sb.Dy(), opts.MaxPx)

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.BiLinear.Scale(dst, dst.Bounds(), src, sb, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, "", false, info
	}
	result := buf.Bytes()
	if opts.CacheDir != "" {
		cacheStore(opts.CacheDir, hash, opts.MaxPx, result)
	}
	info.Downscaled, info.DownscaledWidth, info.DownscaledHeight = true, newW, newH
	info.DownscaledBytes = int64(len(result))
	return result, "image/jpeg", true, info
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
