// Ver 2026-07-28 21:50, by Opus 5
package buildinfo

import (
	"strings"
	"testing"
)

// Short is what a status line, a ps column and `vmr version` all print, so
// its degradation matters more than its happy path: a binary built outside
// a VCS checkout must say "unknown", never an empty column that reads as a
// rendering bug.
func TestShort(t *testing.T) {
	cases := []struct {
		name string
		in   Info
		want string
	}{
		{"clean build", Info{Revision: "fbc034cd30c3e9e787f203578354648ca2d3fe8c"}, "fbc034c"},
		{"dirty build", Info{Revision: "fbc034cd30c3e9e787f203578354648ca2d3fe8c", Modified: true}, "fbc034c-dirty"},
		{"no VCS stamp", Info{GoVersion: "go1.26"}, "unknown"},
		{"short revision passes through", Info{Revision: "abc"}, "abc"},
	}
	for _, c := range cases {
		if got := c.in.Short(); got != c.want {
			t.Errorf("%s: Short() = %q, want %q", c.name, got, c.want)
		}
	}
}

// The dirty note is the field with the most operational value — a running
// instance built from a working tree whose source no longer exists anywhere
// is a reason to distrust a bug report about it — so it must be spelled
// out, not left implicit in a suffix.
func TestStringSpellsOutDirty(t *testing.T) {
	s := Info{Revision: "abcdef1234", Time: "2026-07-27T18:38:00Z", GoVersion: "go1.26", Modified: true}.String()
	for _, want := range []string{"abcdef1-dirty", "2026-07-27T18:38:00Z", "go1.26", "modified working tree"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() = %q, missing %q", s, want)
		}
	}
	if clean := (Info{Revision: "abcdef1234"}).String(); strings.Contains(clean, "modified") {
		t.Errorf("clean build must not mention modification: %q", clean)
	}
}

// Read runs against this test binary, which `go test` builds inside the
// repository — so the stamp is expected to be present here. It must at
// minimum never panic and always report the Go version.
func TestReadReturnsSomethingUsable(t *testing.T) {
	got := Read()
	if got.GoVersion == "" {
		t.Error("GoVersion is empty")
	}
	if got.Short() == "" {
		t.Error("Short() is empty; it must always render something")
	}
}
