// Ver 2026-08-05, by Sonnet 5

// Rule-derived, Step-level "suspect list" findings for a single Journey —
// the same Finding/FindingCode shape internal/report's buildFindings already
// uses for its own efficiency-findings table (stable, never-localized Code; narrative text
// separate; each finding names a suggested Action), applied one level down:
// a report Finding is a row of aggregate statistics, a story Finding points
// at one specific Step. The two types are deliberately NOT shared —
// internal/archtest forbids internal/story from depending on internal/report
// (and vice versa), and the two are different shapes anyway (aggregate row
// vs Step-located pointer).
//
// Every detector here is pure rule/structure matching — no LLM call, no
// judgment about WHY something happened, only THAT a structural pattern
// matched. Findings are explicitly a "candidate/suspect list, not a verdict"
// (the story design specification's
// candidate-list framing): wording is "detected N suspected occurrences, recommend manual
// review", never "the agent made a mistake here".
package story

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"vmr/internal/i18n"
)

// Finding is one Step-located, rule-derived "worth a second look" flag.
type Finding struct {
	// Code is a stable, non-localized identifier for programmatic consumption.
	Code FindingCode `json:"code"`
	// StepSeq locates the finding — the Step whose data completed the
	// pattern match (e.g. the Nth repeat, the step that closed the task
	// without verifying).
	StepSeq int `json:"step_seq"`
	// RelatedSeq are earlier Steps the finding's evidence also references
	// (e.g. a repeat's earlier occurrences, an unverified success's
	// triggering error) — 0 or more, not required to be contiguous with
	// StepSeq.
	RelatedSeq []int `json:"related_seq,omitempty"`
	// Finding/Evidence/Action are narrative text, localized per the lang
	// ComputeFindings was called with. journey-<id>.json is always built
	// with i18n.EN (see cmd/vmr/cmd_story.go's writeJourneyFile), matching
	// report's Report2/vmr-report.json convention; a second, target-language
	// call feeds RenderMarkdown's decision-spine annotations.
	Finding  string `json:"finding"`
	Evidence string `json:"evidence,omitempty"`
	Action   string `json:"action,omitempty"`
}

// FindingCode identifies which detector a Finding came from, independent of
// its (localized) display text. See Finding.Code.
type FindingCode string

const (
	FindingExactRepeatToolCall       FindingCode = "exact_repeat_tool_call"
	FindingNarrationWithoutAction    FindingCode = "narration_without_action"
	FindingUnverifiedSuccess         FindingCode = "error_then_unverified_success"
	FindingReasoningActionMismatch   FindingCode = "reasoning_action_mismatch"
	FindingPlanExecutionMisalignment FindingCode = "plan_execution_misalignment"

	// Phase 2 — see findings_toolresult.go for all four detectors.
	FindingUnadaptedRetry            FindingCode = "error_retry_unadapted"
	FindingUnusedToolResult          FindingCode = "unused_tool_result"
	FindingUnverifiedEntityReference FindingCode = "unverified_entity_reference"
	FindingConstraintTextDropped     FindingCode = "constraint_text_dropped_at_compaction"
)

// ComputeFindings runs every detector (Phase 1's five plus Phase 2's four,
// findings_toolresult.go) over j and returns the combined, Step-order-
// sorted candidate list. Selection (which Steps match, which Code, which
// RelatedSeq) never depends on lang — only the Finding/Evidence/Action text
// does; TestComputeFindingsIsDeterministic locks this in the same way
// report's TestBuildFindingsIsDeterministic does for buildFindings, since
// journey-<id>.json (always EN) and the rendered Markdown (target lang)
// must never disagree on WHICH Steps got flagged.
func ComputeFindings(j *Journey, lang i18n.Lang) []Finding {
	tx := i18n.StoryFindings(lang)
	steps := journeySteps(j)

	var out []Finding
	out = append(out, detectExactRepeatToolCall(steps, tx)...)
	out = append(out, detectNarrationWithoutAction(steps, tx)...)
	out = append(out, detectUnverifiedSuccess(j, tx)...)
	out = append(out, detectReasoningActionMismatch(steps, tx)...)
	out = append(out, detectPlanExecutionMisalignment(j, tx)...)
	out = append(out, detectUnadaptedRetry(steps, tx)...)
	out = append(out, detectUnusedToolResult(steps, tx)...)
	out = append(out, detectUnverifiedEntityReference(steps, tx)...)
	out = append(out, detectConstraintTextDropped(steps, tx)...)

	sort.SliceStable(out, func(a, b int) bool {
		if out[a].StepSeq != out[b].StepSeq {
			return out[a].StepSeq < out[b].StepSeq
		}
		return out[a].Code < out[b].Code // stable tie-break: multiple codes on the same Step
	})
	return out
}

// --- exact_repeat_tool_call ---------------------------------------------

// exactRepeatThreshold is how many byte-identical (name, args) occurrences
// of the same tool call trigger a Finding. Calibrated against this repo's
// own real audit corpus (logs/vmr-audit-2026-07-*) — see the calibration
// notes in docs/VirtualModelRouter_Design_v4_Analytics.md's Findings
// section; real-world incidents this detector is modeled on ran into the
// hundreds of repeats before being noticed (anthropics/claude-code#15909),
// so 3 is deliberately an early-warning bar, not a "this is definitely a
// loop" bar — that's why the finding text says "suspected", not "confirmed".
const exactRepeatThreshold = 3

// toolCallGroup is one (name, args) key's full occurrence list across a
// Journey — toolCallRepeats (metrics.go) only tags pairwise repeat/not, this
// groups by the same toolCallKey identity to get a count and the related
// earlier Step numbers a Finding needs.
type toolCallGroup struct {
	Name string
	Seqs []int // occurrence order
}

func groupToolCallsByKey(steps []*Step) []toolCallGroup {
	idx := map[string]int{}
	var groups []toolCallGroup
	for _, s := range steps {
		for _, tc := range s.ToolCalls {
			key := toolCallKey(tc)
			if gi, ok := idx[key]; ok {
				groups[gi].Seqs = append(groups[gi].Seqs, s.Seq)
			} else {
				idx[key] = len(groups)
				groups = append(groups, toolCallGroup{Name: tc.Name, Seqs: []int{s.Seq}})
			}
		}
	}
	return groups
}

func detectExactRepeatToolCall(steps []*Step, tx i18n.StoryFindingsText) []Finding {
	var out []Finding
	for _, g := range groupToolCallsByKey(steps) {
		if len(g.Seqs) < exactRepeatThreshold {
			continue
		}
		ft := tx.ExactRepeatToolCall(g.Name, len(g.Seqs))
		last := g.Seqs[len(g.Seqs)-1]
		related := append([]int(nil), g.Seqs[:len(g.Seqs)-1]...)
		out = append(out, Finding{
			Code: FindingExactRepeatToolCall, StepSeq: last, RelatedSeq: related,
			Finding: ft.Finding, Evidence: ft.Evidence, Action: ft.Action,
		})
	}
	return out
}

// --- narration_without_action ---------------------------------------------

// narrationMinRun/narrationJaccardThreshold: how many consecutive tool-call-
// free Steps, each pairwise-similar enough to its predecessor by Jaccard
// word-set overlap, count as "circling the same restated intent" rather
// than genuinely different plain-text turns (e.g. a multi-turn clarifying
// conversation, which should NOT trigger this). Same calibration-pending
// status as exactRepeatThreshold — see the calibration notes.
const (
	narrationMinRun           = 3
	narrationJaccardThreshold = 0.5
)

func detectNarrationWithoutAction(steps []*Step, tx i18n.StoryFindingsText) []Finding {
	var out []Finding
	i := 0
	for i < len(steps) {
		if len(steps[i].ToolCalls) != 0 || steps[i].RespText == "" {
			i++
			continue
		}
		runEnd := i + 1
		for runEnd < len(steps) && len(steps[runEnd].ToolCalls) == 0 && steps[runEnd].RespText != "" &&
			jaccardSim(wordSet(steps[runEnd-1].RespText), wordSet(steps[runEnd].RespText)) >= narrationJaccardThreshold {
			runEnd++
		}
		runLen := runEnd - i
		if runLen >= narrationMinRun {
			var related []int
			for k := i; k < runEnd-1; k++ {
				related = append(related, steps[k].Seq)
			}
			ft := tx.NarrationWithoutAction(runLen)
			out = append(out, Finding{
				Code: FindingNarrationWithoutAction, StepSeq: steps[runEnd-1].Seq, RelatedSeq: related,
				Finding: ft.Finding, Evidence: ft.Evidence, Action: ft.Action,
			})
		}
		i = runEnd
	}
	return out
}

func wordSet(s string) map[string]bool {
	fields := strings.Fields(strings.ToLower(s))
	set := make(map[string]bool, len(fields))
	for _, f := range fields {
		set[f] = true
	}
	return set
}

func jaccardSim(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// --- error_then_unverified_success -----------------------------------------

// verificationLikeToolRe is a deliberately local, self-limited heuristic —
// NOT the general read/write tool classifier the dev plan decided
// not to build. It only asks "does this tool's name look like it re-checks
// state", scoped to this one detector; a false negative here just means the
// detector stays silent, which is the safe failure direction.
var verificationLikeToolRe = regexp.MustCompile(`(?i)read|get|list|check|verify|view|stat|cat|show|fetch|status`)

func looksLikeVerification(s *Step) bool {
	for _, tc := range s.ToolCalls {
		if verificationLikeToolRe.MatchString(tc.Name) {
			return true
		}
	}
	return false
}

// detectUnverifiedSuccess runs a small per-Task state machine: an
// is_error-marked tool_result arms "unverified"; a subsequent call whose
// name looks read/verification-shaped disarms it; if the Task's LAST Step
// still carries a Finish (the model considers the turn done) while still
// armed, that's the candidate — an error was seen and the task ended
// without anything that looked like a check in between.
func detectUnverifiedSuccess(j *Journey, tx i18n.StoryFindingsText) []Finding {
	var out []Finding
	for _, task := range j.Tasks {
		unverified := false
		errorSeq := 0
		for i, s := range task.Steps {
			for _, ev := range s.NewEvents {
				if strings.Contains(ev.Msg.Text, isErrorMarker) {
					unverified = true
					errorSeq = s.Seq
					break
				}
			}
			if unverified && looksLikeVerification(s) {
				unverified = false
			}
			isLastOfTask := i == len(task.Steps)-1
			if unverified && isLastOfTask && s.Finish != "" {
				ft := tx.UnverifiedSuccess(errorSeq)
				out = append(out, Finding{
					Code: FindingUnverifiedSuccess, StepSeq: s.Seq, RelatedSeq: []int{errorSeq},
					Finding: ft.Finding, Evidence: ft.Evidence, Action: ft.Action,
				})
			}
		}
	}
	return out
}

// --- reasoning_action_mismatch --------------------------------------------
//
// Calibrated against this repo's own real audit corpus
// (logs/vmr-audit-2026-07-25/26/27) — see the calibration notes in
// docs/VirtualModelRouter_Design_v4_Analytics.md's Findings section. The
// first version (whole-Reasoning-text entity diff, exact string match)
// false-positived on essentially every real multi-step reasoning block:
// (a) reasoning routinely narrates a numbered PLAN across several
// upcoming files/URLs ("1. check X, 2. check Y, 3. check Z"), and only
// the current Step's call touches one of them — the others aren't a
// mismatch, they're later steps; (b) the same real file path gets
// captured as two different entity strings depending on where the regex's
// word-boundary happened to start (reasoning's "~/.hermes/SOUL.md" scans
// as "hermes/SOUL.md", the tool call's "/Users/x/.hermes/SOUL.md" scans as
// "Users/x/.hermes/SOUL.md" — same file, no shared exact string). Two
// fixes address both: only the LAST sentence of the reasoning (the
// immediate justification for THIS action, not the whole plan) is
// scanned, and entity matching is substring-tolerant in either direction
// instead of requiring an exact string.

// reasoningMinChars: below this, a short reasoning blurb ("Let me check
// that file") mentioning an entity is too weak a signal to flag — this
// detector wants a reasoning passage substantial enough that naming a
// specific file/URL and then not touching it in the call is meaningfully
// surprising, not routine.
const reasoningMinChars = 40

// maxEntitiesShown caps how many entities a Finding's text names — a
// triage aid, not an exhaustive diff. Shared by every detector in this
// package (and findings_toolresult.go) that lists entities in its Finding
// text, not just this one.
const maxEntitiesShown = 3

// sentenceSplitRe splits on common EN/CJK sentence terminators — used only
// to isolate the reasoning's LAST sentence (see the detector's calibration
// note above), not for any linguistic analysis. CJK terminators (。！？)
// and newlines split unconditionally; the EN terminators (.!?) only split
// when followed by whitespace, so a filename like "config.go" (a period
// with no trailing space) is never mistaken for a sentence boundary — the
// first version of this regex did exactly that and silently chopped every
// dotted filename in half.
var sentenceSplitRe = regexp.MustCompile(`[。！？\n]+|[.!?]+\s+`)

func lastSentence(s string) string {
	parts := sentenceSplitRe.Split(strings.TrimSpace(s), -1)
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.TrimSpace(parts[i]) != "" {
			return parts[i]
		}
	}
	return s
}

// entityReferenced reports whether e (from the reasoning side) matches any
// action-side entity, tolerating either being a substring of the other —
// see the detector's calibration note above for why exact equality
// false-positives on real paths.
func entityReferenced(e string, actionEntities []string) bool {
	for _, a := range actionEntities {
		if a == e || strings.Contains(a, e) || strings.Contains(e, a) {
			return true
		}
	}
	return false
}

func detectReasoningActionMismatch(steps []*Step, tx i18n.StoryFindingsText) []Finding {
	var out []Finding
	for _, s := range steps {
		if len(s.ToolCalls) == 0 || s.Reasoning == "" {
			continue
		}
		reasoningText := lastSentence(s.Reasoning)
		if len([]rune(reasoningText)) < reasoningMinChars {
			continue
		}
		reasoningEntities := extractEntities(reasoningText)
		if len(reasoningEntities) == 0 {
			continue
		}
		var actionText strings.Builder
		for _, tc := range s.ToolCalls {
			actionText.WriteString(tc.Args)
			actionText.WriteByte(' ')
		}
		actionEntities := extractEntities(actionText.String())
		var missing []string
		for _, e := range reasoningEntities {
			if !entityReferenced(e, actionEntities) {
				missing = append(missing, e)
			}
		}
		if len(missing) == 0 {
			continue
		}
		missing = capEntities(missing)
		ft := tx.ReasoningActionMismatch(strings.Join(missing, ", "))
		out = append(out, Finding{
			Code: FindingReasoningActionMismatch, StepSeq: s.Seq,
			Finding: ft.Finding, Evidence: ft.Evidence, Action: ft.Action,
		})
	}
	return out
}

// --- plan_execution_misalignment ------------------------------------------

// numberedListRe matches "1. " / "1、" style list items, capturing the
// leading number itself (group 1) alongside the item text (group 2) — the
// number is what lastNumberedList uses to tell separate lists apart. The
// detector's own self-limitation (design doc: "自我限定为字符串/实体匹配，
// 不做语义理解"): a Task whose opening reasoning doesn't use this exact
// format is silently skipped, never analyzed by looser heuristics.
var numberedListRe = regexp.MustCompile(`(?m)^\s*(\d+)[.、]\s*(.+)$`)

// lastNumberedList extracts item text from the LAST contiguous numbered
// list in text — calibrated against real corpus data (logs/vmr-audit-
// 2026-07-27/28): reasoning routinely enumerates more than one list in the
// same turn (e.g. "here's how I read the request: 1. ... 2. ... 3. ... 4.
// ..." — a topic breakdown, not a plan — immediately followed by "let me
// plan the approach: 1. ... 2. ... 3. ... 4. ..." — the actual plan). The
// first version matched every numbered line in the whole text and treated
// them as one 8-item plan, which inflated "unmatched" counts against
// items that were never meant to be executed in the first place. A run
// ends and a new one begins whenever the leading number doesn't continue
// increasing from the previous match — most commonly, it restarts at 1 —
// and only the LAST run is treated as the plan actually being acted on.
func lastNumberedList(text string) []string {
	all := numberedListRe.FindAllStringSubmatch(text, -1)
	if len(all) == 0 {
		return nil
	}
	var runs [][]string
	var cur []string
	prevN := 0
	for _, m := range all {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			n = 0
		}
		if len(cur) > 0 && n <= prevN {
			runs = append(runs, cur)
			cur = nil
		}
		cur = append(cur, m[2])
		prevN = n
	}
	if len(cur) > 0 {
		runs = append(runs, cur)
	}
	return runs[len(runs)-1]
}

// minPlanItems: a single numbered sentence embedded in prose isn't "a plan"
// in the sense this detector cares about; two or more items is the bar for
// "the model laid out a multi-step plan".
const minPlanItems = 2

// maxPlanItems caps how large a numbered list this detector treats as an
// actionable plan at all — calibrated against real corpus data
// (logs/vmr-audit-2026-07-1x): past ~8 items, "every single item is
// unmatched" started dominating the hits, and reading the actual Steps
// showed why — a long numbered list is far more often a written report,
// strategic essay, or table-of-contents (never meant to be executed via
// tool calls turn-by-turn) than a real step-by-step execution plan. A
// short list with a partial match (e.g. 1 of 4 items missing) is the
// useful signal; "20 of 20 missing" from a 20-item document is not a
// suspected execution gap, it's this detector applied outside its scope.
const maxPlanItems = 8

func detectPlanExecutionMisalignment(j *Journey, tx i18n.StoryFindingsText) []Finding {
	var out []Finding
	for _, task := range j.Tasks {
		if len(task.Steps) == 0 {
			continue
		}
		first := task.Steps[0]
		planText := first.Reasoning
		if planText == "" {
			planText = first.RespText
		}
		items := lastNumberedList(planText)
		if len(items) < minPlanItems || len(items) > maxPlanItems {
			continue
		}

		// laterText starts from first's OWN tool calls (not its Reasoning/
		// RespText, already consumed as planText) — a plan item can be
		// executed in the SAME turn it was announced in, and the first
		// version of this detector only scanned Steps[1:], flagging
		// same-turn execution as "never referenced again" purely because
		// it never looked at the turn's own action (a calibration
		// regression found against logs/vmr-audit-2026-07-27's real
		// corpus).
		var laterText strings.Builder
		for _, tc := range first.ToolCalls {
			laterText.WriteString(tc.Args)
			laterText.WriteByte(' ')
		}
		for _, s := range task.Steps[1:] {
			laterText.WriteString(s.RespText)
			laterText.WriteByte(' ')
			for _, tc := range s.ToolCalls {
				laterText.WriteString(tc.Args)
				laterText.WriteByte(' ')
			}
		}
		laterEntities := map[string]bool{}
		for _, e := range extractEntities(laterText.String()) {
			laterEntities[e] = true
		}
		laterLower := strings.ToLower(laterText.String())

		skipped := 0
		for _, item := range items {
			if planItemExecuted(item, laterEntities, laterLower) {
				continue
			}
			skipped++
		}
		if skipped == 0 {
			continue
		}
		ft := tx.PlanExecutionMisalignment(skipped, len(items))
		out = append(out, Finding{
			Code: FindingPlanExecutionMisalignment, StepSeq: first.Seq,
			Finding: ft.Finding, Evidence: ft.Evidence, Action: ft.Action,
		})
	}
	return out
}

// planItemExecuted checks a plan item against the Task's later steps by
// (1) shared file-path/URL-shaped entities, falling back to (2) a plain
// keyword substring check for items with no extractable entity — both are
// string/entity matching, never semantic understanding, per this
// detector's own self-limitation.
func planItemExecuted(item string, laterEntities map[string]bool, laterLower string) bool {
	for _, e := range extractEntities(item) {
		if laterEntities[e] {
			return true
		}
	}
	for _, w := range significantWords(item) {
		if strings.Contains(laterLower, w) {
			return true
		}
	}
	return false
}

// significantWords is a rough keyword fallback for plan items with no
// extractEntities hit — lowercased words of at least minSignificantWordLen
// runes, since short words (verbs like "run"/"fix") match too much noise to
// be a useful signal on their own.
const minSignificantWordLen = 4

var wordSplitRe = regexp.MustCompile(`[^\p{L}\p{N}_./-]+`)

func significantWords(s string) []string {
	var out []string
	for _, w := range wordSplitRe.Split(strings.ToLower(s), -1) {
		if len([]rune(w)) >= minSignificantWordLen {
			out = append(out, w)
		}
	}
	return out
}
