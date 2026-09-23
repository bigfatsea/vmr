// Ver 2026-09-22 19:15, by Sonnet 5

package analyze

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vmr/internal/dashboard"
	"vmr/internal/i18n"
	"vmr/internal/journey"
	"vmr/internal/report"
)

// RenderOnlyOptions holds parameters for RunRenderOnly.
type RenderOnlyOptions struct {
	OutDir        string
	RequestedLang string
	LangPassed    bool
	NoCache       bool
}

// RunRenderOnly implements `vmr analyze -render-only`.
// Validates manifest.json, then re-renders in the requested language — or,
// with no -lang, inherits the snapshot's last-rendered language.
func RunRenderOnly(opts RenderOnlyOptions) error {
	m, err := report.ValidateManifest(opts.OutDir)
	if err != nil {
		return fmt.Errorf("-render-only requires a valid manifest: %w", err)
	}

	if !opts.NoCache && tryRenderOnlyL3Cache(opts.OutDir, opts.RequestedLang, opts.LangPassed) {
		return nil
	}

	lang, err := i18n.Parse(m.Lang)
	if err != nil {
		lang = i18n.EN
	}
	if opts.LangPassed {
		reqLang, err := i18n.Parse(opts.RequestedLang)
		if err != nil {
			return fmt.Errorf("-render-only: invalid -lang %q: %w", opts.RequestedLang, err)
		}
		lang = reqLang
	}

	if err := renderAllFromDisk(opts.OutDir, lang); err != nil {
		return err
	}
	if err := writeReportManifest(opts.OutDir, nil, lang); err != nil {
		return err
	}
	recordRenderOnlyL3Cache(opts.OutDir)
	return nil
}

// renderAllFromDisk renders all resident human-readable Markdown products from on-disk JSON.
// Shared by -render-only and full analyze runs.
func renderAllFromDisk(outDir string, lang i18n.Lang) error {
	// 1. vmr-report.md (if the macro slice set exists)
	if _, err := os.Stat(filepath.Join(outDir, report.SliceMacroSummary)); err == nil {
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
		idx := journey.LoadJourneyIndex(indexPath)
		md := journey.RenderJourneyIndexMarkdown(idx, lang)
		if err := writeAtomic(journeysDir, "index.md", []byte(md)); err != nil {
			return fmt.Errorf("write journeys index md: %w", err)
		}

		linkDetails := detailDirHasFiles(detailDir)
		_, reportMDErr := os.Stat(filepath.Join(outDir, "vmr-report.md"))
		reportMDExists := reportMDErr == nil

		for _, jRow := range idx.Journeys {
			base := strings.TrimSuffix(journey.JourneyReportFile(jRow.ID), ".md")
			jsonPath := filepath.Join(journeysDir, "details", base+".json")
			data, err := os.ReadFile(jsonPath)
			if err != nil {
				continue
			}
			var s journey.JourneySummary
			if err := json.Unmarshal(data, &s); err != nil {
				continue
			}
			outPath := filepath.Join(journeysDir, "details", base+".md")
			journeyMD := journey.RenderMarkdownFromSummary(&s, lang, reportMDExists, linkDetails)
			if err := os.WriteFile(outPath, []byte(journeyMD), 0o600); err != nil {
				return fmt.Errorf("write journey md %s: %w", base, err)
			}
		}
	}

	// 4. journeys/benchmarks.md
	benchmarksJSON := filepath.Join(journeysDir, "benchmarks.json")
	if data, err := os.ReadFile(benchmarksJSON); err == nil {
		var stats journey.BenchmarkStats
		if err := json.Unmarshal(data, &stats); err == nil {
			benchMD := journey.RenderBenchmarksMarkdown(stats, lang)
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
				var cmp journey.Comparison
				if err := json.Unmarshal(data, &cmp); err != nil {
					continue
				}
				cmpMDPath := filepath.Join(comparesDir, strings.TrimSuffix(entry.Name(), ".json")+".md")
				if err := os.WriteFile(cmpMDPath, []byte(journey.RenderComparisonMarkdown(cmp, lang)), 0o600); err != nil {
					return fmt.Errorf("write compare md %s: %w", entry.Name(), err)
				}
			}
		}
		_ = RebuildComparesIndex(comparesDir, lang)
	}

	// 6. Idempotently refresh skeletons
	if err := dashboard.WriteSkeletons(outDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: dashboard skeleton refresh failed: %v\n", err)
	}

	return nil
}
