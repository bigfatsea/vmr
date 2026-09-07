// Ver 2026-08-21 01:30, by Sonnet 5

package main

import (
	"fmt"
	"path/filepath"

	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/journey"
	"vmr/internal/taskseg"
)

// journeySetup bundles the outcome of the analyze journey half's
// scan/stitch/candidate/index-row pipeline — every mode (listing, -journey,
// -compare, -benchmark, -render-all) starts from the same setup. Factored
// out (P9.1) so cmdAnalyze can run this pipeline once from its own unified
// flag set's resolution. Split into its own file once extracting it pushed
// cmd_journey.go over its file-size budget — same package, no new import
// boundary.
type journeySetup struct {
	g         *ctxgraph.Graph
	byIdx     map[int]*ctxgraph.Lineage
	cands     []*ctxgraph.Lineage     // ListCandidates' output, self-traffic filtered
	chains    [][]*ctxgraph.Lineage   // cands[i]'s full stitched chain, same index
	freshRows []journey.JourneyIndexRow // cands[i]'s index row, same index — .Category already computed (P9.2 reads this)
	idx       *journey.JourneyIndex
	firstPath string
	prof      taskseg.Profile
}

// setupJourneyRun runs the journey half's scan/stitch/candidate/index-row
// pipeline — the piece every mode dispatch in cmdAnalyze starts from: same
// calls, same order, same self-traffic filtering as the pre-P9.1 inline
// body.
func setupJourneyRun(paths []string, outDir string, includeSelfTraffic bool, llmKey string, selfTrafficTags []string, showUngrouped bool, lang i18n.Lang) (*journeySetup, error) {
	// indexPath is computed (and LoadJourneyIndex'd) up front, before
	// anything is scanned — this is a pure string join plus a best-effort
	// file read, no directory creation, so it stays safe to do even on an
	// -llm-dry-run path that must leave journeys/ untouched if it
	// returns early (ensureJourneysDir/idx.Save only happen once each
	// branch below reaches its own normal write point).
	journeysDir := filepath.Join(outDir, "journeys")
	indexPath := filepath.Join(journeysDir, "index.json")
	prior := journey.LoadJourneyIndex(indexPath)
	cacheDir := filepath.Join(outDir, ".cache", "parse") // shared with the report half — see cmd_report.go
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
	// the report half also calls — see its own doc comment.
	prof := resolveTaskProfile()

	cands := journey.ListCandidates(g)
	selfTraffic := &journey.SelfTrafficStatus{}
	if !includeSelfTraffic {
		if before := len(cands); len(selfTrafficExcludeTags(llmKey, selfTrafficTags)) > 0 {
			cands = filterSelfTrafficCandidates(cands, llmKey, selfTrafficTags)
			selfTraffic.Active = true
			selfTraffic.Excluded = before - len(cands)
		}
	}

	// One batched title fetch across every candidate (journey.PreviewTitles
	// groups reads by source file, so this scans each file at most once no
	// matter how many candidates), reused both by the index rows below and
	// by listJourneys' stdout listing — the index is now the single place
	// that derives a candidate's cheap fields, no branch recomputes them.
	chains := make([][]*ctxgraph.Lineage, len(cands))
	for i, l := range cands {
		chains[i] = ctxgraph.ChainFrom(l, byIdx)
	}
	titles, err := journey.PreviewTitles(chains, prof, lang)
	if err != nil {
		return nil, err
	}
	freshRows := make([]journey.JourneyIndexRow, len(cands))
	for i, l := range cands {
		partial := journey.IsPartialHead(chains[i], firstPath)
		freshRows[i] = journey.BuildJourneyIndexRow(chains[i], titles[l], partial)
	}
	idx := &journey.JourneyIndex{Cache: fileCache, Journeys: journey.MergeJourneyIndexRows(freshRows, prior.Journeys), SelfTraffic: selfTraffic}

	return &journeySetup{
		g: g, byIdx: byIdx, cands: cands, chains: chains, freshRows: freshRows,
		idx: idx, firstPath: firstPath, prof: prof,
	}, nil
}
