// Ver 2026-07-28 22:40, by Sonnet 5

package ctxgraph

import (
	"testing"
	"time"

	"vmr/internal/audit"
)

func mkAuditRec(ts time.Time, body map[string]any) audit.Record {
	return audit.Record{
		TS: ts, Model: "agent", Protocol: "openai", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Method: "POST", Path: "/v1/chat/completions", Body: body},
		},
	}
}

func TestBuildManifest_NonChatBody(t *testing.T) {
	rec := audit.Record{Client: audit.Exchange{Request: audit.Message{Body: "not a map"}}}
	if _, ok := BuildManifest(&rec, "f", 1); ok {
		t.Error("expected ok=false for non-map body")
	}
}

func TestBuildManifest_KeysExcludeLeadingSystem(t *testing.T) {
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

func TestBuildManifest_IdenticalContentSameHash(t *testing.T) {
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
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	rec := mkAuditRec(time.Now(), body)
	m, _ := BuildManifest(&rec, "f", 1)
	want := "anchor:" + m.Keys[0].String()
	if m.SessKey != want {
		t.Errorf("SessKey = %q, want %q", m.SessKey, want)
	}
}

func TestBuildManifest_SessKey_EmptyWhenNoMessagesAndNoMetadata(t *testing.T) {
	body := map[string]any{"messages": []any{}}
	rec := mkAuditRec(time.Now(), body)
	m, _ := BuildManifest(&rec, "f", 1)
	if m.SessKey != "" {
		t.Errorf("SessKey = %q, want empty (no anchor, no metadata)", m.SessKey)
	}
}
