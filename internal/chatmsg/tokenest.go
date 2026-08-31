// Ver 2026-09-01, by Opus 5

package chatmsg

import (
	"vmr/internal/core"
)

// EstimateDegradedTokens computes the input/output token estimates a record
// gets when the upstream reported no usage at all: the router's own
// Facts.EstimatedTokens when it was computed, a body-size estimate
// otherwise (see EstimateBodyTokens for why body estimates share one
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
// respBody is the client-side response body, or nil when there is no
// response to estimate from (a response with no usage still has a body to
// estimate over; a nil response contributes 0).
func EstimateDegradedTokens(facts *core.RequestFacts, reqBody, respBody any) (inEst, outEst int64) {
	if facts != nil {
		inEst = facts.EstimatedTokens
	} else {
		inEst = EstimateBodyTokens(reqBody)
	}
	if respBody != nil {
		outEst = EstimateBodyTokens(respBody)
	}
	return inEst, outEst
}
