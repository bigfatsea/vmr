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
// an attachment's raw (still base64-encoded) bytes, each tagged with the
// attachment kind the marker proves: "data" for Anthropic's source.*
// payloads, "file_data" for Responses' input_file blocks (see the
// openai-python SDK's ResponseInputFileParam — the field is genuinely named
// differently, not just a different nesting of the same key), and the two
// OpenAI image shapes, whose payload is a data URI — nested in image_url.url
// for Chat Completions, a flat image_url for Responses.
//
// "data":" is spanAmbiguous on purpose: Anthropic's image source.data and
// document source.data share the field name, so the marker alone cannot
// classify the span — resolveAmbiguousKind sniffs the source object's
// media_type (which always precedes data) instead. The sniff applies ONLY
// to ambiguous spans; unambiguous markers are never re-classified.
var dataFieldMarkers = []struct {
	marker []byte
	kind   spanKind
}{
	{[]byte(`"data":"`), spanAmbiguous},
	{[]byte(`"file_data":"`), spanDocument},
	{[]byte(`"url":"data:`), spanImage},
	{[]byte(`"image_url":"data:`), spanImage},
}

// mediaTypeSniffWindow is how far back from a `"data":"` value's start the
// source object's media_type field can sit — every integrated shape puts it
// immediately before data ("media_type":"image/png","data":" ≈ 35 bytes);
// 64 leaves room for pretty-printed spacing. The sniff looks for `"image/`
// (quote-anchored, so base64 payload bytes — which never contain quotes —
// cannot false-positive), and anything else fails open to document: the
// same classification today's untyped spans get, never an under-count.
const mediaTypeSniffWindow = 64

type spanKind uint8

const (
	spanDocument spanKind = iota
	spanImage
	// spanAmbiguous is only a dataFieldMarkers-table value: by the time a
	// span is built it has been resolved to spanDocument or spanImage.
	spanAmbiguous
)

// attachmentSpan is one attachment payload's byte range plus the kind the
// producing marker (or media_type sniff) proved. The span bytes are excluded
// from the text estimate and accounted by imageCount*imageTokenEstimate
// (image) or estimateDocumentTokens (document) — which one depends on kind.
type attachmentSpan struct {
	start, end int // [start,end) in body; unterminated trailing values span to end-of-body
	kind       spanKind
}

// attachmentSpans returns every attachment payload value in body as ordered,
// non-overlapping typed spans (the scan resumes after each value, so a
// marker inside an already-captured payload can't produce a second span).
// Unterminated trailing values (truncated request body) are spanned to
// end-of-body: the tail is still payload bytes, not text.
func attachmentSpans(body []byte) []attachmentSpan {
	var spans []attachmentSpan
	pos := 0
	for pos < len(body) {
		best, bestLen := -1, 0
		kind := spanDocument
		for _, m := range dataFieldMarkers {
			if i := bytes.Index(body[pos:], m.marker); i >= 0 && (best < 0 || pos+i < best) {
				best, bestLen = pos+i, len(m.marker)
				kind = m.kind
			}
		}
		if best < 0 {
			break
		}
		start := best + bestLen
		if kind == spanAmbiguous {
			kind = resolveAmbiguousKind(body, start)
		}
		if end := jsonscan.IndexUnescapedQuote(body[start:]); end < 0 {
			spans = append(spans, attachmentSpan{start, len(body), kind})
			break
		} else {
			spans = append(spans, attachmentSpan{start, start + end, kind})
			pos = start + end + 1
		}
	}
	return spans
}

// resolveAmbiguousKind classifies an Anthropic-shape `"data":"` span via the
// source object's media_type field, which every integrated shape puts just
// before the data value.
func resolveAmbiguousKind(body []byte, start int) spanKind {
	lo := start - mediaTypeSniffWindow
	if lo < 0 {
		lo = 0
	}
	if bytes.Contains(body[lo:start], []byte(`"image/`)) {
		return spanImage
	}
	return spanDocument // fail-open: today's classification for anything unrecognizable
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
func estimateTextTokens(body []byte, spans []attachmentSpan) int64 {
	var stats tokenutil.CharStats
	prev := 0
	for _, s := range spans {
		stats.Add(tokenutil.Analyze(body[prev:s.start]))
		prev = s.end
	}
	stats.Add(tokenutil.Analyze(body[prev:]))
	return tokenutil.EstimateFromStats(stats)
}

// estimateDocumentTokens converts the document spans' raw (still
// base64-encoded) byte length into an estimated document token count via
// documentBytesPerToken, but only once some documentMarker confirms an
// attachment is actually present — a pure-text or pure-image request with
// no document marker contributes zero here regardless of what "data" fields
// it might contain for unrelated reasons. Spans the producing marker (or
// media_type sniff) typed as images are skipped: an image's bytes are
// already accounted by imageCount*imageTokenEstimate, and summing them here
// too charged a 500KB inline image ~25K phantom document tokens on top of
// its own (correct) image estimate whenever a document marker appeared
// anywhere in the body — including mere mentions of PDFs in message text.
func estimateDocumentTokens(body []byte, spans []attachmentSpan) int64 {
	// No attachment payload spans → no document bytes to size, whatever
	// markers the body text might mention. Skip the 4 whole-body Contains
	// scans below on the ~95% of requests that carry no attachment at all.
	if len(spans) == 0 {
		return 0
	}
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
		if s.kind != spanDocument {
			continue
		}
		total += int64(s.end - s.start)
	}
	return total / documentBytesPerToken
}
