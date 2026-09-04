// Ver 2026-08-13, by Opus 5
package report

import (
	"testing"

	"vmr/internal/i18n"
)

func TestQuotaExhaustionFinding_FiresAtThreshold(t *testing.T) {
	rep := &Report2{ProviderQuotas: []ProviderQuotaRow{
		{Provider: "acct1", Metric: "requests", Every: "1mo", Live: &LiveQuota{Pct: 90.0}},
	}}
	f := quotaExhaustionFinding(rep, i18n.EN)
	if f == nil {
		t.Fatal("expected a finding at exactly the threshold (90%)")
	}
	if f.Code != FindingProviderQuotaExhaustion {
		t.Errorf("Code = %q, want %q", f.Code, FindingProviderQuotaExhaustion)
	}
	if f.Implicated != "acct1" {
		t.Errorf("Implicated = %q, want acct1", f.Implicated)
	}
}

func TestQuotaExhaustionFinding_BelowThresholdDoesNotFire(t *testing.T) {
	rep := &Report2{ProviderQuotas: []ProviderQuotaRow{
		{Provider: "acct1", Live: &LiveQuota{Pct: 89.9}},
	}}
	if f := quotaExhaustionFinding(rep, i18n.EN); f != nil {
		t.Fatalf("expected no finding below threshold, got %+v", f)
	}
}

// TestQuotaExhaustionFinding_NoLiveDataDoesNotFire is the risk test
// guard: a missing real-time counter must never be treated as "0% used" or
// otherwise fabricate an alert — an estimate must never be the basis of one.
func TestQuotaExhaustionFinding_NoLiveDataDoesNotFire(t *testing.T) {
	rep := &Report2{ProviderQuotas: []ProviderQuotaRow{
		{Provider: "acct1", WindowConsumed: f64(999999), Live: nil},
	}}
	if f := quotaExhaustionFinding(rep, i18n.EN); f != nil {
		t.Fatalf("expected no finding when Live is nil, regardless of WindowConsumed, got %+v", f)
	}
}

// TestQuotaExhaustionFinding_HighButNotOutpacingPeriodDoesNotFire is the
// short-period guard: a healthy every:5h account that is 95% used at 98% of
// the way through its cycle is burning SLOWER than the period elapses, not
// faster — it must not alert every single run near cycle end.
func TestQuotaExhaustionFinding_HighButNotOutpacingPeriodDoesNotFire(t *testing.T) {
	rep := &Report2{ProviderQuotas: []ProviderQuotaRow{
		{Provider: "acct1", Live: &LiveQuota{Pct: 95.0}, PeriodElapsedPct: 98.0},
	}}
	if f := quotaExhaustionFinding(rep, i18n.EN); f != nil {
		t.Fatalf("expected no finding when used%% trails elapsed%%, got %+v", f)
	}
}

// TestQuotaExhaustionFinding_EqualUsedAndElapsedDoesNotFire locks in the
// strict-inequality boundary: Pct == PeriodElapsedPct is Headroom == 1
// exactly, not < 1, so it must not fire.
func TestQuotaExhaustionFinding_EqualUsedAndElapsedDoesNotFire(t *testing.T) {
	rep := &Report2{ProviderQuotas: []ProviderQuotaRow{
		{Provider: "acct1", Live: &LiveQuota{Pct: 95.0}, PeriodElapsedPct: 95.0},
	}}
	if f := quotaExhaustionFinding(rep, i18n.EN); f != nil {
		t.Fatalf("expected no finding when used%% equals elapsed%% (Headroom==1), got %+v", f)
	}
}

// TestQuotaExhaustionFinding_HighAndOutpacingPeriodFires is the mirror of
// the above: same absolute 95% but now genuinely outpacing period elapse
// (80%) — this is the "actually alarming" case and must still fire.
func TestQuotaExhaustionFinding_HighAndOutpacingPeriodFires(t *testing.T) {
	rep := &Report2{ProviderQuotas: []ProviderQuotaRow{
		{Provider: "acct1", Live: &LiveQuota{Pct: 95.0}, PeriodElapsedPct: 80.0},
	}}
	if f := quotaExhaustionFinding(rep, i18n.EN); f == nil {
		t.Fatal("expected a finding when used%% genuinely outpaces elapsed%%")
	}
}

func TestQuotaExhaustionFinding_PicksWorstTieBreaksByName(t *testing.T) {
	rep := &Report2{ProviderQuotas: []ProviderQuotaRow{
		{Provider: "b-acct", Live: &LiveQuota{Pct: 95.0}},
		{Provider: "a-acct", Live: &LiveQuota{Pct: 99.0}},
		{Provider: "c-acct", Live: &LiveQuota{Pct: 99.0}}, // tie with a-acct at the max
	}}
	f := quotaExhaustionFinding(rep, i18n.EN)
	if f == nil || f.Implicated != "a-acct" {
		t.Fatalf("expected the tie to resolve to a-acct (alphabetically first), got %+v", f)
	}
}

func TestQuotaExhaustionFinding_EmptyProviderQuotas(t *testing.T) {
	if f := quotaExhaustionFinding(&Report2{}, i18n.EN); f != nil {
		t.Fatalf("expected no finding with no ProviderQuotas at all, got %+v", f)
	}
}

// TestQuotaExhaustionFinding_ModelScopeInImplicated is the F8 lock-in: a
// per-model Limit's row must name its model in Implicated — "acct1 (gpt-4o)"
// — so an operator can tell a single model's exhaustion from a whole-account
// one.
func TestQuotaExhaustionFinding_ModelScopeInImplicated(t *testing.T) {
	rep := &Report2{ProviderQuotas: []ProviderQuotaRow{
		{Provider: "acct1", Models: []string{"gpt-4o"}, Metric: "cost", Every: "1d",
			Live: &LiveQuota{Pct: 95.0}, PeriodElapsedPct: 50.0},
	}}
	f := quotaExhaustionFinding(rep, i18n.EN)
	if f == nil || f.Implicated != "acct1 (gpt-4o)" {
		t.Fatalf("Implicated = %+v, want acct1 (gpt-4o)", f)
	}
}

// TestQuotaExhaustionFinding_SharedLimitNoModelScope: a shared Limit (Models
// empty) must keep the bare provider name — no empty parentheses.
func TestQuotaExhaustionFinding_SharedLimitNoModelScope(t *testing.T) {
	rep := &Report2{ProviderQuotas: []ProviderQuotaRow{
		{Provider: "acct1", Metric: "requests", Every: "1mo",
			Live: &LiveQuota{Pct: 95.0}, PeriodElapsedPct: 50.0},
	}}
	f := quotaExhaustionFinding(rep, i18n.EN)
	if f == nil || f.Implicated != "acct1" {
		t.Fatalf("Implicated = %+v, want plain acct1", f)
	}
}

// TestQuotaExhaustionFinding_SameProviderTieBreaksByModel: two per-model
// rows of one provider tied at the max must resolve by model name (render
// order across runs must be deterministic — buildProviderQuotaRows' own sort
// uses the same rule).
func TestQuotaExhaustionFinding_SameProviderTieBreaksByModel(t *testing.T) {
	rep := &Report2{ProviderQuotas: []ProviderQuotaRow{
		{Provider: "acct1", Models: []string{"o1"}, Live: &LiveQuota{Pct: 95.0}},
		{Provider: "acct1", Models: []string{"gpt-4o"}, Live: &LiveQuota{Pct: 95.0}},
	}}
	f := quotaExhaustionFinding(rep, i18n.EN)
	if f == nil || f.Implicated != "acct1 (gpt-4o)" {
		t.Fatalf("Implicated = %+v, want acct1 (gpt-4o) (alphabetically first model)", f)
	}
}

func TestQuotaExhaustionFinding_ZH(t *testing.T) {
	rep := &Report2{ProviderQuotas: []ProviderQuotaRow{
		{Provider: "acct1", Metric: "tokens", Every: "1mo", Live: &LiveQuota{Pct: 95.0}},
	}}
	f := quotaExhaustionFinding(rep, i18n.ZH)
	if f == nil || f.Finding != "额度即将耗尽" {
		t.Fatalf("expected zh title, got %+v", f)
	}
}

// TestBuildFindings_IncludesQuotaExhaustion locks in the wiring from
// buildFindings itself, not just the detector in isolation.
func TestBuildFindings_IncludesQuotaExhaustion(t *testing.T) {
	rep := &Report2{ProviderQuotas: []ProviderQuotaRow{
		{Provider: "acct1", Live: &LiveQuota{Pct: 95.0}},
	}}
	findings := buildFindings(rep, i18n.EN)
	found := false
	for _, f := range findings {
		if f.Code == FindingProviderQuotaExhaustion {
			found = true
		}
	}
	if !found {
		t.Fatalf("buildFindings did not include the quota exhaustion finding: %+v", findings)
	}
}
