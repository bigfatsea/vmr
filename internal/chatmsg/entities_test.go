// Ver 2026-07-29 23:00, by Sonnet 5

package chatmsg

import (
	"strconv"
	"strings"
	"testing"
)

// TestExtractEntities locks in ExtractEntities' behavior now that it's a
// public, shared entry point: file paths and
// URLs are found and de-duplicated in order of first appearance, and the
// MaxEntities cap is enforced.
func TestExtractEntities(t *testing.T) {
	t.Parallel()
	text := "see internal/report/session.go and https://example.com/docs, " +
		"also internal/report/session.go again and README.md"
	got := ExtractEntities(text)
	want := []string{"internal/report/session.go", "https://example.com/docs,", "README.md"}
	if len(got) != len(want) {
		t.Fatalf("ExtractEntities = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entity[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExtractEntitiesCapsAtMaxEntities(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < MaxEntities+20; i++ {
		b.WriteString("file")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(".go ")
	}
	got := ExtractEntities(b.String())
	if len(got) != MaxEntities {
		t.Fatalf("ExtractEntities returned %d entities, want the MaxEntities cap of %d", len(got), MaxEntities)
	}
}
