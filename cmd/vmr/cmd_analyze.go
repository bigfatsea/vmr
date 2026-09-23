// Ver 2026-09-23 04:16, by Claude Opus 5.5

// vmr analyze: the single analysis entry point.
// One flag set; three mutually exclusive zoom selectors (-journey/-compare/
// -benchmark) route into exactly the single/pairwise/benchmark view — no
// selector means the default suite (macro report + request index + journey
// index), the one mode that runs both halves.
//
// This file does no rendering or aggregation of its own: pure CLI-layer
// routing and flag/config resolution. All orchestration is delegated to
// internal/analyze.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vmr/internal/analyze"
	"vmr/internal/config"
	"vmr/internal/journey"
)

// resolveLLMOptions validates the -llm-* flag combination: -llm-addr is the
// sole switch that turns the interpretation layer on; -llm-model is required
// alongside it unless -llm-dry-run; -llm-model/-llm-key/-llm-dry-run without
// -llm-addr are rejected outright rather than silently ignored.
func resolveLLMOptions(addr, model, key string, dryRun bool) (analyze.LLMOptions, error) {
	if addr == "" {
		switch {
		case dryRun:
			return analyze.LLMOptions{}, fmt.Errorf("-llm-dry-run requires -llm-addr")
		case model != "" || key != "":
			return analyze.LLMOptions{}, fmt.Errorf("-llm-model/-llm-key require -llm-addr")
		}
		return analyze.LLMOptions{}, nil
	}
	if model == "" && !dryRun {
		return analyze.LLMOptions{}, fmt.Errorf("-llm-model is required when -llm-addr is given (unless -llm-dry-run)")
	}
	return analyze.LLMOptions{
		LLMOptions: journey.LLMOptions{Addr: addr, Model: model, APIKey: key},
		DryRun:     dryRun,
	}, nil
}

// validateAnalyzeModeFlags checks the mutual-exclusion rules across
// cmdAnalyze's mode-selecting flags.
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
		return cmdAnalyzeRenderOnly(fs, fl)
	}

	run, outDir, err := buildAnalyzeRun(fs, fl)
	if err != nil {
		return err
	}
	if err := run.Execute(); err != nil {
		return err
	}
	return maybeOpen(*fl.openFlag, outDir)
}

func cmdAnalyzeRenderOnly(fs *flag.FlagSet, fl *analyzeCLIFlags) error {
	if *fl.journeyArg != "" || *fl.compareArg != "" || *fl.benchmarkFlag || *fl.renderAllFlag || *fl.macroOnlyFlag || *fl.listOnlyFlag || *fl.journeyOnlyFlag || flagPassed(fs, "details") || flagPassed(fs, "include-partial") || *fl.llmAddrFlag != "" || *fl.llmModelFlag != "" || *fl.llmDryRun {
		return fmt.Errorf("-render-only re-renders existing products from disk — mutually exclusive with log aggregation flags")
	}
	rc := resolveReportConfig(*fl.reportConfigPath, os.Stdout)
	outDir := resolveString(*fl.outDirFlag, rc.Output, "reports")
	if err := analyze.RunRenderOnly(analyze.RenderOnlyOptions{
		OutDir:        outDir,
		RequestedLang: *fl.langFlag,
		LangPassed:    flagPassed(fs, "lang"),
		NoCache:       *fl.noCache,
	}); err != nil {
		return err
	}
	return maybeOpen(*fl.openFlag, outDir)
}

func buildAnalyzeRun(fs *flag.FlagSet, fl *analyzeCLIFlags) (*analyze.Run, string, error) {
	hasSelector, err := validateAnalyzeModeFlags(*fl.journeyArg, *fl.compareArg, *fl.benchmarkFlag, *fl.renderAllFlag, *fl.macroOnlyFlag, *fl.listOnlyFlag, *fl.journeyOnlyFlag, flagPassed(fs, "details"))
	if err != nil {
		return nil, "", err
	}

	rc := resolveReportConfig(*fl.reportConfigPath, os.Stdout)
	lang, err := resolveLanguage(*fl.langFlag, rc, os.Stdout)
	if err != nil {
		return nil, "", err
	}
	outDir := resolveString(*fl.outDirFlag, rc.Output, "reports")
	llmAddr := resolveStringExplicit(flagPassed(fs, "llm-addr"), *fl.llmAddrFlag, rc.LLMAddr, "")
	llmModel := resolveString(*fl.llmModelFlag, rc.LLMModel, "")
	llmKey := resolveString(*fl.llmKeyFlag, rc.LLMKey, "")
	llmCacheDir := resolveString(*fl.llmCacheDirFlag, rc.LLMCacheDir, "")
	llmAddrExplicit := flagPassed(fs, "llm-addr")
	if llmAddrExplicit && llmAddr != "" && (*fl.benchmarkFlag || !hasSelector) {
		return nil, "", fmt.Errorf("-llm-addr is not supported with -benchmark or the default suite (would fire one LLM call per journey) — use -journey to interpret one at a time, or -compare for a pairwise interpretation")
	}

	paths, err := resolveInputPaths(fs, *fl.configPath)
	if err != nil {
		return nil, "", err
	}

	cfg, cfgErr := config.Load(*fl.configPath)
	displayCCY := resolveString(*fl.currencyFlag, rc.Currency, "")
	priceRes, pricingInfo, ccy := resolvePricingForAnalyze(cfg, cfgErr, *fl.configPath, displayCCY, rc.ExchangeRate)
	pricingFP := resolvePricingFingerprint(cfg, rc.ExchangeRate)

	now := time.Now()
	quotas, _ := buildProviderQuotas(cfg, cfgErr, *fl.configPath, os.Stderr, now)

	includeSelfTraffic := *fl.includeSelfTraffic
	selfTrafficTags := rc.SelfTrafficClientTags
	var excludeClientTags map[string]bool
	if !includeSelfTraffic {
		excludeClientTags = analyze.SelfTrafficExcludeTags(llmKey, selfTrafficTags)
	}

	quotaInputOutsideLogDir := false
	quotaJSONPath := ""
	if cfg != nil && cfg.LogDir != "" && configHasQuotaLimits(cfg) {
		quotaJSONPath = filepath.Join(cfg.LogDir, "vmr-quota.json")
		quotaInputOutsideLogDir = allPathsOutsideDir(paths, cfg.LogDir)
	}

	llmOpts, _ := resolveLLMOptions(llmAddr, llmModel, llmKey, *fl.llmDryRun)
	llmOpts.CacheDir = llmCacheDir

	run := &analyze.Run{
		Paths:                   paths,
		OutDir:                  outDir,
		Lang:                    lang,
		IncludePartial:          resolveBool(flagPassed(fs, "include-partial"), *fl.includePartialFlag, rc.IncludePartial),
		IncludeSelfTraffic:      includeSelfTraffic,
		SelfTrafficTags:         selfTrafficTags,
		LLMSelfTag:              analyze.LLMSelfTag(llmKey),
		ExcludeClientTags:       excludeClientTags,
		PriceRes:                priceRes,
		PricingInfo:             pricingInfo,
		PricingFingerprint:      pricingFP,
		Currency:                ccy,
		DisplayCCY:              displayCCY,
		ExchangeRate:            rc.ExchangeRate,
		Quotas:                  quotas,
		QuotaJSONPath:           quotaJSONPath,
		QuotaInputOutsideLogDir: quotaInputOutsideLogDir,
		ReportConfigSource:      rc.SourcePath,
		Profile:                 resolveTaskProfile(),
		DetailsOn:               resolveBool(flagPassed(fs, "details"), *fl.detailsFlag, rc.Details),
		ShowUngrouped:           *fl.showUngrouped,
		NoCache:                 *fl.noCache,

		BenchmarkFlag: *fl.benchmarkFlag,
		CompareArg:    *fl.compareArg,
		JourneyArg:    *fl.journeyArg,
		RenderAllFlag: *fl.renderAllFlag,
		MacroOnly:     *fl.macroOnlyFlag,
		ListOnly:      *fl.listOnlyFlag,
		JourneyOnly:   *fl.journeyOnlyFlag,

		LLMKey:          llmKey,
		LLMOpts:         llmOpts,
		LLMAddrExplicit: llmAddrExplicit,
		ValidateLLMOpts: func() error {
			_, err := resolveLLMOptions(llmAddr, llmModel, llmKey, *fl.llmDryRun)
			return err
		},
	}
	return run, outDir, nil
}

func maybeOpen(open bool, outDir string) error {
	if !open {
		return nil
	}
	return serveAndOpen(outDir)
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
	openFlag           *bool
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
		detailsFlag:        fs.Bool("details", false, "also render one Markdown file per request into {out}/requests/details/ (default: false — the requests index links to each record's detail filename regardless, computed without needing the file to exist)"),
		currencyFlag:       fs.String("currency", "", "display currency for $ cost estimates, e.g. CNY|JPY"),
		includePartialFlag: fs.Bool("include-partial", false, "also render journeys whose head looks truncated by the loaded file range (default: report.yaml's include_partial, or false)"),
		showUngrouped:      fs.Bool("show-ungrouped", false, "print the source location of the first few ungrouped records"),
		llmAddrFlag:        fs.String("llm-addr", "", "host:port of an already-running VMR instance — enables the optional LLM interpretation section on -journey's or -compare's report (not supported with -benchmark or the default suite). Never auto-started. Default: report.yaml's llm_addr"),
		llmModelFlag:       fs.String("llm-model", "", "that VMR instance's virtual model name (e.g. \"agent\"), sent verbatim — required with -llm-addr unless -llm-dry-run. Default: report.yaml's llm_model"),
		llmKeyFlag:         fs.String("llm-key", "", "bearer token for that VMR instance, only needed if it has api_keys configured. Default: report.yaml's llm_key"),
		llmCacheDirFlag:    fs.String("llm-cache-dir", "", "directory for the disk cache of LLM interpretation results; absent both here and in report.yaml's llm_cache_dir => no caching, ever"),
		llmDryRun:          fs.Bool("llm-dry-run", false, "with -llm-addr: print every LLM call this run would make — per evidence-pack size estimate and the maximum call count (detector packs included) — and exit without calling anything"),
		openFlag:           fs.Bool("open", false, "after writing the report, serve outDir on 127.0.0.1 (random port, foreground, Ctrl-C to stop) and open the dashboard in the default browser — a one-shot local viewer, distinct from the config-level analytics.serve"),
	}
}
