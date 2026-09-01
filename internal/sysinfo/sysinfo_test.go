package sysinfo

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadMemAlloc(t *testing.T) {
	heapAlloc, sys := ReadMemAlloc()
	if heapAlloc == 0 {
		t.Errorf("ReadMemAlloc() heapAlloc = 0, want > 0")
	}
	if sys == 0 {
		t.Errorf("ReadMemAlloc() sys = 0, want > 0")
	}
}

func TestDirTotalSize(t *testing.T) {
	if got := DirTotalSize(""); got != 0 {
		t.Errorf("DirTotalSize(\"\") = %d, want 0", got)
	}

	if got := DirTotalSize(filepath.Join(t.TempDir(), "nonexistent")); got != 0 {
		t.Errorf("DirTotalSize(nonexistent) = %d, want 0", got)
	}

	tmp := t.TempDir()
	f1 := filepath.Join(tmp, "file1.txt")
	f2 := filepath.Join(tmp, "file2.txt")
	if err := os.WriteFile(f1, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte("world!"), 0600); err != nil {
		t.Fatal(err)
	}

	// Subdirectory with a file - must not be counted (non-recursive)
	subDir := filepath.Join(tmp, "sub")
	if err := os.Mkdir(subDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "file3.txt"), []byte("should not count"), 0600); err != nil {
		t.Fatal(err)
	}

	want := int64(len("hello") + len("world!")) // 5 + 6 = 11
	if got := DirTotalSize(tmp); got != want {
		t.Errorf("DirTotalSize(tmp) = %d, want %d", got, want)
	}
}

func TestDiskFreeSpace(t *testing.T) {
	tmp := t.TempDir()
	free, err := DiskFreeSpace(tmp)
	if err != nil {
		t.Fatalf("DiskFreeSpace(%q) unexpected error: %v", tmp, err)
	}
	if runtime.GOOS != "windows" && free == 0 {
		t.Errorf("DiskFreeSpace(%q) = 0, want > 0 on %s", tmp, runtime.GOOS)
	}
}
