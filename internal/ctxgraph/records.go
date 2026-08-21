// Ver 2026-07-29 17:00, by Sonnet 5

package ctxgraph

import (
	"encoding/json"
	"sync"

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
// pattern) and the per-file reads run on the same bounded worker pool Scan
// uses (scanWorkerCount): decoding a full audit.Record per wanted line is
// real CPU work (zstd decompression + json.Unmarshal, not just a hash), so
// a caller batching many lineages at once (internal/story.BuildAll)
// previously spent the whole wall-clock serially on one core — see
// design-doc review follow-up: -render-all's first cut was ~15x slower
// than the list-only scan on a real 15-file/253-candidate corpus, entirely
// from this loop running one file at a time. Callers needing more than a
// single message's text — internal/story building a Journey's Steps needs
// each manifest's complete request/response — use this rather than
// re-deriving message text from Manifest.Keys alone.
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

	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}

	type fileResult struct {
		recs map[Loc]*audit.Record
		err  error
	}
	results := make([]fileResult, len(paths))
	sem := make(chan struct{}, scanWorkerCount(len(paths)))
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			recs, err := fetchRecordsFromFile(path, byPath[path])
			results[i] = fileResult{recs: recs, err: err}
		}(i, path)
	}
	wg.Wait()

	out := map[Loc]*audit.Record{}
	for _, res := range results {
		if res.err != nil {
			return nil, res.err
		}
		for loc, rec := range res.recs {
			out[loc] = rec
		}
	}
	return out, nil
}

func fetchRecordsFromFile(path string, wantedLines map[int]bool) (map[Loc]*audit.Record, error) {
	rc, err := audit.OpenLogFile(path)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	out := map[Loc]*audit.Record{}
	line := 0
	err = audit.ForEachLine(rc, audit.MaxLogLine, func(lineBytes []byte) {
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
	if err != nil {
		return nil, err
	}
	return out, nil
}
