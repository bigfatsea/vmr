// Ver 2026-09-01, by Stan: Q37

// Candidate selection for one request, split out of ServeWithSnap so the
// failover loop reads as just the loop. The pipeline health filter → hard
// conditions → context-length estimate → pin → sort → quota reorder → sticky
// stays one function (it reads as one thing and its ordering is the design's
// contract), but it no longer inflates the request handler's body.
package router

import (
	"encoding/hex"
	"net/http"
	"time"

	"vmr/internal/adapter"
	"vmr/internal/core"
	"vmr/internal/strategy"
)

// candidateSet is what buildCandidates hands the failover loop: the ordered
// endpoints to try, the subset that passed the health filter (for the
// no-candidates message), the routing reason trail, and the sticky key ("" =
// this conversation isn't sticky or has no fingerprint).
type candidateSet struct {
	endpoints []*core.Endpoint
	healthOK  []*core.Endpoint
	reason    routeReason
	stickyKey string
}

// buildCandidates runs the selection pipeline that decides which endpoints
// this request may try, in which order:
//
//	health filter → hard capability conditions → context-length estimate →
//	pin → stable sort → quota headroom reorder → sticky warm-cache lift.
//
// The order is deliberate and is the routing half's contract — see the design
// docs' Condition-based Routing and Scheduling Flow sections. Every step
// only ever removes or reorders; none mutates the endpoints themselves.
func (rt *Router) buildCandidates(snap *Snapshot, protocol string, creq *core.CanonicalRequest, route *ModelRoute, r *http.Request, now time.Time) candidateSet {
	// Health filter (read-only) + stable multi-key sort.
	//
	// A half-open endpoint (fails>0, cooldown expired) never gets touched by
	// real traffic at all — instead the first caller to notice it's unprobed
	// claims the single-flight slot (Acquire, same method the per-candidate
	// loop below uses) and hands it to a background probe goroutine, then
	// treats the endpoint as unavailable for THIS request exactly as if
	// Acquire had failed. Real requests never wait on that probe and are
	// never diverted for as long as it takes to resolve — only for as long
	// as it takes to notice it needs to run.
	healthOK := make([]*core.Endpoint, 0, len(route.Endpoints))
	for _, ep := range route.Endpoints {
		available, needsProbe := rt.Health.Classify(ep.HealthKey(), now)
		if needsProbe {
			go rt.runProbe(ep, snap)
		}
		if available {
			healthOK = append(healthOK, ep)
		}
	}

	// Hard capability conditions (image/tools/…, see internal/strategy) are
	// certainties — a request either needs a capability or it doesn't, an
	// endpoint either declares it or doesn't — so rejecting every candidate
	// here is a correct "give up" signal, not a bug.
	hardFiltered := make([]*core.Endpoint, 0, len(healthOK))
	for _, ep := range healthOK {
		if strategy.Eligible(ep, creq.Facts) {
			hardFiltered = append(hardFiltered, ep)
		}
	}

	// Context length is an estimate, not a certainty (see
	// docs/VirtualModelRouter_Design_v4_Core.md's Condition-based Routing
	// section), so it never gets to
	// empty a non-empty hardFiltered set on its own — if every declared
	// max_context_tokens looks too small, fall back to hardFiltered and let
	// a real attempt (backed by the ordinary failover loop once the
	// upstream returns a real 400) make the call instead of refusing on a
	// guess.
	candidates := make([]*core.Endpoint, 0, len(hardFiltered))
	for _, ep := range hardFiltered {
		if strategy.WithinContext(ep, creq.Facts) {
			candidates = append(candidates, ep)
		}
	}
	reason := routeReason{total: len(route.Endpoints), healthOK: len(healthOK), afterCond: len(hardFiltered)}
	if len(candidates) == 0 && len(hardFiltered) > 0 {
		candidates = hardFiltered
		reason.ctxFallback = true
	}

	// Pinned routing (X-VMR-Provider / X-VMR-Target-Model): narrow the
	// candidates to the requested provider/target model before sorting, so
	// the pin wins over priority/order — that is the entire point (see
	// pin.go). A pin matching nothing leaves an empty candidate set, which
	// fails below exactly like any other no-available-endpoint request,
	// with the pin named in the message. No pin headers = no-op.
	candidates, reason.pin = applyPinToCandidates(candidates, r)
	strategy.Sort(candidates, route.Dims)

	// Quota-Aware Routing: within each priority tier Sort just established,
	// move quota-bearing endpoints to the front in headroom-score order —
	// see internal/router/quota.go's reorderByQuota and
	// docs/VirtualModelRouter_Design_v4_Quota.md's Scheduling Flow
	// section for why this sits exactly here (after Sort, before Sticky).
	// nil-safe: a no-op returning false when rt.Quota is nil (no
	// quota.Registry wired up).
	reason.quota = reorderByQuota(candidates, route.Dims, rt.Quota, now)

	// Sticky Model: prefer whichever endpoint most recently, successfully
	// served this same conversation, so the upstream prompt cache stays
	// warm (docs/VirtualModelRouter_Design_v4_Core.md's Sticky Model
	// section). Only ever
	// reorders within the already-filtered candidates — an endpoint that's
	// unhealthy or fails a hard condition this turn is never resurrected
	// just because it was the sticky pick last time.
	var stickyKey string
	if route.Sticky {
		if sysHash, firstMsgHash, ok := adapter.SessionFingerprint(creq.Raw, protocol); ok {
			// ClientKeyTag is carried on the request itself (set by the
			// server layer at authentication time), so the sticky bucket is
			// identical whether or not auditing is enabled — previously it
			// was read off the audit record, which left it empty (and all
			// clients folded into one bucket) when rec was nil (Q30).
			stickyKey = creq.ClientKeyTag + ":" + hex.EncodeToString(sysHash[:]) + ":" + hex.EncodeToString(firstMsgHash[:])
			if epKey, lastUsed, found := rt.Sticky.Peek(stickyKey); found {
				if ep := findByHealthKey(candidates, epKey); ep != nil && time.Since(lastUsed) < ep.StickyTTL {
					moveToFront(candidates, ep)
					reason.sticky = true
				}
			}
		}
	}
	return candidateSet{endpoints: candidates, healthOK: healthOK, reason: reason, stickyKey: stickyKey}
}
