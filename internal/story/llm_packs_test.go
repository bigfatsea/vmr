// Ver 2026-08-05, by Sonnet 5

package story

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

func TestBuildSingleJourneyEvidencePack(t *testing.T) {
	at := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	sys := msg("system", "sys")
	u1 := msg("user", "read a.md")
	r1 := mkRec(at, "", []any{sys, u1}, sseToolCalls([]any{
		map[string]any{"id": "c1", "function": map[string]any{"name": "read", "arguments": `{"path":"a.md"}`}},
	}))
	path := writeJSONL(t, []audit.Record{r1})
	j, err := Build(onlyLineage(t, path), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	m := ComputeMetrics(j)
	findings := ComputeFindings(j, i18n.EN)

	pack := BuildSingleJourneyEvidencePack(j, m, findings, i18n.EN)
	if pack.Journey.ID != j.ID {
		t.Errorf("Journey.ID = %q, want %q", pack.Journey.ID, j.ID)
	}
	if pack.Metrics.ToolCallCount != m.ToolCallCount {
		t.Errorf("Metrics not passed through: got %+v", pack.Metrics)
	}
	if len(pack.ToolIndex) != 1 || pack.ToolIndex[0].Tools[0] != "read" {
		t.Errorf("ToolIndex = %+v, want one entry naming tool read", pack.ToolIndex)
	}
	if pack.EstimateChars() <= 0 {
		t.Error("EstimateChars should be positive for a non-empty pack")
	}
}

func TestBuildDivergenceEvidencePack(t *testing.T) {
	t.Run("no divergence found: empty pack, no panic", func(t *testing.T) {
		a := journeyOf(&Step{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("read", `{}`)}})
		b := journeyOf(&Step{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("read", `{}`)}})
		div := computeDivergence(a, b)
		if div.Found {
			t.Fatal("test setup: expected no divergence")
		}
		pack := BuildDivergenceEvidencePack(a, b, div, i18n.EN)
		if pack.AtA != nil || pack.AtB != nil {
			t.Errorf("expected an empty pack when div.Found is false, got %+v", pack)
		}
	})

	t.Run("divergence found: before/at/after windows populated on both sides", func(t *testing.T) {
		a := journeyOf(
			&Step{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"a"}`)}},
			&Step{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"b"}`)}},
			&Step{Seq: 3, ToolCalls: []chatmsg.ToolCall{tc("write", `{}`)}}, // divergence here
			&Step{Seq: 4, RespText: "done"},
		)
		b := journeyOf(
			&Step{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"a"}`)}},
			&Step{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"b"}`)}},
			&Step{Seq: 3, ToolCalls: []chatmsg.ToolCall{tc("delete", `{}`)}}, // different tool: heavy divergence
			&Step{Seq: 4, RespText: "done differently"},
		)
		div := computeDivergence(a, b)
		if !div.Found || div.Index != 2 {
			t.Fatalf("test setup: expected divergence at index 2, got %+v", div)
		}
		pack := BuildDivergenceEvidencePack(a, b, div, i18n.EN)
		if pack.AtA == nil || pack.AtA.Tools[0] != "write" {
			t.Errorf("AtA = %+v, want tools=[write]", pack.AtA)
		}
		if pack.AtB == nil || pack.AtB.Tools[0] != "delete" {
			t.Errorf("AtB = %+v, want tools=[delete]", pack.AtB)
		}
		if len(pack.BeforeA) != 2 || pack.BeforeA[0].Seq != 1 || pack.BeforeA[1].Seq != 2 {
			t.Errorf("BeforeA = %+v, want Steps 1,2", pack.BeforeA)
		}
		if len(pack.AfterA) != 1 || pack.AfterA[0].Seq != 4 {
			t.Errorf("AfterA = %+v, want Step 4", pack.AfterA)
		}
		if pack.EstimateChars() <= 0 {
			t.Error("EstimateChars should be positive")
		}
	})
}

// TestInterpret_SingleJourneyPack covers Interpret's generic path with a
// non-EvidencePack type — confirms the evidencePackKind refactor (llm.go)
// actually dispatches to SingleJourneyEvidencePack's own promptSpec (the
// single-Journey system prompt), not silently falling back to -compare's.
func TestInterpret_SingleJourneyPack(t *testing.T) {
	ts, calls, _, lastBody := mockChatServer(t, "single journey interpretation")
	addr := strings.TrimPrefix(ts.URL, "http://")
	opts := LLMOptions{Addr: addr, Model: "agent"}

	pack := SingleJourneyEvidencePack{
		Journey: JourneyRef{ID: "j-test", Title: "test"},
		Metrics: Metrics{ToolCallCount: 1},
	}

	res, err := Interpret(context.Background(), opts, pack, i18n.EN)
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if res.Text != "single journey interpretation" {
		t.Errorf("Text = %q", res.Text)
	}
	if *calls != 1 {
		t.Fatalf("server calls = %d, want 1", *calls)
	}

	var sentReq chatCompletionRequest
	if err := json.Unmarshal(*lastBody, &sentReq); err != nil {
		t.Fatalf("request body not valid JSON: %v", err)
	}
	if !strings.Contains(sentReq.Messages[0].Content, "Suspected Issues") {
		t.Errorf("system prompt should be the single-journey one, got: %s", sentReq.Messages[0].Content)
	}
	if !strings.Contains(sentReq.Messages[1].Content, `"journey"`) {
		t.Error("user message should embed the single-journey evidence pack JSON")
	}
}

// TestInterpret_DivergencePack mirrors TestInterpret_SingleJourneyPack for
// DivergenceEvidencePack.
func TestInterpret_DivergencePack(t *testing.T) {
	ts, _, _, lastBody := mockChatServer(t, "divergence interpretation")
	addr := strings.TrimPrefix(ts.URL, "http://")
	opts := LLMOptions{Addr: addr, Model: "agent"}

	div := DivergencePoint{Found: true, Index: 2, AStepSeq: 3, BStepSeq: 3, Severity: DivergenceHeavy, ATools: []string{"write"}, BTools: []string{"delete"}}
	pack := DivergenceEvidencePack{Divergence: div}

	if _, err := Interpret(context.Background(), opts, pack, i18n.EN); err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	var sentReq chatCompletionRequest
	if err := json.Unmarshal(*lastBody, &sentReq); err != nil {
		t.Fatalf("request body not valid JSON: %v", err)
	}
	if !strings.Contains(sentReq.Messages[0].Content, "Divergence") {
		t.Errorf("system prompt should be the divergence one, got: %s", sentReq.Messages[0].Content)
	}
	if !strings.Contains(sentReq.Messages[1].Content, `"divergence"`) {
		t.Error("user message should embed the divergence evidence pack JSON")
	}
}

// TestCacheKey_DiffersAcrossPackTypes locks in a real risk the generic
// refactor introduced: two different evidence-pack types must never
// collide in the disk cache even if their JSON serializations happened to
// look similar — promptSpec.Version is what guarantees this (see
// evidencePackKind's doc comment in llm.go).
func TestCacheKey_DiffersAcrossPackTypes(t *testing.T) {
	comparePack := testPack(t)
	singlePack := SingleJourneyEvidencePack{}
	divPack := DivergenceEvidencePack{}

	k1, err := cacheKey("agent", comparePack, i18n.EN)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := cacheKey("agent", singlePack, i18n.EN)
	if err != nil {
		t.Fatal(err)
	}
	k3, err := cacheKey("agent", divPack, i18n.EN)
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k2 || k2 == k3 || k1 == k3 {
		t.Errorf("cache keys must differ across pack types: compare=%s single=%s divergence=%s", k1, k2, k3)
	}
}

// TestBuildEvidencePack_SizeBoundedRegardlessOfStructureRichness is P4.3's
// acceptance test: journey-<id>.json's new (P4.1) "structure" field can, in
// principle, be large — but EvidencePack (llm.go, the -compare LLM
// interpretation input) must not grow because of it. This is not
// hypothetical: BuildEvidencePack takes the full in-memory *Journey (not
// JourneySummary), and Summarize(j, lang) — which now always computes
// Structure — sits directly upstream of Compare/BuildEvidencePack in
// production (cmd_story.go's compareJourneys calls Summarize then Compare
// then BuildEvidencePack in sequence). Proving the pack's size tracks only
// step count (via ToolIndexEntry, already bounded by taskseg.Preview), not
// the richness of the per-step content Structure now also carries, is what
// keeps that upstream growth from leaking downstream.
func TestBuildEvidencePack_SizeBoundedRegardlessOfStructureRichness(t *testing.T) {
	small := buildJourneyWithArgsLen(t, 20)
	huge := buildJourneyWithArgsLen(t, 200000) // same fixture structure_test.go's volume guard uses

	packFor := func(j *Journey) EvidencePack {
		s := Summarize(j, i18n.EN) // computes Structure — the thing under test must not leak through
		cmp := Compare(s, s, i18n.EN)
		return BuildEvidencePack(j, j, cmp, i18n.EN)
	}

	smallChars := packFor(small).EstimateChars()
	hugeChars := packFor(huge).EstimateChars()

	// The only per-step field that could possibly grow with argsLen is
	// ToolIndexEntry.Brief — and Brief comes from RespText (never touched by
	// this fixture's huge tool-call args), not from the args themselves, so
	// it should not move at all. Req adds a few bytes per Step regardless of
	// argsLen. A tiny, step-count-scaled bound (not a proportional-to-
	// argsLen one) is the right assertion here.
	diff := hugeChars - smallChars
	const perStepOverhead = 64 // generous margin for Req string length variance, JSON escaping
	const steps = 4
	if diff > perStepOverhead*steps {
		t.Errorf("EvidencePack grew by %d chars when per-step tool-call args grew by 200000 bytes — want growth bounded by ~%d (step count only), not tracking conversation content size", diff, perStepOverhead*steps)
	}
}

// TestBuildSingleJourneyEvidencePack_SizeBoundedRegardlessOfStructureRichness
// is TestBuildEvidencePack_SizeBoundedRegardlessOfStructureRichness's
// -journey counterpart (llm_single.go's BuildSingleJourneyEvidencePack,
// used by cmd_story.go's renderJourney for the single-Journey LLM
// interpretation section) — same guard, same reasoning, different pack type.
func TestBuildSingleJourneyEvidencePack_SizeBoundedRegardlessOfStructureRichness(t *testing.T) {
	small := buildJourneyWithArgsLen(t, 20)
	huge := buildJourneyWithArgsLen(t, 200000)

	packFor := func(j *Journey) SingleJourneyEvidencePack {
		m := ComputeMetrics(j)
		findings := ComputeFindings(j, i18n.EN)
		_ = Summarize(j, i18n.EN) // computes Structure as a production caller would upstream — must not leak into this pack
		return BuildSingleJourneyEvidencePack(j, m, findings, i18n.EN)
	}

	smallChars := packFor(small).EstimateChars()
	hugeChars := packFor(huge).EstimateChars()

	diff := hugeChars - smallChars
	const perStepOverhead = 64
	const steps = 4
	if diff > perStepOverhead*steps {
		t.Errorf("SingleJourneyEvidencePack grew by %d chars when per-step tool-call args grew by 200000 bytes — want growth bounded by ~%d (step count only), not tracking conversation content size", diff, perStepOverhead*steps)
	}
}
