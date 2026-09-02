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

func TestCollectEntitySpans_ContainedSpansFiltered(t *testing.T) {
	t.Parallel()
	// "internal/report/session.go" contains "session.go", "internal/report", etc.
	// Only the outermost span should survive.
	text := "Check internal/report/session.go for details."
	spans := collectEntitySpans(text)
	if len(spans) == 0 {
		t.Fatal("expected at least 1 span")
	}
	if spans[0].val != "internal/report/session.go" {
		t.Errorf("spans[0].val = %q, want %q", spans[0].val, "internal/report/session.go")
	}
}

func BenchmarkCollectEntitySpans_LargeOutput(b *testing.B) {
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString("src/component/module_")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(".go:42 in ClassName.MethodName with /etc/config.json and https://example.com/api\n")
	}
	text := sb.String()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = collectEntitySpans(text)
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

func TestExtractEntitiesNear(t *testing.T) {
	t.Parallel()
	// "not found" is about config.toml; ChangeSet is ~120 bytes away in an
	// unrelated clause, os.replace is ~300 bytes away.
	text := "ENOENT: config.toml not found while loading. " +
		"the loader then falls back to defaults and continues without a ChangeSet. " +
		strings.Repeat("padding sentence with no entities at all here. ", 4) +
		"later code calls os.replace to swap the file atomically."
	anchors := [][]int{{7, 33}} // roughly the "config.toml not found" span

	near := ExtractEntitiesNear(text, anchors, 60)
	if !contains(near, "config.toml") {
		t.Fatalf("config.toml should be within the window: %v", near)
	}
	if contains(near, "os.replace") {
		t.Fatalf("os.replace is ~300 bytes away, should be excluded: %v", near)
	}

	if got := ExtractEntitiesNear(text, nil, 60); got != nil {
		t.Fatalf("no anchors should return nil, got %v", got)
	}

	// Wide enough window pulls everything in — same set as ExtractEntities.
	wide := ExtractEntitiesNear(text, anchors, 100000)
	if len(wide) != len(ExtractEntities(text)) {
		t.Fatalf("wide window = %v, want same as ExtractEntities = %v", wide, ExtractEntities(text))
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
