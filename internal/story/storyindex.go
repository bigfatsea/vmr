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

// JourneyCategory classifies a candidate Journey by structural signals
// only (see classifyJourney in candidates.go) — CategoryTask is the zero
// value on purpose, so a real task (the common case) never needs an
// explicit tag in vmr-stories.json (omitempty on JourneyIndexRow.Category).
type JourneyCategory string

const (
	CategoryTask      JourneyCategory = ""
	CategoryCron      JourneyCategory = "cron"
	CategoryHeartbeat JourneyCategory = "heartbeat"
	CategorySubagent  JourneyCategory = "subagent"
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
	// Lineages is every ctxgraph.Lineage.LineageID() this Journey's chain
	// is built from (P6.1) — report's SessionRow.ID uses the same
	// identity for the single Lineage it represents, so "does report
	// session X belong to story Journey Y" becomes a set-membership check
	// against this slice instead of a cross-command hash-and-compare.
	Lineages []string `json:"lineages,omitempty"`
	// Category classifies this candidate by title content markers alone
	// (see classifyJourney) so a noisy scheduled/heartbeat/subagent
	// candidate can be told apart from a real task-shaped one without
	// introducing new guessing (P6.3). Omitted (empty string) means
	// CategoryTask — the common case doesn't need an explicit tag.
	Category JourneyCategory `json:"category,omitempty"`
}

// StoryIndex is vmr-stories.json's whole shape: just Journeys. The parse
// cache used to live here too, as a "files" section — it's since moved to
// its own content-hash-sharded directory shared with internal/report
// (ctxgraph.LoadCacheDir/SaveCacheDir, {outDir}/.parse-cache — one level
// above storiesDir), so this index stays purely human-scale (see
// docs/future-strategy/story_report_p3_action_plan_sonnet-5.md batch D).
type StoryIndex struct {
	Journeys []JourneyIndexRow `json:"journeys"`
	// Cache is this run's own ScanCached result, carried on StoryIndex
	// purely as a convenience — every cmdStory branch already threads idx
	// through to saveStoryIndex, so riding along here saves plumbing it as
	// a second parameter everywhere. Never serialized into
	// vmr-stories.json (json:"-"): saveStoryIndex persists it separately,
	// via ctxgraph.SaveCacheDir, at the same point it saves idx itself.
	Cache *ctxgraph.FileCache `json:"-"`
}

// LoadStoryIndex reads path if present. A missing, unreadable, or corrupt
// file all degrade the same way — an empty index (no prior Journey rows) —
// the same best-effort-cache contract internal/imgprep's disk cache uses:
// a bad index must never fail or corrupt the actual run, only cost it the
// Tasks/Rendered carry-forward. Cache is left nil — load it separately via
// ctxgraph.LoadCacheDir, same as internal/report's cmd_report.go does.
func LoadStoryIndex(path string) *StoryIndex {
	empty := &StoryIndex{}
	data, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	var idx StoryIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return empty
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
	lineages := make([]string, len(chain))
	for i, l := range chain {
		requests += len(l.Manifests)
		for _, m := range l.Manifests {
			fileSet[m.Path] = true
		}
		lineages[i] = l.LineageID()
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
		Lineages: lineages,
		Category: classifyJourney(title),
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

// SourceFiles unions the Files lists of idx's rows matching any of ids —
// the same file set BuildJourneyIndexRow already computed for each of those
// Journeys individually, deduplicated and sorted for a stitched pair that
// shares a boundary file. This is -compare's evidence-provenance source:
// exactly the files the two Journeys being compared were built from, never
// the full set of files this run happened to load (that would list every
// unrelated Journey's log file too — see docs/VirtualModelRouter_Design_v4_Analytics.md's
// vmr-stories.json section on why the index exists at all). An id with no
// matching row (shouldn't happen — every id passed here was itself resolved
// from idx's own candidate set moments earlier) simply contributes nothing.
func SourceFiles(idx *StoryIndex, ids ...string) []string {
	if idx == nil {
		return nil
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	set := map[string]bool{}
	for _, r := range idx.Journeys {
		if !want[r.ID] {
			continue
		}
		for _, f := range r.Files {
			set[f] = true
		}
	}
	files := make([]string, 0, len(set))
	for f := range set {
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}

// RenderStoryIndexMarkdown renders vmr-stories.md — a pure, human-facing
// table (no file hashes; those live only in the JSON's "files" section).
// Rows are split by Category (P6.3): task/cron are real work and stay in
// the main, always-expanded table; heartbeat/subagent are structural
// noise (real-corpus measurement: over half a typical candidate list) and
// go in a collapsed <details> block below it, so the landing page's first
// screen is dominated by real tasks. vmr-stories.json (the machine layer)
// is unaffected — it lists every row with no such split, per this
// project's "machine layer never makes editorial cuts" rule.
func RenderStoryIndexMarkdown(rows []JourneyIndexRow, lang i18n.Lang) string {
	t := i18n.StoryIndexT(lang)
	var b strings.Builder
	b.WriteString("# " + t.Title + "\n\n")
	if len(rows) == 0 {
		b.WriteString(t.NoCandidatesNote)
		return b.String()
	}
	var visible, noisy []JourneyIndexRow
	for _, r := range rows {
		if r.Category == CategoryHeartbeat || r.Category == CategorySubagent {
			noisy = append(noisy, r)
		} else {
			visible = append(visible, r)
		}
	}
	if len(visible) > 0 {
		b.WriteString(t.TableHeader)
		for _, r := range visible {
			writeStoryIndexRow(&b, r, t)
		}
	} else {
		b.WriteString(t.NoCandidatesNote)
	}
	if len(noisy) > 0 {
		b.WriteString("\n<details>\n<summary>" + t.NoiseFoldSummary(len(noisy)) + "</summary>\n\n")
		b.WriteString(t.TableHeader)
		for _, r := range noisy {
			writeStoryIndexRow(&b, r, t)
		}
		b.WriteString("\n</details>\n")
	}
	b.WriteString(t.Footer(len(rows)))
	return b.String()
}

// writeStoryIndexRow renders one JourneyIndexRow as a table row — shared
// by RenderStoryIndexMarkdown's visible and collapsed-noise sections so
// the row format has exactly one definition.
func writeStoryIndexRow(b *strings.Builder, r JourneyIndexRow, t i18n.StoryIndexText) {
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
