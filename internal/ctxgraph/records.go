// Ver 2026-07-28 22:55, by Sonnet 5

package ctxgraph

import (
	"encoding/json"

	"vmr/internal/audit"
)

// Loc identifies one audit record by its source coordinate — the same
// (Path, Line) every Manifest already carries.
type Loc struct {
	Path string
	Line int
}

// FetchRecords re-reads the given coordinates and returns the full decoded
// audit.Record at each, batched per file (each file opened and scanned at
// most once regardless of how many lines are wanted from it — zstd isn't
// seekable, so one streaming pass per file is the only efficient access
// pattern, same rationale as BlobIndex.FetchAll). Callers needing more than
// a single message's text — internal/story building a Journey's Steps needs
// each manifest's complete request/response — use this instead of
// BlobIndex.FetchAll.
func FetchRecords(locs []Loc) (map[Loc]*audit.Record, error) {
	byPath := map[string]map[int]bool{}
	for _, loc := range locs {
		lines := byPath[loc.Path]
		if lines == nil {
			lines = map[int]bool{}
			byPath[loc.Path] = lines
		}
		lines[loc.Line] = true
	}
	out := map[Loc]*audit.Record{}
	for path, wantedLines := range byPath {
		if err := fetchRecordsFromFile(path, wantedLines, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func fetchRecordsFromFile(path string, wantedLines map[int]bool, out map[Loc]*audit.Record) error {
	rc, err := audit.OpenLogFile(path)
	if err != nil {
		return err
	}
	defer rc.Close()

	line := 0
	return audit.ForEachLine(rc, audit.MaxLogLine, func(lineBytes []byte) {
		line++
		if !wantedLines[line] {
			return
		}
		var rec audit.Record
		if json.Unmarshal(lineBytes, &rec) != nil {
			return
		}
		out[Loc{Path: path, Line: line}] = &rec
	}, func() { line++ })
}
