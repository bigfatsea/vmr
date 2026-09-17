// Ver 2026-09-15 23:45, by coding

package guard

import (
	"encoding/json"
	"testing"
)

func newTestGuard(t *testing.T) *Guard {
	t.Helper()
	e := testEngine(t)
	return NewGuard(e)
}

func testEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

func TestOutbound_OffSkipsScanning(t *testing.T) {
	g := newTestGuard(t)
	doc, _ := json.Marshal(map[string]string{"text": "AKIA" + repUpperDigit(16)})
	result := g.Outbound(doc, OutOff)
	if len(result.Hits) != 0 {
		t.Errorf("OutOff found hits %+v, want none", result.Hits)
	}
	if string(result.Body) != string(doc) {
		t.Error("OutOff must not modify Body")
	}
}

// TestOutbound_NeverRewrites pins Outbound's post-replace contract: the
// former mode: replace was removed, so no OutMode may ever hand back bytes
// other than the caller's own body.
func TestOutbound_NeverRewrites(t *testing.T) {
	g := newTestGuard(t)
	doc, _ := json.Marshal(map[string]string{"text": "sure, AKIA" + repUpperDigit(16) + " here"})
	for _, mode := range []OutMode{OutOff, OutAuditOnly, OutBlock} {
		result := g.Outbound(doc, mode)
		if string(result.Body) != string(doc) {
			t.Errorf("mode %q must not modify Body", mode)
		}
	}
}

func TestOutbound_AuditOnlyRecordsHits(t *testing.T) {
	g := newTestGuard(t)
	doc, _ := json.Marshal(map[string]string{"text": "sure, AKIA" + repUpperDigit(16) + " here"})
	result := g.Outbound(doc, OutAuditOnly)
	if len(result.Hits) != 1 || result.Hits[0].Rule != "aws-access-key" {
		t.Fatalf("Hits = %+v, want one aws-access-key hit", result.Hits)
	}
	if result.Hits[0].Count != 1 {
		t.Errorf("Count = %d, want 1", result.Hits[0].Count)
	}
	if result.Hits[0].FP == "" {
		t.Error("Hit.FP must not be empty")
	}
}

func TestOutbound_NoHitOnCleanBody(t *testing.T) {
	g := newTestGuard(t)
	doc, _ := json.Marshal(map[string]string{"text": "nothing interesting here"})
	result := g.Outbound(doc, OutAuditOnly)
	if len(result.Hits) != 0 {
		t.Errorf("Hits = %+v, want none", result.Hits)
	}
	if result.BlockedBy != "" {
		t.Errorf("BlockedBy = %q, want empty", result.BlockedBy)
	}
}

func TestOutbound_CountsAmplification(t *testing.T) {
	g := newTestGuard(t)
	cred := "AKIA" + repUpperDigit(16)
	doc, _ := json.Marshal(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": cred},
			map[string]any{"role": "assistant", "content": "ok"},
			map[string]any{"role": "user", "content": cred + " again"},
		},
	})
	result := g.Outbound(doc, OutAuditOnly)
	if len(result.Hits) != 1 {
		t.Fatalf("Hits = %+v, want exactly one rule (repeated)", result.Hits)
	}
	if result.Hits[0].Count != 2 {
		t.Errorf("Count = %d, want 2 (same credential twice in one body)", result.Hits[0].Count)
	}
}

func TestOutbound_MultipleDistinctCredentialsSameRule(t *testing.T) {
	g := newTestGuard(t)
	cred1 := "AKIA" + prngString(10, upperDigitAlphabet, 16)
	cred2 := "AKIA" + prngString(20, upperDigitAlphabet, 16)
	doc, _ := json.Marshal(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": cred1},
			map[string]any{"role": "assistant", "content": cred2},
		},
	})
	result := g.Outbound(doc, OutAuditOnly)
	if len(result.Hits) != 2 {
		t.Fatalf("Hits = %+v, want 2 hits for two distinct credentials under same rule", result.Hits)
	}
	if result.Hits[0].FP == result.Hits[1].FP {
		t.Errorf("distinct credentials should have distinct FPs, got %q and %q", result.Hits[0].FP, result.Hits[1].FP)
	}
}

func TestOutbound_BlockedByReportsFirstTier1Rule(t *testing.T) {
	g := newTestGuard(t)
	doc, _ := json.Marshal(map[string]string{"text": "AKIA" + repUpperDigit(16)})
	result := g.Outbound(doc, OutBlock)
	if result.BlockedBy != "aws-access-key" {
		t.Errorf("BlockedBy = %q, want aws-access-key", result.BlockedBy)
	}
}

func TestOutbound_Tier2NeverSetsBlockedBy(t *testing.T) {
	g := newTestGuard(t)
	doc, _ := json.Marshal(map[string]string{"text": "sk-" + repAlnum(24)}) // generic-sk-prefix, Tier2
	result := g.Outbound(doc, OutBlock)
	if len(result.Hits) != 1 || result.Hits[0].Tier != Tier2 {
		t.Fatalf("Hits = %+v, want one Tier2 hit", result.Hits)
	}
	if result.BlockedBy != "" {
		t.Errorf("BlockedBy = %q, want empty for a Tier2-only match (K-G5: never a basis for blocking)", result.BlockedBy)
	}
}

func TestOutbound_EmptyBodyNoOp(t *testing.T) {
	g := newTestGuard(t)
	result := g.Outbound(nil, OutAuditOnly)
	if len(result.Hits) != 0 {
		t.Errorf("Hits = %+v, want none for an empty body", result.Hits)
	}
}
