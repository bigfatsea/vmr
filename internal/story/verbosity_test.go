// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

package story

import (
	"testing"
)

func TestComputeOutputRepetitionRate(t *testing.T) {
	t.Parallel()

	t.Run("diverse non-repetitive text has low repetition", func(t *testing.T) {
		j := &Journey{
			Tasks: []*Task{{
				Steps: []*Step{
					{
						Seq:      1,
						RespText: "Let us inspect the codebase configuration files first to understand dependencies.",
					},
					{
						Seq:       2,
						Reasoning: "Now running the test suite to observe initial pass and fail status on components.",
					},
					{
						Seq:      3,
						RespText: "All compilation succeeded without errors, proceeding to benchmark evaluation.",
					},
				},
			}},
		}

		rate := ComputeOutputRepetitionRate(j)
		if rate > 0.15 {
			t.Errorf("expected low repetition rate (< 0.15), got %.3f", rate)
		}
	})

	t.Run("highly repetitive text has high repetition", func(t *testing.T) {
		repeated := "Let us check the log file once again. "
		var longText string
		for i := 0; i < 10; i++ {
			longText += repeated
		}

		j := &Journey{
			Tasks: []*Task{{
				Steps: []*Step{
					{
						Seq:      1,
						RespText: longText,
					},
				},
			}},
		}

		rate := ComputeOutputRepetitionRate(j)
		if rate < 0.60 {
			t.Errorf("expected high repetition rate (> 0.60), got %.3f", rate)
		}
	})

	t.Run("empty or very short text returns zero", func(t *testing.T) {
		j := &Journey{
			Tasks: []*Task{{
				Steps: []*Step{
					{Seq: 1, RespText: "Hi there"},
				},
			}},
		}

		rate := ComputeOutputRepetitionRate(j)
		if rate != 0.0 {
			t.Errorf("expected 0.0 for short text, got %.3f", rate)
		}
	})
}
