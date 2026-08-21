// Ver 2026-08-21 01:00, by Sonnet 5

// vmr analyze: the single analysis entry point (P9.1, architecture doc
// §7.9's target model, superseding the P6.5 "third verb" interim state —
// see the CHANGELOG for the full before/after). One flag set, the union of what `vmr
// report`/`vmr story` each had; three mutually exclusive zoom selectors
// (-journey/-compare/-corpus) route into exactly the single/pairwise/corpus
// view `vmr story` already renders for that selector — no selector means
// the default suite (macro report + request index + task index), the one
// mode that runs both halves.
//
// This file does no rendering or aggregation of its own: every branch below
// calls the same functions cmd_report.go/cmd_story.go already exposed
// (runReport, setupStoryRun + renderJourney/renderJourneys/renderAllJourneys/
// compareJourneys/corpusStats) — "pure CLI-layer routing", per the
// ActionPlan's own constraint. `internal/report`/`internal/story` are not
// touched by this file at all.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/story"
)

// taskOnlyCandidates filters su.cands down to the CategoryTask rows (P9.2,
// architecture doc §7.7's classifier, already computed by setupStoryRun's
// BuildJourneyIndexRow call — no new classification logic here). cands and
// freshRows are parallel arrays (same index = same candidate), the
// invariant setupStoryRun's own doc comment states and relies on.
func taskOnlyCandidates(su *storySetup) []*ctxgraph.Lineage {
	var out []*ctxgraph.Lineage
	for i, l := range su.cands {
		if su.freshRows[i].Category == story.CategoryTask {
			out = append(out, l)
		}
	}
	return out
}

// analyzeRun bundles cmdAnalyze's already-resolved flags/config for
// dispatchAnalyze — split out from cmdAnalyze itself once flag definition
// plus resolution plus dispatch together pushed the function over
// archtest's per-function line budget (a "composition, not an algorithm"
// split, same reasoning as cmdReport/cmdStory's own history).
type analyzeRun struct {
	paths              []string
	configPath         string
	outDir             string
	lang               i18n.Lang
	includePartial     bool
	includeSelfTraffic bool
	llmKey             string
	llmAddrExplicit    bool
	resolveLLMOpts     func() (llmCLIOptions, error)
	corpusFlag         bool
	compareArg         string
	journeyArg         string
	renderAllFlag      bool
	detailsOn          bool
	displayCCY         string
	exchangeRate       map[string]float64
	selfTrafficTags    []string
	showUngrouped      bool
}

func cmdAnalyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	// Shared flags — one definition each, used by both halves.
	configPath := fs.String("c", "config.yaml", "config file to resolve log_dir from (when no input files are given) and to resolve pricing from")
	outDirFlag := fs.String("o", "", "output directory (default: ./reports, or report.yaml's output)")
	langFlag := fs.String("lang", "", "output language: en|zh (default: report.yaml's language, or en) — overrides report.yaml")
	reportConfigPath := fs.String("report-config", "", "vmr analyze's sidecar config yaml; absent => auto-load ./report.yaml if present")
	includeSelfTraffic := fs.Bool("include-self-traffic", false, "don't exclude vmr analyze's own -llm-addr self-analysis traffic from either half's totals (default: excluded — see report.yaml's llm_key/self_traffic_client_tags)")
	// Zoom selectors — mutually exclusive; none of them means the default suite.
	journeyArg := fs.String("journey", "", "zoom into this journey: an id or id-prefix, a comma-separated list of ids/prefixes/globs, and/or a shell-style glob (*, ?, [...]) matched against the full id. A selector resolving to exactly one journey renders as before (and alone supports -llm-addr); more than one batches like -render-all. Mutually exclusive with -compare/-corpus; only this half runs (no macro report)")
	compareArg := fs.String("compare", "", "zoom into a pairwise comparison: -compare id1,id2 (each an id or id prefix). Mutually exclusive with -journey/-corpus; only this half runs (no macro report)")
	corpusFlag := fs.Bool("corpus", false, "zoom into corpus-level statistics (metric distributions, Finding hit rates, correlations) across every non-partial candidate journey. Mutually exclusive with -journey/-compare; only this half runs (no macro report)")
	// Default-suite-only scope knob (P9.2) — meaningless (rejected) with a selector above.
	renderAllFlag := fs.Bool("render-all", false, "default suite only: materialize every non-partial candidate journey, not just category=task ones (default: task-only — cron/heartbeat/subagent candidates still appear in the index, just not pre-rendered; render one on demand with -journey <id>)")
	// story-half flags.
	detailsFlag := fs.Bool("details", false, "also render one Markdown file per request into {out}/details/ (default: false — the requests index links to each record's detail filename regardless, computed without needing the file to exist)")
	currencyFlag := fs.String("currency", "", "display currency for $ cost estimates, e.g. CNY|JPY")
	includePartialFlag := fs.Bool("include-partial", false, "also list/render journeys whose head looks truncated by the loaded file range (default: report.yaml's include_partial, or false)")
	showUngrouped := fs.Bool("show-ungrouped", false, "print the source location of the first few ungrouped records")
	llmAddrFlag := fs.String("llm-addr", "", "host:port of an already-running VMR instance — enables the optional LLM interpretation section on -journey's or -compare's report (not supported with -corpus or the default suite). Never auto-started. Default: report.yaml's llm_addr")
	llmModelFlag := fs.String("llm-model", "", "that VMR instance's virtual model name (e.g. \"agent\"), sent verbatim — required with -llm-addr unless -llm-dry-run. Default: report.yaml's llm_model")
	llmKeyFlag := fs.String("llm-key", "", "bearer token for that VMR instance, only needed if it has api_keys configured. Default: report.yaml's llm_key")
	llmCacheDirFlag := fs.String("llm-cache-dir", "", "directory for the disk cache of LLM interpretation results; absent both here and in report.yaml's llm_cache_dir => no caching, ever")
	llmDryRun := fs.Bool("llm-dry-run", false, "with -llm-addr: print the evidence-pack size estimate and exit without calling anything")
	if err := fs.Parse(args); err != nil {
		return err
	}

	selectorCount := 0
	if *journeyArg != "" {
		selectorCount++
	}
	if *compareArg != "" {
		selectorCount++
	}
	if *corpusFlag {
		selectorCount++
	}
	if selectorCount > 1 {
		return fmt.Errorf("-journey/-compare/-corpus are mutually exclusive — pick one zoom level per call")
	}
	hasSelector := selectorCount == 1
	if *renderAllFlag && hasSelector {
		return fmt.Errorf("-render-all has no effect with -journey/-compare/-corpus (it only controls the default suite's rendering scope) — drop one or the other")
	}

	rc := resolveReportConfig(*reportConfigPath, os.Stdout)
	lang, err := resolveLanguage(*langFlag, rc, os.Stdout)
	if err != nil {
		return err
	}
	outDir := resolveString(*outDirFlag, rc.Output, "reports")
	llmAddr := resolveStringExplicit(flagPassed(fs, "llm-addr"), *llmAddrFlag, rc.LLMAddr, "")
	llmModel := resolveString(*llmModelFlag, rc.LLMModel, "")
	// llmKey is resolved (and used for self-traffic exclusion) on every
	// path, including -corpus and the default suite — it identifies PAST
	// self-analysis traffic to exclude, independent of whether THIS run
	// makes a new LLM call. resolveLLMOptions, by contrast, validates a
	// *usable* LLM configuration (requires -llm-addr whenever -llm-model/
	// -llm-key/-llm-dry-run is set) and is only relevant to -journey/
	// -compare, the only branches that consume its result — see
	// dispatchAnalyze's resolveLLMOpts closure.
	llmKey := resolveString(*llmKeyFlag, rc.LLMKey, "")
	llmCacheDir := resolveString(*llmCacheDirFlag, rc.LLMCacheDir, "")
	// Same rejection rule cmd_story.go's cmdStory applies: -llm-addr fires
	// one LLM call per journey, which makes no sense against a batch —
	// -corpus or the default suite (this entry's equivalent of -render-all).
	// -compare/a single-match -journey are the only two shapes that support it.
	llmAddrExplicit := flagPassed(fs, "llm-addr")
	if llmAddrExplicit && (*corpusFlag || !hasSelector) {
		return fmt.Errorf("-llm-addr is not supported with -corpus or the default suite (would fire one LLM call per journey) — use -journey to interpret one at a time, or -compare for a pairwise interpretation")
	}

	paths, err := resolveInputPaths(fs, *configPath)
	if err != nil {
		return err
	}

	return dispatchAnalyze(&analyzeRun{
		paths:              paths,
		configPath:         *configPath,
		outDir:             outDir,
		lang:               lang,
		includePartial:     resolveBool(flagPassed(fs, "include-partial"), *includePartialFlag, rc.IncludePartial),
		includeSelfTraffic: *includeSelfTraffic,
		llmKey:             llmKey,
		llmAddrExplicit:    llmAddrExplicit,
		resolveLLMOpts: func() (llmCLIOptions, error) {
			llmOpts, err := resolveLLMOptions(llmAddr, llmModel, llmKey, *llmDryRun)
			if err != nil {
				return llmCLIOptions{}, err
			}
			llmOpts.CacheDir = llmCacheDir
			return llmOpts, nil
		},
		corpusFlag:      *corpusFlag,
		compareArg:      *compareArg,
		journeyArg:      *journeyArg,
		renderAllFlag:   *renderAllFlag,
		detailsOn:       resolveBool(flagPassed(fs, "details"), *detailsFlag, rc.Details),
		displayCCY:      resolveString(*currencyFlag, rc.Currency, ""),
		exchangeRate:    rc.ExchangeRate,
		selfTrafficTags: rc.SelfTrafficClientTags,
		showUngrouped:   *showUngrouped,
	})
}

// dispatchAnalyze runs setupStoryRun once (needed by every mode) and then
// routes to exactly one of: -corpus, -compare, -journey, or the default
// suite — see cmd_analyze.go's package comment for the "pure CLI routing"
// constraint this implements.
func dispatchAnalyze(r *analyzeRun) error {
	// Story half's setup runs unconditionally and first, for every mode —
	// same rationale cmd_analyze.go carried before P9.1: report.Markdown
	// links to stories/vmr-stories.md when that file already exists at
	// render time (loadStoriesLink, P6.2a), so running story's setup+index
	// write before the report half means a first-ever `analyze` call
	// already gets that edge right.
	su, err := setupStoryRun(r.paths, r.outDir, r.includeSelfTraffic, r.llmKey, r.selfTrafficTags, r.showUngrouped, r.lang)
	if err != nil {
		return err
	}

	switch {
	case r.corpusFlag:
		return corpusStats(su.cands, su.byIdx, su.firstPath, su.prof, r.includePartial, r.outDir, r.lang, su.idx)
	case r.compareArg != "":
		ids := strings.Split(r.compareArg, ",")
		if len(ids) != 2 || ids[0] == "" || ids[1] == "" {
			return fmt.Errorf("-compare wants exactly two comma-separated ids: -compare id1,id2")
		}
		llmOpts, err := r.resolveLLMOpts()
		if err != nil {
			return err
		}
		return compareJourneys(su.cands, su.byIdx, ids[0], ids[1], su.firstPath, su.prof, r.includePartial, r.outDir, llmOpts, r.lang, su.idx)
	case r.journeyArg != "":
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
			return renderJourney(targets[0], su.byIdx, su.firstPath, su.prof, r.includePartial, r.outDir, llmOpts, r.lang, su.idx)
		}
		if r.llmAddrExplicit {
			return fmt.Errorf("-llm-addr is not supported when -journey matches more than one journey (%d matched by %q) — use a single id/pattern that resolves to exactly one journey", len(targets), r.journeyArg)
		}
		return renderJourneys(targets, su.byIdx, su.firstPath, su.prof, r.includePartial, r.outDir, r.lang, su.idx,
			"no matching journeys to render (all skipped as partial-head; pass -include-partial)")
	default:
		// Default suite: story half (P9.2's category=task scope unless
		// -render-all) first, then the macro report half.
		scope := su.cands
		if !r.renderAllFlag {
			scope = taskOnlyCandidates(su)
		}
		if err := renderAllJourneys(scope, su.byIdx, su.firstPath, su.prof, r.includePartial, r.outDir, r.lang, su.idx); err != nil {
			return fmt.Errorf("analyze (story half): %w", err)
		}

		var excludeClientTags map[string]bool
		if !r.includeSelfTraffic {
			excludeClientTags = selfTrafficExcludeTags(r.llmKey, r.selfTrafficTags)
		}
		if err := runReport(r.paths, timestampWriter{w: os.Stdout}, reportRunOpts{
			configPath:        r.configPath,
			outDir:            r.outDir,
			detailsOn:         r.detailsOn,
			lang:              r.lang,
			displayCCY:        r.displayCCY,
			exchangeRate:      r.exchangeRate,
			excludeClientTags: excludeClientTags,
		}); err != nil {
			return fmt.Errorf("analyze (report half): %w", err)
		}
		return nil
	}
}
