// Ver 2026-09-15

// /status's alerts[] contract (console-unification G2, contracts.md §2.1):
// the three sources trigger/don't-trigger as specified, severity grading,
// the error-first/kind+ref ordering, the per-account quota merge, and the
// content discipline — only actionable state, never rolling statistics.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/quota"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

// buildAlerts is the common harness: parse yaml, install a snapshot on a
// fresh router (quota registry wired), return the server + router + snapshot.
func buildAlerts(t *testing.T, yaml string) (*Server, *router.Router, *router.Snapshot) {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	rt.Quota = quota.NewRegistry("")
	rt.Install(snap)
	return New(rt, nil), rt, snap
}

// fetchAlerts decodes the alerts[] key (and its presence) from one /status
// GET against the router's real handler.
func fetchAlerts(t *testing.T, rt *router.Router) ([]statusAlert, bool) {
	t.Helper()
	ts := httptest.NewServer(New(rt, nil).Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Alerts []statusAlert `json:"alerts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Alerts, out.Alerts != nil
}

func TestAlerts_NoneWhenAllHealthy(t *testing.T) {
	s, rt, snap := buildAlerts(t, `
listen: 127.0.0.1:18810
providers:
  - {name: p1, base_url: {openai-completions: http://127.0.0.1:1}, api_key: k1}
models:
  vm:
    endpoints: {openai-completions: [{providers: [p1], models: [m1]}]}
`)
	if got := s.statusAlerts(snap, time.Now(), rt.QuotaStatus()); len(got) != 0 {
		t.Fatalf("alerts = %v, want none (healthy endpoint, clean config, no quota)", got)
	}
	if alerts, present := fetchAlerts(t, rt); present {
		t.Fatalf("alerts key present (%v), want omitted when nothing is wrong", alerts)
	}
}

func TestAlerts_ConfigIssue(t *testing.T) {
	// A non-loopback listen without api_keys is a guaranteed Check() warning
	// (config.checkListenExposure) — the cheapest deterministic trigger.
	s, rt, snap := buildAlerts(t, `
listen: 0.0.0.0:18811
providers:
  - {name: p1, base_url: {openai-completions: http://127.0.0.1:1}, api_key: k1}
models:
  vm:
    endpoints: {openai-completions: [{providers: [p1], models: [m1]}]}
`)
	got := s.statusAlerts(snap, time.Now(), rt.QuotaStatus())
	if len(got) != 1 {
		t.Fatalf("alerts = %v, want exactly the one config issue", got)
	}
	a := got[0]
	if a.Severity != alertSeverityWarning || a.Kind != alertKindConfig {
		t.Fatalf("config alert = %+v, want severity=warning kind=config", a)
	}
	if a.Ref != s.inst.configPath {
		t.Fatalf("config alert ref = %q, want the config path %q", a.Ref, s.inst.configPath)
	}
	if !strings.Contains(a.Message, "open proxy") {
		t.Fatalf("config alert message = %q, want the Check() issue text", a.Message)
	}
}

// TestAlerts_EndpointCooldownAndHalfOpen pins both halves of the endpoint
// rule: an ACTIVE cooldown alerts (with the causal message), and the
// half-open state that follows — failures on record, cooldown expired — is
// the normal recovery path and must NOT alert.
func TestAlerts_EndpointCooldownAndHalfOpen(t *testing.T) {
	s, rt, snap := buildAlerts(t, `
listen: 127.0.0.1:18812
providers:
  - {name: p1, base_url: {openai-completions: http://127.0.0.1:1}, api_key: k1secret}
models:
  vm:
    endpoints: {openai-completions: [{providers: [p1], models: [m1]}]}
`)
	now := time.Now()
	ep := snap.Models["openai-completions"]["vm"].Endpoints[0]

	// Half-open: cooldown expired, failures on record → recovery in progress.
	rt.Health.ReportFailure(ep.HealthKey(), core.ErrRateLimit, 0, now.Add(-2*time.Minute))
	if got := s.statusAlerts(snap, now, nil); len(got) != 0 {
		t.Fatalf("half-open alerts = %v, want none (half-open is the recovery path)", got)
	}

	// Active cooldown → one endpoint warning; ref is the identity triple and
	// the message carries the causal pair (fails + last error class).
	rt.Health.ReportFailure(ep.HealthKey(), core.ErrRateLimit, 0, now)
	got := s.statusAlerts(snap, now, nil)
	if len(got) != 1 {
		t.Fatalf("alerts = %v, want exactly one endpoint cooldown alert", got)
	}
	a := got[0]
	if a.Severity != alertSeverityWarning || a.Kind != alertKindEndpoint {
		t.Fatalf("endpoint alert = %+v, want severity=warning kind=endpoint", a)
	}
	// Plain api_key: key_label is the key's 6-char tail (keyTailLabel).
	if want := "p1:" + ep.KeyLabel + ":m1"; a.Ref != want {
		t.Fatalf("endpoint alert ref = %q, want %q (provider:key_label:model)", a.Ref, want)
	}
	if st := rt.Health.Status(ep.HealthKey(), now); st.Fails != 2 || st.LastError != "rate_limit" {
		t.Fatalf("health state = %+v, want fails=2 last_error=rate_limit", st)
	}
	if !strings.Contains(a.Message, "2 consecutive failures") || !strings.Contains(a.Message, "rate_limit") {
		t.Fatalf("endpoint alert message = %q, want fails count + last error class", a.Message)
	}
}

func TestAlerts_QuotaSeverityAndMerge(t *testing.T) {
	// One account, two limits: a monthly bucket near-exhausted (warning) and
	// a daily gate blown (error) — one merged row, error severity, both
	// limits named in the message.
	s, rt, snap := buildAlerts(t, `
listen: 127.0.0.1:18813
providers:
  - name: p1
    base_url: {openai-completions: http://127.0.0.1:1}
    api_key: k1
    quota:
      limits:
        - {metric: requests, every: 30d, since: 2026-01-01, amount: 100}
        - {metric: requests, every: 1d, since: 2026-01-01, amount: 10}
models:
  vm:
    endpoints: {openai-completions: [{providers: [p1], models: [m1]}]}
`)
	now := time.Now()
	limits := snap.Models["openai-completions"]["vm"].Endpoints[0].Quota.Limits
	rt.Quota.Charge("p1", quota.LimitKey(limits[0], "m1"), quota.PeriodStart(limits[0], now), quota.Counters{Requests: 95}, 0) // 95% → warning
	rt.Quota.Charge("p1", quota.LimitKey(limits[1], "m1"), quota.PeriodStart(limits[1], now), quota.Counters{Requests: 10}, 0) // 100% → error

	got := s.statusAlerts(snap, now, rt.QuotaStatus())
	if len(got) != 1 {
		t.Fatalf("alerts = %v, want ONE merged row for the account", got)
	}
	a := got[0]
	if a.Severity != alertSeverityError || a.Kind != alertKindQuota || a.Ref != "p1" {
		t.Fatalf("quota alert = %+v, want severity=error kind=quota ref=p1", a)
	}
	for _, frag := range []string{"30d", "1d", "95.00%", "100.00%"} {
		if !strings.Contains(a.Message, frag) {
			t.Fatalf("quota alert message %q missing %q — both limits must be listed", a.Message, frag)
		}
	}
}

func TestAlerts_QuotaThresholds(t *testing.T) {
	for _, tc := range []struct {
		name      string
		requests  float64
		wantCount int
		wantSev   string
	}{
		{"below 90%: quiet", 89, 0, ""},
		{"at 90%: warning", 90, 1, alertSeverityWarning},
		{"at 100%: error", 100, 1, alertSeverityError},
		{"over 100%: error", 105, 1, alertSeverityError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, rt, snap := buildAlerts(t, `
listen: 127.0.0.1:18814
providers:
  - name: p1
    base_url: {openai-completions: http://127.0.0.1:1}
    api_key: k1
    quota:
      limits: [{metric: requests, every: 30d, since: 2026-01-01, amount: 100}]
models:
  vm:
    endpoints: {openai-completions: [{providers: [p1], models: [m1]}]}
`)
			now := time.Now()
			l := snap.Models["openai-completions"]["vm"].Endpoints[0].Quota.Limits[0]
			rt.Quota.Charge("p1", quota.LimitKey(l, "m1"), quota.PeriodStart(l, now), quota.Counters{Requests: tc.requests}, 0)

			got := s.statusAlerts(snap, now, rt.QuotaStatus())
			if len(got) != tc.wantCount {
				t.Fatalf("alerts = %v, want %d", got, tc.wantCount)
			}
			if tc.wantCount == 1 && got[0].Severity != tc.wantSev {
				t.Fatalf("severity = %q, want %q", got[0].Severity, tc.wantSev)
			}
		})
	}
}

// TestAlerts_Ordering pins the contract's order: errors first, then warnings
// by kind (config < endpoint < quota), ties broken by ref.
func TestAlerts_Ordering(t *testing.T) {
	s, rt, snap := buildAlerts(t, `
listen: 0.0.0.0:18816
providers:
  - name: p1
    base_url: {openai-completions: http://127.0.0.1:1}
    api_key: k1
    quota:
      limits: [{metric: requests, every: 30d, since: 2026-01-01, amount: 100}]
  - {name: p2, base_url: {openai-completions: http://127.0.0.1:1}, api_key: k2}
models:
  vm:
    endpoints: {openai-completions: [{providers: [p1], models: [m1]}, {providers: [p2], models: [m1]}]}
`)
	now := time.Now()
	// Two cooled-down endpoints on different providers (refs p1:... and
	// p2:...), the config issue the exposed listen always produces, and a
	// blown quota on p1 (error) — every severity/kind in one response.
	for _, p := range []string{"p2", "p1"} { // report p2 first: input order must not leak past the sort
		ep := findEndpoint(t, snap, p, "m1")
		rt.Health.ReportFailure(ep.HealthKey(), core.ErrRateLimit, 0, now)
	}
	l := findEndpoint(t, snap, "p1", "m1").Quota.Limits[0]
	rt.Quota.Charge("p1", quota.LimitKey(l, "m1"), quota.PeriodStart(l, now), quota.Counters{Requests: 100}, 0)

	got := s.statusAlerts(snap, now, rt.QuotaStatus())
	if len(got) != 4 {
		t.Fatalf("alerts = %v, want 1 quota error + 2 endpoint warnings + 1 config warning", got)
	}
	for i, w := range []struct{ sev, kind, ref string }{
		{alertSeverityError, alertKindQuota, "p1"},  // errors first
		{alertSeverityWarning, alertKindConfig, ""}, // then warnings by kind: config
		{alertSeverityWarning, alertKindEndpoint, "p1:k1:m1"},
		{alertSeverityWarning, alertKindEndpoint, "p2:k2:m1"},
	} {
		if got[i].Severity != w.sev || got[i].Kind != w.kind || got[i].Ref != w.ref {
			t.Fatalf("alerts[%d] = %q/%q ref %q, want %q/%q ref %q (full: %v)", i, got[i].Severity, got[i].Kind, got[i].Ref, w.sev, w.kind, w.ref, got)
		}
	}
}

// TestAlerts_RollingStatsNeverAlert pins the content discipline's negative
// half: accumulated traffic counters (the rolling-statistic family) must not
// surface as alerts. A busy-but-healthy server — thousands of requests and
// errors recorded, every endpoint healthy — stays quiet.
func TestAlerts_RollingStatsNeverAlert(t *testing.T) {
	s, rt, snap := buildAlerts(t, `
listen: 127.0.0.1:18817
providers:
  - {name: p1, base_url: {openai-completions: http://127.0.0.1:1}, api_key: k1}
models:
  vm:
    endpoints: {openai-completions: [{providers: [p1], models: [m1]}]}
`)
	for i := 0; i < 1000; i++ {
		rt.Telemetry.RecordRequest(core.ProtocolOpenAICompletions)
		rt.Telemetry.RecordOutcome(false, false) // a full day of errors
		rt.Telemetry.RecordTokens(100, 0, 0, 0, 200)
	}
	if got := s.statusAlerts(snap, time.Now(), nil); len(got) != 0 {
		t.Fatalf("alerts = %v, want none — rolling statistics (24h error counts) are not actionable state", got)
	}
}

func findEndpoint(t *testing.T, snap *router.Snapshot, provider, model string) *core.Endpoint {
	t.Helper()
	for _, byName := range snap.Models {
		for _, route := range byName {
			for _, ep := range route.Endpoints {
				if ep.Provider == provider && ep.Model == model {
					return ep
				}
			}
		}
	}
	t.Fatalf("endpoint %s/%s not found", provider, model)
	return nil
}
