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

	"vmr/internal/chatmsg"
	"vmr/internal/config"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/story"
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
	// macroOnly/listOnly/storyOnly (P15.1) are the three modes
	// `vmr report`/`vmr story` each had that the default suite couldn't
	// previously express — see cmdReport/cmdStory's own doc comments for
	// why they now translate into these instead of keeping a second,
	// independent dispatch. storyOnly was added after an independent
	// review (2026-08-21) caught that its first cut left this as an
	// internal-only field only cmdStory's forwarder could set — a direct
	// `vmr analyze` user had no way to reach "render every candidate,
	// skip the macro report" (what bare `vmr story -render-all` always
	// did), contradicting analyze's own "single entry point, strictly
	// covers every alias behavior" premise. Unlike macroOnly/listOnly,
	// storyOnly composes with renderAllFlag rather than excluding it —
	// -story-only alone means "default suite's non-noise scope, story
	// half only"; -story-only -render-all means "every candidate, story
	// half only" (cmdStory's -render-all forwarding target).
	macroOnly       bool
	listOnly        bool
	storyOnly       bool
	detailsOn       bool
	displayCCY      string
	exchangeRate    map[string]float64
	selfTrafficTags []string
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
	// htmlOn/redactOn (E1): -journey only, single match only — a
	// self-contained HTML view of one journey, optionally with every
	// conversation body swapped for a length placeholder. Same "one journey
	// at a time" constraint as -llm-addr.
	htmlOn   bool
	redactOn bool
}

// validateAnalyzeModeFlags checks the mutual-exclusion rules across
// cmdAnalyze's mode-selecting flags — split out once folding P15.1's new
// modes into cmdAnalyze inline pushed it over archtest's per-function line
// budget (a "composition, not an algorithm" split, same reasoning as
// analyzeRun's own). Returns whether exactly one of -journey/-compare/
// -corpus was given. -story-only is deliberately NOT exclusive with
// -render-all (unlike -macro-only/-list-only) — it composes with it,
// see analyzeRun.storyOnly's own doc comment.
func validateAnalyzeModeFlags(journeyArg, compareArg string, corpusFlag, renderAllFlag, macroOnlyFlag, listOnlyFlag, storyOnlyFlag, detailsPassed bool) (bool, error) {
	selectorCount := 0
	if journeyArg != "" {
		selectorCount++
	}
	if compareArg != "" {
		selectorCount++
	}
	if corpusFlag {
		selectorCount++
	}
	if selectorCount > 1 {
		return false, fmt.Errorf("-journey/-compare/-corpus are mutually exclusive — pick one zoom level per call")
	}
	hasSelector := selectorCount == 1
	if renderAllFlag && hasSelector {
		return false, fmt.Errorf("-render-all has no effect with -journey/-compare/-corpus (it only controls the default suite's rendering scope) — drop one or the other")
	}
	exclusiveCount := 0
	for _, f := range []bool{macroOnlyFlag, listOnlyFlag, storyOnlyFlag} {
		if f {
			exclusiveCount++
		}
	}
	if exclusiveCount > 1 {
		return false, fmt.Errorf("-macro-only/-list-only/-story-only are mutually exclusive — pick one")
	}
	if (macroOnlyFlag || listOnlyFlag) && (hasSelector || renderAllFlag) {
		return false, fmt.Errorf("-macro-only/-list-only replace the default suite entirely — mutually exclusive with -journey/-compare/-corpus/-render-all")
	}
	if storyOnlyFlag && hasSelector {
		return false, fmt.Errorf("-story-only replaces the default suite entirely — mutually exclusive with -journey/-compare/-corpus (but composes with -render-all)")
	}
	if listOnlyFlag && detailsPassed {
		return false, fmt.Errorf("-details has no effect with -list-only (it never renders, let alone materializes, any journey) — drop one or the other")
	}
	return hasSelector, nil
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
	compareArg := fs.String("compare", "", "zoom into a pairwise comparison: -compare id1,id2 (each an id, id-prefix, or shell glob; first candidate matching each side wins). Mutually exclusive with -journey/-corpus; only this half runs (no macro report)")
	corpusFlag := fs.Bool("corpus", false, "zoom into corpus-level statistics (metric distributions, Finding hit rates, correlations) across every non-partial candidate journey. Mutually exclusive with -journey/-compare; only this half runs (no macro report)")
	// Default-suite-only scope knob (P9.2) — meaningless (rejected) with a selector above.
	renderAllFlag := fs.Bool("render-all", false, "default suite only: materialize every non-partial candidate journey, including the heartbeat/poll ones (default: those low-signal heartbeat candidates are the only ones excluded; render one on demand with -journey <id>)")
	// P15.1: the three modes bare `vmr report`/bare `vmr story` (with and
	// without -render-all) each had that the default suite couldn't
	// express — mutually exclusive with the selectors above and each
	// other; -story-only alone composes with -render-all (see below).
	macroOnlyFlag := fs.Bool("macro-only", false, "default suite only: run just the macro report half (equivalent to 'vmr report') — no candidate scan, no journey rendering, no stories/ output. Mutually exclusive with -journey/-compare/-corpus/-render-all/-list-only/-story-only")
	listOnlyFlag := fs.Bool("list-only", false, "default suite only: list candidate journeys without rendering any of them (equivalent to bare 'vmr story') — writes stories/vmr-stories.{md,json} listing every candidate, but no journey-*.md. Mutually exclusive with -journey/-compare/-corpus/-render-all/-macro-only/-story-only/-details")
	storyOnlyFlag := fs.Bool("story-only", false, "default suite only: run just the story half, skipping the macro report — no vmr-report.{json,md}/vmr-requests* written. Composes with -render-all (equivalent to 'vmr story -render-all'); alone, equivalent to bare 'vmr story' (its default non-noise scope) without the macro report. Mutually exclusive with -journey/-compare/-corpus/-macro-only/-list-only")
	// story-half flags.
	htmlFlag := fs.Bool("html", false, "with a single-match -journey or with -compare: also write a self-contained .html dashboard next to the .md ({out}/stories/journey-<id>.html or compare-<a>-vs-<b>.html) — verdict/structure/metrics/findings for a journey, sides/divergence/diff/LLM for a comparison; inline CSS/JS, zero external requests. No effect on any other mode")
	redactFlag := fs.Bool("redact", false, "with -html: replace every conversation body with a '‹text: N chars›' length placeholder and drop the per-step detail links, finding text and (for -compare) the LLM section — structure, metrics, roles, token counts and tool names stay. For sharing outside the team")
	detailsFlag := fs.Bool("details", false, "also render one Markdown file per request into {out}/details/ (default: false — the requests index links to each record's detail filename regardless, computed without needing the file to exist)")
	currencyFlag := fs.String("currency", "", "display currency for $ cost estimates, e.g. CNY|JPY")
	includePartialFlag := fs.Bool("include-partial", false, "also render journeys whose head looks truncated by the loaded file range (default: report.yaml's include_partial, or false)")
	showUngrouped := fs.Bool("show-ungrouped", false, "print the source location of the first few ungrouped records")
	llmAddrFlag := fs.String("llm-addr", "", "host:port of an already-running VMR instance — enables the optional LLM interpretation section on -journey's or -compare's report (not supported with -corpus or the default suite). Never auto-started. Default: report.yaml's llm_addr")
	llmModelFlag := fs.String("llm-model", "", "that VMR instance's virtual model name (e.g. \"agent\"), sent verbatim — required with -llm-addr unless -llm-dry-run. Default: report.yaml's llm_model")
	llmKeyFlag := fs.String("llm-key", "", "bearer token for that VMR instance, only needed if it has api_keys configured. Default: report.yaml's llm_key")
	llmCacheDirFlag := fs.String("llm-cache-dir", "", "directory for the disk cache of LLM interpretation results; absent both here and in report.yaml's llm_cache_dir => no caching, ever")
	llmDryRun := fs.Bool("llm-dry-run", false, "with -llm-addr: print every LLM call this run would make — per evidence-pack size estimate and the maximum call count (detector packs included) — and exit without calling anything")
	if err := fs.Parse(args); err != nil {
		return err
	}

	hasSelector, err := validateAnalyzeModeFlags(*journeyArg, *compareArg, *corpusFlag, *renderAllFlag, *macroOnlyFlag, *listOnlyFlag, *storyOnlyFlag, flagPassed(fs, "details"))
	if err != nil {
		return err
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
	// Gate on the RESOLVED value, not just "was the flag typed": an explicit
	// `-llm-addr ""` is the sanctioned way to suppress a report.yaml llm_addr
	// for this run, so it must pass here; a report.yaml llm_addr with no flag
	// stays silently ignored on these batch shapes (never consulted downstream).
	llmAddrExplicit := flagPassed(fs, "llm-addr")
	if llmAddrExplicit && llmAddr != "" && (*corpusFlag || !hasSelector) {
		return fmt.Errorf("-llm-addr is not supported with -corpus or the default suite (would fire one LLM call per journey) — use -journey to interpret one at a time, or -compare for a pairwise interpretation")
	}
	if *redactFlag && !*htmlFlag {
		return fmt.Errorf("-redact only applies with -html")
	}
	if (*htmlFlag || *redactFlag) && *journeyArg == "" && *compareArg == "" {
		return fmt.Errorf("-html/-redact only apply with -journey (a single journey) or -compare (a pair)")
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
		corpusFlag:         *corpusFlag,
		compareArg:         *compareArg,
		journeyArg:         *journeyArg,
		renderAllFlag:      *renderAllFlag,
		macroOnly:          *macroOnlyFlag,
		listOnly:           *listOnlyFlag,
		storyOnly:          *storyOnlyFlag,
		detailsOn:          resolveBool(flagPassed(fs, "details"), *detailsFlag, rc.Details),
		displayCCY:         resolveString(*currencyFlag, rc.Currency, ""),
		exchangeRate:       rc.ExchangeRate,
		selfTrafficTags:    rc.SelfTrafficClientTags,
		reportConfigSource: rc.SourcePath,
		showUngrouped:      *showUngrouped,
		htmlOn:             *htmlFlag,
		redactOn:           *redactFlag,
	})
}

// dispatchAnalyze routes to exactly one of: -macro-only, -corpus, -compare,
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

	// -macro-only is handled before setupStoryRun runs at all (P15.1): bare
	// `vmr report` never scans/stitches the story-half candidate graph or
	// touches stories/.parse-cache, so running setupStoryRun first and
	// merely skipping its output would leave -macro-only doing strictly
	// more work — and possibly more I/O — than the alias it stands in for,
	// breaking "produces the same output" into "produces the same output,
	// slower and with side effects".
	if r.macroOnly {
		return runReportHalf(r)
	}

	// Story half's setup runs unconditionally and first, for every
	// remaining mode — same rationale cmd_analyze.go carried before P9.1:
	// report.Markdown links to stories/vmr-stories.md when that file
	// already exists at render time (loadStoriesLink, P6.2a), so running
	// story's setup+index write before the report half means a first-ever
	// `analyze` call already gets that edge right.
	su, err := setupStoryRun(r.paths, r.outDir, r.includeSelfTraffic, r.llmKey, r.selfTrafficTags, r.showUngrouped, r.lang)
	if err != nil {
		return err
	}

	switch {
	case r.listOnly:
		return listJourneys(su.idx, su.g, r.outDir, r.includePartial, r.lang)
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
		priceRes, ccy := resolvePricingForAnalyze(r.cfg, r.cfgErr, r.configPath, r.displayCCY, r.exchangeRate)
		return compareJourneys(su.cands, su.byIdx, ids[0], ids[1], su.firstPath, su.prof, r.includePartial, r.outDir, llmOpts, r.lang, su.idx, priceRes, ccy, r.htmlOn, r.redactOn)
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
			priceRes, ccy := resolvePricingForAnalyze(r.cfg, r.cfgErr, r.configPath, r.displayCCY, r.exchangeRate)
			return renderJourney(targets[0], su.byIdx, su.firstPath, su.prof, r.includePartial, r.outDir, llmOpts, r.lang, su.idx, priceRes, ccy, r.htmlOn, r.redactOn)
		}
		if r.llmAddrExplicit {
			return fmt.Errorf("-llm-addr is not supported when -journey matches more than one journey (%d matched by %q) — use a single id/pattern that resolves to exactly one journey", len(targets), r.journeyArg)
		}
		if r.htmlOn {
			return fmt.Errorf("-html/-redact need a -journey selector that resolves to exactly one journey (%d matched by %q)", len(targets), r.journeyArg)
		}
		// true: a -journey selector naming several targets is still a
		// user-named set, not the default suite's implicit batch (P13.1).
		priceRes, ccy := resolvePricingForAnalyze(r.cfg, r.cfgErr, r.configPath, r.displayCCY, r.exchangeRate)
		return renderJourneys(targets, su.byIdx, su.firstPath, su.prof, r.includePartial, r.outDir, r.lang, su.idx,
			"no matching journeys to render (all skipped as partial-head; pass -include-partial)", true, priceRes, ccy)
	default:
		// Default suite: story half (P14.1's non-noise scope unless
		// -render-all) first, then the macro report half.
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
			return fmt.Errorf("analyze (story half): %w", err)
		}
		if r.storyOnly {
			return nil
		}
		return runReportHalf(r)
	}
}

// runReportHalf runs the macro report half against r's already-resolved
// options — the one call site both the default suite's second step and
// -macro-only route through, so "produces the same report" is structural,
// not a maintained-in-parallel promise (P15.1/P15.2).
func runReportHalf(r *analyzeRun) error {
	var excludeClientTags map[string]bool
	if !r.includeSelfTraffic {
		excludeClientTags = selfTrafficExcludeTags(r.llmKey, r.selfTrafficTags)
	}
	if err := runReport(r.paths, timestampWriter{w: os.Stdout}, reportRunOpts{
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
	}); err != nil {
		return fmt.Errorf("analyze (report half): %w", err)
	}
	return nil
}

// analyzeAliasRun bundles the inputs cmdReport and cmdStory have already
// resolved (their own flag sets differ, so they parse them in-place and
// then call dispatchAnalyzeFrom with this struct) — the shared post-parse
// path that IS the convergence. Naming the fields after the flag they
// came from keeps the alias bodies mechanical to read: each alias does its
// own flag.NewFlagSet+Parse, its own per-mode validation, then hands the
// results here unchanged.
type analyzeAliasRun struct {
	// fsAlias is the alias's own flag.FlagSet (already parsed) — the shared
	// helper threads it into resolveInputPaths to read positional args. Each
	// alias has a different flag set, but positional args follow the same
	// "<audit.jsonl|glob>..." convention the helper already understands.
	fsAlias *flag.FlagSet
	// configPath, reportConfigPath, outDir, langFlag, includeSelfTraffic:
	// plain string/bool values resolved by the alias's own flag.NewFlagSet
	// before handing off.
	configPath         string
	reportConfigPath   string
	outDirFlag         string
	langFlag           string
	includeSelfTraffic bool
	// includePartialSet: flagPassed("include-partial") — whether the user
	// typed the flag at all (includePartial lives in *bool on reportConfig,
	// so the alias must distinguish "absent" from "explicitly false").
	// includePartialFlag is the bool's own resolved value (false when
	// absent). Only cmdStory exposes this flag; cmdReport passes false.
	includePartialSet  bool
	includePartialFlag bool
	// LLM fields, all "" / false for cmdReport (its forwarder never makes
	// an LLM call). The shared helper still threads them so an alias can
	// safely pass through cmdAnalyze's exact resolveLLMOpts closure.
	llmAddr     string
	llmAddrSet  bool // flagPassed("llm-addr")
	llmModel    string
	llmKey      string
	llmCacheDir string
	llmDryRun   bool
	// analyzeRun-shaped fields the alias fills directly, because every
	// alias passes through to the same dispatch — these ARE the bits that
	// used to be a hand-maintained mapping in each alias's body.
	corpusFlag    bool
	compareArg    string
	journeyArg    string
	renderAllFlag bool
	macroOnly     bool
	listOnly      bool
	storyOnly     bool
	detailsOn     bool
	detailsSet    bool // flagPassed("details")
	displayCCY    string
	// htmlOn/redactOn: passed through to dispatchAnalyze's html/redact
	// handling. The -html/-redact validation itself stays in the alias
	// bodies (cmdStory validates it before handing off; cmdReport has no
	// such flags), preserving each command's historical error precedence.
	htmlOn   bool
	redactOn bool
	// showUngrouped: cmdReport doesn't have it (purely a story-half flag);
	// the alias passes false to keep the shared helper honest.
	showUngrouped bool
	// aliasName is the forwarder's own name — "report" or "story" — used
	// in the stderr alias hint dispatchAnalyzeFrom prints.
	aliasName string
}

// dispatchAnalyzeFrom is the convergence point (IS-25): every
// post-flag-parse step cmdReport and cmdStory used to duplicate — input
// path resolution, report.yaml load + language resolve, the llm-addr scope
// gate, the analyzeRun build, and the final dispatchAnalyze call — lives
// here once. Each alias does its own flag.NewFlagSet+Parse (their flag
// lists are genuinely different) and its own per-mode validation (the
// corpus-selector and -html/-redact rules are alias-specific and checked
// before handing off, so the error precedence each command always had is
// preserved), then hands the result to this helper. `vmr report` and `vmr
// story` remain thin forwarders: their per-alias flag declarations + one
// call to this helper, no second resolution/dispatch path to keep in sync.
func dispatchAnalyzeFrom(a analyzeAliasRun) error {
	paths, err := resolveInputPaths(a.fsAlias, a.configPath)
	if err != nil {
		return err
	}
	rc := resolveReportConfig(a.reportConfigPath, os.Stdout)
	lang, err := resolveLanguage(a.langFlag, rc, os.Stdout)
	if err != nil {
		return err
	}
	outDir := resolveString(a.outDirFlag, rc.Output, "reports")
	llmKey := resolveString(a.llmKey, rc.LLMKey, "")
	llmCacheDir := resolveString(a.llmCacheDir, rc.LLMCacheDir, "")
	// llmModel follows the same plain-flag merge (no explicit-empty case in
	// resolveLLMOptions, just "if addr then model required"): the alias's
	// own -llm-model flag wins, else report.yaml's llm_model.
	llmModel := resolveString(a.llmModel, rc.LLMModel, "")
	// llmAddr follows resolveStringExplicit: an explicit -llm-addr flag
	// (even when set to "") wins over report.yaml's llm_addr, which in
	// turn wins over the empty default — the same merge order the
	// -llm-addr's "explicit -llm-addr '' suppresses a configured
	// llm_addr" behavior depends on (cmd_analyze.go's own docstring).
	llmAddr := resolveStringExplicit(a.llmAddrSet, a.llmAddr, rc.LLMAddr, "")
	// Gate on the resolved value (mirrors cmdAnalyze's rule): an explicit
	// `-llm-addr ""` (suppress a report.yaml llm_addr for this run) must
	// pass; a configured llm_addr with no flag stays silently ignored on
	// the batch shapes (never consulted downstream). The flag-vs-mode
	// combination check is the alias's own responsibility — cmdStory's
	// specific "-llm-addr is not supported with -render-all/-corpus/
	// bare `vmr story`" message has to be more specific than cmdAnalyze's
	// general one, so the alias gates before calling here.
	hasSelector := a.journeyArg != "" || a.compareArg != ""
	if a.llmAddrSet && llmAddr != "" && (a.corpusFlag || a.renderAllFlag || !hasSelector) {
		return fmt.Errorf("-llm-addr is not supported with -render-all, -corpus, or bare `vmr %s` (would fire one LLM call per journey, or never be used at all) — use -journey to interpret one at a time, or -compare for a pairwise interpretation", a.aliasName)
	}

	return dispatchAnalyze(&analyzeRun{
		paths:              paths,
		configPath:         a.configPath,
		outDir:             outDir,
		lang:               lang,
		includePartial:     resolveBool(a.includePartialSet, a.includePartialFlag, rc.IncludePartial),
		includeSelfTraffic: a.includeSelfTraffic,
		llmKey:             llmKey,
		llmAddrExplicit:    a.llmAddrSet,
		resolveLLMOpts: func() (llmCLIOptions, error) {
			llmOpts, err := resolveLLMOptions(llmAddr, llmModel, llmKey, a.llmDryRun)
			if err != nil {
				return llmCLIOptions{}, err
			}
			llmOpts.CacheDir = llmCacheDir
			return llmOpts, nil
		},
		corpusFlag:         a.corpusFlag,
		compareArg:         a.compareArg,
		journeyArg:         a.journeyArg,
		renderAllFlag:      a.renderAllFlag,
		macroOnly:          a.macroOnly,
		listOnly:           a.listOnly,
		storyOnly:          a.storyOnly,
		detailsOn:          resolveBool(a.detailsSet, a.detailsOn, rc.Details),
		displayCCY:         resolveString(a.displayCCY, rc.Currency, ""),
		exchangeRate:       rc.ExchangeRate,
		selfTrafficTags:    rc.SelfTrafficClientTags,
		reportConfigSource: rc.SourcePath,
		showUngrouped:      a.showUngrouped,
		htmlOn:             a.htmlOn,
		redactOn:           a.redactOn,
	})
}
