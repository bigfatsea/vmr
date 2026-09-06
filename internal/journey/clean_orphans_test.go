// Ver 2026-09-06, by Gemini 3.8 Flash

package journey

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanOrphanJourneys(t *testing.T) {
	tempDir := t.TempDir()

	detailsDir := filepath.Join(tempDir, "journeys", "details")
	comparesDir := filepath.Join(tempDir, "compares")
	reqDetailsDir := filepath.Join(tempDir, "requests", "details")
	reqEvidenceDir := filepath.Join(tempDir, "requests", "evidence")

	for _, dir := range []string{detailsDir, comparesDir, reqDetailsDir, reqEvidenceDir, filepath.Join(detailsDir, "subdir")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	writeFile := func(path string, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	assertExists := func(path string, want bool) {
		t.Helper()
		_, err := os.Stat(path)
		exists := err == nil
		if exists != want {
			t.Errorf("file %s exists = %v, want %v", filepath.Base(path), exists, want)
		}
	}

	// Active journey files in detailsDir
	writeFile(filepath.Join(detailsDir, "j-active1.json"), "{}")
	writeFile(filepath.Join(detailsDir, "j-active1.md"), "# active1")
	writeFile(filepath.Join(detailsDir, "j-active2.json"), "{}")
	writeFile(filepath.Join(detailsDir, "j-active2.md"), "# active2")

	// Orphan journey files in detailsDir
	writeFile(filepath.Join(detailsDir, "j-orphan1.json"), "{}")
	writeFile(filepath.Join(detailsDir, "j-orphan1.md"), "# orphan1")
	writeFile(filepath.Join(detailsDir, "j-orphan2.json"), "{}")

	// Non-journey files and subdirectories in detailsDir (must never be deleted)
	writeFile(filepath.Join(detailsDir, "other.txt"), "hello")
	writeFile(filepath.Join(detailsDir, "subdir", "nested.json"), "{}")

	// Files in other directories (must never be touched per D20 strict boundary rule)
	writeFile(filepath.Join(comparesDir, "compare-orphan.json"), "{}")
	writeFile(filepath.Join(comparesDir, "j-orphan1.json"), "{}")
	writeFile(filepath.Join(reqDetailsDir, "r-orphan.json"), "{}")
	writeFile(filepath.Join(reqDetailsDir, "j-orphan1.json"), "{}")
	writeFile(filepath.Join(reqEvidenceDir, "sysprompt-orphan.md"), "# sys")

	// Call clean with activeIDs: "j-active1" and "active2" (testing both with and without "j-" prefix)
	cleaned, err := CleanOrphanJourneys(detailsDir, []string{"j-active1", "active2"})
	if err != nil {
		t.Fatalf("CleanOrphanJourneys failed: %v", err)
	}

	if cleaned != 3 {
		t.Errorf("cleaned = %d, want 3 (j-orphan1.json, j-orphan1.md, j-orphan2.json)", cleaned)
	}

	// Active files must still exist
	assertExists(filepath.Join(detailsDir, "j-active1.json"), true)
	assertExists(filepath.Join(detailsDir, "j-active1.md"), true)
	assertExists(filepath.Join(detailsDir, "j-active2.json"), true)
	assertExists(filepath.Join(detailsDir, "j-active2.md"), true)

	// Orphan files in detailsDir must have been deleted
	assertExists(filepath.Join(detailsDir, "j-orphan1.json"), false)
	assertExists(filepath.Join(detailsDir, "j-orphan1.md"), false)
	assertExists(filepath.Join(detailsDir, "j-orphan2.json"), false)

	// Non-j- files and subdirs must be untouched
	assertExists(filepath.Join(detailsDir, "other.txt"), true)
	assertExists(filepath.Join(detailsDir, "subdir", "nested.json"), true)

	// External directories must be 100% untouched
	assertExists(filepath.Join(comparesDir, "compare-orphan.json"), true)
	assertExists(filepath.Join(comparesDir, "j-orphan1.json"), true)
	assertExists(filepath.Join(reqDetailsDir, "r-orphan.json"), true)
	assertExists(filepath.Join(reqDetailsDir, "j-orphan1.json"), true)
	assertExists(filepath.Join(reqEvidenceDir, "sysprompt-orphan.md"), true)
}

func TestCleanOrphanJourneys_EdgeCases(t *testing.T) {
	// 1. Non-existent dir
	cleaned, err := CleanOrphanJourneys("/path/to/nowhere/details", []string{"j-1"})
	if err != nil || cleaned != 0 {
		t.Errorf("non-existent dir: got (%d, %v), want (0, nil)", cleaned, err)
	}

	// 2. Empty string dir
	cleaned, err = CleanOrphanJourneys("", []string{"j-1"})
	if err != nil || cleaned != 0 {
		t.Errorf("empty dir: got (%d, %v), want (0, nil)", cleaned, err)
	}

	// 3. Empty activeIDs cleans all j-* files
	tempDir := t.TempDir()
	writeFile := func(name string) {
		_ = os.WriteFile(filepath.Join(tempDir, name), []byte("x"), 0o600)
	}
	writeFile("j-a.json")
	writeFile("j-b.md")
	writeFile("readme.md")

	cleaned, err = CleanOrphanJourneys(tempDir, nil)
	if err != nil {
		t.Fatalf("CleanOrphanJourneys nil activeIDs: %v", err)
	}
	if cleaned != 2 {
		t.Errorf("cleaned = %d, want 2", cleaned)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "readme.md")); err != nil {
		t.Error("readme.md should not be cleaned")
	}
}
