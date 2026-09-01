// Ver 2026-07-26, by Sonnet 5
package fmtutil

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// TestFmtBytes locks the B/KB/MB thresholds and rounding — these three
// functions had no direct test coverage before this package existed (only
// exercised indirectly through report/router output), so this is new
// coverage, not a relocation of an existing test.
func TestFmtBytes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0B"},
		{999, "999B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1 << 20, "1.0MB"},
		{3*(1<<20) + (1 << 19), "3.5MB"},
		{500 * (1 << 30), "500.0GB"},
		{2 * (1 << 40), "2.0TB"},
	}
	for _, c := range cases {
		if got := FmtBytes(c.n); got != c.want {
			t.Errorf("FmtBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestFmtDuration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{45 * time.Second, "45s"},
		{12*time.Minute + 30*time.Second, "12m 30s"},
		{3*time.Hour + 34*time.Minute + 5*time.Second, "3h 34m 5s"},
		{25*time.Hour + 10*time.Second, "25h 0m 10s"},
	}
	for _, c := range cases {
		if got := FmtDuration(c.d); got != c.want {
			t.Errorf("FmtDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// TestFmtSeconds locks the fixed-decimal rendering and the decimals
// parameter (2 for the live router log, 3 for `vmr diagnose`).
func TestFmtSeconds(t *testing.T) {
	t.Parallel()
	d := 6*time.Second + 320*time.Millisecond
	if got, want := FmtSeconds(d, 2), "6.32s"; got != want {
		t.Errorf("FmtSeconds(%s, 2) = %q, want %q", d, got, want)
	}
	if got, want := FmtSeconds(d, 3), "6.320s"; got != want {
		t.Errorf("FmtSeconds(%s, 3) = %q, want %q", d, got, want)
	}
	if got, want := FmtSeconds(0, 2), "0.00s"; got != want {
		t.Errorf("FmtSeconds(0, 2) = %q, want %q", got, want)
	}
}

// TestFmtPercent locks the decimals parameter both internal/report's and
// internal/story's own pctStr now alias — 1 decimal for report's dense
// tables, 0 for story's narrative text — so a future edit to one of those
// aliases can't silently re-diverge them from this shared definition.
func TestFmtPercent(t *testing.T) {
	t.Parallel()
	if got, want := FmtPercent(0.4234, 1), "42.3%"; got != want {
		t.Errorf("FmtPercent(0.4234, 1) = %q, want %q", got, want)
	}
	if got, want := FmtPercent(0.4234, 0), "42%"; got != want {
		t.Errorf("FmtPercent(0.4234, 0) = %q, want %q", got, want)
	}
	if got, want := FmtPercent(0, 1), "0.0%"; got != want {
		t.Errorf("FmtPercent(0, 1) = %q, want %q", got, want)
	}
}

// TestFmtTokens locks the K/M/B thresholds and rounding for the dense
// table-cell format — B5 converged internal/report's and internal/story's
// independently-written fmtTokens onto this function; these cases pin the
// exact boundary/rounding behavior neither package's own tests exercised
// directly (only indirectly, through full-report/story golden output).
func TestFmtTokens(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{5, "5"},
		{999, "999"},
		{1000, "1.0K"},
		{1234, "1.2K"},
		{999999, "1000.0K"},
		{1_000_000, "1.00M"},
		{1_500_000, "1.50M"},
		{999_999_999, "1000.00M"},
		{1_000_000_000, "1.00B"},
		{1_500_000_000, "1.50B"},
	}
	for _, c := range cases {
		if got := FmtTokens(c.n); got != c.want {
			t.Errorf("FmtTokens(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// TestFmtTokensPlain locks the space-separated-unit format ("500 T", "1.2
// KT") — deliberately no "B" tier (unlike FmtTokens): detail.go's per-request
// estimate never approaches a billion tokens, so the extra tier was never
// added here.
func TestFmtTokensPlain(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 T"},
		{5, "5 T"},
		{999, "999 T"},
		{1000, "1.0 KT"},
		{1234, "1.2 KT"},
		{999999, "1000.0 KT"},
		{1_000_000, "1.0 MT"},
		{1_500_000, "1.5 MT"},
		{1_500_000_000, "1500.0 MT"},
	}
	for _, c := range cases {
		if got := FmtTokensPlain(c.n); got != c.want {
			t.Errorf("FmtTokensPlain(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// TestFmtTokensCompact locks the live-log format: always a "KT"/"MT" unit
// (no bare-number tier below 1000, unlike FmtTokens/FmtTokensPlain), and 2
// decimals below 1000 instead of K/M's 1 — at 1 decimal a value under 100
// tokens would round to "0.0KT" and lose the number entirely.
func TestFmtTokensCompact(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0.00KT"},
		{5, "0.01KT"},
		{500, "0.50KT"},
		{999, "1.00KT"},
		{1000, "1.0KT"},
		{1234, "1.2KT"},
		{999999, "1000.0KT"},
		{1_000_000, "1.0MT"},
		{1_500_000, "1.5MT"},
		{1_500_000_000, "1500.0MT"},
	}
	for _, c := range cases {
		if got := FmtTokensCompact(c.n); got != c.want {
			t.Errorf("FmtTokensCompact(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// TestCapStrRuneSafe locks in that CapStr's byte cap never cuts through a
// UTF-8 sequence — Chinese/emoji session titles and compaction needles near
// the cap must stay valid UTF-8. Moved from internal/taskseg, where CapStr
// originally landed alongside the Profile helpers despite carrying
// no dialect-specific knowledge of its own (architecture review's B2
// feedback round).
func TestCapStrRuneSafe(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("审", 100) // 3 bytes per rune
	for n := 0; n <= 12; n++ {
		got := CapStr(s, n)
		if !utf8.ValidString(got) {
			t.Errorf("CapStr(…, %d) produced invalid UTF-8: %q", n, got)
		}
		if len(got) > n {
			t.Errorf("CapStr(…, %d) exceeded the byte cap: %d bytes", n, len(got))
		}
	}
	if got := CapStr("ascii only", 200); got != "ascii only" {
		t.Errorf("short string must be returned whole: %q", got)
	}
}

func TestCapStrNonPositiveN(t *testing.T) {
	t.Parallel()
	if got := CapStr("hello", -1); got != "" {
		t.Errorf("CapStr(…, -1) = %q, want empty string, not a negative slice panic", got)
	}
	if got := CapStr("hello", 0); got != "" {
		t.Errorf("CapStr(…, 0) = %q, want empty string", got)
	}
	if got := CapStr("", -1); got != "" {
		t.Errorf("CapStr(\"\", -1) = %q, want empty string", got)
	}
}

func TestSortedKeys(t *testing.T) {
	t.Parallel()
	m := map[string]int{
		"zebra":  1,
		"apple":  2,
		"mango":  3,
		"banana": 4,
	}
	got := SortedKeys(m)
	want := []string{"apple", "banana", "mango", "zebra"}
	if len(got) != len(want) {
		t.Fatalf("SortedKeys len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SortedKeys[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	empty := map[string]string{}
	if gotEmpty := SortedKeys(empty); len(gotEmpty) != 0 {
		t.Errorf("SortedKeys(empty) = %v, want empty", gotEmpty)
	}
}
