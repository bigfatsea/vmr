// Ver 2026-09-01, by Opus 5

package chatmsg

import (
	"vmr/internal/core"
)

// EstimateDegradedTokens computes the input/output token estimates a record
// gets when the upstream reported no usage at all: the router's own
// Facts.EstimatedTokens when it was computed, a body-size estimate
// otherwise (see EstimateRequestBodyTokens for why body estimates share one
// implementation). One shared function because two copies already drifted:
// internal/report priced unsniffed records from a byte estimate while
// internal/story skipped them entirely, and the divergence was invisible —
// both halves priced through pricing.Rate.Cost (the shared formula) but fed
// it different BASES, which is exactly the failure mode the "an analytics
// number reproducing another must be pinned, not commented" rule is about.
// report's factscache and ctxgraph's manifest both call here directly, so
// the macro report's §2 total and a journey's cost line can't disagree
// about a record's degraded tokens at compile time, not by a comment's
// promise.
//
// Degraded basis rule — the one authority for both sides' fallbacks:
// estimate content tokens from the most faithful representation available;
// degrade fidelity, never kind. Bytes that measure something other than
// content (SSE envelopes, compressed or mangled opaque bodies) must yield
// 0 — a wrong quantity is worse than no estimate — and each side must
// mirror the basis the router actually charged. Applied to the two sides'
// different information states this yields a deliberate FALLBACK ASYMMETRY:
//
//   - Request side (EstimateRequestBodyTokens): raw-byte fallback is correct. The
//     client's plain JSON IS the content plus scaffolding, and the router's
//     input charge (Facts.EstimatedTokens, see internal/server/facts.go) is
//     raw-bytes based — falling back to 0 would make reports diverge from
//     what quota actually deducted.
//   - Response side (EstimateResponseBodyTokens): no raw fallback, 0
//     instead. The router's outTokenMeter never counted envelope bytes, and
//     for truncated/opaque responses the raw bytes measure transport, not
//     generation — the Q04 71x inflation. Falling back to raw would
//     reintroduce it.
//
// The asymmetry is behavior, pinned by
// TestEstimateDegradedBasis_FallbackAsymmetry; do not "unify" it without
// overturning this rule. The aligned cases (extractable text on both sides,
// opaque on both sides) are pinned by TestQuotaParity_StreamingSSE_DegradedEstimateBasis.
//
// respBody is the client-side response body, or nil when there is no
// response to estimate from (a response with no usage still has a body to
// estimate over; a nil response contributes 0). The output side mirrors the
// router's metering basis through EstimateResponseBodyTokens — extracted
// text only, 0 for opaque/binary bodies — while the input side keeps the
// raw-byte basis the router's Facts.EstimatedTokens uses.
func EstimateDegradedTokens(facts *core.RequestFacts, reqBody, respBody any) (inEst, outEst int64) {
	if facts != nil {
		inEst = facts.EstimatedTokens
	} else {
		inEst = EstimateRequestBodyTokens(reqBody)
	}
	if respBody != nil {
		outEst = EstimateResponseBodyTokens(respBody)
	}
	return inEst, outEst
}
