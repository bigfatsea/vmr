// Ver 2026-08-31

// /reports/ static hosting of analyze-produced reports and dashboard
// skeletons (analytics.serve opt-in). Deliberately reads ONLY the
// filesystem: the analytics half's packages are never imported here —
// serve_dir is a plain string config field, and the reports themselves are
// just files on disk (two-halves-one-contract, server side; the import
// boundary is pinned in archtest's forbiddenImports).
package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"vmr/internal/router"
)

// missingDirWarner throttles the "serve_dir missing" log to once per
// directory: the analytics products are regenerable derivatives — "not
// generated yet" is normal for days at a time, and logging it on every
// request would only flood the log without telling the operator anything
// new. The flag re-arms when a hot reload points serve_dir somewhere else,
// so the new directory's first miss is announced again.
type missingDirWarner struct {
	mu      sync.Mutex
	lastDir string
	warned  bool
}

var reportsMissing = &missingDirWarner{}

func (m *missingDirWarner) warn(dir string, logf func(string, ...any)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.warned && m.lastDir == dir {
		return
	}
	m.warned, m.lastDir = true, dir
	if logf != nil {
		logf("reports: serve_dir %s does not exist — analytics products not generated yet (run `vmr analyze`); /reports/* answers 404 until then (this notice logs once per directory)", dir)
	}
}

// reportsContentTypes maps the extensions /reports/ serves to an explicit
// Content-Type. Everything is set before http.ServeContent, because the
// extension→MIME table is thin for this file family (.jsonl in particular
// has no mapping on every platform and would fall back to sniffing or
// application/octet-stream — the latter makes browsers download instead of
// rendering, which breaks the fetch-based dashboards). Extensions outside
// this table are not served at all: the analyze suite only produces these,
// and "unknown extension" is exactly the shape of a file that was never a
// report.
var reportsContentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".json":  "application/json",
	".jsonl": "application/jsonl",
	".md":    "text/markdown; charset=utf-8",
}

// isReportsSkeleton reports whether name is a dashboard skeleton page.
// Skeleton pages carry zero business data (same contract as status.html —
// the page's JS fetches the data and prompts for credentials itself), so
// they are served unauthenticated; every data-bearing extension requires a
// valid API key.
func isReportsSkeleton(name string) bool {
	return strings.HasSuffix(name, ".html")
}

// mountReports registers /reports and /reports/ when analytics.serve is on.
// Route NOT registered otherwise — a request to /reports/* then hits the
// mux's plain 404, which is the documented opt-out shape ("no route" must
// be indistinguishable from "not a vmr feature").
func (s *Server) mountReports(mux *http.ServeMux) {
	snap := s.rt.Snapshot()
	if snap == nil || !snap.Cfg.Analytics.Serve {
		return
	}
	s.reports = &reportsState{dir: snap.Cfg.Analytics.ServeDir}
	mux.HandleFunc("GET /reports", s.reportsHandler)
	mux.HandleFunc("GET /reports/", s.reportsHandler)
}

// reportsState captures the serve-time directory resolution once per mount,
// not per request. dir is the config value as written (possibly relative);
// abs resolves it against the process working directory at mount time —
// `vmr start` has exactly one meaningful cwd, and re-stat'ing a relative
// path on every request would silently follow a chdir the process never
// promised not to do. A hot reload that changes serve_dir replaces this
// whole state via the next mountReports call.
type reportsState struct {
	dir string
	abs string
}

func (rs *reportsState) resolve() string {
	if filepath.IsAbs(rs.dir) {
		return filepath.Clean(rs.dir)
	}
	if rs.abs != "" {
		return rs.abs
	}
	cwd, err := os.Getwd()
	if err != nil {
		// No cwd to anchor to: fall back to the literal relative path and
		// let the OS resolve it the same way it would have anyway.
		return filepath.Clean(rs.dir)
	}
	return filepath.Join(cwd, rs.dir)
}

// reportsHandler serves one file under serve_dir.
//
// Auth model (D9, layered):
//  1. No api_keys configured at all → 403 for EVERYTHING under /reports/*,
//     skeletons included. This is deliberately NOT s.auth (whose len==0
//     means "door open" for the routing API): reports carry full
//     conversation bodies, and "no key configured" on a LAN-exposed
//     listener must degrade to nothing here, not to open access.
//  2. Skeleton pages (.html) → no per-request credential needed.
//  3. Data files (.json/.jsonl/.md) → valid key required, checked against
//     the same snapshot the routing auth uses.
//
// Path handling (D9, defense in depth — each check has its own test):
//   - the URL subpath is cleaned and joined onto the resolved serve_dir,
//     then required to still sit under it (kills absolute-path escapes and
//     any remaining traversal the mux's own cleaning didn't neutralize);
//   - every path component from serve_dir down to the target is Lstat'd:
//     any symlink anywhere in the chain is refused (an in-tree symlink is
//     the one way to point the prefix check at a file outside serve_dir);
//   - directories 404, always. No directory listing: requests/details/
//     alone holds thousands of files, and an auto-generated listing of
//     that is a DoS-shaped response no client asked for.
func (s *Server) reportsHandler(w http.ResponseWriter, r *http.Request) {
	snap := s.rt.Snapshot()
	if snap == nil {
		router.WriteError(w, http.StatusServiceUnavailable, "service_unavailable", "router not yet initialized")
		return
	}
	if len(snap.Cfg.APIKeys) == 0 {
		router.WriteError(w, http.StatusForbidden, "permission_error",
			"/reports/ requires api_keys to be configured — reports carry full conversation bodies and are never served without auth")
		return
	}
	rs := s.reports
	if rs == nil {
		http.NotFound(w, r)
		return
	}
	root := rs.resolve()

	// Directory-missing fast path, BEFORE auth: the products being absent
	// is a global state, and answering 404 for it (instead of 401) keeps
	// "not generated yet" distinguishable from "wrong key" for the
	// dashboard's own error handling.
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		reportsMissing.warn(rs.dir, s.rt.Logf)
		http.NotFound(w, r)
		return
	}

	sub := strings.TrimPrefix(r.URL.Path, "/reports")
	sub = strings.TrimPrefix(sub, "/")
	if sub == "" {
		// /reports or /reports/ itself: no listing, no index page.
		http.NotFound(w, r)
		return
	}

	// Auth check comes after the trivial 404s but before any filesystem
	// work on the target: an unauthorized caller must not be able to use
	// status-code differences (404 vs 403) to learn whether a given data
	// file exists.
	name := filepath.Base(sub)
	if !isReportsSkeleton(name) {
		if _, ok := s.authenticateWithSnap(r, snap); !ok {
			router.WriteError(w, http.StatusUnauthorized, "authentication_error", "invalid or missing API key")
			return
		}
	}

	clean := filepath.Clean("/" + sub) // re-anchor: strips any ".." that survived the mux
	rel := strings.TrimPrefix(clean, "/")
	target := filepath.Join(root, rel)
	if rel == "" || !strings.HasPrefix(target, root+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	if err := reportsCheckSymlinks(root, target); err != nil {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		// TOCTOU guard: the file may have been swapped for a directory (or
		// vanished) between Lstat and Open. Serving nothing is the only
		// safe answer either way.
		http.NotFound(w, r)
		return
	}
	ct, ok := reportsContentTypes[strings.ToLower(filepath.Ext(name))]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, name, st.ModTime(), f)
}

// reportsCheckSymlinks rejects any symlink on the path from root down to
// target. os.Lstat on the final component alone is not enough: an
// intermediate directory being a symlink would relabel everything below it
// while the final component still reads as a regular file. Walking every
// component is O(depth), depth is at most 3 in the real layout
// (requests/details/..., journeys/details/...), and the walk stops at the
// first missing component — deep paths are the exception, not the rule.
// root itself is intentionally NOT checked: the operator chose serve_dir,
// and a symlinked serve_dir is an operator decision, not an escape.
func reportsCheckSymlinks(root, target string) error {
	cur := target
	for {
		if cur == root || len(cur) <= len(root) {
			return nil
		}
		st, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				return nil // missing tail: os.Open reports it 404 below
			}
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return os.ErrInvalid
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return nil // reached filesystem root without hitting root: escape
		}
		cur = parent
	}
}
