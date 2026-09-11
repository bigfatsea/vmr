package livestats

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Persisted artifact names and modes (§3.2/§3.3). 0600 files / 0700 dirs —
// the ledger carries usage profiles, the same bar the audit log is held to.
const (
	rollupFileName = "vmr-stats-rollup.jsonl"
	slimPrefix     = "vmr-stats-"
	fileMode       = 0o600
	dirMode        = 0o700
)

// slimRow is one completed request, verbatim the design's slim example
// (§3.2). Key names are the contract: tokens/client_key_tag/key_label quote
// audit's names, vmodel is the one deliberately renamed field (the flat row
// carries both the virtual and the upstream name, so the virtual one needs
// its own key). The zone offset in ts is write-time, preserved as-is.
type slimRow struct {
	TS           string      `json:"ts"`
	VModel       string      `json:"vmodel"`
	Protocol     string      `json:"protocol"`
	Stream       bool        `json:"stream"`
	Outcome      string      `json:"outcome"`
	ClientKeyTag string      `json:"client_key_tag"`
	Provider     string      `json:"provider"`
	Model        string      `json:"model"`
	KeyLabel     string      `json:"key_label"`
	DurMS        int64       `json:"dur_ms"`
	TTFTMS       int64       `json:"ttft_ms"`
	Tokens       TokenCounts `json:"tokens"`
}

// rollupRow is one (hour × dims) aggregate line, verbatim the design's
// example (§3.3). Rows are append-only; a (hour, dims) key may legitimately
// appear more than once after a crash between rollup append and slim delete
// — readers take the last row per key (§3.3/§6).
type rollupRow struct {
	Hour string      `json:"hour"`
	Dims Dims        `json:"dims"`
	OK   int64       `json:"ok"`
	Err  int64       `json:"error"`
	Cncd int64       `json:"canceled"`
	Toks TokenCounts `json:"tokens"`
	Dur  SumCount    `json:"dur_ms"`
	Ttft SumCount    `json:"ttft_ms"`
}

func (r rollupRow) counters() Counters {
	return Counters{OK: r.OK, Error: r.Err, Canceled: r.Cncd, Tokens: r.Toks, DurMS: r.Dur, TTFTMS: r.Ttft}
}

func countersRow(hk time.Time, k dimsKey, c Counters) rollupRow {
	return rollupRow{
		Hour: hourStartOf(hk).Format(hourFormat),
		Dims: k.dims(),
		OK:   c.OK, Err: c.Error, Cncd: c.Canceled,
		Toks: c.Tokens, Dur: c.DurMS, Ttft: c.TTFTMS,
	}
}

// hourFormat is the rollup row's fixed-format hour stamp with write-time
// offset, matching the design example.
const hourFormat = "2006-01-02T15:04:05Z07:00"

// appendJSONL appends one JSON line to path, creating it (0600) as needed.
// The whole line goes in one Write call; short appends are atomic in
// practice on local POSIX filesystems, and any torn line is dropped at read
// time (§3.3).
func appendJSONL(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, fileMode)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// readJSONL streams f line by line. A trailing line without a newline
// (torn write) still parses; unparseable lines are skipped (§6).
func readJSONL(f io.Reader, decode func(line []byte) error) error {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := decode(line); err != nil {
			continue // torn or corrupt line: skip, keep reading
		}
	}
	return sc.Err()
}

// errSkipLine marks a line readJSONL should silently drop.
var errSkipLine = errSkip()

func errSkip() error { return &skipLine{} }

type skipLine struct{}

func (*skipLine) Error() string { return "skip line" }

// loadRollup reads the rollup file into an in-memory map, last row per
// (hour, dims) key winning (§3.3). A missing file starts from an empty map
// — history loss is an accepted degradation, never a startup blocker (§7).
// Keys carry the stamp's own location. Rows older than minHour are read past
// but not kept: the file is the full archive, the map is the retention
// window (§8). A zero minHour keeps everything.
func loadRollup(path string, minHour time.Time) (map[time.Time]map[dimsKey]Counters, error) {
	m := make(map[time.Time]map[dimsKey]Counters)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return m, err
	}
	defer f.Close()
	err = readJSONL(f, func(line []byte) error {
		var row rollupRow
		if json.Unmarshal(line, &row) != nil || row.Hour == "" {
			return errSkipLine
		}
		hk, err := time.Parse(hourFormat, row.Hour)
		if err != nil {
			return errSkipLine
		}
		if hk.Before(minHour) {
			return nil // outside the retention window: archived on disk, not held in memory
		}
		if m[hk] == nil {
			m[hk] = make(map[dimsKey]Counters)
		}
		m[hk][row.Dims.key()] = row.counters()
		return nil
	})
	return m, err
}

// listSlimFiles returns the directory's slim file names for hours strictly
// before `before`, oldest first (§6: catch-up rolls every un-rolled hour).
func listSlimFiles(dir string, before time.Time) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasPrefix(e.Name(), slimPrefix) || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if e.Name() == rollupFileName {
			continue
		}
		h, ok := parseHourFileName(e.Name())
		if !ok || !h.Before(before) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// rollSlimFile aggregates one complete slim file by (hour × dims) — always
// from the file, never from memory; that is what makes rolling idempotent
// (§5) — and appends the result to the rollup file. A row whose own ts
// disagrees with the file's hour is bucketed by its own ts, the same rule
// the live path uses.
func rollSlimFile(dir, name string) error {
	if _, ok := parseHourFileName(name); !ok {
		return errSkipLine
	}
	total := make(map[time.Time]map[dimsKey]Counters)
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer f.Close()
	err = readJSONL(f, func(line []byte) error {
		var row slimRow
		if json.Unmarshal(line, &row) != nil {
			return errSkipLine
		}
		ts, err := time.Parse(time.RFC3339, row.TS)
		if err != nil {
			return errSkipLine
		}
		hk := hourStartOf(ts)
		if total[hk] == nil {
			total[hk] = make(map[dimsKey]Counters)
		}
		key := rowDimsKey(row)
		c := total[hk][key]
		c.addSample(rowSample(row, ts))
		total[hk][key] = c
		return nil
	})
	if err != nil {
		return err
	}
	hours := make([]time.Time, 0, len(total))
	for hk := range total {
		hours = append(hours, hk)
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i].Before(hours[j]) })
	rollupPath := filepath.Join(dir, rollupFileName)
	for _, hk := range hours {
		for _, k := range dimsKeyList(total[hk]) {
			if err := appendJSONL(rollupPath, countersRow(hk, k, total[hk][k])); err != nil {
				return err
			}
		}
	}
	return nil
}

func rowDimsKey(r slimRow) dimsKey {
	return dimsKey{r.VModel, r.Protocol, r.ClientKeyTag, r.Provider, r.Model, r.KeyLabel, r.Stream}
}

func rowSample(r slimRow, ts time.Time) Sample {
	return Sample{
		TS: ts, VModel: r.VModel, Protocol: r.Protocol, Stream: r.Stream,
		Outcome: r.Outcome, ClientKeyTag: r.ClientKeyTag,
		Provider: r.Provider, Model: r.Model, KeyLabel: r.KeyLabel,
		DurMS: r.DurMS, TTFTMS: r.TTFTMS, Tokens: r.Tokens,
	}
}

// dimsKeyList orders a dims map for deterministic rollup row output.
func dimsKeyList(m map[dimsKey]Counters) []dimsKey {
	keys := make([]dimsKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].id() < keys[j].id() })
	return keys
}

// deleteSlim removes a rolled slim file; failure is non-fatal (worst case:
// the hour rolls twice, which last-wins reading absorbs).
func deleteSlim(dir, name string) error {
	return os.Remove(filepath.Join(dir, name))
}

// openSlim opens (creating if needed) the current hour's slim file for
// appending.
func openSlim(dir string, now time.Time) (*os.File, error) {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, hourFileName(now)), os.O_CREATE|os.O_WRONLY|os.O_APPEND, fileMode)
}
