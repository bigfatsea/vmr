// Ver 2026-07-26, by Sonnet 5
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"vmr/internal/audit"
	"vmr/internal/report"
)

// timestampWriter prefixes every line written through it with
// "2006-01-02 15:04:05.000 " (local time, millisecond precision) — `vmr
// report`'s progress output otherwise has no way to show how long each
// phase/file actually took. One Write() call is assumed to be one
// already-formatted line (true for every fmt.Fprintf call site this wraps),
// so the timestamp lands at the true start of that line, not buffered
// alongside unrelated output.
type timestampWriter struct{ w io.Writer }

func (tw timestampWriter) Write(p []byte) (int, error) {
	if _, err := io.WriteString(tw.w, time.Now().Format("2006-01-02 15:04:05.000")+" "); err != nil {
		return 0, err
	}
	if _, err := tw.w.Write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// defaultPricingFile is the pricing sidecar `vmr report` auto-loads when
// -pricing is not given — the same relative-to-cwd convention config.yaml
// itself uses with -c. Auto-load is silently skipped (no error, no $
// estimates) when this file doesn't exist; an explicit -pricing always wins
// and is used exactly as given, existent or not (report.LoadPricing already
// treats a missing explicit path as "no pricing" rather than an error).
const defaultPricingFile = "pricing.yaml"

// cmdReport aggregates audit JSONL into internal/report's output:
// vmr-report.json/.md, vmr-requests.jsonl/.md (+ per-tag siblings),
// vmr-requests-failed.jsonl/.md (error-analysis index: outcome ==
// error|canceled plus ok-but-truncated, additive — doesn't remove those
// requests from anything above), and one details/*.md+.json per request.
// Inputs may freely mix live plain .jsonl files and .jsonl.zst files that
// the audit logger's housekeeping sweep has since compressed
// (internal/report decompresses transparently) — e.g.
// `vmr report 'vmr-audit-*.jsonl*'`.
func cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	outDir := fs.String("o", "reports", "output directory (default: ./reports)")
	detailsOn := fs.Bool("details", true, "also export one Markdown+JSON file per request into {out}/details/")
	pricingPath := fs.String("pricing", "", "pricing sidecar yaml (per-endpoint unit prices); absent => auto-load ./pricing.yaml if present, else no $ estimates")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("no input files; usage: vmr report [-o dir] [-pricing pricing.yaml] <audit.jsonl|glob>...")
	}
	seen := map[string]bool{}
	var paths []string
	for _, arg := range fs.Args() {
		matches, err := filepath.Glob(arg)
		if err != nil {
			return fmt.Errorf("bad pattern %q: %w", arg, err)
		}
		if len(matches) == 0 {
			return fmt.Errorf("no files match %q", arg)
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				paths = append(paths, m)
			}
		}
	}
	sort.Strings(paths)

	tw := timestampWriter{w: os.Stdout}

	resolvedPricingPath := *pricingPath
	if resolvedPricingPath == "" {
		if _, err := os.Stat(defaultPricingFile); err == nil {
			resolvedPricingPath = defaultPricingFile
			fmt.Fprintf(tw, "pricing: auto-loaded %s (pass -pricing to override)\n", defaultPricingFile)
		}
	}
	pricing, err := report.LoadPricing(resolvedPricingPath)
	if err != nil {
		return err
	}

	// 0o700/0o600: report outputs embed full conversation bodies from the
	// 0600 audit files - the derived copies must not loosen that. Created
	// up front now (used to happen after Build succeeded): the detail
	// writer below needs its output directory to exist before Build's
	// aggregation pass starts feeding it records, since detail rendering
	// now happens inside that same pass instead of as a separate step
	// afterward.
	if err := os.MkdirAll(*outDir, 0o700); err != nil {
		return err
	}

	// Build's onRecord hook (nil when -details=false) renders+writes each
	// record's detail page during the aggregation pass itself, on its own
	// worker pool — no separate third read of the audit source for detail
	// export anymore. Build's own success/failure never depends on this:
	// a detail-write failure surfaces only when dw.Close() is checked below,
	// well after vmr-report.json/md are already safely on disk — same
	// robustness the old separate-WriteDetails-step had, just without the
	// extra pass.
	var dw *report.DetailWriter
	detailDir := filepath.Join(*outDir, "details")
	var onRecord func(*audit.Record, *report.ReqInfo)
	if *detailsOn {
		dw, err = report.NewDetailWriter(detailDir)
		if err != nil {
			return err
		}
		onRecord = dw.Submit
		fmt.Fprintf(tw, "detail export: writing into %s (runs concurrently with the pass below)\n", detailDir)
	}

	// The gap between this line's timestamp and the first "[1/N]" line below
	// is session analysis (AnalyzeSessions) — a full, currently silent pass
	// over every input file that Build() always runs before its own
	// per-file aggregation loop starts printing.
	fmt.Fprintf(tw, "session analysis + aggregation: scanning %d file(s)...\n", len(paths))
	rep, sess, err := report.Build(paths, time.Now(), tw, pricing, onRecord)
	if err != nil {
		return err
	}
	jsonPath := filepath.Join(*outDir, "vmr-report.json")
	mdPath := filepath.Join(*outDir, "vmr-report.md")
	if err := report.WriteJSON(rep, jsonPath); err != nil {
		return err
	}
	if err := os.WriteFile(mdPath, []byte(report.Markdown(rep)), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(tw, "%d records (%d parse errors) from %d file(s)\n", rep.Meta.Records, rep.Meta.ParseErrors, len(paths))
	fmt.Fprintf(tw, "%s\n", jsonPath)
	fmt.Fprintf(tw, "%s\n", mdPath)

	if dw != nil {
		n, err := dw.Close()
		if err != nil {
			return fmt.Errorf("details: %w", err)
		}
		fmt.Fprintf(tw, "%d detail file(s) (.md + .json) in %s\n", n, detailDir)
	}

	// Requests index (+ per-tag siblings) + jsonl.
	rows := rep.RequestRows()
	reqPath := filepath.Join(*outDir, "vmr-requests.jsonl")
	nReq, err := report.WriteRequestsJSONL(rows, reqPath)
	if err != nil {
		return fmt.Errorf("requests export: %w", err)
	}
	fmt.Fprintf(tw, "%s (%d rows)\n", reqPath, nReq)
	if err := report.WriteRequestsIndex(rep, sess, *outDir); err != nil {
		return fmt.Errorf("requests index: %w", err)
	}
	fmt.Fprintf(tw, "%s\n", filepath.Join(*outDir, "vmr-requests.md"))

	// Failed-requests index: a dedicated error-analysis view (outcome ==
	// error|canceled, plus ok-but-truncated), each row linking to its
	// details/*.md+*.json. Purely additive — every other report/requests
	// output above is unaffected and still lists these same failed requests
	// inline as before.
	failedRows := report.FailedRequestRows(rows)
	failedJSONLPath := filepath.Join(*outDir, "vmr-requests-failed.jsonl")
	nFailed, err := report.WriteRequestsJSONL(failedRows, failedJSONLPath)
	if err != nil {
		return fmt.Errorf("failed-requests export: %w", err)
	}
	fmt.Fprintf(tw, "%s (%d rows)\n", failedJSONLPath, nFailed)
	if err := report.WriteFailedIndex(rows, *outDir); err != nil {
		return fmt.Errorf("failed-requests index: %w", err)
	}
	fmt.Fprintf(tw, "%s\n", filepath.Join(*outDir, "vmr-requests-failed.md"))
	return nil
}
