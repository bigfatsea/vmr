// Ver 2026-08-20 00:00, by Sonnet 5

package ctxgraph

import (
	"testing"
	"time"
)

func TestCanonicalPath(t *testing.T) {
	cases := map[string]string{
		"/abs/dir/vmr-audit-2026-07-25.jsonl.zst": "vmr-audit-2026-07-25.jsonl",
		"logs/vmr-audit-2026-07-25.jsonl":         "vmr-audit-2026-07-25.jsonl",
		"vmr-audit-2026-07-25.jsonl":              "vmr-audit-2026-07-25.jsonl",
		"../other/vmr-audit-2026-07-25.jsonl.zst": "vmr-audit-2026-07-25.jsonl",
	}
	for in, want := range cases {
		if got := CanonicalPath(in); got != want {
			t.Errorf("CanonicalPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonicalPath_AbsoluteAndRelativeAgree(t *testing.T) {
	abs := "/Volumes/SSD2T/code/vmr/logs/vmr-audit-2026-07-25.jsonl.zst"
	rel := "logs/vmr-audit-2026-07-25.jsonl.zst"
	if CanonicalPath(abs) != CanonicalPath(rel) {
		t.Errorf("CanonicalPath(abs)=%q != CanonicalPath(rel)=%q", CanonicalPath(abs), CanonicalPath(rel))
	}
}

func TestReqCoord(t *testing.T) {
	got := ReqCoord("logs/vmr-audit-2026-07-25.jsonl.zst", 317)
	want := "vmr-audit-2026-07-25.jsonl:317"
	if got != want {
		t.Errorf("ReqCoord = %q, want %q", got, want)
	}
}

func TestBuildManifest_SetsReqFromRawPathAndKeepsPathRaw(t *testing.T) {
	rec := mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("hi")))
	m, ok := BuildManifest(&rec, "logs/vmr-audit-2026-07-25.jsonl.zst", 42)
	if !ok {
		t.Fatal("BuildManifest returned ok=false")
	}
	if m.Path != "logs/vmr-audit-2026-07-25.jsonl.zst" {
		t.Errorf("Manifest.Path = %q, want the raw scan-input path unchanged (BlobIndex.FetchAll opens it as-is)", m.Path)
	}
	if want := "vmr-audit-2026-07-25.jsonl:42"; m.Req != want {
		t.Errorf("Manifest.Req = %q, want %q", m.Req, want)
	}
}

func TestReqHash8_DeterministicAndDistinct(t *testing.T) {
	a := ReqHash8(ReqCoord("vmr-audit-2026-07-25.jsonl", 1))
	b := ReqHash8(ReqCoord("vmr-audit-2026-07-25.jsonl", 1))
	if a != b {
		t.Errorf("ReqHash8 not deterministic: %q vs %q", a, b)
	}
	if len(a) != 8 {
		t.Errorf("ReqHash8 length = %d, want 8", len(a))
	}
	c := ReqHash8(ReqCoord("vmr-audit-2026-07-25.jsonl", 2))
	if a == c {
		t.Errorf("ReqHash8 collided for two different req strings: %q", a)
	}
}

func TestCheckPathCollisions_NoCollision(t *testing.T) {
	paths := []string{
		"/dir1/vmr-audit-2026-07-25.jsonl.zst",
		"/dir1/vmr-audit-2026-07-26.jsonl.zst",
		"relative/vmr-audit-2026-07-27.jsonl",
	}
	if err := CheckPathCollisions(paths); err != nil {
		t.Errorf("unexpected collision error: %v", err)
	}
}

func TestCheckPathCollisions_SamePathTwiceIsNotACollision(t *testing.T) {
	paths := []string{
		"/dir1/vmr-audit-2026-07-25.jsonl.zst",
		"/dir1/vmr-audit-2026-07-25.jsonl.zst",
	}
	if err := CheckPathCollisions(paths); err != nil {
		t.Errorf("unexpected collision error for a literally repeated path: %v", err)
	}
}

func TestCheckPathCollisions_DifferentDirsSameBasename(t *testing.T) {
	paths := []string{
		"/dir1/vmr-audit-2026-07-25.jsonl.zst",
		"/dir2/vmr-audit-2026-07-25.jsonl",
	}
	if err := CheckPathCollisions(paths); err == nil {
		t.Errorf("expected a collision error, got nil")
	}
}
