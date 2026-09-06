// Ver 2026-09-15, by pi

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vmr/internal/dashboard"
	"vmr/internal/i18n"
	story "vmr/internal/journey"
	"vmr/internal/report"
)

// runRenderOnly implements `vmr analyze -render-only` (§5.4).
// Validates manifest.json, enforces language inheritance (D10),
// and executes the single render path from disk JSON without touching audit logs.
func runRenderOnly(outDir string, requestedLang string, langPassed bool) error {
	m, err := report.ValidateManifest(outDir)
	if err != nil {
		return fmt.Errorf("-render-only requires a valid manifest: %w", err)
	}

	manifestLang, err := i18n.Parse(m.Lang)
	if err != nil {
		manifestLang = i18n.EN
	}

	if langPassed {
		reqLang, err := i18n.Parse(requestedLang)
		if err != nil || reqLang != manifestLang {
			return fmt.Errorf("-render-only cannot change language (snapshot was generated in %q, requested %q); rerun full analyze with -lang to re-aggregate", m.Lang, requestedLang)
		}
	}

	return renderAllFromDisk(outDir, manifestLang)
}

// renderAllFromDisk renders all resident human-readable Markdown products from on-disk JSON (§5.4).
// Shared by -render-only and full analyze runs (D11).
func renderAllFromDisk(outDir string, lang i18n.Lang) error {
	// 1. vmr-report.md (if vmr-report.json exists)
	if _, err := os.Stat(filepath.Join(outDir, "vmr-report.json")); err == nil {
		if err := renderMacroReportFromDisk(outDir, lang); err != nil {
			return fmt.Errorf("render macro report: %w", err)
		}
	}

	// 2. requests/failed.md
	requestsDir := filepath.Join(outDir, "requests")
	detailDir := filepath.Join(requestsDir, "details")
	if _, err := os.Stat(filepath.Join(requestsDir, "failed.jsonl")); err == nil {
		if err := renderFailedIndexFromDisk(requestsDir, lang, detailDir); err != nil {
			return fmt.Errorf("render failed index: %w", err)
		}
	} else if _, err := os.Stat(filepath.Join(requestsDir, "index.json")); err == nil {
		if err := renderFailedIndexFromDisk(requestsDir, lang, detailDir); err != nil {
			return fmt.Errorf("render failed index: %w", err)
		}
	}

	// 3. journeys/index.md and journeys/details/j-<id>.md
	journeysDir := filepath.Join(outDir, "journeys")
	indexPath := filepath.Join(journeysDir, "index.json")
	if _, err := os.Stat(indexPath); err == nil {
		idx := story.LoadStoryIndex(indexPath)
		md := story.RenderStoryIndexMarkdown(idx, lang)
		if err := os.WriteFile(filepath.Join(journeysDir, "index.md"), []byte(md), 0o600); err != nil {
			return fmt.Errorf("write journeys index md: %w", err)
		}

		// D20: Job list comes from journeys/index.json, never directory scan!
		linkDetails := detailDirHasFiles(detailDir)
		_, reportMDErr := os.Stat(filepath.Join(outDir, "vmr-report.md"))
		reportMDExists := reportMDErr == nil

		for _, jRow := range idx.Journeys {
			base := strings.TrimSuffix(story.JourneyReportFile(jRow.ID, jRow.Partial), ".md")
			jsonPath := filepath.Join(journeysDir, "details", base+".json")
			data, err := os.ReadFile(jsonPath)
			if err != nil {
				continue
			}
			var s story.JourneySummary
			if err := json.Unmarshal(data, &s); err != nil {
				continue
			}
			outPath := filepath.Join(journeysDir, "details", base+".md")
			journeyMD := story.RenderMarkdownFromSummary(&s, lang, reportMDExists, linkDetails)
			// Preserve appended LLM interpretation section if present in existing file
			if oldData, err := os.ReadFile(outPath); err == nil {
				if idx := strings.Index(string(oldData), "\n## LLM "); idx >= 0 {
					journeyMD += string(oldData[idx:])
				}
			}
			if err := os.WriteFile(outPath, []byte(journeyMD), 0o600); err != nil {
				return fmt.Errorf("write journey md %s: %w", base, err)
			}
		}
	}

	// 4. journeys/benchmarks.md
	benchmarksJSON := filepath.Join(journeysDir, "benchmarks.json")
	if data, err := os.ReadFile(benchmarksJSON); err == nil {
		var stats story.CorpusStats
		if err := json.Unmarshal(data, &stats); err == nil {
			benchMD := story.RenderCorpusMarkdown(stats, lang)
			if err := os.WriteFile(filepath.Join(journeysDir, "benchmarks.md"), []byte(benchMD), 0o600); err != nil {
				return fmt.Errorf("write benchmarks md: %w", err)
			}
		}
	}

	// 5. compares/index.md and compares/compare-*.md
	comparesDir := filepath.Join(outDir, "compares")
	if fi, err := os.Stat(comparesDir); err == nil && fi.IsDir() {
		entries, err := os.ReadDir(comparesDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasPrefix(entry.Name(), "compare-") || !strings.HasSuffix(entry.Name(), ".json") || entry.Name() == "index.json" {
					continue
				}
				cmpJSONPath := filepath.Join(comparesDir, entry.Name())
				data, err := os.ReadFile(cmpJSONPath)
				if err != nil {
					continue
				}
				var cmp story.Comparison
				if err := json.Unmarshal(data, &cmp); err != nil {
					continue
				}
				cmpMDPath := filepath.Join(comparesDir, strings.TrimSuffix(entry.Name(), ".json")+".md")
				cmpMD := story.RenderComparisonMarkdown(cmp, lang)
				if oldData, err := os.ReadFile(cmpMDPath); err == nil {
					if idx := strings.Index(string(oldData), "\n## LLM "); idx >= 0 {
						cmpMD += string(oldData[idx:])
					}
				}
				_ = os.WriteFile(cmpMDPath, []byte(cmpMD), 0o600)
			}
		}
		_ = RebuildComparesIndex(comparesDir)
	}

	// 6. Idempotently refresh skeletons (§5.4)
	if err := dashboard.WriteSkeletons(outDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: dashboard skeleton refresh failed: %v\n", err)
	}

	return nil
}
