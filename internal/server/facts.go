// Ver 2026-07-25, by Sonnet 5

// RequestFacts computation for condition-based routing (see
// docs/VirtualModelRouter_Design_v4_Core.md's Condition-based Routing
// section). Every estimate here is a coarse, deliberately-conservative
// approximation, not a precise accounting: the guiding cost principle is to
// infer from length/presence rather than parse content, accept imprecision,
// and lean toward overestimating — a wasted preference for a bigger-context
// endpoint is a cheap mistake; a real upstream 400 and the ordinary
// failover loop is the safety net for whatever this estimate gets wrong.
// The Condition-based Routing section's fallback rule is the other half of
// that safety net: an overestimate here can never, by itself, empty an
// otherwise-non-empty candidate set.
package server

import (
	"bytes"

	"vmr/internal/core"
	"vmr/internal/jsonscan"
)

const (
	// imageTokenEstimate is a flat per-detected-image estimate (no pixel
	// decoding), calibrated to a 1920x1080 screenshot's cost on Claude's
	// high-resolution tier (2691 tokens) with a little headroom.
	imageTokenEstimate = 3000

	// documentBytesPerToken converts a base64 document/file payload's raw
	// (still-encoded) byte length into an estimated token count (derived
	// from Anthropic's published 1500-3000-tokens-per-page range).
	documentBytesPerToken = 20
)

// computeRequestFacts derives core.RequestFacts from a request's already-
// buffered body. It never fails: every sub-estimate degrades to zero on
// any shape it doesn't recognize, matching the fail-open posture of the
// scanner it's built from.
//
// hasTools is NOT derived here from a second body scan — it comes from the
// same adapter.TopLevelProbe call server.go already made to extract
// model/stream, folded into that single structural pass instead of a
// second, independent top-level "tools" array scan.
//
// imageCount is NOT derived here from a body scan — it comes from the
// caller's single imgprep.Downscale call (server.go), which walks the
// actual message/content-block structure (protocol-aware: openai
// content[].image_url, anthropic content[].source) rather than doing a raw
// substring search. Threading the already-computed count through, instead
// of re-detecting images here, serves two purposes at once:
//
// 1. Correctness: HasImage (imageCount > 0) feeds a hard capability
// Condition (internal/strategy/conditions.go) with no fallback, so a
// false positive can zero out every candidate endpoint — unlike the rest
// of EstimatedTokens, which is deliberately over-inclusive and only nudges
// a soft preference. The naive byte-scan this replaced did exactly that in
// a real incident: a text-only request quoting something like
// "image_downscale=512px" back from a tool result got routed as one
// needing image support.
// 2. Cost: reusing the count means a no-image request pays for exactly one
// presence check across the whole request (imgprep.HasImageMarker, inside
// Downscale), not that plus a second marker scan for the token estimate.
func computeRequestFacts(body []byte, imageCount int, hasTools bool) core.RequestFacts {
	return core.RequestFacts{
		HasImage:        imageCount > 0,
		HasTools:        hasTools,
		EstimatedTokens: core.EstimateTextTokens(body) + int64(imageCount)*imageTokenEstimate + estimateDocumentTokens(body),
	}
}

// documentMarkers are cheap, wide-net signals that the request carries a
// document/file attachment (PDF, or an unrecognized binary format).
// Presence-only: matching text inside a message (not an
// attachment) is a false positive whose only cost is a harmless
// over-estimate, the same tradeoff HasImageMarker already makes.
var documentMarkers = [][]byte{
	[]byte(`"type":"document"`),
	[]byte(`application/pdf`),
	[]byte(`"type":"file"`),
	[]byte(`input_file`),
}

// dataFieldMarkers are the field names whose value holds a document's raw
// (still base64-encoded) bytes: "data" for Anthropic's source.data, and
// "file_data" for Responses' input_file blocks (see the openai-python SDK's
// ResponseInputFileParam — the field is genuinely named differently, not
// just a different nesting of the same key).
var dataFieldMarkers = [][]byte{[]byte(`"data":"`), []byte(`"file_data":"`)}

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
	for _, marker := range dataFieldMarkers {
		rest := body
		for {
			idx := bytes.Index(rest, marker)
			if idx < 0 {
				break
			}
			rest = rest[idx+len(marker):]
			end := jsonscan.IndexUnescapedQuote(rest)
			if end < 0 {
				break
			}
			total += int64(end)
			rest = rest[end+1:]
		}
	}
	return total / documentBytesPerToken
}
