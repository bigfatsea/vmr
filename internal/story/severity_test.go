// Ver 2026-08-29, by Sonnet 5

package story

import "testing"

func TestJourneySeverity(t *testing.T) {
	t.Run("no findings is clean with no driver", func(t *testing.T) {
		lvl, drv, low := JourneySeverity(nil)
		if lvl != SeverityClean || drv != "" || low {
			t.Fatalf("want clean/\"\"/false, got %q/%q/%v", lvl, drv, low)
		}
	})

	t.Run("only non-critical findings is warning", func(t *testing.T) {
		lvl, drv, low := JourneySeverity([]Finding{
			{Code: FindingUnusedToolResult, StepSeq: 4},
			{Code: FindingReasoningActionMismatch, StepSeq: 2},
		})
		if lvl != SeverityWarning || low {
			t.Fatalf("want warning/false, got %q/%v", lvl, low)
		}
		if drv != FindingReasoningActionMismatch {
			t.Fatalf("driver should be the earliest-step warning, got %q", drv)
		}
	})

	t.Run("any critical finding escalates and drives", func(t *testing.T) {
		lvl, drv, low := JourneySeverity([]Finding{
			{Code: FindingUnusedToolResult, StepSeq: 1},
			{Code: FindingGoalDrift, StepSeq: 9},
			{Code: FindingExactRepeatToolCall, StepSeq: 5},
		})
		if lvl != SeverityCritical || low {
			t.Fatalf("want critical/false, got %q/%v", lvl, low)
		}
		if drv != FindingExactRepeatToolCall {
			t.Fatalf("driver should be the earliest-step critical (5 < 9), got %q", drv)
		}
	})

	t.Run("driver is independent of slice order", func(t *testing.T) {
		a := []Finding{{Code: FindingGoalDrift, StepSeq: 9}, {Code: FindingExactRepeatToolCall, StepSeq: 5}}
		b := []Finding{{Code: FindingExactRepeatToolCall, StepSeq: 5}, {Code: FindingGoalDrift, StepSeq: 9}}
		_, da, _ := JourneySeverity(a)
		_, db, _ := JourneySeverity(b)
		if da != db || da != FindingExactRepeatToolCall {
			t.Fatalf("driver not order-independent: %q vs %q", da, db)
		}
	})

	t.Run("low-confidence finding does not headline over a specific one", func(t *testing.T) {
		lvl, drv, low := JourneySeverity([]Finding{
			{Code: FindingUnverifiedEntityReference, StepSeq: 2},
			{Code: FindingReasoningActionMismatch, StepSeq: 8},
		})
		if lvl != SeverityWarning || low {
			t.Fatalf("want warning/false, got %q/%v", lvl, low)
		}
		if drv != FindingReasoningActionMismatch {
			t.Fatalf("driver should skip the earlier low-confidence finding, got %q", drv)
		}
	})

	t.Run("low-confidence finding still drives when it is the only one at that level", func(t *testing.T) {
		_, drv, low := JourneySeverity([]Finding{
			{Code: FindingUnverifiedEntityReference, StepSeq: 4},
			{Code: FindingUnverifiedEntityReference, StepSeq: 1},
		})
		if drv != FindingUnverifiedEntityReference || !low {
			t.Fatalf("want unverified_entity_reference as sole driver (low=true), got %q/%v", drv, low)
		}
	})

	t.Run("a critical finding still outranks a low-confidence warning", func(t *testing.T) {
		lvl, drv, low := JourneySeverity([]Finding{
			{Code: FindingUnverifiedEntityReference, StepSeq: 1},
			{Code: FindingGoalDrift, StepSeq: 20},
		})
		if lvl != SeverityCritical || drv != FindingGoalDrift || low {
			t.Fatalf("want critical/goal_drift/false, got %q/%q/%v", lvl, drv, low)
		}
	})

	// R88 stopgap: an LLM-inferred finding must never headline a verdict —
	// its text and anchor are influenceable by the analyzed transcript
	// (prompt injection), unlike rule findings' fixed detector prose.
	t.Run("LLM-inferred finding never drives over a rule finding", func(t *testing.T) {
		lvl, drv, low := JourneySeverity([]Finding{
			{Code: FindingUnverifiedCompletionClaim, StepSeq: 0, Source: SourceLLMInferred, Confidence: ConfidenceHigh},
			{Code: FindingUnusedToolResult, StepSeq: 9, Source: SourceRule},
		})
		if lvl != SeverityWarning || drv != FindingUnusedToolResult || low {
			t.Fatalf("driver must be the rule finding even though the LLM finding has the earlier StepSeq: got %q/%q/%v", lvl, drv, low)
		}
	})

	t.Run("all-LLM findings yield no driver at all", func(t *testing.T) {
		findings := []Finding{
			{Code: FindingUnverifiedCompletionClaim, StepSeq: 1, Source: SourceLLMInferred, Confidence: ConfidenceHigh},
			{Code: FindingToolResultMisinterpretation, StepSeq: 3, Source: SourceLLMInferred, Confidence: ConfidenceHigh},
		}
		lvl, drv, _ := JourneySeverity(findings)
		if lvl != SeverityWarning || drv != "" {
			t.Fatalf("no rule finding means no driver: got %q/%q", lvl, drv)
		}
		if _, ok := pickDriver(findings, SeverityWarning, false); ok {
			t.Error("pickDriver must report ok=false when every finding at the level is LLM-inferred")
		}
	})

	t.Run("rule findings' driver selection is unchanged by the LLM exclusion", func(t *testing.T) {
		lvl, drv, low := JourneySeverity([]Finding{
			{Code: FindingGoalDrift, StepSeq: 7, Source: SourceRule},
			{Code: FindingExactRepeatToolCall, StepSeq: 2, Source: SourceRule},
			{Code: FindingGoalDrift, StepSeq: 0, Source: SourceLLMInferred, Confidence: ConfidenceHigh},
		})
		if lvl != SeverityCritical || drv != FindingExactRepeatToolCall || low {
			t.Fatalf("earliest-step rule critical should drive (2 < 7, LLM step 0 ignored): got %q/%q/%v", lvl, drv, low)
		}
	})
}
