// Ver 2026-07-07 02:25, by Fable 5
package strategy

import (
	"math"
	"testing"

	"vmr/internal/core"
)

func TestPrioritySortStableOnTies(t *testing.T) {
	eps := []*core.Endpoint{
		{Provider: "c", Priority: 2},
		{Provider: "a", Priority: 1},
		{Provider: "b", Priority: 1}, // same priority as "a", listed after
	}
	dims, err := Build([]string{"priority"})
	if err != nil {
		t.Fatal(err)
	}
	Sort(eps, dims)
	got := []string{eps[0].Provider, eps[1].Provider, eps[2].Provider}
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order: got %v want %v", got, want)
		}
	}
}

func TestPriorityCompareNoOverflow(t *testing.T) {
	// cmp.Compare avoids the subtraction overflow that a-b produces
	// when the two values are at opposite extremes of int.
	d := priority{}
	a := &core.Endpoint{Priority: math.MaxInt32}
	b := &core.Endpoint{Priority: math.MinInt32}
	if got := d.Compare(a, b); got <= 0 {
		t.Errorf("MaxInt32 vs MinInt32: got %d, want > 0", got)
	}
	if got := d.Compare(b, a); got >= 0 {
		t.Errorf("MinInt32 vs MaxInt32: got %d, want < 0", got)
	}
	if got := d.Compare(a, a); got != 0 {
		t.Errorf("equal priorities: got %d, want 0", got)
	}
}

func TestBuildUnknownDimension(t *testing.T) {
	if _, err := Build([]string{"priority", "nosuch"}); err == nil {
		t.Error("want error for unknown dimension")
	}
}
