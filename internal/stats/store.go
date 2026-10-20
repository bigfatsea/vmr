package stats

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

// File layout inside the statistics directory (<log_dir>/stats):
// YYYY-MM-DD.jsonl is the open day (append-only); a closed day is
// YYYY-MM-DD.jsonl.zst and never changes again unless `vmr stats rebuild
// -force` replaces it. 0600 files / 0700 dir — usage profiles get the same
// bar as the audit log.
const (
	fileMode   = 0o600
	dirMode    = 0o700
	plainExt   = ".jsonl"
	zstExt     = ".jsonl.zst"
	dayLayout  = "2006-01-02"
	tempPrefix = ".tmp-"
)

// maxCachedDays bounds how many closed-day partitions stay parsed in memory:
// enough for the widest single query (maxSpanDays plus the late-row day)
// with headroom, so a long-running process browsing old history evicts
// least-recently-used days instead of growing without bound.
const maxCachedDays = 120

// Store is the event log plus its in-memory partitions. One mutex covers
// the open day (its rows and file handle); a second, independent mutex
// covers the closed-day cache so a slow parse never blocks Record.
type Store struct {
	dir  string
	now  func() time.Time
	logf func(string, ...any)
	dict *dict

	mu         sync.Mutex
	day        string // the open day's key
	rows       []row  // the open day's rows, append-only
	file       *os.File
	lock       *os.File
	memoryOnly bool

	loadMu sync.Mutex
	cache  map[string]*closedDay
	tick   uint64

	bg sync.WaitGroup // background compressions; Close waits for them
}

type closedDay struct {
	rows []row
	path string
	size int64
	mod  time.Time
	used uint64
}

// Open opens (creating if needed) the statistics directory, takes its
// process lock, reloads today's file and compresses any closed day a crash
// left uncompressed. Losing the lock to another process is not an error:
// the store then counts in memory only and neither reads nor writes files.
func Open(dir string, logf func(string, ...any)) (*Store, error) {
	return openAt(dir, time.Now, logf)
}

func openAt(dir string, now func() time.Time, logf func(string, ...any)) (*Store, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	s := &Store{dir: dir, now: now, logf: logf, dict: newDict(), cache: map[string]*closedDay{}}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, err
	}
	s.day = dayKey(now())
	lock, err := acquireDirLock(dir)
	if err != nil {
		s.memoryOnly = true
		logf("WARN stats: %v — counting in memory only, nothing read from or written to disk", err)
		return s, nil
	}
	s.lock = lock
	if rows, err := s.readRows(filepath.Join(dir, s.day+plainExt)); err == nil {
		s.rows = rows
	}
	s.compressLeftovers()
	s.openFile()
	return s, nil
}

// Dir is the statistics directory.
func (s *Store) Dir() string { return s.dir }

// MemoryOnly reports whether another process holds the directory lock.
func (s *Store) MemoryOnly() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memoryOnly
}

func dayKey(t time.Time) string { return t.In(time.Local).Format(dayLayout) }

func parseDay(day string) (time.Time, bool) {
	t, err := time.ParseInLocation(dayLayout, day, time.Local)
	return t, err == nil
}

func (s *Store) openFile() {
	f, err := os.OpenFile(filepath.Join(s.dir, s.day+plainExt), os.O_CREATE|os.O_WRONLY|os.O_APPEND, fileMode)
	if err != nil {
		s.logf("WARN stats: open %s: %v — today's events stay in memory only", s.day+plainExt, err)
		return
	}
	s.file = f
}

// Record books one event: an in-memory append plus one whole-line write.
// The line goes to the file of the day the request completed in, while the
// event's own TS decides its bucket — queries read one extra day to catch a
// request that crossed midnight. A write failure closes the file for the
// rest of the day instead of retrying per request; memory still counts.
func (s *Store) Record(e Event) {
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	line = append(line, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := dayKey(s.now()); d != s.day {
		s.rollLocked(d)
	}
	s.rows = append(s.rows, s.dict.toRow(e))
	if s.file != nil {
		if _, err := s.file.Write(line); err != nil {
			s.logf("WARN stats: write %s: %v — today's remaining events stay in memory only", s.day+plainExt, err)
			s.file.Close()
			s.file = nil
		}
	}
}

// rollLocked closes the open day and starts the next. The closed day is
// compressed in the background; its rows are dropped from memory and will
// be re-read from the compressed file on first use.
func (s *Store) rollLocked(newDay string) {
	old := s.day
	if s.file != nil {
		s.file.Close()
		s.file = nil
	}
	s.day, s.rows = newDay, nil
	if s.memoryOnly {
		return
	}
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		s.compressDay(old)
	}()
	s.openFile()
}

// compressLeftovers compresses every plain closed-day file (a crash between
// day roll and compression leaves one behind).
func (s *Store) compressLeftovers() {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, plainExt) {
			continue
		}
		day := strings.TrimSuffix(name, plainExt)
		if _, ok := parseDay(day); !ok || day >= s.day {
			continue
		}
		s.bg.Add(1)
		go func() {
			defer s.bg.Done()
			s.compressDay(day)
		}()
	}
}

// compressDay replaces day.jsonl with day.jsonl.zst (temp file + rename,
// then remove the plain file). An existing .zst wins: it was renamed into
// place only after a complete write, so the plain file is the stale copy.
func (s *Store) compressDay(day string) {
	src := filepath.Join(s.dir, day+plainExt)
	dst := filepath.Join(s.dir, day+zstExt)
	if _, err := os.Stat(dst); err == nil {
		os.Remove(src)
		return
	}
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	if err := writeZstAtomic(s.dir, dst, func(w io.Writer) error {
		_, err := io.Copy(w, in)
		return err
	}); err != nil {
		s.logf("WARN stats: compress %s: %v", day+plainExt, err)
		return
	}
	os.Remove(src)
}

// writeZstAtomic writes a zstd file via a temp file in dir and renames it
// over dst, so a reader never sees a partial file.
func writeZstAtomic(dir, dst string, write func(io.Writer) error) (err error) {
	tmp, err := os.CreateTemp(dir, tempPrefix+"*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(fileMode); err != nil {
		return err
	}
	enc, err := zstd.NewWriter(tmp, zstd.WithEncoderConcurrency(1))
	if err != nil {
		return err
	}
	if err = write(enc); err != nil {
		enc.Close()
		return err
	}
	if err = enc.Close(); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// readRows parses one day file (plain or zstd). Torn or corrupt lines are
// skipped; a missing file is an error the caller treats as "no data".
func (s *Store) readRows(path string) ([]row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".zst") {
		dec, err := zstd.NewReader(f, zstd.WithDecoderConcurrency(1))
		if err != nil {
			return nil, err
		}
		defer dec.Close()
		r = dec
	}
	var rows []row
	err = eachLine(r, func(line []byte) {
		var e Event
		if json.Unmarshal(line, &e) == nil {
			rows = append(rows, s.dict.toRow(e))
		}
	})
	return rows, err
}

func eachLine(r io.Reader, fn func([]byte)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if b := sc.Bytes(); len(b) > 0 {
			fn(b)
		}
	}
	return sc.Err()
}

// dayRows returns the rows of one day: the open day straight from memory
// (a slice-header snapshot — safe to scan without the lock because rows are
// never mutated after append), a closed day from the cache, re-reading the
// file whenever its size or mtime changed (that is how an online rebuild
// reaches a running process without any signalling).
func (s *Store) dayRows(day string) []row {
	s.mu.Lock()
	if day == s.day {
		rows := s.rows[:len(s.rows):len(s.rows)]
		s.mu.Unlock()
		return rows
	}
	memOnly := s.memoryOnly
	s.mu.Unlock()
	if memOnly {
		return nil
	}

	path := filepath.Join(s.dir, day+zstExt)
	fi, err := os.Stat(path)
	if err != nil {
		path = filepath.Join(s.dir, day+plainExt)
		fi, err = os.Stat(path)
	}
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	if err != nil {
		delete(s.cache, day)
		return nil
	}
	s.tick++
	if c := s.cache[day]; c != nil && c.path == path && c.size == fi.Size() && c.mod.Equal(fi.ModTime()) {
		c.used = s.tick
		return c.rows
	}
	rows, err := s.readRows(path)
	if err != nil {
		delete(s.cache, day)
		return nil
	}
	s.cache[day] = &closedDay{rows: rows, path: path, size: fi.Size(), mod: fi.ModTime(), used: s.tick}
	s.evictLocked()
	return rows
}

func (s *Store) evictLocked() {
	if len(s.cache) <= maxCachedDays {
		return
	}
	days := make([]string, 0, len(s.cache))
	for d := range s.cache {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return s.cache[days[i]].used < s.cache[days[j]].used })
	for _, d := range days[:len(days)-maxCachedDays] {
		delete(s.cache, d)
	}
}

// today is the open day's key as of now (not as of the last Record), so a
// query after midnight on an idle instance still sees the right "today".
func (s *Store) today() string { return dayKey(s.now()) }

// Close stops writing, waits for background compressions and releases the
// directory lock.
func (s *Store) Close() error {
	s.mu.Lock()
	var err error
	if s.file != nil {
		err = s.file.Close()
		s.file = nil
	}
	s.mu.Unlock()
	s.bg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock != nil {
		if cerr := s.lock.Close(); err == nil {
			err = cerr
		}
		s.lock = nil
	}
	return err
}
