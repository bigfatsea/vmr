// Ver 2026-09-03, by pi-agent

package replay

// Quota charging for replayed requests: the two functions chargeReplay
// (the meter that mirrors the router's live tokenCharge) and inEstFor
// (the degraded input-token estimate basis, preferring the record's own
// Facts.EstimatedTokens over a raw-byte estimate). Split out of replay.go
// by the archtest line budget on that file.

import (
	"time"

	"vmr/internal/chatmsg"
	"vmr/internal/core"
	"vmr/internal/quota"
	"vmr/internal/router"
	"vmr/internal/tokenutil"
)

// inEstFor resolves the degraded input-token estimate a replayed request's
// quota charge should use: the record's own Facts.EstimatedTokens when the
// original live request computed one (the same number server/facts.go
// produced and the router charged with — re-estimating from raw bytes
// would silently disagree), else a raw-byte estimate of the request body.
func inEstFor(rv *recordView) int64 {
	if rv.Facts != nil && rv.Facts.EstimatedTokens > 0 {
		return rv.Facts.EstimatedTokens
	}
	return tokenutil.Estimate(rv.Client.Request.Body)
}

// chargeReplay meters one successful replay's consumption against ep's
// provider quota (a no-op when ep.Quota is nil, i.e. -provider has no
// quota: configured) and hands it to router.ChargeResponse — the same
// metric-dispatch/model-multiplier/cost-pricing pipeline chargeQuota uses
// for live traffic (see that function's doc comment). It differs from
// chargeQuota only in how usage is obtained: live traffic sniffs it
// incrementally off a streaming respnorm.NormalizerStream, but replay already
// has the complete request/response bytes in hand (reqBody/respBody), so
// chatmsg.ExtractUsageSides — the same per-side rule respnorm's own usage
// sniffing delegates to — reads it directly from the buffered bytes
// instead, and the degraded estimate comes from the record's own
// Facts.EstimatedTokens (inEst, the live basis) plus a byte estimate of
// the response.
//
// Everything after "how usage was obtained" is router.TokenCountersSides,
// not a second copy of the exact-vs-degraded rule — see that function's
// doc comment for why all three call sites had to converge on one
// implementation. The side flags come from ExtractUsageSides, never from
// the merged `u.In > 0 || u.Out > 0` disjunction: a stream truncated after
// Anthropic's message_start would bill its ~1 placeholder output as exact
// (estimated=0) under that old test, exactly the partial-usage-as-precise
// failure TokenCountersSides exists to prevent.
func chargeReplay(reg *quota.Registry, ep *core.Endpoint, protocol string, reqBody, respBody []byte, inEst int64, now time.Time) {
	if ep.Quota == nil {
		return
	}
	u, inOK, outOK := chatmsg.ExtractUsageSides(respBody, protocol)
	raw, estimated := router.TokenCountersSides(u, inOK, outOK,
		inEst, tokenutil.Estimate(respBody))
	router.ChargeResponse(reg, ep, raw, estimated, now)
}
