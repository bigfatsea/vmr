// Ver 2026-08-07, by Opus 5

package quota

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"vmr/internal/core"
)

func TestRegistry_ChargeAndUsed(t *testing.T) {
	r := NewRegistry("")
	ps := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.Charge("plan-a", "tokens/1mo", ps, Counters{Fresh: 100, Out: 50}, 0)
	r.Charge("plan-a", "tokens/1mo", ps, Counters{Fresh: 20}, 0)
	c, est := r.Used("plan-a", "tokens/1mo", ps)
	if c.Fresh != 120 || c.Out != 50 {
		t.Fatalf("Used = %+v, want Fresh=120 Out=50", c)
	}
	if est != 0 {
		t.Fatalf("estimated = %v, want 0", est)
	}
}

// TestRegistry_ChargeCost_UpdatesCostAndEstimatedCost: ChargeCost applies
// the $ charge and its degraded-estimate share in one locked section — read
// back through Snapshot and both must be there together.
func TestRegistry_ChargeCost_UpdatesCostAndEstimatedCost(t *testing.T) {
	r := NewRegistry("")
	ps := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.ChargeCost("plan-a", "cost/1mo", ps, Counters{Fresh: 100, Out: 50, Cost: 1.5}, 0.5)
	c, est, estCost := r.Snapshot("plan-a", "cost/1mo", ps)
	if c.Cost != 1.5 || c.Fresh != 100 || est != 0 || estCost != 0.5 {
		t.Fatalf("after ChargeCost: counters=%+v est=%v estCost=%v, want Cost=1.5 Fresh=100 est=0 estCost=0.5", c, est, estCost)
	}
	// A second, exact (non-degraded) charge must not touch EstimatedCost.
	r.ChargeCost("plan-a", "cost/1mo", ps, Counters{Cost: 2.5}, 0)
	c, _, estCost = r.Snapshot("plan-a", "cost/1mo", ps)
	if c.Cost != 4 || estCost != 0.5 {
		t.Fatalf("after exact ChargeCost: Cost=%v estCost=%v, want 4 / 0.5 (estimate share unchanged)", c.Cost, estCost)
	}
}

// TestRegistry_ChargeCost_PeriodRollResetsBoth pins the F4 atomicity: a
// period roll zeroes Cost and EstimatedCost together — never one half in the
// old period and the other leaked into the new one, the shape the old
// Charge-then-AddEstimatedCost sequence could produce.
func TestRegistry_ChargeCost_PeriodRollResetsBoth(t *testing.T) {
	r := NewRegistry("")
	p1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	r.ChargeCost("plan-a", "cost/1mo", p1, Counters{Cost: 10}, 5)
	r.ChargeCost("plan-a", "cost/1mo", p2, Counters{Cost: 1}, 0.5)
	c, est, estCost := r.Snapshot("plan-a", "cost/1mo", p2)
	if c.Cost != 1 || est != 0 || estCost != 0.5 {
		t.Fatalf("after period roll: Cost=%v est=%v estCost=%v, want 1/0/0.5 — both halves reset together", c.Cost, est, estCost)
	}
}

// TestRegistry_Snapshot_LazyReset: the read side of the same invariant —
// Snapshot's three values are always from ONE period, even when the call
// itself rolls a stale bucket forward.
func TestRegistry_Snapshot_LazyReset(t *testing.T) {
	r := NewRegistry("")
	p1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	r.Charge("plan-a", "cost/1mo", p1, Counters{Cost: 10}, 1)
	r.Charge("plan-a", "cost/1mo", p1, Counters{Cost: 0}, 9)
	c, est, estCost := r.Snapshot("plan-a", "cost/1mo", p2)
	if c.Cost != 0 || est != 0 || estCost != 0 {
		t.Fatalf("Snapshot across a period roll = (%+v, %v, %v), want all zeroed (new period)", c, est, estCost)
	}
}

func TestRegistry_ConcurrentChargeCost_Race(t *testing.T) {
	r := NewRegistry("")
	ps := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.ChargeCost("plan-a", "cost/1mo", ps, Counters{Requests: 1, Cost: 1}, 0.5)
		}()
	}
	wg.Wait()
	c, _, estCost := r.Snapshot("plan-a", "cost/1mo", ps)
	if c.Requests != 50 || c.Cost != 50 || estCost != 25 {
		t.Fatalf("concurrent ChargeCost: Requests=%v Cost=%v estCost=%v, want 50/50/25 (totals must be conserved)", c.Requests, c.Cost, estCost)
	}
}

func TestRegistry_EstimatedAccumulates(t *testing.T) {
	r := NewRegistry("")
	ps := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.Charge("plan-a", "tokens/1mo", ps, Counters{Fresh: 10}, 10)
	r.Charge("plan-a", "tokens/1mo", ps, Counters{Fresh: 5}, 5)
	_, est := r.Used("plan-a", "tokens/1mo", ps)
	if est != 15 {
		t.Fatalf("estimated = %v, want 15", est)
	}
}

func TestRegistry_LazyResetOnCharge(t *testing.T) {
	r := NewRegistry("")
	p1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	r.Charge("plan-a", "requests/1mo", p1, Counters{Requests: 5}, 0)
	c, _ := r.Used("plan-a", "requests/1mo", p1)
	if c.Requests != 5 {
		t.Fatalf("period 1 requests = %v, want 5", c.Requests)
	}
	// Crossing into period 2 must zero the bucket, not carry the old count.
	r.Charge("plan-a", "requests/1mo", p2, Counters{Requests: 1}, 0)
	c2, _ := r.Used("plan-a", "requests/1mo", p2)
	if c2.Requests != 1 {
		t.Fatalf("period 2 requests after reset = %v, want 1 (old period's count must not carry over)", c2.Requests)
	}
	// The old period's own key, re-queried, is stale forever once we moved
	// past it — Used(p1) after the bucket has advanced to p2 sees a reset
	// bucket back at p1's boundary, not the retained old value: this is
	// documented behavior of the single-bucket-per-limitKey design (no
	// history is kept), not a bug.
}

// TestRegistry_DefaultSinceReloadDoesNotReset is the B2 regression: a Limit
// with no explicit `since` gets its anchor from DefaultSince at every config
// load. A charge, then a simulated reload (fresh DefaultSince a few minutes
// later, same calendar day), then a read — the accumulated count must
// survive, because the calendar-aligned anchor makes PeriodStart identical
// across the reload.
func TestRegistry_DefaultSinceReloadDoesNotReset(t *testing.T) {
	withDisplayZone(t, time.UTC)
	load1 := time.Date(2026, 8, 7, 2, 3, 0, 0, time.UTC)
	load2 := time.Date(2026, 8, 7, 20, 10, 0, 0, time.UTC) // hot reload, same day, ~18h later

	for _, c := range []struct {
		unit string
		n    int
	}{{"min", 5}, {"h", 1}, {"h", 5}, {"d", 1}, {"w", 1}, {"mo", 1}} {
		r := NewRegistry("")
		l := core.Limit{Metric: core.MetricTokens, EveryUnit: c.unit, EveryN: c.n}
		l.Since = DefaultSince(load1, l.EveryUnit)
		key := LimitKey(l, "")

		r.Charge("plan-a", key, PeriodStart(l, load2), Counters{Fresh: 1000}, 0)

		// Reload: re-resolve the anchor exactly as config.Load would.
		l.Since = DefaultSince(load2, l.EveryUnit)
		got, _ := r.Used("plan-a", key, PeriodStart(l, load2))
		if got.Fresh != 1000 {
			t.Errorf("every %d%s: after same-day reload, Used = %v, want 1000 retained (B2)", c.n, c.unit, got.Fresh)
		}
	}
}

// TestRegistry_UsedResetMarksDirty is the B8 regression: a period roll
// observed only by the read path must still be persisted by the flusher.
func TestRegistry_UsedResetMarksDirty(t *testing.T) {
	r := NewRegistry("")
	p1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	r.Charge("plan-a", "requests/1mo", p1, Counters{Requests: 5}, 0)
	r.mu.Lock()
	r.dirty = false
	r.mu.Unlock()

	r.Used("plan-a", "requests/1mo", p2) // rolls the bucket via the read path

	r.mu.Lock()
	dirty := r.dirty
	r.mu.Unlock()
	if !dirty {
		t.Fatal("Used() rolled the bucket to a new period but left dirty=false — the reset would never be persisted (B8)")
	}
}

func TestRegistry_LazyResetOnUsedAlone(t *testing.T) {
	// A pure-read caller (the router's per-request scoring path) must see a
	// correctly-zeroed bucket immediately after a period boundary, without
	// any Charge ever happening in the new period first.
	r := NewRegistry("")
	p1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	r.Charge("plan-a", "requests/1mo", p1, Counters{Requests: 42}, 0)
	c, _ := r.Used("plan-a", "requests/1mo", p2)
	if c.Requests != 0 {
		t.Fatalf("Used() alone across a period boundary = %v, want 0 (reset without any Charge)", c.Requests)
	}
}

// TestRegistry_ClockRollback_KeepsCountsAndWarns pins R49: the lazy reset is
// direction-sensitive. A backward periodStart (NTP correction, VM snapshot
// restore, TZ change) must KEEP the current period's counts and warn once,
// not zero the whole period — a one-way wipe that a later Flush would make
// permanent.
func TestRegistry_ClockRollback_KeepsCountsAndWarns(t *testing.T) {
	r := NewRegistry("")
	var buf bytes.Buffer
	r.SetLogger(log.New(&buf, "", 0))
	key := "requests/1mo"
	p1 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	p0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	r.Charge("plan-a", key, p1, Counters{Requests: 5}, 0)

	// Forward movement still resets (regression — the pre-R49 behavior).
	p2 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if c, _ := r.Used("plan-a", key, p2); c.Requests != 0 {
		t.Fatalf("forward period roll = %v, want 0 (reset still works)", c.Requests)
	}
	r.Charge("plan-a", key, p2, Counters{Requests: 3}, 0)

	// Backward: counts must be KEPT, and a WARN emitted once.
	c, _ := r.Used("plan-a", key, p0)
	if c.Requests != 3 {
		t.Fatalf("count after clock rollback = %v, want 3 retained", c.Requests)
	}
	if !strings.Contains(buf.String(), "clock moved backward") {
		t.Fatalf("expected a clock-rollback WARN, got log=%q", buf.String())
	}
	// Dedup: a second rollback must not log again.
	buf.Reset()
	r.Used("plan-a", key, p0)
	if buf.Len() != 0 {
		t.Fatalf("second rollback produced another WARN: %q", buf.String())
	}
}

// TestRegistry_SamePeriod_NoReset pins the equal-period leg of R49: an equal
// periodStart must not touch the bucket (regression).
func TestRegistry_SamePeriod_NoReset(t *testing.T) {
	r := NewRegistry("")
	ps := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.Charge("plan-a", "requests/1mo", ps, Counters{Requests: 1}, 0)
	r.Charge("plan-a", "requests/1mo", ps, Counters{Requests: 2}, 0)
	c, _ := r.Used("plan-a", "requests/1mo", ps)
	if c.Requests != 3 {
		t.Fatalf("same-period charges = %v, want 3 (equal period must not reset)", c.Requests)
	}
}

func TestRegistry_ProviderKeyExcludesAPIKey(t *testing.T) {
	// This test exists purely to pin the API surface: Charge/Used take a
	// provider NAME string, never an API key or its hash — see quota.go's
	// Registry doc comment for why (key rotation must not reset quota).
	r := NewRegistry("")
	ps := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.Charge("plan-a", "requests/1mo", ps, Counters{Requests: 1}, 0)
	// Simulate "rotating the key": the provider's core.Endpoint.HealthKey()
	// would change, but Registry only ever sees the stable provider name.
	c, _ := r.Used("plan-a", "requests/1mo", ps)
	if c.Requests != 1 {
		t.Fatalf("count lost across what would be a key rotation: %v, want 1", c.Requests)
	}
}

func TestRegistry_IndependentAccountsAndLimitKeys(t *testing.T) {
	r := NewRegistry("")
	ps := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.Charge("plan-a", "requests/1mo", ps, Counters{Requests: 1}, 0)
	r.Charge("plan-b", "requests/1mo", ps, Counters{Requests: 2}, 0)
	r.Charge("plan-a", "tokens/1mo", ps, Counters{Fresh: 3}, 0)
	a, _ := r.Used("plan-a", "requests/1mo", ps)
	b, _ := r.Used("plan-b", "requests/1mo", ps)
	at, _ := r.Used("plan-a", "tokens/1mo", ps)
	if a.Requests != 1 || b.Requests != 2 || at.Fresh != 3 {
		t.Fatalf("cross-contamination between accounts/limitKeys: a=%+v b=%+v at=%+v", a, b, at)
	}
}

func TestRegistry_ConcurrentChargeUsed_Race(t *testing.T) {
	r := NewRegistry("")
	ps := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.Charge("plan-a", "requests/1mo", ps, Counters{Requests: 1}, 0)
		}()
		go func() {
			defer wg.Done()
			r.Used("plan-a", "requests/1mo", ps)
		}()
	}
	wg.Wait()
	c, _ := r.Used("plan-a", "requests/1mo", ps)
	if c.Requests != 50 {
		t.Fatalf("concurrent charges: got %v, want 50", c.Requests)
	}
}
