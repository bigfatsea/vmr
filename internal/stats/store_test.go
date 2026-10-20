package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// clock is a settable test clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time       { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) set(t time.Time)      { c.mu.Lock(); c.t = t; c.mu.Unlock() }
func at(y, m, d, h, mi int) time.Time { return time.Date(y, time.Month(m), d, h, mi, 0, 0, time.Local) }

func ev(ts time.Time, outcome string) Event {
	e := Event{TS: ts, VModel: "coding", Protocol: "openai-completions", Stream: true, Outcome: outcome,
		Attempts: 1, Provider: "p1", Model: "m1", KeyLabel: "k1", ClientKeyTag: "alice", DurMS: 1000, TTFTMS: 200}
	if outcome == OutcomeOK {
		e.Forwarded, e.Status = true, 200
		e.Tokens = Tokens{In: 10, Out: 100, CacheRead: 50, CacheWrite: 5}
	} else {
		e.Status, e.ErrorClass = 502, "upstream_5xx"
	}
	return e
}

func openTest(t *testing.T, dir string, c *clock) *Store {
	t.Helper()
	s, err := openAt(dir, c.now, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEvent_RoundTrip(t *testing.T) {
	in := ev(time.Date(2026, 10, 19, 14, 3, 11, 482_000_000, time.Local), OutcomeOK)
	in.HasTools, in.Src = true, SrcAudit
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Event
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !out.TS.Equal(in.TS) {
		t.Errorf("ts %v != %v", out.TS, in.TS)
	}
	out.TS = in.TS
	if out != in {
		t.Errorf("round trip:\n in %+v\nout %+v", in, out)
	}
	// A non-forwarded event carries no tokens object at all.
	b, _ = json.Marshal(ev(in.TS, OutcomeError))
	if strings.Contains(string(b), `"tokens"`) {
		t.Errorf("non-forwarded event serialized tokens: %s", b)
	}
	if err := json.Unmarshal([]byte(`{"ts":"garbage"}`), &out); err == nil {
		t.Error("a bad ts must be an error, not a zero-time event")
	}
}

func TestStore_PersistAndReload(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t: at(2026, 10, 19, 10, 0)}
	s := openTest(t, dir, c)
	s.Record(ev(at(2026, 10, 19, 9, 0), OutcomeOK))
	s.Record(ev(at(2026, 10, 19, 9, 30), OutcomeError))
	s.Close()

	if _, err := os.Stat(filepath.Join(dir, "2026-10-19.jsonl")); err != nil {
		t.Fatalf("today's file missing: %v", err)
	}
	s = openTest(t, dir, c)
	defer s.Close()
	if got := len(s.dayRows("2026-10-19")); got != 2 {
		t.Fatalf("reloaded rows = %d, want 2", got)
	}
}

func TestStore_DayRollCompressesAndLateRowsCount(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t: at(2026, 10, 19, 23, 50)}
	s := openTest(t, dir, c)
	s.Record(ev(at(2026, 10, 19, 23, 49), OutcomeOK))
	// Crosses midnight: arrived on the 19th, completed on the 20th — filed
	// under the 20th, bucketed by its own arrival time.
	c.set(at(2026, 10, 20, 0, 5))
	s.Record(ev(at(2026, 10, 19, 23, 58), OutcomeOK))
	s.Record(ev(at(2026, 10, 20, 0, 4), OutcomeOK))
	s.bg.Wait()

	if _, err := os.Stat(filepath.Join(dir, "2026-10-19.jsonl.zst")); err != nil {
		t.Fatalf("closed day not compressed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-10-19.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("plain closed file left behind: %v", err)
	}
	q, err := ParseQuery(map[string][]string{"from": {"2026-10-19"}, "to": {"2026-10-20"}}, c.now())
	if err != nil {
		t.Fatal(err)
	}
	res := s.Query(q)
	if got := res.Totals[0]; got != int64(2) {
		t.Errorf("requests that arrived on the 19th = %v, want 2 (one filed under the 20th)", got)
	}
	s.Close()
}

func TestStore_TornAndCorruptLinesSkipped(t *testing.T) {
	dir := t.TempDir()
	good, _ := json.Marshal(ev(at(2026, 10, 19, 9, 0), OutcomeOK))
	content := string(good) + "\nnot json\n" + string(good)[:20]
	if err := os.WriteFile(filepath.Join(dir, "2026-10-19.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s := openTest(t, dir, &clock{t: at(2026, 10, 19, 10, 0)})
	defer s.Close()
	if got := len(s.dayRows("2026-10-19")); got != 1 {
		t.Errorf("rows = %d, want 1", got)
	}
}

func TestStore_LeftoverPlainDayCompressedOnOpen(t *testing.T) {
	dir := t.TempDir()
	good, _ := json.Marshal(ev(at(2026, 10, 18, 9, 0), OutcomeOK))
	if err := os.WriteFile(filepath.Join(dir, "2026-10-18.jsonl"), append(good, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	s := openTest(t, dir, &clock{t: at(2026, 10, 19, 10, 0)})
	s.bg.Wait()
	if _, err := os.Stat(filepath.Join(dir, "2026-10-18.jsonl.zst")); err != nil {
		t.Fatalf("leftover not compressed: %v", err)
	}
	if got := len(s.dayRows("2026-10-18")); got != 1 {
		t.Errorf("rows = %d, want 1", got)
	}
	s.Close()
}

// TestStore_OnlineRebuildIsPickedUp: a closed day replaced on disk (what
// `vmr stats rebuild -force` does next to a running process) is re-read on
// the next query without any signal.
func TestStore_OnlineRebuildIsPickedUp(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t: at(2026, 10, 19, 10, 0)}
	s := openTest(t, dir, c)
	defer s.Close()
	day := "2026-10-18"
	if err := WriteDay(dir, day, []Event{ev(at(2026, 10, 18, 9, 0), OutcomeOK)}, c.now()); err != nil {
		t.Fatal(err)
	}
	if got := len(s.dayRows(day)); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}
	evs := []Event{ev(at(2026, 10, 18, 9, 0), OutcomeOK), ev(at(2026, 10, 18, 9, 1), OutcomeOK), ev(at(2026, 10, 18, 9, 2), OutcomeError)}
	if err := WriteDay(dir, day, evs, c.now()); err != nil {
		t.Fatal(err)
	}
	if got := len(s.dayRows(day)); got != 3 {
		t.Errorf("rows after replace = %d, want 3", got)
	}
	if !DayFileExists(dir, day) || DayFileExists(dir, "2026-10-17") {
		t.Error("DayFileExists wrong")
	}
}

func TestWriteDay_Refusals(t *testing.T) {
	dir := t.TempDir()
	now := at(2026, 10, 19, 10, 0)
	if err := WriteDay(dir, "2026-10-19", []Event{ev(now, OutcomeOK)}, now); err == nil {
		t.Error("writing today must be refused")
	}
	if err := WriteDay(dir, "2026-10-18", nil, now); err == nil {
		t.Error("writing an empty day must be refused")
	}
	if err := WriteDay(dir, "18-10-2026", []Event{ev(now, OutcomeOK)}, now); err == nil {
		t.Error("a malformed day must be refused")
	}
}

func TestStore_SecondProcessIsMemoryOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no flock on windows")
	}
	dir := t.TempDir()
	c := &clock{t: at(2026, 10, 19, 10, 0)}
	a := openTest(t, dir, c)
	defer a.Close()
	b := openTest(t, dir, c)
	defer b.Close()
	if a.MemoryOnly() || !b.MemoryOnly() {
		t.Fatalf("memoryOnly a=%v b=%v, want false/true", a.MemoryOnly(), b.MemoryOnly())
	}
	b.Record(ev(c.now(), OutcomeOK))
	if got := len(b.dayRows("2026-10-19")); got != 1 {
		t.Errorf("memory-only store still counts: rows = %d, want 1", got)
	}
	if got := len(a.dayRows("2026-10-19")); got != 0 {
		t.Errorf("memory-only store must not write the shared file: holder sees %d rows", got)
	}
}

func TestStore_ClosedDayCacheIsBounded(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t: at(2026, 10, 19, 10, 0)}
	s := openTest(t, dir, c)
	defer s.Close()
	d := at(2026, 1, 1, 0, 0)
	for i := 0; i < maxCachedDays+10; i++ {
		day := d.AddDate(0, 0, i)
		if err := WriteDay(dir, dayKey(day), []Event{ev(day.Add(time.Hour), OutcomeOK)}, c.now()); err != nil {
			t.Fatal(err)
		}
		s.dayRows(dayKey(day))
	}
	if len(s.cache) > maxCachedDays {
		t.Errorf("cache holds %d days, cap %d", len(s.cache), maxCachedDays)
	}
}

// TestStore_ConcurrentRecordAndQuery exercises the open day's lock-free
// scan (slice-header snapshot, rows never mutated after append) under
// -race: queries run while writers append.
func TestStore_ConcurrentRecordAndQuery(t *testing.T) {
	now := at(2026, 10, 19, 12, 0)
	s := openTest(t, t.TempDir(), &clock{t: now})
	defer s.Close()
	q, err := ParseQuery(map[string][]string{"from": {"-1h"}, "group": {"client"}, "metrics": {"req,ttft_p90"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				s.Record(ev(now.Add(-time.Minute), OutcomeOK))
			}
		}()
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				s.Query(q)
				s.Events(EventsQuery{Limit: 5})
			}
		}()
	}
	wg.Wait()
	if got := s.Query(q).Totals[1]; got != int64(2000) { // [0] is the client column
		t.Errorf("requests = %v, want 2000", got)
	}
}
