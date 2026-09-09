package livestats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Aggregator is the completion-time ledger (§3.4/§4): in-memory counters for
// the current hour, the rollup history's memory image, and the per-provider
// TTFT/TPS rings — plus the current hour's slim file handle. One mutex
// covers all memory state and the file append (§4.3: one lock per request,
// coarse by design); Record never blocks on anything but that mutex and the
// file write.
type Aggregator struct {
	mu     sync.Mutex
	dir    string
	now    func() time.Time
	hour   time.Time // start of the hour the slim file is open for
	slim   *os.File  // current hour's slim WAL; nil when the write side degraded
	lock   *os.File  // advisory dir lock (.vmr-stats.lock); held for the aggregator's lifetime
	rollup map[time.Time]map[dimsKey]Counters
	cur    map[dimsKey]Counters
	rings  map[ringKey]*ring

	// snapCache serves the /stats read path (CachedSnapshot): a full fold is
	// O(rollup), and the dashboard polls ~1s from possibly several tabs at
	// once, so without this each poll would hold mu through the fold.
	snapCache Snapshot
	snapAt    time.Time
	snapValid bool
}

// New builds the aggregator and performs synchronous restart recovery (§7):
// load rollup (last-wins) → catch-up-roll older slim files → rebuild current
// hour counters and rings from the current slim → open for writing.
func New(dir string) (*Aggregator, error) {
	return NewAt(dir, time.Now)
}

// NewAt is New with an injectable clock, for tests.
func NewAt(dir string, now func() time.Time) (*Aggregator, error) {
	a := &Aggregator{
		dir:    dir,
		now:    now,
		rollup: make(map[time.Time]map[dimsKey]Counters),
		cur:    make(map[dimsKey]Counters),
		rings:  make(map[ringKey]*ring),
	}
	if err := a.recover(now()); err != nil {
		return nil, err
	}
	return a, nil
}

// recover runs the §7 startup sequence. Rollup load and slim rebuild
// degrade gracefully (unreadable history starts empty; a corrupt line is
// skipped); only a failure to create the log dir is fatal.
func (a *Aggregator) recover(now time.Time) error {
	a.hour = hourStartOf(now)
	if err := os.MkdirAll(a.dir, dirMode); err != nil {
		return err
	}

	// Take the advisory dir lock before touching any slim/rollup file. A
	// second vmr process on the same log_dir fails here — cmd_start then
	// degrades this instance to memory-only stats rather than corrupting the
	// first instance's files. When -audit is on, audit.New already failed
	// first; this lock is what covers -audit=false (design §3.2).
	lock, err := acquireDirLock(a.dir)
	if err != nil {
		return err
	}
	a.lock = lock

	// Step 1: scan log_dir, load rollup into memory (last-wins).
	rm, err := loadRollup(a.rollupPath())
	if err != nil {
		rm = make(map[time.Time]map[dimsKey]Counters)
	}
	a.rollup = rm

	// Step 2: catch-up roll every slim file older than the current hour (§6),
	// oldest first.
	old, _ := listSlimFiles(a.dir, a.hour)
	for _, name := range old {
		if err := rollSlimFile(a.dir, name); err == nil {
			deleteSlim(a.dir, name)
		}
	}
	if len(old) > 0 {
		if reloaded, err := loadRollup(a.rollupPath()); err == nil {
			a.rollup = reloaded
		}
	}

	// Step 3: read the current hour's slim file if it exists, rebuilding
	// current-hour counters and rings (rings take the last ≤100 entries).
	slimFile := filepath.Join(a.dir, hourFileName(a.hour))
	if f, err := os.Open(slimFile); err == nil {
		rebuildCurrentHour(f, a.cur, a.rings)
		f.Close()
	}

	// Step 4: open the current hour's slim for appending. Failure degrades
	// this hour's persistence to memory-only (§9).
	f, err := openSlim(a.dir, a.hour)
	if err != nil {
		a.slim = nil
		return nil
	}
	a.slim = f
	return nil
}

// rebuildCurrentHour feeds a current-hour slim's rows into the counter map
// and the rings (rings naturally keep the last ≤100 entries per key).
func rebuildCurrentHour(f *os.File, cur map[dimsKey]Counters, rings map[ringKey]*ring) {
	_ = readJSONL(f, func(line []byte) error {
		var row slimRow
		if json.Unmarshal(line, &row) != nil {
			return errSkipLine
		}
		ts, err := time.Parse(time.RFC3339, row.TS)
		if err != nil {
			return errSkipLine
		}
		s := rowSample(row, ts)
		key := s.key()
		c := cur[key]
		c.addSample(s)
		cur[key] = c
		addRing(rings, s)
		return nil
	})
}

func (a *Aggregator) rollupPath() string {
	return filepath.Join(a.dir, rollupFileName)
}

// Record books one completed request (§4.1): O(1) memory updates plus at
// most one slim append under the aggregator's single mutex. A slim write
// failure degrades only this sample's persistence — the memory update is
// still applied, and the write side shuts down for the hour rather than
// retrying per request.
func (a *Aggregator) Record(s Sample) {
	a.mu.Lock()
	defer a.mu.Unlock()

	sampleHour := hourStartOf(s.TS)
	if sampleHour.Equal(a.hour) {
		a.bookSample(s, true)
		return
	}
	if sampleHour.After(a.hour) {
		a.rollHourLocked(sampleHour)
		a.bookSample(s, true)
		return
	}
	// sampleHour is before a.hour: a late-finishing long request whose arrival
	// belonged to an already rolled hour (§9: hour attribution follows arrival).
	a.bookPastSampleLocked(s, sampleHour)
}

// bookSample applies the §4.2 attribution rules to the live maps and appends
// the slim line. Ring and service-face counters only see forwarded samples.
func (a *Aggregator) bookSample(s Sample, appendFile bool) {
	key := s.key()
	c := a.cur[key]
	c.addSample(s)
	a.cur[key] = c
	addRing(a.rings, s)

	if appendFile && a.slim != nil {
		row := slimRow{
			TS:           s.TS.Format(time.RFC3339),
			VModel:       s.VModel,
			Protocol:     s.Protocol,
			Stream:       s.Stream,
			Outcome:      s.Outcome,
			ClientKeyTag: s.ClientKeyTag,
			Provider:     s.Provider,
			Model:        s.Model,
			KeyLabel:     s.KeyLabel,
			DurMS:        s.DurMS,
			TTFTMS:       s.TTFTMS,
			Tokens:       s.Tokens,
		}
		b, err := json.Marshal(row)
		if err != nil {
			a.slim.Close()
			a.slim = nil
			return
		}
		b = append(b, '\n')
		if _, err := a.slim.Write(b); err != nil {
			a.slim.Close()
			a.slim = nil
		}
	}
}

// bookPastSampleLocked records a sample that arrived in an older hour: update
// rollup memory and write the updated cumulative row to rollup file.
func (a *Aggregator) bookPastSampleLocked(s Sample, hour time.Time) {
	key := s.key()
	if a.rollup[hour] == nil {
		a.rollup[hour] = make(map[dimsKey]Counters)
	}
	c := a.rollup[hour][key]
	c.addSample(s)
	a.rollup[hour][key] = c
	addRing(a.rings, s)

	// In last-wins semantics, appending the updated total ensures subsequent
	// loads reflect the merged state without an upsert.
	_ = appendJSONL(a.rollupPath(), countersRow(hour, key, c))
}

// addRing applies the ring admission rule (§4.2): ok + forwarded + measured
// TTFT only.
func addRing(rings map[ringKey]*ring, s Sample) {
	if s.Outcome != OutcomeOK || s.Provider == "" || s.TTFTMS == 0 {
		return
	}
	k := ringKey{s.Provider, s.Model, s.Stream}
	r := rings[k]
	if r == nil {
		r = &ring{}
		rings[k] = r
	}
	r.add(ringEntry{ts: s.TS, durMS: s.DurMS, ttftMS: s.TTFTMS, tokensOut: s.Tokens.Out})
}

// rollHourLocked closes the open hour: roll slim files older than newHour
// into rollup (from the file, not memory — §5), delete them, fold the
// live counters into the rollup map, and open the new hour's file.
func (a *Aggregator) rollHourLocked(newHour time.Time) {
	if a.slim != nil {
		a.slim.Close()
		a.slim = nil
	}
	old, _ := listSlimFiles(a.dir, newHour)
	for _, name := range old {
		if err := rollSlimFile(a.dir, name); err == nil {
			deleteSlim(a.dir, name)
		}
	}
	for k, c := range a.cur {
		if a.rollup[a.hour] == nil {
			a.rollup[a.hour] = make(map[dimsKey]Counters)
		}
		curC := a.rollup[a.hour][k]
		curC.add(c)
		a.rollup[a.hour][k] = curC
	}
	a.cur = make(map[dimsKey]Counters)
	a.hour = newHour
	if f, err := openSlim(a.dir, a.hour); err == nil {
		a.slim = f
	}
}

// Snapshot aggregates the whole ledger, fresh every call. Read path, holds
// the same coarse mutex (§4.3). Tests and callers needing an exact read use
// this; the /stats HTTP path uses CachedSnapshot.
func (a *Aggregator) Snapshot() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.snapshotLocked()
}

// CachedSnapshot is Snapshot for the /stats read path: it reuses the last
// fold for up to snapCacheTTL so N concurrent dashboard pollers cost one
// aggregation, not N. Record never invalidates it — a monitor tolerates a
// second of lag (§4.3). Uses the injectable clock so the window is testable.
func (a *Aggregator) CachedSnapshot() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.snapValid {
		if d := a.now().Sub(a.snapAt); d >= 0 && d < snapCacheTTL {
			return a.snapCache
		}
	}
	a.snapCache = a.snapshotLocked()
	a.snapAt = a.now()
	a.snapValid = true
	return a.snapCache
}

// Close releases the slim file handle and the advisory dir lock; the
// aggregator stops accepting meaningful work afterwards.
func (a *Aggregator) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var err error
	if a.slim != nil {
		err = a.slim.Close()
		a.slim = nil
	}
	if a.lock != nil {
		if cerr := a.lock.Close(); err == nil {
			err = cerr
		}
		a.lock = nil
	}
	return err
}
