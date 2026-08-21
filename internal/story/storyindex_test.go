// Ver 2026-08-05, by Sonnet 5

package story

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
)

func twoStepChain(t *testing.T) []*ctxgraph.Lineage {
	t.Helper()
	at := func(m int) time.Time { return time.Date(2026, 7, 16, 10, m, 0, 0, time.UTC) }
	r1 := mkRec(at(0), "", []any{msg("system", "sys"), msg("user", "do the thing")}, sseText("ok"))
	r2 := mkRec(at(1), "", []any{msg("system", "sys"), msg("user", "do the thing"), msg("assistant", "ok"), msg("user", "more")}, sseText("done"))
	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	return []*ctxgraph.Lineage{l}
}

func TestBuildJourneyIndexRow_CheapFields(t *testing.T) {
	chain := twoStepChain(t)
	row := BuildJourneyIndexRow(chain, "some title", false)

	if row.ID != ID(chain) {
		t.Errorf("ID = %q, want %q", row.ID, ID(chain))
	}
	if row.Requests != 2 {
		t.Errorf("Requests = %d, want 2 (one per manifest)", row.Requests)
	}
	if row.Title != "some title" {
		t.Errorf("Title = %q, want %q", row.Title, "some title")
	}
	if row.Partial {
		t.Error("Partial should be false")
	}
	if row.Stitched != 1 {
		t.Errorf("Stitched = %d, want 1 (single-lineage chain)", row.Stitched)
	}
	if len(row.Files) != 1 {
		t.Fatalf("Files = %v, want exactly 1 (both manifests share the same source file)", row.Files)
	}
	if want := ctxgraph.CanonicalPath(chain[0].Manifests[0].Path); row.Files[0] != want {
		t.Errorf("Files[0] = %q, want %q (CanonicalPath, matching req's coordinate spelling)", row.Files[0], want)
	}
	// Not built yet — the caller (renderJourney etc.) fills these in only
	// once story.BuildChain has actually run.
	if row.Tasks != 0 || row.Steps != 0 || row.Rendered != "" {
		t.Errorf("expected zero-value Tasks/Steps/Rendered before a full build, got %+v", row)
	}
}

func TestMergeJourneyIndexRows_CarriesForwardBuiltFields(t *testing.T) {
	fresh := []JourneyIndexRow{
		{ID: "j-a", Requests: 2, Title: "fresh title A"},
		{ID: "j-b", Requests: 1, Title: "fresh title B"},
	}
	prior := []JourneyIndexRow{
		{ID: "j-a", Requests: 2, Title: "stale title A", Tasks: 3, Steps: 7, Rendered: "journey-j-a.md"},
		{ID: "j-gone", Requests: 5, Title: "no longer derivable from current files"},
	}
	merged := MergeJourneyIndexRows(fresh, prior)

	if len(merged) != 2 {
		t.Fatalf("got %d rows, want 2 (exactly fresh's count — j-gone must be dropped)", len(merged))
	}
	var a, b *JourneyIndexRow
	for i := range merged {
		switch merged[i].ID {
		case "j-a":
			a = &merged[i]
		case "j-b":
			b = &merged[i]
		}
	}
	if a == nil || b == nil {
		t.Fatalf("expected rows j-a and j-b, got %+v", merged)
	}
	if a.Title != "fresh title A" {
		t.Errorf("a.Title = %q, want fresh's title (fresh always wins for cheap fields)", a.Title)
	}
	if a.Tasks != 3 || a.Steps != 7 || a.Rendered != "journey-j-a.md" {
		t.Errorf("a should carry forward prior's built fields, got Tasks=%d Steps=%d Rendered=%q", a.Tasks, a.Steps, a.Rendered)
	}
	if b.Tasks != 0 || b.Steps != 0 || b.Rendered != "" {
		t.Errorf("b has no prior entry, should stay zero-valued, got %+v", b)
	}
}

func TestMergeJourneyIndexRows_FreshBuiltFieldsWinOverPrior(t *testing.T) {
	fresh := []JourneyIndexRow{
		{ID: "j-a", Requests: 2, Tasks: 4, Steps: 9, Rendered: "journey-j-a.md"},
	}
	prior := []JourneyIndexRow{
		{ID: "j-a", Requests: 2, Tasks: 3, Steps: 7, Rendered: "journey-j-a-partial.md"},
	}
	merged := MergeJourneyIndexRows(fresh, prior)
	if len(merged) != 1 {
		t.Fatalf("got %d rows, want 1", len(merged))
	}
	if merged[0].Tasks != 4 || merged[0].Steps != 9 || merged[0].Rendered != "journey-j-a.md" {
		t.Errorf("this run's own freshly built fields should win, got %+v", merged[0])
	}
}

// TestStoryIndex_SaveLoadRoundTrip covers Save/LoadStoryIndex's remaining
// job — Journeys only; the parse cache used to round-trip through this
// same file (a "files" section) but has since moved to its own
// content-hash-sharded directory (see ctxgraph's own
// TestSaveCacheDir_LoadCacheDir_RoundTrip) and Cache's json:"-" tag.
func TestStoryIndex_SaveLoadRoundTrip(t *testing.T) {
	chain := twoStepChain(t)
	idx := &StoryIndex{
		Cache:    &ctxgraph.FileCache{Files: map[string]ctxgraph.CachedFile{"x": {Hash: "deadbeef"}}},
		Journeys: []JourneyIndexRow{BuildJourneyIndexRow(chain, "t", false)},
	}
	path := filepath.Join(t.TempDir(), "vmr-stories.json")
	if err := idx.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "deadbeef") {
		t.Error("vmr-stories.json should not embed Cache's content (json:\"-\")")
	}
	got := LoadStoryIndex(path)
	if len(got.Journeys) != 1 || got.Journeys[0].ID != idx.Journeys[0].ID {
		t.Fatalf("round-tripped Journeys = %+v, want %+v", got.Journeys, idx.Journeys)
	}
	if got.Cache != nil {
		t.Errorf("LoadStoryIndex should leave Cache nil (load it separately via ctxgraph.LoadCacheDir), got %+v", got.Cache)
	}
}

func TestLoadStoryIndex_MissingFileReturnsEmpty(t *testing.T) {
	idx := LoadStoryIndex(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if idx == nil || len(idx.Journeys) != 0 {
		t.Errorf("expected an empty, non-nil index for a missing file, got %+v", idx)
	}
}

func TestLoadStoryIndex_CorruptFileDegradesToEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vmr-stories.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	idx := LoadStoryIndex(path)
	if idx == nil || len(idx.Journeys) != 0 {
		t.Errorf("expected a corrupt file to degrade to an empty index, got %+v", idx)
	}
}

func TestRenderStoryIndexMarkdown_EmptyAndPopulated(t *testing.T) {
	empty := RenderStoryIndexMarkdown(nil, i18n.EN)
	if empty == "" {
		t.Error("empty render should still produce a title/note, not an empty string")
	}

	chain := twoStepChain(t)
	rows := []JourneyIndexRow{BuildJourneyIndexRow(chain, "调研一下", false)}
	rows[0].Tasks, rows[0].Steps, rows[0].Rendered = 1, 2, "journey-"+rows[0].ID+".md"
	md := RenderStoryIndexMarkdown(rows, i18n.EN)
	for _, want := range []string{rows[0].ID, "调研一下", rows[0].Rendered} {
		if !strings.Contains(md, want) {
			t.Errorf("rendered markdown missing %q:\n%s", want, md)
		}
	}
}

// TestRenderStoryIndexMarkdown_EscapesTitle locks in a fix beyond
// KNOWN_ISSUES §1.37's original scope, found during P12's independent
// review: a Journey title containing a literal "|" written straight into
// this table's cells doesn't just lose content the way an unescaped
// "<!--" does — it splits into extra columns and corrupts that row (and
// visually, everything after it) in vmr-stories.md, the primary
// navigation surface. A real task instruction quoting a shell pipe
// ("ps aux | grep vmr") is a completely ordinary way to trigger this.
func TestRenderStoryIndexMarkdown_EscapesTitle(t *testing.T) {
	row := JourneyIndexRow{ID: "l-deadbeef", Title: "ps aux | grep vmr <!-- keywords -->", Requests: 1}
	md := RenderStoryIndexMarkdown([]JourneyIndexRow{row}, i18n.EN)

	lines := strings.Split(md, "\n")
	var rowLine string
	for _, l := range lines {
		if strings.Contains(l, row.ID) {
			rowLine = l
			break
		}
	}
	if rowLine == "" {
		t.Fatalf("rendered markdown missing the row for %s:\n%s", row.ID, md)
	}
	// writeStoryIndexRow's own format string is 7 columns: id, client,
	// window, tasks, steps, title, rendered — that's 8 unescaped "|"
	// separators. The title's own "|" must survive escaped ("\|", still a
	// literal "|" character but no longer a column delimiter to a GFM
	// table parser) rather than as a 9th bare delimiter.
	if !strings.Contains(rowLine, `aux \| grep`) {
		t.Errorf("row = %q, want the title's literal \"|\" escaped as \"\\|\", not left as a bare column delimiter", rowLine)
	}
	const wantUnescapedColumnSeparators = 8
	unescaped := strings.ReplaceAll(rowLine, `\|`, "")
	if n := strings.Count(unescaped, "|"); n != wantUnescapedColumnSeparators {
		t.Errorf("row (with escaped pipes removed) = %q has %d unescaped \"|\" characters, want exactly %d", unescaped, n, wantUnescapedColumnSeparators)
	}
	if strings.Contains(rowLine, "<!--") {
		t.Errorf("row = %q, want the HTML comment marker escaped, not raw", rowLine)
	}
	if !strings.Contains(rowLine, "&lt;!--") {
		t.Errorf("row = %q, want the escaped form present", rowLine)
	}
}

func TestJourneyIndexRow_JSONRoundTrip(t *testing.T) {
	chain := twoStepChain(t)
	row := BuildJourneyIndexRow(chain, "t", false)
	row.Tasks, row.Steps, row.Rendered = 2, 3, "journey-x.md"
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got JourneyIndexRow
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !got.Start.Equal(row.Start) || !got.End.Equal(row.End) {
		t.Errorf("Start/End didn't round-trip: got %v/%v, want %v/%v", got.Start, got.End, row.Start, row.End)
	}
	if got.ID != row.ID || got.Tasks != row.Tasks || got.Rendered != row.Rendered {
		t.Errorf("round-tripped row = %+v, want %+v", got, row)
	}
}

func TestSourceFiles(t *testing.T) {
	if got := SourceFiles(nil, "j-1"); got != nil {
		t.Errorf("SourceFiles(nil) = %v, want nil", got)
	}

	idx := &StoryIndex{
		Journeys: []JourneyIndexRow{
			{ID: "j-1", Files: []string{"b.jsonl", "a.jsonl"}},
			{ID: "j-2", Files: []string{"b.jsonl", "c.jsonl"}},
			{ID: "j-3", Files: []string{"d.jsonl"}},
		},
	}

	got := SourceFiles(idx, "j-1", "j-2")
	want := []string{"a.jsonl", "b.jsonl", "c.jsonl"}
	if len(got) != len(want) {
		t.Fatalf("SourceFiles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SourceFiles[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Missing ID should contribute nothing
	gotMissing := SourceFiles(idx, "j-nonexistent")
	if len(gotMissing) != 0 {
		t.Errorf("SourceFiles(missing) = %v, want empty slice", gotMissing)
	}
}
