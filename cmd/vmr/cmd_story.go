// Ver 2026-08-01, by Sonnet 5

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/story"
	"vmr/internal/story/profile"
)

// llmCLIOptions bundles the -llm-* flags after validation — a thin CLI-level
// wrapper around story.LLMOptions that also carries -llm-dry-run (a command-
// behavior switch compareJourneys itself acts on, not something
// story.Interpret needs to know about).
type llmCLIOptions struct {
	story.LLMOptions
	DryRun bool
}

// resolveLLMOptions validates the -llm-* flag combination: -llm-addr is the
// sole switch that turns the interpretation layer on (design doc plan
// review — no separate -llm bool); -llm-model is required alongside it
// unless -llm-dry-run (a dry run never sends a request, so it never needs to
// know which model to ask); -llm-model/-llm-key/-llm-dry-run without
// -llm-addr are rejected outright rather than silently ignored, since that
// combination is almost certainly a missed flag, not an intentional no-op.
func resolveLLMOptions(addr, model, key string, dryRun bool) (llmCLIOptions, error) {
	if addr == "" {
		switch {
		case dryRun:
			return llmCLIOptions{}, fmt.Errorf("-llm-dry-run requires -llm-addr")
		case model != "" || key != "":
			return llmCLIOptions{}, fmt.Errorf("-llm-model/-llm-key require -llm-addr")
		}
		return llmCLIOptions{}, nil
	}
	if model == "" && !dryRun {
		return llmCLIOptions{}, fmt.Errorf("-llm-model is required when -llm-addr is given (unless -llm-dry-run)")
	}
	return llmCLIOptions{LLMOptions: story.LLMOptions{Addr: addr, Model: model, APIKey: key}, DryRun: dryRun}, nil
}

// cmdStory renders one agent task's full execution history as a
// self-contained Markdown narrative — see
// docs/VirtualModelRouter_Design_v4_Analytics.md's `internal/story`: Journey 视图 section for the design.
// Step 2: a candidate is no longer always
// exactly one ctxgraph.Lineage — ctxgraph.StitchGraph resolves Contract/
// Fork breaks back to their best predecessor where the evidence supports
// it, and ctxgraph.ChainFrom walks the resulting chain into the ordered
// list of lineages one Journey actually renders. A lineage that still
// starts mid-conversation after best-effort stitching (no confident
// predecessor found) is rendered with an explicit "context was rebuilt
// here, unresolved" notice rather than silently treated as a fresh start.
//
// With no input files given at all, defaults to <-c config.yaml's
// log_dir>/vmr-audit-* (see resolveInputPaths in auditpaths.go), same
// convention as cmdReport.
func cmdStory(args []string) error {
	fs := flag.NewFlagSet("story", flag.ExitOnError)
	configPath := fs.String("c", "config.yaml", "config file to resolve log_dir from, when no input files are given")
	outDirFlag := fs.String("o", "", "output directory (default: ./reports, or report.yaml's output)")
	journeyArg := fs.String("journey", "", "render this journey: an id or id-prefix, a comma-separated list of ids/prefixes/globs, and/or a shell-style glob (*, ?, [...]) matched against the full id — e.g. -journey j-a,j-b or -journey 'j-openclaw-*'. A selector resolving to exactly one journey renders as before (and alone supports -llm-addr); more than one batches like -render-all")
	renderAll := fs.Bool("render-all", false, "render every non-partial candidate journey in one batched pass, instead of picking one id at a time")
	compare := fs.String("compare", "", "compare two journeys' behavior profiles: -compare id1,id2 (each an id or id prefix)")
	corpus := fs.Bool("corpus", false, "compute corpus-level statistics (metric distributions, Finding hit rates, correlations) across every non-partial candidate journey")
	includePartialFlag := fs.Bool("include-partial", false, "also list/render journeys whose head looks truncated by the loaded file range (default: report.yaml's include_partial, or false)")
	showUngrouped := fs.Bool("show-ungrouped", false, "print the source location of the first few ungrouped records")
	llmAddrFlag := fs.String("llm-addr", "", "host:port of an already-running VMR instance — enables the optional LLM interpretation section on -journey's or -compare's report (-compare also adds a second, divergence-point-scoped section when one was detected; not supported with -render-all/-corpus). Never auto-started; the instance must already be up. Default: report.yaml's llm_addr")
	llmModelFlag := fs.String("llm-model", "", "that VMR instance's virtual model name (e.g. \"agent\"), sent verbatim — required with -llm-addr unless -llm-dry-run. Default: report.yaml's llm_model")
	llmKeyFlag := fs.String("llm-key", "", "bearer token for that VMR instance, only needed if it has api_keys configured. Default: report.yaml's llm_key (typically \"${SOME_ENV_VAR}\")")
	llmCacheDirFlag := fs.String("llm-cache-dir", "", "directory for the disk cache of LLM interpretation results; absent both here and in report.yaml's llm_cache_dir => no caching, ever (no implicit default path)")
	llmDryRun := fs.Bool("llm-dry-run", false, "with -llm-addr: print the evidence-pack size estimate and exit without calling anything")
	langFlag := fs.String("lang", "", "output language: en|zh (default: report.yaml's language, or en) — overrides report.yaml")
	reportConfigPath := fs.String("report-config", "", "vmr report/vmr story sidecar config yaml; absent => auto-load ./report.yaml if present")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rc := resolveReportConfig(*reportConfigPath, os.Stdout)
	lang, err := resolveLanguage(*langFlag, rc, os.Stdout)
	if err != nil {
		return err
	}
	outDir := resolveString(*outDirFlag, rc.Output, "reports")
	includePartial := resolveBool(flagPassed(fs, "include-partial"), *includePartialFlag, rc.IncludePartial)
	llmAddr := resolveString(*llmAddrFlag, rc.LLMAddr, "")
	llmModel := resolveString(*llmModelFlag, rc.LLMModel, "")
	llmKey := resolveString(*llmKeyFlag, rc.LLMKey, "")
	llmCacheDir := resolveString(*llmCacheDirFlag, rc.LLMCacheDir, "")
	llmOpts, err := resolveLLMOptions(llmAddr, llmModel, llmKey, *llmDryRun)
	if err != nil {
		return err
	}
	llmOpts.CacheDir = llmCacheDir
	// llmAddrExplicit, not llmOpts.Addr != "", gates every "-llm-addr isn't
	// supported here" rejection below: report.yaml's llm_addr is a
	// standing convenience default for the single-journey/-compare case
	// (so a lone `-journey <id>` never needs -llm-addr typed out), and
	// none of -render-all/-corpus/a multi-match -journey selector ever
	// reads llmOpts at all — they'd just silently not use it. Treating
	// that default's mere presence as a hard error (as this used to)
	// means anyone with an llm_addr configured for convenience can no
	// longer run a plain batch render at all; only an -llm-addr the user
	// actually typed on this invocation is worth rejecting outright.
	llmAddrExplicit := flagPassed(fs, "llm-addr")
	if llmAddrExplicit && (*renderAll || *corpus) {
		return fmt.Errorf("-llm-addr is not supported with -render-all or -corpus (would fire one LLM call per journey) — use -journey to interpret one at a time")
	}
	if *corpus && (*compare != "" || *journeyArg != "" || *renderAll) {
		return fmt.Errorf("-corpus is exclusive with -journey/-render-all/-compare — run it on its own")
	}
	paths, err := resolveInputPaths(fs, *configPath)
	if err != nil {
		return err
	}

	// indexPath is computed (and LoadStoryIndex'd) up front, before
	// anything is scanned — this is a pure string join plus a best-effort
	// file read, no directory creation, so it stays safe to do even on an
	// -llm-dry-run path that must leave reports/stories/ untouched if it
	// returns early (ensureStoriesDir/idx.Save only happen once each
	// branch below reaches its own normal write point).
	storiesDir := filepath.Join(outDir, "stories")
	indexPath := filepath.Join(storiesDir, "vmr-stories.json")
	prior := story.LoadStoryIndex(indexPath)

	fmt.Printf("scanning %d file(s)...\n", len(paths))
	g, fileCache, err := ctxgraph.ScanCached(paths, &prior.Files)
	if err != nil {
		return err
	}
	fmt.Printf("%d lineage(s), %d ungrouped record(s), %d unparseable record(s)\n", len(g.Lineages), len(g.Ungrouped), g.NoBody)
	if *showUngrouped {
		printUngrouped(g.Ungrouped, lang)
	}
	firstPath := paths[0]

	ctxgraph.StitchGraph(g)
	byIdx := ctxgraph.LineageIndex(g)

	// Step 1 ships exactly one profile: OpenClaw-aware
	// but harmless on any other agent's input, since none of its patterns
	// match generic chat text.
	prof := profile.OpenClawAware

	cands := story.ListCandidates(g)

	// One batched title fetch across every candidate (story.PreviewTitles
	// groups reads by source file, so this scans each file at most once no
	// matter how many candidates), reused both by the index rows below and
	// by listJourneys' stdout listing — the index is now the single place
	// that derives a candidate's cheap fields, no branch recomputes them.
	chains := make([][]*ctxgraph.Lineage, len(cands))
	for i, l := range cands {
		chains[i] = ctxgraph.ChainFrom(l, byIdx)
	}
	titles, err := story.PreviewTitles(chains, prof, lang)
	if err != nil {
		return err
	}
	freshRows := make([]story.JourneyIndexRow, len(cands))
	for i, l := range cands {
		partial := story.IsPartialHead(chains[i], firstPath)
		freshRows[i] = story.BuildJourneyIndexRow(chains[i], titles[l], partial)
	}
	idx := &story.StoryIndex{Files: *fileCache, Journeys: story.MergeJourneyIndexRows(freshRows, prior.Journeys)}

	if *compare != "" {
		ids := strings.Split(*compare, ",")
		if len(ids) != 2 || ids[0] == "" || ids[1] == "" {
			return fmt.Errorf("-compare wants exactly two comma-separated ids: -compare id1,id2")
		}
		return compareJourneys(cands, byIdx, ids[0], ids[1], firstPath, prof, includePartial, outDir, llmOpts, paths, lang, idx)
	}
	if *corpus {
		return corpusStats(cands, byIdx, firstPath, prof, includePartial, outDir, lang, idx)
	}
	if *journeyArg != "" {
		ids := make([]string, len(cands))
		for i, ch := range chains {
			ids[i] = story.ID(ch)
		}
		targets, err := resolveJourneySelector(cands, ids, *journeyArg)
		if err != nil {
			return err
		}
		if len(targets) == 1 {
			return renderJourney(targets[0], byIdx, firstPath, prof, includePartial, outDir, llmOpts, lang, idx)
		}
		if llmAddrExplicit {
			return fmt.Errorf("-llm-addr is not supported when -journey matches more than one journey (%d matched by %q) — use a single id/pattern that resolves to exactly one journey", len(targets), *journeyArg)
		}
		return renderJourneys(targets, byIdx, firstPath, prof, includePartial, outDir, lang, idx,
			"no matching journeys to render (all skipped as partial-head; pass -include-partial)")
	}
	if *renderAll {
		return renderAllJourneys(cands, byIdx, firstPath, prof, includePartial, outDir, lang, idx)
	}
	return listJourneys(idx, g, outDir, includePartial, lang)
}

// updateJourneyRow finds id's row in idx.Journeys and fills in the
// full-Journey-only fields (only known once story.BuildChain has actually
// run) — a no-op if id isn't present (shouldn't happen: every id passed
// here was itself resolved from idx's own candidate set moments earlier).
func updateJourneyRow(idx *story.StoryIndex, id string, tasks, steps int, rendered string) {
	for i := range idx.Journeys {
		if idx.Journeys[i].ID == id {
			idx.Journeys[i].Tasks = tasks
			idx.Journeys[i].Steps = steps
			if rendered != "" {
				idx.Journeys[i].Rendered = rendered
			}
			return
		}
	}
}

// saveStoryIndex writes vmr-stories.json + vmr-stories.md into storiesDir
// (creating it if needed) — called at every branch's normal (non-dry-run)
// exit point, so `vmr story` leaves this pair behind regardless of which
// flags were passed, per the design doc's vmr-stories.json section.
func saveStoryIndex(idx *story.StoryIndex, outDir string, lang i18n.Lang) error {
	storiesDir, err := ensureStoriesDir(outDir)
	if err != nil {
		return err
	}
	if err := idx.Save(filepath.Join(storiesDir, "vmr-stories.json")); err != nil {
		return err
	}
	md := story.RenderStoryIndexMarkdown(idx.Journeys, lang)
	return os.WriteFile(filepath.Join(storiesDir, "vmr-stories.md"), []byte(md), 0o600)
}

// resolveJourneyID finds the candidate chain whose ID (the content-addressed
// j-<client>-<start>-<end>-<code>) starts with idPrefix —
// shared by -journey and -compare, which all resolve a user-supplied id
// prefix the same way (first match in candidate order, as printed by
// running with no selector flag at all).
func resolveJourneyID(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, idPrefix string) (*ctxgraph.Lineage, []*ctxgraph.Lineage, error) {
	for _, l := range cands {
		chain := ctxgraph.ChainFrom(l, byIdx)
		if strings.HasPrefix(story.ID(chain), idPrefix) {
			return l, chain, nil
		}
	}
	return nil, nil, fmt.Errorf("no journey matching id prefix %q (run without -journey to list candidates)", idPrefix)
}

// journeyPatternMatches reports whether id satisfies pattern: a shell-style
// glob (pattern contains *, ?, or [) matched against the full id via
// path.Match — path.Match rather than filepath.Match since a journey id is
// a plain string, not a filesystem path, and this must behave identically
// regardless of OS — or, absent any glob character, the original -journey/
// -compare "id or id prefix" prefix match.
func journeyPatternMatches(id, pattern string) bool {
	if strings.ContainsAny(pattern, "*?[") {
		ok, err := path.Match(pattern, id)
		return err == nil && ok
	}
	return strings.HasPrefix(id, pattern)
}

// resolveJourneySelector parses -journey's value — a comma-separated list
// of tokens, each an id/id-prefix or a shell-style glob (see
// journeyPatternMatches) — into the matching candidate set: every token's
// matches are merged and de-duplicated, returned in candidate (i.e.
// chronological listing) order, same order -render-all/-corpus already
// use. Every token must match at least one candidate, else the whole
// selector errors — same "fail loud on what looks like a typo" stance the
// single-id form always had, now applied per token so `-journey
// real,typo` still catches the typo instead of silently rendering only the
// real one. Unlike resolveJourneyID (still used by -compare, which needs
// exactly one match per side and keeps its own "first match wins"
// contract), a plain prefix token here can resolve to more than one
// journey — the natural reading of "select journeys whose id starts with
// this" once selection is a set instead of a single target.
func resolveJourneySelector(cands []*ctxgraph.Lineage, ids []string, selector string) ([]*ctxgraph.Lineage, error) {
	matched := make([]bool, len(cands))
	for _, tok := range strings.Split(selector, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return nil, fmt.Errorf("-journey %q: empty id/pattern between commas", selector)
		}
		hit := false
		for i, id := range ids {
			if journeyPatternMatches(id, tok) {
				matched[i] = true
				hit = true
			}
		}
		if !hit {
			return nil, fmt.Errorf("no journey matching %q (run without -journey to list candidates)", tok)
		}
	}
	var out []*ctxgraph.Lineage
	for i, l := range cands {
		if matched[i] {
			out = append(out, l)
		}
	}
	return out, nil
}

// listJourneys prints the candidate listing (unchanged stdout format) and,
// as of the vmr-stories.json change, also persists it — idx's rows already
// carry everything this needs (id, mark info, request count, time range,
// title), computed once in cmdStory and shared with the index, so this
// function no longer touches ctxgraph/story.PreviewTitles itself.
func listJourneys(idx *story.StoryIndex, g *ctxgraph.Graph, outDir string, includePartial bool, lang i18n.Lang) error {
	t := i18n.CLI(lang)
	excluded := len(g.Lineages) - len(idx.Journeys)
	fmt.Printf("%d candidate journey(s) (%d total lineage(s), %d single-request/scheduled excluded or absorbed into a stitched chain):\n\n", len(idx.Journeys), len(g.Lineages), excluded)

	skippedPartial := 0
	for _, r := range idx.Journeys {
		if r.Partial && !includePartial {
			skippedPartial++
			continue
		}
		mark := ""
		if r.Partial {
			mark = t.HeadTruncatedMark
		}
		if r.Stitched > 1 {
			mark += t.StitchedMark(r.Stitched)
		}
		fmt.Print(t.ListLine(r.ID, mark, r.Requests, r.Start.In(fmtutil.DisplayZone).Format("01-02 15:04"), r.End.In(fmtutil.DisplayZone).Format("15:04"), r.Title))
	}
	if skippedPartial > 0 {
		fmt.Print(t.SkippedPartialNote(skippedPartial))
	}
	fmt.Print(t.RenderHint)
	return saveStoryIndex(idx, outDir, lang)
}

// maxUngroupedShown caps how many ungrouped records -show-ungrouped prints —
// a triage aid, not a report; showing all of them defeats the point when
// there are thousands.
const maxUngroupedShown = 10

// printUngrouped prints the source location of the first few ungrouped
// manifests, so -show-ungrouped gives an operator somewhere to start
// looking instead of just a bare count.
func printUngrouped(ms []*ctxgraph.Manifest, lang i18n.Lang) {
	if len(ms) == 0 {
		return
	}
	t := i18n.CLI(lang)
	n := len(ms)
	if n > maxUngroupedShown {
		n = maxUngroupedShown
	}
	fmt.Print(t.UngroupedHeader(n))
	for _, m := range ms[:n] {
		fmt.Printf("    %s:%d  ts=%s\n", m.Path, m.Line, m.TS.In(fmtutil.DisplayZone).Format("01-02 15:04:05"))
	}
	if len(ms) > n {
		fmt.Print(t.UngroupedMore(len(ms) - n))
	}
}

// renderJourney renders one Journey, optionally appending the single-
// Journey LLM interpretation section when llmOpts.Addr is set — same
// dry-run/degrade contract compareJourneys' own LLM section follows: a
// dry run never leaves a stories/ directory behind, and a call
// failure only drops the LLM section, never fails the command.
func renderJourney(target *ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof profile.Profile, includePartial bool, outDir string, llmOpts llmCLIOptions, lang i18n.Lang, idx *story.StoryIndex) error {
	t := i18n.CLI(lang)
	chain := ctxgraph.ChainFrom(target, byIdx)
	partial := story.IsPartialHead(chain, firstPath)
	if partial && !includePartial {
		return fmt.Errorf("journey %s looks head-truncated — pass -include-partial to render it anyway", story.ID(chain))
	}
	j, err := story.BuildChain(chain, prof, lang)
	if err != nil {
		return err
	}
	j.Partial = partial
	m := story.ComputeMetrics(j)
	findings := story.ComputeFindings(j, lang)

	if llmOpts.Addr != "" && llmOpts.DryRun {
		pack := story.BuildSingleJourneyEvidencePack(j, m, findings, lang)
		chars := pack.EstimateChars()
		fmt.Printf("evidence pack: %d chars (~%d tokens estimated) — dry run, no request sent\n", chars, chars/4)
		return nil
	}

	storiesDir, err := ensureStoriesDir(outDir)
	if err != nil {
		return err
	}

	var llmSection string
	if llmOpts.Addr != "" {
		pack := story.BuildSingleJourneyEvidencePack(j, m, findings, lang)
		chars := pack.EstimateChars()
		fmt.Fprintf(os.Stderr, "calling %s (model=%s): evidence pack %d chars (~%d tokens estimated)\n", llmOpts.Addr, llmOpts.Model, chars, chars/4)
		res, err := story.Interpret(context.Background(), llmOpts.LLMOptions, pack, lang)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: LLM interpretation failed, report will not include it: %v\n", err)
		} else {
			// scope "": renderJourney's document only ever has one LLM
			// section (unlike -compare, there's no second, divergence-
			// scoped call to disambiguate it from).
			llmSection = story.RenderLLMSection(llmOpts.LLMOptions, res, lang, "")
		}
	}

	outPath, err := writeJourneyFile(j, m, findings, storiesDir, lang, llmSection)
	if err != nil {
		return err
	}
	fmt.Print(t.RenderedNote(outPath, len(j.Tasks), journeySteps(j)))
	updateJourneyRow(idx, j.ID, len(j.Tasks), journeySteps(j), filepath.Base(outPath))
	return saveStoryIndex(idx, outDir, lang)
}

// compareJourneys is Step 4's 4d module: resolve
// both id prefixes, build each Journey, diff their already-computed
// behavior profiles (story.Compare), and write the result as one Markdown +
// JSON pair — the same .md+.json convention writeJourneyFile uses for a
// single Journey. Either side being partial-head gates on -include-partial
// exactly like a single-journey render (an unstable ID is still unstable
// when it's one half of a comparison), and the output filename picks up the
// same "-partial" self-disclosure suffix if either side is.
func compareJourneys(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, idA, idB, firstPath string, prof profile.Profile, includePartial bool, outDir string, llmOpts llmCLIOptions, sources []string, lang i18n.Lang, idx *story.StoryIndex) error {
	_, chainA, err := resolveJourneyID(cands, byIdx, idA)
	if err != nil {
		return fmt.Errorf("-compare first id: %w", err)
	}
	_, chainB, err := resolveJourneyID(cands, byIdx, idB)
	if err != nil {
		return fmt.Errorf("-compare second id: %w", err)
	}
	partialA := story.IsPartialHead(chainA, firstPath)
	partialB := story.IsPartialHead(chainB, firstPath)
	if (partialA || partialB) && !includePartial {
		return fmt.Errorf("one or both journeys look head-truncated — pass -include-partial to compare them anyway")
	}

	jA, err := story.BuildChain(chainA, prof, lang)
	if err != nil {
		return err
	}
	jB, err := story.BuildChain(chainB, prof, lang)
	if err != nil {
		return err
	}
	sA, sB := story.Summarize(jA), story.Summarize(jB)
	cmp := story.Compare(sA, sB)
	extras := story.ComputeComparisonExtras(jA, jB, sA.Metrics, sB.Metrics)
	extras.Sources = sources
	cmp.Extras = &extras

	// -llm-dry-run: print the evidence-pack size estimate and return
	// immediately — deliberately checked BEFORE ensureStoriesDir below, so a
	// dry run never leaves so much as an empty reports/stories/ directory
	// behind (design doc C.7: "should I even run this" is a pure query, not
	// a partial run).
	if llmOpts.Addr != "" && llmOpts.DryRun {
		pack := story.BuildEvidencePack(jA, jB, cmp, lang)
		chars := pack.EstimateChars()
		fmt.Printf("evidence pack: %d chars (~%d tokens estimated) — dry run, no request sent\n", chars, chars/4)
		if extras.Divergence.Found {
			divPack := story.BuildDivergenceEvidencePack(jA, jB, extras.Divergence, lang)
			divChars := divPack.EstimateChars()
			fmt.Printf("divergence evidence pack: %d chars (~%d tokens estimated) — dry run, no request sent\n", divChars, divChars/4)
		}
		return nil
	}

	storiesDir, err := ensureStoriesDir(outDir)
	if err != nil {
		return err
	}

	// The compare-scoped LLM interpretation layer (internal/story/llm.go) —
	// entirely optional, switched on by -llm-addr alone.
	var llmSection string
	if llmOpts.Addr != "" {
		pack := story.BuildEvidencePack(jA, jB, cmp, lang)
		chars := pack.EstimateChars()
		fmt.Fprintf(os.Stderr, "calling %s (model=%s): evidence pack %d chars (~%d tokens estimated)\n", llmOpts.Addr, llmOpts.Model, chars, chars/4)
		res, err := story.Interpret(context.Background(), llmOpts.LLMOptions, pack, lang)
		if err != nil {
			// The LLM interpretation layer degrades away on failure —
			// this must never fail the -compare command itself.
			fmt.Fprintf(os.Stderr, "warning: LLM interpretation failed, report will not include it: %v\n", err)
		} else {
			// A non-"" scope: -compare's document can carry a second LLM
			// section below (the divergence-point-scoped call just below,
			// when one fires) — without a distinguishing label here, both
			// would render under the byte-identical heading "## LLM 解读
			// （模型：X）" with nothing in the outline to tell them apart.
			llmSection = story.RenderLLMSection(llmOpts.LLMOptions, res, lang, i18n.LLM(lang).ScopeOverall)
		}

		// A second, separately-cached LLM call scoped to just the
		// divergence point's own evidence window — only fired when
		// divergence-point detection actually found one (a no-divergence Comparison has nothing for
		// this section to interpret, and BuildDivergenceEvidencePack's own
		// contract is to return an empty pack in that case, which isn't
		// worth spending a call on).
		if extras.Divergence.Found {
			divPack := story.BuildDivergenceEvidencePack(jA, jB, extras.Divergence, lang)
			divChars := divPack.EstimateChars()
			fmt.Fprintf(os.Stderr, "calling %s (model=%s) for the divergence point: evidence pack %d chars (~%d tokens estimated)\n", llmOpts.Addr, llmOpts.Model, divChars, divChars/4)
			divRes, err := story.Interpret(context.Background(), llmOpts.LLMOptions, divPack, lang)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: divergence LLM interpretation failed, report will not include it: %v\n", err)
			} else {
				divSection := story.RenderLLMSection(llmOpts.LLMOptions, divRes, lang, i18n.LLM(lang).ScopeDivergence)
				if llmSection != "" {
					llmSection += "\n" + divSection
				} else {
					llmSection = divSection
				}
			}
		}
	}

	base := "compare-" + jA.ID + "-vs-" + jB.ID
	if partialA || partialB {
		base += "-partial"
	}
	mdPath := filepath.Join(storiesDir, base+".md")
	md := story.RenderComparisonMarkdown(cmp, lang)
	if llmSection != "" {
		md += "\n" + llmSection
	}
	if err := os.WriteFile(mdPath, []byte(md), 0o600); err != nil {
		return err
	}
	jsonPath := filepath.Join(storiesDir, base+".json")
	data, err := json.MarshalIndent(cmp, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, data, 0o600); err != nil {
		return err
	}
	fmt.Printf("%s\n", mdPath)
	updateJourneyRow(idx, jA.ID, len(jA.Tasks), journeySteps(jA), "")
	updateJourneyRow(idx, jB.ID, len(jB.Tasks), journeySteps(jB), "")
	return saveStoryIndex(idx, outDir, lang)
}

// renderJourneys renders every given candidate (skipping partial-head ones
// unless includePartial) in one batched pass — story.BuildAll shares a
// single FetchRecords call across every candidate (same fix PreviewTitles
// applied to the listing path), so this costs about the same I/O as just
// listing, not N times more. Shared by renderAllJourneys (-render-all,
// every candidate) and -journey's multi-match dispatch (a comma-list/glob
// selector that resolved to more than one journey) — the two differ only
// in which candidates they pass in and the message printed when none of
// them survive the partial-head filter.
func renderJourneys(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof profile.Profile, includePartial bool, outDir string, lang i18n.Lang, idx *story.StoryIndex, noneMsg string) error {
	var toRender [][]*ctxgraph.Lineage
	var toRenderPartial []bool
	skippedPartial := 0
	for _, l := range cands {
		chain := ctxgraph.ChainFrom(l, byIdx)
		partial := story.IsPartialHead(chain, firstPath)
		if partial && !includePartial {
			skippedPartial++
			continue
		}
		toRender = append(toRender, chain)
		toRenderPartial = append(toRenderPartial, partial)
	}
	if len(toRender) == 0 {
		fmt.Println(noneMsg)
		return saveStoryIndex(idx, outDir, lang)
	}

	journeys, err := story.BuildAll(toRender, prof, lang)
	if err != nil {
		return err
	}
	storiesDir, err := ensureStoriesDir(outDir)
	if err != nil {
		return err
	}
	t := i18n.CLI(lang)
	for i, j := range journeys {
		j.Partial = toRenderPartial[i]
		m := story.ComputeMetrics(j)
		findings := story.ComputeFindings(j, lang)
		outPath, err := writeJourneyFile(j, m, findings, storiesDir, lang, "")
		if err != nil {
			return err
		}
		fmt.Print(t.RenderedNote(outPath, len(j.Tasks), journeySteps(j)))
		updateJourneyRow(idx, j.ID, len(j.Tasks), journeySteps(j), filepath.Base(outPath))
	}
	if skippedPartial > 0 {
		fmt.Print(t.AllRenderedSkipped(skippedPartial))
	}
	fmt.Print(t.AllRenderedNote(len(journeys), storiesDir))
	return saveStoryIndex(idx, outDir, lang)
}

// renderAllJourneys renders every non-partial candidate journey — see
// renderJourneys.
func renderAllJourneys(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof profile.Profile, includePartial bool, outDir string, lang i18n.Lang, idx *story.StoryIndex) error {
	return renderJourneys(cands, byIdx, firstPath, prof, includePartial, outDir, lang, idx,
		"no candidate journeys to render (all skipped as partial-head; pass -include-partial)")
}

// corpusStats builds every non-partial candidate journey (same
// batched BuildAll path renderAllJourneys uses) and compute/write corpus-
// level statistics (vmr-story-corpus.md/.json) instead of per-Journey
// files. Journeys are built here only to feed ComputeCorpusStats — none of
// them are individually rendered or written to disk by this path.
func corpusStats(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof profile.Profile, includePartial bool, outDir string, lang i18n.Lang, idx *story.StoryIndex) error {
	var toRender [][]*ctxgraph.Lineage
	skippedPartial := 0
	for _, l := range cands {
		chain := ctxgraph.ChainFrom(l, byIdx)
		partial := story.IsPartialHead(chain, firstPath)
		if partial && !includePartial {
			skippedPartial++
			continue
		}
		toRender = append(toRender, chain)
	}
	if len(toRender) == 0 {
		fmt.Println("no candidate journeys to analyze (all skipped as partial-head; pass -include-partial)")
		return saveStoryIndex(idx, outDir, lang)
	}

	journeys, err := story.BuildAll(toRender, prof, lang)
	if err != nil {
		return err
	}
	stats := story.ComputeCorpusStats(journeys)

	storiesDir, err := ensureStoriesDir(outDir)
	if err != nil {
		return err
	}
	mdPath := filepath.Join(storiesDir, "vmr-story-corpus.md")
	if err := os.WriteFile(mdPath, []byte(story.RenderCorpusMarkdown(stats, lang)), 0o600); err != nil {
		return err
	}
	jsonPath := filepath.Join(storiesDir, "vmr-story-corpus.json")
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, data, 0o600); err != nil {
		return err
	}
	if skippedPartial > 0 {
		fmt.Printf("%d head-truncated journey(s) skipped (pass -include-partial to include them)\n", skippedPartial)
	}
	fmt.Printf("%d journey(s) analyzed → %s\n", len(journeys), mdPath)
	for _, j := range journeys {
		updateJourneyRow(idx, j.ID, len(j.Tasks), journeySteps(j), "")
	}
	return saveStoryIndex(idx, outDir, lang)
}

// ensureStoriesDir creates (if needed) and returns {outDir}/stories.
// 0o700: story output embeds full conversation bodies, same sensitivity
// as internal/report's details/ — must not loosen that.
func ensureStoriesDir(outDir string) (string, error) {
	storiesDir := filepath.Join(outDir, "stories")
	if err := os.MkdirAll(storiesDir, 0o700); err != nil {
		return "", err
	}
	return storiesDir, nil
}

// writeJourneyFile writes j's rendered Markdown plus its behavior-profile
// JSON (journey-<id>.json, consumed directly by the -compare
// comparison module) into storiesDir, and returns the Markdown path
// written. 0o600: same sensitivity note as ensureStoriesDir — the JSON
// carries token counts and tool-call args derived straight from the
// conversation body. m/findings are computed once by the caller (so a
// caller that also needs them for the single-Journey LLM evidence pack, i.e.
// renderJourney, doesn't pay for ComputeMetrics/ComputeFindings twice);
// llmSection, when non-empty, is appended to the Markdown after the main
// render — same "compute the LLM section first, append before writing"
// pattern compareJourneys already uses.
//
// A partial (head-truncated) Journey gets a "-partial"
// filename suffix — its ID is already unstable (it depends on whatever
// happened to be the earliest loaded manifest), so the suffix is cheap,
// visible self-disclosure that this file's beginning isn't the real
// beginning, without requiring the reader to open it and find the warning
// line first.
func writeJourneyFile(j *story.Journey, m story.Metrics, findings []story.Finding, storiesDir string, lang i18n.Lang, llmSection string) (string, error) {
	base := "journey-" + j.ID
	if j.Partial {
		base += "-partial"
	}
	outPath := filepath.Join(storiesDir, base+".md")
	md := story.RenderMarkdown(j, m, findings, lang)
	if llmSection != "" {
		md += "\n" + llmSection
	}
	if err := os.WriteFile(outPath, []byte(md), 0o600); err != nil {
		return "", err
	}
	jsonPath := filepath.Join(storiesDir, base+".json")
	// Summarize computes its own Metrics/Findings using i18n.EN — same
	// "always-English JSON, target-language Markdown" convention
	// report.buildFindingsForJSON already established; the duplicated
	// computation is pure in-memory work, not I/O (see
	// docs/VirtualModelRouter_Design_v4_Analytics.md's Findings section).
	data, err := json.MarshalIndent(story.Summarize(j), "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(jsonPath, data, 0o600); err != nil {
		return "", err
	}
	return outPath, nil
}

// journeySteps totals a Journey's steps across all its tasks.
func journeySteps(j *story.Journey) int {
	steps := 0
	for _, t := range j.Tasks {
		steps += len(t.Steps)
	}
	return steps
}
