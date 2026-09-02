// Ver 2026-07-23 10:00, by Sonnet 5

package sticky

import (
	"testing"
	"time"
)

func TestRegistry_SetPeek(t *testing.T) {
	t.Parallel()
	r := New()
	if _, _, ok := r.Peek("k"); ok {
		t.Fatalf("expected no entry before Set")
	}
	r.Set("k", "openai-completions/minimax/MiniMax-M3")
	epKey, lastUsed, ok := r.Peek("k")
	if !ok {
		t.Fatalf("expected entry after Set")
	}
	if epKey != "openai-completions/minimax/MiniMax-M3" {
		t.Errorf("epKey = %q", epKey)
	}
	if time.Since(lastUsed) > time.Second {
		t.Errorf("lastUsed should be ~now, got %v ago", time.Since(lastUsed))
	}
}

func TestRegistry_SetOverwrites(t *testing.T) {
	t.Parallel()
	r := New()
	r.Set("k", "endpoint-a")
	r.Set("k", "endpoint-b") // e.g. sticky pointer moved after failover success
	epKey, _, ok := r.Peek("k")
	if !ok || epKey != "endpoint-b" {
		t.Errorf("expected the second Set to win, got epKey=%q ok=%v", epKey, ok)
	}
}

func TestRegistry_ValidityIsCallersJob(t *testing.T) {
	t.Parallel()
	// Peek never evicts or hides an entry based on age — that's explicitly
	// the caller's responsibility (per-endpoint TTL), not this package's.
	r := New()
	r.Set("k", "endpoint-a")
	r.entries["k"] = entry{endpointKey: "endpoint-a", lastUsed: time.Now().Add(-48 * time.Hour)}
	epKey, lastUsed, ok := r.Peek("k")
	if !ok || epKey != "endpoint-a" {
		t.Fatalf("Peek must still return an old entry; validity is the caller's decision")
	}
	if time.Since(lastUsed) < 47*time.Hour {
		t.Errorf("expected the backdated lastUsed to be preserved")
	}
}

func TestRegistry_Len(t *testing.T) {
	t.Parallel()
	r := New()
	if r.Len() != 0 {
		t.Errorf("expected empty registry to have Len 0")
	}
	r.Set("a", "ep1")
	r.Set("b", "ep2")
	if r.Len() != 2 {
		t.Errorf("expected Len 2, got %d", r.Len())
	}
}

func TestRegistry_MaxEntries_EvictsOldest(t *testing.T) {
	t.Parallel()
	r := NewBounded(3)

	now := time.Now()
	r.mu.Lock()
	r.entries["k1"] = entry{endpointKey: "ep1", lastUsed: now.Add(-30 * time.Minute)}
	r.entries["k2"] = entry{endpointKey: "ep2", lastUsed: now.Add(-20 * time.Minute)}
	r.entries["k3"] = entry{endpointKey: "ep3", lastUsed: now.Add(-10 * time.Minute)}
	r.mu.Unlock()

	// Setting k4 should evict the oldest entry ("k1")
	r.Set("k4", "ep4")

	if r.Len() != 3 {
		t.Fatalf("expected Len 3, got %d", r.Len())
	}
	if _, _, ok := r.Peek("k1"); ok {
		t.Errorf("expected k1 to be evicted")
	}
	if _, _, ok := r.Peek("k2"); !ok {
		t.Errorf("expected k2 to be present")
	}
	if _, _, ok := r.Peek("k3"); !ok {
		t.Errorf("expected k3 to be present")
	}
	if _, _, ok := r.Peek("k4"); !ok {
		t.Errorf("expected k4 to be present")
	}

	// Update k2 to make it the freshest
	r.Set("k2", "ep2-updated")
	if r.Len() != 3 {
		t.Fatalf("updating existing key should not grow Len: got %d", r.Len())
	}

	// Setting k5 should now evict k3 (since k2 was updated)
	r.Set("k5", "ep5")
	if r.Len() != 3 {
		t.Fatalf("expected Len 3, got %d", r.Len())
	}
	if _, _, ok := r.Peek("k3"); ok {
		t.Errorf("expected k3 to be evicted")
	}
	if _, _, ok := r.Peek("k2"); !ok {
		t.Errorf("expected k2 to remain present")
	}
	if _, _, ok := r.Peek("k4"); !ok {
		t.Errorf("expected k4 to remain present")
	}
	if _, _, ok := r.Peek("k5"); !ok {
		t.Errorf("expected k5 to remain present")
	}
}

func TestRegistry_MaxEntries_TTLSweepFirst(t *testing.T) {
	t.Parallel()
	r := NewBounded(3)

	now := time.Now()
	r.mu.Lock()
	// k1 is expired past BackstopTTL
	r.entries["k1"] = entry{endpointKey: "ep1", lastUsed: now.Add(-2 * BackstopTTL)}
	r.entries["k2"] = entry{endpointKey: "ep2", lastUsed: now.Add(-10 * time.Minute)}
	r.entries["k3"] = entry{endpointKey: "ep3", lastUsed: now.Add(-5 * time.Minute)}
	r.mu.Unlock()

	// Setting k4 triggers TTL sweep first, removing expired k1
	r.Set("k4", "ep4")

	if r.Len() != 3 {
		t.Fatalf("expected Len 3, got %d", r.Len())
	}
	if _, _, ok := r.Peek("k1"); ok {
		t.Errorf("expected expired k1 to be swept by TTL")
	}
	if _, _, ok := r.Peek("k2"); !ok {
		t.Errorf("expected k2 to remain")
	}
	if _, _, ok := r.Peek("k3"); !ok {
		t.Errorf("expected k3 to remain")
	}
	if _, _, ok := r.Peek("k4"); !ok {
		t.Errorf("expected k4 to remain")
	}
}

func TestRegistry_DefaultMaxEntries(t *testing.T) {
	t.Parallel()
	if MaxEntries != 10000 {
		t.Errorf("expected MaxEntries=10000, got %d", MaxEntries)
	}
	r := New()
	if r.limit() != 10000 {
		t.Errorf("expected default limit=10000, got %d", r.limit())
	}
}
