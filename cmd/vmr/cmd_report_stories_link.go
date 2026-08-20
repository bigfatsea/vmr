// Ver 2026-08-20 16:30, by Sonnet 5

// The "vmr-report.md → stories/vmr-stories.md" navigation edge (P6.2a) and
// the "request index session row → journey" edge (P6.2c) — both need the
// same read of stories/vmr-stories.json, so they're built together here.
// This is the one place `vmr report` looks at `vmr story`'s output: it
// only reads an already-written index (see the architecture doc §7.5's
// "report doesn't generate stories" ruling) and degrades to "nothing to
// link" when that file isn't there — report must work standalone.
package main

import (
	"os"
	"path/filepath"

	"vmr/internal/fmtutil"
	"vmr/internal/report"
	"vmr/internal/story"
)

// loadStoriesLink reads {outDir}/stories/vmr-stories.json if present and
// returns both navigation aids it feeds: the header-line summary
// (StoriesLinkInfo, nil when absent) and a lineage-id -> rendered-journey-
// filename map (nil/empty when absent or nothing's been rendered yet) for
// requests.go's session-card links. A missing or unreadable index is not
// an error — `vmr report` run on its own, with no `vmr story` pass ever
// having touched this output root, is a normal, fully supported case.
func loadStoriesLink(outDir string) (*report.StoriesLinkInfo, map[string]string) {
	indexPath := filepath.Join(outDir, "stories", "vmr-stories.json")
	if _, err := os.Stat(indexPath); err != nil {
		return nil, nil
	}
	idx := story.LoadStoryIndex(indexPath)
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
	info := &report.StoriesLinkInfo{
		Path:         "stories/vmr-stories.md",
		JourneyCount: len(idx.Journeys),
		FromDisplay:  from.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05"),
		ToDisplay:    to.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05"),
	}
	return info, lineageToJourney
}
