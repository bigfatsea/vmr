// Ver 2026-09-07, by pi
//
// Dynamic derived comparison index (§3.8, D21).
// Scans compares/compare-*.json on every analyze run to build compares/index.{json,md}.
// The entire compares/ tree is deliberately excluded from manifest.json fingerprints.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/journey"
)

// CompareItem describes one pairwise comparison entry in compares/index.json (§3.8, D21).
type CompareItem struct {
	Filename string `json:"filename"`
	Markdown string `json:"markdown"`
	HTML     string `json:"html,omitempty"`
	// Partial marks a head-truncated side (D19: data, not filename suffix).
	Partial bool               `json:"partial,omitempty"`
	A       journey.JourneyRef `json:"a_journey"`
	B       journey.JourneyRef `json:"b_journey"`
}

// ComparesIndex is the root schema of compares/index.json (§3.8, D21).
type ComparesIndex struct {
	Count    int           `json:"count"`
	Compares []CompareItem `json:"compares"`
}

// RebuildComparesIndex scans comparesDir for compare-*.json files, parses their
// journey references, sorts them deterministically, and writes index.json and
// index.md atomically (0600). An empty directory produces an empty-state guide.
func RebuildComparesIndex(comparesDir string, lang i18n.Lang) error {
	if err := os.MkdirAll(comparesDir, 0o700); err != nil {
		return fmt.Errorf("mkdir compares dir: %w", err)
	}

	entries, err := os.ReadDir(comparesDir)
	if err != nil {
		return fmt.Errorf("read compares dir: %w", err)
	}

	items := make([]CompareItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "compare-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		if name == "index.json" {
			continue
		}

		fullPath := filepath.Join(comparesDir, name)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		var cmp struct {
			A       journey.JourneyRef `json:"a_journey"`
			B       journey.JourneyRef `json:"b_journey"`
			Partial bool               `json:"partial"`
		}
		if err := json.Unmarshal(data, &cmp); err != nil {
			continue
		}

		mdName := strings.TrimSuffix(name, ".json") + ".md"
		item := CompareItem{
			Filename: name,
			Markdown: mdName,
			Partial:  cmp.Partial,
			A:        cmp.A,
			B:        cmp.B,
		}
		htmlName := strings.TrimSuffix(name, ".json") + ".html"
		if fi, err := os.Stat(filepath.Join(comparesDir, htmlName)); err == nil && !fi.IsDir() {
			item.HTML = htmlName
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Filename < items[j].Filename
	})

	idx := ComparesIndex{
		Count:    len(items),
		Compares: items,
	}

	jsonData, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal compares index: %w", err)
	}
	jsonData = append(jsonData, '\n')
	if err := writeAtomic(comparesDir, "index.json", jsonData); err != nil {
		return fmt.Errorf("write compares index.json: %w", err)
	}

	mdData := renderComparesIndexMarkdown(items, lang)
	if err := writeAtomic(comparesDir, "index.md", []byte(mdData)); err != nil {
		return fmt.Errorf("write compares index.md: %w", err)
	}

	return nil
}

func renderComparesIndexMarkdown(items []CompareItem, lang i18n.Lang) string {
	t := i18n.ComparesIndex(lang)
	var buf bytes.Buffer
	buf.WriteString(t.Title)

	if len(items) == 0 {
		buf.WriteString(t.EmptyState)
		return buf.String()
	}

	buf.WriteString(t.Total(len(items)))
	buf.WriteString(t.TableHeader)

	for _, item := range items {
		formatSide := func(ref journey.JourneyRef) string {
			title := ref.Title
			if title == "" {
				title = ref.ID
			}
			var timeStr string
			if !ref.From.IsZero() && !ref.To.IsZero() {
				fromStr := ref.From.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05")
				toStr := ref.To.In(fmtutil.DisplayZone).Format("15:04:05")
				timeStr = t.TimeRange(fromStr, toStr)
			}
			stepStr := ""
			if ref.Steps > 0 {
				stepStr = t.Steps(ref.Steps)
			}
			return fmt.Sprintf("**%s** (`%s`)%s%s", title, ref.ID, stepStr, timeStr)
		}

		sideA := formatSide(item.A)
		sideB := formatSide(item.B)
		if item.A.Partial {
			sideA += t.PartialMark
		}
		if item.B.Partial {
			sideB += t.PartialMark
		}

		links := fmt.Sprintf("[%s](%s)", item.Markdown, item.Markdown)
		if item.HTML != "" {
			links += fmt.Sprintf(" · [%s](%s)", item.HTML, item.HTML)
		}

		fmt.Fprintf(&buf, "| %s | %s | %s |\n", sideA, sideB, links)
	}

	return buf.String()
}

func writeAtomic(dir, filename string, data []byte) error {
	tmp, err := os.CreateTemp(dir, filename+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	target := filepath.Join(dir, filename)
	return os.Rename(tmpName, target)
}
