// Ver 2026-07-13 19:00, by Sonnet 5
package audit

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestForEachLineSkipsOversizedLines(t *testing.T) {
	input := "short1\n" + strings.Repeat("x", 100) + "\nshort2\n"
	var got []string
	skipped := 0
	err := ForEachLine(strings.NewReader(input), 32, func(line []byte) {
		got = append(got, string(line))
	}, func() { skipped++ })
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if len(got) != 2 || got[0] != "short1" || got[1] != "short2" {
		t.Errorf("lines = %q, want [short1 short2]", got)
	}
}

func TestForEachLineHandlesFinalLineWithoutNewline(t *testing.T) {
	var got []string
	err := ForEachLine(strings.NewReader("a\nb"), 32, func(line []byte) {
		got = append(got, string(line))
	}, nil)
	if err != nil || len(got) != 2 || got[1] != "b" {
		t.Errorf("got %q err=%v, want [a b]", got, err)
	}
}

func TestLineAt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	if err := os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LineAt(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "line2" {
		t.Errorf("LineAt(2) = %q, want %q", got, "line2")
	}
}

func TestLineAt_OutOfRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	if err := os.WriteFile(path, []byte("line1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LineAt(path, 5); err == nil {
		t.Error("expected an error for a line past EOF, got nil")
	}
}

func TestLineAt_RejectsNonPositiveLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	if err := os.WriteFile(path, []byte("line1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{0, -1} {
		if _, err := LineAt(path, n); err == nil {
			t.Errorf("LineAt(%d): expected an error, got nil", n)
		}
	}
}

// TestScanLines_StopsEarly is LineAt's core invariant, tested at the
// scanLines level where it can be proven directly: once fn returns false,
// the underlying reader must not be asked for any more bytes than it
// already buffered opportunistically. A regression here would silently
// undo the whole point of LineAt no longer scanning past its target line —
// see LineAt's doc comment for the measured real-file effect.
func TestScanLines_StopsEarly(t *testing.T) {
	// A short first line, then a "line" far bigger than bufio's 1<<20
	// internal buffer, so satisfying it would require multiple additional
	// Reads if scanning continued past line 1.
	rest := strings.Repeat("x", 4<<20)
	data := []byte("line1\n" + rest + "\n")
	r := &countingReader{data: data}

	var got []string
	err := scanLines(r, MaxLogLine, func(line []byte) bool {
		got = append(got, string(line))
		return false // stop after the first line
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "line1" {
		t.Fatalf("got %q, want [line1]", got)
	}
	// bufio's own read-ahead means some of the 4MB filler may already sit
	// in its buffer from the single Read that produced line1 — but nowhere
	// near the full ~4MB is required, so a generous fraction of the input
	// still proves the scan didn't continue reading to satisfy line 2.
	if r.pos >= len(data)/2 {
		t.Errorf("reader consumed %d of %d bytes — scan did not stop early", r.pos, len(data))
	}
}

// countingReader is a minimal io.Reader that tracks how many bytes of data
// have actually been handed out, so a test can assert a scan stopped
// without over-specifying exact Read call counts/sizes (an implementation
// detail of bufio.Reader, not of scanLines' early-exit contract).
type countingReader struct {
	data []byte
	pos  int
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// TestScanLines_OnSkipAdvancesCaller'sOwnCounter documents and pins the
// contract LineAt's onSkip closure relies on: a caller that counts lines
// itself (n++ in both fn and onSkip, exactly what LineAt does) must see a
// too-long skipped line advance that count exactly like a normal one, or a
// target line coming after it would be misidentified. This is a regression
// test for a real bug: LineAt used to pass onSkip=nil, so its own count
// silently fell behind by one per skipped line (dormant today only because
// no real record has ever approached MaxLogLine's 128MB).
func TestScanLines_OnSkipAdvancesCallersOwnCounter(t *testing.T) {
	data := "aa\n" + strings.Repeat("x", 20) + "\nbb\n" // line 2 exceeds maxLine below
	const maxLine = 5

	var n int
	var found string
	err := scanLines(strings.NewReader(data), maxLine, func(line []byte) bool {
		n++
		if n == 3 {
			found = string(line)
			return false
		}
		return true
	}, func() { n++ }) // mirrors LineAt's own onSkip closure
	if err != nil {
		t.Fatal(err)
	}
	if found != "bb" {
		t.Errorf("3rd line (skipped 2nd one counted) = %q, want %q", found, "bb")
	}
}

func TestOpenLogFile_Plain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vmr-audit-2026-07-07.jsonl")
	if err := os.WriteFile(path, []byte("line1\nline2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, err := OpenLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	var lines []string
	if err := ForEachLine(rc, MaxLogLine, func(l []byte) { lines = append(lines, string(l)) }, nil); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "line1" || lines[1] != "line2" {
		t.Errorf("lines = %q", lines)
	}
}

func TestOpenLogFile_Zst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vmr-audit-2026-07-07.jsonl.zst")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Write([]byte("line1\nline2\n")); err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	rc, err := OpenLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	var lines []string
	if err := ForEachLine(rc, MaxLogLine, func(l []byte) { lines = append(lines, string(l)) }, nil); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "line1" || lines[1] != "line2" {
		t.Errorf("lines = %q", lines)
	}
}

func TestOpenLogFile_RejectsGarbageZst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vmr-audit-2026-07-07.jsonl.zst")
	if err := os.WriteFile(path, []byte("not a zstd frame"), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, err := OpenLogFile(path)
	if err != nil {
		// zstd.NewReader validates the frame header eagerly on some inputs.
		return
	}
	defer rc.Close()
	// Otherwise the error only surfaces once the garbage is actually decoded.
	if err := ForEachLine(rc, MaxLogLine, func([]byte) {}, nil); err == nil {
		t.Error("expected an error reading a non-zstd .zst file, got nil")
	}
}
