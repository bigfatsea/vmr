// Ver 2026-08-05, by Sonnet 5

// vmr-stories.json/.md: a file-hash-keyed parse cache (see
// ctxgraph.FileCache/ScanCached) that doubles as the candidate-Journey
// listing `vmr story` used to only ever print to stdout. One document, not
// a cache file plus a separate index file — both need the exact same
// underlying fact (which files does Journey X depend on), and the JSON was
// already the machine-data layer before this (vmr-stories.md is the human
// one, same split vmr-requests.json/.md already established) — see
// docs/VirtualModelRouter_Design_v4_Analytics.md's vmr-stories.json section
// for the full reasoning (including why this stops at file-level caching
// rather than a narrower, per-Journey file selection).
package story

import (
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// JourneyIndexRow is one candidate Journey's row. Requests/Client/Start/End/
// Title/Partial/Stitched/Files are cheap — derivable from the chain alone,
// recomputed on every run. Tasks/Steps/Rendered are only known once the
// full story.Journey has actually been built at least once (-journey/
// -render-all/-compare/-corpus, never the bare listing pass, which
// deliberately stays cheap — see PreviewTitles) — a row with Requests > 0
// but Tasks == 0 simply hasn't been built yet, not an empty Journey.
type JourneyIndexRow struct {
	ID       string    `json:"id"`
	Client   string    `json:"client,omitempty"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Requests int       `json:"requests"`
	Tasks    int       `json:"tasks,omitempty"`
	Steps    int       `json:"steps,omitempty"`
	Title    string    `json:"title,omitempty"`
	Partial  bool      `json:"partial,omitempty"`
	Stitched int       `json:"stitched,omitempty"` // len(chain), only when >1
	Files    []string  `json:"files"`
	Rendered string    `json:"rendered,omitempty"` // journey-<id>(-partial).md path, once rendered
}

// StoryIndex is vmr-stories.json's whole shape.
type StoryIndex struct {
	Files    ctxgraph.FileCache `json:"files"`
	Journeys []JourneyIndexRow  `json:"journeys"`
}

// LoadStoryIndex reads path if present. A missing, unreadable, or corrupt
// file all degrade the same way — an empty index (no cache hits, no prior
// Journey rows) — the same best-effort-cache contract internal/imgprep's
// disk cache uses: a bad cache must never fail or corrupt the actual run,
// only cost it the speedup and the Tasks/Rendered carry-forward.
func LoadStoryIndex(path string) *StoryIndex {
	empty := &StoryIndex{Files: ctxgraph.FileCache{Files: map[string]ctxgraph.CachedFile{}}}
	data, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	var idx StoryIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return empty
	}
	if idx.Files.Files == nil {
		idx.Files.Files = map[string]ctxgraph.CachedFile{}
	}
	return &idx
}

// Save writes idx to path. 0600: same sensitivity note as every other file
// under reports/stories/ — it's derived straight from the message-hash
// content of the conversations it indexes.
func (idx *StoryIndex) Save(path string) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// BuildJourneyIndexRow derives one candidate's cheap fields from its
// resolved chain and precomputed title — everything listJourneys already
// has on hand, so this never re-reads anything. rendered ("" if this
// invocation didn't render it) and tasks/steps (0 if the full Journey
// wasn't built this run) are for the caller to fill in when it has them;
// MergeJourneyIndexRows carries forward whatever a prior run already
// recorded when this call leaves them zero/empty.
func BuildJourneyIndexRow(chain []*ctxgraph.Lineage, title string, partial bool) JourneyIndexRow {
	head, tail := chain[0], chain[len(chain)-1]
	first, last := head.Manifests[0], tail.Manifests[len(tail.Manifests)-1]
	requests := 0
	fileSet := map[string]bool{}
	for _, l := range chain {
		requests += len(l.Manifests)
		for _, m := range l.Manifests {
			fileSet[m.Path] = true
		}
	}
	files := make([]string, 0, len(fileSet))
	for f := range fileSet {
		files = append(files, f)
	}
	sort.Strings(files)
	return JourneyIndexRow{
		ID:       ID(chain),
		Client:   first.ClientKeyTag,
		Start:    first.TS,
		End:      last.TS,
		Requests: requests,
		Title:    title,
		Partial:  partial,
		Stitched: len(chain),
		Files:    files,
	}
}

// MergeJourneyIndexRows combines this run's freshly derived rows (fresh —
// always the complete, authoritative set for whatever files this run
// loaded) with a prior index's rows: same ID keeps prior's Tasks/Steps/
// Rendered (still valid — a Journey's id is content-addressed, so an
// unchanged id means an unchanged Journey) whenever fresh didn't just
// (re)compute them itself; an id present only in prior (not reconstructible
// from this run's file set) is dropped — the index reflects what THIS run's
// input files can prove, same as the Graph itself would.
func MergeJourneyIndexRows(fresh []JourneyIndexRow, prior []JourneyIndexRow) []JourneyIndexRow {
	priorByID := make(map[string]JourneyIndexRow, len(prior))
	for _, r := range prior {
		priorByID[r.ID] = r
	}
	out := make([]JourneyIndexRow, len(fresh))
	for i, r := range fresh {
		if p, ok := priorByID[r.ID]; ok {
			if r.Tasks == 0 && r.Steps == 0 {
				r.Tasks, r.Steps = p.Tasks, p.Steps
			}
			if r.Rendered == "" {
				r.Rendered = p.Rendered
			}
		}
		out[i] = r
	}
	return out
}

// RenderStoryIndexMarkdown renders vmr-stories.md — a pure, human-facing
// table (no file hashes; those live only in the JSON's "files" section).
func RenderStoryIndexMarkdown(rows []JourneyIndexRow, lang i18n.Lang) string {
	t := i18n.StoryIndexT(lang)
	var b strings.Builder
	b.WriteString("# " + t.Title + "\n\n")
	if len(rows) == 0 {
		b.WriteString(t.NoCandidatesNote)
		return b.String()
	}
	b.WriteString(t.TableHeader)
	for _, r := range rows {
		rendered := t.NotRendered
		if r.Rendered != "" {
			rendered = "[" + r.Rendered + "](" + r.Rendered + ")"
		}
		taskCol := t.NotRendered
		if r.Tasks > 0 || r.Steps > 0 {
			taskCol = strconv.Itoa(r.Tasks)
		}
		stepCol := strconv.Itoa(r.Requests)
		if r.Steps > 0 {
			stepCol = strconv.Itoa(r.Steps)
		}
		title := r.Title
		if r.Partial {
			title = "⚠ " + title
		}
		b.WriteString("| " + r.ID + " | " + r.Client + " | " +
			r.Start.In(fmtutil.DisplayZone).Format("01-02 15:04") + " → " + r.End.In(fmtutil.DisplayZone).Format("01-02 15:04") +
			" | " + taskCol + " | " + stepCol + " | " + title + " | " + rendered + " |\n")
	}
	b.WriteString(t.Footer(len(rows)))
	return b.String()
}
