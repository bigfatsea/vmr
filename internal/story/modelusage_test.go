// Ver 2026-08-12 23:40, by Opus 5
package story

import (
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// TestComputeModelUsage_DetectsSwitchDespiteConstantVirtualModel is the
// gatekeeper for §5.5 ①'s pitfall (see modelusage.go's package doc
// comment): a real Journey where audit.Record.Model (the virtual model,
// "agent" for every step via mkRec/mkRecWithUsage) never changes, but the
// real upstream model recorded in each step's last attempt does. If this
// feature had been built on Manifest.Model instead of the attempt's
// structured fields, ModelSwitches would always come back empty here.
func TestComputeModelUsage_DetectsSwitchDespiteConstantVirtualModel(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 7, 9, 10, m, 0, 0, time.UTC) }
	u1 := msg("user", "investigate X")

	r1 := mkRecWithUsage(at(0), []any{msg("system", "sys"), u1}, "ok1", 100, 10)
	r1.Attempts = []audit.Attempt{{Endpoint: "openai-completions:p1:model-a", Protocol: "openai-completions", Provider: "p1", Model: "model-a"}}

	r2 := mkRecWithUsage(at(1), []any{msg("system", "sys"), u1, msg("assistant", "ok1"), msg("user", "continue")}, "ok2", 200, 20)
	r2.Attempts = []audit.Attempt{{Endpoint: "openai-completions:p1:model-b", Protocol: "openai-completions", Provider: "p1", Model: "model-b"}}

	// Both records carry the SAME virtual model ("agent", set by mkRec) —
	// the whole point of this test.
	if r1.Model != "agent" || r2.Model != "agent" {
		t.Fatalf("test setup broken: virtual model must be constant, got %q / %q", r1.Model, r2.Model)
	}

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	m := ComputeMetrics(j)

	if len(m.ModelSwitches) != 1 {
		t.Fatalf("ModelSwitches = %+v, want exactly 1 (model-a -> model-b)", m.ModelSwitches)
	}
	sw := m.ModelSwitches[0]
	if sw.From != "p1:model-a" || sw.To != "p1:model-b" {
		t.Errorf("switch = %+v, want p1:model-a -> p1:model-b", sw)
	}
	if len(m.ModelUsage) != 2 {
		t.Fatalf("ModelUsage = %+v, want 2 distinct (provider,model) entries", m.ModelUsage)
	}
}

func TestComputeModelUsage_SingleModelHasNoSwitches(t *testing.T) {
	steps := []*Step{
		{Seq: 1, Manifest: &ctxgraph.Manifest{UsageOK: true, Usage: chatmsg.Usage{In: 100, Out: 10}},
			Rec: &audit.Record{Attempts: []audit.Attempt{{Provider: "p1", Model: "m1"}}}},
		{Seq: 2, Manifest: &ctxgraph.Manifest{UsageOK: true, Usage: chatmsg.Usage{In: 200, Out: 20}},
			Rec: &audit.Record{Attempts: []audit.Attempt{{Provider: "p1", Model: "m1"}}}},
	}
	stats, switches := computeModelUsage(steps)
	if len(switches) != 0 {
		t.Errorf("switches = %+v, want none for a single-model journey", switches)
	}
	if len(stats) != 1 || stats[0].Steps != 2 || stats[0].TokensIn != 300 || stats[0].TokensOut != 30 {
		t.Errorf("stats = %+v, want one aggregated entry (steps=2, in=300, out=30)", stats)
	}
}

// A switch observed on a Step whose own request needed more than one
// upstream attempt gets the observational OnFailoverStep marker.
func TestComputeModelUsage_MarksOnFailoverStep(t *testing.T) {
	steps := []*Step{
		{Seq: 1, Manifest: &ctxgraph.Manifest{}, Rec: &audit.Record{Attempts: []audit.Attempt{{Provider: "p1", Model: "m1"}}}},
		{Seq: 2, Manifest: &ctxgraph.Manifest{}, Rec: &audit.Record{Attempts: []audit.Attempt{
			{Provider: "p1", Model: "m1", Error: "rate_limit"}, // failed first attempt
			{Provider: "p2", Model: "m2"},                      // succeeded on a different account
		}}},
	}
	_, switches := computeModelUsage(steps)
	if len(switches) != 1 || !switches[0].OnFailoverStep {
		t.Errorf("switches = %+v, want exactly 1 with OnFailoverStep=true", switches)
	}
}

// A switch observed on a Step whose request succeeded on the first attempt
// must NOT be marked as a failover — the marker is specifically about this
// Step's own attempt count, not "did some switch happen at all".
func TestComputeModelUsage_NoFailoverMarkerOnSingleAttemptStep(t *testing.T) {
	steps := []*Step{
		{Seq: 1, Manifest: &ctxgraph.Manifest{}, Rec: &audit.Record{Attempts: []audit.Attempt{{Provider: "p1", Model: "m1"}}}},
		{Seq: 2, Manifest: &ctxgraph.Manifest{}, Rec: &audit.Record{Attempts: []audit.Attempt{{Provider: "p2", Model: "m2"}}}},
	}
	_, switches := computeModelUsage(steps)
	if len(switches) != 1 || switches[0].OnFailoverStep {
		t.Errorf("switches = %+v, want exactly 1 with OnFailoverStep=false", switches)
	}
}

// TestComputeModelUsage_FailedOverFromEndpointIsVisible is the cheap-
// half fix: a Step whose first attempt failed over to a different endpoint
// used to make the failed-over-FROM endpoint invisible in the usage table
// entirely (stepUpstream only ever read the LAST attempt). It must now show
// up with Steps counted, even though it contributed no tokens.
func TestComputeModelUsage_FailedOverFromEndpointIsVisible(t *testing.T) {
	steps := []*Step{
		{Seq: 1, Manifest: &ctxgraph.Manifest{UsageOK: true, Usage: chatmsg.Usage{In: 100, Out: 10}},
			Rec: &audit.Record{Attempts: []audit.Attempt{
				{Provider: "p1", Model: "m1", Error: "rate_limit"}, // failed over away from
				{Provider: "p2", Model: "m2"},                      // succeeded here
			}}},
	}
	stats, _ := computeModelUsage(steps)
	byKey := map[string]ModelUsageStat{}
	for _, st := range stats {
		byKey[st.Provider+":"+st.Model] = st
	}
	if len(stats) != 2 {
		t.Fatalf("stats = %+v, want 2 entries (both the failed-over-from and the successful endpoint)", stats)
	}
	failedFrom, ok := byKey["p1:m1"]
	if !ok || failedFrom.Steps != 1 {
		t.Errorf("p1:m1 (failed over away from) = %+v, want Steps=1", failedFrom)
	}
	if failedFrom.TokensIn != 0 || failedFrom.TokensOut != 0 {
		t.Errorf("p1:m1 must attribute NO tokens (it never produced usage): %+v", failedFrom)
	}
	succeeded, ok := byKey["p2:m2"]
	if !ok || succeeded.Steps != 1 || succeeded.TokensIn != 100 || succeeded.TokensOut != 10 {
		t.Errorf("p2:m2 (succeeded) = %+v, want Steps=1, TokensIn=100, TokensOut=10", succeeded)
	}
}

// TestComputeModelUsage_RepeatedSameEndpointStepsAllCounted is the
// Regression test: ensures that a
// naive "only bump Steps the first time this (provider,model) entry is
// created" would undercount every Step after the first one that reuses the
// same endpoint — the overwhelmingly common case (most Journeys stay on one
// endpoint across many Steps). Every Step touching the same pair must add 1.
func TestComputeModelUsage_RepeatedSameEndpointStepsAllCounted(t *testing.T) {
	steps := []*Step{
		{Seq: 1, Rec: &audit.Record{Attempts: []audit.Attempt{{Provider: "p1", Model: "m1"}}}},
		{Seq: 2, Rec: &audit.Record{Attempts: []audit.Attempt{{Provider: "p1", Model: "m1"}}}},
		{Seq: 3, Rec: &audit.Record{Attempts: []audit.Attempt{{Provider: "p1", Model: "m1"}}}},
	}
	stats, _ := computeModelUsage(steps)
	if len(stats) != 1 || stats[0].Steps != 3 {
		t.Fatalf("stats = %+v, want a single entry with Steps=3 (one per Step, not just the first)", stats)
	}
}

func TestStepUpstream_FallsBackToEndpointSplit(t *testing.T) {
	for _, tc := range []struct {
		name         string
		endpoint     string
		wantProvider string
		wantModel    string
	}{
		{"colon format", "openai-completions:new-provider:new-model", "new-provider", "new-model"},
		{"slash format (legacy)", "openai/old-provider/old-model", "old-provider", "old-model"},
		{"no attempts, unresolvable endpoint", "-", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Step{
				Manifest: &ctxgraph.Manifest{Endpoint: tc.endpoint},
				Rec:      &audit.Record{}, // no structured Attempts fields -> must fall back
			}
			provider, model := stepUpstream(s)
			if provider != tc.wantProvider || model != tc.wantModel {
				t.Errorf("stepUpstream(%q) = (%q, %q), want (%q, %q)", tc.endpoint, provider, model, tc.wantProvider, tc.wantModel)
			}
		})
	}
}

func TestComputeModelUsage_UnresolvableStepsContributeNothing(t *testing.T) {
	steps := []*Step{
		{Seq: 1, Manifest: &ctxgraph.Manifest{Endpoint: "-"}, Rec: &audit.Record{}},
		{Seq: 2, Manifest: &ctxgraph.Manifest{}, Rec: &audit.Record{Attempts: []audit.Attempt{{Provider: "p1", Model: "m1"}}}},
	}
	stats, switches := computeModelUsage(steps)
	if len(stats) != 1 {
		t.Errorf("stats = %+v, want only the one resolvable step", stats)
	}
	if len(switches) != 0 {
		t.Errorf("switches = %+v, want none (unresolvable step can't anchor a comparison)", switches)
	}
}

// Determinism: two (provider,model) pairs tied on TokensIn must always
// sort the same way, or two runs over the same input could produce
// byte-different journey-<id>.json/.md.
func TestComputeModelUsage_DeterministicTieBreak(t *testing.T) {
	steps := []*Step{
		{Seq: 1, Manifest: &ctxgraph.Manifest{UsageOK: true, Usage: chatmsg.Usage{In: 100}},
			Rec: &audit.Record{Attempts: []audit.Attempt{{Provider: "zeta", Model: "m"}}}},
		{Seq: 2, Manifest: &ctxgraph.Manifest{UsageOK: true, Usage: chatmsg.Usage{In: 100}},
			Rec: &audit.Record{Attempts: []audit.Attempt{{Provider: "alpha", Model: "m"}}}},
	}
	stats, _ := computeModelUsage(steps)
	if len(stats) != 2 || stats[0].Provider != "alpha" || stats[1].Provider != "zeta" {
		t.Errorf("order = %+v, want alpha before zeta on a token tie", stats)
	}
}
