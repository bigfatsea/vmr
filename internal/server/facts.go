// Ver 2026-07-24 12:00, by Sonnet 5

// RequestFacts computation for condition-based routing (see
// docs/VirtualModelRouter_System_Design_v3.md §6.4). Every estimate here is
// a coarse, deliberately-conservative approximation, not a precise
// accounting: the guiding cost principle is to infer from length/presence
// rather than parse content, accept imprecision, and lean toward
// overestimating — a wasted preference for a bigger-context endpoint is a
// cheap mistake; a real upstream 400 and the ordinary failover loop is the
// safety net for whatever this estimate gets wrong. The §6.4 fallback rule
// is the other half of that safety net: an overestimate here can never, by
// itself, empty an otherwise-non-empty candidate set.
package server

import (
	"bytes"

	"vmr/internal/adapter"
	"vmr/internal/core"
	"vmr/internal/imgprep"
)

const (
	// asciiBytesPerToken/wideBytesPerToken split the text estimate by
	// script: ASCII (mostly English) tokenizes at roughly 4 bytes/token
	// across mainstream BPE tokenizers; CJK and other multi-byte UTF-8
	// content is denser and deliberately overestimated at 2 bytes/token —
	// higher than any tokenizer this was checked against actually costs,
	// on purpose (see design doc §1.4).
	asciiBytesPerToken = 4
	wideBytesPerToken  = 2

	// imageTokenEstimate is a flat per-detected-image estimate (no pixel
	// decoding — see design doc §1.4's image section), calibrated to a
	// 1920x1080 screenshot's cost on Claude's high-resolution tier (2691
	// tokens) with a little headroom.
	imageTokenEstimate = 3000

	// documentBytesPerToken converts a base64 document/file payload's raw
	// (still-encoded) byte length into an estimated token count — see
	// design doc §1.4's document section for the calibration (derived from
	// Anthropic's published 1500-3000-tokens-per-page range).
	documentBytesPerToken = 20
)

// computeRequestFacts derives core.RequestFacts from a request's already-
// buffered body. It never fails: every sub-estimate degrades to zero on
// any shape it doesn't recognize, matching the fail-open posture of the
// scanners it's built from (imgprep.HasImageMarker,
// adapter.HasNonEmptyTopLevelArray).
func computeRequestFacts(body []byte, protocol string) core.RequestFacts {
	return core.RequestFacts{
		HasImage:        imgprep.HasImageMarker(body),
		HasTools:        adapter.HasNonEmptyTopLevelArray(body, "tools"),
		EstimatedTokens: estimateTextTokens(body) + estimateImageTokens(body, protocol) + estimateDocumentTokens(body),
	}
}

// estimateTextTokens scans the whole raw body byte-for-byte (JSON
// structure included — its overhead only pushes the estimate up, which is
// the safe direction) classifying by UTF-8 lead byte, no rune decoding.
func estimateTextTokens(body []byte) int64 {
	var ascii, wide int64
	for _, b := range body {
		if b < 0x80 {
			ascii++
		} else {
			wide++
		}
	}
	return ascii/asciiBytesPerToken + wide/wideBytesPerToken
}

// imageCountMarkers approximate "how many images" without decoding any of
// them: a cheap occurrence count of the shape-specific field each protocol
// uses per inline image. This can over- or under-count relative to the
// true image count (message text mentioning the marker, or an unusual
// shape); it exists only to scale imageTokenEstimate, not to describe the
// request precisely.
var (
	openaiImageMarker    = []byte(`"image_url"`)
	anthropicImageMarker = []byte(`"type":"image"`)
)

func estimateImageTokens(body []byte, protocol string) int64 {
	var n int
	if protocol == "anthropic" {
		n = bytes.Count(body, anthropicImageMarker)
	} else {
		n = bytes.Count(body, openaiImageMarker)
	}
	if n == 0 && imgprep.HasImageMarker(body) {
		n = 1 // the cheap presence marker fired but the shape-specific count missed it; assume at least one
	}
	return int64(n) * imageTokenEstimate
}

// documentMarkers are cheap, wide-net signals that the request carries a
// document/file attachment (PDF, or an unrecognized binary format) — see
// design doc §1.4. Presence-only: matching text inside a message (not an
// attachment) is a false positive whose only cost is a harmless
// over-estimate, the same tradeoff HasImageMarker already makes.
var documentMarkers = [][]byte{
	[]byte(`"type":"document"`),
	[]byte(`application/pdf`),
	[]byte(`"type":"file"`),
	[]byte(`input_file`),
}

var dataFieldMarker = []byte(`"data":"`)

// estimateDocumentTokens sums the raw (still base64-encoded) byte length
// of every "data" field in the body and divides by documentBytesPerToken,
// but only once some documentMarker confirms an attachment is actually
// present — a pure-text or pure-image request with no document marker
// contributes zero here regardless of what "data" fields it might contain
// for unrelated reasons. Known imprecision: on protocols where an inline
// image's payload also lives in a "data" field (Anthropic), a request
// carrying both an image and a document sums both spans — an
// over-estimate, the safe direction, not a correctness bug.
func estimateDocumentTokens(body []byte) int64 {
	hasMarker := false
	for _, m := range documentMarkers {
		if bytes.Contains(body, m) {
			hasMarker = true
			break
		}
	}
	if !hasMarker {
		return 0
	}
	var total int64
	rest := body
	for {
		idx := bytes.Index(rest, dataFieldMarker)
		if idx < 0 {
			break
		}
		rest = rest[idx+len(dataFieldMarker):]
		end := indexUnescapedQuote(rest)
		if end < 0 {
			break
		}
		total += int64(end)
		rest = rest[end+1:]
	}
	return total / documentBytesPerToken
}

// indexUnescapedQuote returns the index of the first unescaped '"' in b,
// or -1 if there isn't one. Same backslash-parity check as
// internal/adapter's skipJSONString; kept as a small standalone copy here
// rather than exporting that scanner — this is the only caller outside
// internal/adapter and the logic is a few lines.
func indexUnescapedQuote(b []byte) int {
	i := 0
	for {
		j := bytes.IndexByte(b[i:], '"')
		if j < 0 {
			return -1
		}
		k, n := i+j-1, 0
		for k >= 0 && b[k] == '\\' {
			n++
			k--
		}
		if n%2 == 0 {
			return i + j
		}
		i += j + 1
	}
}
