// Ver 2026-07-13 02:00, by Fable 5
package audit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// TestRecordUnmarshalJSON_NormalizesLegacyProtocolNames locks in the one
// backward-compat chokepoint: a pre-2026-08 audit line names its protocol
// "openai"/"anthropic"; every analytics read path decodes into audit.Record
// and must see the current enum instead.
// TODO(2026-10): delete alongside audit.CanonicalProtocol.
func TestRecordUnmarshalJSON_NormalizesLegacyProtocolNames(t *testing.T) {
	const line = `{"ts":"2026-07-01T00:00:00Z","dur_ms":1,"model":"vm","protocol":"openai","outcome":"ok","stream":false,"client":{"request":{}},"attempts":[{"endpoint":"openai:acct:m","protocol":"openai","url":"u","dur_ms":1,"request":{}},{"endpoint":"anthropic/acct/m","protocol":"anthropic","url":"u","dur_ms":1,"request":{}}]}`
	var r Record
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		t.Fatal(err)
	}
	if r.Protocol != "openai-completions" {
		t.Errorf("Record.Protocol = %q, want openai-completions", r.Protocol)
	}
	if r.Attempts[0].Protocol != "openai-completions" || r.Attempts[0].Endpoint != "openai-completions:acct:m" {
		t.Errorf("attempt[0] = %q / %q, want openai-completions", r.Attempts[0].Protocol, r.Attempts[0].Endpoint)
	}
	if r.Attempts[1].Protocol != "anthropic-messages" || r.Attempts[1].Endpoint != "anthropic-messages/acct/m" {
		t.Errorf("attempt[1] = %q / %q, want anthropic-messages (legacy slash separator preserved)", r.Attempts[1].Protocol, r.Attempts[1].Endpoint)
	}

	// A current-enum line round-trips unchanged.
	var r2 Record
	if err := json.Unmarshal([]byte(strings.ReplaceAll(line, `"openai"`, `"openai-completions"`)), &r2); err != nil {
		t.Fatal(err)
	}
	if r2.Protocol != "openai-completions" {
		t.Errorf("current-enum line mangled: %q", r2.Protocol)
	}
}

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

// TestRedactCopiesNonCredentialValueSlices ensures a non-credential header's
// value slice is deep-copied, not shared with the input's backing array —
// mutating the returned Header in place must never leak back into h.
func TestRedactCopiesNonCredentialValueSlices(t *testing.T) {
	h := http.Header{"X-Trace-Id": {"original"}}
	out := Redact(h)
	out["X-Trace-Id"][0] = "mutated"
	if h["X-Trace-Id"][0] != "original" {
		t.Errorf("mutating Redact's output leaked into the input: %q", h["X-Trace-Id"][0])
	}
}

// TestExtraRedactHeadersMasked covers config's extra_redact_headers →
// audit.SetExtraRedactHeaders → Redact/IsCredentialHeader path: a client's
// own custom auth header, unknown to the built-in credentialHeaders list,
// gets masked once configured and stops being masked once cleared (a
// hot-reload dropping the setting must not leave stale state behind).
func TestExtraRedactHeadersMasked(t *testing.T) {
	t.Cleanup(func() { SetExtraRedactHeaders(nil) })

	h := http.Header{}
	h.Set("X-Custom-Token", "supersecretvalue")
	h.Set("Content-Type", "application/json")

	if IsCredentialHeader("X-Custom-Token") {
		t.Fatal("must not be masked before SetExtraRedactHeaders configures it")
	}
	out := Redact(h)
	if got := out.Get("X-Custom-Token"); got != "supersecretvalue" {
		t.Errorf("before configuring: X-Custom-Token = %q, want unmasked", got)
	}

	SetExtraRedactHeaders([]string{"X-Custom-Token"})
	if !IsCredentialHeader("x-custom-token") {
		t.Error("IsCredentialHeader must match case-insensitively")
	}
	out = Redact(h)
	if got := out.Get("X-Custom-Token"); got != "***alue" {
		t.Errorf("X-Custom-Token = %q, want masked", got)
	}
	if got := out.Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type must not be masked: %q", got)
	}

	SetExtraRedactHeaders(nil)
	if IsCredentialHeader("X-Custom-Token") {
		t.Error("must stop being masked once cleared (e.g. a hot reload dropping the setting)")
	}
}

func TestKeyTag(t *testing.T) {
	cases := []struct {
		name, key, want string
	}{
		{"hyphen at window start", "sk-vmr-team-alice", "alice"},
		{"hyphen mid-window, short suffix", "sk-vmr-proj-al", "al"},
		{"hyphen mid-window, four-char suffix", "sk-vmr-teamx-abcd", "abcd"},
		{"suffix longer than window loses hyphen", "sk-vmr-team-abcdefghi", "bcdefghi"},
		{"no hyphen anywhere", "sk-vmr-team9k3f7a", "am9k3f7a"},
		{"hyphen is the window's last char", "sk-vmr-teamabcde-", "amabcde-"},
		{"key shorter than window, hyphen present", "ab-cd", "cd"},
		{"key shorter than window, no hyphen", "abcd", "abcd"},
		{"key exactly window length, no hyphen", "abcdefgh", "abcdefgh"},
		{"multiple hyphens in window: last one wins", "sk-vmr-a-b-cd", "cd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := KeyTag(c.key); got != c.want {
				t.Errorf("KeyTag(%q) = %q, want %q", c.key, got, c.want)
			}
		})
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
	// compresses it to .zst (Layer 2 of the audit log compression design;
	// runs unconditionally, independent of retention).
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

// TestWriteConcurrentGoroutinesProduceValidJSONL is the correctness property
// the sync.Pool-based rewrite of Write actually depends on: each goroutine
// encodes into its own pooled buffer with no cross-goroutine sharing, so N
// concurrent Write calls must produce exactly N complete,
// independently-parseable JSON lines — never a line that's the
// concatenation, truncation, or interleaving of two records. Run with -race
// to also catch any data race on the pooled buffers or the underlying file.
func TestWriteConcurrentGoroutinesProduceValidJSONL(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.hkWG.Wait()

	const n = 200
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Vary body size so short and long encodes race against each
			// other to expose buffer reuse concurrency issues.
			if err := l.Write(&Record{Model: strings.Repeat("m", i%50+1)}); err != nil {
				t.Errorf("Write: %v", err)
			}
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("want %d lines, got %d", n, len(lines))
	}
	for i, line := range lines {
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("line %d not valid JSON: %v (%q)", i, err, line)
		}
	}
}

func TestWriteAfterCloseIsRefusedAndNeverReopens(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	l.hkWG.Wait()
	if err := l.Write(&Record{Model: "m"}); err != nil {
		t.Fatal(err)
	}
	path := l.Path()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	// A handler that outlived the shutdown drain writes late: must be refused
	// (error for the caller to log), must not touch the closed fd, and must
	// not reopen/create any file — even across a simulated midnight boundary.
	l.now = func() time.Time { return time.Now().AddDate(0, 0, 1) }
	if err := l.Write(&Record{Model: "late"}); err == nil {
		t.Error("post-Close Write must return an error, got nil")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("post-Close Write modified the audit file")
	}
	entries, _ := os.ReadDir(dir)
	n := 0
	for _, e := range entries {
		if e.Name() == lockFileName {
			continue // dir's own lock file, not a reopened audit file
		}
		n++
	}
	if n != 1 {
		t.Errorf("post-Close Write created extra files: %d entries", n)
	}
}

// TestWriteBufPool_OversizedBufferNotRecycled ensures that buffers exceeding
// maxPooledWriteBufCap (1MB) after encoding large requests are dropped rather
// than recycled back into writeBufPool to avoid permanent memory retention.
func TestWriteBufPool_OversizedBufferNotRecycled(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.hkWG.Wait()

	// Drain any existing buffers in pool for this goroutine
	drained := make([]*bytes.Buffer, 0, 10)
	for i := 0; i < 10; i++ {
		b := writeBufPool.Get().(*bytes.Buffer)
		drained = append(drained, b)
	}
	for _, b := range drained {
		writeBufPool.Put(b)
	}

	// Write a record with a >1.5MB body
	largePayload := strings.Repeat("a", 1500000)
	rec := &Record{
		Model: "large-test",
		Client: Exchange{
			Request: Message{
				Path: "/v1/chat/completions",
				Body: largePayload,
			},
		},
	}
	if err := l.Write(rec); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Check the buffer returned from the pool: it should never exceed 1MB capacity
	buf := writeBufPool.Get().(*bytes.Buffer)
	defer writeBufPool.Put(buf)
	if buf.Cap() > maxPooledWriteBufCap {
		t.Errorf("writeBufPool contained oversized buffer with cap %d, want <= %d", buf.Cap(), maxPooledWriteBufCap)
	}
}
