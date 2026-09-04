// Ver 2026-09-04 12:00, by pi-agent

package quota

// TokenUsage is the four raw token components TokenCountersSides needs — a
// plain scalar view so internal/quota stays free of any wire-protocol parser
// dependency (chatmsg). Callers translate their own usage type in; the
// caller owns the non-negative floor on Fresh (chatmsg.Usage.Fresh() is the
// one that guarantees it — see router/quota.go's thin wrapper).
type TokenUsage struct {
	Fresh, CacheRead, CacheWrite, Out int64
}

// TokenCountersSides is the canonical implementation of the exact-vs-degraded
// rule. inSniffed/outSniffed report whether each side of the usage ledger was
// actually parsed; a missing side falls back to the caller's estimate for
// that side (max'd with whatever the usage object did claim — a placeholder
// must never beat real emitted-text evidence), and the estimated share is
// reported honestly as the portion of the charge that came from an estimate,
// never 0 just because SOME usage was seen. The degraded In side charges
// everything to Fresh: it cannot tell cache hits apart, and assuming none is
// the safe direction — it overestimates consumption rather than silently
// crediting a cache discount that may not have happened (see the design
// doc's Metering section).
//
// Lives here, not in router, so internal/report — which archtest forbids
// from importing router — prices its recomputed columns through this one
// implementation instead of a private copy that can drift (the drift commit
// 66006f1 had to repair once already).
func TokenCountersSides(u TokenUsage, inSniffed, outSniffed bool, inEst, outEst int64) (Counters, float64) {
	if inSniffed && outSniffed {
		return Counters{
			Fresh:      float64(u.Fresh),
			CacheRead:  float64(u.CacheRead),
			CacheWrite: float64(u.CacheWrite),
			Out:        float64(u.Out),
		}, 0
	}
	var c Counters
	var est float64
	if inSniffed {
		c.Fresh = float64(u.Fresh)
		c.CacheRead = float64(u.CacheRead)
		c.CacheWrite = float64(u.CacheWrite)
	} else {
		c.Fresh = float64(inEst)
		est += float64(inEst)
	}
	if outSniffed {
		c.Out = float64(u.Out)
	} else {
		out := max(u.Out, outEst)
		c.Out = float64(out)
		est += float64(out)
	}
	return c, est
}
