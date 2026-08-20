// Ver 2026-08-20 17:40, by Sonnet 5

// vmr analyze: the "one analysis verb" (P6.5, architecture doc §7.9) —
// default output is a fully navigable suite (vmr-report.md + vmr-requests.md
// + stories/vmr-stories.md + every rendered journey), from a SINGLE
// invocation with a SINGLE output directory, so "the two commands' outputs
// didn't land in the same place" (the everyday papercut §7.9 names) stops
// being something the user has to remember to avoid.
//
// Implementation choice, made deliberately narrower than the architecture
// doc's most ambitious framing ("one scan, one cache, one graph build"):
// this is `cmdReport` then `cmdStory` called in sequence with a shared -o,
// NOT a deep refactor that threads one shared *ctxgraph.Graph through both
// packages' aggregation. Reasoning: P3 already put a content-hash-sharded
// parse cache (.parse-cache/) under both commands, shared. By the time
// `cmdStory` runs here, every file `cmdReport` just scanned has a hot cache
// entry — ScanCached's second pass is a cache-hit read, not a re-parse. The
// wall-clock gap between "true single scan" and "two passes, second one
// fully cached" is therefore small (see the architecture doc §7.10's own
// numbers: hot-cache story is already 5x faster than cold), while a real
// merge — splitting AnalyzeSessionsCached's scan from its own construction,
// making internal/report and internal/story both accept a pre-built Graph —
// touches both packages' core aggregation paths for a marginal win. This
// command still delivers the actual user-facing promise (one call, one
// output root, guaranteed) at a fraction of the risk. If a real bottleneck
// shows up in practice, revisit with real numbers then — the same
// discipline §7.10 itself applied to the "merge passes" question.
package main

import (
	"flag"
	"fmt"
)

func cmdAnalyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	configPath := fs.String("c", "config.yaml", "config file to resolve log_dir from (when no input files are given) and to resolve pricing from")
	outDirFlag := fs.String("o", "", "output directory (default: ./reports, or report.yaml's output) — shared by both halves of the suite")
	detailsFlag := fs.Bool("details", false, "also render one Markdown file per request into {out}/details/ (default: false — details are available on demand regardless, via each index's computed filename or `vmr replay -print -req <coord>`)")
	includePartialFlag := fs.Bool("include-partial", false, "also render journeys whose head looks truncated by the loaded file range")
	includeSelfTraffic := fs.Bool("include-self-traffic", false, "don't exclude vmr story -llm-addr's own self-analysis traffic (default: excluded, P6.4)")
	langFlag := fs.String("lang", "", "output language: en|zh (default: report.yaml's language, or en)")
	currencyFlag := fs.String("currency", "", "display currency for $ cost estimates, e.g. CNY|JPY")
	reportConfigPath := fs.String("report-config", "", "vmr report/vmr story sidecar config yaml; absent => auto-load ./report.yaml if present")
	if err := fs.Parse(args); err != nil {
		return err
	}
	files := fs.Args()

	common := func() []string {
		a := []string{"-c", *configPath}
		if *outDirFlag != "" {
			a = append(a, "-o", *outDirFlag)
		}
		if *langFlag != "" {
			a = append(a, "-lang", *langFlag)
		}
		if *reportConfigPath != "" {
			a = append(a, "-report-config", *reportConfigPath)
		}
		if *includeSelfTraffic {
			a = append(a, "-include-self-traffic")
		}
		return a
	}

	reportArgs := common()
	if *detailsFlag {
		reportArgs = append(reportArgs, "-details")
	}
	if *currencyFlag != "" {
		reportArgs = append(reportArgs, "-currency", *currencyFlag)
	}
	reportArgs = append(reportArgs, files...)

	// -render-all is not optional here (unlike bare `vmr story`, which
	// stays cheap-by-default for quick listing): analyze's whole point is
	// a navigable suite, and the "session row -> journey" / "journey ->
	// detail" edges (P6.2) only resolve to real files once journeys are
	// actually rendered.
	storyArgs := append(common(), "-render-all")
	if *includePartialFlag {
		storyArgs = append(storyArgs, "-include-partial")
	}
	storyArgs = append(storyArgs, files...)

	// Order matters here, and it's not arbitrary: report.Markdown links
	// to stories/vmr-stories.md when that file already exists at the
	// time it renders (loadStoriesLink, P6.2a) — the architecture doc
	// calls this edge out by name as the single biggest gap in the
	// navigation matrix ("今天命中数为 0"). Running story first means a
	// FIRST-EVER `analyze` call already gets that edge right, not just
	// the second run onward. The tradeoff: journey-*.md's own "-> back
	// to vmr-report.md" link (P6.2d) then lags by one run instead (it
	// stats a vmr-report.md that doesn't exist yet on this first call) —
	// judged the lesser edge to leave momentarily dark, since it's a
	// convenience backlink, not a new landing page.
	if err := cmdStory(storyArgs); err != nil {
		return fmt.Errorf("analyze (story half): %w", err)
	}
	if err := cmdReport(reportArgs); err != nil {
		return fmt.Errorf("analyze (report half): %w", err)
	}
	return nil
}
