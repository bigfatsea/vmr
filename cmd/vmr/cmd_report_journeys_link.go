// Ver 2026-08-20 16:30, by Sonnet 5

// The "vmr-report.md → journeys/index.md" navigation edge (P6.2a) and
// the "request index session row → journey" edge (P6.2c) — both need the
// same read of journeys/index.json, so they're built together here.
// This is the one place the report half looks at the journey half's
// output: it only reads an already-written index (see the architecture
// doc §7.5's "report doesn't generate journeys" ruling) and degrades to
// "nothing to link" when that file isn't there — report must work
// standalone.
package main

import (
	"path/filepath"

	"vmr/internal/fmtutil"
	"vmr/internal/journey"
	"vmr/internal/report"
)

// loadStoriesLink reads {outDir}/journeys/index.json if present and
// returns both navigation aids it feeds: the header-line summary
// (JourneysLinkInfo, nil when absent) and a lineage-id -> rendered-journey-
// filename map (nil/empty when absent or nothing's been rendered yet) for
// requests.go's session-card links. A missing or unreadable index is not
// an error — the report half running on its own, with no journey-half pass
// ever having touched this output root, is a normal, fully supported case.
func loadStoriesLink(outDir string) (*report.JourneysLinkInfo, map[string]string) {
	indexPath := filepath.Join(outDir, "journeys", "index.json")
	idx := journey.LoadJourneyIndex(indexPath)
	if len(idx.Journeys) == 0 {
		return nil, nil
	}

	lineageToJourney := map[string]string{}
	from, to := idx.Journeys[0].Start, idx.Journeys[0].End
	for _, j := range idx.Journeys {
		if j.Start.Before(from) {
			from = j.Start
		}
		if j.End.After(to) {
			to = j.End
		}
		if j.Rendered == "" {
			continue
		}
		for _, lin := range j.Lineages {
			lineageToJourney[lin] = j.Rendered
		}
	}
	info := &report.JourneysLinkInfo{
		Path:         "journeys/index.md",
		JourneyCount: len(idx.Journeys),
		FromDisplay:  from.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05"),
		ToDisplay:    to.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05"),
	}
	return info, lineageToJourney
}
