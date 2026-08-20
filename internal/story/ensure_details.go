// Ver 2026-08-20, by Sonnet 5

package story

import (
	"fmt"
	"io"
	"os"

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
// s.PrevManifest (nil at a Lineage's first Step, including a stitch
// boundary — see its own doc comment on Step) is passed straight through:
// this is what keeps a page EnsureJourneyDetails writes byte-identical to
// the one `vmr report -details` would write for the same record, the P2
// invariant story_report_p5_action_plan_sonnet-5.md §0 (point 2) traces
// through internal/report/session.go's own per-Lineage "prev" semantics.
//
// A per-Step failure is reported to w, not returned — `vmr story` is a
// read-only offline analysis tool, and a single record that fails to
// render (a rare malformed body, a transient disk error) should not cost
// the reader the other 99% of an otherwise-complete Journey narrative. The
// spine's link text is already correct either way (it's a pure function of
// the Step's own Manifest, not of whether the file exists) — a failed Step
// just leaves that one link pointing at a file that doesn't exist yet,
// which is a strictly better failure mode than losing the whole report.
func EnsureJourneyDetails(w io.Writer, j *Journey, detailDir, evidenceDir string, prof taskseg.Profile, lang i18n.Lang) {
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
	for _, s := range journeySteps(j) {
		if s.Rec == nil || s.Manifest == nil {
			continue // defensive — production path guarantees non-nil, test fixtures may not
		}
		if _, err := reqdetail.EnsureRendered(detailDir, s.Rec, s.Manifest.Path, s.Manifest.Line,
			s.Manifest, s.PrevManifest, prof, lang, evidenceDir); err != nil {
			fmt.Fprintf(w, "warning: journey %s step %d: detail export failed: %v\n", j.ID, s.Seq, err)
		}
	}
}
