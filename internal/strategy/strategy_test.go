// Ver 2026-09-23 03:00, by Claude Opus 5.5
package strategy

import (
	"math"
	"testing"

	"vmr/internal/core"
)

func TestPrioritySortStableOnTies(t *testing.T) {
	t.Parallel()
	eps := []*core.Endpoint{
		{Provider: "c", Priority: 2},
		{Provider: "a", Priority: 1},
		{Provider: "b", Priority: 1}, // same priority as "a", listed after
	}
	Sort(eps)
	got := []string{eps[0].Provider, eps[1].Provider, eps[2].Provider}
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order: got %v want %v", got, want)
		}
	}
}

func TestPrioritySortNoOverflow(t *testing.T) {
	t.Parallel()
	// eps[i].Priority < eps[j].Priority avoids subtraction overflow
	// when two values are at opposite extremes of int.
	eps := []*core.Endpoint{
		{Provider: "max", Priority: math.MaxInt32},
		{Provider: "min", Priority: math.MinInt32},
	}
	Sort(eps)
	if eps[0].Provider != "min" || eps[1].Provider != "max" {
		t.Errorf("order: got %v, %v; want min, max", eps[0].Provider, eps[1].Provider)
	}
}
