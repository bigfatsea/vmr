package report

import (
	"encoding/json"
	"io"

	"vmr/internal/audit"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// writeDetailsBaseline renders every record in the given audit files into dir.
// Retained as the differential baseline for TestBuildOnRecordMatchesWriteDetails
// and as a test helper for detail output assertions.
func writeDetailsBaseline(paths []string, dir string, sess *SessionAnalysis, _ io.Writer, lang i18n.Lang, prof taskseg.Profile) (int, error) {
	dw, err := NewDetailWriter(dir, lang, prof)
	if err != nil {
		return 0, err
	}

	var outerErr error
	for _, path := range paths {
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
		if scanErr != nil {
			outerErr = scanErr
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
