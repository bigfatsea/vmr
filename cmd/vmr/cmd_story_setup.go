// Ver 2026-08-21 01:30, by Sonnet 5

package main

import (
	"fmt"
	"path/filepath"

	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/story"
	"vmr/internal/taskseg"
)

// storySetup bundles the outcome of vmr story's scan/stitch/candidate/
// index-row pipeline — every mode (listing, -journey, -compare, -corpus,
// -render-all) starts from the same setup. Factored out (P9.1) so
// cmdAnalyze can run this pipeline once from its own unified flag set's
// resolution, without going through vmr story's own flag.FlagSet. Split
// into its own file alongside cmdStory (P9.1's real-corpus validation run)
// once extracting it pushed cmd_story.go over its file-size budget — same
// package, no new import boundary.
type storySetup struct {
	g         *ctxgraph.Graph
	byIdx     map[int]*ctxgraph.Lineage
	cands     []*ctxgraph.Lineage     // ListCandidates' output, self-traffic filtered
	chains    [][]*ctxgraph.Lineage   // cands[i]'s full stitched chain, same index
	freshRows []story.JourneyIndexRow // cands[i]'s index row, same index — .Category already computed (P9.2 reads this)
	idx       *story.StoryIndex
	firstPath string
	prof      taskseg.Profile
}

// setupStoryRun runs vmr story's scan/stitch/candidate/index-row pipeline —
// the piece every mode dispatch in cmdStory (and, since P9.1, cmdAnalyze)
// starts from. No behavior change from what cmdStory ran inline before
// P9.1: same calls, same order, same self-traffic filtering.
func setupStoryRun(paths []string, outDir string, includeSelfTraffic bool, llmKey string, selfTrafficTags []string, showUngrouped bool, lang i18n.Lang) (*storySetup, error) {
	// indexPath is computed (and LoadStoryIndex'd) up front, before
	// anything is scanned — this is a pure string join plus a best-effort
	// file read, no directory creation, so it stays safe to do even on an
	// -llm-dry-run path that must leave reports/stories/ untouched if it
	// returns early (ensureStoriesDir/idx.Save only happen once each
	// branch below reaches its own normal write point).
	storiesDir := filepath.Join(outDir, "stories")
	indexPath := filepath.Join(storiesDir, "vmr-stories.json")
	prior := story.LoadStoryIndex(indexPath)
	cacheDir := filepath.Join(outDir, ".parse-cache") // shared with `vmr report` — see cmd_report.go
	priorCache := ctxgraph.LoadCacheDir(cacheDir)

	fmt.Printf("scanning %d file(s)...\n", len(paths))
	g, fileCache, err := ctxgraph.ScanCached(paths, priorCache)
	if err != nil {
		return nil, err
	}
	fmt.Printf("%d lineage(s), %d ungrouped record(s), %d unparseable record(s)\n", len(g.Lineages), len(g.Ungrouped), g.NoBody)
	if showUngrouped {
		printUngrouped(g.Ungrouped, lang)
	}
	firstPath := paths[0]

	ctxgraph.StitchGraph(g)
	byIdx := ctxgraph.LineageIndex(g)

	// resolveTaskProfile is the shared cmd/vmr composition-root entry point
	// `vmr report` also calls — see its own doc comment.
	prof := resolveTaskProfile()

	cands := story.ListCandidates(g)
	selfTraffic := &story.SelfTrafficStatus{}
	if !includeSelfTraffic {
		if before := len(cands); len(selfTrafficExcludeTags(llmKey, selfTrafficTags)) > 0 {
			cands = filterSelfTrafficCandidates(cands, llmKey, selfTrafficTags)
			selfTraffic.Active = true
			selfTraffic.Excluded = before - len(cands)
		}
	}

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
		return nil, err
	}
	freshRows := make([]story.JourneyIndexRow, len(cands))
	for i, l := range cands {
		partial := story.IsPartialHead(chains[i], firstPath)
		freshRows[i] = story.BuildJourneyIndexRow(chains[i], titles[l], partial)
	}
	idx := &story.StoryIndex{Cache: fileCache, Journeys: story.MergeJourneyIndexRows(freshRows, prior.Journeys), SelfTraffic: selfTraffic}

	return &storySetup{
		g: g, byIdx: byIdx, cands: cands, chains: chains, freshRows: freshRows,
		idx: idx, firstPath: firstPath, prof: prof,
	}, nil
}
