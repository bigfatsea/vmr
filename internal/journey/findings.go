// Ver 2026-09-21 23:30, by Sonnet 5

// Rule-derived, Step-level "suspect list" findings for a single Journey —
// the same Finding/FindingCode shape internal/report's buildFindings already
// uses for its own efficiency-findings table (stable, never-localized Code; narrative text
// separate; each finding names a suggested Action), applied one level down:
// a report Finding is a row of aggregate statistics, a journey Finding points
// at one specific Step. The two types are deliberately NOT shared —
// internal/archtest forbids internal/journey from depending on internal/report
// (and vice versa), and the two are different shapes anyway (aggregate row
// vs Step-located pointer).
//
// Every detector here is pure rule/structure matching — no LLM call, no
// judgment about WHY something happened, only THAT a structural pattern
// matched. Findings are explicitly a "candidate/suspect list, not a verdict"
// (the journey design specification's
// candidate-list framing): wording is "detected N suspected occurrences, recommend manual
// review", never "the agent made a mistake here".
package journey

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"vmr/internal/i18n"
)

// FindingSource specifies whether a Finding was derived by deterministic rule or LLM inference.
type FindingSource string

const (
	SourceLLMInferred FindingSource = "llm_inferred" // LLM semantic detector inference
)

// FindingConfidence represents the discrete confidence level for LLM-inferred findings.
type FindingConfidence string

const (
	ConfidenceHigh FindingConfidence = "HIGH" // Direct, undeniable textual evidence anchor
)

// Finding is one Step-located, rule-derived or LLM-inferred "worth a second look" flag.
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
	// Source indicates whether this finding was rule-derived or LLM-inferred.
	Source FindingSource `json:"source,omitempty"`
	// Confidence is the discrete confidence level (only populated for LLM-inferred findings).
	Confidence FindingConfidence `json:"confidence,omitempty"`
	// EvidenceAnchor contains a verbatim excerpt from the transcript that triggered the finding.
	EvidenceAnchor string `json:"evidence_anchor,omitempty"`
	// Params carries the raw values that drove this finding (a tool name, a
	// repeat count, a step sequence, a comma-joined entity list — never a
	// pre-formatted or pre-localized string) so a consumer can reconstruct
	// the sentence in any language without recomputing the detector (R1:
	// language is a render-time concern, never baked into the data
	// product — same split as internal/report's Finding.Params). Empty for
	// LLM-inferred findings (Source == SourceLLMInferred) — see LLMLang.
	Params map[string]string `json:"params,omitempty"`
	// LLMLang is the language the LLM was prompted in, set only when
	// Source == SourceLLMInferred. Finding/Evidence/Action for an
	// LLM-inferred finding are the model's own generated text — R1's LLM-
	// original-text exemption: they are NOT re-derivable in another
	// language without a new model call, so they are exempted from the
	// language-invariance requirement rather than reduced to Code+Params,
	// as long as this field says which language they were generated in.
	LLMLang string `json:"llm_lang,omitempty"`
	// Finding/Evidence/Action are narrative text. For a rule-derived
	// finding (Source unset) these are the English baseline, reconstructible
	// from Code+Params — j-<id>.json no longer follows lang for these (R1).
	// For an LLM-inferred finding (Source == SourceLLMInferred) these are
	// the model's own original-language text — see LLMLang above. Either
	// way, j-<id>.md renders its own copy at the actual display language
	// from Code+Params (RenderMarkdownFromSummary), never reading this
	// struct's Finding/Evidence/Action back for the rule-derived case.
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

	// Phase 1b — LLM semantic detectors (see llm_findings.go).
	FindingToolResultMisinterpretation FindingCode = "tool_result_misinterpretation"
	FindingSemanticOscillation         FindingCode = "semantic_oscillation"
	FindingGoalDrift                   FindingCode = "goal_drift"
	FindingUnverifiedCompletionClaim   FindingCode = "unverified_completion_claim"
)

// ComputeFindings runs every detector (Phase 1's five plus Phase 2's four,
// findings_toolresult.go) over j and returns the combined, Step-order-
// sorted candidate list. Selection (which Steps match, which Code, which
// RelatedSeq) never depended on lang; the Finding/Evidence/Action text now
// doesn't either (R1) — this always builds the English baseline plus each
// finding's Params, so j-<id>.json is language-invariant. Markdown rendering
// reconstructs the actually-requested language from Code+Params at render
// time (localizeFinding, called from buildVMFindings) rather than reading
// this call's text back — the same split internal/report's
// buildFindingsForJSON/viewmodel_efficiency.go use.
// TestComputeFindingsIsDeterministic locks in that selection never depends
// on map iteration order either, since j-<id>.json and the rendered
// Markdown must never disagree on WHICH Steps got flagged.
func ComputeFindings(j *Journey) []Finding {
	tx := i18n.JourneyFindings(i18n.EN)
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

// localizeFinding reconstructs f's Finding/Evidence/Action text in lang from
// its Code+Params — the render-time counterpart to each detectXxx's
// construction-time tx.XxxFinding call (ComputeFindings always builds the
// English baseline; this is what lets Markdown show the actually-requested
// language, including when -render-only reads an English-baseline
// j-<id>.json back off disk and renders it in a different language than
// whatever ComputeFindings happened to run with originally).
//
// LLM-inferred findings (Source == SourceLLMInferred) skip re-localization
// — their Finding/Evidence/Action is the model's own generated text (see
// Finding.LLMLang's doc comment), not reconstructible from Params. The LLM
// semantic detector Codes (FindingToolResultMisinterpretation and friends,
// llm_findings.go) only ever appear with that Source set, so the switch
// below never needs a case for them; an unrecognized Code (a future
// addition this function hasn't been taught yet) falls back to the
// persisted text unchanged rather than blanking it.
//
// This function is the one caller that matters for the LLM-inferred
// branch below: it runs only on the Markdown render path
// (viewmodel_spine.go's buildVMFindings), never when building the
// persisted j-<id>.json. That's why Markdown-structure escaping
// (sanitizeMDStruct) happens here rather than at Finding-construction time
// (llm_findings.go) — ComputeLLMFindings' own doc comment and
// KNOWN_ISSUES §1.5 explain why: the JSON stays the model's raw text,
// Markdown gets the escaped copy, and the two concerns (re-localize vs.
// Markdown-escape) are independent — this branch always does the second,
// never the first.
func localizeFinding(f Finding, lang i18n.Lang) Finding {
	if f.Source == SourceLLMInferred {
		f.Evidence = sanitizeMDStruct(f.Evidence)
		f.Action = sanitizeMDStruct(f.Action)
		f.EvidenceAnchor = sanitizeMDStruct(f.EvidenceAnchor)
		return f
	}
	tx := i18n.JourneyFindings(lang)
	var ft i18n.JourneyFindingText
	switch f.Code {
	case FindingExactRepeatToolCall:
		count, _ := strconv.Atoi(f.Params["count"])
		ft = tx.ExactRepeatToolCall(f.Params["tool"], count)
	case FindingNarrationWithoutAction:
		runLen, _ := strconv.Atoi(f.Params["run_len"])
		ft = tx.NarrationWithoutAction(runLen)
	case FindingUnverifiedSuccess:
		errorSeq, _ := strconv.Atoi(f.Params["error_seq"])
		ft = tx.UnverifiedSuccess(errorSeq)
	case FindingReasoningActionMismatch:
		ft = tx.ReasoningActionMismatch(f.Params["entities"])
	case FindingPlanExecutionMisalignment:
		skipped, _ := strconv.Atoi(f.Params["skipped"])
		total, _ := strconv.Atoi(f.Params["total"])
		ft = tx.PlanExecutionMisalignment(skipped, total)
	case FindingUnadaptedRetry:
		ft = tx.UnadaptedRetry(f.Params["tool"])
	case FindingUnusedToolResult:
		ft = tx.UnusedToolResult(f.Params["entities"])
	case FindingUnverifiedEntityReference:
		ft = tx.UnverifiedEntityReference(f.Params["entities"])
	case FindingConstraintTextDropped:
		total, _ := strconv.Atoi(f.Params["total"])
		ft = tx.ConstraintTextDropped(f.Params["entities"], total)
	default:
		return f
	}
	f.Finding, f.Evidence, f.Action = ft.Finding, ft.Evidence, ft.Action
	return f
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

// maxRepeatGap is how many Steps may separate two consecutive occurrences of
// the same (name, args) call while both still count toward one loop run. A
// loop is a temporal phenomenon: real ones re-issue the identical call
// within a step or two of the previous attempt (occasionally interleaving
// one unrelated call); the same call three times spread across a long
// session is working rhythm, not a loop. Without this bound the global
// count flagged exactly those rhythmic repeats (§2.115). Same
// calibration-pending status as exactRepeatThreshold.
const maxRepeatGap = 2

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

func detectExactRepeatToolCall(steps []*Step, tx i18n.JourneyFindingsText) []Finding {
	var out []Finding
	for _, g := range groupToolCallsByKey(steps) {
		// Split the occurrence list into maximal runs of locally-clustered
		// repeats (consecutive gaps <= maxRepeatGap) and fire per run, not
		// per global count.
		runStart := 0
		for i := 1; i <= len(g.Seqs); i++ {
			if i < len(g.Seqs) && g.Seqs[i]-g.Seqs[i-1] <= maxRepeatGap {
				continue
			}
			run := g.Seqs[runStart:i]
			runStart = i
			if len(run) < exactRepeatThreshold {
				continue
			}
			ft := tx.ExactRepeatToolCall(g.Name, len(run))
			last := run[len(run)-1]
			related := append([]int(nil), run[:len(run)-1]...)
			out = append(out, Finding{
				Code: FindingExactRepeatToolCall, StepSeq: last, RelatedSeq: related,
				Finding: ft.Finding, Evidence: ft.Evidence, Action: ft.Action,
				Params: map[string]string{"tool": g.Name, "count": strconv.Itoa(len(run))},
			})
		}
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

func detectNarrationWithoutAction(steps []*Step, tx i18n.JourneyFindingsText) []Finding {
	var out []Finding
	i := 0
	for i < len(steps) {
		if len(steps[i].ToolCalls) != 0 || steps[i].RespText == "" {
			i++
			continue
		}
		runEnd := i + 1
		for runEnd < len(steps) && len(steps[runEnd].ToolCalls) == 0 && steps[runEnd].RespText != "" &&
			jaccardSimilarity(wordSet(steps[runEnd-1].RespText), wordSet(steps[runEnd].RespText)) >= narrationJaccardThreshold {
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
				Params: map[string]string{"run_len": strconv.Itoa(runLen)},
			})
		}
		i = runEnd
	}
	return out
}

func wordSet(s string) map[string]struct{} {
	fields := strings.Fields(strings.ToLower(s))
	set := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		set[f] = struct{}{}
	}
	return set
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
func detectUnverifiedSuccess(j *Journey, tx i18n.JourneyFindingsText) []Finding {
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
					Params: map[string]string{"error_seq": strconv.Itoa(errorSeq)},
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

func cleanEntityForMatch(s string) string {
	s = strings.TrimPrefix(s, "~/")
	s = strings.TrimPrefix(s, "./")
	return s
}

func entityReferenced(e string, actionEntities []string) bool {
	eClean := cleanEntityForMatch(e)
	for _, a := range actionEntities {
		aClean := cleanEntityForMatch(a)
		if a == e || strings.Contains(a, e) || strings.Contains(e, a) ||
			(eClean != "" && (strings.Contains(a, eClean) || strings.Contains(aClean, eClean))) {
			return true
		}
	}
	return false
}

func detectReasoningActionMismatch(steps []*Step, tx i18n.JourneyFindingsText) []Finding {
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
		entities := strings.Join(missing, ", ")
		ft := tx.ReasoningActionMismatch(entities)
		out = append(out, Finding{
			Code: FindingReasoningActionMismatch, StepSeq: s.Seq,
			Finding: ft.Finding, Evidence: ft.Evidence, Action: ft.Action,
			Params: map[string]string{"entities": entities},
		})
	}
	return out
}

// --- plan_execution_misalignment ------------------------------------------

// Plan parsing logic is housed in plan_parse.go (ExtractActionablePlan).
func detectPlanExecutionMisalignment(j *Journey, tx i18n.JourneyFindingsText) []Finding {
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
		items := ExtractActionablePlan(planText)
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
			if planItemExecuted(item.Text, laterEntities, laterLower) {
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
			Params: map[string]string{"skipped": strconv.Itoa(skipped), "total": strconv.Itoa(len(items))},
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
