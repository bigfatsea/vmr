// Ver 2026-08-13, by Opus 5
package report

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPctStr64_ZeroOrNegativeDenominator(t *testing.T) {
	if got := pctStr64(5, 0); got != "-" {
		t.Errorf("pctStr64(5, 0) = %q, want \"-\"", got)
	}
	if got := pctStr64(5, -1); got != "-" {
		t.Errorf("pctStr64(5, -1) = %q, want \"-\"", got)
	}
}

func TestPctStr64_NormalValue(t *testing.T) {
	if got := pctStr64(1, 4); got != "25.0%" {
		t.Errorf("pctStr64(1, 4) = %q, want \"25.0%%\"", got)
	}
}

// TestPctStr64_BeyondInt32Range is the reason this int64 twin of pctStr2
// exists at all — a value like this would silently wrap if narrowed to int
// on a 32-bit build, which pctStr2's call sites never had to worry about
// but a raw token count crossing 2^31 legitimately can.
func TestPctStr64_BeyondInt32Range(t *testing.T) {
	const beyond32Bit = int64(1) << 33 // 8,589,934,592 — well past math.MaxInt32
	if got := pctStr64(beyond32Bit, beyond32Bit*2); got != "50.0%" {
		t.Errorf("pctStr64 with a beyond-int32 numerator/denominator = %q, want \"50.0%%\"", got)
	}
}

// TestDetailCell_LinksOnlyWhenFileActuallyExists: the "文件" column's
// judgment is whether the
// target actually exists on disk right now, not whether some flag was
// passed this run — since vmr analyze can have one half (story, batch
// materializing under -render-all) write details/ while the report half's
// own -details flag stays false, or vice versa. detailCell itself takes
// the pre-built set (buildDetailFileSet, P13.4's F-02 follow-up) rather
// than doing its own os.Stat — this test exercises detailCell directly, so
// it builds that set by hand instead of round-tripping through
// buildDetailFileSet.
func TestDetailCell_LinksOnlyWhenFileActuallyExists(t *testing.T) {
	dir := t.TempDir()
	r := RequestRow{DetailFile: "2026-08-21T09-00-00_agent_x_ok_abcd1234.md", Req: "audit.jsonl:1"}

	if got := detailCell(r, nil); got != "`audit.jsonl:1`" {
		t.Errorf("before the file exists, want the req coordinate fallback, got %q", got)
	}

	if err := os.WriteFile(filepath.Join(dir, r.DetailFile), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := "[" + r.Req + "](details/" + r.DetailFile + ")"
	if got := detailCell(r, map[string]struct{}{r.DetailFile: {}}); got != want {
		t.Errorf("once the file exists, want a link (%q), got %q", want, got)
	}
}

// TestDetailCell_NoDetailFileFallsBackToDash covers the r.DetailFile == ""
// edge (no coordinate to fall back to either) regardless of detailSet.
func TestDetailCell_NoDetailFileFallsBackToDash(t *testing.T) {
	if got := detailCell(RequestRow{}, nil); got != "-" {
		t.Errorf("detailCell with no DetailFile/Req = %q, want \"-\"", got)
	}
}

// TestBuildDetailFileSet covers the os.ReadDir-based set construction
// itself (F-02/F-04 from the independent review of this phase's
// ActionPlan): a missing directory yields an empty set, not an error or a
// panic; hidden files and non-.md entries never leak into the set as if
// they were real detail pages.
func TestBuildDetailFileSet(t *testing.T) {
	if got := buildDetailFileSet(filepath.Join(t.TempDir(), "does-not-exist")); len(got) != 0 {
		t.Errorf("missing directory: want an empty set, got %v", got)
	}

	dir := t.TempDir()
	for _, name := range []string{"real.md", ".DS_Store", ".reqdetail-tmp123.md", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	got := buildDetailFileSet(dir)
	if _, ok := got["real.md"]; !ok || len(got) != 1 {
		t.Errorf("want exactly {\"real.md\"}, got %v", got)
	}
}
