// Ver 2026-07-13 02:00, by Fable 5
package rundir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDefaultsToPersistentHomeDotDir(t *testing.T) {
	got := resolve("/home/u", "x", "/tmp", "vmr_x", "/cwd", "x")
	want := filepath.Join("/home/u", ".vmr", "x")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveFallsBackToTempDirSubdirWhenNoHome(t *testing.T) {
	got := resolve("", "x", "/tmp", "vmr_x", "/cwd", "x")
	want := filepath.Join("/tmp", "vmr_x")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveFallsBackToCWDWhenTempDirEmpty(t *testing.T) {
	got := resolve("", "x", "", "vmr_x", "/cwd", "x")
	want := filepath.Join("/cwd", "x")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveExported(t *testing.T) {
	h, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir in this environment")
	}
	want := filepath.Join(h, ".vmr", "x")
	if got := Resolve("x", "vmr_x", "x"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
