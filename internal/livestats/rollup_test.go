package livestats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSlimFile_JSONSchemaAndPermissions(t *testing.T) {
	dir := t.TempDir()
	fixedTime := time.Date(2026, 9, 7, 14, 32, 1, 0, time.FixedZone("CST", 8*3600))

	f, err := openSlim(dir, fixedTime)
	if err != nil {
		t.Fatalf("openSlim: %v", err)
	}
	defer f.Close()

	// Verify file mode 0600
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("stat slim: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected permissions 0600, got %#o", info.Mode().Perm())
	}

	// Verify filename matching hourFileName
	expectedName := "vmr-stats-20260907-14.jsonl"
	if info.Name() != expectedName {
		t.Errorf("expected filename %q, got %q", expectedName, info.Name())
	}

	// Append a sample line matching design §3.2 example
	row := slimRow{
		TS:           fixedTime.Format(time.RFC3339),
		VModel:       "coding",
		Protocol:     "anthropic-messages",
		Stream:       true,
		Outcome:      "ok",
		ClientKeyTag: "jason",
		Provider:     "p1",
		Model:        "claude-sonnet-4",
		KeyLabel:     "main",
		Forwarded:    true,
		DurMS:        12340,
		TTFTMS:       412,
		Tokens: TokenCounts{
			In:         1200,
			Out:        900,
			CacheRead:  0,
			CacheWrite: 0,
		},
	}
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal slim row: %v", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		t.Fatalf("write slim row: %v", err)
	}

	// Verify keys in the JSON map match design doc §3.2 verbatim
	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	expectedKeys := []string{
		"ts", "vmodel", "protocol", "stream", "outcome", "client_key_tag",
		"provider", "model", "key_label", "forwarded", "dur_ms", "ttft_ms", "tokens",
	}
	for _, k := range expectedKeys {
		if _, ok := parsed[k]; !ok {
			t.Errorf("missing key in slim JSON: %s", k)
		}
	}
	tokensMap, ok := parsed["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("expected tokens to be an object, got %T", parsed["tokens"])
	}
	for _, tk := range []string{"in", "out", "cache_read", "cache_write"} {
		if _, ok := tokensMap[tk]; !ok {
			t.Errorf("missing token component: %s", tk)
		}
	}
}

func TestRollupFile_JSONSchemaAndPermissions(t *testing.T) {
	dir := t.TempDir()
	fixedHour := time.Date(2026, 9, 7, 13, 0, 0, 0, time.FixedZone("CST", 8*3600))
	dims := dimsKey{
		vmodel:       "coding",
		protocol:     "anthropic-messages",
		clientKeyTag: "jason",
		provider:     "p1",
		model:        "claude-sonnet-4",
		keyLabel:     "main",
		stream:       true,
	}
	cnt := Counters{
		OK:       12,
		Error:    1,
		Canceled: 0,
		Tokens: TokenCounts{
			In:         14400,
			Out:        10800,
			CacheRead:  0,
			CacheWrite: 0,
		},
		DurMS:  SumCount{Sum: 148080, N: 13},
		TTFTMS: SumCount{Sum: 4944, N: 12},
	}

	rollupPath := filepath.Join(dir, rollupFileName)
	row := countersRow(fixedHour, dims, cnt)
	if err := appendJSONL(rollupPath, row); err != nil {
		t.Fatalf("appendJSONL: %v", err)
	}

	info, err := os.Stat(rollupPath)
	if err != nil {
		t.Fatalf("stat rollup: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected permissions 0600, got %#o", info.Mode().Perm())
	}

	// Verify JSON keys match design doc §3.3 verbatim
	b, err := os.ReadFile(rollupPath)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	for _, k := range []string{"hour", "dims", "ok", "error", "canceled", "tokens", "dur_ms", "ttft_ms"} {
		if _, ok := parsed[k]; !ok {
			t.Errorf("missing key in rollup JSON: %s", k)
		}
	}
	durObj, ok := parsed["dur_ms"].(map[string]any)
	if !ok || durObj["sum"] == nil || durObj["n"] == nil {
		t.Errorf("dur_ms must have sum and n, got %v", parsed["dur_ms"])
	}
	ttftObj, ok := parsed["ttft_ms"].(map[string]any)
	if !ok || ttftObj["sum"] == nil || ttftObj["n"] == nil {
		t.Errorf("ttft_ms must have sum and n, got %v", parsed["ttft_ms"])
	}
}

func TestRollupFile_LastWinsAndCorruptLineTolerance(t *testing.T) {
	dir := t.TempDir()
	rollupPath := filepath.Join(dir, rollupFileName)
	hour := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	k := dimsKey{vmodel: "coding", provider: "p1", model: "m1", stream: true}

	// Row 1: earlier count (OK = 5)
	c1 := Counters{OK: 5}
	if err := appendJSONL(rollupPath, countersRow(hour, k, c1)); err != nil {
		t.Fatalf("append c1: %v", err)
	}

	// Inject a half-line / corrupted JSON
	f, err := os.OpenFile(rollupPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("{\"hour\":\"corrupted-half-line\n"); err != nil {
		t.Fatalf("write corrupt line: %v", err)
	}
	f.Close()

	// Row 2: later count (OK = 10, should win)
	c2 := Counters{OK: 10}
	if err := appendJSONL(rollupPath, countersRow(hour, k, c2)); err != nil {
		t.Fatalf("append c2: %v", err)
	}

	m, err := loadRollup(rollupPath, time.Time{})
	if err != nil {
		t.Fatalf("loadRollup: %v", err)
	}

	loaded := m[hour][k]
	if loaded.OK != 10 {
		t.Errorf("expected last-wins OK=10, got %d", loaded.OK)
	}
}

func TestRollSlimFile_Idempotent(t *testing.T) {
	dir := t.TempDir()
	hour := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	slimName := hourFileName(hour)

	f, err := os.OpenFile(filepath.Join(dir, slimName), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create slim: %v", err)
	}
	defer f.Close()

	row := slimRow{
		TS:           hour.Add(5 * time.Minute).Format(time.RFC3339),
		VModel:       "coding",
		Protocol:     "openai-completions",
		Stream:       false,
		Outcome:      "ok",
		ClientKeyTag: "alice",
		Provider:     "p2",
		Model:        "gpt-5",
		KeyLabel:     "sec",
		Forwarded:    true,
		DurMS:        500,
		TTFTMS:       100,
		Tokens:       TokenCounts{In: 50, Out: 20},
	}
	b, _ := json.Marshal(row)
	f.Write(append(b, '\n'))
	f.Close()

	// Roll once
	if err := rollSlimFile(dir, slimName); err != nil {
		t.Fatalf("first roll: %v", err)
	}

	// Roll again (simulating duplicate roll on crash before delete)
	if err := rollSlimFile(dir, slimName); err != nil {
		t.Fatalf("second roll: %v", err)
	}

	// Load rollup: last-wins must ensure counts are exactly from one roll, not doubled
	m, err := loadRollup(filepath.Join(dir, rollupFileName), time.Time{})
	if err != nil {
		t.Fatalf("loadRollup: %v", err)
	}

	k := rowDimsKey(row)
	hourTrunc := hourStartOf(hour)
	c := m[hourTrunc][k]
	if c.OK != 1 || c.Tokens.In != 50 || c.Tokens.Out != 20 {
		t.Errorf("idempotency violated: got OK=%d In=%d Out=%d", c.OK, c.Tokens.In, c.Tokens.Out)
	}
}
