// Ver 2026-08-12 23:40, by Opus 5
package report

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vmr/internal/i18n"
)

func renderProvidersStr(rep *Report2, lang i18n.Lang) string {
	var b strings.Builder
	renderProviders(func(f string, a ...any) { b.WriteString(fmt.Sprintf(f, a...)) }, rep, lang)
	return b.String()
}

func TestRenderProvidersEmptySkipsSection(t *testing.T) {
	out := renderProvidersStr(&Report2{}, i18n.EN)
	if out != "" {
		t.Errorf("empty Providers must render nothing, got:\n%s", out)
	}
}

// The core scenario this feature exists for: an AFP/Token-Plan account with
// no resolvable $ price must still get a row — a dash in the cost column,
// never an omitted account.
func TestRenderProvidersNoPricingStillRenders(t *testing.T) {
	rep := &Report2{
		Providers: []ProviderRow{{Provider: "afp-account", Requests: 10, RequestsOK: 10, Models: []string{"m1"}}},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if !strings.Contains(out, "afp-account") {
		t.Errorf("must render the unpriced provider row:\n%s", out)
	}
	if strings.Contains(out, "$ Estimate") {
		t.Errorf("cost column must not appear when no provider has pricing:\n%s", out)
	}
}

func TestRenderProvidersWithQuotaAndCost(t *testing.T) {
	rep := &Report2{
		Pricing: &Pricing{Currency: "USD"},
		Providers: []ProviderRow{
			{Provider: "priced-quota", Requests: 5, RequestsOK: 5, CostEstimate: f64(1.23),
				Quota: []ProviderQuotaRef{{Metric: "tokens", Every: "1mo", Amount: 20000}}},
			{Provider: "no-quota", Requests: 2, RequestsOK: 2, CostEstimate: f64(0.1)},
		},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if !strings.Contains(out, "1.2300") {
		t.Errorf("must render the $ estimate:\n%s", out)
	}
	// The main table itself carries no quota column at all: a
	// declared quota only ever appears in the "Quota vs. Consumption"
	// sub-table, never duplicated here with a second formatter.
	if strings.Contains(out, "Quota Ref.") || strings.Contains(out, "tokens · 1mo") {
		t.Errorf("main table must never render a quota column, even when a provider has a quota:\n%s", out)
	}
}

// TestRenderProvidersTopErrorClassColumn locks in the main table's
// dominant-error-class column: the account's top error class (already
// aggregated into ErrorClasses) must render as a compact "class N(pct%)"
// cell, and "-" when the account had no failures at all.
func TestRenderProvidersTopErrorClassColumn(t *testing.T) {
	rep := &Report2{
		Providers: []ProviderRow{
			{Provider: "flaky", Requests: 10, RequestsOK: 6, Attempts: 10, Failed: 4,
				ErrorClasses: map[string]int{"rate_limit": 3, "transient": 1}},
			{Provider: "clean", Requests: 5, RequestsOK: 5},
		},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if !strings.Contains(out, "rate_limit 3(75.0%)") {
		t.Errorf("must render the dominant error class with its share of failures:\n%s", out)
	}
	if !strings.Contains(out, "| clean | 0 | 5 | 100.0% |") {
		t.Errorf("an account with no failures must render a plain dash for top error class:\n%s", out)
	}
}

func TestRenderProvidersNoQuotaAnywhereOmitsQuotaColumn(t *testing.T) {
	rep := &Report2{
		Providers: []ProviderRow{{Provider: "p1", Requests: 1, RequestsOK: 1}},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if strings.Contains(out, "Quota Ref.") {
		t.Errorf("quota column must not appear when no provider has a quota:\n%s", out)
	}
}

func TestRenderProvidersZH(t *testing.T) {
	rep := &Report2{
		Providers: []ProviderRow{{Provider: "p1", Requests: 1, RequestsOK: 1,
			Quota: []ProviderQuotaRef{{Metric: "tokens", Every: "1mo", Amount: 1000}}}},
	}
	out := renderProvidersStr(rep, i18n.ZH)
	if !strings.Contains(out, "§2.5 账户（Provider）消耗与额度") {
		t.Errorf("zh title missing:\n%s", out)
	}
	if strings.Contains(out, "额度参照") {
		t.Errorf("main table must not render a quota column in zh either:\n%s", out)
	}
}

// --- §2.5 额度与消耗对照 sub-table ---

func TestRenderProviderQuotaTable_AbsentWhenNoRows(t *testing.T) {
	out := renderProvidersStr(&Report2{
		Providers: []ProviderRow{{Provider: "p1", Requests: 1, RequestsOK: 1}},
	}, i18n.EN)
	if strings.Contains(out, "Quota vs. Consumption") {
		t.Errorf("sub-table must not render when ProviderQuotas is empty:\n%s", out)
	}
}

func TestRenderProviderQuotaTable_RendersLiveAndWindowSeparately(t *testing.T) {
	ps := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	pe := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rep := &Report2{
		Providers: []ProviderRow{{Provider: "acct1", Requests: 1, RequestsOK: 1}},
		ProviderQuotas: []ProviderQuotaRow{{
			Provider: "acct1", Metric: "requests", Every: "1mo", Amount: 18000,
			WindowConsumed:   f64(104),
			Live:             &LiveQuota{Used: 12240, Pct: 68.0},
			PeriodStart:      ps,
			PeriodEndsAt:     pe,
			PeriodElapsedPct: 71.0,
		}},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if !strings.Contains(out, "104") || !strings.Contains(out, "12240") {
		t.Errorf("must render both the window-consumed and live-used numbers:\n%s", out)
	}
	if !strings.Contains(out, "68.0%") || !strings.Contains(out, "71.0%") {
		t.Errorf("must render used%% and elapsed%% side by side:\n%s", out)
	}
	if !strings.Contains(out, "08-01 ~ 09-01") {
		t.Errorf("must render the period range:\n%s", out)
	}
	if !strings.Contains(out, "Window Consumed") || !strings.Contains(out, "recomputed") {
		t.Errorf("must render the window-consumed footnote:\n%s", out)
	}
}

func TestRenderProviderQuotaTable_LiveNilRendersDash(t *testing.T) {
	rep := &Report2{
		ProviderQuotas: []ProviderQuotaRow{{
			Provider: "acct1", Metric: "requests", Every: "1mo", Amount: 1000,
			WindowConsumed: f64(5), Live: nil,
		}},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if !strings.Contains(out, "| acct1 | requests | 5 | - |") {
		t.Errorf("Live == nil must render as a dash, not a fabricated number:\n%s", out)
	}
}

// TestRenderProviderQuotaTable_EstimatedPctAnnotatesLiveUsed is the degraded-estimate rule's
// render-layer lock-in: a period whose consumption came partly from a
// degraded estimate must show that share inline, not render identically to
// a fully usage-sniffed period.
func TestRenderProviderQuotaTable_EstimatedPctAnnotatesLiveUsed(t *testing.T) {
	rep := &Report2{
		ProviderQuotas: []ProviderQuotaRow{{
			Provider: "acct1", Metric: "tokens", Every: "1mo", Amount: 1000,
			WindowConsumed: f64(5),
			Live:           &LiveQuota{Used: 400, Pct: 40.0, EstimatedPct: 10.0},
		}},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if !strings.Contains(out, "400 (10.0% est.)") {
		t.Errorf("must annotate the live-used cell with its estimated share:\n%s", out)
	}
}

// TestRenderProviderQuotaTable_ZeroEstimatedPctNoAnnotation makes sure a
// fully usage-sniffed period (EstimatedPct == 0, the common case) renders
// plainly, without an empty or zero-value annotation cluttering every row.
func TestRenderProviderQuotaTable_ZeroEstimatedPctNoAnnotation(t *testing.T) {
	rep := &Report2{
		ProviderQuotas: []ProviderQuotaRow{{
			Provider: "acct1", Metric: "tokens", Every: "1mo", Amount: 1000,
			WindowConsumed: f64(5),
			Live:           &LiveQuota{Used: 400, Pct: 40.0, EstimatedPct: 0},
		}},
	}
	out := renderProvidersStr(rep, i18n.EN)
	// The footnote text itself legitimately mentions "est." as an
	// explanation — check the actual row, not the whole page, for the
	// "(N% est.)" annotation pattern.
	if !strings.Contains(out, "| acct1 | tokens | 5 | 400 |") {
		t.Errorf("live-used cell must render plainly (\"400\", no annotation) when EstimatedPct is 0:\n%s", out)
	}
}

// TestRenderProviderQuotaTable_WindowConsumedNilRendersDash is the
// render-layer lock-in: WindowConsumed == nil (unresolved cost pricing)
// must render "-", never a fabricated "0".
func TestRenderProviderQuotaTable_WindowConsumedNilRendersDash(t *testing.T) {
	rep := &Report2{
		ProviderQuotas: []ProviderQuotaRow{{
			Provider: "acct1", Metric: "cost", Every: "1mo", Amount: 100,
			WindowConsumed: nil, Live: nil,
		}},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if !strings.Contains(out, "| acct1 | cost | - | - |") {
		t.Errorf("nil WindowConsumed must render as a dash, not 0:\n%s", out)
	}
}

// TestRenderProviderQuotaTable_OverQuotaMarksStar is the lock-in:
// Pct is deliberately unclamped, so an over-quota account (>=100%) needs a
// visible flag, not a plain number indistinguishable from a healthy one.
func TestRenderProviderQuotaTable_OverQuotaMarksStar(t *testing.T) {
	rep := &Report2{
		ProviderQuotas: []ProviderQuotaRow{
			{Provider: "over", Metric: "tokens", Every: "1mo", Amount: 1000,
				WindowConsumed: f64(5), Live: &LiveQuota{Used: 1389, Pct: 138.9}},
			{Provider: "healthy", Metric: "tokens", Every: "1mo", Amount: 1000,
				WindowConsumed: f64(5), Live: &LiveQuota{Used: 680, Pct: 68.0}},
			{Provider: "exact", Metric: "tokens", Every: "1mo", Amount: 1000,
				WindowConsumed: f64(5), Live: &LiveQuota{Used: 1000, Pct: 100.0}},
		},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if !strings.Contains(out, "138.9%⭐") {
		t.Errorf("over-quota (138.9%%) must be marked with ⭐:\n%s", out)
	}
	if !strings.Contains(out, "100.0%⭐") {
		t.Errorf("exactly-100%% must also be marked (>= 100, not > 100):\n%s", out)
	}
	if strings.Contains(out, "68.0%⭐") {
		t.Errorf("a healthy under-quota account must not be marked:\n%s", out)
	}
	if !strings.Contains(out, "marks Used% >= 100%") {
		t.Errorf("must render the ⭐ footnote when at least one row is marked:\n%s", out)
	}
}

// TestRenderProviderQuotaTable_OverQuotaFootnoteAbsentWhenNoneFlagged pins
// the marker-footnote rule: the ⭐ footnote used to print unconditionally
// while ‡ and † were both gated on their marker actually appearing. A report where every account is
// healthy must not carry an explanation of a symbol it doesn't contain.
func TestRenderProviderQuotaTable_OverQuotaFootnoteAbsentWhenNoneFlagged(t *testing.T) {
	rep := &Report2{
		ProviderQuotas: []ProviderQuotaRow{
			{Provider: "healthy", Metric: "tokens", Every: "1mo", Amount: 1000,
				WindowConsumed: f64(5), Live: &LiveQuota{Used: 680, Pct: 68.0}},
		},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if strings.Contains(out, "⭐") {
		t.Errorf("must not render the ⭐ marker or its footnote when no row is over quota:\n%s", out)
	}
}

// TestRenderProviderQuotaTable_SourcePathAndCrossInstanceWarning is the source-provenance rule's
// lock-in: the sub-table must name its live-counter source path, and only
// add the cross-instance warning when Meta says the input logs are outside
// that counter's log_dir.
func TestRenderProviderQuotaTable_SourcePathAndCrossInstanceWarning(t *testing.T) {
	rep := &Report2{
		Meta:           Meta{QuotaJSONPath: "/home/x/.vmr/logs/vmr-quota.json", QuotaInputOutsideLogDir: true},
		ProviderQuotas: []ProviderQuotaRow{{Provider: "acct1", WindowConsumed: f64(5)}},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if !strings.Contains(out, "/home/x/.vmr/logs/vmr-quota.json") {
		t.Errorf("must render the live counter's source path:\n%s", out)
	}
	if !strings.Contains(out, "may be from a different machine") {
		t.Errorf("must render the cross-instance warning when Meta flags it:\n%s", out)
	}
}

// TestRenderProviderQuotaTable_NoCrossInstanceWarningWhenNotFlagged makes
// sure the warning is absent by default (QuotaInputOutsideLogDir false) —
// the common, non-mismatched case must not be alarmed unnecessarily.
func TestRenderProviderQuotaTable_NoCrossInstanceWarningWhenNotFlagged(t *testing.T) {
	rep := &Report2{
		Meta:           Meta{QuotaJSONPath: "/home/x/.vmr/logs/vmr-quota.json", QuotaInputOutsideLogDir: false},
		ProviderQuotas: []ProviderQuotaRow{{Provider: "acct1", WindowConsumed: f64(5)}},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if strings.Contains(out, "may be from a different machine") {
		t.Errorf("must not render the cross-instance warning when not flagged:\n%s", out)
	}
}

// TestRenderProviderQuotaTable_NoSourcePathLineWhenMetaEmpty covers the
// unwired case (cmd_report.go didn't set Meta, e.g. an isolated caller of
// Markdown) — must degrade to no source-path line, not a blank/broken one.
func TestRenderProviderQuotaTable_NoSourcePathLineWhenMetaEmpty(t *testing.T) {
	rep := &Report2{
		ProviderQuotas: []ProviderQuotaRow{{Provider: "acct1", WindowConsumed: f64(5)}},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if strings.Contains(out, "real-time counter is read from") {
		t.Errorf("must not render a source-path line when Meta.QuotaJSONPath is empty:\n%s", out)
	}
}

// TestRenderProviderQuotaTable_NoOverlapMarksDagger is the no-overlap rule's lock-in: a row
// whose report window and billing period share no time gets a † marker and
// the explanatory footnote appears; a normal row doesn't get either.
func TestRenderProviderQuotaTable_NoOverlapMarksDagger(t *testing.T) {
	rep := &Report2{
		ProviderQuotas: []ProviderQuotaRow{
			{Provider: "disjoint", WindowConsumed: f64(5), WindowNoOverlap: true},
			{Provider: "normal", WindowConsumed: f64(3), WindowNoOverlap: false},
		},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if !strings.Contains(out, "| disjoint | requests | 5† |") && !strings.Contains(out, "5†") {
		t.Errorf("must mark the disjoint row's Window Consumed cell with †:\n%s", out)
	}
	if strings.Contains(out, "3†") {
		t.Errorf("must not mark a normal (overlapping) row:\n%s", out)
	}
	if !strings.Contains(out, "shares NO time at all") {
		t.Errorf("must render the † footnote when at least one row is flagged:\n%s", out)
	}
}

// TestRenderProviderQuotaTable_NoOverlapFootnoteAbsentWhenNoneFlagged makes
// sure the explanatory footnote doesn't clutter reports where the marker
// never fires.
func TestRenderProviderQuotaTable_NoOverlapFootnoteAbsentWhenNoneFlagged(t *testing.T) {
	rep := &Report2{
		ProviderQuotas: []ProviderQuotaRow{{Provider: "normal", WindowConsumed: f64(3)}},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if strings.Contains(out, "shares NO time at all") {
		t.Errorf("must not render the † footnote when no row is flagged:\n%s", out)
	}
}

// TestRenderProviderQuotaTable_ConfigChangedMarksDoubleDagger is the config-changed rule's
// lock-in: a config-change-caused nil Live must render "-‡" (not a plain
// "-") and trigger the distinct explanatory footnote.
func TestRenderProviderQuotaTable_ConfigChangedMarksDoubleDagger(t *testing.T) {
	rep := &Report2{
		ProviderQuotas: []ProviderQuotaRow{
			{Provider: "changed", WindowConsumed: f64(5), Live: nil, LiveConfigChanged: true},
			{Provider: "stale", WindowConsumed: f64(3), Live: nil, LiveConfigChanged: false},
		},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if !strings.Contains(out, "-‡") {
		t.Errorf("must mark the config-changed row's live cells with -‡:\n%s", out)
	}
	if !strings.Contains(out, "keyed under the OLD config") {
		t.Errorf("must render the config-changed footnote when at least one row is flagged:\n%s", out)
	}
}

// TestRenderProviderQuotaTable_ConfigChangedFootnoteAbsentWhenNoneFlagged
// makes sure the footnote doesn't clutter the common stale-period case.
func TestRenderProviderQuotaTable_ConfigChangedFootnoteAbsentWhenNoneFlagged(t *testing.T) {
	rep := &Report2{
		ProviderQuotas: []ProviderQuotaRow{{Provider: "stale", WindowConsumed: f64(3), Live: nil}},
	}
	out := renderProvidersStr(rep, i18n.EN)
	if strings.Contains(out, "-‡") || strings.Contains(out, "keyed under the OLD config") {
		t.Errorf("must not render the ‡ marker or footnote when LiveConfigChanged is false:\n%s", out)
	}
}

func TestRenderProviderQuotaTable_ZH(t *testing.T) {
	rep := &Report2{
		ProviderQuotas: []ProviderQuotaRow{{Provider: "acct1", Metric: "tokens", Every: "1mo", Amount: 1000}},
	}
	out := renderProvidersStr(rep, i18n.ZH)
	if !strings.Contains(out, "额度与消耗对照") {
		t.Errorf("zh sub-table title missing:\n%s", out)
	}
	if !strings.Contains(out, "不可相减") {
		t.Errorf("zh footnote missing:\n%s", out)
	}
}

// TestRenderProviderQuotaTable_SkippedNoteInsideTable is the N7 lock-in: the
// skipped-attempts note is part of the sub-table — rendered when the table
// is, and absent when the table is absent (even with skip stats set).
func TestRenderProviderQuotaTable_SkippedNoteInsideTable(t *testing.T) {
	withTable := &Report2{
		ProviderQuotas:                []ProviderQuotaRow{{Provider: "acct1", WindowConsumed: f64(5)}},
		ProviderQuotaSkippedAttempts:  2,
		ProviderQuotaSkippedProviders: []string{"ghost-a"},
	}
	out := renderProvidersStr(withTable, i18n.EN)
	if !strings.Contains(out, "2 attempts skipped") {
		t.Errorf("note must render under the quota table when skips exist:\n%s", out)
	}

	withoutTable := &Report2{
		Providers:                     []ProviderRow{{Provider: "p1", Requests: 1, RequestsOK: 1}},
		ProviderQuotaSkippedAttempts:  2,
		ProviderQuotaSkippedProviders: []string{"ghost-a"},
	}
	out = renderProvidersStr(withoutTable, i18n.EN)
	if strings.Contains(out, "attempts skipped") {
		t.Errorf("note must not render without the quota table (orphan note):\n%s", out)
	}
}
