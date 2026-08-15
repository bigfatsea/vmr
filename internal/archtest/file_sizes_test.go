// Ver 2026-07-26, by Sonnet 5
package archtest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// fileLineLimits pins a finding from an architecture review: router.go had
// grown to 948 lines against the design doc's own stated ~550-line budget
// ("若主流程显著变大，说明抽象错了"), and nobody noticed because the budget
// only ever lived in a comment. A file split
// (snapshot.go/limiter.go/transport.go/logfmt.go) brought it back to 538.
// The limit here is set with headroom above that real post-split number,
// not at the design doc's original round figure — the point is catching
// regrowth back toward 948, not fighting over every line.
var fileLineLimits = map[string]int{
	"internal/router/router.go": 700,
	// internal/report's two largest files, budgeted after the same kind of
	// split: the renderer used to be one 1053-line aggregate_render.go and
	// is now render_doc.go (running order + shared table primitive) plus one
	// section_*.go per numbered section. The budget is what keeps a new
	// report section arriving as a new file rather than as another 90 lines
	// appended to whichever file is already the biggest.
	//
	// aggregate.go's 1000 dated from before Part 8 batch B4 (TrafficStats
	// composition + buildInternal decomposition): it was 1015 lines with a
	// single 625-line buildInternal, and is now 503 with buildInternal at
	// ~25 (the accumulation half moved to ingest.go, per-record extraction
	// to recextract.go). Retightened to the same ~15% headroom convention
	// as the rest of this table instead of leaving 1000 as a stale ceiling
	// nearly double the file's actual size.
	"internal/report/aggregate.go":  600,
	"internal/report/render_doc.go": 400,
	// The two files B4 split out of aggregate.go — ingest.go (per-bucket
	// TrafficStats/Row/HourRow/EndpointRow/ClientRow/WorkloadRow/SessionRow
	// accumulation) and recextract.go (per-record buildRec2 extraction) —
	// registered at the same time as the split, same ~15-20% first-time
	// headroom convention jsonscan's scan.go/rewrite.go used below rather
	// than left budget-less like aggregate.go was before its own review.
	"internal/report/ingest.go":     310,
	"internal/report/recextract.go": 310,
	// No prior split here — config.go is at 591 lines today. The budget is
	// a tripwire against the same unnoticed growth pattern that hit
	// router.go, not a statement that 591 is already too big: if it crosses
	// this, split by concern (e.g. a separate file for provider/model
	// validation) rather than raising the number.
	"internal/config/config.go": 750,
	// The three entries below are first-time budgets, not post-split ones
	// like router.go/aggregate.go/render_doc.go above — an architecture
	// review (2026-08-03) found these had grown past aggregate.go's own
	// 1000-line budget (999 lines, effectively at capacity) without ever
	// having a budget of their own to trip. ~15% headroom over each file's
	// size at the time this budget was set: enough for a paragraph-sized
	// addition, not enough to quietly regrow into another 1050-line
	// aggregate_render.go before anyone notices.
	"internal/report/detail.go":  1150,
	"internal/report/session.go": 1100,
	"internal/story/journey.go":  850,
	// The entries below (decision-spine rendering, rule-derived Findings,
	// corpus-level statistics, and the shared infra they landed in
	// compare.go/metrics.go/render_md.go) were pre-registered, not caught
	// after the fact — same ~15% headroom over each file's line count at
	// registration time as the detail.go/session.go/journey.go entries
	// above, a paragraph's worth of room, not an invitation to keep
	// growing unnoticed.
	"internal/story/render_md.go":    350,
	"internal/story/render_spine.go": 380,
	// toolCallLine and its helpers (the per-tool-call argument renderer
	// render_spine.go's decision spine calls into) — split out the moment
	// render_spine.go first crossed its own budget over this, not appended
	// past 380 in place; same ~15% headroom convention as the
	// rest of this table.
	"internal/story/render_spine_args.go":   200,
	"internal/story/findings.go":            580,
	"internal/story/findings_toolresult.go": 320,
	"internal/story/compare.go":             850,
	"internal/story/metrics.go":             470,
	"internal/story/corpus.go":              380,
	"internal/story/render_corpus.go":       150,
	// internal/respnorm/respnorm.go (formerly internal/router/response.go —
	// a full response-normalization state machine: passthrough/buffered/
	// opaque transitions, SSE event splitting, MiniMax quirk trigger points,
	// quota usage sniffing) got its own budget when an architecture review
	// (2026-08-10) flagged it as the one large file this table didn't cover
	// — 850 at 736 lines. Part 8 batch B7 (2026-08-15) moved it into its own
	// package (see internal/respnorm's package doc comment) for fuzzability,
	// not to shrink it, and carried the SAME 850-line budget over unchanged
	// rather than recomputing headroom from the post-move line count: the
	// point of that batch was explicitly not file size (its own review notes
	// router.go doesn't shrink by a single line either), so resetting this
	// number would misrepresent the move as a size fix it never claimed to
	// be. minimax.go (formerly responsefix.go) is a first-time budget at the
	// same ~15-20% headroom convention as every other entry here.
	"internal/respnorm/respnorm.go": 850,
	"internal/respnorm/minimax.go":  235,
	// cmd/vmr had NO entry in this table at all until an architecture review
	// (2026-08-14) noticed cmd_story.go had grown to 741 lines — larger than
	// most of the internal/ files above that DO have a budget. The CLI is
	// thin by design (parse flags, wire, delegate; see CLAUDE.md's module
	// map), so a subcommand file crossing these is a signal that logic
	// belongs in an internal package, not that the number should go up.
	// Same ~15% first-time headroom convention as every other entry here.
	"cmd/vmr/cmd_story.go":  850,
	"cmd/vmr/cmd_check.go":  610,
	"cmd/vmr/cmd_report.go": 500,
	"cmd/vmr/cmd_status.go": 370,
	// classify.go had no budget at all before the B1 batch (2026-08-14)
	// extracted internal/jsonscan out of it and internal/adapter/
	// fingerprint.go, shrinking it from 566 lines to ~160 specifically so it
	// would stay a thin error-classification file — a budget-less file can't
	// tell a contributor "you're rebuilding what B1 just moved out" the way
	// a tripped test can. jsonscan's own two files are first-time budgets at
	// the same ~15-20% headroom convention as every other entry here.
	"internal/adapter/classify.go": 200,
	"internal/jsonscan/scan.go":    320,
	"internal/jsonscan/rewrite.go": 300,
	// internal/taskseg's registration was deliberately deferred past B2 (its
	// files were still small enough that a budget would have been a
	// meaningless number) until B3 landed the session/task-boundary
	// algorithm itself into segment.go — the same first-time ~15-20%
	// headroom convention as every other entry here.
	"internal/taskseg/taskseg.go":  70,
	"internal/taskseg/openclaw.go": 150,
	"internal/taskseg/segment.go":  200,
}

// TestArchitecture_CoreFileSizes counts non-blank lines the same way `wc -l`
// does (this test's own budget above was set from that same count) so a
// contributor can reproduce a failure locally without reading this file's
// counting logic first.
func TestArchitecture_CoreFileSizes(t *testing.T) {
	repoRoot := repoRootDir(t)

	for rel, limit := range fileLineLimits {
		path := filepath.Join(repoRoot, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		n := bytes.Count(data, []byte("\n"))
		if n > limit {
			// "another file in the same package", not "under internal/router"
			// as this message used to say — the table has covered report/
			// story/config/cmd files for far longer than it has covered only
			// the router.
			t.Errorf("%s is %d lines, over its %d-line budget: split it "+
				"into another file in the same package, don't just raise "+
				"this number", rel, n, limit)
		}
	}
}
