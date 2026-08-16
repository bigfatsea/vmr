// Ver 2026-08-17 00:20, by Claude Sonnet 5

// Phase 1b REAL offline calibration harness for the 6 LLM semantic
// detectors (internal/story/llm_findings.go).
//
// This replaces an earlier version of this file that only *looked* like a
// calibration script: it mocked the LLM's HTTP response with a hand-written
// "correct answer" and re-parsed that same string with a hand-rolled copy of
// the threshold logic, never calling story.ComputeLLMFindings (the actual
// production entry point) at all. See docs/future-strategy/
// phase1b_implementation_plan_gemini-3.7-flash.md §7.2 for the full account
// of what was wrong with it.
//
// This version calls the real production path: story.ComputeLLMFindings
// against real Journeys reconstructed from real production audit logs
// (logs/vmr-audit-*.jsonl[.zst], the same files vmr story already reads),
// through the same -llm-addr/-llm-model config vmr story -compare/-journey
// uses — an already-running VMR instance, no separate wiring.
//
// What it deliberately does NOT do: fabricate a Precision/Recall number.
// Whether a HIGH-confidence Finding is actually correct is a judgment call
// only a human reading the transcript can make — a script pretending
// otherwise is exactly the mistake this file replaces. What it DOES verify
// mechanically, with no human input needed: every HIGH-confidence Finding's
// EvidenceAnchor must be a literal substring somewhere in that Journey's
// real recorded transcript (its audit.Record's own JSON) — an anchor that
// isn't is a fabricated citation, full stop, and that check needs no ground
// truth to run. It prints every fired Finding in full so a human can
// spot-check Precision by reading it against real transcripts.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/story"
	"vmr/internal/taskseg"
)

func main() {
	addr := flag.String("llm-addr", "", "host:port of an already-running VMR instance — same flag vmr story -llm-addr takes")
	model := flag.String("llm-model", "agent", "that instance's virtual model name, sent verbatim — same as vmr story -llm-model")
	key := flag.String("llm-key", "", "bearer token, only if that instance has api_keys configured — same as vmr story -llm-key")
	input := flag.String("input", "logs/vmr-audit-*.jsonl*", "glob of real audit log files to sample Journeys from")
	limit := flag.Int("limit", 12, "max Journeys to run through the 6 detectors (each Journey can cost up to 6 LLM calls)")
	minSteps := flag.Int("min-steps", 4, "skip Journeys with fewer than this many Steps — too short to exercise the detectors")
	langFlag := flag.String("lang", "zh", "en|zh — prompt/report language")
	flag.Parse()

	if *addr == "" {
		fmt.Fprintln(os.Stderr, "error: -llm-addr is required — point it at an already-running VMR instance, exactly as `vmr story -llm-addr` does (this script never auto-starts one)")
		os.Exit(2)
	}
	lang := i18n.EN
	if *langFlag == "zh" {
		lang = i18n.ZH
	}

	paths, err := filepath.Glob(*input)
	if err != nil || len(paths) == 0 {
		fmt.Fprintf(os.Stderr, "error: no files matched -input %q: %v\n", *input, err)
		os.Exit(2)
	}

	fmt.Printf("=== VMR Phase 1b REAL Calibration Run ===\n")
	fmt.Printf("scanning %d file(s) matching %q...\n", len(paths), *input)
	g, err := ctxgraph.Scan(paths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: scan: %v\n", err)
		os.Exit(1)
	}

	var journeys []*story.Journey
	for _, l := range g.Lineages {
		j, err := story.Build(l, taskseg.Generic, lang)
		if err != nil || j.Partial || countSteps(j) < *minSteps {
			continue
		}
		journeys = append(journeys, j)
		if len(journeys) >= *limit {
			break
		}
	}
	if len(journeys) == 0 {
		fmt.Fprintf(os.Stderr, "error: no non-partial Journey with >= %d steps found among %d lineage(s) — widen -input or lower -min-steps\n", *minSteps, len(g.Lineages))
		os.Exit(1)
	}
	fmt.Printf("%d lineage(s) scanned, %d Journey(s) sampled for calibration\nLLM endpoint: %s (model=%s)\n\n", len(g.Lineages), len(journeys), *addr, *model)

	opts := story.LLMOptions{Addr: *addr, Model: *model, APIKey: *key}

	total, anchorValid := 0, 0
	byCode := map[story.FindingCode]int{}
	for _, j := range journeys {
		findings, err := story.ComputeLLMFindings(context.Background(), j, opts, lang)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: Journey %s: ComputeLLMFindings: %v\n", j.ID, err)
			continue
		}
		if len(findings) == 0 {
			continue
		}
		pool := transcriptPool(j)
		for _, f := range findings {
			total++
			byCode[f.Code]++
			valid := f.EvidenceAnchor != "" && strings.Contains(pool, f.EvidenceAnchor)
			if valid {
				anchorValid++
			}
			fmt.Printf("--- Journey %s | Step %d | %s | confidence=%s | anchor_valid=%v ---\n", j.ID, f.StepSeq, f.Code, f.Confidence, valid)
			fmt.Printf("  Finding:  %s\n", f.Finding)
			if f.Evidence != "" {
				fmt.Printf("  Evidence: %s\n", f.Evidence)
			}
			fmt.Printf("  Anchor:   %s\n", f.EvidenceAnchor)
			if f.Action != "" {
				fmt.Printf("  Action:   %s\n", f.Action)
			}
			fmt.Println()
		}
	}

	fmt.Printf("=== Summary ===\n")
	fmt.Printf("Journeys sampled: %d | HIGH-confidence LLM findings fired: %d\n", len(journeys), total)
	for _, code := range sortedCodes(byCode) {
		fmt.Printf("  %-42s %d\n", code, byCode[code])
	}
	anchorRate := 100.0
	if total > 0 {
		anchorRate = 100.0 * float64(anchorValid) / float64(total)
	}
	fmt.Printf("\nEvidence Anchor Validity (mechanical — literal substring check against the real transcript): %.1f%% (%d/%d)\n", anchorRate, anchorValid, total)
	if total > 0 && anchorValid < total {
		fmt.Printf("WARNING: %d finding(s) cite an anchor NOT found verbatim in the transcript — that's a fabricated citation regardless of whether the underlying judgment is right; investigate the prompt/model before trusting this detector.\n", total-anchorValid)
	}
	if total == 0 {
		fmt.Printf("\nNo detector fired on any sampled Journey — that's not itself a pass or a fail; it may just mean this sample had nothing to flag. Widen -input/-limit or point at logs known to contain a suspected issue if you need a positive case to inspect.\n")
	}
	fmt.Printf("\nNOTE: Precision/Recall are intentionally not computed here. They require a human to read each finding above against its Journey and judge whether it's actually correct — that judgment can't be scripted, and a script that fabricates one is worse than no number at all.\n")
}

func countSteps(j *story.Journey) int {
	n := 0
	for _, t := range j.Tasks {
		n += len(t.Steps)
	}
	return n
}

// transcriptPool concatenates a Journey's already-reconstructed text fields
// (RespText, Reasoning, ToolCall args, every NewEvents message) into one
// searchable blob — the "original text" an EvidenceAnchor must be a literal
// substring of. This deliberately does NOT just re-marshal each Step's raw
// audit.Record: a streamed response's text arrives as many small SSE
// "delta.content" fragments, so a real, faithfully-quoted multi-word phrase
// almost never survives as one contiguous run in the raw JSON — only in the
// already-reassembled RespText/Reasoning fields story.Build produces. The
// raw record IS still marshaled and appended too, since tool_result text
// (delivered as one complete string in the FOLLOWING step's request body,
// never streamed) and raw tool-call arguments are only visible there.
func transcriptPool(j *story.Journey) string {
	var b strings.Builder
	for _, t := range j.Tasks {
		for _, s := range t.Steps {
			b.WriteString(s.RespText)
			b.WriteByte('\n')
			b.WriteString(s.Reasoning)
			b.WriteByte('\n')
			for _, tc := range s.ToolCalls {
				b.WriteString(tc.Args)
				b.WriteByte('\n')
			}
			for _, ev := range s.NewEvents {
				b.WriteString(ev.Msg.Text)
				b.WriteByte('\n')
			}
			if s.Rec != nil {
				if data, err := json.Marshal(s.Rec); err == nil {
					b.Write(data)
					b.WriteByte('\n')
				}
			}
		}
	}
	return b.String()
}

func sortedCodes(m map[story.FindingCode]int) []story.FindingCode {
	out := make([]story.FindingCode, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
