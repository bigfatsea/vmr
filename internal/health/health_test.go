// Ver 2026-07-07 02:25, by Fable 5
package health

import (
	"testing"
	"time"

	"vmr/internal/core"
)

var t0 = time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)

func TestTransientBackoffCurve(t *testing.T) {
	t.Parallel()
	r := New()
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	for i, w := range want {
		if got := r.ReportFailure("e", core.ErrTransient, 0, t0); got != w {
			t.Errorf("failure #%d: cooldown %v, want %v", i+1, got, w)
		}
	}
	// Cap at 5min.
	for i := 0; i < 20; i++ {
		r.ReportFailure("e", core.ErrTransient, 0, t0)
	}
	if got := r.ReportFailure("e", core.ErrTransient, 0, t0); got != 5*time.Minute {
		t.Errorf("cap: %v", got)
	}
}

func TestAuthAndEndpointLongCooldown(t *testing.T) {
	t.Parallel()
	r := New()
	if got := r.ReportFailure("a", core.ErrAuth, 0, t0); got != 10*time.Minute {
		t.Errorf("auth cooldown: %v", got)
	}
	if got := r.ReportFailure("b", core.ErrEndpoint, 0, t0); got != 10*time.Minute {
		t.Errorf("endpoint cooldown: %v", got)
	}
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
	if got := r.ReportFailure("e", core.ErrTransient, 0, after); got != 4*time.Second {
		t.Errorf("probe failure should deepen backoff: %v", got)
	}
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
	if got := r.ReportFailure("e", core.ErrTransient, 0, after); got != 4*time.Second {
		t.Errorf("neutral must not deepen backoff: %v", got)
	}
}

func TestSuccessResets(t *testing.T) {
	t.Parallel()
	r := New()
	for i := 0; i < 5; i++ {
		r.ReportFailure("e", core.ErrTransient, 0, t0)
	}
	r.ReportSuccess("e")
	if got := r.ReportFailure("e", core.ErrTransient, 0, t0); got != 2*time.Second {
		t.Errorf("backoff should reset after success: %v", got)
	}
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
