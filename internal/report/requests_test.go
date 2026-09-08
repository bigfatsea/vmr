// Ver 2026-08-31, by Sonnet 5
package report

import (
	"testing"
	"time"

	"vmr/internal/fmtutil"
)

// TestOutcomeCell_UnclassifiedFailureIsNotABareDash pins the fix for the
// pre-routing-reject / queue-cancel case: an errored row with an empty
// ErrorClass (nothing ever reached an upstream to classify it) must still
// render a visible failure marker in requests/failed.md, not fall through
// to a bare "-".
func TestOutcomeCell_UnclassifiedFailureIsNotABareDash(t *testing.T) {
	cases := []struct {
		name string
		row  RequestRow
		want string
	}{
		{"error, no class", RequestRow{Outcome: "error"}, "❌unclassified"},
		{"error, classified", RequestRow{Outcome: "error", ErrorClass: "transient"}, "❌transient"},
	}
	for _, c := range cases {
		if got := outcomeCell(c.row); got != c.want {
			t.Errorf("%s: outcomeCell = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestClusterFailedRequests_MissingTimestamps verifies temporal clustering
// resilience against rows with no timestamp (TS == 0 — a row extracted from
// a record the usage/timing layer couldn't stamp). The state machine must
// not decouple from the first real row or inject uninitialized zero-value
// clusters. (ts is epoch ms since §5.6, so a timestamp is either present or
// absent, never syntactically corrupt.)
func TestClusterFailedRequests_MissingTimestamps(t *testing.T) {
	const (
		t0 = int64(1755316810000) // 2026-08-16T04:00:10Z
		t1 = int64(1755316860000) // 2026-08-16T04:01:00Z
		t2 = int64(1755317400000) // 2026-08-16T04:10:00Z
	)

	t.Run("leading missing timestamp followed by 2 grouped failures", func(t *testing.T) {
		rows := []RequestRow{
			{TS: 0, Outcome: "error"},
			{TS: t0, Outcome: "error", ErrorClass: "network"},
			{TS: t1, Outcome: "error", ErrorClass: "network"},
		}
		clusters, maxCount, _, maxClasses := clusterFailedRequests(rows)
		if clusters != 1 {
			t.Errorf("clusters = %d, want 1 (must not prepend an empty cluster for the missing leading row)", clusters)
		}
		if maxCount != 2 {
			t.Errorf("maxCount = %d, want 2", maxCount)
		}
		if maxClasses != "network×2" {
			t.Errorf("maxClasses = %q, want %q", maxClasses, "network×2")
		}
	})

	t.Run("missing timestamps interleaved with two valid clusters", func(t *testing.T) {
		rows := []RequestRow{
			{TS: 0, Outcome: "error"},
			{TS: 0, Outcome: "error"},
			{TS: t0, Outcome: "error", ErrorClass: "network"},
			{TS: 0, Outcome: "error"},
			{TS: t1, Outcome: "error", ErrorClass: "network"},
			{TS: t2, Outcome: "error", ErrorClass: "client"},
		}
		clusters, maxCount, _, _ := clusterFailedRequests(rows)
		if clusters != 2 {
			t.Errorf("clusters = %d, want 2", clusters)
		}
		if maxCount != 2 {
			t.Errorf("maxCount = %d, want 2", maxCount)
		}
	})

	t.Run("all timestamps missing", func(t *testing.T) {
		rows := []RequestRow{
			{TS: 0, Outcome: "error"},
			{TS: 0, Outcome: "error"},
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

// TestFmtDisplayFullConvertsToDisplayZone proves fmtDisplayFull converts
// through fmtutil.DisplayZone rather than reading the input timestamp's
// own embedded offset.
func TestFmtDisplayFullConvertsToDisplayZone(t *testing.T) {
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.FixedZone("TEST-05:00", -5*3600)
	defer func() { fmtutil.DisplayZone = origZone }()

	const in = "2026-07-24T08:17:58+08:00"
	const want = "2026-07-23 19:17:58"

	got := fmtDisplayFull(in)
	if got != want {
		t.Errorf("fmtDisplayFull(%q) = %q, want %q (DisplayZone conversion not applied)", in, got, want)
	}
}

// TestFmtDisplayFullUsesSpaceSeparator proves fmtDisplayFull uses a space
// between date and time ("2026-07-24 00:17:58"), not RFC3339's "T".
func TestFmtDisplayFullUsesSpaceSeparator(t *testing.T) {
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.UTC
	defer func() { fmtutil.DisplayZone = origZone }()

	const in = "2026-07-24T00:17:58Z"
	const want = "2026-07-24 00:17:58"

	got := fmtDisplayFull(in)
	if got != want {
		t.Errorf("fmtDisplayFull(%q) = %q, want %q (space separator, not RFC3339 T)", in, got, want)
	}
}
