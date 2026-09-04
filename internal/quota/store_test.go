// Ver 2026-08-07, by Opus 5

package quota

import (
	"bytes"
	"errors"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vmr/internal/core"
)

func TestStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	ps := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	r := NewRegistry(path)
	r.Charge("plan-a", "tokens/1mo", ps, Counters{Fresh: 100, CacheRead: 10, Out: 20}, 5)
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file perm = %o, want 0600", perm)
	}

	r2 := NewRegistry(path)
	if err := r2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	c, est := r2.Used("plan-a", "tokens/1mo", ps)
	if c.Fresh != 100 || c.CacheRead != 10 || c.Out != 20 || est != 5 {
		t.Fatalf("round-tripped counters = %+v est=%v, want Fresh=100 CacheRead=10 Out=20 est=5", c, est)
	}
}

// TestStore_RoundTrip_CostAndEstimatedCost is TestStore_RoundTrip's
// counterpart: Counters.Cost and bucket.EstimatedCost (both added for
// metric: cost accounting) had no persistence coverage of their own before
// this — TestStore_RoundTrip only ever charges the P1-era int fields, so a
// JSON-tag typo or a dropped field specific to these two would not have
// been caught by any quota-package-level test — the in-memory ChargeCost
// tests in quota_test.go never go through Flush/Load.
func TestStore_RoundTrip_CostAndEstimatedCost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	ps := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	r := NewRegistry(path)
	r.Charge("plan-e", "cost/1mo", ps, Counters{Fresh: 1000, Out: 200, Cost: 12.3456}, 0)
	r.ChargeCost("plan-e", "cost/1mo", ps, Counters{}, 4.5)
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	r2 := NewRegistry(path)
	if err := r2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	c, _ := r2.Used("plan-e", "cost/1mo", ps)
	if c.Cost != 12.3456 {
		t.Fatalf("round-tripped Counters.Cost = %v, want 12.3456", c.Cost)
	}
	if _, _, estCost := r2.Snapshot("plan-e", "cost/1mo", ps); estCost != 4.5 {
		t.Fatalf("round-tripped EstimatedCost = %v, want 4.5", estCost)
	}
}

func TestStore_MissingFile_NotAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")
	r := NewRegistry(path)
	if err := r.Load(); err != nil {
		t.Fatalf("Load on missing file = %v, want nil (first run)", err)
	}
	c, _ := r.Used("plan-a", "requests/1mo", time.Now())
	if c.Requests != 0 {
		t.Fatalf("fresh registry after missing-file Load has non-zero state: %+v", c)
	}
}

func TestStore_CorruptFile_ReturnsErrorButCallerCanProceed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	r := NewRegistry(path)
	if err := r.Load(); err == nil {
		t.Fatal("Load on corrupt file returned nil error, want non-nil (caller decides to proceed from zero)")
	}
	// Per the design doc's Persistence section: a corrupt file must never
	// leave the Registry unusable — it should still behave like a fresh one.
	c, _ := r.Used("plan-a", "requests/1mo", time.Now())
	if c.Requests != 0 {
		t.Fatalf("registry state after failed Load = %+v, want zero value (usable)", c)
	}
	r.Charge("plan-a", "requests/1mo", time.Now(), Counters{Requests: 1}, 0)
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush after failed Load: %v", err)
	}
}

func TestStore_HalfWrittenFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"accounts":{"plan-a":{"requests`), 0o600); err != nil {
		t.Fatalf("seed half-written file: %v", err)
	}
	r := NewRegistry(path)
	if err := r.Load(); err == nil {
		t.Fatal("Load on a half-written (truncated mid-write) file returned nil error, want non-nil")
	}
}

func TestStore_MkdirAllForMissingLogDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "does", "not", "exist", "vmr-quota.json")
	r := NewRegistry(path)
	ps := time.Now()
	r.Charge("plan-a", "requests/1mo", ps, Counters{Requests: 1}, 0)
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush into a non-existent directory tree: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist after Flush: %v", err)
	}
}

func TestStore_FlushIsNoOpWhenNotDirty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	r := NewRegistry(path)
	ps := time.Now()
	r.Charge("plan-a", "requests/1mo", ps, Counters{Requests: 1}, 0)
	if err := r.Flush(); err != nil {
		t.Fatalf("first Flush: %v", err)
	}
	info1, _ := os.Stat(path)

	if err := r.Flush(); err != nil { // nothing changed since — should be a no-op
		t.Fatalf("second Flush: %v", err)
	}
	info2, _ := os.Stat(path)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Fatalf("Flush rewrote the file with nothing dirty: mtimes differ (%v vs %v)", info1.ModTime(), info2.ModTime())
	}
}

func TestStore_EmptyPathNeverWrites(t *testing.T) {
	r := NewRegistry("")
	r.Charge("plan-a", "requests/1mo", time.Now(), Counters{Requests: 1}, 0)
	if err := r.Load(); err != nil {
		t.Fatalf("Load with empty path: %v", err)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush with empty path: %v", err)
	}
}

func TestStore_Flusher_PeriodicAndFinalFlush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	r := NewRegistry(path)
	ps := time.Now()
	r.Charge("plan-a", "requests/1mo", ps, Counters{Requests: 1}, 0)

	stop := r.StartFlusher(20 * time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("flusher never wrote the file within the deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Charge again right before stopping, then rely on the caller's final
	// Flush (the documented cmd_start.go pattern: stop() then Flush()) to
	// capture it — stop() must have fully joined the goroutine first so
	// this Flush can't race with one still in flight.
	r.Charge("plan-a", "requests/1mo", ps, Counters{Requests: 1}, 0)
	stop()
	if err := r.Flush(); err != nil {
		t.Fatalf("final Flush after stop: %v", err)
	}

	r2 := NewRegistry(path)
	if err := r2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	c, _ := r2.Used("plan-a", "requests/1mo", ps)
	if c.Requests != 2 {
		t.Fatalf("final count after stop+Flush = %v, want 2", c.Requests)
	}
}

func TestStore_EmptyPathFlusherIsNoOp(t *testing.T) {
	r := NewRegistry("")
	stop := r.StartFlusher(10 * time.Millisecond)
	stop() // must return promptly, not block forever
}

// TestStore_FlushFailure_KeepsDirtyAndReports pins R47's first half: a Flush
// that fails (here a NaN counter makes json.MarshalIndent fail — the exact
// R42 poisoning trigger) must (a) return the error, (b) NOT swallow dirty, so
// the next tick retries instead of dropping the unsaved state forever.
func TestStore_FlushFailure_KeepsDirtyAndReports(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	r := NewRegistry(path)
	ps := time.Now()
	r.Charge("plan-a", "requests/1mo", ps, Counters{Cost: math.NaN()}, 0) // NaN poisons marshal

	if err := r.Flush(); err == nil {
		t.Fatal("Flush with a NaN counter returned nil, want an error")
	}
	// dirty must survive the failure so the next tick retries.
	r.mu.Lock()
	dirty := r.dirty
	r.mu.Unlock()
	if !dirty {
		t.Fatal("dirty was cleared after a failed Flush — the unsaved state would never be retried")
	}
	// And the file must not exist (failure happened before any rename).
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed Flush left a file behind: %v", err)
	}
}

// TestStore_Flusher_ReportsFailures pins R47's second half: StartFlusher must
// surface repeated Flush failures to its logger instead of dropping them.
func TestStore_Flusher_ReportsFailures(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	r := NewRegistry(path)
	ps := time.Now()
	r.Charge("plan-a", "requests/1mo", ps, Counters{Cost: math.NaN()}, 0) // every tick's Flush fails

	// The flusher goroutine writes through the logger while this test reads,
	// so the buffer needs its own lock (go test -race catches the raw kind).
	var mu sync.Mutex
	var buf bytes.Buffer
	logger := log.New(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return buf.Write(p)
	}), "", 0)
	r.SetLogger(logger)
	stop := r.StartFlusher(10 * time.Millisecond)
	defer stop()

	deadline := time.Now().Add(2 * time.Second)
	for func() bool {
		mu.Lock()
		defer mu.Unlock()
		return buf.Len() == 0
	}() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(buf.String(), "quota flush") {
		t.Fatalf("flusher never logged the Flush failure; log=%q", buf.String())
	}
}

// writerFunc adapts a func to io.Writer without a named type per call site.
type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// TestFlushLog_Dedup pins R47's throttling: identical consecutive failures
// log on the first and every 10th occurrence, not on every tick.
func TestFlushLog_Dedup(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	fl := &flushLog{logger: logger}
	boom := errors.New("disk full")
	for i := 0; i < 25; i++ {
		fl.Error(boom)
	}
	// 25 identical failures: exactly 3 lines (1st, 10th, 20th).
	if n := strings.Count(buf.String(), "\n"); n != 3 {
		t.Fatalf("25 identical failures logged %d lines, want 3 (dedup broken)", n)
	}
	if !strings.Contains(buf.String(), "10 consecutive") || !strings.Contains(buf.String(), "20 consecutive") {
		t.Fatalf("repeated-failure lines must carry the consecutive count: %q", buf.String())
	}
}

// TestStore_LoadFile_NullBucketRejected pins R43: a null bucket decodes fine
// (a zero value), so only an explicit nil-bucket check can reject it — a
// null must fail the load, not panic later in the response path.
func TestStore_LoadFile_NullBucketRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"accounts":{"acct":{"requests/1mo":null}}}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatal("LoadFile accepted a null bucket, want an error (and no panic)")
	}
}

// TestStore_LoadFile_NilAccountsRejected pins the top-level half of
// validateLoadedShape: `accounts: null` (or the key missing entirely) is
// also structural damage, not an empty ledger to silently adopt — Flush
// never writes either shape (the field has no omitempty), so a nil
// top-level map can only come from a hand-edited or damaged file.
func TestStore_LoadFile_NilAccountsRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	for _, body := range []string{`{"version":1,"accounts":null}`, `{"version":1}`} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("seed %s: %v", body, err)
		}
		if _, err := LoadFile(path); err == nil {
			t.Errorf("LoadFile accepted %s, want an error", body)
		}
	}
}

// TestStore_LoadFile_WrongVersionRejected pins R66: a version stamp that is
// never validated is worse than none — refuse to adopt an unknown shape.
func TestStore_LoadFile_WrongVersionRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"accounts":{"acct":{"requests/1mo":{"period_start":0,"counters":{}}}}}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatal("LoadFile accepted version 99, want an error")
	}
}

// TestStore_LoadFile_ValidFileLoaded pins the LoadFile happy path that the
// R43/R66 rejection must not break.
func TestStore_LoadFile_ValidFileLoaded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	r := NewRegistry(path)
	ps := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.Charge("plan-a", "requests/1mo", ps, Counters{Requests: 7}, 0)
	if err := r.Flush(); err != nil {
		t.Fatalf("seed flush: %v", err)
	}
	accounts, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile on a valid file: %v", err)
	}
	if accounts["plan-a"]["requests/1mo"].C.Requests != 7 {
		t.Fatalf("LoadFile returned wrong data: %+v", accounts)
	}
}

// TestStore_Load_StructurallyCorrupt_RegistryUsable pins the R43 end-to-end
// contract: a structurally corrupt file (null bucket) makes Load fail, but
// the Registry must still start from zero and remain usable — the same
// "corrupt file must never stall routing" contract as syntax corruption.
func TestStore_Load_StructurallyCorrupt_RegistryUsable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"accounts":{"acct":{"requests/1mo":null}}}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	r := NewRegistry(path)
	if err := r.Load(); err == nil {
		t.Fatal("Load accepted a null bucket, want an error")
	}
	// Must behave like a fresh registry: no panic, counts work.
	c, _ := r.Used("acct", "requests/1mo", time.Now())
	if c.Requests != 0 {
		t.Fatalf("registry after failed structural Load = %+v, want zero value", c)
	}
	r.Charge("acct", "requests/1mo", time.Now(), Counters{Requests: 1}, 0)
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush after failed structural Load: %v", err)
	}
}

func TestStore_PruneOrphanKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	r := NewRegistry(path)
	ps := time.Now()

	// Seed state with multiple providers and keys
	r.Charge("removed-provider", "requests/1mo", ps, Counters{Requests: 10}, 0)
	r.Charge("active-p1", "requests/1mo", ps, Counters{Requests: 5}, 0)               // old every
	r.Charge("active-p1", "requests/1d", ps, Counters{Requests: 2}, 0)                // current every
	r.Charge("active-p2", "requests/1d#model=gpt-4o", ps, Counters{Requests: 3}, 0)   // allowed model
	r.Charge("active-p2", "requests/1d#model=orphan-m", ps, Counters{Requests: 4}, 0) // orphan model

	if err := r.Flush(); err != nil {
		t.Fatalf("initial Flush: %v", err)
	}

	valid := map[string][]core.Limit{
		"active-p1": {
			{Metric: core.MetricRequests, EveryText: "1d"},
		},
		"active-p2": {
			{Metric: core.MetricRequests, EveryText: "1d", Models: []string{"gpt-4o"}},
		},
	}

	pruned := r.Prune(valid)
	if pruned != 3 { // removed-provider (1), active-p1 old key (1), active-p2 orphan model (1)
		t.Fatalf("pruned = %d, want 3", pruned)
	}

	if err := r.Flush(); err != nil {
		t.Fatalf("Flush after prune: %v", err)
	}

	// Reload in fresh registry and verify
	r2 := NewRegistry(path)
	if err := r2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// removed-provider should be gone
	if c, _ := r2.Used("removed-provider", "requests/1mo", ps); c.Requests != 0 {
		t.Errorf("removed-provider still exists with requests=%v", c.Requests)
	}
	// active-p1 old key gone, new key preserved
	if c, _ := r2.Used("active-p1", "requests/1mo", ps); c.Requests != 0 {
		t.Errorf("active-p1 old key still exists with requests=%v", c.Requests)
	}
	if c, _ := r2.Used("active-p1", "requests/1d", ps); c.Requests != 2 {
		t.Errorf("active-p1 valid key = %v, want 2", c.Requests)
	}
	// active-p2 orphan model gone, gpt-4o preserved
	if c, _ := r2.Used("active-p2", "requests/1d#model=orphan-m", ps); c.Requests != 0 {
		t.Errorf("active-p2 orphan model still exists with requests=%v", c.Requests)
	}
	if c, _ := r2.Used("active-p2", "requests/1d#model=gpt-4o", ps); c.Requests != 3 {
		t.Errorf("active-p2 gpt-4o = %v, want 3", c.Requests)
	}
}

// TestStore_ConcurrentChargeAndFlush verifies data consistency and lack of race
// conditions when online Charge calls happen concurrently with background Flush calls.
func TestStore_ConcurrentChargeAndFlush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-quota.json")
	r := NewRegistry(path)
	ps := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	const (
		numGoroutines = 10
		opsPerRoutine = 50
	)

	var wg sync.WaitGroup
	stopFlush := make(chan struct{})

	// Goroutines repeatedly flushing in the background
	var flushWG sync.WaitGroup
	for i := 0; i < 3; i++ {
		flushWG.Add(1)
		go func() {
			defer flushWG.Done()
			for {
				select {
				case <-stopFlush:
					return
				default:
					_ = r.Flush()
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()
	}

	// Goroutines concurrently charging
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerRoutine; j++ {
				r.Charge("provider-concurrent", "tokens/1mo", ps, Counters{
					Fresh:     10,
					CacheRead: 5,
					Out:       2,
					Requests:  1,
				}, 1)
			}
		}(i)
	}

	wg.Wait()
	close(stopFlush)
	flushWG.Wait()

	// Final flush to ensure all charges are persisted
	if err := r.Flush(); err != nil {
		t.Fatalf("final Flush: %v", err)
	}

	r2 := NewRegistry(path)
	if err := r2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	c, est := r2.Used("provider-concurrent", "tokens/1mo", ps)
	wantFresh := float64(numGoroutines * opsPerRoutine * 10)
	wantCacheRead := float64(numGoroutines * opsPerRoutine * 5)
	wantOut := float64(numGoroutines * opsPerRoutine * 2)
	wantRequests := float64(numGoroutines * opsPerRoutine * 1)
	wantEst := float64(numGoroutines * opsPerRoutine * 1)

	if c.Fresh != wantFresh || c.CacheRead != wantCacheRead || c.Out != wantOut || c.Requests != wantRequests || est != wantEst {
		t.Fatalf("counters mismatch after concurrent charge/flush: got Fresh=%v CacheRead=%v Out=%v Requests=%v est=%v; want Fresh=%v CacheRead=%v Out=%v Requests=%v est=%v",
			c.Fresh, c.CacheRead, c.Out, c.Requests, est, wantFresh, wantCacheRead, wantOut, wantRequests, wantEst)
	}
}
