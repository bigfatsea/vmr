// Ver 2026-07-13 19:00, by Sonnet 5
package audit

import (
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
