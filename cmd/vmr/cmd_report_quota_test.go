// Ver 2026-08-13, by Opus 5
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/fmtutil"
	"vmr/internal/report"
)

// writeQuotaJSON seeds <dir>/vmr-quota.json with one bucket, mirroring
// internal/quota/store.go's on-disk fileFormat directly (this test must not
// depend on internal/quota.Registry's own Flush to stay independent of it).
func writeQuotaJSON(t *testing.T, dir string, periodStart time.Time, requests int64) string {
	t.Helper()
	path := filepath.Join(dir, "vmr-quota.json")
	body := map[string]any{
		"version": 1,
		"accounts": map[string]any{
			"acct1": map[string]any{
				"requests/1mo": map[string]any{
					"period_start": periodStart.Unix(),
					"counters":     map[string]any{"requests": requests},
					"estimated":    0,
				},
			},
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestBuildProviderQuotas_LivePopulatedWhenPeriodMatches is the positive
// case: a vmr-quota.json bucket whose period_start matches PeriodStart(lim,
// now) must populate Live with the right Used/Pct.
func TestBuildProviderQuotas_LivePopulatedWhenPeriodMatches(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, fmtutil.DisplayZone)
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, fmtutil.DisplayZone)
	writeQuotaJSON(t, dir, periodStart, 500)
	configPath := writeTempFile(t, "config.yaml", quotaYAML(dir))

	var tw bytes.Buffer
	cfg, cfgErr := config.Load(configPath)
	quotas, _ := buildProviderQuotas(cfg, cfgErr, configPath, &tw, now)
	ref, ok := quotas["acct1"]
	if !ok {
		t.Fatal("acct1 missing from quotas map")
	}
	if ref.Live == nil {
		t.Fatal("Live should be populated when the on-disk period matches now")
	}
	if ref.Live.Used != 500 {
		t.Errorf("Live.Used = %v, want 500", ref.Live.Used)
	}
	if got, want := ref.Live.Pct, 50.0; got != want {
		t.Errorf("Live.Pct = %v, want %v", got, want)
	}
}

// TestBuildProviderQuotas_StalePeriod_LiveStaysNil is §5.2's stale-period
// trap gate at the source: a bucket left over from a period the process
// wasn't running through (period_start way before what PeriodStart(lim, now)
// computes for "now") must NOT be rendered as this period's usage — Live
// must stay nil, never populated from a mismatched bucket.
func TestBuildProviderQuotas_StalePeriod_LiveStaysNil(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, fmtutil.DisplayZone)            // "now" is in March
	stalePeriodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, fmtutil.DisplayZone) // bucket stuck in January
	writeQuotaJSON(t, dir, stalePeriodStart, 999)
	configPath := writeTempFile(t, "config.yaml", quotaYAML(dir))

	var tw bytes.Buffer
	cfg, cfgErr := config.Load(configPath)
	quotas, _ := buildProviderQuotas(cfg, cfgErr, configPath, &tw, now)
	ref, ok := quotas["acct1"]
	if !ok {
		t.Fatal("acct1 missing from quotas map")
	}
	if ref.Live != nil {
		t.Errorf("Live = %+v, want nil (stale period must be suppressed, not shown as current)", ref.Live)
	}
	// The static reference (Amount/Metric/Every) must still resolve — only
	// the live column degrades, not the whole account.
	if ref.Amount != 1000 || ref.Metric != "requests" {
		t.Errorf("static quota reference should still resolve: %+v", ref)
	}
	// this IS the same limitKey, just an old period — must NOT be
	// misattributed to a config change.
	if ref.LiveConfigChanged {
		t.Error("LiveConfigChanged must be false for a plain stale-period miss (same key, wrong period)")
	}
}

// TestBuildProviderQuotas_ConfigChanged_FlagsDistinctlyFromStalePeriod is
// when config.yaml's quota: metric/every changed since vmr-quota.json
// was last written, the OLD limitKey still has a bucket but the CURRENT one
// never does — this must be flagged LiveConfigChanged, not just a plain
// nil Live indistinguishable from "process wasn't running".
func TestBuildProviderQuotas_ConfigChanged_FlagsDistinctlyFromStalePeriod(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, fmtutil.DisplayZone)
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, fmtutil.DisplayZone)
	// The on-disk bucket is keyed "tokens/1mo" (an old config), but
	// quotaYAML declares "requests/1mo" for acct1 — a genuine key mismatch
	// from a config edit, not a stale period on the same key.
	path := filepath.Join(dir, "vmr-quota.json")
	body := map[string]any{
		"version": 1,
		"accounts": map[string]any{
			"acct1": map[string]any{
				"tokens/1mo": map[string]any{
					"period_start": periodStart.Unix(),
					"counters":     map[string]any{"fresh": 100},
					"estimated":    0,
				},
			},
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeTempFile(t, "config.yaml", quotaYAML(dir))

	var tw bytes.Buffer
	cfg, cfgErr := config.Load(configPath)
	quotas, _ := buildProviderQuotas(cfg, cfgErr, configPath, &tw, now)
	ref, ok := quotas["acct1"]
	if !ok {
		t.Fatal("acct1 missing from quotas map")
	}
	if ref.Live != nil {
		t.Errorf("Live = %+v, want nil (current limitKey has no bucket)", ref.Live)
	}
	if !ref.LiveConfigChanged {
		t.Error("LiveConfigChanged must be true: the provider has an old key's bucket but not the current one")
	}
}

// TestBuildProviderQuotas_NoDataAtAll_LiveConfigChangedStaysFalse covers the
// remaining case: no vmr-quota.json bucket for this provider under ANY
// key (first run, or truly never charged) — must NOT be misread as a config
// change; there's simply no data yet.
func TestBuildProviderQuotas_NoDataAtAll_LiveConfigChangedStaysFalse(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTempFile(t, "config.yaml", quotaYAML(dir))

	var tw bytes.Buffer
	cfg, cfgErr := config.Load(configPath)
	quotas, _ := buildProviderQuotas(cfg, cfgErr, configPath, &tw, time.Now())
	ref, ok := quotas["acct1"]
	if !ok {
		t.Fatal("acct1 missing from quotas map")
	}
	if ref.LiveConfigChanged {
		t.Error("LiveConfigChanged must be false when there's no vmr-quota.json at all (no other key to infer a config change from)")
	}
}

// TestBuildProviderQuotas_MissingQuotaJSON_AllLiveNilNoError is the common
// first-run case: no vmr-quota.json at all must not fail the report, just
// leave every Live nil.
func TestBuildProviderQuotas_MissingQuotaJSON_AllLiveNilNoError(t *testing.T) {
	dir := t.TempDir()
	configPath := writeTempFile(t, "config.yaml", quotaYAML(dir))

	var tw bytes.Buffer
	cfg, cfgErr := config.Load(configPath)
	quotas, _ := buildProviderQuotas(cfg, cfgErr, configPath, &tw, time.Now())
	ref, ok := quotas["acct1"]
	if !ok {
		t.Fatal("acct1 missing from quotas map")
	}
	if ref.Live != nil {
		t.Errorf("Live = %+v, want nil when vmr-quota.json doesn't exist", ref.Live)
	}
	if tw.String() != "" {
		t.Errorf("a missing vmr-quota.json is the normal first-run case, must not warn: %q", tw.String())
	}
}

// TestBuildProviderQuotas_CorruptQuotaJSON_WarnsButDoesNotPanic covers the
// same "a statistics helper must never break the report" posture buildPricing
// already has, for the vmr-quota.json read.
func TestBuildProviderQuotas_CorruptQuotaJSON_WarnsButDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vmr-quota.json"), []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeTempFile(t, "config.yaml", quotaYAML(dir))

	var tw bytes.Buffer
	cfg, cfgErr := config.Load(configPath)
	quotas, _ := buildProviderQuotas(cfg, cfgErr, configPath, &tw, time.Now())
	ref, ok := quotas["acct1"]
	if !ok {
		t.Fatal("acct1 missing from quotas map")
	}
	if ref.Live != nil {
		t.Errorf("Live = %+v, want nil on a corrupt vmr-quota.json", ref.Live)
	}
	if tw.String() == "" {
		t.Error("a corrupt vmr-quota.json should print a warning")
	}
}

// TestBuildProviderQuotas_LoadErrDoesNotWarnItself is the unit-level
// lock-in, mirroring TestBuildPricing_LoadErrDoesNotWarnItself: this
// function must not print its own cfgErr warning either — cmdReport prints
// the one unified warning for both callees now.
func TestBuildProviderQuotas_LoadErrDoesNotWarnItself(t *testing.T) {
	var tw bytes.Buffer
	cfg, cfgErr := config.Load("/nonexistent/config.yaml")
	quotas, _ := buildProviderQuotas(cfg, cfgErr, "/nonexistent/config.yaml", &tw, time.Now())
	if quotas != nil {
		t.Errorf("quotas = %+v, want nil on cfgErr", quotas)
	}
	if tw.String() != "" {
		t.Errorf("buildProviderQuotas must not print its own cfgErr warning, got: %q", tw.String())
	}
}

// TestAllPathsOutsideDir is the source-provenance rule's helper unit test: mixed-location paths
// (some under dir, some not) must NOT count as "outside" — only a run
// where every single input path resolves elsewhere.
func TestAllPathsOutsideDir(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	inFile := filepath.Join(logDir, "vmr-audit-1.jsonl")
	outFile := filepath.Join(dir, "archived", "vmr-audit-old.jsonl")

	if got := allPathsOutsideDir([]string{outFile}, logDir); !got {
		t.Error("a single path entirely outside logDir must report true")
	}
	if got := allPathsOutsideDir([]string{inFile, outFile}, logDir); got {
		t.Error("a mix of in-dir and out-of-dir paths must report false (not ALL outside)")
	}
	if got := allPathsOutsideDir([]string{inFile}, logDir); got {
		t.Error("a path under logDir must report false")
	}
	if got := allPathsOutsideDir(nil, logDir); got {
		t.Error("no paths at all must report false, not a false-positive warning")
	}
	if got := allPathsOutsideDir([]string{outFile}, ""); got {
		t.Error("an empty logDir (unresolvable) must report false, not a false-positive warning")
	}
}

// TestCmdReport_QuotaSourceMetaWiredWhenSubTableRenders is the source-provenance rule's end-to-end
// lock-in: cmdReport must populate Meta.QuotaJSONPath (and flag
// QuotaInputOutsideLogDir) exactly when the §2.5 sub-table has rows to show.
func TestCmdReport_QuotaSourceMetaWiredWhenSubTableRenders(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// No vmr-quota.json needed here — a row renders as soon as acct1
	// resolves its declared quota:, Live nil or not; this test only cares
	// about the two Meta fields, not the Live column's own contents.
	configPath := writeTempFile(t, "config.yaml", quotaYAML(logDir))

	// The input audit file lives OUTSIDE logDir on purpose, to exercise the
	// cross-instance warning path end to end.
	auditDir := filepath.Join(dir, "archived")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatal(err)
	}
	auditPath := filepath.Join(auditDir, "vmr-audit-2026-01-15.jsonl")
	line := `{"ts":"2026-01-15T10:00:00Z","dur_ms":5,"model":"m1","protocol":"openai","outcome":"ok","client":{"request":{}}}` + "\n"
	if err := os.WriteFile(auditPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	if err := cmdReport([]string{"-c", configPath, "-o", outDir, auditPath}); err != nil {
		t.Fatalf("cmdReport: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "vmr-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep report.Report2
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Meta.QuotaJSONPath == "" {
		t.Fatal("Meta.QuotaJSONPath must be set when the quota sub-table has rows")
	}
	if !rep.Meta.QuotaInputOutsideLogDir {
		t.Error("Meta.QuotaInputOutsideLogDir must be true — the input audit file is outside logDir")
	}
}

func quotaYAML(logDir string) string {
	return "listen: 127.0.0.1:0\nlog_dir: " + logDir + "\nproviders:\n  - name: acct1\n    base_url: {openai: https://example.com/v1}\n    api_key: test-key\n    quota:\n      limits: [{metric: requests, every: 1mo, since: 2026-01-01, amount: 1000}]\nmodels:\n  m1:\n    endpoints:\n      - protocol: openai\n        provider: acct1\n        models: [real-model]\n"
}

// tokensQuotaYAML is quotaYAML's tokens-metric sibling, needed to exercise
// EstimatedPct wiring: metric: requests' EstimatedPct is always 0,
// so testing the wiring requires a tokens (or cost) account instead.
func tokensQuotaYAML(logDir string) string {
	return "listen: 127.0.0.1:0\nlog_dir: " + logDir + "\nproviders:\n  - name: acct1\n    base_url: {openai: https://example.com/v1}\n    api_key: test-key\n    quota:\n      limits: [{metric: tokens, every: 1mo, since: 2026-01-01, amount: 1000}]\nmodels:\n  m1:\n    endpoints:\n      - protocol: openai\n        provider: acct1\n        models: [real-model]\n"
}

// costParityYAML is the metric: cost sibling of quotaYAML/tokensQuotaYAML,
// used by cmd/vmr/quota_parity_test.go. It pins the four per-1M component
// prices through providers[].pricing.overrides rather than relying on the
// embedded standard table: the parity test drives the router side with the
// same four numbers as a hand-built core.PricingSpec, and a test whose
// expected value could shift under it every time the LiteLLM snapshot is
// regenerated would be a flake, not a guard.
func costParityYAML(logDir string) string {
	return "listen: 127.0.0.1:0\nlog_dir: " + logDir + "\npricing:\n  currency: USD\nproviders:\n  - name: acct1\n    base_url: {openai: https://example.com/v1}\n    api_key: test-key\n    quota:\n      limits: [{metric: cost, every: 1mo, since: 2026-01-01, amount: 100}]\n    pricing:\n      overrides:\n        - {model: real-model, in_fresh: 3, cache_read: 0.3, cache_write: 3.75, out: 15}\nmodels:\n  m1:\n    endpoints:\n      - protocol: openai\n        provider: acct1\n        models: [real-model]\n"
}

// writeTokensQuotaJSON is writeQuotaJSON's tokens-metric sibling with a
// non-zero "estimated" so TestBuildProviderQuotas_EstimatedPctWiredFromBucket
// can exercise quota.EstimatedPct's tokens branch end to end.
func writeTokensQuotaJSON(t *testing.T, dir string, periodStart time.Time, fresh, out, estimated int64) string {
	t.Helper()
	path := filepath.Join(dir, "vmr-quota.json")
	body := map[string]any{
		"version": 1,
		"accounts": map[string]any{
			"acct1": map[string]any{
				"tokens/1mo": map[string]any{
					"period_start": periodStart.Unix(),
					"counters":     map[string]any{"fresh": fresh, "out": out},
					"estimated":    estimated,
				},
			},
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestBuildProviderQuotas_EstimatedPctWiredFromBucket is the degraded-estimate rule's end-to-end
// lock-in: the on-disk bucket's raw Estimated count must reach
// report.LiveQuota.EstimatedPct via quota.EstimatedPct, not get silently
// dropped the way it was before this fix.
func TestBuildProviderQuotas_EstimatedPctWiredFromBucket(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, fmtutil.DisplayZone)
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, fmtutil.DisplayZone)
	// raw total = 100 (fresh) + 300 (out) = 400; 40 of it estimated -> 10%.
	writeTokensQuotaJSON(t, dir, periodStart, 100, 300, 40)
	configPath := writeTempFile(t, "config.yaml", tokensQuotaYAML(dir))

	var tw bytes.Buffer
	cfg, cfgErr := config.Load(configPath)
	quotas, _ := buildProviderQuotas(cfg, cfgErr, configPath, &tw, now)
	ref, ok := quotas["acct1"]
	if !ok || ref.Live == nil {
		t.Fatal("acct1's Live should be populated")
	}
	if got, want := ref.Live.EstimatedPct, 10.0; got != want {
		t.Errorf("Live.EstimatedPct = %v, want %v", got, want)
	}
}
