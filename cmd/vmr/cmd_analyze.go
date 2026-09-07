// Ver 2026-08-21 01:00, by Sonnet 5

// vmr analyze: the single analysis entry point (P9.1, architecture doc
// §7.9's target model, superseding the P6.5 "third verb" interim state).
// One flag set; three mutually exclusive zoom selectors (-journey/-compare/
// -benchmark) route into exactly the single/pairwise/benchmark view — no
// selector means the default suite (macro report + request index + journey
// index), the one mode that runs both halves.
//
// This file does no rendering or aggregation of its own: every branch below
// calls the same functions cmd_report.go/cmd_story.go already expose
// (runReport, setupStoryRun + renderJourney/renderJourneys/renderAllJourneys/
// compareJourneys/corpusStats) — "pure CLI-layer routing", per the
// ActionPlan's own constraint. `internal/report`/`internal/journey` are not
// touched by this file at all.
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vmr/internal/chatmsg"
	"vmr/internal/config"
	"vmr/internal/ctxgraph"
	"vmr/internal/dashboard"
	"vmr/internal/i18n"
	story "vmr/internal/journey"
	"vmr/internal/report"
)

// renderableCandidates filters su.cands down to the non-noise rows
// (story.IsNoiseCategory, already computed by setupStoryRun's
// BuildJourneyIndexRow call — no new classification logic here). An
// earlier version kept CategoryTask only, which left cron/subagent
// candidates visible in the index but permanently unrenderable by
// default, contradicting the index's own display split — story.IsNoiseCategory
// is now the one place both answers come from. cands and freshRows are
// parallel arrays (same index = same candidate), the invariant
// setupStoryRun's own doc comment states and relies on.
func renderableCandidates(su *storySetup) []*ctxgraph.Lineage {
	var out []*ctxgraph.Lineage
	for i, l := range su.cands {
		if !story.IsNoiseCategory(su.freshRows[i].Category) {
			out = append(out, l)
		}
	}
	return out
}

// analyzeRun bundles cmdAnalyze's already-resolved flags/config for
// dispatchAnalyze — split out from cmdAnalyze itself once flag definition
// plus resolution plus dispatch together pushed the function over
// archtest's per-function line budget (a "composition, not an algorithm"
// split).
type analyzeRun struct {
	paths              []string
	configPath         string
	outDir             string
	lang               i18n.Lang
	includePartial     bool
	includeSelfTraffic bool
	llmKey             string
	llmAddr            string
	llmModel           string
	llmAddrExplicit    bool
	resolveLLMOpts     func() (llmCLIOptions, error)
	benchmarkFlag      bool
	compareArg         string
	journeyArg         string
	renderAllFlag      bool
	macroOnly          bool
	listOnly           bool
	journeyOnly        bool
	detailsOn          bool
	displayCCY         string
	exchangeRate       map[string]float64
	selfTrafficTags    []string
	// reportConfigSource is the report.yaml these settings came from, empty
	// when none was loaded — carried through so the macro report can name
	// its own configuration source (report.Meta.ReportConfigPath).
	reportConfigSource string
	// cfg/cfgErr: the single config.Load for this analyze run, set at the
	// top of dispatchAnalyze and shared by the story half's pricing
	// resolver and the report half — a second independent Load could
	// observe a different file if an edit lands mid-run (P-7-7). cfgErr is
	// non-fatal: every consumer degrades on its own (pricing falls back to
	// the standard table, the quota section skips).
	cfg           *config.Config
	cfgErr        error
	showUngrouped bool
	noCache       bool
}

// validateAnalyzeModeFlags checks the mutual-exclusion rules across
// cmdAnalyze's mode-selecting flags — split out once folding P15.1's new
// modes into cmdAnalyze inline pushed it over archtest's per-function line
// budget (a "composition, not an algorithm" split, same reasoning as
// analyzeRun's own). Returns whether exactly one of -journey/-compare/
// validateAnalyzeModeFlags checks the mutual-exclusion rules across
// cmdAnalyze's mode-selecting flags — split out once folding P15.1's new
// modes into cmdAnalyze inline pushed it over archtest's per-function line
// budget (a "composition, not an algorithm" split, same reasoning as
// analyzeRun's own). Returns whether exactly one of -journey/-compare/
// -benchmark was given. -journey-only is deliberately NOT exclusive with
// -render-all (unlike -macro-only/-list-only) — it composes with it,
// see analyzeRun.journeyOnly's own doc comment.
func validateAnalyzeModeFlags(journeyArg, compareArg string, benchmarkFlag, renderAllFlag, macroOnlyFlag, listOnlyFlag, journeyOnlyFlag, detailsPassed bool) (bool, error) {
	selectorCount := 0
	if journeyArg != "" {
		selectorCount++
	}
	if compareArg != "" {
		selectorCount++
	}
	if benchmarkFlag {
		selectorCount++
	}
	if selectorCount > 1 {
		return false, fmt.Errorf("-journey/-compare/-benchmark are mutually exclusive — pick one zoom level per call")
	}
	hasSelector := selectorCount == 1
	if renderAllFlag && hasSelector {
		return false, fmt.Errorf("-render-all has no effect with -journey/-compare/-benchmark (it only controls the default suite's rendering scope) — drop one or the other")
	}
	exclusiveCount := 0
	for _, f := range []bool{macroOnlyFlag, listOnlyFlag, journeyOnlyFlag} {
		if f {
			exclusiveCount++
		}
	}
	if exclusiveCount > 1 {
		return false, fmt.Errorf("-macro-only/-list-only/-journey-only are mutually exclusive — pick one")
	}
	if (macroOnlyFlag || listOnlyFlag) && (hasSelector || renderAllFlag) {
		return false, fmt.Errorf("-macro-only/-list-only replace the default suite entirely — mutually exclusive with -journey/-compare/-benchmark/-render-all")
	}
	if journeyOnlyFlag && hasSelector {
		return false, fmt.Errorf("-journey-only replaces the default suite entirely — mutually exclusive with -journey/-compare/-benchmark (but composes with -render-all)")
	}
	if listOnlyFlag && detailsPassed {
		return false, fmt.Errorf("-details has no effect with -list-only (it never renders, let alone materializes, any journey) — drop one or the other")
	}
	return hasSelector, nil
}

func cmdAnalyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	fl := bindAnalyzeCLIFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *fl.renderOnlyFlag {
		if *fl.journeyArg != "" || *fl.compareArg != "" || *fl.benchmarkFlag || *fl.renderAllFlag || *fl.macroOnlyFlag || *fl.listOnlyFlag || *fl.journeyOnlyFlag || flagPassed(fs, "details") || flagPassed(fs, "include-partial") || *fl.llmAddrFlag != "" || *fl.llmModelFlag != "" || *fl.llmDryRun {
			return fmt.Errorf("-render-only re-renders existing products from disk — mutually exclusive with log aggregation flags")
		}
		rc := resolveReportConfig(*fl.reportConfigPath, os.Stdout)
		outDir := resolveString(*fl.outDirFlag, rc.Output, "reports")
		if !*fl.noCache && tryRenderOnlyL3Cache(outDir, *fl.langFlag, flagPassed(fs, "lang")) {
			return nil
		}
		if err := runRenderOnly(outDir, *fl.langFlag, flagPassed(fs, "lang")); err != nil {
			return err
		}
		recordRenderOnlyL3Cache(outDir)
		return nil
	}

	hasSelector, err := validateAnalyzeModeFlags(*fl.journeyArg, *fl.compareArg, *fl.benchmarkFlag, *fl.renderAllFlag, *fl.macroOnlyFlag, *fl.listOnlyFlag, *fl.journeyOnlyFlag, flagPassed(fs, "details"))
	if err != nil {
		return err
	}

	rc := resolveReportConfig(*fl.reportConfigPath, os.Stdout)
	lang, err := resolveLanguage(*fl.langFlag, rc, os.Stdout)
	if err != nil {
		return err
	}
	outDir := resolveString(*fl.outDirFlag, rc.Output, "reports")
	llmAddr := resolveStringExplicit(flagPassed(fs, "llm-addr"), *fl.llmAddrFlag, rc.LLMAddr, "")
	llmModel := resolveString(*fl.llmModelFlag, rc.LLMModel, "")
	// llmKey is resolved (and used for self-traffic exclusion) on every
	// path, including -benchmark and the default suite — it identifies PAST
	// self-analysis traffic to exclude, independent of whether THIS run
	// makes a new LLM call. resolveLLMOptions, by contrast, validates a
	// *usable* LLM configuration (requires -llm-addr whenever -llm-model/
	// -llm-key/-llm-dry-run is set) and is only relevant to -journey/
	// -compare, the only branches that consume its result — see
	// dispatchAnalyze's resolveLLMOpts closure.
	llmKey := resolveString(*fl.llmKeyFlag, rc.LLMKey, "")
	llmCacheDir := resolveString(*fl.llmCacheDirFlag, rc.LLMCacheDir, "")
	// -llm-addr fires one LLM call per journey, which makes no sense against a batch —
	// -benchmark or the default suite (this entry's equivalent of -render-all).
	// -compare/a single-match -journey are the only two shapes that support it.
	// Gate on the RESOLVED value, not just "was the flag typed": an explicit
	// `-llm-addr ""` is the sanctioned way to suppress a report.yaml llm_addr
	// for this run, so it must pass here; a report.yaml llm_addr with no flag
	// stays silently ignored on these batch shapes (never consulted downstream).
	llmAddrExplicit := flagPassed(fs, "llm-addr")
	if llmAddrExplicit && llmAddr != "" && (*fl.benchmarkFlag || !hasSelector) {
		return fmt.Errorf("-llm-addr is not supported with -benchmark or the default suite (would fire one LLM call per journey) — use -journey to interpret one at a time, or -compare for a pairwise interpretation")
	}

	paths, err := resolveInputPaths(fs, *fl.configPath)
	if err != nil {
		return err
	}

	return dispatchAnalyze(&analyzeRun{
		paths:              paths,
		configPath:         *fl.configPath,
		outDir:             outDir,
		lang:               lang,
		includePartial:     resolveBool(flagPassed(fs, "include-partial"), *fl.includePartialFlag, rc.IncludePartial),
		includeSelfTraffic: *fl.includeSelfTraffic,
		noCache:            *fl.noCache,
		llmKey:             llmKey,
		llmAddr:            llmAddr,
		llmModel:           llmModel,
		llmAddrExplicit:    llmAddrExplicit,
		resolveLLMOpts: func() (llmCLIOptions, error) {
			llmOpts, err := resolveLLMOptions(llmAddr, llmModel, llmKey, *fl.llmDryRun)
			if err != nil {
				return llmCLIOptions{}, err
			}
			llmOpts.CacheDir = llmCacheDir
			return llmOpts, nil
		},
		benchmarkFlag:      *fl.benchmarkFlag,
		compareArg:         *fl.compareArg,
		journeyArg:         *fl.journeyArg,
		renderAllFlag:      *fl.renderAllFlag,
		macroOnly:          *fl.macroOnlyFlag,
		listOnly:           *fl.listOnlyFlag,
		journeyOnly:        *fl.journeyOnlyFlag,
		detailsOn:          resolveBool(flagPassed(fs, "details"), *fl.detailsFlag, rc.Details),
		displayCCY:         resolveString(*fl.currencyFlag, rc.Currency, ""),
		exchangeRate:       rc.ExchangeRate,
		selfTrafficTags:    rc.SelfTrafficClientTags,
		reportConfigSource: rc.SourcePath,
		showUngrouped:      *fl.showUngrouped,
	})
}

// dispatchAnalyze routes to exactly one of: -macro-only, -benchmark, -compare,
// -journey, -list-only, or the default suite — see cmd_analyze.go's package
// comment for the "pure CLI routing" constraint this implements.
func dispatchAnalyze(r *analyzeRun) error {
	// S-2 shape counters are process-global atomics; zero them at the start of
	// each analyze run so the Context Rot section reports this run's count, not
	// a daemon's accumulated total.
	chatmsg.ResetUnrecognizedShapeCounts()

	// One config.Load for the whole run: both halves need the effective
	// config (pricing overrides, quota windows), and two independent loads
	// could disagree if an edit lands between them (P-7-7). resolveInputPaths
	// does its own earlier load purely for the log_dir path fallback — a
	// separate concern that never feeds the cost/quota basis.
	r.cfg, r.cfgErr = config.Load(r.configPath)

	mode := analyzeModeString(r)
	targetL2, ok := computeTargetL2(r, mode)
	if ok && tryL2Cache(r, targetL2, mode) {
		return nil
	}

	if r.macroOnly {
		return runMacroOnly(r)
	}

	su, err := setupStoryRun(r.paths, r.outDir, r.includeSelfTraffic, r.llmKey, r.selfTrafficTags, r.showUngrouped, r.lang)
	if err != nil {
		return err
	}

	// Every successful run converges on finishAnalyze below — the single
	// exit that refreshes the skeleton pages, rebuilds the compares index,
	// and commits the manifest. Failed runs touch none of that: they
	// produced no snapshot, and creating ./reports as a side effect of a
	// failed invocation pollutes cwd (TestCmdReport_NoMatches regression).

	switch {
	case r.listOnly:
		err = listJourneys(su.idx, su.g, r.outDir, r.includePartial, r.lang)
	case r.benchmarkFlag:
		err = runBenchmark(r, su)
	case r.compareArg != "":
		err = dispatchCompare(r, su)
	case r.journeyArg != "":
		err = dispatchJourney(r, su)
	default:
		var rep *report.Report2
		rep, err = dispatchDefaultSuite(r, su)
		if err == nil {
			return finishAnalyze(r, rep)
		}
	}
	if err != nil {
		return err
	}
	return finishAnalyze(r, nil)
}

// finishAnalyze is the one successful-run exit shared by every analyze
// mode (§5.4's "每次 analyze 调用都幂等刷新骨架页"): the six skeleton
// dashboard pages are rewritten into the output root first, so /reports/
// always serves pages from the running binary (a skeleton refresh failure
// only warns on stderr — pages may be stale, never the analysis itself);
// then the compares index is rebuilt from what this run wrote (D21);
// commitManifest runs last as the snapshot's admission token (§3.4). rep
// is nil for modes that didn't run the report half; the manifest then
// stamps only the slices that actually exist on disk.
func finishAnalyze(r *analyzeRun, rep *report.Report2) error {
	if err := dashboard.WriteSkeletons(r.outDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: dashboard skeleton refresh failed (pages may be stale until next analyze): %v\n", err)
	}
	if err := RebuildComparesIndex(filepath.Join(r.outDir, "compares")); err != nil {
		fmt.Fprintf(os.Stderr, "compares index rebuild failed (stale until next analyze): %v\n", err)
	}
	// rep != nil means the report half ran and already committed the manifest
	// itself — it must exist before that half's markdown renderers read it
	// (cmd_report.go's writeReportManifest); re-committing here would only
	// bump generated_at. Zoom/list/benchmark/journey-only runs commit here:
	// their journeys/index.json rewrite is the last stamped artifact.
	if rep == nil {
		if err := commitManifest(r, rep); err != nil {
			return err
		}
	}
	recordPostAnalyzeCache(r)
	return nil
}

// commitManifest is the snapshot's admission token (§3.4): built after
// every other file this run wrote — including journeys/index.json, which
// zoom modes also update — and written last, atomically. rep is nil for
// modes that didn't run the report half; the manifest then stamps only the
// slices that actually exist on disk.
func commitManifest(r *analyzeRun, rep *report.Report2) error {
	manifest, err := report.BuildManifest(r.outDir, rep, r.lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: manifest not written — this snapshot will be treated as invalid by -render-only and the L2 cache until the next analyze: %v\n", err)
		return nil
	}
	if err := report.WriteManifest(r.outDir, manifest); err != nil {
		fmt.Fprintf(os.Stderr, "warning: manifest not written — this snapshot will be treated as invalid by -render-only and the L2 cache until the next analyze: %v\n", err)
	}
	return nil
}

// runMacroOnly is -macro-only's whole run: the macro report half, then the
// same finishAnalyze exit every other mode uses (skeleton refresh, compares
// index, manifest last per §3.4).
func runMacroOnly(r *analyzeRun) error {
	rep, err := runReportHalf(r)
	if err != nil {
		return err
	}
	return finishAnalyze(r, rep)
}

// runBenchmark finishes the -benchmark zoom: corpus statistics.
func runBenchmark(r *analyzeRun, su *storySetup) error {
	return corpusStats(su.cands, su.byIdx, su.firstPath, su.prof, r.includePartial, r.outDir, r.lang, su.idx)
}

// dispatchCompare routes -compare's pairwise zoom.
func dispatchCompare(r *analyzeRun, su *storySetup) error {
	ids := strings.Split(r.compareArg, ",")
	if len(ids) != 2 || ids[0] == "" || ids[1] == "" {
		return fmt.Errorf("-compare wants exactly two comma-separated ids: -compare id1,id2")
	}
	llmOpts, err := r.resolveLLMOpts()
	if err != nil {
		return err
	}
	priceRes, ccy := resolvePricingForAnalyze(r.cfg, r.cfgErr, r.configPath, r.displayCCY, r.exchangeRate)
	return compareJourneys(su.cands, su.byIdx, ids[0], ids[1], su.firstPath, su.prof, r.includePartial, r.outDir, llmOpts, r.lang, su.idx, priceRes, ccy)
}

// dispatchJourney routes -journey's zoom: single-match render, or a batch
// over the multi-match selector.
func dispatchJourney(r *analyzeRun, su *storySetup) error {
	ids := make([]string, len(su.cands))
	for i, ch := range su.chains {
		ids[i] = story.ID(ch)
	}
	targets, err := resolveJourneySelector(su.cands, ids, r.journeyArg)
	if err != nil {
		return err
	}
	if len(targets) == 1 {
		llmOpts, err := r.resolveLLMOpts()
		if err != nil {
			return err
		}
		priceRes, ccy := resolvePricingForAnalyze(r.cfg, r.cfgErr, r.configPath, r.displayCCY, r.exchangeRate)
		return renderJourney(targets[0], su.byIdx, su.firstPath, su.prof, r.includePartial, r.outDir, llmOpts, r.lang, su.idx, priceRes, ccy)
	}
	if r.llmAddrExplicit {
		return fmt.Errorf("-llm-addr is not supported when -journey matches more than one journey (%d matched by %q) — use a single id/pattern that resolves to exactly one journey", len(targets), r.journeyArg)
	}
	// true: a -journey selector naming several targets is still a
	// user-named set, not the default suite's implicit batch (P13.1).
	priceRes, ccy := resolvePricingForAnalyze(r.cfg, r.cfgErr, r.configPath, r.displayCCY, r.exchangeRate)
	return renderJourneys(targets, su.byIdx, su.firstPath, su.prof, r.includePartial, r.outDir, r.lang, su.idx,
		"no matching journeys to render (all skipped as partial-head; pass -include-partial)", true, priceRes, ccy)
}

// dispatchDefaultSuite runs the no-selector default suite: story half first,
// then the macro report half (unless -journey-only), then the orphan sweep;
// it returns the report for the caller's manifest commit (§3.4).
func dispatchDefaultSuite(r *analyzeRun, su *storySetup) (*report.Report2, error) {
	scope := su.cands
	if !r.renderAllFlag {
		scope = renderableCandidates(su)
	}
	// materializeDetails = r.renderAllFlag: -render-all is an
	// explicit "materialize everything" ask; the default non-noise
	// suite is not — it renders each spine's "→ detail" pointer as an
	// inline `file:line` coordinate (a pure function of each Step's own
	// Manifest, see EnsureJourneyDetails' doc comment) rather than a
	// link, so no detail files are written. This keeps the default
	// suite from writing 160MB+/details on every `vmr analyze` run
	// regardless of whether anyone reads them.
	// priceRes/ccy: batch-rendered journey files carry the same cost
	// data the single -journey zoom produces (same resolver source,
	// same ComputeJourneyCost) — formerly a nil cost here left every
	// default-suite journey-*.md/.json without its cost line.
	priceRes, ccy := resolvePricingForAnalyze(r.cfg, r.cfgErr, r.configPath, r.displayCCY, r.exchangeRate)
	if err := renderAllJourneys(scope, su.byIdx, su.firstPath, su.prof, r.includePartial, r.outDir, r.lang, su.idx, r.renderAllFlag, priceRes, ccy); err != nil {
		return nil, fmt.Errorf("analyze (story half): %w", err)
	}

	activeIDs := make([]string, len(su.cands))
	for i, ch := range su.chains {
		activeIDs[i] = story.ID(ch)
	}
	_, _ = story.CleanOrphanJourneys(filepath.Join(r.outDir, "journeys", "details"), activeIDs)

	var rep *report.Report2
	if !r.journeyOnly {
		var err error
		rep, err = runReportHalf(r)
		if err != nil {
			return nil, err
		}
		// Now that vmr-report.md exists, re-render journeys/details/j-<id>.md from disk
		// so reportMDExists is consistently true (matching -render-only) (D11).
		if err := renderAllFromDisk(r.outDir, r.lang); err != nil {
			return nil, fmt.Errorf("render from disk: %w", err)
		}
	}
	return rep, nil
}

// runReportHalf runs the macro report half against r's already-resolved
// options — the one call site both the default suite's second step and
// -macro-only route through, so "produces the same report" is structural,
// not a maintained-in-parallel promise (P15.1/P15.2).
func runReportHalf(r *analyzeRun) (*report.Report2, error) {
	var excludeClientTags map[string]bool
	if !r.includeSelfTraffic {
		excludeClientTags = selfTrafficExcludeTags(r.llmKey, r.selfTrafficTags)
	}
	rep, err := runReport(r.paths, timestampWriter{w: os.Stdout}, reportRunOpts{
		configPath:        r.configPath,
		cfg:               r.cfg,
		cfgErr:            r.cfgErr,
		outDir:            r.outDir,
		detailsOn:         r.detailsOn,
		lang:              r.lang,
		displayCCY:        r.displayCCY,
		exchangeRate:      r.exchangeRate,
		excludeClientTags: excludeClientTags,
		reportConfigPath:  r.reportConfigSource,
	})
	if err != nil {
		return nil, fmt.Errorf("analyze (report half): %w", err)
	}
	return rep, nil
}

type analyzeCLIFlags struct {
	configPath         *string
	outDirFlag         *string
	langFlag           *string
	reportConfigPath   *string
	includeSelfTraffic *bool
	noCache            *bool
	journeyArg         *string
	compareArg         *string
	benchmarkFlag      *bool
	renderAllFlag      *bool
	macroOnlyFlag      *bool
	renderOnlyFlag     *bool
	listOnlyFlag       *bool
	journeyOnlyFlag    *bool
	detailsFlag        *bool
	currencyFlag       *string
	includePartialFlag *bool
	showUngrouped      *bool
	llmAddrFlag        *string
	llmModelFlag       *string
	llmKeyFlag         *string
	llmCacheDirFlag    *string
	llmDryRun          *bool
}

func bindAnalyzeCLIFlags(fs *flag.FlagSet) *analyzeCLIFlags {
	return &analyzeCLIFlags{
		configPath:         fs.String("c", "config.yaml", "config file to resolve log_dir from (when no input files are given) and to resolve pricing from"),
		outDirFlag:         fs.String("o", "", "output directory (default: ./reports, or report.yaml's output)"),
		langFlag:           fs.String("lang", "", "output language: en|zh (default: report.yaml's language, or en) — overrides report.yaml"),
		reportConfigPath:   fs.String("report-config", "", "vmr analyze's sidecar config yaml; absent => auto-load ./report.yaml if present"),
		includeSelfTraffic: fs.Bool("include-self-traffic", false, "don't exclude vmr analyze's own -llm-addr self-analysis traffic from either half's totals (default: excluded — see report.yaml's llm_key/self_traffic_client_tags)"),
		noCache:            fs.Bool("no-cache", false, "bypass L2/L3 product caches and force full re-aggregation and re-rendering"),
		journeyArg:         fs.String("journey", "", "zoom into this journey: an id or id-prefix, a comma-separated list of ids/prefixes/globs, and/or a shell-style glob (*, ?, [...]) matched against the full id. A selector resolving to exactly one journey renders as before (and alone supports -llm-addr); more than one batches like -render-all. Mutually exclusive with -compare/-benchmark; only this half runs (no macro report)"),
		compareArg:         fs.String("compare", "", "zoom into a pairwise comparison: -compare id1,id2 (each an id, id-prefix, or shell glob; first candidate matching each side wins). Mutually exclusive with -journey/-benchmark; only this half runs (no macro report)"),
		benchmarkFlag:      fs.Bool("benchmark", false, "zoom into benchmark-level statistics (metric distributions, Finding hit rates, correlations) across every non-partial candidate journey. Mutually exclusive with -journey/-compare; only this half runs (no macro report)"),
		renderAllFlag:      fs.Bool("render-all", false, "default suite only: materialize every non-partial candidate journey, including the heartbeat/poll ones (default: those low-signal heartbeat candidates are the only ones excluded; render one on demand with -journey <id>)"),
		macroOnlyFlag:      fs.Bool("macro-only", false, "default suite only: run just the macro report half — no candidate scan, no journey rendering, no journeys/ output. Mutually exclusive with -journey/-compare/-benchmark/-render-all/-list-only/-journey-only"),
		renderOnlyFlag:     fs.Bool("render-only", false, "re-render all resident human-readable Markdown products from existing on-disk JSON without re-aggregating audit logs"),
		listOnlyFlag:       fs.Bool("list-only", false, "default suite only: list candidate journeys without rendering any of them — writes journeys/index.{md,json} listing every candidate, but no j-*.md. Mutually exclusive with -journey/-compare/-benchmark/-render-all/-macro-only/-journey-only/-details"),
		journeyOnlyFlag:    fs.Bool("journey-only", false, "default suite only: run just the journey half, skipping the macro report — no vmr-report.md or macro/* written. Composes with -render-all; alone, equivalent to default suite's non-noise scope without the macro report. Mutually exclusive with -journey/-compare/-benchmark/-macro-only/-list-only"),
		detailsFlag:        fs.Bool("details", false, "also render one Markdown file per request into {out}/details/ (default: false — the requests index links to each record's detail filename regardless, computed without needing the file to exist)"),
		currencyFlag:       fs.String("currency", "", "display currency for $ cost estimates, e.g. CNY|JPY"),
		includePartialFlag: fs.Bool("include-partial", false, "also render journeys whose head looks truncated by the loaded file range (default: report.yaml's include_partial, or false)"),
		showUngrouped:      fs.Bool("show-ungrouped", false, "print the source location of the first few ungrouped records"),
		llmAddrFlag:        fs.String("llm-addr", "", "host:port of an already-running VMR instance — enables the optional LLM interpretation section on -journey's or -compare's report (not supported with -benchmark or the default suite). Never auto-started. Default: report.yaml's llm_addr"),
		llmModelFlag:       fs.String("llm-model", "", "that VMR instance's virtual model name (e.g. \"agent\"), sent verbatim — required with -llm-addr unless -llm-dry-run. Default: report.yaml's llm_model"),
		llmKeyFlag:         fs.String("llm-key", "", "bearer token for that VMR instance, only needed if it has api_keys configured. Default: report.yaml's llm_key"),
		llmCacheDirFlag:    fs.String("llm-cache-dir", "", "directory for the disk cache of LLM interpretation results; absent both here and in report.yaml's llm_cache_dir => no caching, ever"),
		llmDryRun:          fs.Bool("llm-dry-run", false, "with -llm-addr: print every LLM call this run would make — per evidence-pack size estimate and the maximum call count (detector packs included) — and exit without calling anything"),
	}
}

func analyzeModeString(r *analyzeRun) string {
	switch {
	case r.macroOnly:
		return "macro-only"
	case r.listOnly:
		return "list-only"
	case r.benchmarkFlag:
		return "benchmark"
	case r.compareArg != "":
		return "compare:" + r.compareArg
	case r.journeyArg != "":
		return "journey:" + r.journeyArg
	case r.journeyOnly:
		return "journey-only"
	default:
		return "default"
	}
}

func computeTargetL2(r *analyzeRun, mode string) ([32]byte, bool) {
	inHashes, err := report.ComputeInputHashes(r.paths)
	if err != nil || len(inHashes) == 0 {
		return [32]byte{}, false
	}
	pricingFP := resolvePricingFingerprint(r.cfg, r.exchangeRate)
	// LLM identity rides the params fingerprint only on the modes that
	// consume it: on -journey/-compare an L2 hit must not silently swallow a
	// requested -llm-addr interpretation, while on the batch shapes the
	// resolved value is deliberately ignored (never consulted downstream).
	llmAddr, llmModel := "", ""
	if strings.HasPrefix(mode, "journey:") || strings.HasPrefix(mode, "compare:") {
		llmAddr, llmModel = r.llmAddr, r.llmModel
	}
	paramsFP := report.ComputeAnalysisParamsFingerprint(report.AnalysisParams{
		Lang:               r.lang.String(),
		TaskProfile:        resolveTaskProfile().Name(),
		IncludePartial:     r.includePartial,
		IncludeSelfTraffic: r.includeSelfTraffic,
		SelfTrafficTags:    r.selfTrafficTags,
		LLMSelfTag:         llmSelfTag(r.llmKey),
		LLMAddr:            llmAddr,
		LLMModel:           llmModel,
		DisplayCCY:         r.displayCCY,
		RenderAll:          r.renderAllFlag,
		Details:            r.detailsOn,
		Mode:               mode,
	})
	return report.ComputeL2Digest(inHashes, pricingFP, report.ManifestFormat, paramsFP), true
}

func tryL2Cache(r *analyzeRun, targetL2 [32]byte, mode string) bool {
	if r.noCache {
		return false
	}
	rec, hit := report.CheckL2Cache(r.outDir, targetL2)
	if !hit {
		return false
	}
	vmFP, err := report.ComputeVMFingerprintFromManifest(r.outDir)
	if err != nil {
		return false
	}
	targetL3 := report.ComputeL3Digest(vmFP[:], report.RendererVersion, r.lang.String())
	l3Hit := report.CheckL3Cache(r.outDir, targetL3, mode)
	if !l3Hit {
		if err := renderAllFromDisk(r.outDir, r.lang); err != nil {
			return false
		}
		rec.VMFingerprint = hex.EncodeToString(vmFP[:])
		rec.L3Digest = hex.EncodeToString(targetL3[:])
		rec.RendererVersion = report.RendererVersion
		_ = report.SaveCacheRecord(r.outDir, rec)
	}

	if err := dashboard.WriteSkeletons(r.outDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: dashboard skeleton refresh failed (pages may be stale until next analyze): %v\n", err)
	}
	if err := RebuildComparesIndex(filepath.Join(r.outDir, "compares")); err != nil {
		fmt.Fprintf(os.Stderr, "compares index rebuild failed (stale until next analyze): %v\n", err)
	}
	return true
}

func recordPostAnalyzeCache(r *analyzeRun) {
	mode := analyzeModeString(r)
	targetL2, ok := computeTargetL2(r, mode)
	if !ok {
		return
	}
	vmFP, err := report.ComputeVMFingerprintFromManifest(r.outDir)
	if err != nil {
		return
	}
	targetL3 := report.ComputeL3Digest(vmFP[:], report.RendererVersion, r.lang.String())
	rec := &report.CacheRecord{
		L2Digest:        hex.EncodeToString(targetL2[:]),
		VMFingerprint:   hex.EncodeToString(vmFP[:]),
		L3Digest:        hex.EncodeToString(targetL3[:]),
		FormatVersion:   report.ManifestFormat,
		RendererVersion: report.RendererVersion,
	}
	_ = report.SaveCacheRecord(r.outDir, rec)
}

func tryRenderOnlyL3Cache(outDir string, requestedLang string, langPassed bool) bool {
	m, err := report.ValidateManifest(outDir)
	if err != nil {
		return false
	}
	manifestLang, err := i18n.Parse(m.Lang)
	if err != nil {
		manifestLang = i18n.EN
	}
	if langPassed {
		reqLang, err := i18n.Parse(requestedLang)
		if err != nil || reqLang != manifestLang {
			return false
		}
	}
	vmFP, err := report.ComputeVMFingerprintFromManifest(outDir)
	if err != nil {
		return false
	}
	targetL3 := report.ComputeL3Digest(vmFP[:], report.RendererVersion, manifestLang.String())
	if !report.CheckL3Cache(outDir, targetL3, "default") {
		return false
	}
	_ = dashboard.WriteSkeletons(outDir)
	return true
}

func recordRenderOnlyL3Cache(outDir string) {
	m, err := report.ValidateManifest(outDir)
	if err != nil {
		return
	}
	manifestLang, err := i18n.Parse(m.Lang)
	if err != nil {
		manifestLang = i18n.EN
	}
	vmFP, err := report.ComputeVMFingerprintFromManifest(outDir)
	if err != nil {
		return
	}
	targetL3 := report.ComputeL3Digest(vmFP[:], report.RendererVersion, manifestLang.String())
	rec, _ := report.LoadCacheRecord(outDir)
	if rec == nil {
		rec = &report.CacheRecord{FormatVersion: report.ManifestFormat}
	}
	rec.VMFingerprint = hex.EncodeToString(vmFP[:])
	rec.L3Digest = hex.EncodeToString(targetL3[:])
	rec.RendererVersion = report.RendererVersion
	_ = report.SaveCacheRecord(outDir, rec)
}
