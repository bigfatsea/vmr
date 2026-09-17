// Ver 2026-09-16, by Sonnet 5

package report

import (
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/taskseg"
)

// TestGuardE2E_BuildPopulatesReportGuard exercises the full pipeline a
// real M3/M4 producer would drive: JSONL on disk -> Build's fresh-decode
// path (extractRecordFacts -> buildRec2 -> ingestRecord -> guardCollector)
// -> Report2.Guard. Proves the wiring end-to-end, not just guardcol.go in
// isolation (guardcol_test.go) or the ViewModel in isolation
// (viewmodel_guard_test.go).
func TestGuardE2E_BuildPopulatesReportGuard(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	at := func(sec int) time.Time { return time.Date(2026, 9, 13, 10, 0, sec, 0, zone) }

	recs := []audit.Record{
		{
			TS: at(0), Model: "coding", Protocol: "anthropic-messages", Outcome: "ok",
			Client: audit.Exchange{Request: audit.Message{Body: map[string]any{"model": "coding"}}},
			Guard: &audit.GuardRecord{
				Ver: 1,
				Hits: []audit.Hit{
					{Rule: "gcp-api-key", Tier: 1, Count: 2, FP: "fp-1"},
				},
			},
		},
		{
			TS: at(1), Model: "coding", Protocol: "anthropic-messages", Outcome: "ok",
			Client: audit.Exchange{Request: audit.Message{Body: map[string]any{"model": "coding"}}},
			Guard:  &audit.GuardRecord{Ver: 1}, // stamped, no hits
		},
		{
			TS: at(2), Model: "coding", Protocol: "anthropic-messages", Outcome: "ok",
			Client: audit.Exchange{Request: audit.Message{Body: map[string]any{"model": "coding"}}},
			// Guard nil: the Fallback Path (ADR-12) still covers this
			// record -- extractRecordFacts always attempts
			// scanRecordForGuard when Guard is nil -- it just finds
			// nothing in this trivial body, so it counts toward
			// RecordsScanned/RecordsFallbackScanned but not RecordsWithHits.
		},
	}
	path := writeJSONL(t, recs)

	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if rep.Guard == nil {
		t.Fatal("rep.Guard is nil after Build with 3 records")
	}
	if rep.Guard.RecordsScanned != 3 {
		t.Errorf("RecordsScanned = %d, want 3 (2 stamped + 1 fallback-scanned)", rep.Guard.RecordsScanned)
	}
	if rep.Guard.RecordsStamped != 2 || rep.Guard.RecordsFallbackScanned != 1 {
		t.Errorf("RecordsStamped/RecordsFallbackScanned = %d/%d, want 2/1",
			rep.Guard.RecordsStamped, rep.Guard.RecordsFallbackScanned)
	}
	if rep.Guard.RecordsWithHits != 1 {
		t.Errorf("RecordsWithHits = %d, want 1", rep.Guard.RecordsWithHits)
	}
	if len(rep.Guard.Rules) != 1 || rep.Guard.Rules[0].Rule != "gcp-api-key" || rep.Guard.Rules[0].TotalHits != 2 {
		t.Errorf("Rules = %+v, unexpected", rep.Guard.Rules)
	}
}

// TestGuardE2E_FallbackScanCoversCleanRecord is the same pipeline with zero
// Guard-bearing records (every real report today) — this is exactly the
// scenario M2.2 exists for: rep.Guard is now populated by the Fallback
// Path even though nothing ever stamped Record.Guard, reporting full
// coverage (RecordsFallbackScanned == every ingested record) with zero
// hits, not the pre-M2.2 "rep.Guard stays nil" behavior.
func TestGuardE2E_FallbackScanCoversCleanRecord(t *testing.T) {
	recs := []audit.Record{
		{TS: time.Now(), Model: "coding", Protocol: "anthropic-messages", Outcome: "ok",
			Client: audit.Exchange{Request: audit.Message{Body: map[string]any{"model": "coding"}}}},
	}
	path := writeJSONL(t, recs)
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if rep.Guard == nil {
		t.Fatal("rep.Guard is nil, want the Fallback Path to cover this record")
	}
	if rep.Guard.RecordsScanned != 1 || rep.Guard.RecordsFallbackScanned != 1 || rep.Guard.RecordsStamped != 0 {
		t.Errorf("rep.Guard = %+v, want RecordsScanned=1/RecordsFallbackScanned=1/RecordsStamped=0", rep.Guard)
	}
	if rep.Guard.RecordsWithHits != 0 || len(rep.Guard.Rules) != 0 || rep.Guard.Inbound != nil {
		t.Errorf("rep.Guard = %+v, want no hits and no inbound findings on a body with no assistant response", rep.Guard)
	}
}

// TestGuardE2E_NilOnEmptyLog confirms rep.Guard stays nil (not an empty
// struct) when there is truly nothing to ingest — ADR-12's backward-
// compatibility contract still holds for the one case where the Fallback
// Path never runs at all.
func TestGuardE2E_NilOnEmptyLog(t *testing.T) {
	path := writeJSONL(t, nil)
	rep, _, err := Build([]string{path}, time.Now(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if rep.Guard != nil {
		t.Errorf("rep.Guard = %+v, want nil on an empty log", rep.Guard)
	}
}

// TestGuardE2E_CacheHitPathCarriesGuard is the reason CacheSchemaVersion
// was bumped (ctxgraph/cache.go's v11 note): a second BuildCached run over
// the same file must take the cache-hit path (ingestCachedFile, not a
// fresh decode) and still see Guard data — proving factscache.go's
// recordFacts.Guard round-trips through the on-disk/in-memory cache
// correctly, not just through a fresh decode.
func TestGuardE2E_CacheHitPathCarriesGuard(t *testing.T) {
	recs := []audit.Record{
		{TS: time.Now(), Model: "coding", Protocol: "anthropic-messages", Outcome: "ok",
			Client: audit.Exchange{Request: audit.Message{Body: map[string]any{"model": "coding"}}},
			Guard:  &audit.GuardRecord{Ver: 1, Hits: []audit.Hit{{Rule: "gcp-api-key", Tier: 1, Count: 1, FP: "fp-1"}}},
		},
	}
	path := writeJSONL(t, recs)
	now := time.Now()

	_, _, cache1, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil)
	if err != nil {
		t.Fatalf("first BuildCached (cold): %v", err)
	}

	// Second run reuses cache1 for the same, unmodified file — scanFiles'
	// cache-hit branch (onRecord == nil && ok) takes over and never
	// reopens/re-decodes it.
	rep2, _, _, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, cache1, nil, nil)
	if err != nil {
		t.Fatalf("second BuildCached (warm): %v", err)
	}
	if rep2.Guard == nil {
		t.Fatal("rep2.Guard is nil on the cache-hit path — Guard did not survive the fact cache round trip")
	}
	if rep2.Guard.RecordsScanned != 1 || len(rep2.Guard.Rules) != 1 || rep2.Guard.Rules[0].Rule != "gcp-api-key" {
		t.Errorf("rep2.Guard = %+v, unexpected on cache-hit path", rep2.Guard)
	}
}

// TestGuardE2E_InboundOnlyStampTriggersOutboundFallback verifies that an
// inbound-only stamped record (e.g. outbound mode: off, completion hook
// stamped SanitizedRunes, so OutMode is empty and Hits is empty) does NOT
// suppress the fallback scan on its request body. Both cold and warm
// cache paths must report the outbound finding.
func TestGuardE2E_InboundOnlyStampTriggersOutboundFallback(t *testing.T) {
	recs := []audit.Record{
		{
			TS: time.Now(), Model: "coding", Protocol: "anthropic-messages", Outcome: "ok",
			Client: audit.Exchange{
				Request: audit.Message{
					Body: map[string]any{
						"model": "coding",
						"messages": []any{
							map[string]any{
								"role":    "user",
								"content": "key AIzaSyD9x8w7v6u5t4s3r2q1p0o9n8m7l6k5j4i",
							},
						},
					},
				},
			},
			// Inbound-only stamp: OutMode empty, Ver 0, Hits nil.
			Guard: &audit.GuardRecord{
				SanitizedRunes: map[string]int{"C": 2},
			},
		},
	}
	path := writeJSONL(t, recs)
	now := time.Now()

	rep, _, cache1, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached cold: %v", err)
	}
	if rep.Guard == nil {
		t.Fatal("rep.Guard is nil, want outbound fallback to run")
	}
	if rep.Guard.RecordsScanned != 1 || rep.Guard.RecordsFallbackScanned != 1 || rep.Guard.RecordsStamped != 0 {
		t.Errorf("scanned/fallback/stamped = %d/%d/%d, want 1/1/0",
			rep.Guard.RecordsScanned, rep.Guard.RecordsFallbackScanned, rep.Guard.RecordsStamped)
	}
	if rep.Guard.RecordsWithHits != 1 {
		t.Errorf("RecordsWithHits = %d, want 1", rep.Guard.RecordsWithHits)
	}
	if len(rep.Guard.Rules) != 1 || rep.Guard.Rules[0].Rule != "gcp-api-key" {
		t.Errorf("Rules = %+v, want gcp-api-key hit", rep.Guard.Rules)
	}

	// Warm cache run must produce the same result via factscache.
	rep2, _, _, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, cache1, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached warm: %v", err)
	}
	if rep2.Guard == nil {
		t.Fatal("rep2.Guard is nil on warm cache")
	}
	if rep2.Guard.RecordsWithHits != 1 || len(rep2.Guard.Rules) != 1 || rep2.Guard.Rules[0].Rule != "gcp-api-key" {
		t.Errorf("rep2.Guard on warm cache = %+v, unexpected", rep2.Guard)
	}
}

// TestGuardE2E_ScanErrorStampTriggersFallback verifies that a record whose
// online outbound scan crashed (server/guard.go's applyOutboundGuard
// panic-recover stamp: OutMode "error", Ver set, Hits nil) is NOT treated
// as an authoritative "scanned clean" verdict — the offline fallback must
// still run and can still find a real credential the crashed online scan
// never got to report. Before the fix, guardOutboundStamped counted Ver!=0
// alone as "stamped" and skipped the fallback entirely, silently losing
// this credential from both online and offline detection.
func TestGuardE2E_ScanErrorStampTriggersFallback(t *testing.T) {
	recs := []audit.Record{
		{
			TS: time.Now(), Model: "coding", Protocol: "anthropic-messages", Outcome: "ok",
			Client: audit.Exchange{
				Request: audit.Message{
					Body: map[string]any{
						"model": "coding",
						"messages": []any{
							map[string]any{
								"role":    "user",
								"content": "key AIzaSyD9x8w7v6u5t4s3r2q1p0o9n8m7l6k5j4i",
							},
						},
					},
				},
			},
			// The panic-recover shape: OutMode "error", Ver set, no Hits —
			// the online scan crashed before it could produce a verdict.
			Guard: &audit.GuardRecord{OutMode: "error", Ver: 1},
		},
	}
	path := writeJSONL(t, recs)
	now := time.Now()

	rep, _, cache1, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached cold: %v", err)
	}
	if rep.Guard == nil {
		t.Fatal("rep.Guard is nil, want the fallback scan to have run on the errored record")
	}
	if rep.Guard.RecordsStamped != 0 || rep.Guard.RecordsFallbackScanned != 1 {
		t.Errorf("stamped/fallback = %d/%d, want 0/1 (an error stamp must route to the fallback path)",
			rep.Guard.RecordsStamped, rep.Guard.RecordsFallbackScanned)
	}
	if rep.Guard.RecordsWithHits != 1 {
		t.Errorf("RecordsWithHits = %d, want 1 (the fallback scan must recover the credential the crashed online scan missed)", rep.Guard.RecordsWithHits)
	}
	if len(rep.Guard.Rules) != 1 || rep.Guard.Rules[0].Rule != "gcp-api-key" {
		t.Errorf("Rules = %+v, want a gcp-api-key hit", rep.Guard.Rules)
	}

	// Warm cache run must produce the same result.
	rep2, _, _, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, cache1, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached warm: %v", err)
	}
	if rep2.Guard == nil || rep2.Guard.RecordsWithHits != 1 {
		t.Errorf("rep2.Guard = %+v, want the same fallback hit on warm cache", rep2.Guard)
	}
}

// TestGuardScanSafe_RecoversPanic confirms a panicking fallback scan (e.g.
// a bug in the shared regex/entropy layer internal/guard.Scan and ScanText
// both call into) is contained to that one record — never crashes the
// batch, never leaves rf.GuardScan in a half-computed state.
func TestGuardScanSafe_RecoversPanic(t *testing.T) {
	got, failed := guardScanSafe(func() *GuardScanFacts {
		panic("simulated fallback-scan crash")
	})
	if got != nil {
		t.Errorf("guardScanSafe() = %+v, want nil after a recovered panic", got)
	}
	if !failed {
		t.Error("guardScanSafe() failed = false, want true after panic")
	}

	// The non-panicking path must be a pure passthrough.
	want := &GuardScanFacts{ToolCallsInspected: 3}
	got, failed = guardScanSafe(func() *GuardScanFacts { return want })
	if got != want {
		t.Errorf("guardScanSafe() = %+v, want %+v unchanged on the non-panicking path", got, want)
	}
	if failed {
		t.Error("guardScanSafe() failed = true, want false on clean run")
	}
}

// TestGuardE2E_StampedRecordExtractsInboundForensics verifies that an
// online-stamped record (OutMode "audit_only") runs the inbound forensics
// pass (scanInboundFacts) over its response body, populating Inbound findings
// (tool calls, runes, echoes) without losing them (ISSUE-01).
func TestGuardE2E_StampedRecordExtractsInboundForensics(t *testing.T) {
	recs := []audit.Record{
		{
			TS: time.Now(), Model: "coding", Protocol: "anthropic-messages", Outcome: "ok",
			Client: audit.Exchange{
				Request: audit.Message{
					Body: map[string]any{
						"model": "coding",
						"messages": []any{
							map[string]any{
								"role":    "user",
								"content": "key AIzaSyD9x8w7v6u5t4s3r2q1p0o9n8m7l6k5j4i",
							},
						},
					},
				},
				Response: &audit.Message{
					Body: "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\"}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"tu_1\",\"name\":\"bash\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"command\\\":\\\"rm -rf /\\\"}\"}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\ndata: {\"type\":\"message_stop\"}\n\n",
				},
			},
			Guard: &audit.GuardRecord{
				OutMode: "audit_only",
				Ver:     1,
				Hits: []audit.Hit{
					{Rule: "gcp-api-key", Tier: 1, Count: 1, FP: "fp-1"},
				},
			},
		},
	}
	path := writeJSONL(t, recs)
	now := time.Now()

	rep, _, cache1, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached cold: %v", err)
	}
	if rep.Guard == nil {
		t.Fatal("rep.Guard is nil")
	}
	if rep.Guard.RecordsStamped != 1 {
		t.Errorf("RecordsStamped = %d, want 1", rep.Guard.RecordsStamped)
	}
	if rep.Guard.Inbound == nil {
		t.Fatal("rep.Guard.Inbound is nil for stamped record with tool call in response")
	}
	if rep.Guard.Inbound.ToolCallsInspected != 1 {
		t.Errorf("ToolCallsInspected = %d, want 1", rep.Guard.Inbound.ToolCallsInspected)
	}
	if len(rep.Guard.Inbound.ToolFindings) == 0 {
		t.Fatal("ToolFindings is empty, want rm -rf / finding")
	}
	if rep.Guard.Inbound.ToolFindings[0].Category != "destructive_root_deletion" {
		t.Errorf("ToolFindings[0].Category = %q, want destructive_root_deletion", rep.Guard.Inbound.ToolFindings[0].Category)
	}

	// Warm cache run must produce identical inbound facts.
	rep2, _, _, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, cache1, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached warm: %v", err)
	}
	if rep2.Guard == nil || rep2.Guard.Inbound == nil {
		t.Fatal("rep2.Guard or Inbound is nil on warm cache")
	}
	if rep2.Guard.Inbound.ToolCallsInspected != 1 || len(rep2.Guard.Inbound.ToolFindings) == 0 {
		t.Errorf("rep2.Guard.Inbound = %+v, want tool finding on warm cache", rep2.Guard.Inbound)
	}
}

// TestScanRecordForGuard_TextEchoCountNotAmplifiedByResendCount covers the
// independent review's finding: a coding-agent session resends its whole
// growing history every turn, so the same credential can appear many
// times in one request body. countEchoes must still count "how many times
// the response echoed the credential" (1, here), not multiply that by how
// many times the credential happens to already appear in the request.
func TestScanRecordForGuard_TextEchoCountNotAmplifiedByResendCount(t *testing.T) {
	cred := "AIzaSyD9x8w7v6u5t4s3r2q1p0o9n8m7l6k5j4i"
	rec := &audit.Record{
		TS: time.Now(), Model: "coding", Protocol: "anthropic-messages", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{
				Body: map[string]any{
					"model": "coding",
					"messages": []any{
						map[string]any{"role": "user", "content": "key " + cred},
						map[string]any{"role": "assistant", "content": "ok"},
						map[string]any{"role": "user", "content": "key " + cred},
						map[string]any{"role": "assistant", "content": "ok"},
						map[string]any{"role": "user", "content": "key " + cred},
					},
				},
			},
			Response: &audit.Message{
				Body: "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\"}}\n\n" +
					"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
					"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"leaked: " + cred + "\"}}\n\n" +
					"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
					"data: {\"type\":\"message_stop\"}\n\n",
			},
		},
	}
	facts := scanRecordForGuard(rec)
	if facts == nil {
		t.Fatal("scanRecordForGuard returned nil, want non-nil facts")
	}
	if facts.TextEchoCount != 1 {
		t.Errorf("TextEchoCount = %d, want 1 (the credential appears 3 times in the resent request history but is echoed once in the response)", facts.TextEchoCount)
	}
}

// TestScanRecordForGuard_TextEchoCountNotAmplifiedByMultiRuleMatch covers a
// second amplification source distinct from resend count: a single
// credential that satisfies more than one rule's shape (a 48-char legacy
// OpenAI key also matches Tier2's looser generic-sk-prefix, per
// guard.TestFingerprint_RuleNameBound) must still contribute exactly one
// copy to scanOutbound's `known` set, not one per matching rule -- two
// copies of the same secret in `known` would double-count a single real
// echo in the response.
func TestScanRecordForGuard_TextEchoCountNotAmplifiedByMultiRuleMatch(t *testing.T) {
	cred := "sk-OhbVrpoiVgRV5IfLBcbfnoGMbJmTPSIAoCLrZ3aWZkSBvrjn" // matches both openai-legacy-key and generic-sk-prefix
	rec := &audit.Record{
		TS: time.Now(), Model: "coding", Protocol: "anthropic-messages", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{
				Body: map[string]any{
					"model": "coding",
					"messages": []any{
						map[string]any{"role": "user", "content": "key " + cred},
					},
				},
			},
			Response: &audit.Message{
				Body: "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\"}}\n\n" +
					"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
					"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"leaked: " + cred + "\"}}\n\n" +
					"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
					"data: {\"type\":\"message_stop\"}\n\n",
			},
		},
	}
	facts := scanRecordForGuard(rec)
	if facts == nil {
		t.Fatal("scanRecordForGuard returned nil, want non-nil facts")
	}
	if len(facts.Hits) != 2 {
		t.Fatalf("Hits = %+v, want 2 (openai-legacy-key + generic-sk-prefix, same secret)", facts.Hits)
	}
	if facts.TextEchoCount != 1 {
		t.Errorf("TextEchoCount = %d, want 1 (one real echo, not one per matching rule)", facts.TextEchoCount)
	}
}
