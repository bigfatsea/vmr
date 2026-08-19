// Ver 2026-08-20 00:00, by Sonnet 5

package ctxgraph

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// CanonicalPath normalizes a scan-input path to its coordinate form: the
// basename with any compression suffix housekeeping may have added
// stripped, so a log file's identity survives its own plain→.zst rotation
// (internal/audit/housekeep.go's auditFileRE) and is independent of
// whether the caller passed an absolute or relative path.
//
// This is an identity string only — cache keys, published coordinates,
// filenames — never the string to pass to os.Open/audit.OpenLogFile, which
// need the real, resolvable (possibly relative, possibly absolute) path.
// Callers that need both keep the original path around for I/O and only
// normalize the copy used for identity.
func CanonicalPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".zst")
}

// ReqCoord formats the request-level coordinate every audit record gets:
// CanonicalPath(path) + ":" + line. line is the 1-based logical line
// audit.ForEachLine's counting already uses (see manifest.go's Line field),
// not a byte offset — no new convention, just a name for one that already
// existed as two separate fields. BuildManifest calls this once per record
// and stores the result as Manifest.Req; every other consumer of the
// coordinate (internal/report's RequestRow.Req, internal/reqdetail's
// filenames) should read that stored field rather than recomputing it from
// a Path they may not have in the same (possibly non-canonical) form.
func ReqCoord(path string, line int) string {
	return CanonicalPath(path) + ":" + strconv.Itoa(line)
}

// ReqHash8 is the coordinate's content-addressed short form used for
// deterministic filenames (internal/reqdetail's detail pages): md5 of the
// req string, first 4 bytes rendered as 8 hex characters — the same
// "count/short-hash" convention internal/report/session.go's toolsSig
// already uses for tool-set fingerprints. Not a full identity check (that
// remains the req string itself); short enough to read in a filename while
// still being effectively collision-free at this corpus's scale.
func ReqHash8(req string) string {
	sum := md5.Sum([]byte(req))
	return hex.EncodeToString(sum[:4])
}

// CheckPathCollisions fails loudly when two distinct input paths would
// normalize to the same CanonicalPath — the one scenario the "basename is
// the coordinate" choice does not defend against on its own (audit file
// names carry a date, so this does not happen in practice; see the
// architecture doc's coordinate-contract section). Scan and ScanCached
// each call this once at their own entry, rather than sharing one caller,
// since they are independent entry points with no shared call stack.
func CheckPathCollisions(paths []string) error {
	seen := make(map[string]string, len(paths))
	for _, p := range paths {
		key := CanonicalPath(p)
		if prior, ok := seen[key]; ok && prior != p {
			return fmt.Errorf("ctxgraph: path collision: %q and %q both normalize to coordinate basename %q — rename one or pass an unambiguous path", prior, p, key)
		}
		seen[key] = p
	}
	return nil
}
