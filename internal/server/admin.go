// Ver 2026-08-23 14:50, by Gemini

// The admin surface: /status. Split out of server.go,
// which is the client-facing HTTP surface (auth, chat ingress, models) —
// this file answers questions about the *process*, not about a request.
package server

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"vmr/internal/audit"
	"vmr/internal/buildinfo"
	"vmr/internal/core"
	"vmr/internal/fmtutil"
	"vmr/internal/health"
	"vmr/internal/imgprep"
	"vmr/internal/router"
)

// instance is the process-level identity /status reports so a caller
// who reached this port can tell *which* vmr answered: with several
// instances on one machine (different configs, different ports), the port
// is all a discovery tool has to go on, and everything else about the
// instance — above all which config.yaml it is actually serving — is
// otherwise unknowable from the outside. The config path is absolute
// because the process may have been started from any working directory,
// and the relative path it was given means nothing to whoever is reading.
type instance struct {
	configPath string
	startedAt  time.Time
	cwd        string
	executable string
}

// WithInstance records who this process is, for /status. Separate
// from New (rather than two more parameters) because only `vmr start` can
// answer these — every test constructs a Server without them, and their
// absence is a fully valid state: adminStatus just omits the fields.
// configPath is made absolute here, at the one call site that still knows
// the process's original working directory.
func (s *Server) WithInstance(configPath string, startedAt time.Time) *Server {
	if abs, err := filepath.Abs(configPath); err == nil {
		configPath = abs
	}
	cwd, _ := os.Getwd()
	exe, _ := os.Executable()
	s.inst = instance{
		configPath: configPath,
		startedAt:  startedAt,
		cwd:        cwd,
		executable: exe,
	}
	return s
}

// vmrVersion is read once: the VCS stamp is baked into the binary and
// cannot change while it runs, and ReadBuildInfo walks an in-binary table
// on every call.
var vmrVersion = sync.OnceValue(func() string { return buildinfo.Read().Short() })

// endpointStatus is one upstream endpoint inside a /status model entry:
// identity plus live health, and the endpoint's own capabilities /
// context-window override. capabilities/max_context_tokens are always
// emitted ([] / 0 = unconstrained — the same reading as
// core.Endpoint.HasCapability) so consumers can rely on the keys.
type endpointStatus struct {
	Endpoint string `json:"endpoint"`
	Protocol string `json:"protocol"`
	Priority int    `json:"priority"`
	health.Status
	Capabilities     []string `json:"capabilities"`
	MaxContextTokens int64    `json:"max_context_tokens"`
}

// modelStatus is one virtual-model × protocol face in /status's models
// array — the same name may exist in both protocol groups, and mixing their
// endpoints under one key reads as one model with double the endpoints.
// Capabilities is the union across endpoints and MaxContextTokens their
// maximum: what an agent configuring this virtual model may rely on overall;
// per-endpoint values live on each endpointStatus.
type modelStatus struct {
	ID               string           `json:"id"`
	Protocol         string           `json:"protocol"`
	Capabilities     []string         `json:"capabilities"`
	MaxContextTokens int64            `json:"max_context_tokens"`
	Endpoints        []endpointStatus `json:"endpoints"`
}

// adminStatus reports process identity, config freshness, system resources,
// traffic telemetry, per-endpoint health and quota gauges.
func (s *Server) adminStatus(w http.ResponseWriter, r *http.Request) {
	// Same rationale as /health's header — every field moves on every request
	// and the dashboard polls this endpoint.
	w.Header().Set("Cache-Control", "no-store")
	snap := s.rt.Snapshot()
	now := time.Now()
	// One entry per (virtual model, protocol), sorted — deterministic JSON
	// between polls of an unchanged config.
	models := []modelStatus{}
	for _, protocol := range core.SortedKeys(snap.Models) {
		for _, name := range core.SortedKeys(snap.Models[protocol]) {
			route := snap.Models[protocol][name]
			m := modelStatus{
				ID:               name,
				Protocol:         protocol,
				Capabilities:     unionCapabilities(route.Endpoints),
				MaxContextTokens: maxContextTokens(route.Endpoints),
				Endpoints:        make([]endpointStatus, 0, len(route.Endpoints)),
			}
			for _, ep := range route.Endpoints {
				m.Endpoints = append(m.Endpoints, endpointStatus{
					Endpoint:         ep.Name(),
					Protocol:         protocol,
					Priority:         ep.Priority,
					Status:           s.rt.Health.Status(ep.HealthKey(), now),
					Capabilities:     nonNilStrings(ep.Capabilities),
					MaxContextTokens: ep.MaxContextTokens,
				})
			}
			models = append(models, m)
		}
	}

	body := map[string]any{
		"instance":     s.instanceBlock(snap, len(models), instanceBaseURLs(requestScheme(r), r.Host)),
		"system":       s.systemBlock(snap),
		"traffic":      s.trafficBlock(),
		"models":       models,
		"current_time": now,
	}

	// Absent (not an empty array) when no quota.Registry is wired up or no
	// provider has quota: configured.
	if qs := s.rt.QuotaStatus(); len(qs) > 0 {
		body["quota"] = qs
	}
	if ab := s.auditBlock(snap); ab != nil {
		body["audit"] = ab
	}
	if icb := s.imageCacheBlock(snap); icb != nil {
		body["image_cache"] = icb
	}

	router.WriteJSON(w, http.StatusOK, body)
}

// instanceBlock assembles process identity, execution environment,
// concurrency throttles, config freshness / warnings, and the base_urls
// derived from the request that asked for this status.
func (s *Server) instanceBlock(snap *router.Snapshot, models int, baseURLs map[string]string) map[string]any {
	inst := map[string]any{
		"pid":        os.Getpid(),
		"listen":     snap.Cfg.Listen,
		"models":     models,
		"version":    vmrVersion(),
		"go_version": runtime.Version(),
		"os_arch":    runtime.GOOS + "/" + runtime.GOARCH,
		"base_urls":  baseURLs,
	}
	if s.inst.cwd != "" {
		inst["cwd"] = s.inst.cwd
	}
	if s.inst.executable != "" {
		inst["executable"] = s.inst.executable
	}
	if !s.inst.startedAt.IsZero() {
		inst["started_at"] = s.inst.startedAt
		inst["uptime_seconds"] = int64(time.Since(s.inst.startedAt).Seconds())
		inst["uptime"] = fmtutil.FmtDuration(time.Since(s.inst.startedAt))
	}

	limit, inFlight, waiting := s.rt.Concurrency()
	inst["concurrency"] = map[string]any{
		"limit": limit, "in_flight": inFlight, "waiting": waiting,
	}

	cfgBlock := map[string]any{}
	if s.inst.configPath != "" {
		cfgBlock["path"] = s.inst.configPath
	}
	if s.inst.configPath != "" && !s.inst.startedAt.IsZero() {
		loadedAt := s.inst.startedAt
		if okAt := s.rt.ReloadState().OKAt; okAt.After(loadedAt) {
			loadedAt = okAt
		}
		if stale, mtime := router.ConfigStale(s.inst.configPath, loadedAt); !mtime.IsZero() {
			cfgBlock["mtime"] = mtime
			cfgBlock["stale"] = stale
		}
	}
	if rl, ok := reloadBlock(s.rt.ReloadState()); ok {
		cfgBlock["reload"] = rl
	}
	if issues := snap.Cfg.Check(); len(issues) > 0 {
		cfgBlock["issues"] = issues
	}
	if len(cfgBlock) > 0 {
		inst["config"] = cfgBlock
	}
	return inst
}

// unionCapabilities merges the endpoints' effective capability sets into one
// sorted, deduplicated list — the model-level answer to "what can this model
// do". Empty means unconstrained (matches core.Endpoint.HasCapability's
// len==0 reading) but is still emitted, as [].
func unionCapabilities(endpoints []*core.Endpoint) []string {
	seen := make(map[string]bool)
	var out []string
	for _, ep := range endpoints {
		for _, c := range ep.Capabilities {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	sort.Strings(out) // deterministic JSON regardless of endpoint iteration order
	return nonNilStrings(out)
}

// nonNilStrings turns a nil slice into an empty one so it marshals as JSON
// [] instead of null — capabilities is documented as always an array.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// maxContextTokens is the largest context window any endpoint of the model
// offers; 0 means no endpoint declares a limit (unconstrained).
func maxContextTokens(endpoints []*core.Endpoint) int64 {
	var max int64
	for _, ep := range endpoints {
		if ep.MaxContextTokens > max {
			max = ep.MaxContextTokens
		}
	}
	return max
}

// instanceBaseURLs derives the client-facing base URL for each ingress
// protocol from the request itself, not from listen: whatever host the
// caller used to reach /status (and whether over TLS) is exactly what it
// should point its client at — 127.0.0.1 stays 127.0.0.1, localhost stays
// localhost, a LAN IP stays that IP. Display only; never consulted for
// auth or routing.
func instanceBaseURLs(scheme, host string) map[string]string {
	if host == "" {
		host = "127.0.0.1" // Host-less request fallback
	}
	base := scheme + "://" + host + "/v1/"
	return map[string]string{
		core.ProtocolOpenAICompletions: base,
		core.ProtocolAnthropicMessages: base,
		core.ProtocolOpenAIResponses:   base,
	}
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// systemBlock returns lightweight runtime memory, goroutine, and disk free space.
func (s *Server) systemBlock(snap *router.Snapshot) map[string]any {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	mem := map[string]any{
		"heap_alloc_bytes": ms.HeapAlloc,
		"heap_alloc":       fmtutil.FmtBytes(int64(ms.HeapAlloc)),
		"sys_bytes":        ms.Sys,
		"sys":              fmtutil.FmtBytes(int64(ms.Sys)),
	}

	targetDir := snap.Cfg.LogDir
	if targetDir == "" {
		targetDir = "."
	}
	freeBytes, _ := diskFreeSpace(targetDir)

	disk := map[string]any{
		"free_space_bytes": freeBytes,
		"free_space":       fmtutil.FmtBytes(int64(freeBytes)),
	}

	return map[string]any{
		"goroutines": runtime.NumGoroutine(),
		"memory":     mem,
		"disk":       disk,
	}
}

// trafficBlock extracts current in-memory traffic counters and sticky registry size.
func (s *Server) trafficBlock() map[string]any {
	tSnap := s.rt.Telemetry.Snapshot()
	return map[string]any{
		"requests": tSnap.Requests,
		"tokens":   tSnap.Tokens,
		"sticky":   map[string]any{"entries": s.rt.Sticky.Len()},
	}
}

// auditBlock summarizes audit logging status and on-disk size.
func (s *Server) auditBlock(snap *router.Snapshot) map[string]any {
	if snap.Cfg.LogDir == "" {
		return nil
	}
	todayFile := audit.ActiveLogPath(snap.Cfg.LogDir, time.Now())
	if s.audit != nil {
		todayFile = s.audit.Path()
	}
	var activeBytes int64
	if fi, err := os.Stat(todayFile); err == nil {
		activeBytes = fi.Size()
	}
	totalBytes := dirTotalSize(snap.Cfg.LogDir)

	return map[string]any{
		"enabled":                s.audit != nil,
		"active_file_size_bytes": activeBytes,
		"active_file_size":       fmtutil.FmtBytes(activeBytes),
		"total_size_bytes":       totalBytes,
		"total_size":             fmtutil.FmtBytes(totalBytes),
		"retention_days":         audit.RetentionDays(),
	}
}

// imageCacheBlock summarizes image downscale cache disk status.
func (s *Server) imageCacheBlock(snap *router.Snapshot) map[string]any {
	// enabled means "some model actually downscales (and therefore caches)
	// inline images": the global image_downscale, OR any virtual model with
	// a positive explicit override (config models[].image_downscale) - a
	// per-model override always wins over the global setting (see
	// ModelRoute.EffectiveImageDownscaleMaxPx), so global-off + one-model-on
	// still has the cache in use. Per-model settings themselves are config
	// detail (vmr check shows them); this is only the runtime yes/no.
	enabled := snap.Cfg.ImageDownscaleMaxPx > 0
	if !enabled {
		for _, byName := range snap.Models {
			for _, r := range byName {
				if r != nil && r.ImageDownscaleMaxPx != nil && *r.ImageDownscaleMaxPx > 0 {
					enabled = true
					break
				}
			}
			if enabled {
				break
			}
		}
	}
	cacheDir := snap.Cfg.ImageCacheDir
	var sizeBytes int64
	if cacheDir != "" {
		sizeBytes = dirTotalSize(cacheDir)
	}
	return map[string]any{
		"enabled":        enabled,
		"size_bytes":     sizeBytes,
		"size":           fmtutil.FmtBytes(sizeBytes),
		"capacity_bytes": imgprep.DefaultCacheCapBytes,
		"capacity":       fmtutil.FmtBytes(imgprep.DefaultCacheCapBytes),
	}
}

// dirTotalSize sums sizes of all regular files in dir without recursion.
func dirTotalSize(dir string) int64 {
	if dir == "" {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err == nil {
			total += info.Size()
		}
	}
	return total
}

// reloadBlock renders the last hot-reload attempt.
func reloadBlock(rs router.ReloadState) (map[string]any, bool) {
	if rs.At.IsZero() {
		return nil, false
	}
	m := map[string]any{
		"at":      rs.At,
		"trigger": rs.Trigger,
		"ok":      rs.OK,
		"count":   rs.Count,
	}
	if rs.Err != "" {
		m["error"] = rs.Err
	}
	return m, true
}
