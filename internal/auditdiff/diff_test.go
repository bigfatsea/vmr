// Ver 2026-09-13, by Sonnet 5

package auditdiff

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
)

func makeTestRecord(model, protocol, sysPrompt string, msgs []map[string]string, tools []string, inTok, outTok, cacheRead int64) audit.Record {
	body := map[string]any{
		"model": model,
	}
	var msgList []any
	if sysPrompt != "" {
		msgList = append(msgList, map[string]any{
			"role":    "system",
			"content": sysPrompt,
		})
	}
	for _, m := range msgs {
		msgList = append(msgList, map[string]any{
			"role":    m["role"],
			"content": m["content"],
		})
	}
	body["messages"] = msgList

	if len(tools) > 0 {
		var toolList []any
		for _, t := range tools {
			toolList = append(toolList, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": t,
				},
			})
		}
		body["tools"] = toolList
	}

	rec := audit.Record{
		TS:       time.Now(),
		Model:    model,
		Protocol: protocol,
		Outcome:  "ok",
		Attempts: []audit.Attempt{
			{
				Endpoint: "provider:mock:model",
				Response: &audit.Message{Status: 200},
			},
		},
	}
	rec.Client.Request.Body = body

	respBody := map[string]any{
		"usage": map[string]any{
			"prompt_tokens":           inTok,
			"completion_tokens":       outTok,
			"input_tokens":            inTok - cacheRead,
			"output_tokens":           outTok,
			"cache_read_input_tokens": cacheRead,
			"prompt_tokens_details": map[string]any{
				"cached_tokens": cacheRead,
			},
		},
	}
	rec.Client.Response = &audit.Message{
		Body: respBody,
	}

	return rec
}

// buildManifests turns two synthetic records into the (*Manifest, *Manifest)
// pair Compute takes — the same BuildManifest call cmd_diff.go itself makes
// after internal/replay.LoadRecord resolves a real coordinate; path/line
// here are pure identity metadata (BuildManifest does no file I/O), so a
// placeholder coordinate string is enough for these unit tests.
func buildManifests(t *testing.T, recA, recB *audit.Record) (*ctxgraph.Manifest, *ctxgraph.Manifest) {
	t.Helper()
	mA, okA := ctxgraph.BuildManifest(recA, "a.jsonl", 1)
	if !okA {
		t.Fatal("BuildManifest(recA) returned ok=false")
	}
	mB, okB := ctxgraph.BuildManifest(recB, "b.jsonl", 2)
	if !okB {
		t.Fatal("BuildManifest(recB) returned ok=false")
	}
	return mA, mB
}

// roundTrip re-encodes rec through JSON, the same transformation a record
// undergoes going through the real audit log (replay.LoadRecord's own
// json.Unmarshal into audit.Record) — the request/response Body maps
// coming out of that hold float64 for every number (encoding/json's own
// convention), never the int64 a Go literal map would carry, and
// chatmsg.num only accepts float64/json.Number. A test that skips this
// round-trip would test Compute against a *audit.Record shape production
// code never actually receives.
func roundTrip(t *testing.T, rec audit.Record) audit.Record {
	t.Helper()
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	var out audit.Record
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	return out
}

func computeAndRender(t *testing.T, recA, recB audit.Record) string {
	t.Helper()
	recA = roundTrip(t, recA)
	recB = roundTrip(t, recB)
	mA, mB := buildManifests(t, &recA, &recB)
	rep := Compute("a.jsonl:1", "b.jsonl:2", &recA, &recB, mA, mB)
	var buf bytes.Buffer
	Render(&buf, rep)
	return buf.String()
}

func TestCompute_Extension(t *testing.T) {
	recA := makeTestRecord("coding", "openai-completions", "you are helpful", []map[string]string{
		{"role": "user", "content": "hello"},
		{"role": "assistant", "content": "hi there"},
	}, []string{"read"}, 100, 20, 0)

	recB := makeTestRecord("coding", "openai-completions", "you are helpful", []map[string]string{
		{"role": "user", "content": "hello"},
		{"role": "assistant", "content": "hi there"},
		{"role": "user", "content": "what is 2+2?"},
		{"role": "assistant", "content": "4"},
	}, []string{"read"}, 150, 25, 0)

	out := computeAndRender(t, recA, recB)

	if !strings.Contains(out, "Messages (A=2, B=4, LCP=2)") {
		t.Errorf("expected LCP=2 and counts (2, 4), got:\n%s", out)
	}
	if !strings.Contains(out, "[2] first divergence: B extends A with 2 new message(s) (first role=user)") {
		t.Errorf("expected extension divergence line, got:\n%s", out)
	}
	if !strings.Contains(out, "A tail: 0") || !strings.Contains(out, "B tail: 2 new message(s)") {
		t.Errorf("expected tail lines, got:\n%s", out)
	}
	if !strings.Contains(out, "Verdict: B is an extension of A") {
		t.Errorf("expected extension verdict, got:\n%s", out)
	}
	if !strings.Contains(out, "Structural fact, not a root cause.") {
		t.Errorf("expected disclaimer, got:\n%s", out)
	}
}

func TestCompute_TruncatedRetry(t *testing.T) {
	recA := makeTestRecord("coding", "anthropic-messages", "sys", []map[string]string{
		{"role": "user", "content": "step 1"},
		{"role": "assistant", "content": "doing step 1"},
		{"role": "user", "content": "do step 2"},
	}, []string{"tool1"}, 1000, 200, 500)

	recB := makeTestRecord("coding", "anthropic-messages", "sys", []map[string]string{
		{"role": "user", "content": "step 1"},
		{"role": "assistant", "content": "doing step 1 differently"},
	}, []string{"tool1"}, 800, 150, 400)

	out := computeAndRender(t, recA, recB)

	if !strings.Contains(out, "Messages (A=3, B=2, LCP=1)") {
		t.Errorf("expected LCP=1, got:\n%s", out)
	}
	if !strings.Contains(out, "[1] first divergence: A msg#1 (role=assistant) rewritten in B (hash differs)") {
		t.Errorf("expected rewritten divergence line, got:\n%s", out)
	}
	if !strings.Contains(out, "A tail: 2 new message(s)") {
		t.Errorf("expected A tail: 2 new message(s), got:\n%s", out)
	}
	if !strings.Contains(out, "Verdict: B looks like a context-truncated retry of A (tail replaced, same system).") {
		t.Errorf("expected truncated retry verdict, got:\n%s", out)
	}
}

func TestCompute_TruncatedPrefix(t *testing.T) {
	recA := makeTestRecord("coding", "anthropic-messages", "sys", []map[string]string{
		{"role": "user", "content": "step 1"},
		{"role": "assistant", "content": "step 1 done"},
		{"role": "user", "content": "step 2"},
	}, []string{}, 100, 50, 0)

	recB := makeTestRecord("coding", "anthropic-messages", "sys", []map[string]string{
		{"role": "user", "content": "step 1"},
	}, []string{}, 50, 10, 0)

	out := computeAndRender(t, recA, recB)

	if !strings.Contains(out, "Messages (A=3, B=1, LCP=1)") {
		t.Errorf("expected LCP=1, got:\n%s", out)
	}
	if !strings.Contains(out, "[1] first divergence: B truncated after 1 message(s)") {
		t.Errorf("expected truncated divergence line, got:\n%s", out)
	}
	if !strings.Contains(out, "A tail: 2 new message(s)") {
		t.Errorf("expected A tail, got:\n%s", out)
	}
	if !strings.Contains(out, "B tail: 0") {
		t.Errorf("expected B tail: 0, got:\n%s", out)
	}
	if !strings.Contains(out, "Verdict: B looks like a context-truncated prefix of A (2 message(s) truncated, same system).") {
		t.Errorf("expected truncated prefix verdict, got:\n%s", out)
	}
}

func TestCompute_UnrelatedContext(t *testing.T) {
	recA := makeTestRecord("coding", "openai-completions", "", []map[string]string{
		{"role": "user", "content": "write python script"},
	}, nil, 10, 10, 0)

	recB := makeTestRecord("coding", "openai-completions", "", []map[string]string{
		{"role": "user", "content": "explain quantum physics"},
	}, nil, 10, 10, 0)

	out := computeAndRender(t, recA, recB)

	if !strings.Contains(out, "Messages (A=1, B=1, LCP=0)") {
		t.Errorf("expected LCP=0, got:\n%s", out)
	}
	if !strings.Contains(out, "Verdict: B and A share no common message context (LCP=0, unrelated context).") {
		t.Errorf("expected unrelated context verdict, got:\n%s", out)
	}
}

func TestCompute_SystemChanged(t *testing.T) {
	recA := makeTestRecord("coding", "openai-completions", "System instructions v1", []map[string]string{
		{"role": "user", "content": "hello"},
	}, nil, 10, 10, 0)

	recB := makeTestRecord("coding", "openai-completions", "System instructions v2", []map[string]string{
		{"role": "user", "content": "hello"},
	}, nil, 10, 10, 0)

	out := computeAndRender(t, recA, recB)

	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "sys_hash") && strings.Contains(l, "(same)") {
			t.Errorf("sys_hash line should not contain (same): %s", l)
		}
	}
	if !strings.Contains(out, "Verdict: B uses a new system prompt relative to A (system hash differs).") {
		t.Errorf("expected new system prompt verdict, got:\n%s", out)
	}
}

func TestCompute_Identical(t *testing.T) {
	recA := makeTestRecord("coding", "openai-completions", "System instructions", []map[string]string{
		{"role": "user", "content": "hello"},
	}, []string{"grep"}, 100, 50, 40)

	recB := makeTestRecord("coding", "openai-completions", "System instructions", []map[string]string{
		{"role": "user", "content": "hello"},
	}, []string{"grep"}, 100, 50, 40)

	out := computeAndRender(t, recA, recB)

	if !strings.Contains(out, "first divergence: none (all 1 message(s) identical)") {
		t.Errorf("expected identical divergence line, got:\n%s", out)
	}
	if !strings.Contains(out, "Verdict: B has identical message context to A (all messages and system prompt match).") {
		t.Errorf("expected identical verdict, got:\n%s", out)
	}
}

func TestCompute_ToolsAndUsageFormatting(t *testing.T) {
	recA := makeTestRecord("coding", "anthropic-messages", "sys", []map[string]string{
		{"role": "user", "content": "test"},
	}, []string{"read", "write"}, 53182, 629, 44800)

	recB := makeTestRecord("coding", "openai-completions", "sys", []map[string]string{
		{"role": "user", "content": "test"},
	}, []string{"read", "write", "web_search", "memory_write"}, 12004, 991, 9900)

	out := computeAndRender(t, recA, recB)

	// Check Header alignment and usage
	if !strings.Contains(out, "model        coding               coding               (same)") {
		t.Errorf("expected model row with (same), got:\n%s", out)
	}
	if !strings.Contains(out, "usage        in 53,182 / out 629  in 12,004 / out 991  (cached 44,800 → 9,900)") {
		t.Errorf("expected usage row with cached annotation, got:\n%s", out)
	}
	// Check tools diff
	if !strings.Contains(out, "toolset      2 tools              4 tools              (+2: web_search, memory_write)") {
		t.Errorf("expected tools row with (+2: web_search, memory_write), got:\n%s", out)
	}
	// Check sys_hash (same)
	if !strings.Contains(out, "sys_hash") || !strings.Contains(out, "(same)") {
		t.Errorf("expected sys_hash to be same, got:\n%s", out)
	}
}

func TestCompute_ToolsRemovedAndReplaced(t *testing.T) {
	recA := makeTestRecord("coding", "openai-completions", "", []map[string]string{{"role": "user", "content": "hi"}}, []string{"toolA", "toolB"}, 10, 10, 0)
	recB := makeTestRecord("coding", "openai-completions", "", []map[string]string{{"role": "user", "content": "hi"}}, []string{"toolB", "toolC"}, 10, 10, 0)

	out := computeAndRender(t, recA, recB)

	if !strings.Contains(out, "(+1: toolC, -1: toolA)") {
		t.Errorf("expected (+1: toolC, -1: toolA), got:\n%s", out)
	}
}

// A toolset with identical names but a changed schema (description / parameters)
// is a real cache break the name-only diff misses — the manifest ToolsHash catches it.
func TestCompute_ToolSchemaChangedSameNames(t *testing.T) {
	mkRec := func(desc string) audit.Record {
		rec := makeTestRecord("coding", "openai-completions", "", []map[string]string{{"role": "user", "content": "hi"}}, nil, 10, 10, 0)
		body := rec.Client.Request.Body.(map[string]any)
		body["tools"] = []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "search",
				"description": desc,
			},
		}}
		return rec
	}

	out := computeAndRender(t, mkRec("search the web"), mkRec("search the web and local files"))

	if !strings.Contains(out, "(same names, tool schema changed)") {
		t.Errorf("expected tools row to flag a schema change, got:\n%s", out)
	}
}
