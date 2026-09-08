// Ver 2026-08-01, by Sonnet 5

package main

import (
	"context"
	"encoding/json"
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
	"vmr/internal/journey"
	"vmr/internal/pricing"
	"vmr/internal/taskseg"
)

// llmCLIOptions bundles the -llm-* flags after validation — a thin CLI-level
// wrapper around journey.LLMOptions that also carries -llm-dry-run (a command-
// behavior switch compareJourneys itself acts on, not something
// journey.Interpret needs to know about).
type llmCLIOptions struct {
	journey.LLMOptions
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
	return llmCLIOptions{LLMOptions: journey.LLMOptions{Addr: addr, Model: model, APIKey: key}, DryRun: dryRun}, nil
}

// updateJourneyRow finds id's row in idx.Journeys and fills in the
// full-Journey-only fields (only known once journey.BuildChain has actually
// run) — a no-op if id isn't present (shouldn't happen: every id passed
// here was itself resolved from idx's own candidate set moments earlier).
func updateJourneyRow(idx *journey.JourneyIndex, id string, tasks, steps int, rendered string) {
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

// saveJourneyIndex writes index.json + index.md into journeysDir
// (creating it if needed), plus this run's parse cache into
// {outDir}/.cache/parse (shared with report half — see cmd_report.go).
func saveJourneyIndex(idx *journey.JourneyIndex, outDir string, lang i18n.Lang) error {
	journeysDir, err := ensureJourneysDir(outDir)
	if err != nil {
		return err
	}
	if idx != nil && len(idx.Journeys) > 1 {
		idx.Clusters = journey.ComputeTaskClusters(idx.Journeys)
	}
	indexPath := filepath.Join(journeysDir, "index.json")
	if err := idx.Save(indexPath); err != nil {
		return err
	}
	diskIdx := journey.LoadJourneyIndex(indexPath)
	md := journey.RenderJourneyIndexMarkdown(diskIdx, lang)
	if err := os.WriteFile(filepath.Join(journeysDir, "index.md"), []byte(md), 0o600); err != nil {
		return err
	}
	return ctxgraph.SaveCacheDir(filepath.Join(outDir, ".cache", "parse"), idx.Cache)
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
		if journeyPatternMatches(journey.ID(chain), pat) {
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
// chronological listing) order, same order -render-all/-benchmark already
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
// as of the journeys/index.json change, also persists it — idx's rows already
// carry everything this needs (id, mark info, request count, time range,
// title), computed once and shared with the index, so this
// function no longer touches ctxgraph/journey.PreviewTitles itself.
func listJourneys(idx *journey.JourneyIndex, g *ctxgraph.Graph, outDir string, includePartial bool, lang i18n.Lang) error {
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
	return saveJourneyIndex(idx, outDir, lang)
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
// dry run never leaves a journeys/ directory behind, and a call
// failure only drops the LLM section, never fails the command.
func renderJourney(target *ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof taskseg.Profile, includePartial bool, outDir string, llmOpts llmCLIOptions, lang i18n.Lang, idx *journey.JourneyIndex, priceRes *pricing.Resolver, ccy string) error {
	t := i18n.CLI(lang)
	chain := ctxgraph.ChainFrom(target, byIdx)
	partial := journey.IsPartialHead(chain, firstPath)
	if partial && !includePartial {
		return fmt.Errorf("journey %s looks head-truncated — pass -include-partial to render it anyway", journey.ID(chain))
	}
	j, err := journey.BuildChain(chain, prof, lang)
	if err != nil {
		return err
	}
	j.Partial = partial
	m := journey.ComputeMetrics(j)
	findings := journey.ComputeFindings(j, lang)

	if llmOpts.Addr != "" && llmOpts.DryRun {
		// Every pack the run would send — each detector whose candidate
		// filter fires, plus the always-sent interpretation — not just the
		// interpretation pack (R90: the old single-pack estimate understated
		// the call count by up to 7x).
		fmt.Print(journey.FormatLLMDryRun(journey.EstimateLLMDryRun(j, m, findings, lang)))
		return nil
	}

	journeysDir, err := ensureJourneysDir(outDir)
	if err != nil {
		return err
	}

	var llmInterp *journey.LLMInterpretation
	var llmFindings []journey.Finding
	if llmOpts.Addr != "" {
		if findingsLLM, err := journey.ComputeLLMFindings(context.Background(), j, llmOpts.LLMOptions, lang); err == nil && len(findingsLLM) > 0 {
			llmFindings = findingsLLM
			findings = append(findings, llmFindings...)
			sort.SliceStable(findings, func(a, b int) bool {
				if findings[a].StepSeq != findings[b].StepSeq {
					return findings[a].StepSeq < findings[b].StepSeq
				}
				return findings[a].Code < findings[b].Code
			})
		}
		pack := journey.BuildSingleJourneyEvidencePack(j, m, findings, lang)
		chars := pack.EstimateChars()
		fmt.Fprintf(os.Stderr, "calling %s (model=%s): evidence pack %d chars (~%d tokens estimated)\n", llmOpts.Addr, llmOpts.Model, chars, chars/4)
		res, err := journey.Interpret(context.Background(), llmOpts.LLMOptions, pack, lang)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: LLM interpretation failed, report will not include it: %v\n", err)
		}
		// Recorded even on failure (status "failed") — the persisted record
		// is what j-<id>.json and the .md's rendered section both come from
		// (§3.6), and a failed attempt is worth seeing in the JSON too.
		// scope "": this document only ever has one LLM section (unlike
		// -compare, there's no second, divergence-scoped call).
		llmInterp = journey.NewLLMInterpretation(llmOpts.LLMOptions, res, err, "")
	}

	cost := journey.ComputeJourneyCost(j, priceRes, ccy)

	detailDir, evidenceDir := detailAndEvidenceDirs(outDir)
	// true: a single named -journey target, not a batch scope (P13.1).
	outPath, err := writeJourneyFile(j, m, findings, journeysDir, lang, llmInterp, llmFindings, prof, detailDir, evidenceDir, &cost, true, nil)
	if err != nil {
		return err
	}
	fmt.Print(t.RenderedNote(outPath, len(j.Tasks), journeySteps(j)))
	updateJourneyRow(idx, j.ID, len(j.Tasks), journeySteps(j), filepath.ToSlash(filepath.Join("details", journeyBaseName(j)+".md")))
	return saveJourneyIndex(idx, outDir, lang)
}

// compareJourneys is Differential analysis: resolve
// both id prefixes, build each Journey, diff their already-computed
// behavior profiles (journey.Compare), and write the result as one Markdown +
// JSON pair — the same .md+.json convention writeJourneyFile uses for a
// single Journey. Either side being partial-head gates on -include-partial
// exactly like a single-journey render (an unstable ID is still unstable
// when it's one half of a comparison); partiality itself rides as data —
// the Comparison's Partial field plus the .md banner (D19: no filename
// suffix).
func compareJourneys(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, idA, idB, firstPath string, prof taskseg.Profile, includePartial bool, outDir string, llmOpts llmCLIOptions, lang i18n.Lang, idx *journey.JourneyIndex, priceRes *pricing.Resolver, ccy string) error {
	_, chainA, err := resolveJourneyID(cands, byIdx, idA)
	if err != nil {
		return fmt.Errorf("-compare first id: %w", err)
	}
	_, chainB, err := resolveJourneyID(cands, byIdx, idB)
	if err != nil {
		return fmt.Errorf("-compare second id: %w", err)
	}
	partialA := journey.IsPartialHead(chainA, firstPath)
	partialB := journey.IsPartialHead(chainB, firstPath)
	if (partialA || partialB) && !includePartial {
		return fmt.Errorf("one or both journeys look head-truncated — pass -include-partial to compare them anyway")
	}

	jA, err := journey.BuildChain(chainA, prof, lang)
	if err != nil {
		return err
	}
	jB, err := journey.BuildChain(chainB, prof, lang)
	if err != nil {
		return err
	}
	// Stamp partiality onto the built Journeys before anything derives a
	// Summary or filename from them: IsPartialHead's verdict lived only in
	// these locals, so the compare path's own side JSONs and the Comparison's
	// Partial field silently lost the fact (D19 makes the field the carrier).
	jA.Partial = partialA
	jB.Partial = partialB
	sA, sB := journey.Summarize(jA, lang), journey.Summarize(jB, lang)
	cmp := journey.Compare(sA, sB, lang)
	// ReportFile points at each side's own journey report; the comparison
	// lives under compares/, so the .md's side-block link must climb out to
	// journeys/details/ to resolve.
	cmp.A.ReportFile = filepath.ToSlash(filepath.Join("..", "journeys", "details", journey.JourneyReportFile(jA.ID)))
	cmp.B.ReportFile = filepath.ToSlash(filepath.Join("..", "journeys", "details", journey.JourneyReportFile(jB.ID)))
	extras := journey.ComputeComparisonExtras(jA, jB, sA.Metrics, sB.Metrics, priceRes, ccy)
	extras.Sources = journey.SourceFiles(idx, jA.ID, jB.ID)
	cmp.Extras = &extras

	// -llm-dry-run: print the evidence-pack size estimate and return
	// immediately — deliberately checked BEFORE ensureStoriesDir below, so a
	// dry run never leaves so much as an empty journeys/ directory
	// behind (design doc C.7: "should I even run this" is a pure query, not
	// a partial run).
	if llmOpts.Addr != "" && llmOpts.DryRun {
		pack := journey.BuildEvidencePack(jA, jB, cmp, lang)
		chars := pack.EstimateChars()
		fmt.Printf("evidence pack: %d chars (~%d tokens estimated) — dry run, no request sent\n", chars, chars/4)
		if extras.Divergence.Found {
			divPack := journey.BuildDivergenceEvidencePack(jA, jB, extras.Divergence, lang)
			divChars := divPack.EstimateChars()
			fmt.Printf("divergence evidence pack: %d chars (~%d tokens estimated) — dry run, no request sent\n", divChars, divChars/4)
		}
		return nil
	}

	detailDir, evidenceDir := detailAndEvidenceDirs(outDir)
	// Each side's own journey-<id>.json picks up the same per-side cost the
	// tale-of-the-tape shows, computed off cmp.Extras.Cost rather than a
	// second ComputeJourneyCost pass.
	costA, costB := extras.Cost.A, extras.Cost.B
	journeysDir, err := ensureJourneysDir(outDir)
	if err != nil {
		return err
	}
	if err := ensureJourneyFile(jA, journeysDir, lang, prof, detailDir, evidenceDir, &costA); err != nil {
		return err
	}
	if err := ensureJourneyFile(jB, journeysDir, lang, prof, detailDir, evidenceDir, &costB); err != nil {
		return err
	}

	llmOverall, llmDiv := compareLLMRecords(jA, jB, cmp, extras, llmOpts, lang)
	cmp.LLMInterpretation = llmOverall
	cmp.LLMDivergence = llmDiv

	comparesDir, err := ensureComparesDir(outDir)
	if err != nil {
		return err
	}

	base := "compare-" + jA.ID + "-vs-" + jB.ID
	jsonPath := filepath.Join(comparesDir, base+".json")
	data, err := json.MarshalIndent(cmp, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, data, 0o600); err != nil {
		return err
	}

	diskData, err := os.ReadFile(jsonPath)
	if err != nil {
		return err
	}
	var diskCmp journey.Comparison
	if err := json.Unmarshal(diskData, &diskCmp); err != nil {
		return err
	}

	mdPath := filepath.Join(comparesDir, base+".md")
	// The LLM sections render inside RenderComparisonMarkdown from the two
	// records this JSON now carries — no post-render append (§3.6).
	if err := os.WriteFile(mdPath, []byte(journey.RenderComparisonMarkdown(diskCmp, lang)), 0o600); err != nil {
		return err
	}
	fmt.Printf("%s\n", mdPath)
	updateJourneyRow(idx, jA.ID, len(jA.Tasks), journeySteps(jA), filepath.ToSlash(filepath.Join("details", journeyBaseName(jA)+".md")))
	updateJourneyRow(idx, jB.ID, len(jB.Tasks), journeySteps(jB), filepath.ToSlash(filepath.Join("details", journeyBaseName(jB)+".md")))
	return saveJourneyIndex(idx, outDir, lang)
}

// compareLLMRecords runs the overall and divergence LLM interpretation
// calls for -compare, degrading gracefully on failure without failing the
// command. Returns both calls' persisted records (nil when -llm-addr is off
// or, for the divergence call, no divergence point was found) — the caller
// stamps them on the Comparison before it's marshaled, so compare-*.json is
// what the .md's LLM sections render from (§3.6).
func compareLLMRecords(jA, jB *journey.Journey, cmp journey.Comparison, extras journey.ComparisonExtras, llmOpts llmCLIOptions, lang i18n.Lang) (overall, div *journey.LLMInterpretation) {
	if llmOpts.Addr == "" {
		return nil, nil
	}
	pack := journey.BuildEvidencePack(jA, jB, cmp, lang)
	chars := pack.EstimateChars()
	fmt.Fprintf(os.Stderr, "calling %s (model=%s): evidence pack %d chars (~%d tokens estimated)\n", llmOpts.Addr, llmOpts.Model, chars, chars/4)
	res, err := journey.Interpret(context.Background(), llmOpts.LLMOptions, pack, lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: LLM interpretation failed, report will not include it: %v\n", err)
	}
	overall = journey.NewLLMInterpretation(llmOpts.LLMOptions, res, err, journey.LLMScopeOverall)

	if extras.Divergence.Found {
		divPack := journey.BuildDivergenceEvidencePack(jA, jB, extras.Divergence, lang)
		divChars := divPack.EstimateChars()
		fmt.Fprintf(os.Stderr, "calling %s (model=%s) for the divergence point: evidence pack %d chars (~%d tokens estimated)\n", llmOpts.Addr, llmOpts.Model, divChars, divChars/4)
		divRes, divErr := journey.Interpret(context.Background(), llmOpts.LLMOptions, divPack, lang)
		if divErr != nil {
			fmt.Fprintf(os.Stderr, "warning: divergence LLM interpretation failed, report will not include it: %v\n", divErr)
		}
		div = journey.NewLLMInterpretation(llmOpts.LLMOptions, divRes, divErr, journey.LLMScopeDivergence)
	}
	return overall, div
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
// this function's callers in cmd_journey.go/cmd_analyze.go pass true for it;
// only renderAllJourneys' own default-suite caller (not -render-all) ever
// passes false — see writeJourneyFile's doc comment for what false means.
//
// priceRes/ccy thread the same pricing resolution the single-journey
// renderJourney uses, so a batch-rendered journey-<id>.{md,json} is
// byte-identical to what -journey <id> would have produced (same CostFact,
// same overview cost line). priceRes may be nil (no pricing resolvable at
// all) — ComputeJourneyCost then yields the documented unresolved fact,
// exactly as the single-journey path would.
func renderJourneys(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof taskseg.Profile, includePartial bool, outDir string, lang i18n.Lang, idx *journey.JourneyIndex, noneMsg string, materializeDetails bool, priceRes *pricing.Resolver, ccy string) error {
	var toRender [][]*ctxgraph.Lineage
	var toRenderPartial []bool
	skippedPartial := 0
	for _, l := range cands {
		chain := ctxgraph.ChainFrom(l, byIdx)
		partial := journey.IsPartialHead(chain, firstPath)
		if partial && !includePartial {
			skippedPartial++
			continue
		}
		toRender = append(toRender, chain)
		toRenderPartial = append(toRenderPartial, partial)
	}
	if len(toRender) == 0 {
		fmt.Println(noneMsg)
		return saveJourneyIndex(idx, outDir, lang)
	}

	journeysDir, err := ensureJourneysDir(outDir)
	if err != nil {
		return err
	}
	t := i18n.CLI(lang)
	detailDir, evidenceDir := detailAndEvidenceDirs(outDir)
	rendered := 0
	for _, br := range batchByBytes(toRender, renderBatchBudgetBytes) {
		start, end := br[0], br[1]
		journeys, batchRecs, err := journey.BuildAllWithRecords(toRender[start:end], prof, lang)
		if err != nil {
			return err
		}
		for i, j := range journeys {
			j.Partial = toRenderPartial[start+i]
			m := journey.ComputeMetrics(j)
			findings := journey.ComputeFindings(j, lang)
			// Same cost computation as single -journey's renderJourney — the
			// batch rendered the same journeys and must produce the same
			// files (formerly a nil cost here: batch output silently lacked
			// the cost line the zoomed-in render of the same journey had).
			cost := journey.ComputeJourneyCost(j, priceRes, ccy)
			// batchRecs: EnsureJourneyDetails reuses this batch's already-
			// decompressed records instead of re-reading the source files.
			outPath, err := writeJourneyFile(j, m, findings, journeysDir, lang, nil, nil, prof, detailDir, evidenceDir, &cost, materializeDetails, batchRecs)
			if err != nil {
				return err
			}
			fmt.Print(t.RenderedNote(outPath, len(j.Tasks), journeySteps(j)))
			updateJourneyRow(idx, j.ID, len(j.Tasks), journeySteps(j), filepath.ToSlash(filepath.Join("details", filepath.Base(outPath))))
		}
		rendered += len(journeys)
	}
	if skippedPartial > 0 {
		fmt.Print(t.AllRenderedSkipped(skippedPartial))
	}
	fmt.Print(t.AllRenderedNote(rendered, journeysDir))
	return saveJourneyIndex(idx, outDir, lang)
}

// renderAllJourneys renders every non-partial candidate journey — see
// renderJourneys. materializeDetails distinguishes an explicit
// "render everything, details included" ask (-render-all, or
// vmr analyze -render-all) from the default suite's implicit
// category=task batch (cmd_analyze.go's dispatchAnalyze passes false
// there) — the latter is exactly the unbounded-materialization case to
// avoid (238+ candidates' worth of Step detail pages written on every run
// whether or not anyone reads them).
func renderAllJourneys(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof taskseg.Profile, includePartial bool, outDir string, lang i18n.Lang, idx *journey.JourneyIndex, materializeDetails bool, priceRes *pricing.Resolver, ccy string) error {
	return renderJourneys(cands, byIdx, firstPath, prof, includePartial, outDir, lang, idx,
		"no candidate journeys to render (all skipped as partial-head; pass -include-partial)", materializeDetails, priceRes, ccy)
}

// renderBenchmarks builds every non-partial candidate journey (same
// batched BuildAll path renderAllJourneys uses) and compute/write corpus-
// level statistics (journeys/benchmarks.{md,json}) instead of per-Journey
// files. Journeys are built here only to feed ComputeBenchmarkStats — none of
// them are individually rendered or written to disk by this path.
func renderBenchmarks(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof taskseg.Profile, includePartial bool, outDir string, lang i18n.Lang, idx *journey.JourneyIndex) error {
	var toRender [][]*ctxgraph.Lineage
	skippedPartial := 0
	for _, l := range cands {
		chain := ctxgraph.ChainFrom(l, byIdx)
		partial := journey.IsPartialHead(chain, firstPath)
		if partial && !includePartial {
			skippedPartial++
			continue
		}
		toRender = append(toRender, chain)
	}
	if len(toRender) == 0 {
		fmt.Println("no candidate journeys to analyze (all skipped as partial-head; pass -include-partial)")
		return saveJourneyIndex(idx, outDir, lang)
	}

	// Build in byte-budgeted batches (same bound renderJourneys uses): each
	// batch's records are released before the next fetch, but the built
	// Journeys are ~1% of that and all accumulate cheaply — the corpus
	// stats need every one of them at once, and 586 of them is ~300 MB.
	var journeys []*journey.Journey
	for _, br := range batchByBytes(toRender, renderBatchBudgetBytes) {
		js, err := journey.BuildAll(toRender[br[0]:br[1]], prof, lang)
		if err != nil {
			return err
		}
		journeys = append(journeys, js...)
	}
	stats := journey.ComputeBenchmarkStats(journeys)

	journeysDir, err := ensureJourneysDir(outDir)
	if err != nil {
		return err
	}
	jsonPath := filepath.Join(journeysDir, "benchmarks.json")
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, data, 0o600); err != nil {
		return err
	}

	diskData, err := os.ReadFile(jsonPath)
	if err != nil {
		return err
	}
	var diskStats journey.BenchmarkStats
	if err := json.Unmarshal(diskData, &diskStats); err != nil {
		return err
	}
	mdPath := filepath.Join(journeysDir, "benchmarks.md")
	if err := os.WriteFile(mdPath, []byte(journey.RenderBenchmarksMarkdown(diskStats, lang)), 0o600); err != nil {
		return err
	}
	if skippedPartial > 0 {
		fmt.Printf("%d head-truncated journey(s) skipped (pass -include-partial to include them)\n", skippedPartial)
	}
	fmt.Printf("%d journey(s) analyzed → %s\n", len(journeys), mdPath)
	for _, j := range journeys {
		updateJourneyRow(idx, j.ID, len(j.Tasks), journeySteps(j), "")
	}
	return saveJourneyIndex(idx, outDir, lang)
}

// detailAndEvidenceDirs returns {outDir}/details and {outDir}/evidence — the
// same layout internal/report's DetailWriter uses (setupDetailWriter/
// NewDetailWriter in cmd_report.go), shared so a detail page materialized
// by either command is reachable at the same path and a decision spine's
// "→ detail" link (P5.2, journey.EnsureJourneyDetails) resolves regardless of
// which command wrote it first.
func detailAndEvidenceDirs(outDir string) (detailDir, evidenceDir string) {
	return filepath.Join(outDir, "requests", "details"), filepath.Join(outDir, "requests", "evidence")
}

// ensureJourneysDir creates (if needed) and returns {outDir}/journeys.
// 0o700: journey output embeds full conversation bodies, same sensitivity
// as internal/report's details/ — must not loosen that.
func ensureJourneysDir(outDir string) (string, error) {
	journeysDir := filepath.Join(outDir, "journeys")
	if err := os.MkdirAll(journeysDir, 0o700); err != nil {
		return "", err
	}
	return journeysDir, nil
}

// ensureComparesDir creates (if needed) and returns {outDir}/compares.
func ensureComparesDir(outDir string) (string, error) {
	comparesDir := filepath.Join(outDir, "compares")
	if err := os.MkdirAll(comparesDir, 0o700); err != nil {
		return "", err
	}
	return comparesDir, nil
}

// journeyBaseName returns the base filename (without .md/.json extension)
// for j - the stem shared by both artifacts, derived from journey's
// JourneyReportFile (the single naming source of truth) by dropping the
// canonical .md extension.
func journeyBaseName(j *journey.Journey) string {
	return strings.TrimSuffix(journey.JourneyReportFile(j.ID), ".md")
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
func ensureJourneyFile(j *journey.Journey, journeysDir string, lang i18n.Lang, prof taskseg.Profile, detailDir, evidenceDir string, cost *journey.CostFact) error {
	m := journey.ComputeMetrics(j)
	findings := journey.ComputeFindings(j, lang)
	// true: both -compare sides are user-named targets, same as a single
	// -journey render (P13.1) — not a batch scope.
	_, err := writeJourneyFile(j, m, findings, journeysDir, lang, nil, nil, prof, detailDir, evidenceDir, cost, true, nil)
	return err
}

func writeJourneyFile(j *journey.Journey, m journey.Metrics, findings []journey.Finding, journeysDir string, lang i18n.Lang, llmInterp *journey.LLMInterpretation, llmFindings []journey.Finding, prof taskseg.Profile, detailDir, evidenceDir string, cost *journey.CostFact, materializeDetails bool, recs map[ctxgraph.Loc]*audit.Record) (string, error) {
	detailsDir := filepath.Join(journeysDir, "details")
	if err := os.MkdirAll(detailsDir, 0o700); err != nil {
		return "", err
	}
	base := journeyBaseName(j)
	outPath := filepath.Join(detailsDir, base+".md")
	if materializeDetails {
		journey.EnsureJourneyDetails(os.Stderr, j, recs, detailDir, evidenceDir, prof, lang)
	}

	jsonPath := filepath.Join(detailsDir, base+".json")
	// If the caller did not supply new LLM results (e.g. ensureJourneyFile
	// called during -compare, or a re-render without -llm-addr), preserve
	// any existing LLM interpretation/findings on disk so an expensive LLM
	// run is not silently clobbered.
	if llmInterp == nil && len(llmFindings) == 0 {
		if existingBytes, err := os.ReadFile(jsonPath); err == nil {
			var existing journey.JourneySummary
			if err := json.Unmarshal(existingBytes, &existing); err == nil {
				if existing.LLMInterpretation != nil {
					llmInterp = existing.LLMInterpretation
				}
				if len(existing.LLMFindings) > 0 {
					llmFindings = existing.LLMFindings
				}
			}
		}
	}
	summary := journey.NewJourneySummary(j, m, findings, llmFindings, cost, llmInterp)
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(jsonPath, data, 0o600); err != nil {
		return "", err
	}

	diskData, err := os.ReadFile(jsonPath)
	if err != nil {
		return "", err
	}
	var s journey.JourneySummary
	if err := json.Unmarshal(diskData, &s); err != nil {
		return "", err
	}

	_, reportMDErr := os.Stat(filepath.Join(filepath.Dir(journeysDir), "vmr-report.md"))
	linkDetails := materializeDetails || detailDirHasFiles(detailDir)
	// The LLM section renders inside the VM from summary.LLMInterpretation —
	// this .md is a pure function of the .json written above (§3.6).
	md := journey.RenderMarkdownFromSummary(&s, lang, reportMDErr == nil, linkDetails)
	if err := os.WriteFile(outPath, []byte(md), 0o600); err != nil {
		return "", err
	}
	return outPath, nil
}

// journeySteps totals a Journey's steps across all its tasks.
func journeySteps(j *journey.Journey) int {
	steps := 0
	for _, t := range j.Tasks {
		steps += len(t.Steps)
	}
	return steps
}
