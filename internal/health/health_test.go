// Ver 2026-07-07 02:25, by Fable 5
package health

import (
	"fmt"
	"testing"
	"time"

	"vmr/internal/core"
)

var t0 = time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)

// wantJitter asserts d is within backoff's ±10% jitter bounds around want.
// backoff is deliberately jittered (R12), so cooldown assertions must be
// ranges, never exact values.
func wantJitter(t *testing.T, what string, d, want time.Duration) {
	t.Helper()
	if d < want-want/10 || d > want+want/10 {
		t.Errorf("%s: got %v, want %v ±10%%", what, d, want)
	}
}

func TestTransientBackoffCurve(t *testing.T) {
	t.Parallel()
	r := New()
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	for i, w := range want {
		got := r.ReportFailure("e", core.ErrTransient, 0, t0)
		wantJitter(t, fmt.Sprintf("failure #%d", i+1), got, w)
	}
	// Cap at 5min.
	for i := 0; i < 20; i++ {
		r.ReportFailure("e", core.ErrTransient, 0, t0)
	}
	wantJitter(t, "cap", r.ReportFailure("e", core.ErrTransient, 0, t0), 5*time.Minute)
}

func TestAuthAndEndpointLongCooldown(t *testing.T) {
	t.Parallel()
	r := New()
	wantJitter(t, "auth cooldown", r.ReportFailure("a", core.ErrAuth, 0, t0), 10*time.Minute)
	wantJitter(t, "endpoint cooldown", r.ReportFailure("b", core.ErrEndpoint, 0, t0), 10*time.Minute)
}

func TestRetryAfterHonored(t *testing.T) {
	t.Parallel()
	r := New()
	if got := r.ReportFailure("e", core.ErrRateLimit, 42*time.Second, t0); got != 42*time.Second {
		t.Errorf("retry-after: %v", got)
	}
	if r.Available("e", t0.Add(41*time.Second)) {
		t.Error("should still be cooling down")
	}
	if !r.Available("e", t0.Add(43*time.Second)) {
		t.Error("should be half-open after cooldown expiry")
	}
}

func TestHalfOpenSingleFlightProbe(t *testing.T) {
	t.Parallel()
	r := New()
	r.ReportFailure("e", core.ErrTransient, 0, t0)
	after := t0.Add(3 * time.Second) // cooldown (2s) expired → half-open

	if !r.Acquire("e", after) {
		t.Fatal("first caller should win the probe slot")
	}
	if r.Acquire("e", after) {
		t.Error("second caller must be rejected while probe is in flight")
	}
	if r.Available("e", after) {
		t.Error("Available should report false while probe is in flight")
	}

	// Probe fails → deeper cooldown, probe slot released.
	wantJitter(t, "probe failure deepens backoff", r.ReportFailure("e", core.ErrTransient, 0, after), 4*time.Second)
	if r.Acquire("e", after.Add(1*time.Second)) {
		t.Error("must be cooling down again after failed probe")
	}

	// Cooldown expires again, probe succeeds → fully healthy, no probe gate.
	again := after.Add(5 * time.Second)
	if !r.Acquire("e", again) {
		t.Fatal("probe after second cooldown")
	}
	r.ReportSuccess("e")
	if !r.Acquire("e", again) || !r.Acquire("e", again) {
		t.Error("healthy endpoint must admit concurrent requests")
	}
}

// TestClassify locks in Classify's single-lock replacement for the router's
// former Status(key,now).Fails>0 + Acquire(key,now)/Available(key,now)
// two-call sequence: same outcomes, one call.
func TestClassify(t *testing.T) {
	t.Parallel()
	r := New()

	if available, needsProbe := r.Classify("unknown", t0); !available || needsProbe {
		t.Errorf("never-seen key: available=%v needsProbe=%v, want true,false", available, needsProbe)
	}

	r.ReportFailure("e", core.ErrTransient, 0, t0)
	if available, needsProbe := r.Classify("e", t0.Add(time.Second)); available || needsProbe {
		t.Errorf("still cooling down: available=%v needsProbe=%v, want false,false", available, needsProbe)
	}

	after := t0.Add(3 * time.Second) // cooldown (2s) expired → half-open
	available, needsProbe := r.Classify("e", after)
	if available || !needsProbe {
		t.Errorf("first caller after cooldown: available=%v needsProbe=%v, want false,true", available, needsProbe)
	}
	available, needsProbe = r.Classify("e", after)
	if available || needsProbe {
		t.Errorf("second caller while probe in flight: available=%v needsProbe=%v, want false,false", available, needsProbe)
	}

	r.ReportSuccess("e")
	if available, needsProbe := r.Classify("e", after); !available || needsProbe {
		t.Errorf("healthy after ReportSuccess: available=%v needsProbe=%v, want true,false", available, needsProbe)
	}
}

func TestTransientHonorsRetryAfter(t *testing.T) {
	t.Parallel()
	r := New()
	// OpenRouter sends Retry-After on 503 (classified transient).
	if got := r.ReportFailure("e", core.ErrTransient, 30*time.Second, t0); got != 30*time.Second {
		t.Errorf("transient retry-after: %v", got)
	}
}

func TestReportNeutralReleasesProbeOnly(t *testing.T) {
	t.Parallel()
	r := New()
	r.ReportFailure("e", core.ErrTransient, 0, t0) // cooldown 2s, fails=1
	after := t0.Add(3 * time.Second)               // half-open

	if !r.Acquire("e", after) {
		t.Fatal("should win probe slot")
	}
	// Probe hit a content flag: neutral outcome.
	r.ReportNeutral("e")
	// Probe slot released — the next caller may probe again immediately…
	if !r.Acquire("e", after) {
		t.Error("probe slot must be released after neutral report")
	}
	// …and failure count was not deepened: a subsequent failure backs off to
	// 4s (fails=2), not 8s (which fails=3 would give).
	wantJitter(t, "neutral must not deepen backoff", r.ReportFailure("e", core.ErrTransient, 0, after), 4*time.Second)
}

func TestSuccessResets(t *testing.T) {
	t.Parallel()
	r := New()
	for i := 0; i < 5; i++ {
		r.ReportFailure("e", core.ErrTransient, 0, t0)
	}
	r.ReportSuccess("e")
	wantJitter(t, "backoff should reset after success", r.ReportFailure("e", core.ErrTransient, 0, t0), 2*time.Second)
}

func TestStatus(t *testing.T) {
	t.Parallel()
	r := New()
	if st := r.Status("never-seen", t0); !st.Available || st.Fails != 0 {
		t.Errorf("unknown endpoint should be available: %+v", st)
	}
	r.ReportFailure("e", core.ErrAuth, 0, t0)
	st := r.Status("e", t0)
	if st.Available || st.Fails != 1 || st.LastError != "auth" {
		t.Errorf("status: %+v", st)
	}
}

func TestStatusReportsProbing(t *testing.T) {
	t.Parallel()
	r := New()
	r.ReportFailure("e", core.ErrTransient, 0, t0)
	after := t0.Add(3 * time.Second) // cooldown (2s) expired → half-open

	if st := r.Status("e", after); st.Probing {
		t.Errorf("half-open with no probe in flight should report Probing=false: %+v", st)
	}
	if !r.Acquire("e", after) {
		t.Fatal("should win the probe slot")
	}
	if st := r.Status("e", after); !st.Probing {
		t.Errorf("with the probe slot held, Status should report Probing=true: %+v", st)
	}
	r.ReportSuccess("e")
	if st := r.Status("e", after); st.Probing {
		t.Errorf("after the probe resolves, Probing must be released: %+v", st)
	}
}

// TestStatusServingDistinctFromAvailableWhenHalfOpen pins the fix for a
// finding from the 2026-08-12 review (VMR_项目全面Review报告 B2): a
// half-open endpoint (cooldown expired, Fails>0) reported Available==true
// in /status even though Classify — what the router actually
// consults — treats it as unavailable to real traffic that round (only a
// background probe gets dispatched). Serving is the field that must track
// Classify's answer; Available is kept at its narrower "cooldown expired"
// meaning for backward compatibility (see Status's doc comment).
func TestStatusServingDistinctFromAvailableWhenHalfOpen(t *testing.T) {
	t.Parallel()
	r := New()
	r.ReportFailure("e", core.ErrTransient, 0, t0)
	after := t0.Add(3 * time.Second) // cooldown (2s) expired → half-open

	st := r.Status("e", after)
	if !st.Available {
		t.Errorf("half-open endpoint: Available should still be true (cooldown expired), got %+v", st)
	}
	if st.Serving {
		t.Errorf("half-open endpoint: Serving should be false (real traffic isn't routed here yet), got %+v", st)
	}
	avail, needsProbe := r.Classify("e", after)
	if avail {
		t.Fatalf("test setup: Classify should agree the endpoint isn't available to real traffic, got available=%v needsProbe=%v", avail, needsProbe)
	}

	r.ReportSuccess("e")
	st = r.Status("e", after)
	if !st.Available || !st.Serving {
		t.Errorf("recovered endpoint: both Available and Serving should be true, got %+v", st)
	}
}

func TestRetryAfterCappedAtOneHour(t *testing.T) {
	t.Parallel()
	r := New()
	// Retry-After is upstream-controlled input; a bogus huge value must not
	// lock the endpoint out beyond the long-cooldown cap (1h).
	if got := r.ReportFailure("e", core.ErrRateLimit, 48*time.Hour, t0); got != time.Hour {
		t.Errorf("retry-after must be capped at 1h, got %v", got)
	}
	if !r.Available("e", t0.Add(time.Hour+time.Second)) {
		t.Error("should be half-open after the capped cooldown expires")
	}
}

// TestCurveSwitchResetsFailureDepth pins R04: fails counts consecutive
// failures under the *current* backoff curve. Five transient failures leave
// the transient curve at depth 5 (32s), but the first auth failure must
// start the long curve at its base — the old shared counter took that 401
// at depth 6, straight to the 1h cap. Same in the other direction.
func TestCurveSwitchResetsFailureDepth(t *testing.T) {
	t.Parallel()
	r := New()
	for i := 0; i < 5; i++ {
		r.ReportFailure("e", core.ErrTransient, 0, t0)
	}
	wantJitter(t, "first auth after 5 transient failures", r.ReportFailure("e", core.ErrAuth, 0, t0), 10*time.Minute)
	if got := r.ReportFailure("e", core.ErrAuth, 0, t0); got >= time.Hour {
		t.Errorf("auth run should deepen from 10min, not start near the cap: %v", got)
	}
	wantJitter(t, "first transient after auth run", r.ReportFailure("e", core.ErrTransient, 0, t0), 2*time.Second)
}

// TestSameCurveStillDeepens is the regression half of R04: within one curve
// the depth is preserved across the switch-reset change.
func TestSameCurveStillDeepens(t *testing.T) {
	t.Parallel()
	r := New()
	wantJitter(t, "fail 1", r.ReportFailure("e", core.ErrAuth, 0, t0), 10*time.Minute)
	wantJitter(t, "fail 2", r.ReportFailure("e", core.ErrAuth, 0, t0), 20*time.Minute)
	wantJitter(t, "fail 3", r.ReportFailure("e", core.ErrEndpoint, 0, t0), 40*time.Minute)
}

// TestProbeSuccessDecaysNotClears pins R05: a probe's 2xx is weaker
// evidence than real traffic completing, so it decays fails by one instead
// of zeroing them; only a real ReportSuccess clears.
func TestProbeSuccessDecaysNotClears(t *testing.T) {
	t.Parallel()
	r := New()
	for i := 0; i < 3; i++ {
		r.ReportFailure("e", core.ErrTransient, 0, t0)
	}
	r.ReportProbeSuccess("e")
	if st := r.Status("e", t0); st.Fails != 2 {
		t.Fatalf("probe success must decay fails to 2, got %d", st.Fails)
	}
	// The endpoint left cooldown but stays half-open: real traffic still
	// isn't served until the count reaches zero.
	if st := r.Status("e", t0); st.Serving {
		t.Error("decayed endpoint must stay half-open (Serving=false)")
	}
	r.ReportSuccess("e")
	if st := r.Status("e", t0); st.Fails != 0 || !st.Serving {
		t.Fatalf("real success must clear fails: %+v", st)
	}
}

// TestFlappingEndpointKeepsBackoff pins R05's acceptance scenario: with
// probe successes and real failures alternating, the depth never returns to
// zero (no real success ever lands), so the cooldown never falls back to
// the shallowest step. The old clear-on-probe behavior reset a flapping
// endpoint to 2s forever.
func TestFlappingEndpointKeepsBackoff(t *testing.T) {
	t.Parallel()
	r := New()
	for i := 0; i < 3; i++ {
		r.ReportFailure("e", core.ErrTransient, 0, t0)
	}
	for i := 0; i < 5; i++ {
		r.ReportProbeSuccess("e")
		if st := r.Status("e", t0); st.Fails < 1 {
			t.Fatalf("iter %d: probe success cleared the failure depth", i)
		}
		got := r.ReportFailure("e", core.ErrTransient, 0, t0)
		if got < 4*time.Second {
			t.Fatalf("iter %d: cooldown %v fell back to the shallowest step", i, got)
		}
	}
}

// TestReleaseProbeReturnsSlotWithoutVerdict pins ReleaseProbe's contract:
// the slot is handed back, but the pending failure depth and cooldown are
// untouched — no verdict was implied.
func TestReleaseProbeReturnsSlotWithoutVerdict(t *testing.T) {
	t.Parallel()
	r := New()
	r.ReportFailure("e", core.ErrTransient, 0, t0) // fails=1, 2s cooldown
	after := t0.Add(3 * time.Second)               // half-open
	if !r.Acquire("e", after) {
		t.Fatal("should win probe slot")
	}
	r.ReleaseProbe("e")
	if st := r.Status("e", after); st.Probing {
		t.Error("probe slot must be released")
	}
	if st := r.Status("e", after); st.Fails != 1 {
		t.Errorf("ReleaseProbe must not touch fails, got %d", st.Fails)
	}
	if r.Available("e", t0.Add(1*time.Second)) {
		t.Error("ReleaseProbe must not clear the cooldown")
	}
	// A later verdict still lands on the untouched state: this failure
	// deepens to 4s (fails=2), not 8s (which a phantom extra failure would
	// give) — and it's not a curve switch, since nothing re-classified.
	wantJitter(t, "failure after released probe", r.ReportFailure("e", core.ErrTransient, 0, after), 4*time.Second)
}

// TestAllDeclaredErrorClassesHaveExplicitCurve pins S4's enumeration: every
// ErrorClass declared in core is asserted against its curve. A class added
// to core must be added to both this list and ReportFailure's explicit
// switch — the switch's default branch is out-of-enum values only.
func TestAllDeclaredErrorClassesHaveExplicitCurve(t *testing.T) {
	t.Parallel()
	long := map[core.ErrorClass]bool{core.ErrAuth: true, core.ErrEndpoint: true}
	for _, class := range []core.ErrorClass{
		core.ErrClient,
		core.ErrAuth,
		core.ErrRateLimit,
		core.ErrEndpoint,
		core.ErrTransient,
		core.ErrContent,
		core.ErrContextLimit,
		core.ErrQuirk,
	} {
		r := New()
		got := r.ReportFailure("e", class, 0, t0)
		if long[class] && got < 10*time.Minute-10*time.Minute/10 {
			t.Errorf("%v: cooldown %v, want the long curve", class, got)
		}
		if !long[class] && got > 5*time.Minute+5*time.Minute/10 {
			t.Errorf("%v: cooldown %v, want the transient curve", class, got)
		}
	}
}

// TestPruneRemovesOrphans pins health.Registry.Prune: state for keys not in
// keep is dropped, kept keys survive untouched.
func TestPruneRemovesOrphans(t *testing.T) {
	t.Parallel()
	r := New()
	r.ReportFailure("keep", core.ErrRateLimit, time.Minute, t0)
	r.ReportFailure("drop", core.ErrRateLimit, time.Minute, t0)
	if n := r.Prune(map[string]bool{"keep": true}); n != 1 {
		t.Fatalf("pruned=%d, want 1", n)
	}
	if r.Available("keep", t0) {
		t.Error("kept key must survive prune with its cooldown")
	}
	if !r.Available("drop", t0) || r.Status("drop", t0).Fails != 0 {
		t.Error("pruned key must behave like never-seen")
	}
	if n := r.Prune(map[string]bool{"keep": true}); n != 0 {
		t.Errorf("second prune removed %d, want 0", n)
	}
}

// TestMonotonicClockSemantics locks S3's three properties: cooldown
// arithmetic goes through the monotonic clock (ReportFailure's now.Add
// preserves the reading, Before compares monotonically), and a zero-value
// cooldownUntil never blocks — even against a wall-clock-only timestamp far
// in the past.
func TestMonotonicClockSemantics(t *testing.T) {
	t.Parallel()
	r := New()
	now := time.Now() // carries a monotonic reading
	r.ReportFailure("e", core.ErrRateLimit, 20*time.Millisecond, now)
	if r.Available("e", now) {
		t.Error("cooldown must be active at the failure instant itself")
	}
	if r.Available("e", time.Now()) {
		t.Error("cooldown must still be active immediately after")
	}
	time.Sleep(30 * time.Millisecond)
	if !r.Available("e", time.Now()) {
		t.Error("cooldown must expire by monotonic time")
	}
	r.ReportSuccess("e")
	// t0 is a wall-clock-only timestamp in the past; a zero cooldownUntil
	// must never compare as "in the future" against anything.
	if !r.Available("e", t0) {
		t.Error("zero cooldownUntil must never block")
	}
	if st := r.Status("e", t0); !st.Available || !st.CooldownUntil.IsZero() {
		t.Errorf("zero cooldownUntil must not surface in Status: %+v", st)
	}
}
