// Ver 2026-07-29 22:30, by Sonnet 5

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vmr/internal/ctxgraph"
	"vmr/internal/story"
	"vmr/internal/story/profile"
)

// cmdStory renders one agent task's full execution history as a
// self-contained Markdown narrative — see
// docs/Agent任务叙事报告_设计与价值论证_2026-07-28_opus-5.md for the design.
// Step 2 (design doc Appendix C.4 T2.2): a candidate is no longer always
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
	includePartial := fs.Bool("include-partial", false, "also list/render journeys whose head looks truncated by the loaded file range (design doc §11 D1)")
	showUngrouped := fs.Bool("show-ungrouped", false, "print the source location of the first few ungrouped records (design-doc review §2.1)")
	if err := fs.Parse(args); err != nil {
		return err
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
		printUngrouped(g.Ungrouped)
	}
	firstPath := paths[0]

	ctxgraph.StitchGraph(g)
	byIdx := ctxgraph.LineageIndex(g)

	// Step 1 ships exactly one profile (design doc §11 D5): OpenClaw-aware
	// but harmless on any other agent's input, since none of its patterns
	// match generic chat text.
	prof := profile.OpenClawAware

	cands := story.ListCandidates(g)

	if *journeyArg != "" {
		var target *ctxgraph.Lineage
		for _, l := range cands {
			chain := ctxgraph.ChainFrom(l, byIdx)
			if strings.HasPrefix(story.ID(chain), *journeyArg) {
				target = l
				break
			}
		}
		if target == nil {
			return fmt.Errorf("no journey matching id prefix %q (run without -journey to list candidates)", *journeyArg)
		}
		return renderJourney(target, byIdx, firstPath, prof, *includePartial, *outDir)
	}
	if *renderAll {
		return renderAllJourneys(cands, byIdx, firstPath, prof, *includePartial, *outDir)
	}
	return listJourneys(cands, byIdx, g, firstPath, prof, *includePartial)
}

func listJourneys(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, g *ctxgraph.Graph, firstPath string, prof profile.Profile, includePartial bool) error {
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
	// chains are rooted in it (design-doc review §1.2).
	chains := make([][]*ctxgraph.Lineage, len(toShow))
	for i, r := range toShow {
		chains[i] = r.chain
	}
	titles, err := story.PreviewTitles(chains, prof)
	if err != nil {
		return err
	}

	for _, r := range toShow {
		mark := ""
		if r.partial {
			mark = " [断头]"
		}
		if len(r.chain) > 1 {
			mark += fmt.Sprintf(" [缝合×%d]", len(r.chain))
		}
		head, tail := r.chain[0], r.chain[len(r.chain)-1]
		first, last := head.Manifests[0], tail.Manifests[len(tail.Manifests)-1]
		steps := 0
		for _, cl := range r.chain {
			steps += len(cl.Manifests)
		}
		fmt.Printf("  %s%-6s %3d 轮  %s → %s  %s\n",
			story.ID(r.chain), mark, steps,
			first.TS.Format("01-02 15:04"), last.TS.Format("15:04"), titles[r.l])
	}
	if skippedPartial > 0 {
		fmt.Printf("\n%d 个断头 journey 已跳过（-include-partial 显示；见设计文档 §11 D1）\n", skippedPartial)
	}
	fmt.Printf("\n用 -journey <id前缀> 渲染其中一个\n")
	return nil
}

// maxUngroupedShown caps how many ungrouped records -show-ungrouped prints —
// a triage aid, not a report; showing all of them defeats the point when
// there are thousands (design-doc review §2.1).
const maxUngroupedShown = 10

// printUngrouped prints the source location of the first few ungrouped
// manifests, so -show-ungrouped gives an operator somewhere to start
// looking instead of just a bare count.
func printUngrouped(ms []*ctxgraph.Manifest) {
	if len(ms) == 0 {
		return
	}
	n := len(ms)
	if n > maxUngroupedShown {
		n = maxUngroupedShown
	}
	fmt.Printf("  前 %d 条未归组记录:\n", n)
	for _, m := range ms[:n] {
		fmt.Printf("    %s:%d  ts=%s\n", m.Path, m.Line, m.TS.Format("01-02 15:04:05"))
	}
	if len(ms) > n {
		fmt.Printf("    ... 还有 %d 条\n", len(ms)-n)
	}
}

func renderJourney(target *ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof profile.Profile, includePartial bool, outDir string) error {
	chain := ctxgraph.ChainFrom(target, byIdx)
	partial := story.IsPartialHead(chain, firstPath)
	if partial && !includePartial {
		return fmt.Errorf("journey %s looks head-truncated (design doc §11 D1) — pass -include-partial to render it anyway", story.ID(chain))
	}
	j, err := story.BuildChain(chain, prof)
	if err != nil {
		return err
	}
	j.Partial = partial
	storiesDir, err := ensureStoriesDir(outDir)
	if err != nil {
		return err
	}
	outPath, err := writeJourneyFile(j, storiesDir)
	if err != nil {
		return err
	}
	fmt.Printf("%s (%d 任务, %d 轮)\n", outPath, len(j.Tasks), journeySteps(j))
	return nil
}

// renderAllJourneys renders every non-partial candidate in one batched
// pass instead of requiring -journey <id> one at a time — story.BuildAll
// shares a single FetchRecords call across every candidate (same fix
// PreviewTitles applied to the listing path), so this costs about the same
// I/O as just listing, not N times more.
func renderAllJourneys(cands []*ctxgraph.Lineage, byIdx map[int]*ctxgraph.Lineage, firstPath string, prof profile.Profile, includePartial bool, outDir string) error {
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

	journeys, err := story.BuildAll(toRender, prof)
	if err != nil {
		return err
	}
	storiesDir, err := ensureStoriesDir(outDir)
	if err != nil {
		return err
	}
	for i, j := range journeys {
		j.Partial = toRenderPartial[i]
		outPath, err := writeJourneyFile(j, storiesDir)
		if err != nil {
			return err
		}
		fmt.Printf("%s (%d 任务, %d 轮)\n", outPath, len(j.Tasks), journeySteps(j))
	}
	if skippedPartial > 0 {
		fmt.Printf("\n%d 个断头 journey 已跳过（-include-partial 渲染；见设计文档 §11 D1）\n", skippedPartial)
	}
	fmt.Printf("\n%d 个 journey 已渲染到 %s\n", len(journeys), storiesDir)
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
// JSON (design doc §6.5: journey-<id>.json, consumed directly by Step 4's
// 4d comparison module) into storiesDir, and returns the Markdown path
// written. 0o600: same sensitivity note as ensureStoriesDir — the JSON
// carries token counts and tool-call args derived straight from the
// conversation body.
//
// A partial (head-truncated, design doc §11 D1) Journey gets a "-partial"
// filename suffix — its ID is already unstable (it depends on whatever
// happened to be the earliest loaded manifest), so the suffix is cheap,
// visible self-disclosure that this file's beginning isn't the real
// beginning, without requiring the reader to open it and find the warning
// line first.
func writeJourneyFile(j *story.Journey, storiesDir string) (string, error) {
	base := "journey-" + j.ID
	if j.Partial {
		base += "-partial"
	}
	outPath := filepath.Join(storiesDir, base+".md")
	if err := os.WriteFile(outPath, []byte(story.RenderMarkdown(j)), 0o600); err != nil {
		return "", err
	}
	jsonPath := filepath.Join(storiesDir, base+".json")
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
