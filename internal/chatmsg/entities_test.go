// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

package chatmsg

import (
	"strconv"
	"strings"
	"testing"
)

// TestExtractEntities locks in ExtractEntities' behavior:
// URLs with stripped punctuation, file paths, system paths, directory paths,
// CamelCase/snake_case symbols, and whitelisted CLI commands are found in order.
func TestExtractEntities(t *testing.T) {
	t.Parallel()
	text := "see internal/report/session.go and https://example.com/docs, " +
		"also internal/report/session.go again and README.md"
	got := ExtractEntities(text)
	want := []string{"internal/report/session.go", "https://example.com/docs", "README.md"}
	if len(got) != len(want) {
		t.Fatalf("ExtractEntities = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entity[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExtractEntities_ExtendedPatterns(t *testing.T) {
	t.Parallel()
	text := "We checked /etc/hosts, ./cmd/vmr, internal/story/, ExtractEntities, exact_repeat_tool_call, and ran go test ./..."
	got := ExtractEntities(text)

	contains := func(list []string, target string) bool {
		for _, s := range list {
			if s == target {
				return true
			}
		}
		return false
	}

	expected := []string{
		"/etc/hosts",
		"./cmd/vmr",
		"internal/story/",
		"ExtractEntities",
		"exact_repeat_tool_call",
		"go test",
	}

	for _, exp := range expected {
		if !contains(got, exp) {
			t.Errorf("ExtractEntities missing expected entity %q, got: %v", exp, got)
		}
	}
}

func TestExtractEntities_NegativeCases(t *testing.T) {
	t.Parallel()
	// Negative cases: conversational phrases and common slashes shouldn't be picked up as entities
	text := "let's go read the file and/or check yes/no options before we go ahead"
	got := ExtractEntities(text)

	// "and/" and "yes/" (not just the full "and/or"/"yes/no") are the actual
	// tokens a single-segment-before-the-slash regex would extract — a past
	// version of this test checked only the un-truncated phrase and missed
	// the real false positive.
	forbidden := []string{"go read", "and/or", "yes/no", "go ahead", "let's go", "and/", "yes/"}
	for _, f := range forbidden {
		for _, g := range got {
			if g == f {
				t.Errorf("ExtractEntities extracted unwanted noise %q", f)
			}
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
