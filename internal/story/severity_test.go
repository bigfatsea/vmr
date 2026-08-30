// Ver 2026-08-29, by Sonnet 5

package story

import "testing"

func TestJourneySeverity(t *testing.T) {
	t.Run("no findings is clean with no driver", func(t *testing.T) {
		lvl, drv := JourneySeverity(nil)
		if lvl != SeverityClean || drv != "" {
			t.Fatalf("want clean/\"\", got %q/%q", lvl, drv)
		}
	})

	t.Run("only non-critical findings is warning", func(t *testing.T) {
		lvl, drv := JourneySeverity([]Finding{
			{Code: FindingUnusedToolResult, StepSeq: 4},
			{Code: FindingReasoningActionMismatch, StepSeq: 2},
		})
		if lvl != SeverityWarning {
			t.Fatalf("want warning, got %q", lvl)
		}
		if drv != FindingReasoningActionMismatch {
			t.Fatalf("driver should be the earliest-step warning, got %q", drv)
		}
	})

	t.Run("any critical finding escalates and drives", func(t *testing.T) {
		lvl, drv := JourneySeverity([]Finding{
			{Code: FindingUnusedToolResult, StepSeq: 1},
			{Code: FindingGoalDrift, StepSeq: 9},
			{Code: FindingExactRepeatToolCall, StepSeq: 5},
		})
		if lvl != SeverityCritical {
			t.Fatalf("want critical, got %q", lvl)
		}
		if drv != FindingExactRepeatToolCall {
			t.Fatalf("driver should be the earliest-step critical (5 < 9), got %q", drv)
		}
	})

	t.Run("driver is independent of slice order", func(t *testing.T) {
		a := []Finding{{Code: FindingGoalDrift, StepSeq: 9}, {Code: FindingExactRepeatToolCall, StepSeq: 5}}
		b := []Finding{{Code: FindingExactRepeatToolCall, StepSeq: 5}, {Code: FindingGoalDrift, StepSeq: 9}}
		_, da := JourneySeverity(a)
		_, db := JourneySeverity(b)
		if da != db || da != FindingExactRepeatToolCall {
			t.Fatalf("driver not order-independent: %q vs %q", da, db)
		}
	})
}
