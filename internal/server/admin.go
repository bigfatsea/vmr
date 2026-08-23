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
	"sync"
	"time"

	"vmr/internal/audit"
	"vmr/internal/buildinfo"
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

// adminStatus reports process identity, config freshness, system resources,
// traffic telemetry, per-endpoint health and quota gauges.
func (s *Server) adminStatus(w http.ResponseWriter, r *http.Request) {
	// Same rationale as /health's header — every field moves on every request
	// and the dashboard polls this endpoint.
	w.Header().Set("Cache-Control", "no-store")
	snap := s.rt.Snapshot()
	now := time.Now()
	type epStatus struct {
		Endpoint string `json:"endpoint"`
		Protocol string `json:"protocol"`
		Priority int    `json:"priority"`
		health.Status
	}
	// Keyed "name [protocol]": the same virtual-model name may exist in
	// both protocol groups, and mixing their endpoints under one
	// key reads as one model with double the endpoints.
	out := map[string][]epStatus{}
	for protocol, byName := range snap.Models {
		for name, route := range byName {
			key := name + " [" + protocol + "]"
			for _, ep := range route.Endpoints {
				out[key] = append(out[key], epStatus{
					Endpoint: ep.Name(),
					Protocol: protocol,
					Priority: ep.Priority,
					Status:   s.rt.Health.Status(ep.HealthKey(), now),
				})
			}
		}
	}

	body := map[string]any{
		"instance":     s.instanceBlock(snap, len(out)),
		"system":       s.systemBlock(snap),
		"traffic":      s.trafficBlock(),
		"models":       out,
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
// concurrency throttles, and config freshness / warnings.
func (s *Server) instanceBlock(snap *router.Snapshot, models int) map[string]any {
	inst := map[string]any{
		"pid":        os.Getpid(),
		"listen":     snap.Cfg.Listen,
		"models":     models,
		"version":    vmrVersion(),
		"go_version": runtime.Version(),
		"os_arch":    runtime.GOOS + "/" + runtime.GOARCH,
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
	todayFile := filepath.Join(snap.Cfg.LogDir, "vmr-audit-"+time.Now().Format("2006-01-02")+".jsonl")
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
