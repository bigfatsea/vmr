// Ver 2026-09-03, by pi-agent

package story

import (
	"context"
	"fmt"
	"os"
	"sync"

	"vmr/internal/i18n"
)

// llmFindingsBudget caps ComputeLLMFindings' total wall time across all six
// detectors. Each detector's own HTTP call is already bounded by
// llmHTTPTimeout, but serially those compound to ~6× that on a slow or
// unreachable -llm-addr; running the detectors concurrently plus this
// budget makes the whole call's worst case one detector timeout plus
// slack, not six. A var (not a const) only so tests can shrink it — never
// tuned at runtime.
var llmFindingsBudget = 2 * llmHTTPTimeout

// llmDetector is one ComputeLLMFindings detector in runnable form — a name
// for the skipped-detector stderr line, plus the detectLLM* function
// itself. All detectors are fail-open: they return nil findings, never an
// error.
type llmDetector struct {
	name string
	run  func(context.Context, *Journey, LLMOptions, i18n.Lang) []Finding
}

// llmDetectors is the fixed detector roster. Order here is only the merge
// order into the raw list — the final findings order is decided by the
// sort.SliceStable at the end of ComputeLLMFindings, so concurrent
// execution cannot reorder output.
var llmDetectors = []llmDetector{
	{"tool_result_misinterpretation", detectLLMToolResultMisinterpretation},
	{"semantic_oscillation", detectLLMSemanticOscillation},
	{"goal_drift", detectLLMGoalDrift},
	{"constraint_dropped", detectLLMConstraintDropped},
	{"plan_misalignment", detectLLMPlanMisalignment},
	{"unverified_completion_claim", detectLLMUnverifiedCompletionClaim},
}

// runDetectorsConcurrently runs every detector in llmDetectors in parallel
// under one total time budget and merges their findings. Detectors are
// independent — each builds its own prompt and makes its own Interpret
// call, no shared mutable state — so the only synchronization is the
// result slot each goroutine writes, one per detector. When the budget
// expires, Interpret's ctx select unwinds every still-running detector;
// each goroutine records whether its detector was still running at cancel
// time (ctx.Err() checked immediately after its own return — the honest
// per-detector signal), and an interrupted detector is reported on stderr
// rather than silently shrinking the findings list: "empty because the
// budget cut it" must never be mistaken for "legitimately found nothing".
func runDetectorsConcurrently(ctx context.Context, j *Journey, opts LLMOptions, lang i18n.Lang) []Finding {
	ctx, cancel := context.WithTimeout(ctx, llmFindingsBudget)
	defer cancel()

	type detectorResult struct {
		findings    []Finding
		interrupted bool
	}
	results := make([]detectorResult, len(llmDetectors))
	var wg sync.WaitGroup
	for i, d := range llmDetectors {
		wg.Add(1)
		go func(i int, d llmDetector) {
			defer wg.Done()
			findings := d.run(ctx, j, opts, lang)
			results[i] = detectorResult{findings: findings, interrupted: ctx.Err() != nil}
		}(i, d)
	}
	wg.Wait()

	var raw []Finding
	for i, d := range llmDetectors {
		raw = append(raw, results[i].findings...)
		if results[i].interrupted {
			fmt.Fprintf(os.Stderr, "warning: journey %s: LLM detector %s skipped (total budget %s expired)\n",
				j.ID, d.name, llmFindingsBudget)
		}
	}
	return raw
}
