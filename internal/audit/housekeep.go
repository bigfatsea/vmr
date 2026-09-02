// Ver 2026-07-08 16:20, by Sonnet 5

package audit

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// compressTempPattern is the temp file compressOne writes its zstd stream
// into. A unique name, not dst+".tmp": two concurrent compresses of the same
// archive must not interleave their streams into one fixed file and rename
// the wreck over the destination — with two vmr processes sharing a log_dir
// that is unrecoverable archive corruption. Same mechanism as every other
// atomic write site in the repo.
const compressTempPattern = ".audit-compress-*.tmp"

func isCompressTemp(name string) bool {
	return strings.HasPrefix(name, ".audit-compress-") && strings.HasSuffix(name, ".tmp")
}

// staleTempPrefixes are the atomic-write temp prefixes this package's sweep
// reclaims: compressOne's own .audit-compress-* (see compressTempPattern) and
// quota's .vmr-quota-* (internal/quota writes vmr-quota.json through
// os.CreateTemp — a crashed process leaves its temp behind exactly like a
// crashed compress does). The quota prefix is kept as a local copy rather than
// imported from internal/quota: audit is a leaf that must not couple to
// quota's internals for a three-word filename convention.
var staleTempPrefixes = []string{".audit-compress-", ".vmr-quota-"}

// isStaleTemp reports whether name is a temp file this sweep may reclaim —
// any process' atomic-write temp, audit's or quota's.
func isStaleTemp(name string) bool {
	if !strings.HasSuffix(name, ".tmp") {
		return false
	}
	for _, p := range staleTempPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// auditFileRE matches this package's own filename shape, plain or already
// compressed: vmr-audit-YYYY-MM-DD.jsonl[.zst]. Anything else in the audit
// directory (vmr.log, a config file, a stray artifact) is left alone.
var auditFileRE = regexp.MustCompile(`^vmr-audit-(\d{4}-\d{2}-\d{2})\.jsonl(\.zst)?$`)

// housekeep does two independent, best-effort passes over dir in a single
// directory listing:
//
// - compress every plain .jsonl file dated before today (never the file
// that might still be open for writing);
// - if a retention window is configured (RetentionDays > 0), delete any
// audit file — plain or compressed — dated before the cutoff.
//
// Both passes key off the date encoded in the filename, not file mtime or
// content, so this is a single bounded os.ReadDir (one entry per day ever
// kept) rather than a content scan or a stat-every-file walk. Errors are
// logged to stderr and otherwise swallowed: housekeeping must never take the
// process down or block a caller (see scheduleHousekeeping in audit.go).
func housekeep(dir string, today time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit: housekeeping: read %s: %v\n", dir, err)
		return
	}
	todayDate := today.Format("2006-01-02")
	retention := RetentionDays()
	var cutoff time.Time
	if retention > 0 {
		cutoff, _ = time.Parse("2006-01-02", todayDate)
		cutoff = cutoff.AddDate(0, 0, -retention)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// An atomic-write temp left behind by a crashed process (audit's
		// compressOne or quota's store): removable once it can't be a live
		// writer (a sweep finishes in minutes; the 24h bound is what keeps
		// this from racing one on a platform without the dir lock — see
		// lock_windows.go).
		if isStaleTemp(e.Name()) {
			if info, err := e.Info(); err == nil && info.ModTime().Before(today.Add(-24*time.Hour)) {
				if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
					fmt.Fprintf(os.Stderr, "audit: housekeeping: remove stale temp %s: %v\n", e.Name(), err)
				}
			}
			continue
		}
		m := auditFileRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		date, compressed := m[1], m[2] != ""
		name := e.Name()

		// Expired files are purged directly without wasting CPU/IO compressing
		// them first. Only retained past days get compressed.
		if retention > 0 {
			if t, err := time.Parse("2006-01-02", date); err == nil && t.Before(cutoff) {
				purgeOne(dir, name, retention)
				continue
			}
		}
		if !compressed && date != todayDate {
			compressOne(dir, name)
		}
	}
}

// compressOne replaces dir/name with a zstd-compressed dir/name+".zst",
// reporting whether dir/name+".zst" exists on disk when it returns. Written
// to a temp file and renamed into place so a crash mid-compress never leaves
// a truncated .zst nor loses the original — the source is only removed after
// the renamed copy is confirmed on disk.
func compressOne(dir, name string) bool {
	src := filepath.Join(dir, name)
	dst := src + ".zst"
	if _, err := os.Stat(dst); err == nil {
		// A previous sweep got as far as the rename but was interrupted
		// before removing the original (crash, kill -9): finish that up now
		// instead of leaving both copies on disk forever.
		if err := os.Remove(src); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "audit: compress %s: remove stale original: %v\n", name, err)
		}
		return true
	}
	tmp, err := os.CreateTemp(dir, compressTempPattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit: compress %s: temp: %v\n", name, err)
		return false
	}
	tmpName := tmp.Name()
	tmp.Close()
	if err := compressFile(src, tmpName); err != nil {
		fmt.Fprintf(os.Stderr, "audit: compress %s: %v\n", name, err)
		os.Remove(tmpName)
		return false
	}
	if err := os.Rename(tmpName, dst); err != nil {
		fmt.Fprintf(os.Stderr, "audit: compress %s: rename: %v\n", name, err)
		os.Remove(tmpName)
		return false
	}
	if err := os.Remove(src); err != nil {
		fmt.Fprintf(os.Stderr, "audit: compress %s: remove original: %v\n", name, err)
		return true // .zst is valid and in place; original cleanup will retry next sweep
	}
	return true
}

func compressFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	enc, err := zstd.NewWriter(out) // library default level: fast, still a large multi-MB match window
	if err != nil {
		return err
	}
	if _, err = io.Copy(enc, in); err != nil {
		enc.Close()
		return err
	}
	if err = enc.Close(); err != nil {
		return err
	}
	return out.Sync()
}

func purgeOne(dir, name string, retentionDays int) {
	path := filepath.Join(dir, name)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			// Already gone — e.g. the resume path removed the plain .jsonl
			// this same sweep is now purging the .zst sibling of. Not an
			// error, nothing to report.
			return
		}
		fmt.Fprintf(os.Stderr, "audit: retention: remove %s: %v\n", name, err)
		return
	}
	fmt.Fprintf(os.Stderr, "audit: retention: removed %s (older than %d days)\n", name, retentionDays)
}
