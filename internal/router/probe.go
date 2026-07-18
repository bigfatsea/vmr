// Ver 2026-07-18 23:00, by Sonnet 5

// probe_mode: active's background half of Serve (see router.go) — a
// dedicated, lightweight request that verifies a half-open endpoint without
// making any real client wait on it.
package router

import (
	"context"
	"io"
	"time"

	"vmr/internal/adapter"
	"vmr/internal/core"
	"vmr/internal/probe"
)

// probeBodyCap bounds how much of a probe's response body gets read — small
// on purpose: probe.Request asks for at most a few dozen tokens back, and an
// error body this size is already generous (see errBodyCap in router.go for
// the same reasoning applied to real traffic).
const probeBodyCap = 32 << 10

// runProbe sends one probe.Request to ep and reports the outcome to the
// health registry, exactly the way tryOne reports a real attempt's outcome.
// The caller (Serve's candidate-building loop) has already claimed the
// single-flight slot via Health.Acquire before launching this as a
// goroutine — runProbe's only job is to make sure that claim always resolves
// via ReportSuccess/ReportFailure/ReportNeutral, whatever happens, or the
// endpoint would stay locked in "probing" forever (see Acquire's doc
// comment in internal/health/health.go).
func (rt *Router) runProbe(ep *core.Endpoint, snap *Snapshot) {
	key := ep.HealthKey()
	ad, ok := adapter.Get(ep.AdapterType)
	if !ok { // validated at config load; defensive only, mirrors tryOne
		rt.Health.ReportNeutral(key)
		return
	}

	body, nonce := probe.Request(ep.Model)
	creq := &core.CanonicalRequest{Model: ep.Model, Stream: false, Raw: body}
	ctx, cancel := context.WithTimeout(context.Background(), snap.Cfg.ProbeTimeout.D())
	defer cancel()

	req, _, err := ad.BuildRequest(ctx, ep, creq)
	if err != nil {
		// A build failure is about vmr's own request construction, not the
		// endpoint — same "says nothing about health" call tryOne makes for
		// this class of error, just without a health penalty since there was
		// never a real request behind this one to have failed either.
		rt.logf("probe ep=%s build_error=%v", ep.Name(), err)
		rt.Health.ReportNeutral(key)
		return
	}

	start := time.Now()
	resp, err := snap.clientFor(ep).Do(req)
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		cd := rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now())
		rt.logf("probe ep=%s net_error=%v dur=%s cooldown=%s", ep.Name(), err, dur, cd)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, probeBodyCap))

	if resp.StatusCode >= 400 {
		class := ad.ClassifyError(resp.StatusCode, respBody)
		if class == core.ErrContent || class == core.ErrClient {
			// Request-specific outcomes — the probe prompt itself got
			// flagged/rejected — say nothing about the endpoint's health,
			// same rule tryOne applies to these two classes for real traffic.
			rt.Health.ReportNeutral(key)
			rt.logf("probe ep=%s status=%d class=%s dur=%s (no cooldown)", ep.Name(), resp.StatusCode, class, dur)
			return
		}
		cd := rt.Health.ReportFailure(key, class, parseRetryAfter(resp.Header), time.Now())
		rt.logf("probe ep=%s status=%d class=%s dur=%s cooldown=%s", ep.Name(), resp.StatusCode, class, dur, cd)
		return
	}

	// 2xx: the endpoint answered — that alone is enough to mark it healthy
	// again. Whether it echoed the nonce is logged for observability only;
	// it never turns a reachable, responding endpoint back into a failure
	// (see docs/ActiveProbeAndFailoverFix_Sonnet5.md §1.1 for why the
	// runtime probe is deliberately more lenient here than `vmr diagnose`,
	// which does warn on a missing echo).
	rt.Health.ReportSuccess(key)
	rt.logf("probe ep=%s status=%d echoed=%v dur=%s", ep.Name(), resp.StatusCode, probe.Echoed(respBody, nonce), dur)
}
