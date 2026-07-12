// Ver 2026-07-08 16:20, by Sonnet 5
package audit

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func TestRedactMasksCredentials(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer sk-secret-key-abcd")
	h.Set("x-api-key", "sk-another-secret-wxyz")
	h.Set("Content-Type", "application/json")
	out := Redact(h)
	if got := out.Get("Authorization"); got != "Bearer ***abcd" {
		t.Errorf("authorization: %q", got)
	}
	if got := out.Get("X-Api-Key"); got != "***wxyz" {
		t.Errorf("x-api-key: %q", got)
	}
	if got := out.Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type must not be masked: %q", got)
	}
	// Original untouched.
	if h.Get("Authorization") != "Bearer sk-secret-key-abcd" {
		t.Error("Redact must not mutate the input")
	}
}

func TestEncodeBody(t *testing.T) {
	if b := EncodeBody([]byte(`{"a":1}`)); string(b.(json.RawMessage)) != `{"a":1}` {
		t.Errorf("json body: %v", b)
	}
	if b := EncodeBody([]byte("data: hello\n\n")); b.(string) != "data: hello\n\n" {
		t.Errorf("sse body: %v", b)
	}
	if b := EncodeBody(nil); b != nil {
		t.Errorf("empty body: %v", b)
	}
	big := strings.Repeat("x", 10<<20)
	b := EncodeBody([]byte(big))
	if len(b.(string)) != len(big) {
		t.Errorf("large body must be recorded in full, unmodified: got len=%d want %d", len(b.(string)), len(big))
	}
}

func TestDailyRotation(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.hkWG.Wait() // let New's startup sweep (nothing to do on an empty dir) finish first

	day1 := time.Date(2026, 7, 7, 23, 59, 0, 0, time.Local)
	l.now = func() time.Time { return day1 }
	if err := l.Write(&Record{TS: day1, Model: "m1"}); err != nil {
		t.Fatal(err)
	}
	// This first Write is itself a "rotation" (l.f was nil) and schedules its
	// own (no-op, since day1's file is "today" by day1's own clock) sweep.
	// Draining it here — rather than only once at the end — avoids racing
	// scheduleHousekeeping's overlap guard against the day2 sweep below: two
	// back-to-back Write calls in a tight test loop can otherwise hit the
	// CAS-skip path and silently drop the day2 sweep that's actually under test.
	l.hkWG.Wait()
	day2 := day1.Add(2 * time.Minute)
	l.now = func() time.Time { return day2 }
	if err := l.Write(&Record{TS: day2, Model: "m2"}); err != nil {
		t.Fatal(err)
	}
	l.hkWG.Wait() // rotating into day2 triggers a housekeeping sweep of day1's now-closed file

	// day1 is no longer "today" as of the day2 write: the rotation sweep
	// compresses it to .zst (Layer 2 of the audit log compression design —
	// docs/AuditLogCompression_Analysis_Sonnet5.md — runs unconditionally,
	// independent of retention).
	if _, err := os.Stat(filepath.Join(dir, "vmr-audit-2026-07-07.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("day1 plain file should have been compressed away, stat err=%v", err)
	}
	f, err := os.Open(filepath.Join(dir, "vmr-audit-2026-07-07.jsonl.zst"))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := zstd.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	day1Data, err := io.ReadAll(dec)
	dec.Close()
	f.Close()
	if err != nil {
		t.Fatal(err)
	}

	// day2 is still "today": it must stay plain and untouched.
	day2Data, err := os.ReadFile(filepath.Join(dir, "vmr-audit-2026-07-08.jsonl"))
	if err != nil {
		t.Fatalf("2026-07-08: %v", err)
	}

	for date, dm := range map[string]struct {
		data  []byte
		model string
	}{
		"2026-07-07": {day1Data, "m1"},
		"2026-07-08": {day2Data, "m2"},
	} {
		var rec Record
		if err := json.Unmarshal(dm.data, &rec); err != nil || rec.Model != dm.model {
			t.Errorf("%s: %v model=%s", date, err, rec.Model)
		}
	}
}

func TestNilLoggerNoop(t *testing.T) {
	var l *Logger
	if err := l.Write(&Record{}); err != nil {
		t.Error(err)
	}
	if err := l.Close(); err != nil {
		t.Error(err)
	}
}

func TestDirEnv(t *testing.T) {
	t.Setenv("VMR_LOG_DIR", "/some/dir")
	if Dir() != "/some/dir" {
		t.Errorf("env dir: %s", Dir())
	}
	t.Setenv("VMR_LOG_DIR", "")
	want := filepath.Join(os.TempDir(), "vmr_logs")
	if Dir() != want {
		t.Errorf("default dir: got %s, want %s", Dir(), want)
	}
}
