// Ver 2026-07-10 00:00, by Sonnet 5
package rundir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveEnvOverrideUsedAsIsNoSubdirAppended(t *testing.T) {
	got := resolve("/explicit/path", "/tmp", "vmr_x", "/cwd", "x")
	if got != "/explicit/path" {
		t.Errorf("got %q, want the env value verbatim", got)
	}
}

func TestResolveFallsBackToTempDirSubdir(t *testing.T) {
	got := resolve("", "/tmp", "vmr_x", "/cwd", "x")
	want := filepath.Join("/tmp", "vmr_x")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveFallsBackToCWDWhenTempDirEmpty(t *testing.T) {
	got := resolve("", "", "vmr_x", "/cwd", "x")
	want := filepath.Join("/cwd", "x")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveExportedEnvVar(t *testing.T) {
	t.Setenv("RUNDIR_TEST_VAR", "/explicit")
	if got := Resolve("RUNDIR_TEST_VAR", "vmr_x", "x"); got != "/explicit" {
		t.Errorf("got %q", got)
	}
}

func TestResolveExportedDefaultsToTempSubdir(t *testing.T) {
	t.Setenv("RUNDIR_TEST_VAR", "")
	want := filepath.Join(os.TempDir(), "vmr_x")
	if got := Resolve("RUNDIR_TEST_VAR", "vmr_x", "x"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
