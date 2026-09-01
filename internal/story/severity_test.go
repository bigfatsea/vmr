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
}
