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
//
// SCOPE OF "LEAN TOWARD OVERESTIMATING": it is justified only where the
// estimate feeds a routing filter or a soft preference — both of which have
// a fallback. It is NOT justified for quota metering: router/quota.go's
// degraded In-side charge reuses EstimatedTokens verbatim when the upstream
// reported no usage, and that number is written permanently into
// vmr-quota.json. An inflated basis there is mis-accounted spend with no
// safety net, which is why attachment payload bytes are excluded from the
// text estimate below instead of being absorbed into the "overestimate is
// safe" posture — a 500KB inline image counted as text is ~100K phantom
// tokens on top of the image's own imageTokenEstimate.
package server

import (
	"bytes"

	"vmr/internal/core"
	"vmr/internal/jsonscan"
	"vmr/internal/tokenutil"
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
// actual message/content-block structure (protocol-aware: openai-completions
// content[].image_url, anthropic-messages content[].source) rather than doing
// a raw substring search. Threading the already-computed count through, instead
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
	// One attachment-span scan feeds both the text estimate (which must
	// EXCLUDE these ranges — their bytes are base64 payload, not message
	// text, and are accounted separately below) and estimateDocumentTokens
	// (which sums them). Two independent scans could silently disagree
	// about where the attachments are, so there is only one.
	spans := attachmentSpans(body)
	return core.RequestFacts{
		HasImage:        imageCount > 0,
		HasTools:        hasTools,
		EstimatedTokens: estimateTextTokens(body, spans) + int64(imageCount)*imageTokenEstimate + estimateDocumentTokens(body, spans),
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

// dataFieldMarkers are the field names / value prefixes whose value holds
// an attachment's raw (still base64-encoded) bytes: "data" for Anthropic's
// source.* payloads, "file_data" for Responses' input_file blocks (see the
// openai-python SDK's ResponseInputFileParam — the field is genuinely named
// differently, not just a different nesting of the same key), and the two
// OpenAI image shapes, whose payload is a data URI — nested in image_url.url
// for Chat Completions, a flat image_url for Responses.
var dataFieldMarkers = [][]byte{
	[]byte(`"data":"`),
	[]byte(`"file_data":"`),
	[]byte(`"url":"data:`),
	[]byte(`"image_url":"data:`),
}

// attachmentSpans returns the byte ranges of every attachment payload value
// in body as [start,end) pairs — ordered, non-overlapping (the scan resumes
// after each value, so a marker inside an already-captured payload can't
// produce a second span). Unterminated trailing values (truncated request
// body) are spanned to end-of-body: the tail is still payload bytes, not
// text.
func attachmentSpans(body []byte) [][2]int {
	var spans [][2]int
	pos := 0
	for pos < len(body) {
		best, bestLen := -1, 0
		for _, m := range dataFieldMarkers {
			if i := bytes.Index(body[pos:], m); i >= 0 && (best < 0 || pos+i < best) {
				best, bestLen = pos+i, len(m)
			}
		}
		if best < 0 {
			break
		}
		start := best + bestLen
		if end := jsonscan.IndexUnescapedQuote(body[start:]); end < 0 {
			spans = append(spans, [2]int{start, len(body)})
			break
		} else {
			spans = append(spans, [2]int{start, start + end})
			pos = start + end + 1
		}
	}
	return spans
}

// estimateTextTokens estimates the token count of body's NON-attachment
// bytes: the character-class weights are additive, so estimating the
// complement of the spans segment-by-segment and rounding once equals the
// estimate over the body with payloads spliced out, without copying body.
// Each span's bytes are accounted by imageCount*imageTokenEstimate or
// estimateDocumentTokens instead — counting them here too would double-
// charge a 500KB inline image as ~100K phantom text tokens on top of its
// own (correct) image estimate, and that inflated total is what quota
// metering's degraded In-side charge would write to the ledger.
func estimateTextTokens(body []byte, spans [][2]int) int64 {
	var stats tokenutil.CharStats
	prev := 0
	for _, s := range spans {
		stats.Add(tokenutil.Analyze(body[prev:s[0]]))
		prev = s[1]
	}
	stats.Add(tokenutil.Analyze(body[prev:]))
	return tokenutil.EstimateFromStats(stats)
}

// estimateDocumentTokens converts the attachment spans' raw (still
// base64-encoded) byte length into an estimated document token count via
// documentBytesPerToken, but only once some documentMarker confirms an
// attachment is actually present — a pure-text or pure-image request with
// no document marker contributes zero here regardless of what "data" fields
// it might contain for unrelated reasons. Known imprecision: a request
// carrying both an image and a document sums both spans — an over-estimate,
// the safe direction, not a correctness bug.
func estimateDocumentTokens(body []byte, spans [][2]int) int64 {
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
	for _, s := range spans {
		total += int64(s[1] - s[0])
	}
	return total / documentBytesPerToken
}
