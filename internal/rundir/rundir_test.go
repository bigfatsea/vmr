// Ver 2026-07-12 12:00, by Fable 5
package rundir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveEnvOverrideUsedAsIsNoSubdirAppended(t *testing.T) {
	got := resolve("/explicit/path", "/home/u", "x", "/tmp", "vmr_x", "/cwd", "x")
	if got != "/explicit/path" {
		t.Errorf("got %q, want the env value verbatim", got)
	}
}

func TestResolveDefaultsToPersistentHomeDotDir(t *testing.T) {
	got := resolve("", "/home/u", "x", "/tmp", "vmr_x", "/cwd", "x")
	want := filepath.Join("/home/u", ".vmr", "x")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveFallsBackToTempDirSubdirWhenNoHome(t *testing.T) {
	got := resolve("", "", "x", "/tmp", "vmr_x", "/cwd", "x")
	want := filepath.Join("/tmp", "vmr_x")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveFallsBackToCWDWhenTempDirEmpty(t *testing.T) {
	got := resolve("", "", "x", "", "vmr_x", "/cwd", "x")
	want := filepath.Join("/cwd", "x")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveExportedEnvVar(t *testing.T) {
	t.Setenv("RUNDIR_TEST_VAR", "/explicit")
	if got := Resolve("RUNDIR_TEST_VAR", "x", "vmr_x", "x"); got != "/explicit" {
		t.Errorf("got %q", got)
	}
}

func TestResolveExportedDefaultsToHomeDotDir(t *testing.T) {
	t.Setenv("RUNDIR_TEST_VAR", "")
	h, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir in this environment")
	}
	want := filepath.Join(h, ".vmr", "x")
	if got := Resolve("RUNDIR_TEST_VAR", "x", "vmr_x", "x"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
