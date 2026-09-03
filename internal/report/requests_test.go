// Ver 2026-08-31, by Sonnet 5
package report

import "testing"

// TestFinishCell_UnclassifiedFailureIsNotABareDash pins the fix for the
// pre-routing-reject / queue-cancel case: outcome != "ok" with an empty
// ErrorClass (nothing ever reached an upstream to classify it) must still
// render a visible failure marker in the per-turn / scheduled tables, not
// fall through to orDash(Finish) and read as a normal finish. It also
// keeps finishCell consistent with outcomeCell's own "unclassified"
// fallback.
func TestFinishCell_UnclassifiedFailureIsNotABareDash(t *testing.T) {
	cases := []struct {
		name string
		row  RequestRow
		want string
	}{
		{"error, no class", RequestRow{Outcome: "error"}, "❌unclassified"},
		{"canceled while queued", RequestRow{Outcome: "canceled"}, "❌unclassified"},
		{"error, classified", RequestRow{Outcome: "error", ErrorClass: "transient"}, "❌transient"},
		{"canceled, classified", RequestRow{Outcome: "canceled", ErrorClass: "canceled"}, "❌canceled"},
		{"ok, truncated", RequestRow{Outcome: "ok", Truncated: true}, "⚠️trunc"},
		{"ok, normal finish", RequestRow{Outcome: "ok", Finish: "stop"}, "stop"},
		{"ok, no finish reason", RequestRow{Outcome: "ok"}, "-"},
	}
	for _, c := range cases {
		if got := finishCell(c.row); got != c.want {
			t.Errorf("%s: finishCell = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestClusterFailedRequests_CorruptedTimestamps verifies temporal clustering
// resilience against corrupt or missing timestamps (e.g. leading rows with
// unparseable TS). The state machine must not decouple from the first valid
// row or inject uninitialized zero-value clusters.
func TestClusterFailedRequests_CorruptedTimestamps(t *testing.T) {
	t.Run("leading corrupted timestamp followed by 2 grouped failures", func(t *testing.T) {
		rows := []RequestRow{
			{TS: "invalid-timestamp", Outcome: "error"},
			{TS: "2026-08-16T04:00:10Z", Outcome: "error", ErrorClass: "network"},
			{TS: "2026-08-16T04:01:00Z", Outcome: "error", ErrorClass: "network"},
		}
		clusters, maxCount, _, maxClasses := clusterFailedRequests(rows)
		if clusters != 1 {
			t.Errorf("clusters = %d, want 1 (must not prepend an empty cluster for the corrupt leading row)", clusters)
		}
		if maxCount != 2 {
			t.Errorf("maxCount = %d, want 2", maxCount)
		}
		if maxClasses != "network×2" {
			t.Errorf("maxClasses = %q, want %q", maxClasses, "network×2")
		}
	})

	t.Run("multiple corrupted timestamps with two valid clusters", func(t *testing.T) {
		rows := []RequestRow{
			{TS: "", Outcome: "error"},
			{TS: "bad-ts", Outcome: "error"},
			{TS: "2026-08-16T04:00:10Z", Outcome: "error", ErrorClass: "network"},
			{TS: "corrupted-middle", Outcome: "error"},
			{TS: "2026-08-16T04:01:00Z", Outcome: "error", ErrorClass: "network"},
			{TS: "2026-08-16T04:10:00Z", Outcome: "error", ErrorClass: "client"},
		}
		clusters, maxCount, _, _ := clusterFailedRequests(rows)
		if clusters != 2 {
			t.Errorf("clusters = %d, want 2", clusters)
		}
		if maxCount != 2 {
			t.Errorf("maxCount = %d, want 2", maxCount)
		}
	})

	t.Run("different timezone offsets clustered correctly", func(t *testing.T) {
		// 04:00:10Z is equal to 12:00:10+08:00
		rows := []RequestRow{
			{TS: "2026-08-16T04:00:10Z", Outcome: "error", ErrorClass: "network"},
			{TS: "2026-08-16T12:01:00+08:00", Outcome: "error", ErrorClass: "network"},
		}
		clusters, maxCount, _, _ := clusterFailedRequests(rows)
		if clusters != 1 || maxCount != 2 {
			t.Errorf("clusters=%d maxCount=%d, want 1/2 across different timezone offsets", clusters, maxCount)
		}
	})

	t.Run("all corrupted timestamps", func(t *testing.T) {
		rows := []RequestRow{
			{TS: "bad1", Outcome: "error"},
			{TS: "bad2", Outcome: "error"},
		}
		clusters, maxCount, _, _ := clusterFailedRequests(rows)
		if clusters != 0 || maxCount != 0 {
			t.Errorf("got clusters=%d maxCount=%d, want 0/0", clusters, maxCount)
		}
	})
}

func TestBuildRequestRow_UsageFlags(t *testing.T) {
	rc := &rec2{
		usageInOK:  true,
		usageOutOK: false,
	}
	rr := buildRequestRow(rc)
	if !rr.UsageInOK || rr.UsageOutOK {
		t.Errorf("got UsageInOK=%v, UsageOutOK=%v, want true, false", rr.UsageInOK, rr.UsageOutOK)
	}

	rc2 := &rec2{
		usageInOK:  false,
		usageOutOK: true,
	}
	rr2 := buildRequestRow(rc2)
	if rr2.UsageInOK || !rr2.UsageOutOK {
		t.Errorf("got UsageInOK=%v, UsageOutOK=%v, want false, true", rr2.UsageInOK, rr2.UsageOutOK)
	}
}

func TestTagSummary_UsageInOKGate(t *testing.T) {
	// rowKnownZeroIn: Usage is known on the in-side, but TokensIn is 0.
	// TokensIn > 0 gate would have skipped counting this known record.
	rowKnownZeroIn := RequestRow{
		Outcome:        "ok",
		UsageInOK:      true,
		TokensIn:       0,
		TokensInFresh:  0,
		TokensInCached: 0,
	}
	// rowUnknownWithIn: Defensive case: UsageInOK is false, but TokensIn > 0.
	// Must not be counted in tokensKnown.
	rowUnknownWithIn := RequestRow{
		Outcome:   "ok",
		UsageInOK: false,
		TokensIn:  5,
	}

	summary := tagSummary([]RequestRow{rowKnownZeroIn, rowUnknownWithIn})
	if summary.tokensKnown != 1 {
		t.Errorf("tagSummary.tokensKnown = %d, want 1", summary.tokensKnown)
	}
}
