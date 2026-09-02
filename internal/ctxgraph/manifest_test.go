// Ver 2026-07-28 22:40, by Sonnet 5

package ctxgraph

import (
	"crypto/md5"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
)

func mkAuditRec(ts time.Time, body map[string]any) audit.Record {
	return audit.Record{
		TS: ts, Model: "agent", Protocol: "openai-completions", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: body},
		},
	}
}

func TestBuildManifest_NonChatBody(t *testing.T) {
	t.Parallel()
	rec := audit.Record{Client: audit.Exchange{Request: audit.Message{Body: "not a map"}}}
	if _, ok := BuildManifest(&rec, "f", 1); ok {
		t.Error("expected ok=false for non-map body")
	}
}

func TestBuildManifest_KeysExcludeLeadingSystem(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "sys prompt"},
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "assistant", "content": "hello"},
		},
	}
	rec := mkAuditRec(time.Now(), body)
	m, ok := BuildManifest(&rec, "f", 1)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !m.HasSys || m.LeadSys != 1 {
		t.Errorf("HasSys=%v LeadSys=%d, want true/1", m.HasSys, m.LeadSys)
	}
	if len(m.Keys) != 2 {
		t.Fatalf("Keys len=%d, want 2 (user+assistant, system excluded)", len(m.Keys))
	}
	if len(m.MsgIdx) != len(m.Keys) {
		t.Fatalf("MsgIdx len=%d, want %d", len(m.MsgIdx), len(m.Keys))
	}
	// MsgIdx must point at the position within chatmsg.Messages() output:
	// index 0 is the system message, so the first key's message is at idx 1.
	if m.MsgIdx[0] != 1 || m.MsgIdx[1] != 2 {
		t.Errorf("MsgIdx = %v, want [1 2]", m.MsgIdx)
	}
}

func TestBuildManifest_MultipleLeadingSystemMessagesFoldIntoOneHash(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "part one"},
			map[string]any{"role": "system", "content": "part two"},
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	rec := mkAuditRec(time.Now(), body)
	m, ok := BuildManifest(&rec, "f", 1)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if m.LeadSys != 2 {
		t.Errorf("LeadSys=%d, want 2 (both system messages folded)", m.LeadSys)
	}
	if len(m.Keys) != 1 {
		t.Errorf("Keys len=%d, want 1 (only the user message)", len(m.Keys))
	}
}

func TestBuildManifest_NoSystemMessage(t *testing.T) {
	t.Parallel()
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	rec := mkAuditRec(time.Now(), body)
	m, ok := BuildManifest(&rec, "f", 1)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if m.HasSys || m.LeadSys != 0 {
		t.Errorf("HasSys=%v LeadSys=%d, want false/0", m.HasSys, m.LeadSys)
	}
}

func TestBuildManifest_AnthropicTopLevelSystem(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"system":   "you are helpful",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	rec := mkAuditRec(time.Now(), body)
	m, ok := BuildManifest(&rec, "f", 1)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !m.HasSys || m.LeadSys != 1 {
		t.Errorf("HasSys=%v LeadSys=%d, want true/1 (anthropic top-level system)", m.HasSys, m.LeadSys)
	}
	if len(m.Keys) != 1 {
		t.Errorf("Keys len=%d, want 1", len(m.Keys))
	}
	// MsgIdx must account for the +1 offset chatmsg.MsgOffset introduces
	// when system is prepended as message #0.
	if m.MsgIdx[0] != 1 {
		t.Errorf("MsgIdx = %v, want [1]", m.MsgIdx)
	}
}

// TestBuildManifest_ResponsesTopLevelInstructions is
// TestBuildManifest_AnthropicTopLevelSystem's openai-responses counterpart:
// proves session/lineage grouping — the actual gap this test closes — works
// for Responses-shaped bodies (top-level "instructions" + "input" array
// instead of "system" + "messages"), not just the per-request detail view.
func TestBuildManifest_ResponsesTopLevelInstructions(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"instructions": "you are helpful",
		"input":        []any{map[string]any{"role": "user", "content": "hi"}},
	}
	rec := mkAuditRec(time.Now(), body)
	rec.Protocol = "openai-responses"
	m, ok := BuildManifest(&rec, "f", 1)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !m.HasSys || m.LeadSys != 1 {
		t.Errorf("HasSys=%v LeadSys=%d, want true/1 (openai-responses top-level instructions)", m.HasSys, m.LeadSys)
	}
	if len(m.Keys) != 1 {
		t.Errorf("Keys len=%d, want 1", len(m.Keys))
	}
	if m.MsgIdx[0] != 1 {
		t.Errorf("MsgIdx = %v, want [1]", m.MsgIdx)
	}
	if m.SessKey == "" {
		t.Error("SessKey should not be empty — this is the actual grouping signal vmr report/story rely on")
	}
}

// TestBuildManifest_ResponsesSameConversationSameSessKey proves two turns of
// the same Responses-protocol conversation (second turn resends the first
// turn's input plus a follow-up, the way agent clients always resend full
// history) land in the same SessKey bucket — the concrete, user-visible
// behavior "vmr report/vmr story can't group Responses traffic into
// sessions" was about.
func TestBuildManifest_ResponsesSameConversationSameSessKey(t *testing.T) {
	t.Parallel()
	turn1 := map[string]any{
		"instructions": "you are helpful",
		"input":        []any{map[string]any{"role": "user", "content": "hi"}},
	}
	turn2 := map[string]any{
		"instructions": "you are helpful",
		"input": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "assistant", "content": "hello, how can I help?"},
			map[string]any{"role": "user", "content": "follow up question"},
		},
	}
	r1 := mkAuditRec(time.Now(), turn1)
	r1.Protocol = "openai-responses"
	r2 := mkAuditRec(time.Now(), turn2)
	r2.Protocol = "openai-responses"
	m1, ok1 := BuildManifest(&r1, "f", 1)
	m2, ok2 := BuildManifest(&r2, "f", 2)
	if !ok1 || !ok2 {
		t.Fatalf("expected ok=true for both: ok1=%v ok2=%v", ok1, ok2)
	}
	if m1.SessKey != m2.SessKey {
		t.Errorf("SessKey mismatch across turns of the same conversation: %q vs %q", m1.SessKey, m2.SessKey)
	}
	if m1.Keys[0] != m2.Keys[0] {
		t.Error("turn 2's resent first message should hash identically to turn 1's")
	}
}

func TestBuildManifest_IdenticalContentSameHash(t *testing.T) {
	t.Parallel()
	body1 := map[string]any{"messages": []any{
		map[string]any{"role": "system", "content": "sys"},
		map[string]any{"role": "user", "content": "same text"},
	}}
	body2 := map[string]any{"messages": []any{
		map[string]any{"role": "system", "content": "sys"},
		map[string]any{"role": "user", "content": "same text"},
	}}
	r1 := mkAuditRec(time.Now(), body1)
	r2 := mkAuditRec(time.Now(), body2)
	m1, _ := BuildManifest(&r1, "f", 1)
	m2, _ := BuildManifest(&r2, "f", 2)
	if m1.Keys[0] != m2.Keys[0] {
		t.Error("identical message content should hash identically")
	}
	if m1.SysHash != m2.SysHash {
		t.Error("identical system prompt should hash identically")
	}
}

func TestBuildManifest_SessKey_MetadataUserID(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"metadata": map[string]any{"user_id": "app_abc_session_1234-5678"},
	}
	rec := mkAuditRec(time.Now(), body)
	m, _ := BuildManifest(&rec, "f", 1)
	if m.SessKey != "meta:session_1234-5678" {
		t.Errorf("SessKey = %q, want meta:session_1234-5678", m.SessKey)
	}
}

func TestBuildManifest_SessKey_AnchorFallback(t *testing.T) {
	t.Parallel()
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	rec := mkAuditRec(time.Now(), body)
	m, _ := BuildManifest(&rec, "f", 1)
	want := "anchor:" + m.Keys[0].String()
	if m.SessKey != want {
		t.Errorf("SessKey = %q, want %q", m.SessKey, want)
	}
}

func TestLeadingSystemText_SingleMessage(t *testing.T) {
	t.Parallel()
	msgs := []chatmsg.Message{{Role: "system", Text: "you are helpful"}, {Role: "user", Text: "hi"}}
	if got := LeadingSystemText(msgs, 1); got != "you are helpful" {
		t.Errorf("LeadingSystemText = %q, want %q", got, "you are helpful")
	}
}

func TestLeadingSystemText_ConcatenatesMultipleLeadingMessages(t *testing.T) {
	t.Parallel()
	msgs := []chatmsg.Message{
		{Role: "system", Text: "part one"},
		{Role: "system", Text: "part two"},
		{Role: "user", Text: "hi"},
	}
	if got := LeadingSystemText(msgs, 2); got != "part onepart two" {
		t.Errorf("LeadingSystemText = %q, want %q", got, "part onepart two")
	}
}

func TestLeadingSystemText_ZeroLeadSysIsEmpty(t *testing.T) {
	t.Parallel()
	msgs := []chatmsg.Message{{Role: "user", Text: "hi"}}
	if got := LeadingSystemText(msgs, 0); got != "" {
		t.Errorf("LeadingSystemText = %q, want empty", got)
	}
}

// TestLeadingSystemText_MatchesBuildManifestSysHash locks in the property
// EnsureSysPromptEvidence (internal/reqdetail) depends on: the text this
// function returns for a Manifest's own LeadSys must hash to that exact
// Manifest's SysHash, or the evidence blob written under that hash's
// filename would not actually be the text the hash names.
func TestLeadingSystemText_MatchesBuildManifestSysHash(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "part one"},
			map[string]any{"role": "system", "content": "part two"},
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	rec := mkAuditRec(time.Now(), body)
	m, ok := BuildManifest(&rec, "f", 1)
	if !ok {
		t.Fatal("expected ok=true")
	}
	msgs := chatmsg.Messages(body)
	text := LeadingSystemText(msgs, m.LeadSys)
	if got := md5.Sum([]byte(text)); got != m.SysHash {
		t.Errorf("md5(LeadingSystemText(...)) = %x, want SysHash %x", got, m.SysHash)
	}
}

func TestBuildManifest_SessKey_EmptyWhenNoMessagesAndNoMetadata(t *testing.T) {
	t.Parallel()
	body := map[string]any{"messages": []any{}}
	rec := mkAuditRec(time.Now(), body)
	m, _ := BuildManifest(&rec, "f", 1)
	if m.SessKey != "" {
		t.Errorf("SessKey = %q, want empty (no anchor, no metadata)", m.SessKey)
	}
}

// TestBuildManifest_ServedEndpoint pins the cost-attribution field's rule —
// the same one internal/report's endpointInfo applies (the duplication is
// pinned from the other side by cmd/vmr's cost_basis_parity_test): prefer
// the strictly successful attempt, fall back to the last attempt that got
// a < 400 response header at all, empty when none did. Every fixture here
// mirrors a real router shape: an early client cancel has attempts but no
// responses; a soft-block failover leaves a 2xx attempt with an error set;
// a 4xx-then-2xx failover serves from the second endpoint.
func TestBuildManifest_ServedEndpoint(t *testing.T) {
	t.Parallel()
	const (
		epA = "openai-completions:acme:a"
		epB = "openai-completions:acme:b"
	)
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}}

	t.Run("no attempts at all", func(t *testing.T) {
		rec := mkAuditRec(time.Now(), body)
		m, _ := BuildManifest(&rec, "f", 1)
		if m.Endpoint != "-" || m.ServedEndpoint != "" {
			t.Errorf("Endpoint=%q ServedEndpoint=%q, want \"-\"/\"\"", m.Endpoint, m.ServedEndpoint)
		}
	})
	t.Run("early cancel: attempts but no response ever committed", func(t *testing.T) {
		rec := mkAuditRec(time.Now(), body)
		rec.Outcome = "canceled"
		rec.Attempts = []audit.Attempt{
			{Endpoint: epA, Protocol: "openai-completions", Provider: "acme", Model: "a"},
		}
		m, _ := BuildManifest(&rec, "f", 1)
		if m.Endpoint != epA {
			t.Errorf("Endpoint = %q, want the attempted %q", m.Endpoint, epA)
		}
		if m.ServedEndpoint != "" {
			t.Errorf("ServedEndpoint = %q, want empty — nothing served, nothing to price", m.ServedEndpoint)
		}
	})
	t.Run("mid-stream cancel: 2xx committed then client went away", func(t *testing.T) {
		rec := mkAuditRec(time.Now(), body)
		rec.Outcome = "canceled"
		rec.Attempts = []audit.Attempt{
			{Endpoint: epA, Protocol: "openai-completions", Provider: "acme", Model: "a",
				Response: &audit.Message{Status: 200}, Error: "canceled"},
		}
		m, _ := BuildManifest(&rec, "f", 1)
		if m.ServedEndpoint != epA {
			t.Errorf("ServedEndpoint = %q, want %q — the committed 2xx served real bytes", m.ServedEndpoint, epA)
		}
	})
	t.Run("4xx then 2xx failover serves from the second endpoint", func(t *testing.T) {
		rec := mkAuditRec(time.Now(), body)
		rec.Attempts = []audit.Attempt{
			{Endpoint: epA, Protocol: "openai-completions", Provider: "acme", Model: "a",
				Response: &audit.Message{Status: 429}, Error: "rate_limit"},
			{Endpoint: epB, Protocol: "openai-completions", Provider: "acme", Model: "b",
				Response: &audit.Message{Status: 200}},
		}
		m, _ := BuildManifest(&rec, "f", 1)
		if m.Endpoint != epB || m.ServedEndpoint != epB {
			t.Errorf("Endpoint=%q ServedEndpoint=%q, want both %q", m.Endpoint, m.ServedEndpoint, epB)
		}
	})
	t.Run("soft-block 2xx then 5xx: served is the 2xx attempt though outcome is error", func(t *testing.T) {
		rec := mkAuditRec(time.Now(), body)
		rec.Outcome = "error"
		rec.Attempts = []audit.Attempt{
			{Endpoint: epA, Protocol: "openai-completions", Provider: "acme", Model: "a",
				Response: &audit.Message{Status: 200}, Error: "content:soft_block"},
			{Endpoint: epB, Protocol: "openai-completions", Provider: "acme", Model: "b",
				Response: &audit.Message{Status: 500}, Error: "server:5xx"},
		}
		m, _ := BuildManifest(&rec, "f", 1)
		if m.ServedEndpoint != epA {
			t.Errorf("ServedEndpoint = %q, want %q (the only < 400 response)", m.ServedEndpoint, epA)
		}
	})
	t.Run("strictly successful attempt preferred over a later error-bearing 2xx", func(t *testing.T) {
		// Two < 400 attempts, the later one carrying an error string: the
		// clean one wins, exactly like endpointInfo's successEp preference.
		rec := mkAuditRec(time.Now(), body)
		rec.Attempts = []audit.Attempt{
			{Endpoint: epA, Protocol: "openai-completions", Provider: "acme", Model: "a",
				Response: &audit.Message{Status: 200}},
			{Endpoint: epB, Protocol: "openai-completions", Provider: "acme", Model: "b",
				Response: &audit.Message{Status: 200}, Error: "truncated"},
		}
		m, _ := BuildManifest(&rec, "f", 1)
		if m.ServedEndpoint != epA {
			t.Errorf("ServedEndpoint = %q, want %q (the strictly successful attempt)", m.ServedEndpoint, epA)
		}
	})
}

func TestBuildManifest_HashCache(t *testing.T) {
	t.Parallel()
	largeContent := strings.Repeat("data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", 100)
	body1 := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": largeContent},
			map[string]any{"role": "assistant", "content": "turn 1 reply"},
		},
	}
	body2 := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": largeContent},
			map[string]any{"role": "assistant", "content": "turn 1 reply"},
			map[string]any{"role": "user", "content": "turn 2 question"},
		},
	}
	rec1 := mkAuditRec(time.Now(), body1)
	rec2 := mkAuditRec(time.Now(), body2)

	m1, ok1 := BuildManifest(&rec1, "f", 1)
	m2, ok2 := BuildManifest(&rec2, "f", 2)
	if !ok1 || !ok2 {
		t.Fatalf("BuildManifest failed: ok1=%v ok2=%v", ok1, ok2)
	}

	if len(m1.Keys) != 2 || len(m2.Keys) != 3 {
		t.Fatalf("unexpected keys length: m1=%d m2=%d", len(m1.Keys), len(m2.Keys))
	}
	if m1.Keys[0] != m2.Keys[0] {
		t.Errorf("cached large message hash mismatch: %v vs %v", m1.Keys[0], m2.Keys[0])
	}
	if m1.Keys[1] != m2.Keys[1] {
		t.Errorf("cached assistant message hash mismatch: %v vs %v", m1.Keys[1], m2.Keys[1])
	}
}

func TestBuildManifest_UsageProtocolAware(t *testing.T) {
	t.Parallel()
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	usageMap := map[string]any{
		"usage": map[string]any{
			"input_tokens":            float64(100),
			"cache_read_input_tokens": float64(50),
		},
	}
	recAnthropic := audit.Record{
		Protocol: "anthropic-messages",
		Client: audit.Exchange{
			Request:  audit.Message{Body: body},
			Response: &audit.Message{Body: usageMap},
		},
	}
	mAnthropic, ok := BuildManifest(&recAnthropic, "f", 1)
	// The fixture reports no output_tokens at all, so the Out side is
	// honestly unknown (UsageOutOK false) even though In is real.
	if !ok || !mAnthropic.UsageInOK || mAnthropic.UsageOutOK || mAnthropic.Usage.In != 150 {
		t.Errorf("anthropic manifest usage In=%d (want 150, 100+50), inOK/outOK=%v/%v (want true/false — no output reported)", mAnthropic.Usage.In, mAnthropic.UsageInOK, mAnthropic.UsageOutOK)
	}

	recResponses := audit.Record{
		Protocol: "openai-responses",
		Client: audit.Exchange{
			Request:  audit.Message{Body: body},
			Response: &audit.Message{Body: usageMap},
		},
	}
	mResponses, ok := BuildManifest(&recResponses, "f", 2)
	if !ok || !mResponses.UsageInOK || mResponses.UsageOutOK || mResponses.Usage.In != 100 {
		t.Errorf("responses manifest usage In=%d (want 100, inclusive), inOK/outOK=%v/%v (want true/false — no output reported)", mResponses.Usage.In, mResponses.UsageInOK, mResponses.UsageOutOK)
	}
}
