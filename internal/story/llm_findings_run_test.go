// Ver 2026-09-03, by pi-agent

package story

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// llmRunTestJourney builds a one-step journey the fake-detector tests can
// anchor findings against: the finding's EvidenceAnchor must appear
// verbatim in the transcript and its StepSeq must be a real step, or the
// verification pass in ComputeLLMFindings drops it.
func llmRunTestJourney(t *testing.T) *Journey {
	t.Helper()
	at := func(m int) time.Time { return time.Date(2026, 8, 16, 10, m, 0, 0, time.UTC) }
	r1 := audit.Record{
		TS: at(0), DurMS: 100, Model: "agent", Protocol: "openai-completions", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: map[string]any{
				"model": "agent", "messages": []any{msg("user", "test")},
			}},
			Response: &audit.Message{Status: 200, Body: `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"reply"}}]}
data: [DONE]`},
		},
	}
	l := onlyLineage(t, writeJSONL(t, []audit.Record{r1}))
	j, err := Build(l, taskseg.Generic, i18n.ZH)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return j
}

// withFakeDetectors swaps the detector roster for fakes and restores it on
// cleanup — the seam that makes ComputeLLMFindings' concurrency and
// budget testable without a live or mock LLM HTTP server (see
// NOTES_FOR_LEAD: no such server harness exists in this package's test
// infra).
func withFakeDetectors(t *testing.T, fakes ...llmDetector) {
	t.Helper()
	old := llmDetectors
	oldBudget := llmFindingsBudget
	llmDetectors = fakes
	llmFindingsBudget = 2 * time.Second
	t.Cleanup(func() {
		llmDetectors = old
		llmFindingsBudget = oldBudget
	})
}

func fakeFinding(seq int, anchor string) Finding {
	return Finding{StepSeq: seq, Code: "test_finding", EvidenceAnchor: anchor}
}

// TestComputeLLMFindings_ConcurrentMerge pins that the concurrent runner
// merges every detector's findings into ComputeLLMFindings' output —
// concurrency must not drop or duplicate a detector's results. The final
// order is decided by the sort at the end of ComputeLLMFindings, so the
// fake detectors intentionally return their findings in a scrambled order.
func TestComputeLLMFindings_ConcurrentMerge(t *testing.T) {
	j := llmRunTestJourney(t)
	withFakeDetectors(t,
		llmDetector{"fake_b", func(context.Context, *Journey, LLMOptions, i18n.Lang) []Finding {
			return []Finding{fakeFinding(1, "reply")}
		}},
		llmDetector{"fake_a", func(context.Context, *Journey, LLMOptions, i18n.Lang) []Finding {
			return []Finding{fakeFinding(1, "test")}
		}},
		llmDetector{"fake_empty", func(context.Context, *Journey, LLMOptions, i18n.Lang) []Finding {
			return nil
		}},
	)

	res, err := ComputeLLMFindings(context.Background(), j, LLMOptions{Addr: "127.0.0.1:1", Model: "agent"}, i18n.ZH)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(res) != 2 {
		t.Fatalf("merged %d findings, want 2 (one from each non-empty fake): %+v", len(res), res)
	}
	// Both fakes' anchors are real transcript substrings ("reply" and
	// "test"), so both survive verification. Same (StepSeq, Code) on both,
	// so the final order is the merge order — assert set, not order.
	got := map[string]bool{res[0].EvidenceAnchor: true, res[1].EvidenceAnchor: true}
	if !got["reply"] || !got["test"] {
		t.Errorf("merged findings missing an anchor: %+v", res)
	}
}

// TestComputeLLMFindings_BudgetCutsSlowDetector pins the total-time-budget
// contract: a detector that hangs until its ctx is cancelled is unwound
// when the budget expires, its empty slot is reported on stderr, and the
// fast detector's findings still come back — the whole call returns, not
// just eventually, but bounded by roughly the budget rather than the sum
// of detector times.
func TestComputeLLMFindings_BudgetCutsSlowDetector(t *testing.T) {
	j := llmRunTestJourney(t)
	withFakeDetectors(t,
		llmDetector{"fake_slow", func(ctx context.Context, _ *Journey, _ LLMOptions, _ i18n.Lang) []Finding {
			<-ctx.Done() // hang until the budget cancels us
			return nil
		}},
		llmDetector{"fake_fast", func(context.Context, *Journey, LLMOptions, i18n.Lang) []Finding {
			return []Finding{fakeFinding(1, "reply")}
		}},
	)

	start := time.Now()
	res, err := ComputeLLMFindings(context.Background(), j, LLMOptions{Addr: "127.0.0.1:1", Model: "agent"}, i18n.ZH)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("call took %v — the budget did not cut the slow detector loose", elapsed)
	}
	if len(res) != 1 || res[0].EvidenceAnchor != "reply" {
		t.Fatalf("fast detector's finding lost under the budget: %+v", res)
	}
}

// captureStderr swaps os.Stderr for a pipe, runs fn, and returns what it
// wrote — the only way to observe the runner's skipped-detector warnings.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestRunDetectorsConcurrently_ReportsSkippedOnStderr pins the
// not-silently-shrinking rule: when the budget expires, every detector
// that came back empty gets an explicit stderr line naming it, so a
// findings list missing a detector is never mistaken for "that detector
// legitimately found nothing".
func TestRunDetectorsConcurrently_ReportsSkippedOnStderr(t *testing.T) {
	j := llmRunTestJourney(t)
	withFakeDetectors(t,
		llmDetector{"fake_slow", func(ctx context.Context, _ *Journey, _ LLMOptions, _ i18n.Lang) []Finding {
			<-ctx.Done()
			return nil
		}},
		llmDetector{"fake_fast_empty", func(context.Context, *Journey, LLMOptions, i18n.Lang) []Finding {
			return nil
		}},
	)

	stderr := captureStderr(t, func() {
		raw := runDetectorsConcurrently(context.Background(), j, LLMOptions{Addr: "x"}, i18n.ZH)
		if len(raw) != 0 {
			t.Errorf("raw = %+v, want empty", raw)
		}
	})
	if !strings.Contains(stderr, "fake_slow") || !strings.Contains(stderr, "skipped") {
		t.Errorf("stderr missing skipped-detector line for fake_slow:\n%s", stderr)
	}
	if strings.Contains(stderr, "fake_fast_empty") {
		t.Errorf("stderr flagged fake_fast_empty as skipped, but the budget had not expired when it returned:\n%s", stderr)
	}
}
