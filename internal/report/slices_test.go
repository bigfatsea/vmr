// Ver 2026-09-21 22:00, by Sonnet 5

package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// makeSampleReport builds a comprehensive Report2 for testing slice extraction.
func makeSampleReport() *Report2 {
	cost1 := 1.2345
	cost2 := 0.5678
	return &Report2{
		Meta: Meta{
			Format:      Format,
			GeneratedAt: "2026-09-06T20:00:00Z",
			Inputs:      []string{"test-audit-1.jsonl", "test-audit-2.jsonl"},
			Records:     100,
			ParseErrors: 1,
			From:        "2026-09-06T10:00:00Z",
			To:          "2026-09-06T20:00:00Z",
		},
		Overall: Row{
			Date: "2026-09-06",
			TrafficStats: TrafficStats{
				Requests:        100,
				OK:              95,
				Errors:          5,
				TokensIn:        50000,
				TokensInCached:  30000,
				TokensInFresh:   20000,
				TokensOut:       15000,
				TokensKnown:     90,
				CacheEfficiency: 0.60,
				RequestsWithDur: 95,
				DurMSP50:        400,
				DurMSP95:        1200,
			},
			SuccessRate:       0.95,
			TokensCoveragePct: 90.0,
			DurLowN:           false,
			CostEstimate:      &cost1,
		},
		ByModel: []Row{
			{
				Model:    "gpt-5",
				Protocol: "openai",
				TrafficStats: TrafficStats{
					Requests:        80,
					OK:              78,
					TokensIn:        40000,
					TokensInCached:  25000,
					TokensInFresh:   15000,
					TokensOut:       12000,
					TokensKnown:     75,
					CacheEfficiency: 0.62,
					RequestsWithDur: 78,
				},
				SuccessRate:       0.97,
				TokensCoveragePct: 93.75,
				DurLowN:           false,
				CostEstimate:      &cost1,
			},
			{
				Model:    "claude-4",
				Protocol: "anthropic",
				TrafficStats: TrafficStats{
					Requests:        20,
					OK:              17,
					TokensIn:        10000,
					TokensInCached:  5000,
					TokensInFresh:   5000,
					TokensOut:       3000,
					TokensKnown:     15,
					CacheEfficiency: 0.50,
					RequestsWithDur: 17,
				},
				SuccessRate:       0.85,
				TokensCoveragePct: 75.0,
				DurLowN:           true,
				CostEstimate:      &cost2,
			},
		},
		ByDate: []Row{
			{
				Date: "2026-09-06",
				TrafficStats: TrafficStats{
					Requests: 100,
					OK:       95,
				},
			},
		},
		Hours: []HourRow{
			{
				Date: "2026-09-06",
				Hour: 14,
				TrafficStats: TrafficStats{
					Requests: 50,
					OK:       48,
				},
			},
		},
		HoursOfDay: []HourRow{
			{
				Hour: 14,
				TrafficStats: TrafficStats{
					Requests: 50,
					OK:       48,
				},
			},
		},
		Endpoints: []EndpointRow{
			{
				Endpoint:          "openai:gpt-5",
				Attempts:          82,
				OK:                78,
				Failed:            4,
				Forwarded:         80,
				Availability:      0.95,
				Requests:          80,
				RequestsOK:        78,
				TokensCoveragePct: 93.75,
				DurLowN:           false,
				CostEstimate:      &cost1,
			},
		},
		EndpointsAll: []EndpointRow{
			{
				Endpoint:           "openai:gpt-5",
				Attempts:           82,
				OK:                 78,
				Failed:             4,
				Forwarded:          80,
				Availability:       0.95,
				Requests:           80,
				RequestsOK:         78,
				CostEstimate:       &cost1,
				CostEstimateEst:    0.10,
				CostRateIncomplete: false,
			},
			{
				Endpoint:           "anthropic:claude-4",
				Attempts:           20,
				OK:                 17,
				Failed:             3,
				Forwarded:          18,
				Availability:       0.85,
				Requests:           18,
				RequestsOK:         17,
				CostEstimate:       nil, // unpriced but forwarded
				CostRateIncomplete: true,
			},
		},
		ByClient: []ClientRow{
			{
				ClientKey: "agent-1",
				TrafficStats: TrafficStats{
					Requests: 60,
					OK:       58,
				},
				SuccessRate:       0.97,
				TokensCoveragePct: 95.0,
				CostEstimate:      &cost1,
			},
		},
		Workloads: []WorkloadRow{
			{
				Class: "agent",
				TrafficStats: TrafficStats{
					Requests: 90,
					OK:       86,
				},
			},
		},
		Sessions: []SessionRow{
			{
				ID:    "l-session-1",
				Title: "Refactor router",
				From:  "2026-09-06T10:00:00Z",
				To:    "2026-09-06T12:00:00Z",
				TrafficStats: TrafficStats{
					Requests: 15,
					OK:       15,
				},
			},
		},
		Compactions: []CompactionRow{
			{
				TS:          "2026-09-06T11:30:00Z",
				TSMS:        1788780600000,
				TSDisplay:   "2026-09-06 11:30:00",
				Summarizes:  "l-session-1",
				ContinuesTo: "l-session-2",
				TokensIn:    8000,
				TokensOut:   1200,
			},
		},
		Tools: []ToolShapeRow{
			{
				Shape:              "tools:3/abcdef12",
				Requests:           40,
				Declared:           []string{"bash", "read", "write"},
				DeclaredBytes:      1500,
				SchemaBytesShipped: 60000,
				DistinctCalled:     2,
				DeclareUtilization: 0.67,
				SchemaWasteBytes:   20000,
			},
		},
		Efficiency: []Finding{
			{
				Code:    FindingToolSchemaWaste,
				Finding: "Tool schema waste",
				Metric:  "schema_bytes_shipped",
				Value:   "60 KB",
			},
		},
		Sticky: &StickyEffect{
			Continued: StickyGroup{Requests: 70, CacheEfficiency: 0.85},
			Switched:  StickyGroup{Requests: 10, CacheEfficiency: 0.20},
			First:     20,
		},
		Providers: []ProviderRow{
			{
				Provider:     "openai-prod",
				Models:       []string{"gpt-5"},
				Requests:     80,
				RequestsOK:   78,
				Attempts:     82,
				Failed:       4,
				CostEstimate: &cost1,
			},
		},
		ProviderQuotas: []ProviderQuotaRow{
			{
				Provider:       "openai-prod",
				Metric:         "tokens",
				Every:          "1mo",
				Amount:         100000000,
				WindowConsumed: 50000,
			},
		},
		ProviderQuotaSkippedAttempts:  2,
		ProviderQuotaSkippedProviders: []string{"test-unused"},
		ClientEndpoints: []ClientEndpointRow{
			{
				ClientKey: "agent-1",
				Endpoint:  "openai:gpt-5",
				Requests:  60,
			},
		},
	}
}

// TestMacroSlices_EquivalenceWithReport2 verifies that the 5 domain slices
// preserve all data from Report2 without information loss (TASK_SPEC Task 1/3).
func TestMacroSlices_EquivalenceWithReport2(t *testing.T) {
	rep := makeSampleReport()

	// 1. Build the 5 slices
	summary := BuildSummarySlice(rep)
	finance := BuildFinanceSlice(rep)
	reliability := BuildReliabilitySlice(rep)
	workloads := BuildWorkloadsSlice(rep)
	ctxEff := BuildContextEfficiencySlice(rep)

	// Verify SummarySlice
	if summary.Overall.Requests != rep.Overall.Requests || summary.Overall.OK != rep.Overall.OK {
		t.Errorf("SummarySlice.Overall mismatch: got %d reqs, want %d", summary.Overall.Requests, rep.Overall.Requests)
	}
	if len(summary.Efficiency) != len(rep.Efficiency) {
		t.Errorf("SummarySlice.Efficiency len = %d, want %d", len(summary.Efficiency), len(rep.Efficiency))
	}
	if len(summary.Highlights) == 0 {
		t.Errorf("SummarySlice.Highlights is empty")
	}

	// Verify FinanceSlice
	if len(finance.ByModel) != len(rep.ByModel) {
		t.Errorf("FinanceSlice.ByModel len = %d, want %d", len(finance.ByModel), len(rep.ByModel))
	}
	if len(finance.ByClient) != len(rep.ByClient) {
		t.Errorf("FinanceSlice.ByClient len = %d, want %d", len(finance.ByClient), len(rep.ByClient))
	}
	if len(finance.Providers) != len(rep.Providers) {
		t.Errorf("FinanceSlice.Providers len = %d, want %d", len(finance.Providers), len(rep.Providers))
	}
	if len(finance.ProviderQuotas) != len(rep.ProviderQuotas) {
		t.Errorf("FinanceSlice.ProviderQuotas len = %d, want %d", len(finance.ProviderQuotas), len(rep.ProviderQuotas))
	}
	if finance.ProviderQuotaSkippedAttempts != rep.ProviderQuotaSkippedAttempts {
		t.Errorf("FinanceSlice.ProviderQuotaSkippedAttempts = %d, want %d",
			finance.ProviderQuotaSkippedAttempts, rep.ProviderQuotaSkippedAttempts)
	}
	if len(finance.ProviderQuotaSkippedProviders) != len(rep.ProviderQuotaSkippedProviders) {
		t.Errorf("FinanceSlice.ProviderQuotaSkippedProviders len = %d, want %d",
			len(finance.ProviderQuotaSkippedProviders), len(rep.ProviderQuotaSkippedProviders))
	}
	// Cost coverage check
	if finance.CostCoverage.UnpricedCount != 1 {
		t.Errorf("CostCoverage.UnpricedCount = %d, want 1", finance.CostCoverage.UnpricedCount)
	}
	if finance.CostCoverage.IncompleteRateCount != 1 {
		t.Errorf("CostCoverage.IncompleteRateCount = %d, want 1", finance.CostCoverage.IncompleteRateCount)
	}
	if finance.CostCoverage.DegradedEstimatePct <= 0 {
		t.Errorf("CostCoverage.DegradedEstimatePct = %f, want > 0", finance.CostCoverage.DegradedEstimatePct)
	}

	// Verify ReliabilitySlice
	if len(reliability.Endpoints) != len(rep.Endpoints) {
		t.Errorf("ReliabilitySlice.Endpoints len = %d, want %d", len(reliability.Endpoints), len(rep.Endpoints))
	}
	if len(reliability.EndpointsAll) != len(rep.EndpointsAll) {
		t.Errorf("ReliabilitySlice.EndpointsAll len = %d, want %d", len(reliability.EndpointsAll), len(rep.EndpointsAll))
	}
	if reliability.Sticky == nil || reliability.Sticky.Continued.Requests != rep.Sticky.Continued.Requests {
		t.Errorf("ReliabilitySlice.Sticky mismatch")
	}

	// Verify WorkloadsSlice
	if len(workloads.ByDate) != len(rep.ByDate) {
		t.Errorf("WorkloadsSlice.ByDate len = %d, want %d", len(workloads.ByDate), len(rep.ByDate))
	}
	if len(workloads.Hours) != len(rep.Hours) {
		t.Errorf("WorkloadsSlice.Hours len = %d, want %d", len(workloads.Hours), len(rep.Hours))
	}
	if len(workloads.HoursOfDay) != len(rep.HoursOfDay) {
		t.Errorf("WorkloadsSlice.HoursOfDay len = %d, want %d", len(workloads.HoursOfDay), len(rep.HoursOfDay))
	}
	if len(workloads.Workloads) != len(rep.Workloads) {
		t.Errorf("WorkloadsSlice.Workloads len = %d, want %d", len(workloads.Workloads), len(rep.Workloads))
	}
	if len(workloads.ClientEndpoints) != len(rep.ClientEndpoints) {
		t.Errorf("WorkloadsSlice.ClientEndpoints len = %d, want %d", len(workloads.ClientEndpoints), len(rep.ClientEndpoints))
	}

	// Verify ContextEfficiencySlice
	if len(ctxEff.Sessions) != len(rep.Sessions) {
		t.Errorf("ContextEfficiencySlice.Sessions len = %d, want %d", len(ctxEff.Sessions), len(rep.Sessions))
	}
	if len(ctxEff.Compactions) != len(rep.Compactions) {
		t.Errorf("ContextEfficiencySlice.Compactions len = %d, want %d", len(ctxEff.Compactions), len(rep.Compactions))
	}
	if len(ctxEff.Tools) != len(rep.Tools) {
		t.Errorf("ContextEfficiencySlice.Tools len = %d, want %d", len(ctxEff.Tools), len(rep.Tools))
	}
}

// TestDualTimeFields verifies that timestamps serialize both ts (epoch ms)
// and ts_display (DisplayZone formatted string) (§3.3, §5.6).
func TestDualTimeFields(t *testing.T) {
	origZone := fmtutil.DisplayZone
	testZone := time.FixedZone("TEST+08:00", 8*3600)
	fmtutil.DisplayZone = testZone
	defer func() { fmtutil.DisplayZone = origZone }()

	sampleTime := time.Date(2026, 9, 6, 12, 30, 45, 0, time.UTC)
	tp := NewTimePoint(sampleTime)

	// 1. Check epoch milliseconds
	wantMS := sampleTime.UnixMilli()
	if tp.TS != wantMS {
		t.Errorf("TimePoint.TS = %d, want %d", tp.TS, wantMS)
	}

	// 2. Check DisplayZone representation (12:30 UTC + 8h = 20:30)
	wantDisplay := "2026-09-06 20:30:45"
	if tp.TSDisplay != wantDisplay {
		t.Errorf("TimePoint.TSDisplay = %q, want %q", tp.TSDisplay, wantDisplay)
	}

	// 3. Check JSON serialization of TimePoint
	data, err := json.Marshal(tp)
	if err != nil {
		t.Fatalf("marshal TimePoint: %v", err)
	}
	var unmarshaled struct {
		TS        int64  `json:"ts"`
		TSDisplay string `json:"ts_display"`
	}
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("unmarshal TimePoint: %v", err)
	}
	if unmarshaled.TS != wantMS || unmarshaled.TSDisplay != wantDisplay {
		t.Errorf("unmarshaled = %+v, want ts=%d display=%q", unmarshaled, wantMS, wantDisplay)
	}

	// 4. Test CompactionRow dual time serialization
	comp := CompactionRow{
		TS:          "2026-09-06T12:30:45Z",
		Summarizes:  "l-test-1",
		ContinuesTo: "l-test-2",
		TokensIn:    5000,
		TokensOut:   800,
	}
	compData, err := json.Marshal(comp)
	if err != nil {
		t.Fatalf("marshal CompactionRow: %v", err)
	}
	var rawMap map[string]any
	if err := json.Unmarshal(compData, &rawMap); err != nil {
		t.Fatalf("unmarshal CompactionRow to map: %v", err)
	}
	if rawTS, ok := rawMap["ts"].(float64); !ok || int64(rawTS) != wantMS {
		t.Errorf("CompactionRow JSON ts = %v (int64: %d), want %d", rawMap["ts"], int64(rawTS), wantMS)
	}
	if rawDisplay, ok := rawMap["ts_display"].(string); !ok || rawDisplay != wantDisplay {
		t.Errorf("CompactionRow JSON ts_display = %v, want %q", rawMap["ts_display"], wantDisplay)
	}

	// 5. Test CompactionRow unmarshaling from JSON with epoch ms
	var decodedComp CompactionRow
	if err := json.Unmarshal(compData, &decodedComp); err != nil {
		t.Fatalf("unmarshal CompactionRow: %v", err)
	}
	if decodedComp.TSMS != wantMS {
		t.Errorf("decodedComp.TSMS = %d, want %d", decodedComp.TSMS, wantMS)
	}
	if decodedComp.TSDisplay != wantDisplay {
		t.Errorf("decodedComp.TSDisplay = %q, want %q", decodedComp.TSDisplay, wantDisplay)
	}
	if decodedComp.TokensIn != 5000 || decodedComp.TokensOut != 800 {
		t.Errorf("decodedComp tokens mismatch: In=%d Out=%d", decodedComp.TokensIn, decodedComp.TokensOut)
	}
}

// TestManifest_AtomicWriteAndValidation tests writing the 5 slices and manifest,
// verifying SHA-256 digests, atomic guarantees, and tamper detection (TASK_SPEC Task 2/3).
func TestManifest_AtomicWriteAndValidation(t *testing.T) {
	tempDir := t.TempDir()
	rep := makeSampleReport()

	// 1. Write the 5 macro slices
	if err := WriteMacroSlices(tempDir, rep); err != nil {
		t.Fatalf("WriteMacroSlices: %v", err)
	}

	// Check file existence and permissions (0600)
	for _, rel := range MacroSlicePaths {
		path := filepath.Join(tempDir, rel)
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat slice %s: %v", rel, err)
		}
		if fi.IsDir() {
			t.Fatalf("slice %s is a directory", rel)
		}
		perm := fi.Mode().Perm()
		if perm != 0600 {
			t.Errorf("slice %s perm = %o, want 0600", rel, perm)
		}
	}

	// 2. Build and write manifest
	manifest, err := BuildManifest(tempDir, rep, i18n.EN)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	if manifest.Format != ManifestFormat {
		t.Errorf("manifest.Format = %d, want %d", manifest.Format, ManifestFormat)
	}
	if len(manifest.Slices) != 5 {
		t.Errorf("manifest.Slices has %d entries, want 5", len(manifest.Slices))
	}
	for _, rel := range MacroSlicePaths {
		ref, ok := manifest.Slices[rel]
		if !ok {
			t.Errorf("manifest missing slice %s", rel)
			continue
		}
		if ref.SHA256 == "" {
			t.Errorf("manifest slice %s has empty sha256", rel)
		}
	}

	if err := WriteManifest(tempDir, manifest); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Check manifest file permissions (0600)
	manifestPath := filepath.Join(tempDir, "manifest.json")
	mfi, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatalf("stat manifest: %v", err)
	}
	if mfi.Mode().Perm() != 0600 {
		t.Errorf("manifest perm = %o, want 0600", mfi.Mode().Perm())
	}

	// 3. Validate snapshot with ValidateManifest
	validated, err := ValidateManifest(tempDir)
	if err != nil {
		t.Fatalf("ValidateManifest failed on valid snapshot: %v", err)
	}
	if validated.Format != ManifestFormat {
		t.Errorf("validated.Format = %d, want %d", validated.Format, ManifestFormat)
	}

	// 4. Tamper detection: modify one slice by appending a space
	summaryPath := filepath.Join(tempDir, SliceMacroSummary)
	content, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if err := os.WriteFile(summaryPath, append(content, ' '), 0600); err != nil {
		t.Fatalf("tamper summary: %v", err)
	}
	if _, err := ValidateManifest(tempDir); err == nil {
		t.Errorf("ValidateManifest succeeded after slice tampering, want sha256 mismatch error")
	} else if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Errorf("ValidateManifest returned unexpected error: %v", err)
	}

	// 5. Restore summary, delete manifest: must fail admission
	if err := os.WriteFile(summaryPath, content, 0600); err != nil {
		t.Fatalf("restore summary: %v", err)
	}
	if err := os.Remove(manifestPath); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}
	if _, err := ValidateManifest(tempDir); err == nil {
		t.Errorf("ValidateManifest succeeded without manifest.json, want error")
	}

	// 6. Format mismatch detection: write manifest with Format = 10
	manifest.Format = 10
	if err := WriteManifest(tempDir, manifest); err != nil {
		t.Fatalf("WriteManifest with format 10: %v", err)
	}
	if _, err := ValidateManifest(tempDir); err == nil {
		t.Errorf("ValidateManifest succeeded with format 10, want mismatch error")
	} else if !strings.Contains(err.Error(), "format mismatch") {
		t.Errorf("ValidateManifest returned unexpected error: %v", err)
	}
}

// TestManifest_PartialMacroSetRejected pins N8: a snapshot the report half
// ran for must have all five macro slices, and a manifest that records only
// some of them is corrupt, not a valid macro-free snapshot.
func TestManifest_PartialMacroSetRejected(t *testing.T) {
	rep := makeSampleReport()

	// BuildManifest with rep != nil must refuse when a macro slice is gone.
	t.Run("BuildManifest refuses a missing macro slice", func(t *testing.T) {
		dir := t.TempDir()
		if err := WriteMacroSlices(dir, rep); err != nil {
			t.Fatalf("WriteMacroSlices: %v", err)
		}
		if err := os.Remove(filepath.Join(dir, SliceMacroFinance)); err != nil {
			t.Fatalf("remove finance slice: %v", err)
		}
		if _, err := BuildManifest(dir, rep, i18n.EN); err == nil {
			t.Fatal("BuildManifest succeeded with finance.json missing, want error")
		} else if !strings.Contains(err.Error(), "finance.json") {
			t.Errorf("error should name the missing slice: %v", err)
		}
	})

	// ValidateManifest must reject a manifest that records 4 of 5 macro slices.
	t.Run("ValidateManifest refuses a partial macro set on record", func(t *testing.T) {
		dir := t.TempDir()
		if err := WriteMacroSlices(dir, rep); err != nil {
			t.Fatalf("WriteMacroSlices: %v", err)
		}
		m, err := BuildManifest(dir, rep, i18n.EN)
		if err != nil {
			t.Fatalf("BuildManifest: %v", err)
		}
		delete(m.Slices, SliceMacroReliability)
		if err := WriteManifest(dir, m); err != nil {
			t.Fatalf("WriteManifest: %v", err)
		}
		if _, err := ValidateManifest(dir); err == nil {
			t.Fatal("ValidateManifest accepted a manifest recording 4/5 macro slices, want error")
		}
	})

	// An empty Slices map is not a valid snapshot.
	t.Run("ValidateManifest refuses an empty slice set", func(t *testing.T) {
		dir := t.TempDir()
		m := &Manifest{Format: ManifestFormat, Lang: "en"}
		if err := WriteManifest(dir, m); err != nil {
			t.Fatalf("WriteManifest: %v", err)
		}
		if _, err := ValidateManifest(dir); err == nil {
			t.Fatal("ValidateManifest accepted a manifest with no slices, want error")
		}
	})

	// A journey/zoom snapshot (rep == nil, no macro slices) stays valid.
	t.Run("macro-free snapshot still validates", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "journeys"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, SliceJourneysIndex), []byte(`{"journeys":[]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		m, err := BuildManifest(dir, nil, i18n.EN)
		if err != nil {
			t.Fatalf("BuildManifest(rep=nil): %v", err)
		}
		if err := WriteManifest(dir, m); err != nil {
			t.Fatalf("WriteManifest: %v", err)
		}
		if _, err := ValidateManifest(dir); err != nil {
			t.Errorf("ValidateManifest rejected a valid macro-free snapshot: %v", err)
		}
	})

	t.Run("BuildManifest with rep=nil preserves existing snapshot provenance", func(t *testing.T) {
		dir := t.TempDir()
		// 1. Initial snapshot with provenance
		initial := &Manifest{
			Format:      ManifestFormat,
			TimeRange:   [2]string{"2026-08-24T00:00:00Z", "2026-08-24T23:59:59Z"},
			Inputs:      []InputFile{{Path: "logs/audit.jsonl", SHA256: "abc"}},
			Footnotes:   map[string]FootnoteRef{"note": {Code: "text"}},
			Disclaimers: []DisclaimerRef{{Code: "disc"}},
		}
		if err := WriteManifest(dir, initial); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "journeys"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, SliceJourneysIndex), []byte(`{"journeys":[]}`), 0o600); err != nil {
			t.Fatal(err)
		}

		// 2. Zoom run (rep == nil) builds manifest
		updated, err := BuildManifest(dir, nil, i18n.EN)
		if err != nil {
			t.Fatalf("BuildManifest(nil): %v", err)
		}
		if updated.TimeRange != initial.TimeRange {
			t.Errorf("TimeRange = %v, want %v", updated.TimeRange, initial.TimeRange)
		}
		if len(updated.Inputs) != 1 || updated.Inputs[0].Path != "logs/audit.jsonl" {
			t.Errorf("Inputs = %v, want preserved inputs", updated.Inputs)
		}
		if updated.Footnotes["note"].Code != "text" {
			t.Errorf("Footnotes = %v, want preserved footnotes", updated.Footnotes)
		}
		if len(updated.Disclaimers) != 1 || updated.Disclaimers[0].Code != "disc" {
			t.Errorf("Disclaimers = %v, want preserved disclaimers", updated.Disclaimers)
		}
	})
}

// TestConfidenceFields verifies that TokensCoveragePct and DurLowN are correctly
// computed on Row and EndpointRow during aggregation finish (§3.3).
func TestConfidenceFields(t *testing.T) {
	// Case 1: High confidence, adequate sample size
	r1 := Row{
		TrafficStats: TrafficStats{
			Requests:        100,
			OK:              95,
			TokensKnown:     95,
			RequestsWithDur: 50,
		},
	}
	finishRow(&r1)
	if r1.TokensCoveragePct != 95.0 {
		t.Errorf("r1.TokensCoveragePct = %f, want 95.0", r1.TokensCoveragePct)
	}
	if r1.DurLowN {
		t.Errorf("r1.DurLowN = true, want false (n=50)")
	}

	// Case 2: Low confidence (< 90% basis), low sample size (< 20)
	r2 := Row{
		TrafficStats: TrafficStats{
			Requests:        50,
			OK:              45,
			TokensKnown:     30,
			RequestsWithDur: 10,
		},
	}
	finishRow(&r2)
	if r2.TokensCoveragePct != 60.0 {
		t.Errorf("r2.TokensCoveragePct = %f, want 60.0", r2.TokensCoveragePct)
	}
	if !r2.DurLowN {
		t.Errorf("r2.DurLowN = false, want true (n=10)")
	}

	// Case 3: EndpointRow confidence
	ep := EndpointRow{
		Requests:        25,
		RequestsOK:      20,
		TokensKnown:     20,
		RequestsWithDur: 15,
	}
	finishEndpoint(&ep)
	if ep.TokensCoveragePct != 80.0 {
		t.Errorf("ep.TokensCoveragePct = %f, want 80.0", ep.TokensCoveragePct)
	}
	if !ep.DurLowN {
		t.Errorf("ep.DurLowN = false, want true (n=15)")
	}
}

// TestFootnotesAndDisclaimersStructure verifies that structured footnotes
// and disclaimers carry stable codes (§3.3) — and, per R1, that they no
// longer take a lang parameter at all, since a code is by definition the
// same in every language.
func TestFootnotesAndDisclaimersStructure(t *testing.T) {
	rep := makeSampleReport()

	fn, disc := BuildFootnotesAndDisclaimers(rep)
	if fn["¹"].Code == "" || fn["⚠️low-n"].Code == "" {
		t.Errorf("footnotes missing required keys: %+v", fn)
	}

	// makeSampleReport sets Compactions but not Pricing — disc should carry
	// exactly the compaction disclaimer, not the pricing one.
	if len(disc) != 1 || disc[0].Code != "compaction_retention" {
		t.Errorf("disc = %+v, want exactly one compaction_retention entry (report has Compactions but no Pricing)", disc)
	}

	// Separately, a report WITH Pricing must carry the pricing_estimate
	// disclaimer with its params — Params is where a consumer gets back
	// what Pricing.Disclaimer(lang) used to bake straight into the text.
	priced := makeSampleReport()
	priced.Pricing = &Pricing{Currency: "USD", StandardGeneratedAt: "2026-08-31"}
	_, discPriced := BuildFootnotesAndDisclaimers(priced)
	var sawPricing bool
	for _, d := range discPriced {
		if d.Code == "pricing_estimate" {
			sawPricing = true
			if d.Params["currency"] != "USD" || d.Params["as_of"] != "2026-08-31" {
				t.Errorf("pricing_estimate disclaimer params = %+v, want currency=USD as_of=2026-08-31", d.Params)
			}
		}
	}
	if !sawPricing {
		t.Errorf("discPriced = %+v, want a pricing_estimate entry", discPriced)
	}
}

// TestSlicesAreLangInvariant is R1's machine-checkable acceptance test for
// the report side (codebase-weight-analysis doc §7): analyzing the same
// input once per language must produce byte-identical macro/*.json slices
// and an identical manifest.json apart from its Lang/GeneratedAt fields —
// language is a render-time (Markdown) concern only, never baked into the
// JSON data product. Uses heartbeatDreamDiaryTiedRecords (aggregate_test.go)
// since it's already proven to trigger both a Finding (FindingCronRedundancy)
// and a Highlight (the cache-warn branch, same cache_efficiency=0 fixture),
// so this test exercises the exact fields R1 is about, not just a report
// with no findings at all.
func TestSlicesAreLangInvariant(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, heartbeatDreamDiaryTiedRecords())
	now := time.Now()

	build := func(lang i18n.Lang) (slices map[string][]byte, manifest *Manifest) {
		rep, _, _, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil)
		if err != nil {
			t.Fatalf("BuildCached: %v", err)
		}
		outDir := t.TempDir()
		if err := WriteMacroSlices(outDir, rep); err != nil {
			t.Fatalf("WriteMacroSlices: %v", err)
		}
		manifest, err = BuildManifest(outDir, rep, lang)
		if err != nil {
			t.Fatalf("BuildManifest: %v", err)
		}
		slices = make(map[string][]byte)
		for _, rel := range MacroSlicePaths {
			data, err := os.ReadFile(filepath.Join(outDir, rel))
			if err != nil {
				t.Fatalf("read %s: %v", rel, err)
			}
			slices[rel] = data
		}
		return slices, manifest
	}

	enSlices, enManifest := build(i18n.EN)
	zhSlices, zhManifest := build(i18n.ZH)

	var sawFinding, sawHighlight bool
	if bytes.Contains(enSlices[SliceMacroSummary], []byte(`"cron_redundancy"`)) {
		sawFinding = true
	}
	if bytes.Contains(enSlices[SliceMacroSummary], []byte(`"cache_warn"`)) {
		sawHighlight = true
	}
	if !sawFinding || !sawHighlight {
		t.Fatalf("fixture should trigger both a Finding and a Highlight — sawFinding=%v sawHighlight=%v, summary.json:\n%s",
			sawFinding, sawHighlight, enSlices[SliceMacroSummary])
	}

	for _, rel := range MacroSlicePaths {
		if !bytes.Equal(enSlices[rel], zhSlices[rel]) {
			t.Errorf("%s differs between -lang en and -lang zh (R1 violation):\n--- en ---\n%s\n--- zh ---\n%s",
				rel, enSlices[rel], zhSlices[rel])
		}
	}

	// manifest.json: same everything except Lang (by design) and
	// GeneratedAt (wall clock, expected to differ between the two builds).
	enManifest.Lang, zhManifest.Lang = "", ""
	enManifest.GeneratedAt, zhManifest.GeneratedAt = TimePoint{}, TimePoint{}
	enBytes, err := json.Marshal(enManifest)
	if err != nil {
		t.Fatal(err)
	}
	zhBytes, err := json.Marshal(zhManifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(enBytes, zhBytes) {
		t.Errorf("manifest.json differs between -lang en and -lang zh beyond Lang/GeneratedAt (R1 violation):\n--- en ---\n%s\n--- zh ---\n%s", enBytes, zhBytes)
	}
}
