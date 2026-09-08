// Ver 2026-08-01, by Sonnet 5

// The per-request data layer: requests/index.json is the machine-readable
// single source of truth for request rows (D7/§3.7 — the human-readable
// vmr-requests*.md index family was deleted; interactive browsing is the
// request-browser.html skeleton page's job, triage stays on
// requests/failed.md). The index carries a SessionMeta projection (session
// titles, aliases, per-task titles) and a journey cross-link map so the
// dashboard can group and navigate without re-deriving them.
// All displayed timestamps are rendered in fmtutil.DisplayZone (the system
// default timezone) regardless of the source record's own offset.

package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// RequestsIndex is requests/index.json's whole shape: one row per request.
// The parse cache used to live here too, as a "files" section (see
// ctxgraph.FileCache/ScanCached) — it's since moved to its own
// content-hash-sharded directory shared with internal/journey
// (ctxgraph.LoadCacheDir/SaveCacheDir, {outDir}/.cache/parse), so this
// index stays purely human-scale.
// requests/failed.jsonl stays a plain flat JSONL — it's a filtered
// dump of Requests, not itself an independent cache.
// SessionMeta carries one session's title, alias, and per-task title mapping
// projected into requests/index.json (§3.3).
type SessionMeta struct {
	Title string            `json:"title,omitempty"`
	Alias string            `json:"alias,omitempty"`
	Tasks map[string]string `json:"tasks,omitempty"`
}

type RequestsIndex struct {
	Requests    []RequestRow           `json:"requests"`
	Sessions    map[string]SessionMeta `json:"sessions,omitempty"`
	JourneyLink map[string]string      `json:"journey_link,omitempty"`
}

// WriteRequestsJSON writes requests/index.json — RequestsIndex's rows only;
// the parse cache is persisted separately (see RequestsIndex's doc
// comment).
func WriteRequestsJSON(rows []RequestRow, path string) (n int, err error) {
	idx := RequestsIndex{Requests: rows}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// WriteRequestsJSONL writes one RequestRow per line — used for
// requests/failed.jsonl, a filtered dump with no cache section of its
// own (see RequestsIndex's doc comment).
func WriteRequestsJSONL(rows []RequestRow, path string) (n int, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() {
		// Close can be where a full disk's delayed write failure actually
		// surfaces (Flush below only pushes into the OS buffer) — a plain
		// `defer f.Close()` would swallow that and report "success" over an
		// incomplete file.
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	bw := bufio.NewWriter(f)
	for _, r := range rows {
		data, merr := json.Marshal(r)
		if merr != nil {
			return 0, merr
		}
		bw.Write(data)
		bw.WriteByte('\n')
	}
	if err = bw.Flush(); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// WriteRequestsIndex writes requests/index.json (the machine-readable single source of truth
// for per-request drill-down, populated with session analysis projection and journey cross-links).
// The legacy human-readable Markdown request indexes (vmr-requests.md, vmr-requests-<tag>.md,
// vmr-requests-cron-*.md) are retired per D7 / §3.7.
func WriteRequestsIndex(rep *Report2, sess *SessionAnalysis, dir string, lang i18n.Lang, journeyLink map[string]string, detailDir string) error {
	rows := rep.RequestRows()
	sessions := make(map[string]SessionMeta)
	if sess != nil {
		for _, s := range sess.Sessions {
			meta := SessionMeta{
				Title: s.Title,
				Alias: s.DisplayAlias,
				Tasks: make(map[string]string),
			}
			for _, t := range s.Tasks {
				meta.Tasks[t.ID] = t.Title
			}
			sessions[s.ID] = meta
		}
	}
	idx := RequestsIndex{
		Requests:    rows,
		Sessions:    sessions,
		JourneyLink: journeyLink,
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "index.json"), data, 0o600)
}

// fmtDisplayFull renders an RFC3339 timestamp (CompactionRow.TS, the
// Meta.From/To window bounds) as "2026-07-24 00:17:58" in
// fmtutil.DisplayZone (the system default timezone), regardless of the
// value's own embedded offset. Falls back to a raw cut when the timestamp
// doesn't parse (defensive; Build always writes RFC3339).
func fmtDisplayFull(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return cut(ts, 19)
	}
	return t.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05")
}

func orDashModel(m string) string {
	if m == "" {
		return "-"
	}
	return m
}

// buildDetailFileSet lists detailDir once and returns its .md basenames as
// a set — detailCell's existence check would otherwise be one
// os.Stat per row, and a full request index re-renders every row across
// several tables (per-session cards, the scheduled rollup, the failed
// index): a real full-corpus run (11k+ requests) would issue 20,000+ redundant
// stat syscalls — most of them ENOENT lookups against an empty or
// nonexistent directory on the common (default suite) path — for
// information one os.ReadDir already gives in full. A missing directory
// returns a nil (empty) set, not an error. Hidden files (dotfiles, a
// stray .reqdetail-*.tmp from an interrupted atomic write) and non-.md
// entries are excluded so they can never be mistaken for a real detail
// page's presence.
func buildDetailFileSet(detailDir string) map[string]struct{} {
	entries, err := os.ReadDir(detailDir)
	if err != nil {
		return nil
	}
	set := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}

// detailCell renders the "文件" column. detailSet (buildDetailFileSet) is
// {outDir}/details' current contents, always computed regardless of
// whether this run's -details flag was passed — the criterion is whether
// r.DetailFile actually exists right now, not whether a flag was set:
// since `vmr analyze` runs the journey half (which may batch-materialize
// details for -render-all) before the report half, a flag-only check
// could claim "no details were written"
// while 306 of them sit on disk, or the reverse. r.DetailFile itself is
// always computed (session.go, a pure function of the record's identity)
// whether or not anything was ever written to it — see FileName's own doc
// comment for why an existence check, not the filename's mere presence, is
// what's authoritative here. When the target isn't there, this falls back
// to r.Req, the record's stable req coordinate (basename:line,
// ctxgraph.ReqCoord) as inline code, letting the reader fetch it on demand
// (`vmr replay -req COORD -print`) without ever producing a dead link.
func detailCell(r RequestRow, detailSet map[string]struct{}) string {
	if r.DetailFile != "" {
		if _, ok := detailSet[r.DetailFile]; ok {
			label := r.Req
			if label == "" {
				label = r.DetailFile
			}
			return fmt.Sprintf("[%s](details/%s)", label, r.DetailFile)
		}
	}
	if r.Req == "" {
		return "-"
	}
	return "`" + r.Req + "`"
}

func sessTaskCell(r RequestRow) string {
	if r.Session == "" {
		return "-"
	}
	if r.Task == "" {
		return r.Session
	}
	return r.Session + "/" + r.Task
}

func outcomeCell(r RequestRow) string {
	switch r.Outcome {
	case "ok":
		if r.Truncated {
			return "ok⚠️trunc"
		}
		return "ok"
	case "canceled":
		return "canceled"
	default:
		ec := r.ErrorClass
		if ec == "" {
			ec = "unclassified"
		}
		return "❌" + ec
	}
}
