// Ver 2026-09-22 19:15, by Sonnet 5

package analyze

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/journey"
	"vmr/internal/report"
	"vmr/internal/taskseg"
)

// timestampWriter prefixes every line written through it with
// "2006-01-02 15:04:05.000 " (fmtutil.DisplayZone, millisecond precision).
type timestampWriter struct{ w io.Writer }

func (tw timestampWriter) Write(p []byte) (int, error) {
	if _, err := io.WriteString(tw.w, time.Now().In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05.000")+" "); err != nil {
		return 0, err
	}
	if _, err := tw.w.Write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (r *Run) progressWriter() io.Writer {
	if r.Progress != nil {
		return r.Progress
	}
	return timestampWriter{w: os.Stdout}
}

// setupDetailWriter creates {outDir}/requests/details and starts the
// detail-page worker pool when detailsOn.
func setupDetailWriter(outDir string, detailsOn bool, lang i18n.Lang, tw io.Writer, prof taskseg.Profile) (dw *report.DetailWriter, detailDir string, onRecord func(*audit.Record, *report.ReqInfo), err error) {
	detailDir = filepath.Join(outDir, "requests", "details")
	if !detailsOn {
		return nil, detailDir, nil, nil
	}
	dw, err = report.NewDetailWriter(detailDir, lang, prof)
	if err != nil {
		return nil, detailDir, nil, err
	}
	fmt.Fprintf(tw, "detail export: writing into %s (runs concurrently with the pass below)\n", detailDir)
	return dw, detailDir, dw.Submit, nil
}

// DetailDirHasFiles reports whether dir already contains at least one entry.
func DetailDirHasFiles(dir string) bool {
	return detailDirHasFiles(dir)
}

func detailDirHasFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// detailsPresentFor is the criterion for Meta.DetailsEnabled.
func detailsPresentFor(detailsOn bool, detailDir string) bool {
	return detailsOn || detailDirHasFiles(detailDir)
}

// runReportHalf runs the macro report half against r's already-resolved options.
func (r *Run) runReportHalf() (*report.Report, error) {
	rep, err := r.runReport(r.Paths, r.progressWriter())
	if err != nil {
		return nil, fmt.Errorf("analyze (report half): %w", err)
	}
	return rep, nil
}

// runReport executes the report half's full pipeline — session analysis,
// aggregation, pricing/quota resolution, and every derived file it writes.
func (r *Run) runReport(paths []string, tw io.Writer) (*report.Report, error) {
	if err := os.MkdirAll(r.OutDir, 0o700); err != nil {
		return nil, err
	}

	dw, detailDir, onRecord, err := setupDetailWriter(r.OutDir, r.DetailsOn, r.Lang, tw, r.profile())
	if err != nil {
		return nil, err
	}
	requestsDir := filepath.Join(r.OutDir, "requests")
	if err := os.MkdirAll(requestsDir, 0o700); err != nil {
		return nil, err
	}
	reqPath := filepath.Join(requestsDir, "index.json")
	cacheDir := filepath.Join(r.OutDir, ".cache", "parse")
	priorCache := ctxgraph.LoadCacheDir(cacheDir)
	now := time.Now()

	fmt.Fprintf(tw, "session analysis + aggregation: scanning %d file(s)...\n", len(paths))
	rep, sess, cache, err := report.Build(report.BuildOptions{
		Paths:             paths,
		Now:               now,
		Progress:          tw,
		PricingInfo:       r.PricingInfo,
		PricingSrc:        r.PriceRes,
		OnRecord:          onRecord,
		Profile:           r.profile(),
		PriorCache:        priorCache,
		Quotas:            r.Quotas,
		ExcludeClientTags: r.ExcludeClientTags,
	})
	if err != nil {
		return nil, err
	}

	if r.QuotaJSONPath != "" && len(rep.ProviderQuotas) > 0 {
		rep.Meta.QuotaJSONPath = r.QuotaJSONPath
		rep.Meta.QuotaInputOutsideLogDir = r.QuotaInputOutsideLogDir
	}
	rep.Meta.ReportConfigPath = r.ReportConfigSource
	rep.Meta.DetailsEnabled = detailsPresentFor(r.DetailsOn, detailDir)
	if err := report.WriteMacroSlices(r.OutDir, rep); err != nil {
		return nil, err
	}

	if dw != nil {
		n, err := dw.Close()
		if err != nil {
			return nil, fmt.Errorf("details: %w", err)
		}
		fmt.Fprintf(tw, "%d detail file(s) (.md) in %s\n", n, detailDir)
	}

	_, lineageToJourney := loadJourneysLink(r.OutDir)

	rows := rep.RequestRows()
	if err := ctxgraph.SaveCacheDir(cacheDir, cache); err != nil {
		return nil, fmt.Errorf("parse cache: %w", err)
	}
	if err := report.WriteRequestsIndex(rep, sess, requestsDir, lineageToJourney); err != nil {
		return nil, fmt.Errorf("requests index: %w", err)
	}
	fmt.Fprintf(tw, "%s (%d rows)\n", reqPath, len(rows))

	failedRows := report.FailedRequestRows(rows)
	failedJSONLPath := filepath.Join(requestsDir, "failed.jsonl")
	nFailed, err := report.WriteRequestsJSONL(failedRows, failedJSONLPath)
	if err != nil {
		return nil, fmt.Errorf("failed-requests export: %w", err)
	}
	fmt.Fprintf(tw, "%s (%d rows)\n", failedJSONLPath, nFailed)

	if err := writeReportManifest(r.OutDir, rep, r.Lang); err != nil {
		return nil, err
	}

	if err := renderMacroReportFromDisk(r.OutDir, r.Lang); err != nil {
		return nil, fmt.Errorf("render macro report: %w", err)
	}
	mdPath := filepath.Join(r.OutDir, "vmr-report.md")
	fmt.Fprintf(tw, "%d records (%d parse errors) from %d file(s)\n%s\n%s\n",
		rep.Meta.Records, rep.Meta.ParseErrors, len(paths), r.OutDir+"/macro/*.json + manifest.json", mdPath)

	if err := renderFailedIndexFromDisk(requestsDir, r.Lang, detailDir); err != nil {
		return nil, fmt.Errorf("failed-requests index: %w", err)
	}
	fmt.Fprintf(tw, "%s\n", filepath.Join(requestsDir, "failed.md"))
	return rep, nil
}

// writeReportManifest builds and commits the snapshot manifest.
func writeReportManifest(outDir string, rep *report.Report, lang i18n.Lang) error {
	manifest, err := report.BuildManifest(outDir, rep, lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: manifest not written — this snapshot will be treated as invalid by -render-only and the L2 cache until the next analyze: %v\n", err)
		return nil
	}
	if err := report.WriteManifest(outDir, manifest); err != nil {
		fmt.Fprintf(os.Stderr, "warning: manifest not written — this snapshot will be treated as invalid by -render-only and the L2 cache until the next analyze: %v\n", err)
	}
	return nil
}

// renderMacroReportFromDisk reads the manifest plus the five macro slices from
// outDir, builds the MacroReportVM, and serializes it to vmr-report.md.
func renderMacroReportFromDisk(outDir string, lang i18n.Lang) error {
	rep, err := report.LoadReport(outDir)
	if err != nil {
		return fmt.Errorf("load report json: %w", err)
	}
	journeysLink, lineageToJourney := loadJourneysLink(outDir)
	md := report.MacroMarkdown(rep, lang, journeysLink, lineageToJourney)
	mdPath := filepath.Join(outDir, "vmr-report.md")
	return os.WriteFile(mdPath, []byte(md), 0o600)
}

// renderFailedIndexFromDisk reads requests/index.json (or requests/failed.jsonl)
// from requestsDir and renders requests/failed.md.
func renderFailedIndexFromDisk(requestsDir string, lang i18n.Lang, detailDir string) error {
	var rows []report.RequestRow
	indexPath := filepath.Join(requestsDir, "index.json")
	if data, err := os.ReadFile(indexPath); err == nil {
		var idx report.RequestsIndex
		if err := json.Unmarshal(data, &idx); err == nil {
			rows = idx.Requests
		}
	}
	if len(rows) == 0 {
		failedJSONLPath := filepath.Join(requestsDir, "failed.jsonl")
		if data, err := os.ReadFile(failedJSONLPath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				var r report.RequestRow
				if err := json.Unmarshal([]byte(line), &r); err == nil {
					rows = append(rows, r)
				}
			}
		}
	}
	return report.WriteFailedIndex(rows, requestsDir, lang, detailDir)
}

// loadJourneysLink reads {outDir}/journeys/index.json if present and
// returns both navigation aids it feeds.
func loadJourneysLink(outDir string) (*report.JourneysLinkInfo, map[string]string) {
	indexPath := filepath.Join(outDir, "journeys", "index.json")
	idx := journey.LoadJourneyIndex(indexPath)
	if len(idx.Journeys) == 0 {
		return nil, nil
	}

	lineageToJourney := map[string]string{}
	from, to := idx.Journeys[0].Start, idx.Journeys[0].End
	for _, j := range idx.Journeys {
		if j.Start.Before(from) {
			from = j.Start
		}
		if j.End.After(to) {
			to = j.End
		}
		if j.Rendered == "" {
			continue
		}
		for _, lin := range j.Lineages {
			lineageToJourney[lin] = j.Rendered
		}
	}
	info := &report.JourneysLinkInfo{
		Path:         "journeys/index.md",
		JourneyCount: len(idx.Journeys),
		FromDisplay:  from.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05"),
		ToDisplay:    to.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05"),
	}
	return info, lineageToJourney
}
