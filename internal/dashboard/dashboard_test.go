// Ver 2026-09-06, by Claude
package dashboard

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestWriteSkeletons_BasicShape covers the core contract: dir created when
// missing, exactly the six pages written flat at the root, 0600/0700 modes,
// and non-empty contents.
func TestWriteSkeletons_BasicShape(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reports", "nested") // dir itself doesn't exist yet
	if err := WriteSkeletons(dir); err != nil {
		t.Fatalf("WriteSkeletons: %v", err)
	}

	// Directory mode: 0700 or stricter (umask can only strip bits, never add).
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("dir mode = %v, want no group/other bits (0700 base)", fi.Mode().Perm())
	}

	for _, name := range skeletonPages {
		path := filepath.Join(dir, name)
		fi, err := os.Stat(path)
		if err != nil {
			t.Errorf("missing skeleton %s: %v", name, err)
			continue
		}
		if fi.IsDir() {
			t.Errorf("%s is a directory", name)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s mode = %v, want no group/other bits (0600 base)", name, fi.Mode().Perm())
		}
		if fi.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}

	// And nothing else at the root: flat layout is the #data= path contract.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
	}
	want := map[string]bool{}
	for _, n := range skeletonPages {
		want[n] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("root entries = %v, want exactly %v", got, want)
	}
}

// TestWriteSkeletons_Idempotent runs WriteSkeletons twice and requires the
// second pass to be byte-identical (§6.2: every analyze call refreshes the
// skeletons; a run that appends or corrupts would drift /reports/).
func TestWriteSkeletons_Idempotent(t *testing.T) {
	dir := t.TempDir()
	if err := WriteSkeletons(dir); err != nil {
		t.Fatalf("first WriteSkeletons: %v", err)
	}
	first := map[string][]byte{}
	for _, name := range skeletonPages {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		first[name] = data
	}

	if err := WriteSkeletons(dir); err != nil {
		t.Fatalf("second WriteSkeletons: %v", err)
	}
	for _, name := range skeletonPages {
		second, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(first[name]) != string(second) {
			t.Errorf("%s changed between runs (not idempotent)", name)
		}
	}
}

// TestWriteSkeletons_OverwriteStale pins the "overwrite whatever was there"
// half of the contract: a user-modified or stale page must not survive a
// WriteSkeletons call — /reports/ pages are always the binary's own.
func TestWriteSkeletons_OverwriteStale(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "tool-waste.html")
	if err := os.WriteFile(stale, []byte("<html>user customized</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteSkeletons(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(stale)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "<html>user customized</html>" {
		t.Error("stale user-edited page survived WriteSkeletons — overwrite is required")
	}
}

// TestAssetNames_MatchesSkeletonPages locks the embed FS against the
// hardcoded write list: every .html under assets/ must be in skeletonPages,
// so adding a page without wiring it in fails here instead of silently not
// shipping.
func TestAssetNames_MatchesSkeletonPages(t *testing.T) {
	names, err := AssetNames()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	for _, want := range skeletonPages {
		if !got[want] {
			t.Errorf("skeletonPages lists %q but no assets/%s exists in the embed FS", want, want)
		}
		delete(got, want)
	}
	for extra := range got {
		t.Errorf("assets/%s exists in the embed FS but is missing from skeletonPages — it would never be written", extra)
	}
}

// TestSkeletonPages_NoExternalDependencies guards the zero-build-chain rule
// (§6.3): skeleton pages must reference nothing outside themselves beyond
// the report-root-relative slices — no CDN, no npm, no chart library.
func TestSkeletonPages_NoExternalDependencies(t *testing.T) {
	for _, name := range skeletonPages {
		data, err := assets.ReadFile("assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		s := string(data)
		for _, bad := range []string{"http://", "https://", "src=\"http", "href=\"http", "@import", "require(", "import "} {
			if name == "common.js" {
				continue
			}
			// The SVG favicon data-URI embeds "http://www.w3.org" — the SVG
			// namespace declaration, not a network fetch. Everything else
			// must be clean.
			cleaned := replaceAll(s, "http://www.w3.org/2000/svg", "")
			if contains(cleaned, bad) {
				t.Errorf("%s contains external reference %q — pages must be self-contained", name, bad)
			}
		}
	}
}

func replaceAll(s, old, new string) string {
	out := ""
	for {
		i := indexOf(s, old)
		if i < 0 {
			return out + s
		}
		out += s[:i] + new
		s = s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func contains(s, sub string) bool {
	return indexOf(s, sub) >= 0
}
