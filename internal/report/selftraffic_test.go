// Ver 2026-08-20 17:20, by Sonnet 5

package report

import (
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// selfTrafficFixtureRecords returns two records that differ only in
// client_key_tag: "workload" (the thing being analyzed) and "vmrstory"
// (a stand-in for vmr story -llm-addr's own self-analysis traffic).
func selfTrafficFixtureRecords() []map[string]any {
	mk := func(ts time.Time, tag string) map[string]any {
		return map[string]any{
			"ts": ts.Format(time.RFC3339), "dur_ms": 100, "model": "agent", "protocol": "openai-completions", "outcome": "ok",
			"client_key_tag": tag,
			"client": map[string]any{
				"request": map[string]any{"body": map[string]any{"model": "agent", "messages": []any{
					map[string]any{"role": "user", "content": "hi"},
				}}},
				"response": map[string]any{"status": 200, "body": map[string]any{
					"model": "agent",
					"choices": []any{map[string]any{"finish_reason": "stop",
						"message": map[string]any{"role": "assistant", "content": "ok"}}},
					"usage": map[string]any{"prompt_tokens": 1000, "completion_tokens": 5},
				}},
			},
			"attempts": []map[string]any{{"endpoint": "openai-completions:p:m", "dur_ms": 100, "response": map[string]any{"status": 200}}},
		}
	}
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	return []map[string]any{
		mk(t0, "workload"),
		mk(t0.Add(time.Minute), "vmrstory"),
	}
}

// TestIngestRecord_ExcludesSelfTraffic covers P6.4: a record whose
// client_key_tag matches the exclusion set never reaches Overall (or any
// other bucket), but IS counted in Meta.SelfTrafficExcluded — excluded,
// not silently dropped.
func TestIngestRecord_ExcludesSelfTraffic(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, selfTrafficFixtureRecords())

	rep, _, _, err := BuildCached([]string{path}, time.Now(), nil, nil, nil, nil,
		taskseg.OpenClawAware, nil, nil, map[string]bool{"vmrstory": true})
	if err != nil {
		t.Fatalf("BuildCached: %v", err)
	}
	if rep.Overall.Requests != 1 {
		t.Errorf("Overall.Requests = %d, want 1 (the vmrstory record must be excluded)", rep.Overall.Requests)
	}
	if rep.Meta.SelfTrafficExcluded != 1 {
		t.Errorf("Meta.SelfTrafficExcluded = %d, want 1", rep.Meta.SelfTrafficExcluded)
	}
	// Meta.Records still counts every record scanned, excluded or not —
	// it answers "how many lines did this run read", a different question
	// than "how many contributed to the totals" (same distinction ParseErrors
	// already draws).
	if rep.Meta.Records != 2 {
		t.Errorf("Meta.Records = %d, want 2 (counts scanned records regardless of exclusion)", rep.Meta.Records)
	}
	for _, c := range rep.ByClient {
		if c.ClientKey == "vmrstory" {
			t.Errorf("ByClient still has an entry for the excluded tag: %+v", c)
		}
	}
}

// TestSelfTrafficExclusion_ConfiguredButNothingMatched pins the appendix
// disclosure fix: an exclusion set that IS configured but matches 0 records
// in this window must report "exclusion active" (SelfTrafficExclusionActive
// == true), not fall back to the "not configured" line — which would be a
// false statement and would contradict the story half's own disclosure on
// the same run.
func TestSelfTrafficExclusion_ConfiguredButNothingMatched(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, selfTrafficFixtureRecords())

	rep, _, _, err := BuildCached([]string{path}, time.Now(), nil, nil, nil, nil,
		taskseg.OpenClawAware, nil, nil, map[string]bool{"some-tag-not-in-this-log": true})
	if err != nil {
		t.Fatalf("BuildCached: %v", err)
	}
	if rep.Meta.SelfTrafficExcluded != 0 {
		t.Fatalf("SelfTrafficExcluded = %d, want 0 (nothing in the fixture matches)", rep.Meta.SelfTrafficExcluded)
	}
	if !rep.Meta.SelfTrafficExclusionActive {
		t.Fatal("SelfTrafficExclusionActive = false, want true (a non-empty exclusion set was passed)")
	}
	md := Markdown(rep, i18n.EN, nil, nil)
	if !strings.Contains(md, "Self-traffic: exclusion active") {
		t.Errorf("appendix should say exclusion is active:\n%s", md)
	}
	if strings.Contains(md, "exclusion not active") {
		t.Errorf("appendix must not claim exclusion is not active when a set was configured:\n%s", md)
	}
}

// TestIngestRecord_NoExclusionByDefault proves the nil-map path (no
// llm_key configured, the common case) excludes nothing.
func TestIngestRecord_NoExclusionByDefault(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, selfTrafficFixtureRecords())

	rep, _, _, err := BuildCached([]string{path}, time.Now(), nil, nil, nil, nil,
		taskseg.OpenClawAware, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached: %v", err)
	}
	if rep.Overall.Requests != 2 {
		t.Errorf("Overall.Requests = %d, want 2 (nil exclusion set excludes nothing)", rep.Overall.Requests)
	}
	if rep.Meta.SelfTrafficExcluded != 0 {
		t.Errorf("Meta.SelfTrafficExcluded = %d, want 0", rep.Meta.SelfTrafficExcluded)
	}
}

// TestExcludeSelfTraffic_ToolsAndCompactionsDontLeak covers a regression
// guard where buildTools/buildCompactions (§5/§6.7) read straight from
// SessionAnalysis, a separate pass from ingestRecord's own per-record
// skip — an excluded record's tool declaration or compaction entry used
// to still surface in those two sections even though Overall/ByClient
// correctly excluded it.
func TestExcludeSelfTraffic_ToolsAndCompactionsDontLeak(t *testing.T) {
	t0 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	workloadWithTool := map[string]any{
		"ts": t0.Format(time.RFC3339), "dur_ms": 100, "model": "agent", "protocol": "openai-completions", "outcome": "ok",
		"client_key_tag": "workload",
		"client": map[string]any{
			"request": map[string]any{"body": map[string]any{
				"model":    "agent",
				"tools":    []any{map[string]any{"type": "function", "function": map[string]any{"name": "real_tool"}}},
				"messages": []any{map[string]any{"role": "user", "content": "hi"}},
			}},
			"response": map[string]any{"status": 200, "body": map[string]any{
				"model": "agent",
				"choices": []any{map[string]any{"finish_reason": "stop",
					"message": map[string]any{"role": "assistant", "content": "ok",
						"tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "real_tool", "arguments": "{}"}}}}}},
				"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 5},
			}},
		},
		"attempts": []map[string]any{{"endpoint": "openai-completions:p:m", "dur_ms": 100, "response": map[string]any{"status": 200}}},
	}
	selfTrafficWithTool := map[string]any{
		"ts": t0.Add(time.Minute).Format(time.RFC3339), "dur_ms": 100, "model": "agent", "protocol": "openai-completions", "outcome": "ok",
		"client_key_tag": "vmrstory",
		"client": map[string]any{
			"request": map[string]any{"body": map[string]any{
				"model":    "agent",
				"tools":    []any{map[string]any{"type": "function", "function": map[string]any{"name": "self_analysis_tool"}}},
				"messages": []any{map[string]any{"role": "user", "content": "interpret this journey"}},
			}},
			"response": map[string]any{"status": 200, "body": map[string]any{
				"model": "agent",
				"choices": []any{map[string]any{"finish_reason": "stop",
					"message": map[string]any{"role": "assistant", "content": "ok"}}},
				"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 5},
			}},
		},
		"attempts": []map[string]any{{"endpoint": "openai-completions:p:m", "dur_ms": 100, "response": map[string]any{"status": 200}}},
	}
	selfTrafficCompaction := map[string]any{
		"ts": t0.Add(2 * time.Minute).Format(time.RFC3339), "dur_ms": 100, "model": "agent", "protocol": "openai-completions", "outcome": "ok",
		"client_key_tag": "vmrstory",
		"client": map[string]any{
			"request": map[string]any{"body": map[string]any{
				"model": "agent",
				"messages": []any{
					map[string]any{"role": "system", "content": "You are a context summarization assistant."},
					map[string]any{"role": "user", "content": "worked on self-analysis.go"},
				},
			}},
			"response": map[string]any{"status": 200, "body": map[string]any{
				"model": "agent",
				"choices": []any{map[string]any{"finish_reason": "stop",
					"message": map[string]any{"role": "assistant", "content": "Summary: self-analysis"}}},
				"usage": map[string]any{"prompt_tokens": 500, "completion_tokens": 30},
			}},
		},
		"attempts": []map[string]any{{"endpoint": "openai-completions:p:m", "dur_ms": 100, "response": map[string]any{"status": 200}}},
	}

	dir := t.TempDir()
	path := writeTempJSONL(t, dir, []map[string]any{workloadWithTool, selfTrafficWithTool, selfTrafficCompaction})

	rep, _, _, err := BuildCached([]string{path}, time.Now(), nil, nil, nil, nil,
		taskseg.OpenClawAware, nil, nil, map[string]bool{"vmrstory": true})
	if err != nil {
		t.Fatalf("BuildCached: %v", err)
	}

	for _, ts := range rep.Tools {
		if ts.Shape != "tools:0" {
			for _, name := range ts.Declared {
				if name == "self_analysis_tool" {
					t.Errorf("rep.Tools leaked the excluded record's tool declaration: %+v", ts)
				}
			}
		}
	}
	if len(rep.Compactions) != 0 {
		t.Errorf("rep.Compactions = %+v, want empty (the only compaction record is self-traffic)", rep.Compactions)
	}
	if rep.Meta.SelfTrafficExcluded != 2 {
		t.Errorf("Meta.SelfTrafficExcluded = %d, want 2", rep.Meta.SelfTrafficExcluded)
	}
}

// TestExcludeSelfTraffic_DetailsNotMaterialized covers the same review's
// "orphan file" finding: with -details semantics (onRecord non-nil), an
// excluded record must not get a details/*.md page written for it either
// — onRecord is called before ingestRecord's own skip, so without an
// explicit check there it would materialize a page nothing ever links to.
func TestExcludeSelfTraffic_DetailsNotMaterialized(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, selfTrafficFixtureRecords())

	var onRecordTags []string
	onRecord := func(rec *audit.Record, _ *ReqInfo) {
		onRecordTags = append(onRecordTags, rec.ClientKeyTag)
	}

	_, _, _, err := BuildCached([]string{path}, time.Now(), nil, nil, nil, onRecord,
		taskseg.OpenClawAware, nil, nil, map[string]bool{"vmrstory": true})
	if err != nil {
		t.Fatalf("BuildCached: %v", err)
	}
	for _, tag := range onRecordTags {
		if tag == "vmrstory" {
			t.Errorf("onRecord (the -details writer) was called for an excluded record: tags = %v", onRecordTags)
		}
	}
	if len(onRecordTags) != 1 || onRecordTags[0] != "workload" {
		t.Errorf("onRecord tags = %v, want exactly [\"workload\"]", onRecordTags)
	}
}
