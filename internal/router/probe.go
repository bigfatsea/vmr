// Ver 2026-08-02, by Sonnet 5

// Serve's background half (see router.go) — a dedicated, lightweight
// request that verifies a half-open endpoint without making any real
// client wait on it.
package router

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"vmr/internal/adapter"
	"vmr/internal/core"
	"vmr/internal/probe"
	"vmr/internal/quota"
)

// probeBodyCap bounds how much of a probe's response body gets read — small
// on purpose: probe.Request asks for at most a few dozen tokens back, and an
// error body this size is already generous (see errBodyCap in router.go for
// the same reasoning applied to real traffic).
const probeBodyCap = 32 << 10

// runProbe sends one probe.Request to ep and reports the outcome to the
// health registry, exactly the way tryOne reports a real attempt's outcome
// — except that a probe's 2xx is the weaker ReportProbeSuccess (decay, not
// clear): see internal/health's ReportProbeSuccess for why. The caller
// (Serve's candidate-building loop, via Classify's needsProbe return) has
// already claimed the single-flight slot before launching this as a
// goroutine — runProbe's only job is to make sure that claim always resolves
// via ReportProbeSuccess/ReportFailure/ReportNeutral, whatever happens, or
// the endpoint would stay locked in "probing" forever (see Acquire's doc
// comment in internal/health/health.go).
func (rt *Router) runProbe(ep *core.Endpoint, snap *Snapshot) {
	key := ep.HealthKey()
	logPrefix := tagCol("probe") + epLabel(ep)
	defer func() {
		if r := recover(); r != nil {
			rt.Health.ReportNeutral(key)
			rt.logf("%s, panic=%v", logPrefix, r)
		}
	}()

	ad, ok := adapter.Get(ep.AdapterType)
	if !ok { // validated at config load; defensive only, mirrors tryOne
		rt.Health.ReportNeutral(key)
		return
	}

	// The probe body's shape must match the endpoint's protocol — Request's
	// Chat-Completions-shaped ("messages") body sent to a Responses endpoint
	// would be rejected as missing the required "input" field, which
	// ClassifyError would most likely read as ErrClient (a bad-request
	// verdict, see adapter.DefaultClassify) and thus ReportNeutral: the
	// endpoint would never leave "probing" and stay half-open forever, even
	// though it's perfectly healthy. See probe.ResponsesRequest's doc
	// comment for why this dispatches on protocol instead of adding a
	// second, wrong-shaped probe body to the mix.
	var body json.RawMessage
	var nonce string
	if ep.AdapterType == core.ProtocolOpenAIResponses {
		body, nonce = probe.ResponsesRequest(ep.Model)
	} else {
		body, nonce = probe.Request(ep.Model)
	}
	creq := &core.CanonicalRequest{Model: ep.Model, Stream: false, Raw: body}
	ctx, cancel := context.WithTimeout(context.Background(), snap.Cfg.ProbeTimeout.D())
	defer cancel()

	req, _, err := ad.BuildRequest(ctx, ep, creq)
	if err != nil {
		// A build failure is about vmr's own request construction, not the
		// endpoint — same "says nothing about health" call tryOne makes for
		// this class of error, just without a health penalty since there was
		// never a real request behind this one to have failed either.
		rt.logf("%s, error=build:%v", logPrefix, err)
		rt.Health.ReportNeutral(key)
		return
	}

	start := time.Now()
	resp, err := snap.clientFor(ep).Do(req)
	dur := time.Since(start)
	if err != nil {
		cd := rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now())
		rt.logf("%s, error=network:%v, dur=%s, cooldown=%s", logPrefix, err, fmtDur(dur), cd)
		return
	}
	defer resp.Body.Close()
	// A probe consumes one real upstream request; charge it so request-
	// metered accounts don't accrue usage the local ledger never sees.
	// Token/cost limits get zero counters: the probe's usage is not parsed
	// here (its whole response is capped at probeBodyCap), and undercounting
	// a few dozen tokens is the honest bound. nil-safe when no quota
	// Registry is wired up or the endpoint carries no quota, like chargeQuota.
	ChargeResponse(rt.Quota, ep, quota.Counters{}, 0, time.Now())
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, probeBodyCap))

	if resp.StatusCode >= 400 {
		class := ad.ClassifyError(resp.StatusCode, respBody)
		if class == core.ErrContent || class == core.ErrClient || class == core.ErrContextLimit || class == core.ErrQuirk {
			// Request-specific outcomes — the probe prompt itself got
			// flagged/rejected/(implausibly) overflowed the window or tripped a
			// vendor protocol quirk — say nothing about the endpoint's health,
			// same rule tryOne applies to these classes for real traffic.
			// ErrContextLimit/ErrQuirk are essentially unreachable here in
			// practice (probe.Request's body is a fixed few dozen tokens),
			// included for consistency rather than a live gap.
			rt.Health.ReportNeutral(key)
			rt.logf("%s, status=%d, class=%s, dur=%s (no cooldown)", logPrefix, resp.StatusCode, class, fmtDur(dur))
			return
		}
		cd := rt.Health.ReportFailure(key, class, parseRetryAfter(resp.Header), time.Now())
		rt.logf("%s, status=%d, class=%s, dur=%s, cooldown=%s", logPrefix, resp.StatusCode, class, fmtDur(dur), cd)
		return
	}

	// 2xx: the endpoint answered — that alone is enough to let it out of
	// cooldown, but only as probe-evidence: ReportProbeSuccess decays fails
	// by one instead of clearing, so a flapping endpoint's backoff survives
	// the small request that passes where real traffic fails. Whether it
	// echoed the nonce is logged for observability only; it never turns a
	// reachable, responding endpoint back into a failure. Deliberately more
	// lenient than `vmr diagnose`, which does warn on a missing echo —
	// diagnose is a one-shot human check, this is a background health signal
	// that must not flap on a borderline vendor.
	rt.Health.ReportProbeSuccess(key)
	rt.logf("%s, status=%d, echoed=%v, dur=%s", logPrefix, resp.StatusCode, probe.Echoed(respBody, nonce), fmtDur(dur))
}
