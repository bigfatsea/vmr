// Ver 2026-07-26, by Sonnet 5
package archtest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	"internal/report/aggregate.go":  1000,
	"internal/report/render_doc.go": 400,
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
}

// TestArchitecture_CoreFileSizes counts non-blank lines the same way `wc -l`
// does (this test's own budget above was set from that same count) so a
// contributor can reproduce a failure locally without reading this file's
// counting logic first.
func TestArchitecture_CoreFileSizes(t *testing.T) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	repoRoot := filepath.Dir(strings.TrimSpace(string(out)))

	for rel, limit := range fileLineLimits {
		path := filepath.Join(repoRoot, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		n := bytes.Count(data, []byte("\n"))
		if n > limit {
			t.Errorf("%s is %d lines, over its %d-line budget: split it "+
				"into another file under internal/router, don't just raise "+
				"this number", rel, n, limit)
		}
	}
}
