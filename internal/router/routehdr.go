// Ver 2026-07-28 15:05, by Opus 5

// Real-time routing feedback on the response itself: why this endpoint was
// picked, and what the endpoints tried before it did wrong.
//
// All of it is already recorded in the audit log, per attempt, in far more
// detail — but that answers the question minutes later, from a different
// tool. These two headers answer it in the terminal you are already looking
// at, which is where the question is actually asked ("why did that go to
// the expensive provider?", "it worked, but was that a failover?").
//
// Both extend the existing X-VMR-Endpoint/X-VMR-Attempts deviation from
// byte-faithful passthrough rather than opening a new one (design doc §5.4:
// vmr-generated response metadata is a sanctioned exception, provider
// response headers are still copied verbatim). Neither carries anything the
// client couldn't already infer from X-VMR-Endpoint — the same provider
// names, no keys, no URLs.
package router

import (
	"net/http"
	"strconv"
	"strings"

	"vmr/internal/core"
)

// routeReason describes how the candidate list for one request was arrived
// at. Every field is a count the failover loop already had on hand.
type routeReason struct {
	total       int  // endpoints configured for this virtual model
	healthOK    int  // survived the health filter
	afterCond   int  // survived hard capability conditions
	ctxFallback bool // every declared context window looked too small; fell back (§6.4)
	sticky      bool // a sticky pointer reordered the list
}

// String renders only what actually happened: the overwhelmingly common
// "nothing was eliminated, order decided it" case stays a short header
// instead of a row of zeroes nobody reads.
func (rr routeReason) String() string {
	pick := "order"
	if rr.sticky {
		pick = "sticky"
	}
	parts := []string{
		"pick=" + pick,
		"eligible=" + strconv.Itoa(rr.afterCond) + "/" + strconv.Itoa(rr.total),
	}
	if n := rr.total - rr.healthOK; n > 0 {
		parts = append(parts, "cooldown="+strconv.Itoa(n))
	}
	if n := rr.healthOK - rr.afterCond; n > 0 {
		parts = append(parts, "conditions="+strconv.Itoa(n))
	}
	if rr.ctxFallback {
		parts = append(parts, "ctx_fallback=1")
	}
	return strings.Join(parts, " ")
}

// failoverTrail accumulates "what the earlier attempts did wrong" as the
// loop walks candidates. Set on the ResponseWriter *before* each attempt
// rather than after the loop: the attempt that finally succeeds writes the
// response headers itself, from deep inside forwardSuccess, and never
// returns control to Serve first. Recording failures-so-far up front means
// a successful failover still carries the trail that explains it.
type failoverTrail []string

func (ft *failoverTrail) add(ep *core.Endpoint, status int) {
	what := "err" // build or network failure: no HTTP response ever arrived
	if status > 0 {
		what = strconv.Itoa(status)
	}
	*ft = append(*ft, headerSafe(ep.Provider)+"/"+headerSafe(ep.Model)+":"+what)
}

func (ft failoverTrail) apply(h http.Header) {
	if len(ft) > 0 {
		h.Set("X-VMR-Failover", strings.Join(ft, ", "))
	}
}

// headerSafe strips anything that can't appear in a header value. Provider
// and model names come from config.yaml, not from a request, so this is a
// belt-and-suspenders guard against a config typo producing a header Go's
// writer would reject (which would fail the whole response, not just this
// diagnostic) — not a defense against hostile input.
func headerSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == ',' || r == ':' {
			return -1
		}
		return r
	}, s)
}
