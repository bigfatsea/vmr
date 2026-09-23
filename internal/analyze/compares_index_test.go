// Ver 2026-09-22 19:15, by Sonnet 5

package analyze

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/i18n"
	"vmr/internal/journey"
)

func TestRebuildComparesIndex_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	comparesDir := filepath.Join(dir, "compares")

	if err := RebuildComparesIndex(comparesDir, i18n.EN); err != nil {
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

	if err := RebuildComparesIndex(comparesDir, i18n.EN); err != nil {
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
		t.Fatalf("want 1 item, got count=%d, len=%d", idx.Count, len(idx.Compares))
	}
	if idx.Compares[0].Filename != f1 {
		t.Errorf("want Filename %s, got %s", f1, idx.Compares[0].Filename)
	}

	// 2. Add a second comparison and re-run (determinism check)
	f2 := "compare-j-aaa-vs-j-bbb.json"
	makeCompareJSON(f2, "j-aaa", "Task A", "j-bbb", "Task B")

	if err := RebuildComparesIndex(comparesDir, i18n.EN); err != nil {
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
		t.Fatalf("want 2 items, got count=%d, len=%d", idx.Count, len(idx.Compares))
	}
	// Alphabetical sort: f2 ("compare-j-aaa...") must be first
	if idx.Compares[0].Filename != f2 || idx.Compares[1].Filename != f1 {
		t.Errorf("sort order mismatch: got [%s, %s]", idx.Compares[0].Filename, idx.Compares[1].Filename)
	}

	// 3. Delete f2 and re-run (self-healing check)
	if err := os.Remove(filepath.Join(comparesDir, f2)); err != nil {
		t.Fatal(err)
	}
	if err := RebuildComparesIndex(comparesDir, i18n.EN); err != nil {
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
		t.Fatalf("want 1 item after deletion, got count=%d, len=%d", idx.Count, len(idx.Compares))
	}
	if idx.Compares[0].Filename != f1 {
		t.Errorf("want Filename %s, got %s", f1, idx.Compares[0].Filename)
	}

	mdBytes, err := os.ReadFile(filepath.Join(comparesDir, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mdBytes), "Task 1") {
		t.Errorf("index.md missing Task 1:\n%s", string(mdBytes))
	}
}

func TestRebuildComparesIndex_LanguageZH(t *testing.T) {
	comparesDir := filepath.Join(t.TempDir(), "compares")
	if err := os.MkdirAll(comparesDir, 0o700); err != nil {
		t.Fatal(err)
	}

	cmp := struct {
		A journey.JourneyRef `json:"a_journey"`
		B journey.JourneyRef `json:"b_journey"`
	}{
		A: journey.JourneyRef{ID: "j-alpha", Title: "任务甲", From: time.Now().Add(-10 * time.Minute), To: time.Now(), Steps: 3},
		B: journey.JourneyRef{ID: "j-beta", Title: "任务乙", From: time.Now().Add(-5 * time.Minute), To: time.Now(), Steps: 4},
	}
	data, err := json.MarshalIndent(cmp, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(comparesDir, "compare-j-alpha-vs-j-beta.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RebuildComparesIndex(comparesDir, i18n.ZH); err != nil {
		t.Fatalf("RebuildComparesIndex(ZH): %v", err)
	}

	mdData, err := os.ReadFile(filepath.Join(comparesDir, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	mdStr := string(mdData)

	if !strings.Contains(mdStr, "Journey 对照索引") {
		t.Errorf("want Chinese title 'Journey 对照索引', got:\n%s", mdStr)
	}
	if !strings.Contains(mdStr, "对照总数：1") {
		t.Errorf("want Chinese total '对照总数：1', got:\n%s", mdStr)
	}
	if !strings.Contains(mdStr, "A 侧（基线）") {
		t.Errorf("want Chinese table header 'A 侧（基线）', got:\n%s", mdStr)
	}
	if strings.Contains(mdStr, "Side A (Baseline)") {
		t.Errorf("expected no English header 'Side A (Baseline)', got:\n%s", mdStr)
	}
	if !strings.Contains(mdStr, "（3 步）") {
		t.Errorf("want Chinese steps '（3 步）', got:\n%s", mdStr)
	}
}
