// Ver 2026-09-22 19:15, by Sonnet 5

package analyze

import (
	"fmt"
	"path/filepath"

	"vmr/internal/ctxgraph"
	"vmr/internal/journey"
)

// JourneySetup bundles the outcome of the analyze journey half's
// scan/stitch/candidate/index-row pipeline.
type JourneySetup struct {
	G         *ctxgraph.Graph
	ByIdx     map[int]*ctxgraph.Lineage
	Cands     []*ctxgraph.Lineage       // ListCandidates' output, self-traffic filtered
	Chains    [][]*ctxgraph.Lineage     // cands[i]'s full stitched chain, same index
	FreshRows []journey.JourneyIndexRow // cands[i]'s index row, same index
	Idx       *journey.JourneyIndex
	FirstPath string
}

// SetupJourney runs the journey setup pipeline on r.
func (r *Run) SetupJourney() (*JourneySetup, error) {
	return r.setupJourneyRun()
}

func (r *Run) setupJourneyRun() (*JourneySetup, error) {
	journeysDir := filepath.Join(r.OutDir, "journeys")
	indexPath := filepath.Join(journeysDir, "index.json")
	prior := journey.LoadJourneyIndex(indexPath)
	cacheDir := filepath.Join(r.OutDir, ".cache", "parse")
	priorCache := ctxgraph.LoadCacheDir(cacheDir)

	fmt.Printf("scanning %d file(s)...\n", len(r.Paths))
	g, fileCache, err := ctxgraph.ScanCached(r.Paths, priorCache)
	if err != nil {
		return nil, err
	}
	fmt.Printf("%d lineage(s), %d ungrouped record(s), %d unparseable record(s)\n", len(g.Lineages), len(g.Ungrouped), g.NoBody)
	if r.ShowUngrouped {
		printUngrouped(g.Ungrouped, r.Lang)
	}
	firstPath := r.Paths[0]

	ctxgraph.StitchGraph(g)
	byIdx := ctxgraph.LineageIndex(g)
	prof := r.profile()

	cands := journey.ListCandidates(g)
	selfTraffic := &journey.SelfTrafficStatus{}
	if !r.IncludeSelfTraffic {
		llmKey := r.LLMKey
		if llmKey == "" {
			llmKey = r.LLMOpts.APIKey
		}
		if before := len(cands); len(SelfTrafficExcludeTags(llmKey, r.SelfTrafficTags)) > 0 {
			cands = filterSelfTrafficCandidates(cands, llmKey, r.SelfTrafficTags)
			selfTraffic.Active = true
			selfTraffic.Excluded = before - len(cands)
		}
	}

	chains := make([][]*ctxgraph.Lineage, len(cands))
	for i, l := range cands {
		chains[i] = ctxgraph.ChainFrom(l, byIdx)
	}
	titles, err := journey.PreviewTitles(chains, prof)
	if err != nil {
		return nil, err
	}
	freshRows := make([]journey.JourneyIndexRow, len(cands))
	for i, l := range cands {
		partial := journey.IsPartialHead(chains[i], firstPath)
		freshRows[i] = journey.BuildJourneyIndexRow(chains[i], titles[l], partial)
	}
	idx := &journey.JourneyIndex{Cache: fileCache, Journeys: journey.MergeJourneyIndexRows(freshRows, prior.Journeys), SelfTraffic: selfTraffic}

	return &JourneySetup{
		G: g, ByIdx: byIdx, Cands: cands, Chains: chains, FreshRows: freshRows,
		Idx: idx, FirstPath: firstPath,
	}, nil
}

// renderableCandidates filters su.Cands down to the non-noise rows
// (journey.IsNoiseCategory, already computed by setupJourneyRun's
// BuildJourneyIndexRow call).
func renderableCandidates(su *JourneySetup) []*ctxgraph.Lineage {
	var out []*ctxgraph.Lineage
	for i, l := range su.Cands {
		if !journey.IsNoiseCategory(su.FreshRows[i].Category) {
			out = append(out, l)
		}
	}
	return out
}
