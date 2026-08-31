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
	"sort"
	"strings"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/pricing"
	"vmr/internal/story"
	"vmr/internal/taskseg"
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

// cmdStory is `vmr story`'s own flag set (P15.2: unchanged from before the
// CLI convergence — same flags, same defaults, including -render-all always
// meaning "every candidate", with no P9.2/P14.1 category filtering — that
// filtering is a `vmr analyze`-only default, not a change to what
// -render-all itself means). It no longer dispatches on its own: it
// resolves its flags, then hands the result to dispatchAnalyze — the same
// function cmdAnalyze itself calls — so `vmr story`'s five call shapes
// (bare/-journey/-compare/-corpus/-render-all) can't drift from what `vmr
// analyze` does for the equivalent flags the way they once did:
// resolveLLMOptions is now called lazily, through the same
// resolveLLMOpts closure cmdAnalyze builds, instead of unconditionally up
// front — matching cmdAnalyze's own on-demand validation.
func cmdStory(args []string) error {
	fs := flag.NewFlagSet("story", flag.ExitOnError)
	configPath := fs.String("c", "config.yaml", "config file to resolve log_dir from, when no input files are given")
	outDirFlag := fs.String("o", "", "output directory (default: ./reports, or report.yaml's output)")
	journeyArg := fs.String("journey", "", "render this journey: an id or id-prefix, a comma-separated list of ids/prefixes/globs, and/or a shell-style glob (*, ?, [...]) matched against the full id — e.g. -journey j-a,j-b or -journey 'j-openclaw-*'. A selector resolving to exactly one journey renders as before (and alone supports -llm-addr); more than one batches like -render-all")
	renderAll := fs.Bool("render-all", false, "render every non-partial candidate journey in one batched pass, instead of picking one id at a time")
	compare := fs.String("compare", "", "compare two journeys' behavior profiles: -compare id1,id2 (each an id, id-prefix, or shell glob; first candidate matching each side wins)")
	corpus := fs.Bool("corpus", false, "compute corpus-level statistics (metric distributions, Finding hit rates, correlations) across every non-partial candidate journey")
	includePartialFlag := fs.Bool("include-partial", false, "also render journeys whose head looks truncated by the loaded file range (default: report.yaml's include_partial, or false)")
	showUngrouped := fs.Bool("show-ungrouped", false, "print the source location of the first few ungrouped records")
	llmAddrFlag := fs.String("llm-addr", "", "host:port of an already-running VMR instance — enables the optional LLM interpretation section on -journey's or -compare's report (-compare also adds a second, divergence-point-scoped section when one was detected; not supported with -render-all/-corpus). Never auto-started; the instance must already be up. Default: report.yaml's llm_addr")
	llmModelFlag := fs.String("llm-model", "", "that VMR instance's virtual model name (e.g. \"agent\"), sent verbatim — required with -llm-addr unless -llm-dry-run. Default: report.yaml's llm_model")
	llmKeyFlag := fs.String("llm-key", "", "bearer token for that VMR instance, only needed if it has api_keys configured. Default: report.yaml's llm_key (typically \"${SOME_ENV_VAR}\")")
	llmCacheDirFlag := fs.String("llm-cache-dir", "", "directory for the disk cache of LLM interpretation results; absent both here and in report.yaml's llm_cache_dir => no caching, ever (no implicit default path)")
	llmDryRun := fs.Bool("llm-dry-run", false, "with -llm-addr: print the evidence-pack size estimate and exit without calling anything")
	langFlag := fs.String("lang", "", "output language: en|zh (default: report.yaml's language, or en) — overrides report.yaml")
	reportConfigPath := fs.String("report-config", "", "vmr analyze's sidecar config yaml (shared with this alias); absent => auto-load ./report.yaml if present")
	includeSelfTraffic := fs.Bool("include-self-traffic", false, "don't exclude vmr analyze's own -llm-addr self-analysis traffic from the candidate journey list (default: excluded — see report.yaml's llm_key/self_traffic_client_tags)")
	htmlFlag := fs.Bool("html", false, "with a single-match -journey or with -compare: also write a self-contained .html dashboard next to the .md ({out}/stories/journey-<id>.html or compare-<a>-vs-<b>.html) — verdict/structure/metrics/findings for a journey, sides/divergence/diff/LLM for a comparison; inline CSS/JS, zero external requests. No effect on any other mode")
	redactFlag := fs.Bool("redact", false, "with -html: replace every conversation body with a '‹text: N chars›' length placeholder and drop the per-step detail links, finding text and (for -compare) the LLM section — structure, metrics, roles, token counts and tool names stay. For sharing outside the team")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *corpus && (*compare != "" || *journeyArg != "" || *renderAll) {
		return fmt.Errorf("-corpus is exclusive with -journey/-render-all/-compare — run it on its own")
	}
	// Same -html/-redact gate cmdAnalyze applies: -redact needs -html, and
	// both apply only to a single -journey or a -compare pair (the only two
	// shapes RenderHTML/RenderComparisonHTML cover).
	if *redactFlag && !*htmlFlag {
		return fmt.Errorf("-redact only applies with -html")
	}
	if (*htmlFlag || *redactFlag) && *journeyArg == "" && *compare == "" {
		return fmt.Errorf("-html/-redact only apply with -journey (a single journey) or -compare (a pair)")
	}
	paths, err := resolveInputPaths(fs, *configPath)
	if err != nil {
		return err
	}

	rc := resolveReportConfig(*reportConfigPath, os.Stdout)
	lang, err := resolveLanguage(*langFlag, rc, os.Stdout)
	if err != nil {
		return err
	}
	llmAddr := resolveStringExplicit(flagPassed(fs, "llm-addr"), *llmAddrFlag, rc.LLMAddr, "")
	llmModel := resolveString(*llmModelFlag, rc.LLMModel, "")
	llmKey := resolveString(*llmKeyFlag, rc.LLMKey, "")
	llmCacheDir := resolveString(*llmCacheDirFlag, rc.LLMCacheDir, "")
	llmAddrExplicit := flagPassed(fs, "llm-addr")
	hasSelector := *compare != "" || *journeyArg != ""
	// Gate on the resolved value: an explicit `-llm-addr ""` (suppress a
	// report.yaml llm_addr for this run) must pass; a configured llm_addr
	// with no flag stays silently ignored on these batch shapes.
	if llmAddrExplicit && llmAddr != "" && (*corpus || *renderAll || !hasSelector) {
		return fmt.Errorf("-llm-addr is not supported with -render-all, -corpus, or bare `vmr story` (would fire one LLM call per journey, or never be used at all) — use -journey to interpret one at a time, or -compare for a pairwise interpretation")
	}

	// -journey/-compare/-corpus/-render-all all carry over to `vmr analyze`
	// unchanged (same flag, same output). Bare `vmr story` (no selector) is
	// the one case that does NOT — it lists candidates only, while bare
	// `vmr analyze` renders the default suite — so the hint calls that out
	// explicitly rather than implying a blanket 1:1 swap (independent
	// review, 2026-08-21 — see this file's P9 ActionPlan §4.3's "执行记录").
	// `-list-only` (P15.1) is bare `vmr story`'s real equivalent.
	fmt.Fprintln(os.Stderr, "vmr story: alias for `vmr analyze` with -list-only (bare) or -journey/-compare/-corpus/-render-all (unchanged) — kept for muscle memory, produces byte-identical output. See `vmr analyze -h`.")

	return dispatchAnalyze(&analyzeRun{
		paths:              paths,
		configPath:         *configPath,
		outDir:             resolveString(*outDirFlag, rc.Output, "reports"),
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
		corpusFlag:    *corpus,
		compareArg:    *compare,
		journeyArg:    *journeyArg,
		renderAllFlag: *renderAll,
		listOnly:      !hasSelector && !*corpus && !*renderAll,
		// storyOnly (a real cmdAnalyze flag, -story-only — see analyzeRun's
		// own doc comment): `vmr story -render-all` alone never ran the
		// report half, and `vmr analyze -story-only -render-all` is now its
		// exact, publicly-reachable equivalent — not an internal-only field
		// only this forwarder could set.
		storyOnly:       *renderAll && !hasSelector,
		selfTrafficTags: rc.SelfTrafficClientTags,
		showUngrouped:   *showUngrouped,
		htmlOn:          *htmlFlag,
		redactOn:        *redactFlag,
	})
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
// (creating it if needed), plus this run's parse cache into
// {outDir}/.parse-cache (shared with `vmr report` — see cmd_report.go) —
// called at every branch's normal (non-dry-run) exit point, so `vmr story`
// leaves this triple behind regardless of which flags were passed, per the
// design doc's vmr-stories.json section.
func saveStoryIndex(idx *story.StoryIndex, outDir string, lang i18n.Lang) error {
	storiesDir, err := ensureStoriesDir(outDir)
	if err != nil {
		return err
	}
	if err := idx.Save(filepath.Join(storiesDir, "vmr-stories.json")); err != nil {
		return err
	}
	md := story.RenderStoryIndexMarkdown(idx.Journeys, lang)
	if err := os.WriteFile(filepath.Join(storiesDir, "vmr-stories.md"), []byte(md), 0o600); err != nil {
		return err
	}
	return ctxgraph.SaveCacheDir(filepath.Join(outDir, ".parse-cache"), idx.Cache)
}

// resolveJourneyID finds the candidate chain whose ID (the content-addressed
// j-<client>-<start>-<end>-<code>) matches pat — a shell-style glob, or (absent
// any glob character) an id prefix, per journeyPatternMatches. Used by -compare,
// which needs exactly one match per side and keeps a "first match in candidate
// order wins" contract (as printed by running with no selector flag at all);
// -journey's set-valued selector goes through resolveJourneySelector instead.
func resolveJourneyID(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, pat string) (*ctxgraph.Lineage, []*ctxgraph.Lineage, error) {
	for _, l := range cands {
		chain := ctxgraph.ChainFrom(l, byIdx)
		if journeyPatternMatches(story.ID(chain), pat) {
			return l, chain, nil
		}
	}
	return nil, nil, fmt.Errorf("no journey matching %q (run without -journey to list candidates)", pat)
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
func renderJourney(target *ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof taskseg.Profile, includePartial bool, outDir string, llmOpts llmCLIOptions, lang i18n.Lang, idx *story.StoryIndex, priceRes *pricing.Resolver, ccy string, htmlOn, redactOn bool) error {
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
	var llmFindings []story.Finding
	if llmOpts.Addr != "" {
		if findingsLLM, err := story.ComputeLLMFindings(context.Background(), j, llmOpts.LLMOptions, lang); err == nil && len(findingsLLM) > 0 {
			llmFindings = findingsLLM
			findings = append(findings, llmFindings...)
			sort.SliceStable(findings, func(a, b int) bool {
				if findings[a].StepSeq != findings[b].StepSeq {
					return findings[a].StepSeq < findings[b].StepSeq
				}
				return findings[a].Code < findings[b].Code
			})
		}
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

	cost := story.ComputeJourneyCost(j, priceRes, ccy)

	detailDir, evidenceDir := detailAndEvidenceDirs(outDir)
	// true: a single named -journey target, not a batch scope (P13.1).
	outPath, err := writeJourneyFile(j, m, findings, storiesDir, lang, llmSection, llmFindings, prof, detailDir, evidenceDir, &cost, true, nil)
	if err != nil {
		return err
	}
	fmt.Print(t.RenderedNote(outPath, len(j.Tasks), journeySteps(j)))
	if htmlOn {
		htmlPath := filepath.Join(storiesDir, "journey-"+j.ID+".html")
		if j.Partial {
			htmlPath = filepath.Join(storiesDir, "journey-"+j.ID+"-partial.html")
		}
		// 0600: same sensitivity as the .md — the redacted variant still
		// keeps structure and metrics, but the un-redacted one carries full
		// conversation bodies.
		if err := os.WriteFile(htmlPath, []byte(story.RenderHTML(j, m, findings, cost, lang, redactOn)), 0o600); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", htmlPath)
	}
	updateJourneyRow(idx, j.ID, len(j.Tasks), journeySteps(j), filepath.Base(outPath))
	return saveStoryIndex(idx, outDir, lang)
}

// compareJourneys is Differential analysis: resolve
// both id prefixes, build each Journey, diff their already-computed
// behavior profiles (story.Compare), and write the result as one Markdown +
// JSON pair — the same .md+.json convention writeJourneyFile uses for a
// single Journey. Either side being partial-head gates on -include-partial
// exactly like a single-journey render (an unstable ID is still unstable
// when it's one half of a comparison), and the output filename picks up the
// same "-partial" self-disclosure suffix if either side is.
func compareJourneys(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, idA, idB, firstPath string, prof taskseg.Profile, includePartial bool, outDir string, llmOpts llmCLIOptions, lang i18n.Lang, idx *story.StoryIndex, priceRes *pricing.Resolver, ccy string, htmlOn, redactOn bool) error {
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
	sA, sB := story.Summarize(jA, lang), story.Summarize(jB, lang)
	cmp := story.Compare(sA, sB, lang)
	extras := story.ComputeComparisonExtras(jA, jB, sA.Metrics, sB.Metrics, priceRes, ccy)
	extras.Sources = story.SourceFiles(idx, jA.ID, jB.ID)
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

	detailDir, evidenceDir := detailAndEvidenceDirs(outDir)
	// Each side's own journey-<id>.json picks up the same per-side cost the
	// tale-of-the-tape shows, computed off cmp.Extras.Cost rather than a
	// second ComputeJourneyCost pass.
	costA, costB := extras.Cost.A, extras.Cost.B
	if err := ensureJourneyFile(jA, storiesDir, lang, prof, detailDir, evidenceDir, &costA); err != nil {
		return err
	}
	if err := ensureJourneyFile(jB, storiesDir, lang, prof, detailDir, evidenceDir, &costB); err != nil {
		return err
	}

	llmSection, llmResult := compareLLMSections(jA, jB, cmp, extras, llmOpts, lang)

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
	if htmlOn {
		htmlPath := filepath.Join(storiesDir, base+".html")
		// 0600: same sensitivity as the .md — the un-redacted variant carries
		// full excerpt text.
		if err := os.WriteFile(htmlPath, []byte(story.RenderComparisonHTML(cmp, llmResult, lang, redactOn)), 0o600); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", htmlPath)
	}
	updateJourneyRow(idx, jA.ID, len(jA.Tasks), journeySteps(jA), journeyBaseName(jA)+".md")
	updateJourneyRow(idx, jB.ID, len(jB.Tasks), journeySteps(jB), journeyBaseName(jB)+".md")
	return saveStoryIndex(idx, outDir, lang)
}

// compareLLMSections runs the overall and divergence LLM interpretation
// calls for -compare, degrading gracefully on failure without failing the
// command. Returns both the Markdown section (for the .md report) and the
// structured result (for the .html dashboard) from the same two calls.
func compareLLMSections(jA, jB *story.Journey, cmp story.Comparison, extras story.ComparisonExtras, llmOpts llmCLIOptions, lang i18n.Lang) (string, story.CompareLLMResult) {
	result := story.CompareLLMResult{Model: llmOpts.Model}
	if llmOpts.Addr == "" {
		return "", result
	}
	var llmSection string
	pack := story.BuildEvidencePack(jA, jB, cmp, lang)
	chars := pack.EstimateChars()
	fmt.Fprintf(os.Stderr, "calling %s (model=%s): evidence pack %d chars (~%d tokens estimated)\n", llmOpts.Addr, llmOpts.Model, chars, chars/4)
	res, err := story.Interpret(context.Background(), llmOpts.LLMOptions, pack, lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: LLM interpretation failed, report will not include it: %v\n", err)
	} else {
		result.Overall = res
		llmSection = story.RenderLLMSection(llmOpts.LLMOptions, res, lang, i18n.LLM(lang).ScopeOverall)
	}

	if extras.Divergence.Found {
		divPack := story.BuildDivergenceEvidencePack(jA, jB, extras.Divergence, lang)
		divChars := divPack.EstimateChars()
		fmt.Fprintf(os.Stderr, "calling %s (model=%s) for the divergence point: evidence pack %d chars (~%d tokens estimated)\n", llmOpts.Addr, llmOpts.Model, divChars, divChars/4)
		divRes, err := story.Interpret(context.Background(), llmOpts.LLMOptions, divPack, lang)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: divergence LLM interpretation failed, report will not include it: %v\n", err)
		} else {
			result.Divergence, result.DivergenceUsed = divRes, true
			divSection := story.RenderLLMSection(llmOpts.LLMOptions, divRes, lang, i18n.LLM(lang).ScopeDivergence)
			if llmSection != "" {
				llmSection += "\n" + divSection
			} else {
				llmSection = divSection
			}
		}
	}
	return llmSection, result
}

// renderJourneys renders every given candidate (skipping partial-head ones
// unless includePartial), in byte-budgeted batches (see
// renderBatchBudgetBytes for why). Shared by renderAllJourneys (-render-all/the
// default suite's category=task scope, P9.2) and -journey's multi-match
// dispatch (a comma-list/glob selector that resolved to more than one
// journey) — the two differ only in which candidates they pass in, the
// message printed when none of them survive the partial-head filter, and
// (P13.1) materializeDetails: a -journey selector is still a user-named
// target set even when it resolves to more than one match, so both of
// this function's callers in cmd_story.go/cmd_analyze.go pass true for it;
// only renderAllJourneys' own default-suite caller (not -render-all) ever
// passes false — see writeJourneyFile's doc comment for what false means.
func renderJourneys(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof taskseg.Profile, includePartial bool, outDir string, lang i18n.Lang, idx *story.StoryIndex, noneMsg string, materializeDetails bool) error {
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

	storiesDir, err := ensureStoriesDir(outDir)
	if err != nil {
		return err
	}
	t := i18n.CLI(lang)
	detailDir, evidenceDir := detailAndEvidenceDirs(outDir)
	rendered := 0
	for _, br := range batchByBytes(toRender, renderBatchBudgetBytes) {
		start, end := br[0], br[1]
		journeys, batchRecs, err := story.BuildAllWithRecords(toRender[start:end], prof, lang)
		if err != nil {
			return err
		}
		for i, j := range journeys {
			j.Partial = toRenderPartial[start+i]
			m := story.ComputeMetrics(j)
			findings := story.ComputeFindings(j, lang)
			// Batch renders (default suite, -render-all, multi-match -journey)
			// carry no cost: pricing resolution is a zoom-in feature, same as
			// -html itself — only single -journey / -compare thread a resolver.
			// batchRecs: EnsureJourneyDetails reuses this batch's already-
			// decompressed records instead of re-reading the source files.
			outPath, err := writeJourneyFile(j, m, findings, storiesDir, lang, "", nil, prof, detailDir, evidenceDir, nil, materializeDetails, batchRecs)
			if err != nil {
				return err
			}
			fmt.Print(t.RenderedNote(outPath, len(j.Tasks), journeySteps(j)))
			updateJourneyRow(idx, j.ID, len(j.Tasks), journeySteps(j), filepath.Base(outPath))
		}
		rendered += len(journeys)
	}
	if skippedPartial > 0 {
		fmt.Print(t.AllRenderedSkipped(skippedPartial))
	}
	fmt.Print(t.AllRenderedNote(rendered, storiesDir))
	return saveStoryIndex(idx, outDir, lang)
}

// renderAllJourneys renders every non-partial candidate journey — see
// renderJourneys. materializeDetails distinguishes an explicit
// "render everything, details included" ask (vmr story -render-all, or
// vmr analyze -render-all) from the default suite's implicit
// category=task batch (cmd_analyze.go's dispatchAnalyze passes false
// there) — the latter is exactly the unbounded-materialization case to
// avoid (238+ candidates' worth of Step detail pages written on every run
// whether or not anyone reads them).
func renderAllJourneys(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof taskseg.Profile, includePartial bool, outDir string, lang i18n.Lang, idx *story.StoryIndex, materializeDetails bool) error {
	return renderJourneys(cands, byIdx, firstPath, prof, includePartial, outDir, lang, idx,
		"no candidate journeys to render (all skipped as partial-head; pass -include-partial)", materializeDetails)
}

// corpusStats builds every non-partial candidate journey (same
// batched BuildAll path renderAllJourneys uses) and compute/write corpus-
// level statistics (vmr-story-corpus.md/.json) instead of per-Journey
// files. Journeys are built here only to feed ComputeCorpusStats — none of
// them are individually rendered or written to disk by this path.
func corpusStats(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof taskseg.Profile, includePartial bool, outDir string, lang i18n.Lang, idx *story.StoryIndex) error {
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

	// Build in byte-budgeted batches (same bound renderJourneys uses): each
	// batch's records are released before the next fetch, but the built
	// Journeys are ~1% of that and all accumulate cheaply — the corpus
	// stats need every one of them at once, and 586 of them is ~300 MB.
	var journeys []*story.Journey
	for _, br := range batchByBytes(toRender, renderBatchBudgetBytes) {
		js, err := story.BuildAll(toRender[br[0]:br[1]], prof, lang)
		if err != nil {
			return err
		}
		journeys = append(journeys, js...)
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

// detailAndEvidenceDirs returns {outDir}/details and {outDir}/evidence — the
// same layout internal/report's DetailWriter uses (setupDetailWriter/
// NewDetailWriter in cmd_report.go), shared so a detail page materialized
// by either command is reachable at the same path and a decision spine's
// "→ detail" link (P5.2, story.EnsureJourneyDetails) resolves regardless of
// which command wrote it first.
func detailAndEvidenceDirs(outDir string) (detailDir, evidenceDir string) {
	return filepath.Join(outDir, "details"), filepath.Join(outDir, "evidence")
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

// journeyBaseName returns the base filename (without .md/.json extension)
// for j - the stem shared by both artifacts, derived from story's
// JourneyReportFile (the single naming source of truth) by dropping the
// canonical .md extension.
func journeyBaseName(j *story.Journey) string {
	return strings.TrimSuffix(story.JourneyReportFile(j.ID, j.Partial), ".md")
}

// ensureJourneyFile (re)writes j's journey report (.md + .json) and
// materializes its Step detail pages, so running `vmr analyze -compare`
// directly also produces the individual journey reports with working
// links. Unconditionally re-renders even when journey-<id>.md already
// exists: the default suite (materializeDetails=false) can have written
// this exact file with inline coordinates and no materialized details, and
// -compare naming that same journey is a user-named target that must get
// the linked form (P13.1 → review §12.5's 12-B). EnsureJourneyDetails and
// the re-render are both cheap here — EnsureRendered's fingerprint check
// (P12) makes an already-materialized Step a fast skip, and RenderMarkdown
// is a pure string build.
func ensureJourneyFile(j *story.Journey, storiesDir string, lang i18n.Lang, prof taskseg.Profile, detailDir, evidenceDir string, cost *story.CostFact) error {
	m := story.ComputeMetrics(j)
	findings := story.ComputeFindings(j, lang)
	// true: both -compare sides are user-named targets, same as a single
	// -journey render (P13.1) — not a batch scope.
	_, err := writeJourneyFile(j, m, findings, storiesDir, lang, "", nil, prof, detailDir, evidenceDir, cost, true, nil)
	return err
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
//
// Before rendering, EnsureJourneyDetails materializes this Journey's own
// Step detail pages (and system-prompt/tool evidence blobs) under
// detailDir/evidenceDir — P5.2's "渲染时目标缺失即按需补生成": the decision
// spine's "→ detail" links and the system-prompt header's evidence links
// must resolve without requiring the caller to have separately run
// `vmr report -details` first. materializeDetails gates this (P13.1): a
// user-named target (single -journey, either -compare side) always passes
// true; a batch render (-journey matching several, the default suite, or
// -render-all) decides per-caller — see renderJourneys/renderAllJourneys'
// own doc comments. When false, RenderMarkdown's linkDetails is false too
// (see the call below): the spine renders each Step's "→ detail" pointer as
// an inline `file:line` coordinate instead of a link, so an unmaterialized
// detail page is never linked (B10 / review §12.5's 12-B).
func writeJourneyFile(j *story.Journey, m story.Metrics, findings []story.Finding, storiesDir string, lang i18n.Lang, llmSection string, llmFindings []story.Finding, prof taskseg.Profile, detailDir, evidenceDir string, cost *story.CostFact, materializeDetails bool, recs map[ctxgraph.Loc]*audit.Record) (string, error) {
	base := journeyBaseName(j)
	outPath := filepath.Join(storiesDir, base+".md")
	if materializeDetails {
		story.EnsureJourneyDetails(os.Stderr, j, recs, detailDir, evidenceDir, prof, lang)
	}
	_, reportMDErr := os.Stat(filepath.Join(filepath.Dir(storiesDir), "vmr-report.md"))
	// linkDetails == materializeDetails: this call just wrote (or skipped as
	// already-current) this Journey's detail pages iff materializeDetails, so
	// the spine's "→ detail" pointers link only when they will resolve; the
	// default batch suite (materializeDetails=false) gets inline coordinates
	// instead of 404 links (P13.1 / B10 / review §12.5).
	md := story.RenderMarkdown(j, m, findings, lang, reportMDErr == nil, materializeDetails, cost)
	if llmSection != "" {
		md += "\n" + llmSection
	}
	if err := os.WriteFile(outPath, []byte(md), 0o600); err != nil {
		return "", err
	}
	jsonPath := filepath.Join(storiesDir, base+".json")
	summary := story.NewJourneySummary(j, m, findings, llmFindings, cost)
	data, err := json.MarshalIndent(summary, "", "  ")
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
