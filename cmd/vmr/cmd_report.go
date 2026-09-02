// Ver 2026-07-29 14:00, by Sonnet 5
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/pricing"
	"vmr/internal/report"
)

// timestampWriter prefixes every line written through it with
// "2006-01-02 15:04:05.000 " (fmtutil.DisplayZone, millisecond precision) — `vmr
// report`'s progress output otherwise has no way to show how long each
// phase/file actually took. One Write() call is assumed to be one
// already-formatted line (true for every fmt.Fprintf call site this wraps),
// so the timestamp lands at the true start of that line, not buffered
// alongside unrelated output.
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

// buildPricing resolves standard pricing tables and optional provider/global overrides from config.
// Degrades gracefully to embedded standard pricing if config is missing or invalid.
func buildPricing(cfg *config.Config, loadErr error, configPath string, tw io.Writer, displayCCY string, extraRates map[string]float64) (*pricing.Resolver, *report.Pricing) {
	standard, err := pricing.LoadStandard()
	if err != nil {
		fmt.Fprintf(tw, "pricing: embedded standard table failed to load (%v) — no $ estimates\n", err)
		return nil, nil
	}
	summary := &report.Pricing{Currency: standard.Currency, StandardGeneratedAt: standard.GeneratedAt}

	var resolver *pricing.Resolver
	var configRates map[string]float64
	if loadErr != nil {
		// cmdReport already printed one unified warning for cfgErr — a
		// second, near-identical one here would just repeat it.
		resolver = pricing.NewResolver(standard, nil, 1, summary.Currency)
	} else {
		table := standard
		factor := 1.0
		if t, err := cfg.PricingTable(); err == nil && t != nil {
			table = t
			factor, _ = cfg.PricingAccounting()
		}
		perProvider := map[string]pricing.ProviderPolicy{}
		overrideCount := 0
		for name, policy := range cfg.ProviderPricingPolicies {
			perProvider[name] = policy
			overrideCount += len(policy.Overrides)
		}
		if cfg.Pricing != nil {
			summary.Currency = cfg.Pricing.Currency
			summary.Supplement = cfg.Pricing.Supplement
			configRates = cfg.Pricing.ExchangeRate
		}
		summary.ProviderOverrides = overrideCount
		if overrideCount > 0 {
			fmt.Fprintf(tw, "pricing: %d provider override rule(s) loaded from %s\n", overrideCount, configPath)
		}
		resolver = pricing.NewResolver(table, perProvider, factor, summary.Currency)
	}
	if summary.Currency == "" {
		summary.Currency = "USD"
	}

	if displayCCY != "" && !strings.EqualFold(displayCCY, summary.Currency) {
		rates := map[string]float64{}
		for k, v := range configRates {
			rates[k] = v
		}
		for k, v := range extraRates { // report.yaml's own rates win over config.yaml's on a matching key
			rates[k] = v
		}
		if factor, ok := pricing.FactorBetween(summary.Currency, displayCCY, rates); ok {
			resolver = resolver.WithDisplayFactor(factor)
			summary.Currency = displayCCY
		} else {
			summary.RequestedCurrency = displayCCY
			fmt.Fprintf(tw, "pricing: no exchange rate to convert %s -> %s for -currency, showing %s instead (add exchange_rate: {%s: <rate>} to config.yaml's pricing: block or report.yaml)\n", summary.Currency, displayCCY, summary.Currency, displayCCY)
		}
	}
	return resolver, summary
}

// resolvePricingForAnalyze builds the pricing resolver the story half needs
// for zoom views (-journey / -compare), which don't run runReport. Same
// degrade-gracefully contract: an unreadable config falls back to the embedded
// standard table. Warnings go to stderr rather than being discarded, so
// unresolvable supplement paths or missing exchange rates are visible.
func resolvePricingForAnalyze(configPath, displayCCY string, exchangeRate map[string]float64) (*pricing.Resolver, string) {
	tw := timestampWriter{w: os.Stderr}
	cfg, cfgErr := config.Load(configPath)
	if cfgErr != nil {
		fmt.Fprintf(tw, "config: %s not usable (%v) — $ estimates use the standard price table only (no supplement, no account overrides)\n", configPath, cfgErr)
	}
	resolver, info := buildPricing(cfg, cfgErr, configPath, tw, displayCCY, exchangeRate)
	ccy := "USD"
	if info != nil && info.Currency != "" {
		ccy = info.Currency
	}
	return resolver, ccy
}

// allPathsOutsideDir reports whether EVERY entry in paths resolves
// outside dir — used to flag "the live quota counter's log_dir doesn't
// contain a single one of the audit logs this report is analyzing", the
// cheap same-machine signal that the two might be different instances.
// Deliberately ALL-outside, not ANY-outside: a mixed run (some paths under
// log_dir, some not — e.g. one archived file alongside the live directory)
// is the normal case for analyzing "this instance's logs plus one old
// archive", not a mismatch worth flagging. An empty/unresolvable dir (a
// config.yaml that couldn't set LogDir at all) can't be compared against,
// so returns false rather than a false-positive warning.
func allPathsOutsideDir(paths []string, dir string) bool {
	if dir == "" || len(paths) == 0 {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for _, p := range paths {
		absP, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absDir, absP)
		if err != nil {
			continue
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false // at least one path IS under dir
		}
	}
	return true
}

// cmdReport aggregates audit JSONL into internal/report's output:
// vmr-report.json/.md, vmr-requests.json/.md (+ per-tag siblings),
// vmr-requests-failed.jsonl/.md (error-analysis index: outcome ==
// error|canceled plus ok-but-truncated, additive — doesn't remove those
// requests from anything above), and one details/*.md+.json per request.
// Inputs may freely mix live plain .jsonl files and .jsonl.zst files that
// the audit logger's housekeeping sweep has since compressed
// (internal/report decompresses transparently) — e.g.
// `vmr report 'vmr-audit-*.jsonl*'`. With no input files given at all,
// defaults to <-c config.yaml's log_dir>/vmr-audit-* (see resolveInputPaths
// in auditpaths.go) — the common case of "just report on this instance's
// own logs" needs no arguments beyond an optional -c.
// setupDetailWriter creates {outDir}/details and starts the detail-page
// worker pool when detailsOn — Build's onRecord hook (nil when !detailsOn)
// renders+writes each record's detail page during the aggregation pass
// itself, on its own worker pool, so there's no separate third read of the
// audit source for detail export. Build's own success/failure never
// depends on this: a detail-write failure surfaces only when the returned
// *report.DetailWriter's Close is checked, well after vmr-report.json/md
// are already safely on disk — same robustness the old separate-
// WriteDetails-step had, just without the extra pass.
func setupDetailWriter(outDir string, detailsOn bool, lang i18n.Lang, tw io.Writer) (dw *report.DetailWriter, detailDir string, onRecord func(*audit.Record, *report.ReqInfo), err error) {
	detailDir = filepath.Join(outDir, "details")
	if !detailsOn {
		return nil, detailDir, nil, nil
	}
	dw, err = report.NewDetailWriter(detailDir, lang, resolveTaskProfile())
	if err != nil {
		return nil, detailDir, nil, err
	}
	fmt.Fprintf(tw, "detail export: writing into %s (runs concurrently with the pass below)\n", detailDir)
	return dw, detailDir, dw.Submit, nil
}

// detailDirHasFiles reports whether dir already contains at least one
// entry. A missing directory is "no", not an error.
func detailDirHasFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// detailsPresentFor is the criterion for
// Meta.DetailsEnabled: detailsOn alone covers this run's OWN -details
// writer (guaranteed to finish by the time this command returns, even
// though it hasn't flushed to disk yet at the point runReport reads this);
// the OR checks for detail pages a DIFFERENT half of the same `vmr
// analyze` invocation already wrote and flushed before this command ever
// started — story's batch materialization under -render-all (P13.1). A
// flag-only check goes stale the moment two halves of one invocation can
// populate details/ independently.
func detailsPresentFor(detailsOn bool, detailDir string) bool {
	return detailsOn || detailDirHasFiles(detailDir)
}

// reportRunOpts bundles vmr report's already-resolved parameters — every
// value cmdReport itself derives from its own flags/report.yaml before the
// pipeline in runReport starts doing anything. Factored out (P9.1) so
// cmdAnalyze can drive the exact same pipeline from its own unified flag
// set's resolution, instead of the pre-P9 approach of re-serializing
// resolved values into a []string and having cmdReport re-parse them.
type reportRunOpts struct {
	configPath        string
	outDir            string
	detailsOn         bool
	lang              i18n.Lang
	displayCCY        string
	exchangeRate      map[string]float64
	excludeClientTags map[string]bool
	reportConfigPath  string
}

// runReport executes vmr report's full pipeline — session analysis,
// aggregation, pricing/quota resolution, and every derived file it writes
// (vmr-report.{json,md}, details/*, vmr-requests*.{json,md,jsonl}) — against
// already-resolved opts. This is the same pipeline cmdReport's own body ran
// inline before P9.1; the split has no behavior change of its own, only a
// different caller (cmdAnalyze, see cmd_analyze.go) can now reach it without
// going through vmr report's own flag.FlagSet.
func runReport(paths []string, tw timestampWriter, opts reportRunOpts) error {
	// Single config.Load, shared by buildPricing and
	// buildProviderQuotas below — see either function's own doc comment for
	// why splitting this into two independent loads would be a consistency
	// bug (a config edit landing between them), not just a wasted read. A
	// load failure here is NOT fatal to `vmr report`: both callees degrade
	// independently (pricing falls back to the embedded standard table;
	// the quota section simply doesn't render) — see cfgErr's threading
	// below, never returned as this function's own error.
	cfg, cfgErr := config.Load(opts.configPath)
	if cfgErr != nil {
		// One unified warning for both degrade paths — buildPricing
		// and buildProviderQuotas used to each print their own near-
		// duplicate of this, so a bare-logs `vmr report` run reliably saw
		// two warnings naming the same unreadable file.
		fmt.Fprintf(tw, "config: %s not usable (%v) — $ estimates use the standard price table only (no account overrides), §2.5 renders without quota references\n", opts.configPath, cfgErr)
	}
	pricingSrc, pricingInfo := buildPricing(cfg, cfgErr, opts.configPath, tw, opts.displayCCY, opts.exchangeRate)

	// 0o700/0o600: report outputs embed full conversation bodies from the
	// 0600 audit files - the derived copies must not loosen that. Created up
	// front (the detail writer below needs its output directory to exist
	// before Build's aggregation pass starts feeding it records).
	if err := os.MkdirAll(opts.outDir, 0o700); err != nil {
		return err
	}

	dw, detailDir, onRecord, err := setupDetailWriter(opts.outDir, opts.detailsOn, opts.lang, tw)
	if err != nil {
		return err
	}
	// The gap between this line's timestamp and the first "[1/N]" line below
	// is session analysis (AnalyzeSessions) — a full, currently silent pass
	// over every input file that Build() always runs before its own
	// per-file aggregation loop starts printing. priorCache (from
	// {outDir}/.parse-cache, shared with `vmr story` — see
	// docs/VirtualModelRouter_Design_v4_Analytics.md's vmr-requests.json
	// section) lets that pass skip re-parsing/re-hashing any input file
	// whose content hasn't changed.
	reqPath := filepath.Join(opts.outDir, "vmr-requests.json")
	cacheDir := filepath.Join(opts.outDir, ".parse-cache")
	priorCache := ctxgraph.LoadCacheDir(cacheDir)
	now := time.Now()
	quotas, quotaJSONPath := buildProviderQuotas(cfg, cfgErr, opts.configPath, tw, now)
	fmt.Fprintf(tw, "session analysis + aggregation: scanning %d file(s)...\n", len(paths))
	rep, sess, cache, err := report.BuildCached(paths, now, tw, pricingInfo, pricingSrc, onRecord, resolveTaskProfile(), priorCache, quotas, opts.excludeClientTags)
	if err != nil {
		return err
	}
	// Name the live-quota counter's own source path in the report, and
	// flag when every input audit log lies outside this instance's log_dir
	// — the one-machine variant of "the live column may be from a different
	// instance than the logs being analyzed" that can happen today (copying
	// a colleague's audit logs onto a machine with its own healthy
	// vmr-quota.json). Only meaningful when the sub-table actually has
	// something to render.
	if quotaJSONPath != "" && len(rep.ProviderQuotas) > 0 {
		rep.Meta.QuotaJSONPath = quotaJSONPath
		rep.Meta.QuotaInputOutsideLogDir = allPathsOutsideDir(paths, cfg.LogDir)
	}
	rep.Meta.ReportConfigPath = opts.reportConfigPath
	rep.Meta.DetailsEnabled = detailsPresentFor(opts.detailsOn, detailDir) // see its own doc comment
	report.LocalizeEfficiency(rep, opts.lang)
	jsonPath := filepath.Join(opts.outDir, "vmr-report.json")
	mdPath := filepath.Join(opts.outDir, "vmr-report.md")
	if err := report.WriteJSON(rep, jsonPath); err != nil {
		return err
	}
	storiesLink, lineageToJourney := loadStoriesLink(opts.outDir)
	if err := os.WriteFile(mdPath, []byte(report.Markdown(rep, opts.lang, storiesLink, lineageToJourney)), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(tw, "%d records (%d parse errors) from %d file(s)\n%s\n%s\n",
		rep.Meta.Records, rep.Meta.ParseErrors, len(paths), jsonPath, mdPath)
	if err := writeToolWasteCard(rep, opts.outDir, opts.lang, tw); err != nil {
		return err
	}

	if dw != nil {
		n, err := dw.Close()
		if err != nil {
			return fmt.Errorf("details: %w", err)
		}
		fmt.Fprintf(tw, "%d detail file(s) (.md) in %s\n", n, detailDir)
	}

	// Requests index (+ per-tag siblings) + json (data only — the parse
	// cache is persisted separately, into cacheDir, right below).
	rows := rep.RequestRows()
	nReq, err := report.WriteRequestsJSON(rows, reqPath)
	if err != nil {
		return fmt.Errorf("requests export: %w", err)
	}
	fmt.Fprintf(tw, "%s (%d rows)\n", reqPath, nReq)
	if err := ctxgraph.SaveCacheDir(cacheDir, cache); err != nil {
		return fmt.Errorf("parse cache: %w", err)
	}
	if err := report.WriteRequestsIndex(rep, sess, opts.outDir, opts.lang, lineageToJourney, detailDir); err != nil {
		return fmt.Errorf("requests index: %w", err)
	}
	fmt.Fprintf(tw, "%s\n", filepath.Join(opts.outDir, "vmr-requests.md"))

	// Failed-requests index: a dedicated error-analysis view (outcome ==
	// error|canceled, plus ok-but-truncated), each row linking to its
	// details/*.md. Purely additive — every other report/requests output
	// above is unaffected and still lists these same failed requests
	// inline as before.
	failedRows := report.FailedRequestRows(rows)
	failedJSONLPath := filepath.Join(opts.outDir, "vmr-requests-failed.jsonl")
	nFailed, err := report.WriteRequestsJSONL(failedRows, failedJSONLPath)
	if err != nil {
		return fmt.Errorf("failed-requests export: %w", err)
	}
	fmt.Fprintf(tw, "%s (%d rows)\n", failedJSONLPath, nFailed)
	if err := report.WriteFailedIndex(rows, opts.outDir, opts.lang, detailDir); err != nil {
		return fmt.Errorf("failed-requests index: %w", err)
	}
	fmt.Fprintf(tw, "%s\n", filepath.Join(opts.outDir, "vmr-requests-failed.md"))
	return nil
}

// writeToolWasteCard writes {out}/tool-waste.html — the standalone
// shareable card — whenever the report has tool data. Carries only tool
// names/counts/byte sizes (no conversation content), 0600 like every other
// derived file. Skipped silently when nothing declared tools.
func writeToolWasteCard(rep *report.Report2, outDir string, lang i18n.Lang, tw io.Writer) error {
	if len(rep.Tools) == 0 {
		return nil
	}
	twPath := filepath.Join(outDir, "tool-waste.html")
	if err := os.WriteFile(twPath, []byte(report.RenderToolWasteHTML(rep, lang)), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(tw, "%s\n", twPath)
	return nil
}

// cmdReport is `vmr report`'s own flag set (P15.2: unchanged from before the
// CLI convergence — same flags, same defaults, same resolution helpers) and
// no longer runs its own resolution/dispatch. It parses its flags in-place,
// then hands the result to dispatchAnalyzeFrom with macroOnly: true
// (cmd_analyze.go's -macro-only, P15.1) — the same call cmdAnalyze itself
// makes for `-macro-only`, so "vmr report produces what vmr analyze
// -macro-only produces" is structural, not a promise kept by hand across
// two independent implementations (IS-25).
func cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	configPath := fs.String("c", "config.yaml", "config file to resolve log_dir from (when no input files are given) and to resolve pricing from (providers[].pricing / global pricing: block); without a readable config, $ estimates fall back to the built-in standard price table")
	outDirFlag := fs.String("o", "", "output directory (default: ./reports, or report.yaml's output)")
	detailsFlag := fs.Bool("details", false, "also render one Markdown file per request into {out}/details/ (default: report.yaml's details, or false — the requests index links to each record's detail filename regardless, computed without needing the file to exist; pass -details to materialize them all up front)")
	langFlag := fs.String("lang", "", "output language: en|zh (default: report.yaml's language, or en) — overrides report.yaml")
	currencyFlag := fs.String("currency", "", "display currency for $ cost estimates, e.g. CNY|JPY (default: report.yaml's currency, or whatever currency pricing resolved in — usually -c's config.yaml pricing.currency, or USD); needs a matching rate in config.yaml's pricing.exchange_rate or report.yaml's exchange_rate")
	reportConfigPath := fs.String("report-config", "", "vmr analyze's sidecar config yaml (shared with this alias); absent => auto-load ./report.yaml if present")
	includeSelfTraffic := fs.Bool("include-self-traffic", false, "don't exclude vmr analyze's own -llm-addr self-analysis traffic from cost/usage totals (default: excluded — see report.yaml's llm_key/self_traffic_client_tags)")
	// cmd_analyze.go/cmd_story.go both resolve -llm-key for
	// self-traffic exclusion (it identifies PAST self-analysis traffic,
	// independent of whether this run makes a new LLM call — see
	// cmd_analyze.go's own comment on this) — cmdReport never had this
	// flag, so `vmr report`'s self-traffic exclusion could only ever read
	// report.yaml's llm_key, never override it per-call like its siblings.
	llmKeyFlag := fs.String("llm-key", "", "identifies past self-analysis traffic to exclude from totals — not used to make a new LLM call (vmr report never does). Default: report.yaml's llm_key")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "vmr report: alias for `vmr analyze -macro-only` — kept for muscle memory, produces byte-identical output. See `vmr analyze -h`.")
	return dispatchAnalyzeFrom(analyzeAliasRun{
		fsAlias:            fs,
		configPath:         *configPath,
		reportConfigPath:   *reportConfigPath,
		outDirFlag:         *outDirFlag,
		langFlag:           *langFlag,
		includeSelfTraffic: *includeSelfTraffic,
		llmKey:             *llmKeyFlag,
		detailsOn:          *detailsFlag,
		detailsSet:         flagPassed(fs, "details"),
		displayCCY:         *currencyFlag,
		macroOnly:          true,
		aliasName:          "report",
	})
}
