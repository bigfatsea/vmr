// Ver 2026-08-07, by Opus 5

package quota

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
		t.Fatalf("round-tripped counters = %+v est=%d, want Fresh=100 CacheRead=10 Out=20 est=5", c, est)
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
	r.Charge("plan-a", "requests/1mo", time.Now(), Counters{Requests: 1}, 0)
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
	r.Charge("plan-a", "requests/1mo", time.Now(), Counters{Requests: 1}, 0)
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
	r.Charge("plan-a", "requests/1mo", time.Now(), Counters{Requests: 1}, 0)

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
	r.Charge("plan-a", "requests/1mo", time.Now(), Counters{Requests: 1}, 0)
	stop()
	if err := r.Flush(); err != nil {
		t.Fatalf("final Flush after stop: %v", err)
	}

	r2 := NewRegistry(path)
	if err := r2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	c, _ := r2.Used("plan-a", "requests/1mo", time.Now())
	if c.Requests != 2 {
		t.Fatalf("final count after stop+Flush = %d, want 2", c.Requests)
	}
}

func TestStore_EmptyPathFlusherIsNoOp(t *testing.T) {
	r := NewRegistry("")
	stop := r.StartFlusher(10 * time.Millisecond)
	stop() // must return promptly, not block forever
}
