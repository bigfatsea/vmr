// Ver 2026-08-07, by Opus 5

package quota

import (
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
