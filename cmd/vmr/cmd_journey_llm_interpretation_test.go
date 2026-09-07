// Ver 2026-09-15, by pi

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vmr/internal/journey"
)

// mockInterpretServer answers one chat-completions POST with a fixed reply —
// the same shape internal/journey's llm_test.go mock serves, local here
// because journey's is test-package-private.
func mockInterpretServer(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + jsonString(t, reply) + `}}]}`))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func jsonString(t *testing.T, s string) string {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal %q: %v", s, err)
	}
	return string(data)
}

func firstJourneyID(t *testing.T, outDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, "journeys", "index.json"))
	if err != nil {
		t.Fatalf("read journeys/index.json: %v", err)
	}
	var idx journey.JourneyIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("unmarshal journeys/index.json: %v", err)
	}
	if len(idx.Journeys) == 0 {
		t.Fatal("journeys/index.json lists no journeys")
	}
	return idx.Journeys[0].ID
}

// TestJourneyLLMInterpretation_PersistedInJSONAndRenderOnly is the §3.6/N2
// acceptance guard, end to end: the -llm-addr interpretation lands in
// j-<id>.json's llm_interpretation (model/status/duration/text), the .md's
// "## LLM" section renders from that record alone, and -render-only
// reproduces the section from the JSON — even when the old .md it would
// previously have scraped from no longer contains it (the removed
// bypass-splice must not be the mechanism).
func TestJourneyLLMInterpretation_PersistedInJSONAndRenderOnly(t *testing.T) {
	logPath := fixtureAuditLogs(t)
	outDir := t.TempDir()
	ts := mockInterpretServer(t, "The journey investigated the deploy failure in three steps.")

	// 1. Baseline full run (no LLM — the default suite never calls it).
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, logPath})
	}); err != nil {
		t.Fatalf("full cmdAnalyze failed: %v", err)
	}
	id := firstJourneyID(t, outDir)

	// 2. Single-journey zoom with the interpretation layer on.
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-journey", id, "-llm-addr", strings.TrimPrefix(ts.URL, "http://"), "-llm-model", "agent", "-o", outDir, logPath})
	}); err != nil {
		t.Fatalf("cmdAnalyze -journey with -llm-addr failed: %v", err)
	}

	// 3. The JSON carries the record.
	data, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", id+".json"))
	if err != nil {
		t.Fatalf("read %s.json: %v", id, err)
	}
	var s journey.JourneySummary
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal %s.json: %v", id, err)
	}
	if s.LLMInterpretation == nil {
		t.Fatalf("j-%s.json has no llm_interpretation record", id)
	}
	rec := s.LLMInterpretation
	if rec.Status != journey.LLMStatusOK || rec.Model != "agent" || rec.Text == "" || rec.DurationMS < 0 {
		t.Errorf("llm_interpretation = %+v, want status ok, model agent, non-empty text", rec)
	}
	if rec.Scope != "" {
		t.Errorf("single-journey record scope = %q, want \"\"", rec.Scope)
	}

	// 4. The .md renders the section — and its bytes come from the JSON:
	// strip the LLM tail from the .md, then -render-only must restore it
	// byte-identically (under the old scrape hack, a stripped .md would
	// stay stripped — nothing to scrape).
	mdPath := filepath.Join(outDir, "journeys", "details", id+".md")
	mdData, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read %s.md: %v", id, err)
	}
	md := string(mdData)
	secIdx := strings.Index(md, "\n## LLM Interpretation")
	if secIdx < 0 {
		t.Fatalf("%s.md does not render the LLM section:\n%s", id, md)
	}
	llmTail := md[secIdx:]
	if err := os.WriteFile(mdPath, []byte(md[:secIdx]), 0o600); err != nil {
		t.Fatalf("strip LLM tail: %v", err)
	}

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-render-only", "-no-cache"})
	}); err != nil {
		t.Fatalf("-render-only failed: %v", err)
	}
	restored, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("re-read %s.md: %v", id, err)
	}
	if string(restored) != md {
		t.Errorf("-render-only did not restore the .md byte-identically from the JSON record\nwant tail %q\ngot  %q", llmTail, string(restored)[min(len(string(restored)), secIdx):])
	}
}

// TestJourneyLLMInterpretation_FailureRecordedButNotRendered covers the
// degrade contract: a failed interpretation call is persisted with status
// "failed" (the JSON is honest about the attempt) but renders no section,
// and -render-only reproduces that absence from the JSON.
func TestJourneyLLMInterpretation_FailureRecordedButNotRendered(t *testing.T) {
	logPath := fixtureAuditLogs(t)
	outDir := t.TempDir()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream exploded", http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, logPath})
	}); err != nil {
		t.Fatalf("full cmdAnalyze failed: %v", err)
	}
	id := firstJourneyID(t, outDir)

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-journey", id, "-llm-addr", strings.TrimPrefix(ts.URL, "http://"), "-llm-model", "agent", "-o", outDir, logPath})
	}); err != nil {
		t.Fatalf("cmdAnalyze -journey with a failing -llm-addr must not fail the run: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", id+".json"))
	if err != nil {
		t.Fatalf("read %s.json: %v", id, err)
	}
	var s journey.JourneySummary
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal %s.json: %v", id, err)
	}
	if s.LLMInterpretation == nil {
		t.Fatal("the failed attempt must still be recorded in llm_interpretation")
	}
	if s.LLMInterpretation.Status != journey.LLMStatusFailed || s.LLMInterpretation.Error == "" {
		t.Errorf("llm_interpretation = %+v, want status failed with the error carried", s.LLMInterpretation)
	}

	mdData, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", id+".md"))
	if err != nil {
		t.Fatalf("read %s.md: %v", id, err)
	}
	if strings.Contains(string(mdData), "## LLM") {
		t.Errorf("a failed record must not render an LLM section:\n%s", mdData)
	}

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-render-only", "-no-cache"})
	}); err != nil {
		t.Fatalf("-render-only failed: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", id+".md"))
	if err != nil {
		t.Fatalf("re-read %s.md: %v", id, err)
	}
	if string(after) != string(mdData) {
		t.Error("-render-only changed the .md although the JSON has no renderable record")
	}
}

// TestCompareLLMInterpretation_PersistedInJSONAndRenderOnly is the compare
// side's §3.6 guard: both interpretation records (overall + divergence when
// found) land in compare-*.json, the .md renders them from those records
// alone, and -render-only reproduces compare-*.md byte-identically from the
// JSON — the removed scrape must not be the mechanism keeping the section.
func TestCompareLLMInterpretation_PersistedInJSONAndRenderOnly(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	path, idA, idB := writeTwoCandidateJourneys(t, outDir)
	ts := mockInterpretServer(t, "Both journeys solved the task; the second did it with fewer tool calls.")

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-compare", idA + "," + idB, "-llm-addr", strings.TrimPrefix(ts.URL, "http://"), "-llm-model", "agent", "-o", outDir, path})
	}); err != nil {
		t.Fatalf("cmdAnalyze -compare with -llm-addr failed: %v", err)
	}

	cmpJSONPath := filepath.Join(outDir, "compares", "compare-"+idA+"-vs-"+idB+".json")
	data, err := os.ReadFile(cmpJSONPath)
	if err != nil {
		t.Fatalf("read compare json: %v", err)
	}
	var cmp journey.Comparison
	if err := json.Unmarshal(data, &cmp); err != nil {
		t.Fatalf("unmarshal compare json: %v", err)
	}
	if cmp.LLMInterpretation == nil || cmp.LLMInterpretation.Status != journey.LLMStatusOK ||
		cmp.LLMInterpretation.Scope != journey.LLMScopeOverall || cmp.LLMInterpretation.Model != "agent" {
		t.Errorf("llm_interpretation = %+v, want an ok overall record from model agent", cmp.LLMInterpretation)
	}
	if cmp.LLMDivergence != nil &&
		(cmp.LLMDivergence.Status != journey.LLMStatusOK || cmp.LLMDivergence.Scope != journey.LLMScopeDivergence) {
		t.Errorf("llm_divergence = %+v, want nil (no divergence) or an ok divergence record", cmp.LLMDivergence)
	}

	cmpMDPath := filepath.Join(outDir, "compares", "compare-"+idA+"-vs-"+idB+".md")
	fullMD, err := os.ReadFile(cmpMDPath)
	if err != nil {
		t.Fatalf("read compare md: %v", err)
	}
	// The overall record always renders; the divergence section only when the
	// pair actually diverged (this fixture's pair doesn't necessarily).
	wantSections := 1
	if cmp.LLMDivergence != nil {
		wantSections = 2
	}
	if got := strings.Count(string(fullMD), "\n## LLM Interpretation"); got != wantSections {
		t.Errorf("compare md renders %d LLM sections, want %d:\n%s", got, wantSections, fullMD)
	}

	// Strip both sections, then render-only (cache off) must restore them
	// byte-identically from the JSON records.
	if err := os.WriteFile(cmpMDPath, fullMD[:strings.Index(string(fullMD), "\n## LLM Interpretation")], 0o600); err != nil {
		t.Fatalf("strip LLM tail: %v", err)
	}
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-render-only", "-no-cache"})
	}); err != nil {
		t.Fatalf("-render-only failed: %v", err)
	}
	restored, err := os.ReadFile(cmpMDPath)
	if err != nil {
		t.Fatalf("re-read compare md: %v", err)
	}
	if string(restored) != string(fullMD) {
		t.Error("-render-only did not restore compare-*.md byte-identically from the JSON records")
	}
}
