// Ver 2026-07-26, by Sonnet 5

// Live router log line formatting. Split out of router.go — pure move, no
// behavior change.
package router

import (
	"fmt"
	"strings"
	"time"

	"vmr/internal/audit"
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

// tagCol pads s to a fixed 8-char, left-aligned column — every log line
// starts with one so the fields after it stay vertically aligned regardless
// of how long the actor name is.
func tagCol(s string) string {
	return fmt.Sprintf("%-8s", s)
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
// change shape) — with a "(stream)" suffix when the request is streaming.
func epLabel(ep *core.Endpoint, stream bool) string {
	label := ep.AdapterType + ":" + ep.Provider + ":" + ep.Model
	if stream {
		label += "(stream)"
	}
	return label
}

// fmtDur renders an elapsed duration for the dur= column — see
// fmtutil.FmtSeconds; 2 decimals is this log's fixed precision.
func fmtDur(d time.Duration) string {
	return fmtutil.FmtSeconds(d, 2)
}

// capField renders the live router log's cap= column: which capabilities
// this specific request actually exercised (RequestFacts, computed once at
// ingress from the raw body), not what the endpoint declares support for —
// that's config.yaml's job, and repeating it here per line would just be
// noise. "-" when the request used none of the tracked capabilities.
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
	if len(caps) == 0 {
		return "-"
	}
	return strings.Join(caps, ",")
}

// attemptPrefix renders the fixed lead-in shared by every tryOne outcome
// line: client tag, virtual model routed to physical endpoint, what the
// request actually used, and the attempt number. reqBytes is the built
// request's on-wire size; pass -1 before BuildRequest has produced one (the
// req=.../...ESTKT column is then omitted entirely rather than printed as 0).
func attemptPrefix(rec *audit.Record, creq *core.CanonicalRequest, ep *core.Endpoint, attempt int, reqBytes int64) string {
	stream := ""
	if creq.Stream {
		stream = "(stream)"
	}
	req := ""
	if reqBytes >= 0 {
		req = fmt.Sprintf(", req=%s/%s", fmtutil.FmtBytes(reqBytes), fmtutil.FmtTokens(creq.Facts.EstimatedTokens))
	}
	return fmt.Sprintf("%s%s:%s -> %s:%s%s%s, cap=%s, attempt=%d",
		clientTag(rec), ep.AdapterType, creq.Model, ep.Provider, ep.Model, stream, req, capField(creq.Facts), attempt)
}
