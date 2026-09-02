// Ver 2026-08-20 00:00, by Sonnet 5
package reqdetail

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/core"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// TestNormDescriptions_AllKnownStepsHaveText guards against a norm step
// name being added to the router-side trail (internal/respnorm)
// without a matching entry in i18n.Detail — writeNorms falls back to
// "（未知步骤）" for anything missing, which is silent and easy to forget.
func TestNormDescriptions_AllKnownStepsHaveText(t *testing.T) {
	for _, step := range []string{
		"model_rewrite", "done_appended", "think_strip", "thinking_process_strip",
		"buffered", "resumed_stream", "soft_block_detected", "opaque",
		"overflow_raw_passthrough", "crlf_framing_suspected",
		"thinking_process_pattern_detected",
	} {
		var b strings.Builder
		writeNorms(&b, []string{step}, i18n.Detail(i18n.EN))
		if strings.Contains(b.String(), "unknown step") {
			t.Errorf("norm step %q has no description in normDescriptions", step)
		}
	}
}

// TestAttemptUpstreamFallback covers backward compatibility with audit
// logs written before Attempt.Protocol/Provider/Model existed: the three
// segments must still be recoverable by splitting Endpoint via
// core.SplitEndpointLabel, which accepts both the current ":"-joined format
// and the "/"-joined form older audit logs used — a record with an empty
// structured triple can in principle carry either, so both must resolve
// (a prior version of this function only handled "/", silently returning
// ("","","") for a ":"-joined Endpoint whose structured fields were empty,
// disagreeing with internal/story/modelusage.go's stepUpstream on the same
// record).
func TestAttemptUpstreamFallback(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		a                                 audit.Attempt
		wantProtocol, wantProv, wantModel string
	}{
		{"new log: structured fields used directly",
			audit.Attempt{Endpoint: "openai-completions:minimax:MiniMax-M3", Protocol: "openai-completions", Provider: "minimax", Model: "MiniMax-M3"},
			"openai-completions", "minimax", "MiniMax-M3"},
		{"structured fields empty, ':'-joined Endpoint: falls back to splitting it",
			audit.Attempt{Endpoint: "openai-completions:minimax:MiniMax-M3"},
			"openai-completions", "minimax", "MiniMax-M3"},
		{"old log: falls back to splitting the '/'-joined Endpoint",
			audit.Attempt{Endpoint: "openai-completions/minimax/MiniMax-M3"},
			"openai-completions", "minimax", "MiniMax-M3"},
		{"old log: model name itself contains '/' (OpenRouter-style), only first two separators are structural",
			audit.Attempt{Endpoint: "openai-completions/openrouter/z-ai/glm-5.2"},
			"openai-completions", "openrouter", "z-ai/glm-5.2"},
		{"':'-joined Endpoint, model name itself contains '/' (OpenRouter-style)",
			audit.Attempt{Endpoint: "openai-completions:openrouter:z-ai/glm-5.2"},
			"openai-completions", "openrouter", "z-ai/glm-5.2"},
		{"old log, unparseable endpoint: no crash, empty triple",
			audit.Attempt{Endpoint: "not-a-real-endpoint"},
			"", "", ""},
		{"partial structured: Protocol only, falls back to splitting Endpoint for Provider and Model",
			audit.Attempt{Endpoint: "openai-completions:minimax:MiniMax-M3", Protocol: "openai-completions"},
			"openai-completions", "minimax", "MiniMax-M3"},
		{"partial structured: Provider only, falls back to splitting Endpoint for Protocol and Model",
			audit.Attempt{Endpoint: "openai-completions:minimax:MiniMax-M3", Provider: "minimax"},
			"openai-completions", "minimax", "MiniMax-M3"},
		{"no attempt at all", audit.Attempt{}, "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			protocol, provider, model := AttemptUpstream(tc.a)
			if protocol != tc.wantProtocol || provider != tc.wantProv || model != tc.wantModel {
				t.Errorf("AttemptUpstream(%+v) = (%q,%q,%q), want (%q,%q,%q)",
					tc.a, protocol, provider, model, tc.wantProtocol, tc.wantProv, tc.wantModel)
			}
		})
	}
}

// TestRealModelFallback covers RealModel() specifically against an
// old-format record whose only endpoint info is the "/"-joined Endpoint
// string — the exact scenario that regressed detail filenames to "_none_"
// for every historical record before AttemptUpstream's fallback was added.
func TestRealModelFallback(t *testing.T) {
	rec := &audit.Record{Attempts: []audit.Attempt{{Endpoint: "openai-completions/minimax/MiniMax-M3"}}}
	if got := RealModel(rec); got != "MiniMax-M3" {
		t.Errorf("RealModel = %q, want %q", got, "MiniMax-M3")
	}
	if got := RealModel(&audit.Record{}); got != "none" {
		t.Errorf("RealModel with no attempts = %q, want none", got)
	}
}

// TestFileName_DeterministicAndCoordinateUnique locks in the naming
// contract: no batch-order dependency (no "used" collision counter — the
// coordinate hash is unique on its own), no local-timezone dependency (the
// timestamp segment uses ts's own offset, not fmtutil.DisplayZone), and
// unsafe characters sanitized in the decorative model/outcome segments.
func TestFileName_DeterministicAndCoordinateUnique(t *testing.T) {
	ts := time.Date(2026, 7, 9, 0, 31, 6, 804_000_000, time.FixedZone("CST", 8*3600))
	req := ctxgraph.ReqCoord("vmr-audit-2026-07-08.jsonl", 42)

	got := FileName(ts, "agent", "MiniMax-M3", "ok", req)
	if !strings.HasPrefix(got, "20260709-003106.804_agent_MiniMax-M3_ok_") || !strings.HasSuffix(got, ".md") {
		t.Errorf("got %q, want the ts-own-offset/model/outcome prefix with a coordinate-hash suffix", got)
	}

	// Same coordinate, called twice: identical name (deterministic, no
	// hidden batch state).
	if got2 := FileName(ts, "agent", "MiniMax-M3", "ok", req); got2 != got {
		t.Errorf("FileName not deterministic: %q vs %q", got, got2)
	}

	// A different coordinate (different line) for the SAME ts/model/outcome
	// must differ — this is what replaced the old "-2" collision suffix.
	req2 := ctxgraph.ReqCoord("vmr-audit-2026-07-08.jsonl", 43)
	if got3 := FileName(ts, "agent", "MiniMax-M3", "ok", req2); got3 == got {
		t.Errorf("two different coordinates produced the same filename: %q", got)
	}

	// Unsafe characters sanitized.
	if got := FileName(ts, "my model/v2", "y", "error", req); strings.ContainsAny(got, "/ ") {
		t.Errorf("unsafe characters not sanitized: %q", got)
	}

	// Rejected request: empty model/real model fall back to their labels
	// ("(rejected)"/"none"), sanitized the same as any other segment — the
	// parens are stripped by sanitizeName like any other unsafe character.
	if got := FileName(ts, "", "", "error", req); !strings.Contains(got, "rejected") || !strings.Contains(got, "none") {
		t.Errorf("rejected name = %q", got)
	}
}

func TestFileNameForRecord_MatchesFileName(t *testing.T) {
	ts := time.Date(2026, 7, 9, 0, 31, 6, 804_000_000, time.FixedZone("CST", 8*3600))
	rec := &audit.Record{TS: ts, Model: "agent", Outcome: "ok",
		Attempts: []audit.Attempt{{Endpoint: "openai-completions:minimax:MiniMax-M3", Model: "MiniMax-M3"}}}
	got := FileNameForRecord(rec, "vmr-audit-2026-07-08.jsonl", 42)
	want := FileName(ts, "agent", "MiniMax-M3", "ok", ctxgraph.ReqCoord("vmr-audit-2026-07-08.jsonl", 42))
	if got != want {
		t.Errorf("FileNameForRecord = %q, want %q (must funnel through the same FileName formatter)", got, want)
	}
}

func TestFileNameForManifest_MatchesFileNameForRecord(t *testing.T) {
	// FileNameForManifest is what a "previous turn" link computes when only
	// the predecessor's Manifest (not its full audit.Record) is available —
	// it must agree with what FileNameForRecord would have computed for
	// that same record, or the link points at a name nothing was ever
	// written under.
	ts := time.Date(2026, 7, 9, 0, 31, 6, 804_000_000, time.FixedZone("CST", 8*3600))
	rec := &audit.Record{TS: ts, Model: "agent", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Body: map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}}},
		},
		Attempts: []audit.Attempt{{Endpoint: "openai-completions:minimax:MiniMax-M3", Model: "MiniMax-M3"}}}
	m, ok := ctxgraph.BuildManifest(rec, "vmr-audit-2026-07-08.jsonl", 42)
	if !ok {
		t.Fatal("BuildManifest returned ok=false")
	}
	got := FileNameForManifest(m)
	want := FileNameForRecord(rec, "vmr-audit-2026-07-08.jsonl", 42)
	if got != want {
		t.Errorf("FileNameForManifest = %q, want %q (must agree with FileNameForRecord for the same record)", got, want)
	}
}

func TestCodeFence(t *testing.T) {
	// Content containing a triple-backtick run must get a longer fence.
	out := codeFence("hi\n```go\ncode\n```")
	if !strings.HasPrefix(out, "````\n") || !strings.HasSuffix(out, "````\n") {
		t.Errorf("fence not extended:\n%s", out)
	}
	if out := codeFence("plain"); !strings.HasPrefix(out, "```\n") {
		t.Errorf("plain fence wrong:\n%s", out)
	}
}

func TestDiffHeaderTable(t *testing.T) {
	base := http.Header{"Same": {"v"}, "Changed": {"old"}, "Removed": {"gone"}}
	other := http.Header{"Same": {"v"}, "Changed": {"new"}, "Added": {"fresh"}}
	table, changed := diffHeaderTable(base, other, i18n.Detail(i18n.EN))
	if changed != 3 {
		t.Errorf("changed = %d, want 3", changed)
	}
	for _, want := range []string{
		"| 🟢 | Added | fresh |",
		"| 🔴 | Removed | ~~gone~~ |",
		"| 🔶 | Changed | old → new |",
		"| | Same | v |", // unchanged still listed, unmarked
	} {
		if !strings.Contains(table, want) {
			t.Errorf("table missing %q:\n%s", want, table)
		}
	}
}

func TestRenderBodyDiffMarksChanges(t *testing.T) {
	client := map[string]any{
		"model": "agent", "stream": true,
		"messages": []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	attempt := map[string]any{
		"model": "MiniMax-M3", "stream": true,
		"messages": []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "user", "content": "hello resized"},
		},
	}
	var b strings.Builder
	renderBodyDiff(&b, client, attempt, i18n.Detail(i18n.EN))
	out := b.String()
	for _, want := range []string{
		`| 🔶 | model | "agent" → "MiniMax-M3" |`,
		"| | stream | true |",         // unchanged field still listed
		"- #1 system · 3 chars",       // unchanged message still listed, unmarked
		"🔶 #2 user · 5 → 13 chars",    // changed message marked with sizes
		"hello resized",               // attempt-side content available inline
		"Messages diff (2, 1 changed", // summary carries the change count
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestRenderBodyDiffExcludesResponsesConversationFields proves the
// param-diff table's "bulky" exclusion — already covering "messages"/
// "tools"/"system" — also covers openai-responses' "input"/"instructions",
// so a Responses-protocol detail page doesn't dump the entire conversation
// twice: once (correctly) under "Messages diff", and again as raw JSON in
// the top-level param table meant for small metadata fields only.
func TestRenderBodyDiffExcludesResponsesConversationFields(t *testing.T) {
	client := map[string]any{
		"model": "agent", "instructions": "you are helpful",
		"input": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	attempt := map[string]any{
		"model": "real-model", "instructions": "you are helpful",
		"input": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	var b strings.Builder
	renderBodyDiff(&b, client, attempt, i18n.Detail(i18n.EN))
	out := b.String()
	if strings.Contains(out, "\"input\"") || strings.Contains(out, "| input |") {
		t.Errorf("input array leaked into the param diff table instead of staying in Messages diff:\n%s", out)
	}
	if strings.Contains(out, "| instructions |") {
		t.Errorf("instructions leaked into the param diff table:\n%s", out)
	}
}

func TestRoleChars(t *testing.T) {
	// openai-completions shape: roles taken as-is, tool_calls counted to assistant.
	openai := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": strings.Repeat("s", 10)},
			map[string]any{"role": "user", "content": strings.Repeat("u", 30)},
			map[string]any{"role": "assistant", "content": strings.Repeat("a", 20)},
			map[string]any{"role": "tool", "content": strings.Repeat("t", 40), "tool_call_id": "c1"},
		},
	}
	rc := RoleChars(openai)
	if rc["system"] != 10 || rc["user"] != 30 || rc["assistant"] != 20 || rc["tool"] != 40 {
		t.Errorf("openai RoleChars = %v", rc)
	}

	// anthropic shape: top-level system counts as one message; tool_result
	// parts inside a user message count as "tool", not "user".
	anthropic := map[string]any{
		"system": strings.Repeat("s", 5),
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": strings.Repeat("u", 7)},
				map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "xx"},
			}},
		},
	}
	rc = RoleChars(anthropic)
	if rc["system"] != 5 || rc["user"] != 7 || rc["tool"] == 0 {
		t.Errorf("anthropic RoleChars = %v", rc)
	}

	// openai-responses shape: top-level instructions counts as "system";
	// function_call/function_call_output Items have no "role" key at all
	// and must still land under a sensible bucket ("assistant"/"tool"), not
	// silently vanish into an empty-string role.
	responses := map[string]any{
		"instructions": strings.Repeat("s", 5),
		"input": []any{
			map[string]any{"role": "user", "content": strings.Repeat("u", 7)},
			map[string]any{"type": "function_call", "call_id": "c1", "name": "exec", "arguments": strings.Repeat("a", 9)},
			map[string]any{"type": "function_call_output", "call_id": "c1", "output": strings.Repeat("t", 11)},
		},
	}
	rc = RoleChars(responses)
	if rc["system"] != 5 || rc["user"] != 7 || rc["assistant"] == 0 || rc["tool"] == 0 {
		t.Errorf("openai-responses RoleChars = %v", rc)
	}
	if _, hasEmptyRole := rc[""]; hasEmptyRole {
		t.Errorf("RoleChars should never bucket a Responses non-message Item under an empty role: %v", rc)
	}

	// Non-chat bodies yield nothing.
	if rc := RoleChars("not json"); rc != nil {
		t.Errorf("string body RoleChars = %v", rc)
	}
}

// TestRoleTokens locks in that RoleTokens shares RoleChars' traversal (same
// per-role attribution) but sizes each fragment with tokenutil.EstimateText
// instead of a rune count.
func TestRoleTokens(t *testing.T) {
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": strings.Repeat("u", 40)},
			map[string]any{"role": "assistant", "content": strings.Repeat("a", 80)},
		},
	}
	rt := RoleTokens(body)
	// 40 * 0.206 = 8.24 -> 8; 80 * 0.206 = 16.48 -> 16
	if rt["user"] != 8 || rt["assistant"] != 16 {
		t.Errorf("RoleTokens = %v, want user=8 assistant=16", rt)
	}
	if rc := RoleTokens("not json"); rc != nil {
		t.Errorf("string body RoleTokens = %v", rc)
	}
}

func TestRoleStatLine(t *testing.T) {
	chars := map[string]int64{"system": 10, "user": 30, "assistant": 20, "tool": 40}
	got := roleStatLine(chars, false, false)
	want := "system 10.0% · user 30.0% · assistant 20.0% · tool 40.0%"
	if got != want {
		t.Errorf("share-only line:\ngot  %q\nwant %q", got, want)
	}
	withChars := roleStatLine(chars, true, false)
	if !strings.Contains(withChars, "tool 40 (40.0%)") {
		t.Errorf("withChars line missing counts: %q", withChars)
	}
	if roleStatLine(nil, false, false) != "" {
		t.Error("empty map should render empty line")
	}
}

// TestRender_RawPreStrip covers both states of the "② VMR → 上游" raw
// pre-strip display: full content when the audit record captured it
// (RawPreStrip populated — see internal/respnorm), and a graceful
// "not captured" note for records logged before that capture existed.
func TestRender_RawPreStrip(t *testing.T) {
	base := func(rawPreStrip any) *audit.Record {
		return &audit.Record{TS: time.Now(), Model: "agent", Outcome: "ok",
			Client: audit.Exchange{
				Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: http.Header{}, Body: map[string]any{}},
			},
			Attempts: []audit.Attempt{{
				Endpoint: "openai-completions/minimax/MiniMax-M3", URL: "https://x/v1",
				Request:     audit.Message{Headers: http.Header{}},
				Response:    &audit.Message{Status: 200, Headers: http.Header{}},
				Norm:        []string{"buffered", "think_strip", "model_rewrite"},
				RawPreStrip: rawPreStrip,
			}},
		}
	}

	withRaw := Render(base(`data: {"choices":[{"delta":{"content":"<think>step 1</think>final answer"}}]}`+"\n\n"), "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, false)
	if !strings.Contains(withRaw, "Pre-strip raw content") || !strings.Contains(withRaw, "<think>step 1</think>final answer") {
		t.Errorf("full pre-strip content not rendered:\n%s", withRaw)
	}
	if strings.Contains(withRaw, "didn't retain the pre-strip raw content") {
		t.Error("should not show the unavailable note when RawPreStrip is populated")
	}

	withoutRaw := Render(base(nil), "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, false)
	if !strings.Contains(withoutRaw, "didn't retain the pre-strip raw content") {
		t.Errorf("missing graceful fallback note when RawPreStrip is nil:\n%s", withoutRaw)
	}
}

// TestRender_FactsLine locks in that the detail Markdown surfaces
// audit.Record.Facts (vmr's own pre-routing analysis) verbatim — no
// recomputation from the stored request body — and that it appears near
// the top of the document (before section ① renders the full request),
// not buried after the detailed sections. A nil Facts (request rejected
// before fact computation ran) must render nothing, not a blank/zero line.
func TestRender_FactsLine(t *testing.T) {
	base := func(facts *core.RequestFacts) *audit.Record {
		return &audit.Record{TS: time.Now(), Model: "agent", Outcome: "ok",
			Client: audit.Exchange{
				Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: http.Header{}, Body: map[string]any{}},
			},
			Facts: facts,
		}
	}

	withFacts := Render(base(&core.RequestFacts{HasImage: true, HasTools: false, EstimatedTokens: 1234}), "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, false)
	if !strings.Contains(withFacts, "VMR pre-routing judgment") {
		t.Errorf("facts line missing:\n%s", withFacts)
	}
	if !strings.Contains(withFacts, "Capabilities required: `image`") {
		t.Errorf("facts line should list only the detected capability `image`:\n%s", withFacts)
	}
	if strings.Contains(withFacts, "`tools`") {
		t.Errorf("facts line should not list tools when HasTools is false:\n%s", withFacts)
	}
	if factsIdx, reqIdx := strings.Index(withFacts, "VMR pre-routing judgment"), strings.Index(withFacts, "① Client"); factsIdx < 0 || reqIdx < 0 || factsIdx > reqIdx {
		t.Errorf("facts line must appear before section ①, got factsIdx=%d reqIdx=%d", factsIdx, reqIdx)
	}
	if !strings.Contains(withFacts, "Estimated token count: 1.2 KT") {
		t.Errorf("facts line should render the plain (non-EST-suffixed) token estimate:\n%s", withFacts)
	}

	both := Render(base(&core.RequestFacts{HasImage: true, HasTools: true, EstimatedTokens: 500}), "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, false)
	if !strings.Contains(both, "Capabilities required: `image`, `tools`") {
		t.Errorf("facts line should list both detected capabilities joined by \", \":\n%s", both)
	}
	if !strings.Contains(both, "Estimated token count: 500 T") {
		t.Errorf("facts line should render sub-1000 estimate as plain T:\n%s", both)
	}

	neither := Render(base(&core.RequestFacts{HasImage: false, HasTools: false, EstimatedTokens: 10}), "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, false)
	if !strings.Contains(neither, "Capabilities required: none") {
		t.Errorf("facts line should show none when no capability is detected:\n%s", neither)
	}

	withoutFacts := Render(base(nil), "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, false)
	if strings.Contains(withoutFacts, "VMR pre-routing judgment") {
		t.Errorf("nil Facts must render nothing:\n%s", withoutFacts)
	}
}

// TestFileName_TimezoneIndependent proves the naming contract's other hard
// requirement: the filename must not change with the machine's display
// timezone (fmtutil.DisplayZone, which defaults to time.Local) — only the
// record's own embedded offset matters. FileName's implementation simply
// never references DisplayZone (unlike the pre-P2 detailFileName, which
// called ts.In(fmtutil.DisplayZone)), so this locks that in by mutating
// DisplayZone directly, the same way this package's own tests are allowed to
// (see fmtutil's package doc: zero production writes, only _test.go writes
// this var).
func TestFileName_TimezoneIndependent(t *testing.T) {
	ts := time.Date(2026, 7, 9, 0, 31, 6, 804_000_000, time.FixedZone("CST", 8*3600))
	req := ctxgraph.ReqCoord("vmr-audit-2026-07-08.jsonl", 42)
	want := FileName(ts, "agent", "MiniMax-M3", "ok", req)

	orig := fmtutil.DisplayZone
	defer func() { fmtutil.DisplayZone = orig }()
	for _, zone := range []*time.Location{time.UTC, time.FixedZone("EST", -5*3600), time.FixedZone("JST", 9*3600)} {
		fmtutil.DisplayZone = zone
		if got := FileName(ts, "agent", "MiniMax-M3", "ok", req); got != want {
			t.Errorf("DisplayZone=%v: FileName = %q, want %q (must not depend on the display timezone)", zone, got, want)
		}
	}
}

// TestRender_RejectedRecordNoManifest covers the m == nil case: a record
// whose client request body never parsed as a chat object (malformed
// JSON, missing auth, wrong shape) never gets a ctxgraph.Manifest at all
// (ctxgraph.BuildManifest returns ok=false for it) — it can never be
// another record's lineage predecessor either, so this is purely about
// Render/FileNameForRecord not panicking and falling back sanely when m
// and prev are both nil, the one shape every other test in this file
// passes a non-nil m for.
func TestRender_RejectedRecordNoManifest(t *testing.T) {
	rec := &audit.Record{TS: time.Now(), Outcome: "error",
		Client: audit.Exchange{
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: http.Header{}, Body: "not a json object"},
		},
	}
	got := Render(rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, false)
	if !strings.Contains(got, "(rejected)") {
		t.Errorf("rejected record's overview should show (rejected) for the missing model, got:\n%s", got)
	}
	if strings.Contains(got, "previous turn") || strings.Contains(got, "上一轮") {
		t.Errorf("a record with no Manifest can have no previous-turn link, got:\n%s", got)
	}

	name := FileNameForRecord(rec, "audit.jsonl", 1)
	if !strings.Contains(name, "rejected") || !strings.Contains(name, "none") {
		t.Errorf("FileNameForRecord for a rejected record = %q, want the (rejected)/none fallbacks", name)
	}
}

// sseBody builds a minimal valid SSE response body chatmsg.ReassembleSSE
// accepts, carrying text as the assistant's content — same shape
// cmd/vmr's storySSE test helper uses.
func sseBody(text string) string {
	return `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"` + text + `"}}],"model":"agent"}
data: {"choices":[{"index":0,"finish_reason":"stop","delta":{}}]}
data: [DONE]`
}

// TestRenderClientResponse_RawSSEIsReferenceNotCopy: the raw SSE wire body
// must no longer be inlined
// verbatim — only referenced by the record's own req coordinate — while
// the reassembled model output (renderStreamSummary's job, not a copy of
// the wire bytes) still renders in full.
func TestRenderClientResponse_RawSSEIsReferenceNotCopy(t *testing.T) {
	rec := &audit.Record{TS: time.Now(), Model: "agent", Outcome: "ok", Stream: true,
		Client: audit.Exchange{
			Request:  audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: http.Header{}, Body: map[string]any{}},
			Response: &audit.Message{Status: 200, Headers: http.Header{}, Body: sseBody("distinctive-reply-marker")},
		},
	}
	got := Render(rec, "vmr-audit-2026-07-08.jsonl", 42, nil, nil, taskseg.OpenClawAware, i18n.EN, false)

	// The reassembled content still renders in full (interpretation, not a
	// copy of the wire bytes).
	if !strings.Contains(got, "distinctive-reply-marker") {
		t.Errorf("reassembled model output missing, got:\n%s", got)
	}
	// The raw wire structure (only present in a verbatim SSE dump, never in
	// the reassembled summary) must be gone.
	if strings.Contains(got, `"delta":{"role":"assistant"`) {
		t.Errorf("raw SSE wire body still inlined verbatim, got:\n%s", got)
	}
	// A coordinate-based reference to fetch it on demand takes its place.
	coord := ctxgraph.ReqCoord("vmr-audit-2026-07-08.jsonl", 42)
	if !strings.Contains(got, coord) || !strings.Contains(got, "vmr replay -print -req") {
		t.Errorf("missing coordinate-based raw-SSE retrieval reference (want %q + \"vmr replay -print -req\"), got:\n%s", coord, got)
	}
}

// deltaFixture builds two related records sharing a common opening
// system+user prefix, for exercising P13.3's history-folding path:
// prevRec's messages are entirely covered by curRec's, so
// ctxgraph.Classify(prevManifest, curManifest).LCP > 0 and deltaStart
// lands after the shared prefix. lcp0 additionally makes curRec share
// NOTHING with prevRec (LCP == 0, and no leading system message at all),
// exercising the deltaStart == 0 boundary where nothing should fold.
func deltaFixture(t *testing.T, lcp0 bool) (curRec *audit.Record, curManifest, prevManifest *ctxgraph.Manifest) {
	t.Helper()
	at := func(m int) time.Time { return time.Date(2026, 8, 21, 9, m, 0, 0, time.UTC) }
	prevRec := &audit.Record{TS: at(0), Model: "agent", Outcome: "ok",
		Client: audit.Exchange{Request: audit.Message{Body: map[string]any{"messages": []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "user", "content": "prev-only-content-marker"},
		}}}},
	}
	curMsgs := []any{
		map[string]any{"role": "system", "content": "sys"},
		map[string]any{"role": "user", "content": "prev-only-content-marker"},
		map[string]any{"role": "assistant", "content": "new-reply-marker"},
		map[string]any{"role": "user", "content": "new-continue-marker"},
	}
	if lcp0 {
		// No leading system message and no shared content at all — LeadSys
		// == 0 and LCP == 0, so deltaStart == 0.
		curMsgs = []any{map[string]any{"role": "user", "content": "totally-unrelated-content"}}
	}
	curRec = &audit.Record{TS: at(1), Model: "agent", Outcome: "ok",
		Client: audit.Exchange{Request: audit.Message{Body: map[string]any{"messages": curMsgs}}},
	}
	var ok bool
	prevManifest, ok = ctxgraph.BuildManifest(prevRec, "vmr-audit-2026-07-08.jsonl", 41)
	if !ok {
		t.Fatal("BuildManifest(prevRec) returned ok=false")
	}
	curManifest, ok = ctxgraph.BuildManifest(curRec, "vmr-audit-2026-07-08.jsonl", 42)
	if !ok {
		t.Fatal("BuildManifest(curRec) returned ok=false")
	}
	return curRec, curManifest, prevManifest
}

// TestRenderClientRequest_FoldsHistoryBeforeDelta: messages before
// deltaStart (the shared prefix with
// prev) fold into one link to prev's own detail page instead of each
// being re-rendered in full.
func TestRenderClientRequest_FoldsHistoryBeforeDelta(t *testing.T) {
	curRec, curManifest, prevManifest := deltaFixture(t, false)
	got := Render(curRec, "vmr-audit-2026-07-08.jsonl", 42, curManifest, prevManifest, taskseg.OpenClawAware, i18n.EN, false)

	if strings.Contains(got, "prev-only-content-marker") {
		t.Errorf("history before deltaStart should be folded, not re-rendered, got:\n%s", got)
	}
	if !strings.Contains(got, "new-reply-marker") || !strings.Contains(got, "new-continue-marker") {
		t.Errorf("messages at/after deltaStart must still render in full, got:\n%s", got)
	}
	if n := strings.Count(got, "🆕"); n < 2 {
		t.Errorf("want at least 2 🆕-prefixed messages (reply+continue), got %d in:\n%s", n, got)
	}
	prevLink := FileNameForManifest(prevManifest)
	if !strings.Contains(got, prevLink) {
		t.Errorf("folded-history note must link to the previous turn's own page (%s), got:\n%s", prevLink, got)
	}
	if n := strings.Count(got, prevLink); n < 1 {
		t.Errorf("folded-history link should appear exactly once (not once per folded message), got %d in:\n%s", n, got)
	}
}

// TestRenderClientRequest_NoDeltaRendersFullHistory covers the prev == nil
// case (a lineage's first Step, or a stitch boundary, per §2.2's "the
// chain has to have a starting point somewhere") — folding must never
// trigger without a previous turn to point at.
func TestRenderClientRequest_NoDeltaRendersFullHistory(t *testing.T) {
	curRec, curManifest, _ := deltaFixture(t, false)
	got := Render(curRec, "vmr-audit-2026-07-08.jsonl", 42, curManifest, nil, taskseg.OpenClawAware, i18n.EN, false)
	if !strings.Contains(got, "prev-only-content-marker") {
		t.Errorf("with prev == nil, every message must render in full (no folding), got:\n%s", got)
	}
}

// TestRenderClientRequest_EvidenceLinkedZeroLCPDoesNotFold covers a
// boundary the independent review of this phase's ActionPlan flagged for
// explicit coverage (F-05): linkEvidence=true AND a leading system prompt
// (leadSys > 0) AND LCP == 0 against prev — so deltaStart == leadSys, not
// 0. The evidence-skip branch (i < leadSys) already consumes every index
// the fold branch could have folded, so foldedHistory must stay false: the
// system prompt gets an evidence link (not folded, not inlined) and the
// sole user message renders in full with the 🆕 prefix (not folded either,
// since deltaStart == leadSys means the loop's i < deltaStart never fires
// once evidence-skip has already handled [0, leadSys)).
func TestRenderClientRequest_EvidenceLinkedZeroLCPDoesNotFold(t *testing.T) {
	prevRec := &audit.Record{TS: time.Now(), Model: "agent", Outcome: "ok",
		Client: audit.Exchange{Request: audit.Message{Body: map[string]any{"messages": []any{
			map[string]any{"role": "system", "content": "old-sys"},
			map[string]any{"role": "user", "content": "prev-marker"},
		}}}},
	}
	curRec := &audit.Record{TS: time.Now(), Model: "agent", Outcome: "ok",
		Client: audit.Exchange{Request: audit.Message{Body: map[string]any{"messages": []any{
			map[string]any{"role": "system", "content": "new-sys-content"},
			map[string]any{"role": "user", "content": "totally-different-user-msg"},
		}}}},
	}
	prevManifest, ok := ctxgraph.BuildManifest(prevRec, "vmr-audit-2026-07-08.jsonl", 41)
	if !ok {
		t.Fatal("BuildManifest(prevRec) returned ok=false")
	}
	curManifest, ok := ctxgraph.BuildManifest(curRec, "vmr-audit-2026-07-08.jsonl", 42)
	if !ok {
		t.Fatal("BuildManifest(curRec) returned ok=false")
	}
	if curManifest.LeadSys == 0 {
		t.Fatal("fixture invalid: want curRec to have a leading system message")
	}

	got := Render(curRec, "vmr-audit-2026-07-08.jsonl", 42, curManifest, prevManifest, taskseg.OpenClawAware, i18n.EN, true)

	if !strings.Contains(got, "System Prompt") || !strings.Contains(got, "../evidence/") {
		t.Errorf("system prompt should get an evidence link (not folded, not inlined), got:\n%s", got)
	}
	// "prior context" alone would also match the unrelated, always-present
	// IncrementNote summary line — check for HistoryFoldedNote's own
	// distinguishing wording instead.
	if strings.Contains(got, "previous turn's detail page") {
		t.Errorf("no folded-history note should appear when deltaStart == leadSys, got:\n%s", got)
	}
	if !strings.Contains(got, "totally-different-user-msg") {
		t.Errorf("the sole user message must still render in full, got:\n%s", got)
	}
}

// TestRenderClientRequest_ZeroLCPRendersFullHistory covers the
// deltaStart == 0 boundary (prev != nil but nothing is shared, e.g. a
// same-lineage full context reset) — the loop condition is i < deltaStart,
// which is never true when deltaStart is 0, so nothing should fold and
// every message renders with the 🆕 prefix.
func TestRenderClientRequest_ZeroLCPRendersFullHistory(t *testing.T) {
	curRec, curManifest, prevManifest := deltaFixture(t, true)
	got := Render(curRec, "vmr-audit-2026-07-08.jsonl", 42, curManifest, prevManifest, taskseg.OpenClawAware, i18n.EN, false)
	if !strings.Contains(got, "totally-unrelated-content") {
		t.Errorf("with deltaStart == 0, the message must render in full (no folding), got:\n%s", got)
	}
	if !strings.Contains(got, "🆕") {
		t.Errorf("with deltaStart == 0, the sole message should still carry the 🆕 prefix, got:\n%s", got)
	}
}

// TestRenderOverviewClientAddrEscaped verifies that special characters
// (e.g. pipes, newlines) in Client.Addr are escaped so the Markdown
// table structure remains intact.
func TestRenderOverviewClientAddrEscaped(t *testing.T) {
	rec := &audit.Record{
		TS:      time.Now(),
		Model:   "agent",
		Outcome: "ok",
		Client: audit.Exchange{
			Addr:    "10.0.0.1:1234 | bad\npipe",
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: map[string]any{}},
		},
	}
	got := Render(rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, false)
	if !strings.Contains(got, `10.0.0.1:1234 \| bad pipe`) {
		t.Errorf("overview table must escape pipe and newline in client addr, got:\n%s", got)
	}
}
