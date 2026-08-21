// Ver 2026-08-20 00:00, by Sonnet 5

// Per-request detail export: every audit record becomes one Markdown file
// under {out}/details/, named by its coordinate hash (see
// internal/reqdetail.FileName) so reruns are deterministic regardless of
// batch size or machine timezone. This file is the report-side adapter
// only: it owns the worker pool and the ReqInfo→(path,line,Manifest,prev
// Manifest) translation, while the actual rendering and naming logic lives
// in internal/reqdetail — the leaf both this package and internal/story
// render detail pages through, so a page generated via either package's
// code path is byte-identical (see reqdetail's package doc and
// docs/future-strategy/story_report_p2_action_plan_sonnet-5.md §3).
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/reqdetail"
	"vmr/internal/taskseg"
)

// detailWorkerCount bounds how many records get rendered+written
// concurrently in WriteDetails. Capped well below NumCPU on large machines:
// each job is two small-file writes, and past a point more goroutines just
// contend on the filesystem/GC instead of finishing faster.
func detailWorkerCount() int {
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	if n > 16 {
		n = 16
	}
	return n
}

// detailJob is one record's render-and-write work, queued for a worker. wg
// (optional) is Done() by the worker that processes this job — see
// detailWriter.submit.
type detailJob struct {
	rec  *audit.Record
	info *ReqInfo
	path string
	line int
	wg   *sync.WaitGroup
}

// manifestsFor resolves j's own and lineage-predecessor Manifest from its
// ReqInfo, when it has one. info.manifest/info.Parent are the same
// correlated-during-session-analysis data attach() (session.go) already
// established; this just hands them to reqdetail in its plain-argument
// shape instead of the ReqInfo type reqdetail must not depend on.
func (j detailJob) manifestsFor() (m, prev *ctxgraph.Manifest) {
	if j.info == nil {
		return nil, nil
	}
	m = j.info.manifest
	if j.info.Parent != nil {
		prev = j.info.Parent.manifest
	}
	return m, prev
}

// writeOneDetail renders one record's detail page if it isn't already on
// disk, via reqdetail.EnsureRendered — idempotent (a rerun over the same
// records skips every file it already wrote) and, since P3.1, without a
// same-named .json copy of the raw record: that used to be a byte-for-byte
// duplicate of data that already exists, addressably, in the source audit
// log (see internal/audit.LineAt and `vmr replay -req COORD -print`, its
// replacement) — see docs/future-strategy/story_report_architecture_opus-5.md
// §7.6c. Errors are reported through recordErr rather than returned, since
// this runs on a worker goroutine, not the caller's.
func writeOneDetail(dir, evidenceDir string, lang i18n.Lang, prof taskseg.Profile, j detailJob, n *int64, recordErr func(error)) {
	m, prev := j.manifestsFor()
	if _, err := reqdetail.EnsureRendered(dir, j.rec, j.path, j.line, m, prev, prof, lang, evidenceDir); err != nil {
		recordErr(err)
		return
	}
	atomic.AddInt64(n, 1)
}

// DetailWriter is a bounded worker pool that renders and writes one .md per
// submitted record (the same-named .json copy is gone since P3.1 — see
// writeOneDetail) — the reusable half of what used to be
// WriteDetails' own, self-contained implementation. It has two callers now:
// WriteDetails itself (drives it from its own file-scanning loop, one
// submit per record, batched per file via a *sync.WaitGroup so its progress
// line still reports real per-file elapsed time), and Build's onRecord hook
// (cmd/vmr constructs one and passes its Submit method — driven directly
// during Build's existing aggregation pass, no file scan of its own at all;
// see Build's doc comment for why). Every record's detail page depends only
// on that record's own (audit.Record, path, line, Manifest, prev Manifest)
// tuple, so there's no cross-record ordering constraint either caller needs
// to preserve — and, unlike before P2, no shared naming state either: the
// coordinate hash makes every name unique on its own, so submit/Submit no
// longer need a mutex-guarded `used` fallback-naming map.
type DetailWriter struct {
	dir         string
	evidenceDir string
	lang        i18n.Lang
	prof        taskseg.Profile
	jobs        chan detailJob
	poolWG      sync.WaitGroup
	n           int64
	errMu       sync.Mutex
	firstErr    error
}

// NewDetailWriter creates dir and starts the worker pool (detailWorkerCount
// goroutines) rendering every submitted record's detail page in lang. prof
// resolves the two dialect-aware judgments Render needs (NoReply, chat-id
// extraction) — pass the same Profile the caller's session analysis uses.
// Callers must eventually call Close to drain it.
//
// Shared evidence blobs (system prompt, declared tool set — see
// internal/reqdetail's evidence.go) go into "evidence" next to dir's own
// parent, i.e. dir's sibling — dir is always {outDir}/details (every
// caller follows this convention), so this resolves to {outDir}/evidence,
// matching internal/story's own future use of the same directory (P3.4's
// scope is `vmr report` only; the convention itself is package-agnostic).
func NewDetailWriter(dir string, lang i18n.Lang, prof taskseg.Profile) (*DetailWriter, error) {
	// 0o700/0o600 throughout: detail files carry the same full conversation
	// bodies as the audit JSONL they were derived from, which is
	// deliberately written 0600 — the exports must not silently loosen that.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	numWorkers := detailWorkerCount()
	dw := &DetailWriter{
		dir:         dir,
		evidenceDir: filepath.Join(filepath.Dir(dir), "evidence"),
		lang:        lang,
		prof:        prof,
		jobs:        make(chan detailJob, numWorkers*4),
	}
	dw.poolWG.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer dw.poolWG.Done()
			for j := range dw.jobs {
				writeOneDetail(dw.dir, dw.evidenceDir, dw.lang, dw.prof, j, &dw.n, dw.recordErr)
				if j.wg != nil {
					j.wg.Done()
				}
			}
		}()
	}
	return dw, nil
}

func (dw *DetailWriter) recordErr(err error) {
	dw.errMu.Lock()
	if dw.firstErr == nil {
		dw.firstErr = err
	}
	dw.errMu.Unlock()
}

func (dw *DetailWriter) hasErr() bool {
	dw.errMu.Lock()
	defer dw.errMu.Unlock()
	return dw.firstErr != nil
}

// submit queues one record's render+write. wg is optional: pass one to wait
// for a batch to finish (WriteDetails waits per file); pass nil for
// fire-and-forget, relying on a later Close to drain everything (Build's
// hook, via Submit below — it has no natural "batch" boundary of its own).
// path/line default to info's own coordinate when info is non-nil (the
// common case, and the one WriteDetails' fallback below needs when info IS
// nil); WriteDetails passes its own scan-loop path/line explicitly since it
// has them regardless of whether Lookup found a ReqInfo.
func (dw *DetailWriter) submit(rec *audit.Record, info *ReqInfo, path string, line int, wg *sync.WaitGroup) {
	if wg != nil {
		wg.Add(1)
	}
	dw.jobs <- detailJob{rec: rec, info: info, path: path, line: line, wg: wg}
}

// Submit queues one record's render+write, fire-and-forget — the exported
// entry point for a caller (e.g. Build's onRecord hook) with no per-batch
// wait of its own. A no-op once a prior job has already failed, matching
// WriteDetails' own short-circuit, so a broken output directory (e.g. disk
// full) doesn't queue thousands more doomed jobs once it's known bad.
func (dw *DetailWriter) Submit(rec *audit.Record, info *ReqInfo) {
	if dw.hasErr() {
		return
	}
	path, line := "", 0
	if info != nil {
		path, line = info.Path, info.Line
	}
	dw.submit(rec, info, path, line, nil)
}

// Close drains the pool and returns the total records written and the
// first error encountered, if any.
func (dw *DetailWriter) Close() (int, error) {
	close(dw.jobs)
	dw.poolWG.Wait()
	return int(atomic.LoadInt64(&dw.n)), dw.firstErr
}

// WriteDetails renders every record in the given audit files into dir (one
// .md + one same-named .json per record, in lang). Returns the number of
// record files written. Reruns overwrite deterministically — every name is
// the record's own coordinate hash (internal/reqdetail.FileName), so unlike
// before P2 there is no batch-order or cross-run dependency to keep paths
// aligned for: any subset of files, scanned via any path spelling, names
// each record the same way.
//
// sess (optional, nil = plain mode) supplies the session grouping: detail
// pages gain a previous-turn link and delta highlight when sess correlates
// this record to a lineage predecessor.
//
// This is a standalone alternative to Build's onRecord hook — a second,
// independent read of the same audit files, for callers that want detail
// export without running the full aggregation pass. `vmr report` itself no
// longer calls this (Build's hook covers it in one pass instead); it stays
// for tests and any other standalone use.
//
// progress (optional, nil = silent) gets one line per input file.
func WriteDetails(paths []string, dir string, sess *SessionAnalysis, progress io.Writer, lang i18n.Lang, prof taskseg.Profile) (int, error) {
	dw, err := NewDetailWriter(dir, lang, prof)
	if err != nil {
		return 0, err
	}

	var outerErr error
	for fileIdx, path := range paths {
		fileStart := time.Now()
		before := atomic.LoadInt64(&dw.n)
		rc, err := audit.OpenLogFile(path)
		if err != nil {
			outerErr = err
			break
		}
		line := 0
		var fileWG sync.WaitGroup
		scanErr := audit.ForEachLine(rc, audit.MaxLogLine, func(lineBytes []byte) {
			line++
			if dw.hasErr() {
				return
			}
			var rec audit.Record
			if err := json.Unmarshal(lineBytes, &rec); err != nil {
				return // Build already counts parse errors
			}
			info := sess.Lookup(path, line)
			dw.submit(&rec, info, path, line, &fileWG)
		}, func() { line++ }) // skipped lines still advance the counter so sess.Lookup keys stay aligned with AnalyzeSessions
		rc.Close()
		fileWG.Wait() // drain this file's jobs so the progress line below reflects real elapsed time
		if progress != nil {
			fmt.Fprintf(progress, "[%d/%d] %s  done: %d detail file pairs (%s)\n",
				fileIdx+1, len(paths), path, atomic.LoadInt64(&dw.n)-before, time.Since(fileStart).Round(time.Millisecond))
		}
		if scanErr != nil {
			outerErr = fmt.Errorf("%s: %w", path, scanErr)
			break
		}
		if dw.hasErr() {
			break
		}
	}

	n, err := dw.Close()
	if outerErr != nil {
		return n, outerErr
	}
	return n, err
}
