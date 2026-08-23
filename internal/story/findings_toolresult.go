// Ver 2026-08-05, by Sonnet 5

// Phase 2's four Finding detectors — all built on I1
// (chatmsg.ToolResultList), the tool_call↔tool_result precise-pairing
// primitive findings.go's Phase 1 detectors didn't need (they only ever
// asked "did an error marker appear somewhere in this Step's new content",
// never "which specific call errored, what did it actually say"). Split
// into its own file rather than appended to findings.go — see
// internal/archtest/file_sizes_test.go's budget notes: findings.go was
// already near its Phase-1 budget before this batch existed.
package story

import (
	"regexp"
	"strings"

	"vmr/internal/chatmsg"
	"vmr/internal/i18n"
)

// toolResultsFor returns the ToolResult entries answering steps[i]'s own
// ToolCalls. Looked up from the FOLLOWING step's request body rather than
// steps[i]'s own — a well-formed protocol turn can't get a new response
// without its pending tool_calls being answered first (the same causal
// guarantee chatmsg.CheckToolPairing's F9 invariant rests on), so the
// answering tool_results always show up as part of the next request's
// history. Returns nil for a Step with no ToolCalls, or the Journey's last
// Step (no following request to look in — its calls' results, if any,
// aren't visible in what was recorded).
//
// Matches in two passes — exact id first, then chatmsg.NormalizeToolCallID's
// underscore-stripped form — and, on a normalized-only match, rewrites the
// returned ToolResult.CallID back to the ORIGINAL steps[i].ToolCalls[].ID
// (never the possibly-stripped id the client echoed). Every caller —
// render_spine_step.go's byID lookup and this file's three Finding
// detectors' errored[tc.ID]-style lookups — keys off the calling Step's own
// tc.ID, so this rewrite is what lets every one of them work unchanged: the
// normalization is fully contained here.
func toolResultsFor(steps []*Step, i int) []chatmsg.ToolResult {
	if len(steps[i].ToolCalls) == 0 || i+1 >= len(steps) || steps[i+1].Rec == nil {
		return nil
	}
	exact := make(map[string]bool, len(steps[i].ToolCalls))
	byNorm := make(map[string]string, len(steps[i].ToolCalls)) // normalized id -> original tc.ID
	for _, tc := range steps[i].ToolCalls {
		exact[tc.ID] = true
		byNorm[chatmsg.NormalizeToolCallID(tc.ID)] = tc.ID
	}
	body, _ := steps[i+1].Rec.Client.Request.Body.(map[string]any)
	var out []chatmsg.ToolResult
	for _, r := range chatmsg.ToolResultList(chatmsg.RawArray(body)) {
		if exact[r.CallID] {
			out = append(out, r)
			continue
		}
		if orig, ok := byNorm[chatmsg.NormalizeToolCallID(r.CallID)]; ok {
			r.CallID = orig
			out = append(out, r)
		}
	}
	return out
}

// --- error_retry_unadapted (a refinement of ErrorRecoveryCount) -----------

// retryLookaheadSteps bounds how far forward this detector looks for a
// same-tool retry after an error — a retry ten Steps later, with a lot of
// other work in between, is a different situation than an immediate
// verbatim re-issue; this stays a "right after the error" signal, not a
// general "was this tool ever retried" scan.
const retryLookaheadSteps = 5

// firstSameNameCall returns the first ToolCall named name in
// steps[from:to] and the Step that issued it, or (nil, nil) if none.
func firstSameNameCall(steps []*Step, from, to int, name string) (*Step, *chatmsg.ToolCall) {
	for j := from; j < to; j++ {
		for k := range steps[j].ToolCalls {
			if steps[j].ToolCalls[k].Name == name {
				return steps[j], &steps[j].ToolCalls[k]
			}
		}
	}
	return nil, nil
}

// detectUnadaptedRetry is 5.3's Step-level, precisely-paired refinement of
// the existing Journey-level ErrorRecoveryCount metric: ErrorRecoveryCount
// only asks "did the agent issue ANY tool call after an error marker
// appeared somewhere in the Step" (a proxy for "tried to recover"); this
// asks the sharper question "was the very next same-tool retry a
// byte-identical repeat of the call that just failed" — i.e., not adapting
// at all, versus retrying with different arguments (which this detector
// stays silent on, since a changed argument is itself weak evidence of an
// attempted fix, not a suspected problem). Anthropic-only, like
// ErrorRecoveryCount and for the same documented reason: OpenAI's protocol
// has no standard is_error field to detect the triggering error from.
func detectUnadaptedRetry(steps []*Step, tx i18n.StoryFindingsText) []Finding {
	var out []Finding
	for i, s := range steps {
		results := toolResultsFor(steps, i)
		if len(results) == 0 {
			continue
		}
		errored := map[string]bool{}
		for _, r := range results {
			if r.IsError {
				errored[r.CallID] = true
			}
		}
		for _, tc := range s.ToolCalls {
			if !errored[tc.ID] {
				continue
			}
			end := i + 1 + retryLookaheadSteps
			if end > len(steps) {
				end = len(steps)
			}
			retryStep, retry := firstSameNameCall(steps, i+1, end, tc.Name)
			if retry == nil || retry.Args != tc.Args {
				continue
			}
			ft := tx.UnadaptedRetry(tc.Name)
			out = append(out, Finding{
				Code: FindingUnadaptedRetry, StepSeq: retryStep.Seq, RelatedSeq: []int{s.Seq},
				Finding: ft.Finding, Evidence: ft.Evidence, Action: ft.Action,
			})
		}
	}
	return out
}

// --- shared "is this entity referenced again later" scan ----------------

// stepTextEntities extracts entities from everything a Step itself
// produced (response text, reasoning, and its own tool_call arguments) —
// the same pool detectReasoningActionMismatch already scans on the action
// side, generalized here to "does ANY later Step's own output reference
// this entity", not just the immediately following one.
func stepTextEntities(s *Step) []string {
	var b strings.Builder
	b.WriteString(s.RespText)
	b.WriteByte(' ')
	b.WriteString(s.Reasoning)
	b.WriteByte(' ')
	for _, tc := range s.ToolCalls {
		b.WriteString(tc.Args)
		b.WriteByte(' ')
	}
	return extractEntities(b.String())
}

// laterEntityMembership reports, for each of targets, whether it (or a
// substring-compatible match — see entityReferenced's own doc comment for
// why exact-string equality false-positives on real paths) is referenced
// anywhere in steps[fromIdx:]'s own output. Shared by 5.6 (fires when
// false — never referenced) and 5.7 (fires when true — referenced despite
// being falsified) since both are the same "later text" scan, just
// checking opposite outcomes.
func laterEntityMembership(steps []*Step, fromIdx int, targets []string) map[string]bool {
	var laterEntities []string
	for j := fromIdx; j < len(steps); j++ {
		laterEntities = append(laterEntities, stepTextEntities(steps[j])...)
	}
	out := make(map[string]bool, len(targets))
	for _, t := range targets {
		out[t] = entityReferenced(t, laterEntities)
	}
	return out
}

func capEntities(items []string) []string {
	if len(items) > maxEntitiesShown {
		return items[:maxEntitiesShown]
	}
	return items
}

// --- unused_tool_result -----------------------------------------------------

// detectUnusedToolResult flags a tool result that went entirely
// unreferenced afterward — the Step-level counterpart to
// Metrics.ContextUtilization (a Journey-level aggregate rate): this
// locates WHICH result, not just that the rate is low. Located at the
// calling Step (s.Seq) — the decision-spine already marks that Step, and
// that's where a reader scanning ⚠️ marks expects to find the explanation.
//
// Calibrated against this repo's own real audit corpus (logs/vmr-audit-
// 2026-07-1x/2x) — see the calibration notes in
// docs/VirtualModelRouter_Design_v4_Analytics.md's Findings section. The
// first version fired per ENTITY (any single filename/URL in the result
// that never recurred), and on real traffic that produced roughly 40
// findings per affected Journey: a directory listing or search result
// routinely names 10-30 files, of which an agent normally only follows up
// on a handful by design — that's completely ordinary behavior, not a
// wasted result. This version only fires when NONE of a result's entities
// are ever referenced again — "the whole result was ignored", the
// original, meaningfully rare signal this detector was meant to catch.
func detectUnusedToolResult(steps []*Step, tx i18n.StoryFindingsText) []Finding {
	var out []Finding
	for i, s := range steps {
		results := toolResultsFor(steps, i)
		for _, r := range results {
			entities := extractEntities(r.Text)
			if len(entities) == 0 {
				continue
			}
			membership := laterEntityMembership(steps, i+1, entities)
			anyUsed := false
			for _, e := range entities {
				if membership[e] {
					anyUsed = true
					break
				}
			}
			if anyUsed {
				continue
			}
			ft := tx.UnusedToolResult(strings.Join(capEntities(entities), ", "))
			out = append(out, Finding{
				Code: FindingUnusedToolResult, StepSeq: s.Seq,
				Finding: ft.Finding, Evidence: ft.Evidence, Action: ft.Action,
			})
		}
	}
	return out
}

// --- unverified_entity_reference ---------------------------------------------

// falsificationRe matches a tool result reporting that something doesn't
// exist — the "证伪" signal 5.7 anchors on, deliberately narrower than a
// general error marker: an ordinary command failure ("permission denied",
// "connection refused") says nothing about whether an ENTITY exists, only
// a not-found-shaped message does.
var falsificationRe = regexp.MustCompile(`(?i)ENOENT|not found|404|no such file|does not exist|文件不存在|未找到|不存在`)

// detectUnverifiedEntityReference flags an entity a tool result reported as
// missing/not-found, that a LATER Step still refers to without any visible
// re-verification in between — the design doc's own framing applies
// directly here: this is a suspicious signal anchored on an explicit tool
// falsification, not a confirmed hallucination (the tool itself could be
// wrong, or the entity could have been created in the meantime by a Step
// this detector doesn't specifically check for).
func detectUnverifiedEntityReference(steps []*Step, tx i18n.StoryFindingsText) []Finding {
	var out []Finding
	for i, s := range steps {
		results := toolResultsFor(steps, i)
		for _, r := range results {
			if !falsificationRe.MatchString(r.Text) {
				continue
			}
			entities := extractEntities(r.Text)
			if len(entities) == 0 {
				continue
			}
			membership := laterEntityMembership(steps, i+1, entities)
			var stillReferenced []string
			for _, e := range entities {
				if membership[e] {
					stillReferenced = append(stillReferenced, e)
				}
			}
			if len(stillReferenced) == 0 {
				continue
			}
			ft := tx.UnverifiedEntityReference(strings.Join(capEntities(stillReferenced), ", "))
			out = append(out, Finding{
				Code: FindingUnverifiedEntityReference, StepSeq: s.Seq,
				Finding: ft.Finding, Evidence: ft.Evidence, Action: ft.Action,
			})
		}
	}
	return out
}

// --- constraint_text_dropped_at_compaction -----------------------------------

// detectConstraintTextDropped is a thin wrapper around Step.Compaction
// (the compaction-reconstruction layer's already-computed, already-tested information-loss summary) —
// deliberately NOT a new entity-extraction/parsing path: buildCompactionInfo
// already diffs the predecessor's last rendered request against this
// Step's opening content and records which entities disappeared. This adds
// nothing except turning a non-empty SwallowedEntities list into a Finding.
// Per the design doc's own framing, this is an UNVERIFIED, hypothesis-level
// check (the "Governance Decay" failure mode it's modeled on has been named
// in the literature but never had a validated log-side detector) — the
// Finding text says so explicitly, and calibration against real corpus
// hit rate should happen before this is trusted the way the Phase 1
// detectors now are.
func detectConstraintTextDropped(steps []*Step, tx i18n.StoryFindingsText) []Finding {
	var out []Finding
	for _, s := range steps {
		if s.Compaction == nil || len(s.Compaction.SwallowedEntities) == 0 {
			continue
		}
		shown := capEntities(s.Compaction.SwallowedEntities)
		ft := tx.ConstraintTextDropped(strings.Join(shown, ", "), len(s.Compaction.SwallowedEntities))
		out = append(out, Finding{
			Code: FindingConstraintTextDropped, StepSeq: s.Seq,
			Finding: ft.Finding, Evidence: ft.Evidence, Action: ft.Action,
		})
	}
	return out
}
