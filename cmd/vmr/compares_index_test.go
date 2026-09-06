// Ver 2026-09-06, by Gemini 3.8 Flash

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	journey "vmr/internal/journey"
)

func TestRebuildComparesIndex_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	comparesDir := filepath.Join(dir, "compares")

	if err := RebuildComparesIndex(comparesDir); err != nil {
		t.Fatalf("RebuildComparesIndex on non-existent/empty dir: %v", err)
	}

	jsonPath := filepath.Join(comparesDir, "index.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	var idx ComparesIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("unmarshal index.json: %v", err)
	}
	if idx.Count != 0 || len(idx.Compares) != 0 {
		t.Errorf("want Count 0 and empty Compares, got Count=%d, len=%d", idx.Count, len(idx.Compares))
	}

	mdPath := filepath.Join(comparesDir, "index.md")
	mdData, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	if !strings.Contains(string(mdData), "No comparisons found in this directory") {
		t.Errorf("want empty state guide in index.md, got:\n%s", string(mdData))
	}
}

func TestRebuildComparesIndex_ScanAndSelfHealing(t *testing.T) {
	comparesDir := filepath.Join(t.TempDir(), "compares")
	if err := os.MkdirAll(comparesDir, 0o700); err != nil {
		t.Fatal(err)
	}

	makeCompareJSON := func(name string, idA, titleA, idB, titleB string) {
		cmp := struct {
			A journey.JourneyRef `json:"a_journey"`
			B journey.JourneyRef `json:"b_journey"`
		}{
			A: journey.JourneyRef{ID: idA, Title: titleA, From: time.Now().Add(-10 * time.Minute), To: time.Now(), Steps: 5},
			B: journey.JourneyRef{ID: idB, Title: titleB, From: time.Now().Add(-5 * time.Minute), To: time.Now(), Steps: 6},
		}
		data, _ := json.MarshalIndent(cmp, "", "  ")
		if err := os.WriteFile(filepath.Join(comparesDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Write first comparison
	f1 := "compare-j-test1-vs-j-test2.json"
	makeCompareJSON(f1, "j-test1", "Task 1", "j-test2", "Task 2")

	if err := RebuildComparesIndex(comparesDir); err != nil {
		t.Fatalf("RebuildComparesIndex: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(comparesDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var idx ComparesIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}
	if idx.Count != 1 || len(idx.Compares) != 1 {
		t.Fatalf("want count 1, got %d", idx.Count)
	}
	if idx.Compares[0].Filename != f1 || idx.Compares[0].A.Title != "Task 1" || idx.Compares[0].B.Title != "Task 2" {
		t.Errorf("unexpected compare item: %+v", idx.Compares[0])
	}

	// 2. Write second comparison
	f2 := "compare-j-alpha-vs-j-beta.json"
	makeCompareJSON(f2, "j-alpha", "Alpha", "j-beta", "Beta")

	if err := RebuildComparesIndex(comparesDir); err != nil {
		t.Fatalf("RebuildComparesIndex: %v", err)
	}

	data, err = os.ReadFile(filepath.Join(comparesDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}
	if idx.Count != 2 || len(idx.Compares) != 2 {
		t.Fatalf("want count 2, got %d", idx.Count)
	}
	// Deterministic sort: alpha comes before test1
	if idx.Compares[0].Filename != f2 || idx.Compares[1].Filename != f1 {
		t.Errorf("expected sorted filenames [%s, %s], got [%s, %s]", f2, f1, idx.Compares[0].Filename, idx.Compares[1].Filename)
	}

	// 3. Self-healing: Delete f1 and re-scan
	if err := os.Remove(filepath.Join(comparesDir, f1)); err != nil {
		t.Fatal(err)
	}
	if err := RebuildComparesIndex(comparesDir); err != nil {
		t.Fatalf("RebuildComparesIndex after deletion: %v", err)
	}

	data, err = os.ReadFile(filepath.Join(comparesDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}
	if idx.Count != 1 || len(idx.Compares) != 1 {
		t.Fatalf("want count 1 after deletion, got %d", idx.Count)
	}
	if idx.Compares[0].Filename != f2 {
		t.Errorf("want remaining item %s, got %s", f2, idx.Compares[0].Filename)
	}

	// Check index.md table
	mdData, err := os.ReadFile(filepath.Join(comparesDir, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	mdStr := string(mdData)
	if !strings.Contains(mdStr, "Total comparisons: 1") || !strings.Contains(mdStr, "Alpha") {
		t.Errorf("index.md missing expected content:\n%s", mdStr)
	}
}
