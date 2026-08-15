// Ver 2026-07-26, by Sonnet 5

// Live router log line formatting. Split out of router.go — pure move, no
// behavior change.
package router

import (
	"fmt"
	"math"
	"strings"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/core"
	"vmr/internal/fmtutil"
)

func (rt *Router) logf(format string, args ...any) {
	if rt.Logger != nil {
		rt.Logger.Printf(format, args...)
	}
}

// Logf is logf, exported so callers outside this package (internal/server,
// for the handful of lines it logs itself — e.g. audit write failures) go
// through the same nil-safe path and pick up the same timestamp/format
// instead of falling back to the unstamped global "log" package.
func (rt *Router) Logf(format string, args ...any) {
	rt.logf(format, args...)
}

// tagCol pads s to a fixed 9-char, left-aligned column — every log line
// starts with one so the fields after it stay vertically aligned regardless
// of how long the actor name is. 9 (not 8) so an exactly-8-char client tag
// still gets a trailing separator space before the next field, instead of
// running into it (an 8-wide column left zero room for a tag that long).
func tagCol(s string) string {
	return fmt.Sprintf("%-9s", s)
}

// clientTag is the tagCol value for a real client request: the audit key
// tag that identifies who sent it, or "-" when auditing is off (rec == nil)
// or the request carried no matching key.
func clientTag(rec *audit.Record) string {
	tag := "-"
	if rec != nil && rec.ClientKeyTag != "" {
		tag = rec.ClientKeyTag
	}
	return tagCol(tag)
}

// epLabel is the log-only endpoint label — colon-joined protocol:provider:model
// (as opposed to Endpoint.Name()'s slash form, which is a stable identifier
// used in the admin status API and the X-VMR-Endpoint header and must not
// change shape). Only probe.go still calls this directly — a probe request
// is never streamed, so there's no stream marker to carry; tryOne's own
// attemptPrefix builds the client-facing "virtual -> physical" form inline.
func epLabel(ep *core.Endpoint) string {
	return ep.AdapterType + ":" + ep.Provider + ":" + ep.Model
}

// fmtDur renders an elapsed duration for the dur= column — see
// fmtutil.FmtSeconds; 2 decimals is this log's fixed precision.
func fmtDur(d time.Duration) string {
	return fmtutil.FmtSeconds(d, 2)
}

// capField renders the live router log's capability column: which
// capabilities this specific request actually exercised (RequestFacts,
// computed once at ingress from the raw body), not what the endpoint
// declares support for — that's config.yaml's job, and repeating it here
// per line would just be noise. Pipe-joined ("tools|image"), no field label
// (the vocabulary is self-describing and doesn't collide with anything
// else in the line); "" — meaning: omit the whole segment — for a
// pure-text request, so a normal chat turn doesn't carry a "-" for a
// column it never uses.
func capField(f core.RequestFacts) string {
	var caps []string
	if f.HasImage {
		caps = append(caps, "image")
	}
	if f.HasAudio {
		caps = append(caps, "audio")
	}
	if f.HasVideo {
		caps = append(caps, "video")
	}
	if f.HasTools {
		caps = append(caps, "tools")
	}
	if f.WantsThinking {
		caps = append(caps, "think")
	}
	return strings.Join(caps, "|")
}

// estTokenField renders the pre-call token estimate (creq.Facts.
// EstimatedTokens, computed once at ingress) for a log line whose outcome
// never learns actual usage — every error/failover tail (build error,
// network error, upstream error) reads this, since none of them reaches
// respnorm.Wrap. forwardSuccess's usageTokenField also falls back to this
// exact string when the upstream never reported usage at all. Appends
// "(est)" itself rather than relying on fmtutil.FmtTokensCompact to carry
// an estimate marker — see that function's doc comment.
func estTokenField(creq *core.CanonicalRequest) string {
	return "in " + fmtutil.FmtTokensCompact(creq.Facts.EstimatedTokens) + "(est)"
}

// usageTokenField renders a successful response's actual token usage —
// "in $X, ch $N%, cw $X, out $X" — omitting any component the upstream
// didn't report (chatmsg.Usage's doc comment: 0 means "not reported", not
// "billed zero"). ch is the cache-hit share of In (CacheRead/In, already
// included in In per Usage's doc comment), not an absolute count — the one
// column here that's a ratio rather than a token count. Falls back to
// estTokenField when ok is false: the upstream never reported usage at all
// (opaque/compressed body, or a stream that died before any usage-bearing
// block arrived — see tokenCharge's doc comment for the same fallback on
// the quota-charging side).
func usageTokenField(u chatmsg.Usage, ok bool, creq *core.CanonicalRequest) string {
	if !ok {
		return estTokenField(creq)
	}
	parts := []string{"in " + fmtutil.FmtTokensCompact(u.In)}
	if u.In > 0 && u.CacheRead > 0 {
		parts = append(parts, fmt.Sprintf("ch %d%%", int(math.Round(float64(u.CacheRead)/float64(u.In)*100))))
	}
	if u.CacheWrite > 0 {
		parts = append(parts, "cw "+fmtutil.FmtTokensCompact(u.CacheWrite))
	}
	if u.Out > 0 {
		parts = append(parts, "out "+fmtutil.FmtTokensCompact(u.Out))
	}
	return strings.Join(parts, ", ")
}

// attemptPrefix renders the fixed lead-in shared by every tryOne outcome
// line: client tag, virtual model routed to physical endpoint (» for a
// streaming request, > for a non-streaming one — no separate "(stream)"
// suffix), and which capabilities it exercised. Token usage and the
// attempt/status/duration tail differ per outcome (estimate-only on every
// error path, actual usage once forwardSuccess has one; done vs.
// still-failing-over), so callers append those themselves instead of this
// baking in one shape that would fit none of them well.
func attemptPrefix(rec *audit.Record, creq *core.CanonicalRequest, ep *core.Endpoint) string {
	arrow := ">"
	if creq.Stream {
		arrow = "»"
	}
	prefix := fmt.Sprintf("%s%s:%s %s %s:%s", clientTag(rec), ep.AdapterType, creq.Model, arrow, ep.Provider, ep.Model)
	if caps := capField(creq.Facts); caps != "" {
		prefix += ", " + caps
	}
	return prefix
}
