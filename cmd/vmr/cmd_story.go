// Ver 2026-08-01, by Sonnet 5

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
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
	outDir := fs.String("o", "reports", "output directory (default: ./reports)")
	journeyArg := fs.String("journey", "", "render this journey (id or id prefix, as printed by running with no -journey)")
	renderAll := fs.Bool("render-all", false, "render every non-partial candidate journey in one batched pass, instead of picking one id at a time")
	compare := fs.String("compare", "", "compare two journeys' behavior profiles: -compare id1,id2 (each an id or id prefix)")
	corpus := fs.Bool("corpus", false, "compute corpus-level statistics (metric distributions, Finding hit rates, correlations) across every non-partial candidate journey")
	includePartial := fs.Bool("include-partial", false, "also list/render journeys whose head looks truncated by the loaded file range")
	showUngrouped := fs.Bool("show-ungrouped", false, "print the source location of the first few ungrouped records")
	llmAddr := fs.String("llm-addr", "", "host:port of an already-running VMR instance — enables the optional LLM interpretation section on -journey's or -compare's report (-compare also adds a second, divergence-point-scoped section when one was detected; not supported with -render-all/-corpus). Never auto-started; the instance must already be up")
	llmModel := fs.String("llm-model", "", "that VMR instance's virtual model name (e.g. \"agent\"), sent verbatim — required with -llm-addr unless -llm-dry-run")
	llmKey := fs.String("llm-key", "", "bearer token for that VMR instance, only needed if it has api_keys configured")
	llmDryRun := fs.Bool("llm-dry-run", false, "with -llm-addr: print the evidence-pack size estimate and exit without calling anything")
	langFlag := fs.String("lang", "", "output language: en|zh (default: report.yaml's language, or en) — overrides report.yaml")
	reportConfigPath := fs.String("report-config", "", "vmr report/vmr story sidecar config yaml; absent => auto-load ./report.yaml if present")
	if err := fs.Parse(args); err != nil {
		return err
	}
	lang, err := resolveLanguage(*langFlag, *reportConfigPath, os.Stdout)
	if err != nil {
		return err
	}
	llmOpts, err := resolveLLMOptions(*llmAddr, *llmModel, *llmKey, *llmDryRun)
	if err != nil {
		return err
	}
	if llmOpts.Addr != "" && (*renderAll || *corpus) {
		return fmt.Errorf("-llm-addr is not supported with -render-all or -corpus (would fire one LLM call per journey) — use -journey to interpret one at a time")
	}
	if *corpus && (*compare != "" || *journeyArg != "" || *renderAll) {
		return fmt.Errorf("-corpus is exclusive with -journey/-render-all/-compare — run it on its own")
	}
	paths, err := resolveInputPaths(fs, *configPath)
	if err != nil {
		return err
	}

	fmt.Printf("scanning %d file(s)...\n", len(paths))
	g, err := ctxgraph.Scan(paths)
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

	if *compare != "" {
		ids := strings.Split(*compare, ",")
		if len(ids) != 2 || ids[0] == "" || ids[1] == "" {
			return fmt.Errorf("-compare wants exactly two comma-separated ids: -compare id1,id2")
		}
		return compareJourneys(cands, byIdx, ids[0], ids[1], firstPath, prof, *includePartial, *outDir, llmOpts, paths, lang)
	}
	if *corpus {
		return corpusStats(cands, byIdx, firstPath, prof, *includePartial, *outDir, lang)
	}
	if *journeyArg != "" {
		target, _, err := resolveJourneyID(cands, byIdx, *journeyArg)
		if err != nil {
			return err
		}
		return renderJourney(target, byIdx, firstPath, prof, *includePartial, *outDir, llmOpts, lang)
	}
	if *renderAll {
		return renderAllJourneys(cands, byIdx, firstPath, prof, *includePartial, *outDir, lang)
	}
	return listJourneys(cands, byIdx, g, firstPath, prof, *includePartial, lang)
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

func listJourneys(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, g *ctxgraph.Graph, firstPath string, prof profile.Profile, includePartial bool, lang i18n.Lang) error {
	t := i18n.CLI(lang)
	excluded := len(g.Lineages) - len(cands)
	fmt.Printf("%d candidate journey(s) (%d total lineage(s), %d single-request/scheduled excluded or absorbed into a stitched chain):\n\n", len(cands), len(g.Lineages), excluded)

	type row struct {
		l       *ctxgraph.Lineage
		chain   []*ctxgraph.Lineage
		partial bool
	}
	var toShow []row
	skippedPartial := 0
	for _, l := range cands {
		chain := ctxgraph.ChainFrom(l, byIdx)
		partial := story.IsPartialHead(chain, firstPath)
		if partial && !includePartial {
			skippedPartial++
			continue
		}
		toShow = append(toShow, row{l, chain, partial})
	}

	// One batched fetch across all candidates instead of one per chain —
	// story.PreviewTitles groups the underlying reads by source file, so
	// this scans each file at most once no matter how many candidate
	// chains are rooted in it.
	chains := make([][]*ctxgraph.Lineage, len(toShow))
	for i, r := range toShow {
		chains[i] = r.chain
	}
	titles, err := story.PreviewTitles(chains, prof, lang)
	if err != nil {
		return err
	}

	for _, r := range toShow {
		mark := ""
		if r.partial {
			mark = t.HeadTruncatedMark
		}
		if len(r.chain) > 1 {
			mark += t.StitchedMark(len(r.chain))
		}
		head, tail := r.chain[0], r.chain[len(r.chain)-1]
		first, last := head.Manifests[0], tail.Manifests[len(tail.Manifests)-1]
		steps := 0
		for _, cl := range r.chain {
			steps += len(cl.Manifests)
		}
		fmt.Print(t.ListLine(story.ID(r.chain), mark, steps, first.TS.In(fmtutil.DisplayZone).Format("01-02 15:04"), last.TS.In(fmtutil.DisplayZone).Format("15:04"), titles[r.l]))
	}
	if skippedPartial > 0 {
		fmt.Print(t.SkippedPartialNote(skippedPartial))
	}
	fmt.Print(t.RenderHint)
	return nil
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
func renderJourney(target *ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof profile.Profile, includePartial bool, outDir string, llmOpts llmCLIOptions, lang i18n.Lang) error {
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
		llmOpts.CacheDir = filepath.Join(storiesDir, ".llm-cache")
		res, err := story.Interpret(context.Background(), llmOpts.LLMOptions, pack, lang)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: LLM interpretation failed, report will not include it: %v\n", err)
		} else {
			llmSection = story.RenderLLMSection(llmOpts.LLMOptions, res, lang)
		}
	}

	outPath, err := writeJourneyFile(j, m, findings, storiesDir, lang, llmSection)
	if err != nil {
		return err
	}
	fmt.Print(t.RenderedNote(outPath, len(j.Tasks), journeySteps(j)))
	return nil
}

// compareJourneys is Step 4's 4d module: resolve
// both id prefixes, build each Journey, diff their already-computed
// behavior profiles (story.Compare), and write the result as one Markdown +
// JSON pair — the same .md+.json convention writeJourneyFile uses for a
// single Journey. Either side being partial-head gates on -include-partial
// exactly like a single-journey render (an unstable ID is still unstable
// when it's one half of a comparison), and the output filename picks up the
// same "-partial" self-disclosure suffix if either side is.
func compareJourneys(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, idA, idB, firstPath string, prof profile.Profile, includePartial bool, outDir string, llmOpts llmCLIOptions, sources []string, lang i18n.Lang) error {
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
		llmOpts.CacheDir = filepath.Join(storiesDir, ".llm-cache")

		pack := story.BuildEvidencePack(jA, jB, cmp, lang)
		chars := pack.EstimateChars()
		fmt.Fprintf(os.Stderr, "calling %s (model=%s): evidence pack %d chars (~%d tokens estimated)\n", llmOpts.Addr, llmOpts.Model, chars, chars/4)
		res, err := story.Interpret(context.Background(), llmOpts.LLMOptions, pack, lang)
		if err != nil {
			// The LLM interpretation layer degrades away on failure —
			// this must never fail the -compare command itself.
			fmt.Fprintf(os.Stderr, "warning: LLM interpretation failed, report will not include it: %v\n", err)
		} else {
			llmSection = story.RenderLLMSection(llmOpts.LLMOptions, res, lang)
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
				divSection := story.RenderLLMSection(llmOpts.LLMOptions, divRes, lang)
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
	return nil
}

// renderAllJourneys renders every non-partial candidate in one batched
// pass instead of requiring -journey <id> one at a time — story.BuildAll
// shares a single FetchRecords call across every candidate (same fix
// PreviewTitles applied to the listing path), so this costs about the same
// I/O as just listing, not N times more.
func renderAllJourneys(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof profile.Profile, includePartial bool, outDir string, lang i18n.Lang) error {
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
		fmt.Println("no candidate journeys to render (all skipped as partial-head; pass -include-partial)")
		return nil
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
	}
	if skippedPartial > 0 {
		fmt.Print(t.AllRenderedSkipped(skippedPartial))
	}
	fmt.Print(t.AllRenderedNote(len(journeys), storiesDir))
	return nil
}

// corpusStats builds every non-partial candidate journey (same
// batched BuildAll path renderAllJourneys uses) and compute/write corpus-
// level statistics (vmr-story-corpus.md/.json) instead of per-Journey
// files. Journeys are built here only to feed ComputeCorpusStats — none of
// them are individually rendered or written to disk by this path.
func corpusStats(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof profile.Profile, includePartial bool, outDir string, lang i18n.Lang) error {
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
		return nil
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
	return nil
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
