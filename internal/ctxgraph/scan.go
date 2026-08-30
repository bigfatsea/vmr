// Ver 2026-07-28 22:30, by Sonnet 5

package ctxgraph

import (
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"sync"

	"vmr/internal/audit"
)

// Graph is the result of scanning a set of audit files: every SessKey
// bucket split into lineages at its structural breaks.
type Graph struct {
	Lineages []*Lineage
	// Ungrouped holds manifests that parsed but derived no SessKey at all
	// (no metadata.user_id and no non-system messages to anchor on) — kept
	// only for a completeness count, since they carry no lineage
	// information.
	Ungrouped []*Manifest
	// NoBody counts records whose client request body wasn't a parseable
	// chat object at all (rejected requests, malformed JSON) — these never
	// become a Manifest in the first place.
	NoBody int
}

// Scan reads paths (audit JSONL, optionally .zst-compressed — see
// audit.OpenLogFile) and builds a Graph. Per-file reading runs on a bounded
// worker pool (same rationale as internal/report/session.go's
// AnalyzeSessions: each file's manifests are a pure function of that file
// alone, so parallelizing which file gets read first cannot change the
// final result once everything is merged and stably sorted by timestamp
// afterward, single-threaded).
func Scan(paths []string) (*Graph, error) {
	if err := CheckPathCollisions(paths); err != nil {
		return nil, err
	}
	results := make([]fileScanResult, len(paths))
	sem := make(chan struct{}, scanWorkerCount(len(paths)))
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = scanFile(path)
		}(i, path)
	}
	wg.Wait()

	var all []*Manifest
	noBody := 0
	for _, res := range results {
		if res.err != nil {
			return nil, res.err
		}
		all = append(all, res.manifests...)
		noBody += res.noBody
	}
	return buildGraph(all, noBody), nil
}

// buildGraph is Scan's shared tail: sort every already-parsed Manifest by
// timestamp, bucket by SessKey, and split each bucket into lineages — pure
// in-memory work over Manifests, with no dependency on where they came from
// (freshly parsed this call, or reused from a cache — see ScanCached in
// cache.go). Manifest count, not source file bytes, is what this scales
// with, so it stays cheap even when most of the corpus is cache-sourced.
func buildGraph(all []*Manifest, noBody int) *Graph {
	sort.SliceStable(all, func(i, j int) bool { return all[i].TS.Before(all[j].TS) })

	g := &Graph{NoBody: noBody}
	buckets := map[string][]*Manifest{}
	var order []string
	for _, m := range all {
		if m.SessKey == "" {
			g.Ungrouped = append(g.Ungrouped, m)
			continue
		}
		if _, ok := buckets[m.SessKey]; !ok {
			order = append(order, m.SessKey)
		}
		buckets[m.SessKey] = append(buckets[m.SessKey], m)
	}

	idx := 0
	for _, key := range order {
		for _, l := range splitBucket(key, buckets[key]) {
			l.Idx = idx
			idx++
			g.Lineages = append(g.Lineages, l)
		}
	}
	return g
}

// scanWorkerCount bounds file-read concurrency the same way AnalyzeSessions
// does: zstd decompression is CPU-bound, so more workers than cores (or
// than files) just adds scheduling overhead.
func scanWorkerCount(files int) int {
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	if files > 0 && n > files {
		n = files
	}
	if n < 1 {
		n = 1
	}
	return n
}

type fileScanResult struct {
	manifests []*Manifest
	noBody    int
	err       error
}

// scanFile reads and BuildManifest()s every record in one audit file.
func scanFile(path string) fileScanResult {
	rc, err := audit.OpenLogFile(path)
	if err != nil {
		return fileScanResult{err: err}
	}
	defer rc.Close()

	var out []*Manifest
	noBody := 0
	line := 0
	scanErr := audit.ForEachLine(rc, audit.MaxLogLine, func(lineBytes []byte) {
		line++
		var rec audit.Record
		if json.Unmarshal(lineBytes, &rec) != nil {
			noBody++
			return
		}
		if m, ok := BuildManifest(&rec, path, line); ok {
			m.Bytes = len(lineBytes) // decompressed JSON line length — see Manifest.Bytes
			out = append(out, m)
		} else {
			noBody++
		}
	}, func() { line++ })
	if scanErr != nil {
		return fileScanResult{err: fmt.Errorf("%s: %w", path, scanErr)}
	}
	return fileScanResult{manifests: out, noBody: noBody}
}
