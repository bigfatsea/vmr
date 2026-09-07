// Ver 2026-09-15, by pi

package journey

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"vmr/internal/i18n"
)

// §3.6's render-from-JSON contract: the .md's LLM section is a pure function
// of the persisted LLMInterpretation record. These guards pin the record's
// round-trip stability, the two outcome mappings, and both renderers' record
// consumption — the regression surface of the removed "scrape the appended
// section from the old .md" bypass-splice.

var errBoom = errors.New("server returned 500")

func TestNewLLMInterpretation_Outcomes(t *testing.T) {
	opts := LLMOptions{Model: "agent"}

	ok := NewLLMInterpretation(opts, InterpretResult{Text: "reading", Cached: true, Duration: 1500 * time.Millisecond}, nil, LLMScopeOverall)
	if ok.Status != LLMStatusOK || ok.Text != "reading" || !ok.Cached || ok.DurationMS != 1500 {
		t.Errorf("ok record = %+v, want status ok with text/cached/duration carried", ok)
	}
	if ok.Model != "agent" || ok.Scope != LLMScopeOverall {
		t.Errorf("ok record = %+v, want model/scope stamped", ok)
	}

	failed := NewLLMInterpretation(opts, InterpretResult{}, errBoom, LLMScopeDivergence)
	if failed.Status != LLMStatusFailed || failed.Error != errBoom.Error() {
		t.Errorf("failed record = %+v, want status failed with the error carried", failed)
	}
	if failed.Text != "" || failed.Cached || failed.DurationMS != 0 {
		t.Errorf("failed record = %+v, want no text/cached/duration on failure", failed)
	}
}

// TestLLMInterpretation_JSONRoundTripRendersIdentically pins §3.6's core
// guarantee in both languages: marshaling the record into j-<id>.json /
// compare-*.json and reading it back must render the byte-identical LLM
// section — the JSON is the single rendering input, so -render-only cannot
// drift from the full run.
func TestLLMInterpretation_JSONRoundTripRendersIdentically(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.EN, i18n.ZH} {
		for _, scope := range []string{"", LLMScopeOverall, LLMScopeDivergence} {
			rec := &LLMInterpretation{
				Model:      "agent",
				Scope:      scope,
				Status:     LLMStatusOK,
				DurationMS: 4210,
				Cached:     true,
				Text:       "## 自有标题会降级\n\n### 保留层级\n\n```json\n{\"untouched\": true}\n```",
			}
			data, err := json.Marshal(rec)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var back LLMInterpretation
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			want := RenderLLMSection(rec, lang)
			got := RenderLLMSection(&back, lang)
			if got != want {
				t.Errorf("lang %v scope %q: round-trip render drifted\nwant %q\ngot  %q", lang, scope, want, got)
			}
			if want == "" {
				t.Fatal("ok record must render non-empty")
			}
			if !strings.HasPrefix(want, "## LLM") {
				t.Errorf("section must open with the LLM heading, got %q", want)
			}
		}
	}
}

// TestJourneyVM_LLMSessionFromSummary covers the viewmodel's consumption:
// the section renders last, from s.LLMInterpretation alone; a summary
// without a record renders no LLM section at all.
func TestJourneyVM_LLMSessionFromSummary(t *testing.T) {
	j := vmEquivalenceFixture(t)
	base := NewJourneySummary(j, ComputeMetrics(j), ComputeFindings(j, i18n.EN), nil, nil, nil)
	if got := RenderMarkdownFromSummary(&base, i18n.EN, false, false); strings.Contains(got, "## LLM") {
		t.Error("summary without a record must not render an LLM section")
	}

	with := NewJourneySummary(j, ComputeMetrics(j), ComputeFindings(j, i18n.EN), nil, nil,
		&LLMInterpretation{Model: "agent", Status: LLMStatusOK, Text: "the model's own reading"})
	md := RenderMarkdownFromSummary(&with, i18n.EN, false, false)
	idx := strings.Index(md, "## LLM Interpretation")
	if idx < 0 {
		t.Fatalf("summary with a record must render the LLM section:\n%s", md)
	}
	// The section is the document's tail — same position the old append put it.
	if tail := md[idx:]; !strings.Contains(tail, "the model's own reading") || !strings.HasSuffix(md, "\n") {
		t.Errorf("LLM section must be the document's tail, got:\n%s", tail)
	}
	// A failed record renders nothing, matching the full run's omission.
	withFailed := NewJourneySummary(j, ComputeMetrics(j), ComputeFindings(j, i18n.EN), nil, nil,
		&LLMInterpretation{Model: "agent", Status: LLMStatusFailed, Error: "boom"})
	if got := RenderMarkdownFromSummary(&withFailed, i18n.EN, false, false); strings.Contains(got, "## LLM") {
		t.Errorf("failed record must not render an LLM section:\n%s", got)
	}
}

// TestRenderComparisonMarkdown_LLMSessionsFromRecords pins the compare side's
// byte layout: overall first, divergence second, each present section
// prefixed by exactly one "\n" — reproducing the old caller-side append from
// the JSON records alone.
func TestRenderComparisonMarkdown_LLMSessionsFromRecords(t *testing.T) {
	sA := JourneySummary{ID: "j-a", Title: "A", Metrics: Metrics{ModelMS: 1000}}
	sB := JourneySummary{ID: "j-b", Title: "B", Metrics: Metrics{ModelMS: 2000}}
	base := RenderComparisonMarkdown(Compare(sA, sB, i18n.EN), i18n.EN)
	if strings.Contains(base, "## LLM") {
		t.Error("comparison without records must not render an LLM section")
	}

	cmp := Compare(sA, sB, i18n.EN)
	cmp.LLMInterpretation = &LLMInterpretation{Model: "agent", Status: LLMStatusOK, Scope: LLMScopeOverall, Text: "overall reading"}
	cmp.LLMDivergence = &LLMInterpretation{Model: "agent", Status: LLMStatusOK, Scope: LLMScopeDivergence, Text: "divergence reading"}
	md := RenderComparisonMarkdown(cmp, i18n.EN)

	overallIdx := strings.Index(md, "overall comparison")
	divIdx := strings.Index(md, "divergence point")
	if overallIdx < 0 || divIdx < 0 || overallIdx > divIdx {
		t.Errorf("overall section must precede the divergence section:\n%s", md)
	}
	if !strings.Contains(md, "overall reading") || !strings.Contains(md, "divergence reading") {
		t.Errorf("both records' bodies must render:\n%s", md)
	}
	// Exactly one "\n" before each section heading (the old append's layout).
	if strings.Count(md, "\n## LLM Interpretation") != 2 {
		t.Errorf("want exactly two '\\n'-prefixed LLM sections:\n%s", md)
	}

	// A failed overall + ok divergence reproduces the old single-append layout.
	cmp.LLMInterpretation = &LLMInterpretation{Model: "agent", Status: LLMStatusFailed, Scope: LLMScopeOverall, Error: "boom"}
	md = RenderComparisonMarkdown(cmp, i18n.EN)
	if strings.Count(md, "\n## LLM Interpretation") != 1 || !strings.Contains(md, "divergence reading") {
		t.Errorf("failed overall must be skipped, divergence still rendered:\n%s", md)
	}
}
