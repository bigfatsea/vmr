package stats

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DayOf is the day key an event is filed under on disk: the local day it
// completed in, which is what the live path does (Store.Record writes to the
// file open at completion). A rebuild must file by the same rule, otherwise
// a request that crossed midnight lands in a different file than the live
// path would have put it, and rebuilding next to live days double-counts or
// drops it. Queries read one extra day after the range to catch exactly
// these.
func DayOf(e Event) string {
	return dayKey(e.TS.Add(time.Duration(e.DurMS) * time.Millisecond))
}

// DayFileExists reports whether dir already holds a file for day.
func DayFileExists(dir, day string) bool {
	for _, ext := range []string{zstExt, plainExt} {
		if _, err := os.Stat(filepath.Join(dir, day+ext)); err == nil {
			return true
		}
	}
	return false
}

// WriteDay atomically writes events as the closed day file for day,
// replacing any existing one, and removes a stale plain copy. It refuses
// the current (open) day and later: that file belongs to the running
// process's live writer, and a rebuild must never race it. Writing needs no
// lock — a running process only ever appends to today's file, and it picks
// up a replaced closed day on its next query (dayRows re-stats every read).
func WriteDay(dir, day string, events []Event, now time.Time) error {
	if _, ok := parseDay(day); !ok {
		return fmt.Errorf("stats: bad day %q", day)
	}
	if day >= dayKey(now) {
		return fmt.Errorf("stats: refusing to write %s: only days before today can be rebuilt", day)
	}
	if len(events) == 0 {
		return fmt.Errorf("stats: refusing to write an empty %s", day)
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	sorted := append([]Event(nil), events...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TS.Before(sorted[j].TS) })
	dst := filepath.Join(dir, day+zstExt)
	if err := writeZstAtomic(dir, dst, func(w io.Writer) error {
		enc := json.NewEncoder(w)
		for _, e := range sorted {
			if err := enc.Encode(e); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, day+plainExt)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// WriteToday writes events as today's (open) day file. Today's file belongs
// to the running vmr, so this only proceeds when it can take the statistics
// directory lock — i.e. no vmr is running on this log_dir. That is the
// deploy window: stopping the old binary, rebuilding today from the audit
// log and starting the new one leaves no gap on upgrade day. The file is
// written plain (not compressed) because the next vmr start appends to it.
func WriteToday(dir string, events []Event, now time.Time) error {
	if len(events) == 0 {
		return fmt.Errorf("stats: refusing to write an empty today")
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	lock, err := acquireDirLock(dir)
	if err != nil {
		return fmt.Errorf("stats: cannot rebuild today while a vmr process holds %s — stop it first (closed days can be rebuilt while it runs): %w", dir, err)
	}
	if lock != nil {
		defer lock.Close()
	}
	day := dayKey(now)
	sorted := append([]Event(nil), events...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TS.Before(sorted[j].TS) })
	tmp, err := os.CreateTemp(dir, tempPrefix+"*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if err := tmp.Chmod(fileMode); err != nil {
		tmp.Close()
		return err
	}
	enc := json.NewEncoder(tmp)
	for _, e := range sorted {
		if err := enc.Encode(e); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, day+plainExt))
}
