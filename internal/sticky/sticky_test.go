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
	r.Set("k", "openai/minimax/MiniMax-M3")
	epKey, lastUsed, ok := r.Peek("k")
	if !ok {
		t.Fatalf("expected entry after Set")
	}
	if epKey != "openai/minimax/MiniMax-M3" {
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
