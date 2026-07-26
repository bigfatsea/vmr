// Ver 2026-07-08 16:20, by Sonnet 5

package audit

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func decompress(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec, err := zstd.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	out, err := io.ReadAll(dec)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be gone, stat err=%v", path, err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist: %v", path, err)
	}
}

func TestHousekeep_CompressesPastDaysOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "vmr-audit-2026-07-06.jsonl", `{"model":"m1"}`+"\n")
	writeFile(t, dir, "vmr-audit-2026-07-08.jsonl", `{"model":"today"}`+"\n") // today: must stay plain

	today := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	housekeep(dir, today)

	mustNotExist(t, filepath.Join(dir, "vmr-audit-2026-07-06.jsonl"))
	mustExist(t, filepath.Join(dir, "vmr-audit-2026-07-06.jsonl.zst"))
	if got := decompress(t, filepath.Join(dir, "vmr-audit-2026-07-06.jsonl.zst")); got != `{"model":"m1"}`+"\n" {
		t.Errorf("round-trip mismatch: %q", got)
	}
	mustExist(t, filepath.Join(dir, "vmr-audit-2026-07-08.jsonl")) // untouched: still today
}

func TestHousekeep_SkipsAlreadyCompressed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "vmr-audit-2026-07-06.jsonl.zst", "not a real zst frame, but that's fine — never read")

	housekeep(dir, time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC))

	// No plain sibling was ever created, and the existing .zst must be
	// left exactly alone (no re-compress attempt, no .tmp litter).
	mustExist(t, filepath.Join(dir, "vmr-audit-2026-07-06.jsonl.zst"))
	mustNotExist(t, filepath.Join(dir, "vmr-audit-2026-07-06.jsonl.zst.tmp"))
}

func TestHousekeep_IgnoresNonAuditFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "vmr.log", "some unrelated service log\n")
	writeFile(t, dir, "notes.txt", "unrelated\n")

	housekeep(dir, time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC))

	mustExist(t, filepath.Join(dir, "vmr.log"))
	mustExist(t, filepath.Join(dir, "notes.txt"))
}

func TestHousekeep_RetentionDisabledByDefault(t *testing.T) {
	SetRetentionDays(0)
	dir := t.TempDir()
	// Far older than any sane retention window, but retention is off.
	writeFile(t, dir, "vmr-audit-2020-01-01.jsonl.zst", "irrelevant bytes")

	housekeep(dir, time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC))

	mustExist(t, filepath.Join(dir, "vmr-audit-2020-01-01.jsonl.zst"))
}

func TestHousekeep_RetentionDeletesOldFiles(t *testing.T) {
	SetRetentionDays(30)
	defer SetRetentionDays(0) // don't leak into other tests

	dir := t.TempDir()
	writeFile(t, dir, "vmr-audit-2026-05-01.jsonl.zst", "too old, compressed") // 68 days before today
	writeFile(t, dir, "vmr-audit-2026-06-01.jsonl", "too old, still plain")    // 37 days before today
	writeFile(t, dir, "vmr-audit-2026-06-20.jsonl.zst", "within retention")    // 18 days before today
	writeFile(t, dir, "vmr-audit-2026-07-08.jsonl", "today")                   // today

	today := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	housekeep(dir, today)

	mustNotExist(t, filepath.Join(dir, "vmr-audit-2026-05-01.jsonl.zst"))
	// The too-old plain file gets compressed *and* immediately purged by the
	// same sweep — end state is "gone", not "compressed and kept".
	mustNotExist(t, filepath.Join(dir, "vmr-audit-2026-06-01.jsonl"))
	mustNotExist(t, filepath.Join(dir, "vmr-audit-2026-06-01.jsonl.zst"))
	mustExist(t, filepath.Join(dir, "vmr-audit-2026-06-20.jsonl.zst"))
	mustExist(t, filepath.Join(dir, "vmr-audit-2026-07-08.jsonl"))
}

func TestHousekeep_ResumesInterruptedCompress(t *testing.T) {
	dir := t.TempDir()
	// Simulates a crash between the rename and the original-file removal in
	// compressOne: both the plain file and its .zst sibling exist.
	writeFile(t, dir, "vmr-audit-2026-07-06.jsonl", "stale original, should be cleaned up")
	writeFile(t, dir, "vmr-audit-2026-07-06.jsonl.zst", "the real compressed data")

	housekeep(dir, time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC))

	mustNotExist(t, filepath.Join(dir, "vmr-audit-2026-07-06.jsonl"))
	mustExist(t, filepath.Join(dir, "vmr-audit-2026-07-06.jsonl.zst"))
	// The .zst content must be untouched — it's the resume path, not a re-compress.
	data, err := os.ReadFile(filepath.Join(dir, "vmr-audit-2026-07-06.jsonl.zst"))
	if err != nil || string(data) != "the real compressed data" {
		t.Errorf(".zst content changed: %q err=%v", data, err)
	}
}

// TestHousekeep_RetentionWithInterruptedCompressResume covers the case
// purgeOne's os.IsNotExist guard exists for: a date past the retention
// cutoff whose plain file is ALSO mid-resume (both it and its .zst exist).
// The plain-file entry's compressOne resume removes the plain file and
// purges the now-current .zst; the SAME date's separate .zst directory
// entry (read before either removal, per housekeep's single os.ReadDir)
// then reaches purgeOne a second time for a file already gone — that
// second removal must be silent, not an ENOENT error on stderr.
func TestHousekeep_RetentionWithInterruptedCompressResume(t *testing.T) {
	SetRetentionDays(30)
	defer SetRetentionDays(0)

	dir := t.TempDir()
	writeFile(t, dir, "vmr-audit-2026-05-01.jsonl", "stale original")
	writeFile(t, dir, "vmr-audit-2026-05-01.jsonl.zst", "compressed, past retention")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	housekeep(dir, time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC))
	w.Close()
	os.Stderr = origStderr
	stderr, _ := io.ReadAll(r)

	mustNotExist(t, filepath.Join(dir, "vmr-audit-2026-05-01.jsonl"))
	mustNotExist(t, filepath.Join(dir, "vmr-audit-2026-05-01.jsonl.zst"))
	if strings.Contains(string(stderr), "no such file") {
		t.Errorf("the second, redundant purge of an already-removed file must be silent, got stderr: %s", stderr)
	}
	if !strings.Contains(string(stderr), "removed vmr-audit-2026-05-01.jsonl.zst") {
		t.Errorf("expected exactly one successful removal log line, got stderr: %s", stderr)
	}
}

func TestCompressFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.jsonl")
	dst := filepath.Join(dir, "out.zst")
	content := `{"a":1}` + "\n" + `{"b":2}` + "\n"
	writeFile(t, dir, "in.jsonl", content)

	if err := compressFile(src, dst); err != nil {
		t.Fatal(err)
	}
	if got := decompress(t, dst); got != content {
		t.Errorf("round-trip mismatch: got %q want %q", got, content)
	}
}
