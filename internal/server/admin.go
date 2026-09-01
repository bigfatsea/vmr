// Package server provides the HTTP admin and routing surface.
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
	"vmr/internal/sysinfo"
)

type (
	instance struct {
		configPath, cwd, executable string
		startedAt                   time.Time
	}
	endpointStatus struct {
		Endpoint string `json:"endpoint"`
		Protocol string `json:"protocol"`
		Priority int    `json:"priority"`
		health.Status
		Capabilities     []string `json:"capabilities"`
		MaxContextTokens int64    `json:"max_context_tokens"`
	}
	modelStatus struct {
		ID               string           `json:"id"`
		Protocol         string           `json:"protocol"`
		Capabilities     []string         `json:"capabilities"`
		MaxContextTokens int64            `json:"max_context_tokens"`
		Endpoints        []endpointStatus `json:"endpoints"`
	}
	metricSample struct {
		at  time.Time
		val uint64
	}
)

const statusMetricsTTL = 30 * time.Second

var (
	vmrVersion    = sync.OnceValue(func() string { return buildinfo.Read().Short() })
	metricsMu     sync.Mutex
	statusMetrics = map[string]metricSample{}
)

func (s *Server) WithInstance(configPath string, startedAt time.Time) *Server {
	if abs, err := filepath.Abs(configPath); err == nil {
		configPath = abs
	}
	cwd, _ := os.Getwd()
	exe, _ := os.Executable()
	s.inst = instance{configPath: configPath, startedAt: startedAt, cwd: cwd, executable: exe}
	return s
}

func getCached(key string, fetch func() uint64) uint64 {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	e, ok := statusMetrics[key]
	if !ok || time.Since(e.at) >= statusMetricsTTL {
		e = metricSample{time.Now(), fetch()}
		statusMetrics[key] = e
	}
	return e.val
}

func cachedDirTotalSize(dir string) int64 {
	if dir == "" {
		return 0
	}
	return int64(getCached("d:"+dir, func() uint64 { return uint64(sysinfo.DirTotalSize(dir)) }))
}

func cachedDiskFreeSpace(dir string) uint64 {
	return getCached("f:"+dir, func() uint64 { return sysinfo.DiskFreeBytes(dir) })
}

func (s *Server) adminStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	snap, now, t := s.rt.Snapshot(), time.Now(), s.rt.Telemetry.Snapshot()
	body := map[string]any{
		"instance": s.instanceBlock(snap, instanceBaseURLs(requestScheme(r), r.Host)), "system": s.systemBlock(snap),
		"traffic": map[string]any{"requests": t.Requests, "tokens": t.Tokens, "sticky": map[string]any{"entries": s.rt.Sticky.Len()}},
		"models":  statusModels(snap, now, s.rt.Health), "current_time": now,
	}
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

func statusModels(snap *router.Snapshot, now time.Time, h *health.Registry) (models []modelStatus) {
	for _, p := range core.SortedKeys(snap.Models) {
		for _, name := range core.SortedKeys(snap.Models[p]) {
			route := snap.Models[p][name]
			eps, seen, caps, maxTokens := make([]endpointStatus, len(route.Endpoints)), map[string]bool{}, []string{}, int64(0)
			for i, ep := range route.Endpoints {
				maxTokens = max(maxTokens, ep.MaxContextTokens)
				all := append([]string{}, ep.Capabilities...)
				for _, c := range all {
					if !seen[c] {
						seen[c], caps = true, append(caps, c)
					}
				}
				eps[i] = endpointStatus{
					Endpoint: ep.Name(), Protocol: p, Priority: ep.Priority,
					Status: h.Status(ep.HealthKey(), now), Capabilities: all, MaxContextTokens: ep.MaxContextTokens,
				}
			}
			sort.Strings(caps)
			models = append(models, modelStatus{ID: name, Protocol: p, Capabilities: caps, MaxContextTokens: maxTokens, Endpoints: eps})
		}
	}
	return models
}

func (s *Server) instanceBlock(snap *router.Snapshot, baseURLs map[string]string) map[string]any {
	var modelCount int
	for _, m := range snap.Models {
		modelCount += len(m)
	}
	limit, inFlight, waiting := s.rt.Concurrency()
	inst := map[string]any{
		"pid": os.Getpid(), "listen": snap.Cfg.Listen, "models": modelCount,
		"version": vmrVersion(), "go_version": runtime.Version(), "os_arch": runtime.GOOS + "/" + runtime.GOARCH,
		"base_urls": baseURLs, "concurrency": map[string]any{"limit": limit, "in_flight": inFlight, "waiting": waiting},
	}
	if s.inst.cwd != "" {
		inst["cwd"] = s.inst.cwd
	}
	if s.inst.executable != "" {
		inst["executable"] = s.inst.executable
	}
	if !s.inst.startedAt.IsZero() {
		up := time.Since(s.inst.startedAt)
		inst["started_at"], inst["uptime_seconds"], inst["uptime"] = s.inst.startedAt, int64(up.Seconds()), fmtutil.FmtDuration(up)
	}
	cfg := map[string]any{}
	if s.inst.configPath != "" {
		cfg["path"] = s.inst.configPath
		loaded := s.inst.startedAt
		if okAt := s.rt.ReloadState().OKAt; okAt.After(loaded) {
			loaded = okAt
		}
		if stale, mtime := router.ConfigStale(s.inst.configPath, loaded); !loaded.IsZero() && !mtime.IsZero() {
			cfg["mtime"], cfg["stale"] = mtime, stale
		}
	}
	if rl, ok := reloadBlock(s.rt.ReloadState()); ok {
		cfg["reload"] = rl
	}
	if issues := snap.Cfg.Check(); len(issues) > 0 {
		cfg["issues"] = issues
	}
	if len(cfg) > 0 {
		inst["config"] = cfg
	}
	return inst
}

func instanceBaseURLs(scheme, host string) map[string]string {
	if host == "" {
		host = "127.0.0.1"
	}
	b := scheme + "://" + host + "/v1/"
	return map[string]string{core.ProtocolOpenAICompletions: b, core.ProtocolAnthropicMessages: b, core.ProtocolOpenAIResponses: b}
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func (s *Server) systemBlock(snap *router.Snapshot) map[string]any {
	heapAlloc, sys := sysinfo.ReadMemAlloc()
	free := cachedDiskFreeSpace(snap.Cfg.LogDir)
	return map[string]any{
		"goroutines": runtime.NumGoroutine(),
		"memory":     map[string]any{"heap_alloc_bytes": heapAlloc, "heap_alloc": fmtutil.FmtBytes(int64(heapAlloc)), "sys_bytes": sys, "sys": fmtutil.FmtBytes(int64(sys))},
		"disk":       map[string]any{"free_space_bytes": free, "free_space": fmtutil.FmtBytes(int64(free))},
	}
}

func (s *Server) auditBlock(snap *router.Snapshot) map[string]any {
	if snap.Cfg.LogDir == "" {
		return nil
	}
	path := audit.ActiveLogPath(snap.Cfg.LogDir, time.Now())
	if s.audit != nil {
		path = s.audit.Path()
	}
	var active int64
	if fi, err := os.Stat(path); err == nil {
		active = fi.Size()
	}
	total := cachedDirTotalSize(snap.Cfg.LogDir)
	return map[string]any{
		"enabled": s.audit != nil, "active_file_size_bytes": active, "active_file_size": fmtutil.FmtBytes(active),
		"total_size_bytes": total, "total_size": fmtutil.FmtBytes(total), "retention_days": audit.RetentionDays(),
	}
}

func (s *Server) imageCacheBlock(snap *router.Snapshot) map[string]any {
	enabled := snap.Cfg.ImageDownscaleMaxPx > 0
	for _, byName := range snap.Models {
		for _, r := range byName {
			if r != nil && r.ImageDownscaleMaxPx != nil && *r.ImageDownscaleMaxPx > 0 {
				enabled = true
			}
		}
	}
	size := cachedDirTotalSize(snap.Cfg.ImageCacheDir)
	return map[string]any{
		"enabled":    enabled,
		"size_bytes": size, "size": fmtutil.FmtBytes(size),
		"capacity_bytes": imgprep.DefaultCacheCapBytes, "capacity": fmtutil.FmtBytes(imgprep.DefaultCacheCapBytes),
	}
}

func reloadBlock(rs router.ReloadState) (map[string]any, bool) {
	if rs.At.IsZero() {
		return nil, false
	}
	m := map[string]any{"at": rs.At, "trigger": rs.Trigger, "ok": rs.OK, "count": rs.Count}
	if rs.Err != "" {
		m["error"] = rs.Err
	}
	return m, true
}
