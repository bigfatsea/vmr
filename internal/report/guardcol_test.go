// Ver 2026-09-16, by Sonnet 5

package report

import (
	"fmt"
	"testing"

	"vmr/internal/audit"
)

// TestGuardCollector_NilResultWhenNothingScanned confirms result() is nil
// when add() was never called at all -- e.g. every record in the run was
// excluded before reaching guardCollector (self-traffic exclusion) or the
// log was empty. A record that DID reach add() with guard == nil is a
// different case (the Fallback Path covered it) -- see
// TestGuardCollector_FallbackPathCoversNilGuardRecord.
func TestGuardCollector_NilResultWhenNothingScanned(t *testing.T) {
	gc := newGuardCollector()
	if got := gc.result(); got != nil {
		t.Errorf("result() = %+v, want nil when add() was never called", got)
	}
}

// TestGuardCollector_FallbackPathCoversNilGuardRecord is M2.2's own
// contract: a record with guard == nil and guardScan == nil (the Fallback
// Path ran and found nothing, e.g. an empty request body) still counts as
// fallback-scanned -- extractRecordFacts always attempts the scan when
// Guard is nil, so "found nothing" and "never looked" are not the same
// thing, and only the latter should ever produce a nil GuardSummary.
func TestGuardCollector_FallbackPathCoversNilGuardRecord(t *testing.T) {
	gc := newGuardCollector()
	gc.add(&rec2{})
	got := gc.result()
	if got == nil {
		t.Fatal("result() = nil, want coverage of the fallback-scanned record")
	}
	if got.RecordsScanned != 1 || got.RecordsFallbackScanned != 1 || got.RecordsStamped != 0 {
		t.Errorf("got = %+v, want RecordsScanned=1/RecordsFallbackScanned=1/RecordsStamped=0", got)
	}
	if got.RecordsWithHits != 0 {
		t.Errorf("RecordsWithHits = %d, want 0", got.RecordsWithHits)
	}
}

func TestGuardCollector_Aggregation(t *testing.T) {
	gc := newGuardCollector()
	// Record 1: two hits of the same rule, two distinct fingerprints.
	gc.add(&rec2{guard: &audit.GuardRecord{
		Ver: 1,
		Hits: []audit.Hit{
			{Rule: "gcp-api-key", Tier: 1, Count: 3, FP: "fp-a"},
			{Rule: "gcp-api-key", Tier: 1, Count: 1, FP: "fp-b"},
		},
	}})
	// Record 2: same rule again (same fingerprint as record 1's first hit —
	// must not double-count UniqueFP), plus a Tier2 rule and a historical
	// replace-era restore count.
	gc.add(&rec2{guard: &audit.GuardRecord{
		Ver: 1,
		Hits: []audit.Hit{
			{Rule: "gcp-api-key", Tier: 1, Count: 5, FP: "fp-a"},
			{Rule: "generic-sk-prefix", Tier: 2, Count: 2, FP: "fp-c"},
		},
	}})
	// Record 3: no hits at all, but Guard is non-nil (Agent Guard was
	// active and found nothing) — must count toward RecordsScanned, not
	// RecordsWithHits.
	gc.add(&rec2{guard: &audit.GuardRecord{Ver: 1}})

	got := gc.result()
	if got == nil {
		t.Fatal("result() = nil, want a populated GuardSummary")
	}
	if got.RecordsScanned != 3 {
		t.Errorf("RecordsScanned = %d, want 3", got.RecordsScanned)
	}
	if got.RecordsWithHits != 2 {
		t.Errorf("RecordsWithHits = %d, want 2", got.RecordsWithHits)
	}
	if got.RulesetVersion != 1 {
		t.Errorf("RulesetVersion = %d, want 1", got.RulesetVersion)
	}
	if len(got.Rules) != 2 {
		t.Fatalf("Rules = %+v, want 2 entries", got.Rules)
	}
	byName := map[string]GuardRuleRow{}
	for _, r := range got.Rules {
		byName[r.Rule] = r
	}
	gcp := byName["gcp-api-key"]
	if gcp.TotalHits != 9 { // 3+1+5
		t.Errorf("gcp-api-key TotalHits = %d, want 9", gcp.TotalHits)
	}
	if gcp.UniqueFP != 2 { // fp-a (seen twice, counted once) + fp-b
		t.Errorf("gcp-api-key UniqueFP = %d, want 2", gcp.UniqueFP)
	}
	if gcp.RecordsWith != 2 {
		t.Errorf("gcp-api-key RecordsWith = %d, want 2", gcp.RecordsWith)
	}
	// Record 1's two hits are DISTINCT credentials (fp-a, fp-b), so its
	// contribution is max(3, 1)=3, not their sum -- MaxPerRecord measures one
	// credential resent N times, never N distinct credentials pasted once
	// each. Record 2 contributes 5 (its only gcp-api-key hit). Max across
	// records is 5.
	if gcp.MaxPerRecord != 5 {
		t.Errorf("gcp-api-key MaxPerRecord = %d, want 5", gcp.MaxPerRecord)
	}
	sk := byName["generic-sk-prefix"]
	if sk.TotalHits != 2 || sk.UniqueFP != 1 || sk.RecordsWith != 1 || sk.MaxPerRecord != 2 {
		t.Errorf("generic-sk-prefix row = %+v, unexpected", sk)
	}
}

// TestGuardCollector_MaxPerRecordDistinctCredentialsNotAmplification pins
// down the distinction TestGuardCollector_Aggregation's shape doesn't
// actually exercise: ten DISTINCT credentials (a config template pasted
// once, e.g.) must read as "ten separate leaks, amplification 1x", never as
// "one credential resent ten times" — that conflation would tell an operator
// a single secret is circulating through a long replayed session when
// nothing was ever resent at all.
func TestGuardCollector_MaxPerRecordDistinctCredentialsNotAmplification(t *testing.T) {
	gc := newGuardCollector()
	hits := make([]audit.Hit, 10)
	for i := range hits {
		hits[i] = audit.Hit{Rule: "openai-project-key", Tier: 1, Count: 1, FP: fmt.Sprintf("fp-%d", i)}
	}
	gc.add(&rec2{guard: &audit.GuardRecord{Ver: 1, Hits: hits}})

	got := gc.result()
	if got == nil {
		t.Fatal("result() = nil, want a populated GuardSummary")
	}
	row := got.Rules[0]
	if row.TotalHits != 10 {
		t.Errorf("TotalHits = %d, want 10 (ten distinct credentials, one occurrence each)", row.TotalHits)
	}
	if row.UniqueFP != 10 {
		t.Errorf("UniqueFP = %d, want 10", row.UniqueFP)
	}
	if row.MaxPerRecord != 1 {
		t.Errorf("MaxPerRecord = %d, want 1 (each credential appeared once — no amplification, not 10)", row.MaxPerRecord)
	}
}

// TestGuardCollector_VersionConflict confirms a ruleset-version disagreement
// across scanned records reports RulesetVersion 0 (ambiguous) rather than
// silently picking one version's number as if it were universal.
func TestGuardCollector_VersionConflict(t *testing.T) {
	gc := newGuardCollector()
	gc.add(&rec2{guard: &audit.GuardRecord{Ver: 1}})
	gc.add(&rec2{guard: &audit.GuardRecord{Ver: 2}})
	got := gc.result()
	if got.RulesetVersion != 0 {
		t.Errorf("RulesetVersion = %d, want 0 on a version conflict", got.RulesetVersion)
	}
}

func TestGuardCollector_RecordsScanFailed(t *testing.T) {
	gc := newGuardCollector()
	gc.add(&rec2{guardScanFailed: true})
	gc.add(&rec2{guard: &audit.GuardRecord{Ver: 1}})
	got := gc.result()
	if got.RecordsScanFailed != 1 {
		t.Errorf("RecordsScanFailed = %d, want 1", got.RecordsScanFailed)
	}
}
