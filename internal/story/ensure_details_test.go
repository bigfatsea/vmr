// Ver 2026-08-20, by Sonnet 5

package story

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vmr/internal/i18n"
	"vmr/internal/reqdetail"
	"vmr/internal/taskseg"
)

func TestEnsureJourneyDetails_MaterializesEveryStep(t *testing.T) {
	j := buildTestJourney(t, 3, false)
	dir := t.TempDir()
	detailDir := filepath.Join(dir, "details")
	evidenceDir := filepath.Join(dir, "evidence")

	var warnings bytes.Buffer
	EnsureJourneyDetails(&warnings, j, detailDir, evidenceDir, taskseg.Generic, i18n.EN)
	if warnings.Len() != 0 {
		t.Fatalf("unexpected warnings: %s", warnings.String())
	}

	steps := journeySteps(j)
	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(steps))
	}
	for _, s := range steps {
		want := reqdetail.FileNameForManifest(s.Manifest)
		target := filepath.Join(detailDir, want)
		if _, err := os.Stat(target); err != nil {
			t.Errorf("step %d: expected detail file %s to exist: %v", s.Seq, target, err)
		}
	}
	// The fixture's leading system message ("sys") should have produced a
	// shared evidence blob too.
	entries, err := os.ReadDir(evidenceDir)
	if err != nil {
		t.Fatalf("ReadDir(evidenceDir): %v", err)
	}
	if len(entries) == 0 {
		t.Errorf("expected at least one sysprompt evidence file, found none")
	}

	// Idempotency: a second call must not error and must not change the
	// file set (EnsureRendered's own existence check should make this a
	// no-op for every already-written Step).
	before, _ := os.ReadDir(detailDir)
	warnings.Reset()
	EnsureJourneyDetails(&warnings, j, detailDir, evidenceDir, taskseg.Generic, i18n.EN)
	if warnings.Len() != 0 {
		t.Fatalf("unexpected warnings on repeat call: %s", warnings.String())
	}
	after, _ := os.ReadDir(detailDir)
	if len(before) != len(after) {
		t.Errorf("repeat call changed the file count: before=%d after=%d", len(before), len(after))
	}
}

// TestEnsureJourneyDetails_GracefulDegradation locks in §2.1(c)'s error
// policy: a detail-export failure (here, detailDir cannot be created
// because a same-named regular file already occupies that path) is
// reported as a warning and does NOT panic or otherwise abort — `vmr
// story` is a read-only offline analysis tool and one bad Step must not
// cost the reader the rest of an otherwise-renderable Journey.
func TestEnsureJourneyDetails_GracefulDegradation(t *testing.T) {
	j := buildTestJourney(t, 2, false)
	dir := t.TempDir()
	// Occupy the details/ path with a plain file so MkdirAll fails.
	detailDir := filepath.Join(dir, "details")
	if err := os.WriteFile(detailDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	evidenceDir := filepath.Join(dir, "evidence")

	var warnings bytes.Buffer
	EnsureJourneyDetails(&warnings, j, detailDir, evidenceDir, taskseg.Generic, i18n.EN)
	if warnings.Len() == 0 {
		t.Fatal("expected a warning when detailDir cannot be created, got none")
	}
	if !strings.Contains(warnings.String(), j.ID) {
		t.Errorf("warning should mention the journey id %q, got: %s", j.ID, warnings.String())
	}
}
