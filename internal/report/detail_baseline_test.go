// Ver 2026-09-12 12:00, by dev

package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"vmr/internal/audit"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// writeDetailsBaseline renders every record in the given audit files into dir (one
// .md per record, in lang — no same-named .json since P3.1). Returns the
// number of record files written. Reruns overwrite deterministically.
//
// Retained only as the two-pass differential baseline
// TestBuildOnRecordMatchesWriteDetails (detail_test.go) diffs Build's
// single-pass onRecord hook against, byte-for-byte.
func writeDetailsBaseline(paths []string, dir string, sess *SessionAnalysis, progress io.Writer, lang i18n.Lang, prof taskseg.Profile) (int, error) {
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
			p, l := path, line
			if info != nil {
				p, l = info.Path, info.Line
			}
			dw.jobs <- detailJob{rec: &rec, info: info, path: p, line: l}
		}, func() { line++ }) // skipped lines still advance the counter so sess.Lookup keys stay aligned with AnalyzeSessions
		rc.Close()
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
