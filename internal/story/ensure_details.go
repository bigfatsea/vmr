// Ver 2026-08-20, by Sonnet 5

package story

import (
	"fmt"
	"io"
	"os"
	"sync"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/reqdetail"
	"vmr/internal/taskseg"
)

// EnsureJourneyDetails materializes every Step's detail page (and, when it
// has one, its system-prompt/tool-set evidence blobs — see
// reqdetail.EnsureSysPromptEvidence/EnsureToolsEvidence) under
// detailDir/evidenceDir, skipping any that already exist. Idempotent and
// safe to call before every render (EnsureRendered's own existence check
// makes repeat calls cheap) — this is what lets a spine's "→ detail" link
// (render_spine_step.go) resolve without requiring the caller to have run
// `vmr report -details` first.
//
// recs is the record map the caller already fetched (BuildAllWithRecords)
// — passed straight through so a batch render doesn't re-decompress its
// source files just to write detail pages. nil is fine: the records are
// then streamed back in with ctxgraph.ForEachRecord, one pass per file,
// discarded as each page is written (the single -journey / default-suite
// path, where re-reading one Journey's files costs nothing).
//
// s.PrevManifest (nil at a Lineage's first Step, including a stitch
// boundary — see its own doc comment on Step) is passed straight through:
// this is what keeps a page EnsureJourneyDetails writes byte-identical to
// the one `vmr report -details` would write for the same record, matching
// internal/report/session.go's own per-Lineage "prev" semantics.
//
// A per-Step failure is reported to w, not returned — `vmr story` is a
// read-only offline analysis tool, and a single record that fails to
// render (a rare malformed body, a transient disk error) should not cost
// the reader the other 99% of an otherwise-complete Journey narrative. The
// spine's link text is already correct either way (it's a pure function of
// the Step's own Manifest, not of whether the file exists) — a failed Step
// just leaves that one link pointing at a file that doesn't exist yet,
// which is a strictly better failure mode than losing the whole report.
func EnsureJourneyDetails(w io.Writer, j *Journey, recs map[ctxgraph.Loc]*audit.Record, detailDir, evidenceDir string, prof taskseg.Profile, lang i18n.Lang) {
	// 0o700: same sensitivity note as report.NewDetailWriter's own
	// MkdirAll — detail pages carry the same full conversation bodies as
	// the 0600 audit log they're derived from. reqdetail.EnsureRendered
	// itself never creates detailDir (writeFileAtomic's os.CreateTemp
	// requires it to already exist, same contract report.NewDetailWriter
	// satisfies once up front) — a single MkdirAll failure here would
	// otherwise surface as the same warning N times, once per Step, for
	// one root cause; reporting it once and returning is more useful.
	if err := os.MkdirAll(detailDir, 0o700); err != nil {
		fmt.Fprintf(w, "warning: journey %s: detail export skipped, could not create %s: %v\n", j.ID, detailDir, err)
		return
	}

	render := func(s *Step, rec *audit.Record) {
		if rec == nil || s.Manifest == nil {
			return
		}
		if _, err := reqdetail.EnsureRendered(detailDir, rec, s.Manifest.Path, s.Manifest.Line,
			s.Manifest, s.PrevManifest, prof, lang, evidenceDir); err != nil {
			fmt.Fprintf(w, "warning: journey %s step %d: detail export failed: %v\n", j.ID, s.Seq, err)
		}
	}

	steps := journeySteps(j)
	if recs != nil {
		for _, s := range steps {
			if s.Manifest == nil {
				continue
			}
			render(s, recs[ctxgraph.Loc{Path: s.Manifest.Path, Line: s.Manifest.Line}])
		}
		return
	}

	byLoc := make(map[ctxgraph.Loc]*Step, len(steps))
	var locs []ctxgraph.Loc
	for _, s := range steps {
		if s.Manifest == nil {
			continue
		}
		loc := ctxgraph.Loc{Path: s.Manifest.Path, Line: s.Manifest.Line}
		byLoc[loc] = s
		locs = append(locs, loc)
	}
	// ForEachRecord calls fn from concurrent per-file workers; EnsureRendered
	// writes files (its own pages, plus shared evidence blobs two Steps can
	// collide on), so serialize — the win here is not re-decompressing, not
	// parallel rendering.
	var mu sync.Mutex
	if err := ctxgraph.ForEachRecord(locs, func(loc ctxgraph.Loc, rec *audit.Record) {
		mu.Lock()
		defer mu.Unlock()
		render(byLoc[loc], rec)
	}); err != nil {
		fmt.Fprintf(w, "warning: journey %s: detail export incomplete, could not re-read records: %v\n", j.ID, err)
	}
}
