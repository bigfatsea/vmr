package stats

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

//go:embed web/stats.html
var pageHTML []byte

//go:embed web/stats.js web/dashboard.js web/dashboard.css web/vendor/uplot-1.6.32.min.js web/vendor/uplot-1.6.32.min.css
var assetFS embed.FS

// PageHTML is the statistics page shell. It carries the console's shared
// asset markers; the server injects its console CSS/JS before serving, so
// this page keeps the same chrome (navigation, auth modal) as every other
// console page without this package depending on the server.
func PageHTML() []byte { return pageHTML }

// asset is one static file under /stats/assets/. Our own scripts and styles
// change with every build that touches them, so they are revalidated by
// content ETag; the
// vendored uPlot carries its version in the name and is cached for good.
type asset struct {
	body        []byte
	contentType string
	cache       string
	etag        string
}

var assets = func() map[string]asset {
	load := func(name, ct, cache string) asset {
		b, _ := assetFS.ReadFile("web/" + name) // embed guarantees presence
		sum := sha256.Sum256(b)
		return asset{body: b, contentType: ct, cache: cache, etag: `"` + hex.EncodeToString(sum[:8]) + `"`}
	}
	const immutable = "public, max-age=31536000, immutable"
	return map[string]asset{
		"stats.js":             load("stats.js", "text/javascript; charset=utf-8", "no-cache"),
		"dashboard.js":         load("dashboard.js", "text/javascript; charset=utf-8", "no-cache"),
		"dashboard.css":        load("dashboard.css", "text/css; charset=utf-8", "no-cache"),
		"uplot-1.6.32.min.js":  load("vendor/uplot-1.6.32.min.js", "text/javascript; charset=utf-8", immutable),
		"uplot-1.6.32.min.css": load("vendor/uplot-1.6.32.min.css", "text/css; charset=utf-8", immutable),
	}
}()

// Handler serves the statistics HTTP surface. A nil store (the statistics
// directory could not be opened) answers 503 on every data route.
type Handler struct {
	s   *Store
	now func() time.Time
}

// NewHandler wraps s; s may be nil.
func NewHandler(s *Store) *Handler {
	h := &Handler{s: s, now: time.Now}
	if s != nil {
		h.now = s.now
	}
	return h
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// queryNow is "now" for a request: the end of the current millisecond. Rows
// carry millisecond timestamps and a range is half-open, so a request that
// completed in the very millisecond a query runs would otherwise sit exactly
// on the excluded end and be missed by "up to now".
func (h *Handler) queryNow() time.Time {
	return h.now().Truncate(time.Millisecond).Add(time.Millisecond)
}

func (h *Handler) unavailable(w http.ResponseWriter) bool {
	if h == nil || h.s == nil {
		writeErr(w, http.StatusServiceUnavailable, "statistics are unavailable on this instance (the statistics directory could not be opened — see the startup log)")
		return true
	}
	return false
}

type schemaStep struct {
	Name    string `json:"name"`
	Seconds int64  `json:"seconds"`
}

// Schema serves GET /stats/schema: the dimension and metric registries the
// page builds every selector and tooltip from, plus the query limits.
func (h *Handler) Schema(w http.ResponseWriter, r *http.Request) {
	steps := make([]schemaStep, 0, len(ladder))
	for _, s := range ladder {
		steps = append(steps, schemaStep{Name: s.name, Seconds: int64(s.d / time.Second)})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dims":    dims,
		"metrics": metrics,
		"steps":   steps,
		"limits": map[string]int{
			"max_span_days": maxSpanDays, "max_buckets": maxBuckets, "auto_buckets": autoBuckets,
			"max_group_dims": maxGroupDims, "max_top": maxTop, "max_last": maxLast, "max_events": maxEvents,
		},
		"other_group": OtherGroup,
		"now":         fmtTime(h.now()),
	})
}

// Query serves GET /stats/query.
func (h *Handler) Query(w http.ResponseWriter, r *http.Request) {
	if h.unavailable(w) {
		return
	}
	q, err := ParseQuery(r.URL.Query(), h.queryNow())
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h.s.Query(q))
}

// Events serves GET /stats/events.
func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
	if h.unavailable(w) {
		return
	}
	v := r.URL.Query()
	q, err := ParseEventsQuery(v.Get, v["where"], h.queryNow())
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": h.s.Events(q)})
}

// Asset serves GET /stats/assets/{name}. Static and data-free, so it sits
// outside auth like every console page shell.
func Asset(w http.ResponseWriter, r *http.Request) {
	a, ok := assets[strings.TrimPrefix(r.URL.Path, "/stats/assets/")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", a.contentType)
	w.Header().Set("Cache-Control", a.cache)
	w.Header().Set("ETag", a.etag)
	if r.Header.Get("If-None-Match") == a.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(a.body)
}
