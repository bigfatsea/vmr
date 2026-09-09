package livestats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestAggregator_AttributionRules(t *testing.T) {
	dir := t.TempDir()
	clock := time.Date(2026, 9, 7, 15, 0, 0, 0, time.Local)
	now := func() time.Time { return clock }

	agg, err := NewAt(dir, now)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	// 1. Unforwarded failure (rejected locally, Provider is empty)
	sUnf := Sample{
		TS:           clock.Add(time.Minute),
		VModel:       "coding",
		Protocol:     "openai-completions",
		Stream:       false,
		Outcome:      OutcomeError,
		ClientKeyTag: "alice",
		Provider:     "",
		Model:        "",
		DurMS:        5,
		TTFTMS:       0,
		Tokens:       TokenCounts{In: 100, Out: 50}, // should not be counted
	}
	agg.Record(sUnf)

	// 2. Forwarded success with ttft_ms = 0 (unmeasured)
	sZeroTTFT := Sample{
		TS:           clock.Add(2 * time.Minute),
		VModel:       "coding",
		Protocol:     "openai-completions",
		Stream:       false,
		Outcome:      OutcomeOK,
		ClientKeyTag: "alice",
		Provider:     "p1",
		Model:        "m1",
		KeyLabel:     "main",
		DurMS:        100,
		TTFTMS:       0, // unmeasured: must be excluded from ttft sum/n and ring
		Tokens:       TokenCounts{In: 200, Out: 100},
	}
	agg.Record(sZeroTTFT)

	// 3. Forwarded success with valid TTFT
	sValid := Sample{
		TS:           clock.Add(3 * time.Minute),
		VModel:       "coding",
		Protocol:     "openai-completions",
		Stream:       false,
		Outcome:      OutcomeOK,
		ClientKeyTag: "alice",
		Provider:     "p1",
		Model:        "m1",
		KeyLabel:     "main",
		DurMS:        500,
		TTFTMS:       120,
		Tokens:       TokenCounts{In: 300, Out: 150},
	}
	agg.Record(sValid)

	snap := agg.Snapshot()

	// Verify ProviderRow
	if len(snap.ByProviderModel) != 1 {
		t.Fatalf("expected 1 provider row, got %d", len(snap.ByProviderModel))
	}
	pr := snap.ByProviderModel[0]
	if pr.Provider != "p1" || pr.Model != "m1" {
		t.Errorf("unexpected provider/model: %s/%s", pr.Provider, pr.Model)
	}
	if pr.OK != 2 {
		t.Errorf("expected 2 OKs for provider, got %d", pr.OK)
	}
	// Tokens should be sum of sample 2 and 3: in=500, out=250
	if pr.Tokens.In != 500 || pr.Tokens.Out != 250 {
		t.Errorf("expected In=500 Out=250, got In=%d Out=%d", pr.Tokens.In, pr.Tokens.Out)
	}
	// TTFTMS: only sample 3 has non-zero TTFT
	if pr.TTFTMS.N != 1 || pr.TTFTMS.Sum != 120 {
		t.Errorf("expected TTFT N=1 Sum=120, got N=%d Sum=%d", pr.TTFTMS.N, pr.TTFTMS.Sum)
	}
	// Ring: only sample 3 should be in ring
	if pr.Last10 == nil || pr.Last10.TTFTP50 != 120 {
		t.Errorf("expected Last10 TTFT p50=120, got %v", pr.Last10)
	}
}

func TestAggregator_HourlyLazyRollAndFileLifecycle(t *testing.T) {
	dir := t.TempDir()
	clock := time.Date(2026, 9, 7, 10, 0, 0, 0, time.Local)
	now := func() time.Time { return clock }

	agg, err := NewAt(dir, now)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	// Record in hour 10
	s1 := Sample{
		TS:       clock.Add(10 * time.Minute),
		VModel:   "coding",
		Outcome:  OutcomeOK,
		Provider: "p1",
		Model:    "m1",
		Tokens:   TokenCounts{In: 10, Out: 10},
	}
	agg.Record(s1)

	h10Slim := filepath.Join(dir, hourFileName(clock))
	if _, err := os.Stat(h10Slim); err != nil {
		t.Fatalf("expected hour 10 slim to exist: %v", err)
	}

	// Advance clock to hour 11 and record sample arriving in hour 11
	clock = clock.Add(time.Hour)
	s2 := Sample{
		TS:       clock.Add(5 * time.Minute),
		VModel:   "coding",
		Outcome:  OutcomeOK,
		Provider: "p1",
		Model:    "m1",
		Tokens:   TokenCounts{In: 20, Out: 20},
	}
	agg.Record(s2)

	// Hour 10 slim should have been rolled and deleted
	if _, err := os.Stat(h10Slim); !os.IsNotExist(err) {
		t.Errorf("expected hour 10 slim to be deleted, err = %v", err)
	}

	// Rollup file should exist and have hour 10 data
	rollupPath := filepath.Join(dir, rollupFileName)
	if _, err := os.Stat(rollupPath); err != nil {
		t.Errorf("expected rollup file to exist: %v", err)
	}

	// Hour 11 slim should now exist
	h11Slim := filepath.Join(dir, hourFileName(clock))
	if _, err := os.Stat(h11Slim); err != nil {
		t.Errorf("expected hour 11 slim to exist: %v", err)
	}

	// Snapshot should reflect both hour 10 and hour 11 in hourly[]
	snap := agg.Snapshot()
	if len(snap.Hourly) != 2 {
		t.Errorf("expected 2 hourly rows, got %d", len(snap.Hourly))
	}
}

func TestAggregator_RestartRecovery(t *testing.T) {
	dir := t.TempDir()
	h9 := time.Date(2026, 9, 7, 9, 0, 0, 0, time.Local)
	h10 := time.Date(2026, 9, 7, 10, 0, 0, 0, time.Local)

	// Step 1: create an unrolled slim file from hour 9 (simulating shutdown before roll)
	f9, err := openSlim(dir, h9)
	if err != nil {
		t.Fatalf("openSlim h9: %v", err)
	}
	row9 := slimRow{
		TS:       h9.Add(10 * time.Minute).Format(time.RFC3339),
		VModel:   "coding",
		Outcome:  OutcomeOK,
		Provider: "p1",
		Model:    "m1",
		Tokens:   TokenCounts{In: 100, Out: 50},
	}
	b9, _ := json.Marshal(row9)
	f9.Write(append(b9, '\n'))
	f9.Close()

	// Step 2: create an hour 10 slim file with 120 samples
	f10, err := openSlim(dir, h10)
	if err != nil {
		t.Fatalf("openSlim h10: %v", err)
	}
	for i := 1; i <= 120; i++ {
		row10 := slimRow{
			TS:       h10.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
			VModel:   "coding",
			Outcome:  OutcomeOK,
			Provider: "p1",
			Model:    "m1",
			DurMS:    int64(i * 10),
			TTFTMS:   int64(i),
			Tokens:   TokenCounts{In: 1, Out: 1},
		}
		b10, _ := json.Marshal(row10)
		f10.Write(append(b10, '\n'))
	}
	f10.Close()

	// Step 3: Start Aggregator at hour 10
	now := func() time.Time { return h10.Add(30 * time.Minute) }
	agg, err := NewAt(dir, now)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	// Hour 9 slim should have been catch-up rolled and deleted
	if _, err := os.Stat(filepath.Join(dir, hourFileName(h9))); !os.IsNotExist(err) {
		t.Errorf("expected hour 9 slim to be deleted after catch-up roll")
	}

	// Hour 10 slim should still exist and be currently active
	if _, err := os.Stat(filepath.Join(dir, hourFileName(h10))); err != nil {
		t.Errorf("hour 10 slim should remain open for write")
	}

	snap := agg.Snapshot()

	// Ring must have recovered the last ≤100 samples (samples 21..120)
	if len(snap.ByProviderModel) != 1 {
		t.Fatalf("expected 1 provider row, got %d", len(snap.ByProviderModel))
	}
	pr := snap.ByProviderModel[0]
	if pr.Last10 == nil || pr.Last100 == nil {
		t.Fatalf("expected Last10 and Last100 to be populated")
	}
	// For samples 21..120 (100 items), TTFT are 21..120.
	// p50: ceil(0.50 * 100) = 50 -> element 50 (val 21 + 49 = 70)
	if pr.Last100.TTFTP50 != 70 {
		t.Errorf("expected Last100 TTFT p50=70, got %d", pr.Last100.TTFTP50)
	}

	// ProviderRow reflects cumulative count (1 from h9 + 120 from h10 = 121)
	if pr.OK != 121 {
		t.Errorf("expected cumulative OK=121, got %d", pr.OK)
	}

	// Hourly rows should separately show hour 9 and hour 10
	if len(snap.Hourly) != 2 {
		t.Fatalf("expected 2 hourly rows, got %d", len(snap.Hourly))
	}
	if snap.Hourly[0].Counters.OK != 1 || snap.Hourly[1].Counters.OK != 120 {
		t.Errorf("hourly breakdown mismatch: h9=%d, h10=%d", snap.Hourly[0].Counters.OK, snap.Hourly[1].Counters.OK)
	}
}

// TestAggregator_DirLockRejectsSecondInstance: two aggregators on one dir
// cannot coexist — the second fails to take the advisory flock, so slim/rollup
// stay single-writer even with -audit=false (design §3.2). Skipped on Windows,
// where the lock is a deliberate no-op.
func TestAggregator_DirLockRejectsSecondInstance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no flock on windows; acquireDirLock is a deliberate no-op there")
	}
	dir := t.TempDir()
	now := func() time.Time { return time.Date(2026, 9, 7, 12, 0, 0, 0, time.Local) }

	a1, err := NewAt(dir, now)
	if err != nil {
		t.Fatalf("first NewAt: %v", err)
	}

	if a2, err := NewAt(dir, now); err == nil {
		a2.Close()
		t.Fatal("second NewAt on the same dir succeeded, want lock error")
	}

	// After the first releases, a fresh instance can take the dir.
	a1.Close()
	a3, err := NewAt(dir, now)
	if err != nil {
		t.Fatalf("NewAt after first Close: %v", err)
	}
	a3.Close()
}

// TestAggregator_CachedSnapshotStaleWindow: CachedSnapshot reuses the last
// fold for up to snapCacheTTL (Record does not invalidate it); past the TTL it
// recomputes. Snapshot itself stays always-fresh.
func TestAggregator_CachedSnapshotStaleWindow(t *testing.T) {
	dir := t.TempDir()
	clock := time.Date(2026, 9, 7, 13, 0, 0, 0, time.Local)
	now := func() time.Time { return clock }

	agg, err := NewAt(dir, now)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	mk := func() Sample {
		return Sample{
			TS: clock, VModel: "coding", Outcome: OutcomeOK,
			Provider: "p1", Model: "m1", DurMS: 100, TTFTMS: 10,
			Tokens: TokenCounts{In: 1, Out: 1},
		}
	}

	agg.Record(mk())
	first := agg.CachedSnapshot()
	if got := providerOK(first); got != 1 {
		t.Fatalf("cached OK after 1 record = %d, want 1", got)
	}

	// Within the TTL: a new record is not reflected by CachedSnapshot, but
	// Snapshot sees it immediately.
	agg.Record(mk())
	if got := providerOK(agg.CachedSnapshot()); got != 1 {
		t.Errorf("cached OK within TTL = %d, want stale 1", got)
	}
	if got := providerOK(agg.Snapshot()); got != 2 {
		t.Errorf("fresh Snapshot OK = %d, want 2", got)
	}

	// Past the TTL: CachedSnapshot recomputes.
	clock = clock.Add(snapCacheTTL + time.Millisecond)
	if got := providerOK(agg.CachedSnapshot()); got != 2 {
		t.Errorf("cached OK past TTL = %d, want refreshed 2", got)
	}
}

func providerOK(s Snapshot) int64 {
	var n int64
	for _, r := range s.ByProviderModel {
		n += r.OK
	}
	return n
}

func TestAggregator_Concurrency(t *testing.T) {
	dir := t.TempDir()
	clock := time.Date(2026, 9, 7, 16, 0, 0, 0, time.Local)
	now := func() time.Time { return clock }

	agg, err := NewAt(dir, now)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer agg.Close()

	var wg sync.WaitGroup
	workers := 8
	perWorker := 50

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				s := Sample{
					TS:           clock.Add(time.Duration(i) * time.Second),
					VModel:       "coding",
					Protocol:     "anthropic-messages",
					Stream:       true,
					Outcome:      OutcomeOK,
					ClientKeyTag: "bob",
					Provider:     "p1",
					Model:        "sonnet",
					KeyLabel:     "key1",
					DurMS:        200,
					TTFTMS:       50,
					Tokens:       TokenCounts{In: 10, Out: 20},
				}
				agg.Record(s)
				if i%10 == 0 {
					_ = agg.Snapshot()
					_ = agg.CachedSnapshot()
				}
			}
		}(w)
	}
	wg.Wait()

	snap := agg.Snapshot()
	expectedTotal := int64(workers * perWorker)
	if len(snap.ByProviderModel) != 1 {
		t.Fatalf("expected 1 provider model row, got %d", len(snap.ByProviderModel))
	}
	if snap.ByProviderModel[0].OK != expectedTotal {
		t.Errorf("expected OK=%d, got %d", expectedTotal, snap.ByProviderModel[0].OK)
	}
}
