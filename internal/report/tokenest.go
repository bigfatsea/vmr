// Ver 2026-08-14, by Opus 5

// The analytics half's reproduction of the routing half's degraded token
// estimate — split out of aggregate.go to keep that file under
// internal/archtest's line budget, the same reason cost.go exists.
package report

import (
	"vmr/internal/audit"
	"vmr/internal/core"
)

// estimateDegradedTokens reproduces internal/router/quota.go's tokenCharge
// degraded path for a record whose usage was never sniffed: the request side
// reuses the pre-routing estimate vmr already computed and persisted
// (audit.Record.Facts.EstimatedTokens — the SAME value the router read off
// creq.Facts, not a re-derivation), the response side re-runs
// core.EstimateTextTokens over the recorded body. Both halves charge entirely
// to Fresh/Out, exactly as the router does — the degraded path cannot tell
// cache hits apart.
//
// Only called for a record that actually reached an endpoint (rc.endpoint
// non-empty): the router charges per FORWARDED attempt, so a request whose
// every attempt failed must contribute nothing here, the same basis
// EndpointRow's exact token fields already use.
//
// Known, deliberate residual: the router counts UPSTREAM bytes (respStream's
// ingest hook sees every source byte, including in opaque mode), while the
// only body an offline reader has is the CLIENT-side one the recorder
// captured — after model-name rewrite, after any response-normalization strip,
// and capped at recorderBodyCap. The two agree exactly when no normalization
// changed the byte count, and differ by that delta when it did. This is why
// ProviderQuotaRow carries WindowEstimatedPct rather than presenting the
// recomputed column as authoritative: the estimate is reported as an
// estimate. Reading the router's own charged number back out of the audit
// record would make the two agree by construction — and would also make §2.5's
// recomputed column a readout instead of the independent cross-check it
// exists to be (see ProviderQuotaRow's doc comment).
func estimateDegradedTokens(arec *audit.Record) (inEst, outEst int64) {
	if arec.Facts != nil {
		inEst = arec.Facts.EstimatedTokens
	}
	if arec.Client.Response != nil {
		outEst = core.EstimateTextTokens(bodyRaw(arec.Client.Response.Body))
	}
	return inEst, outEst
}
