// Ver 2026-07-28 14:45, by Opus 5

// The loopback-only admin surface: /admin/status. Split out of server.go,
// which is the client-facing HTTP surface (auth, chat ingress, models) —
// this file answers questions about the *process*, not about a request.
package server

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"vmr/internal/buildinfo"
	"vmr/internal/core"
	"vmr/internal/health"
	"vmr/internal/router"
)

// instance is the process-level identity /admin/status reports so a caller
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
}

// WithInstance records who this process is, for /admin/status. Separate
// from New (rather than two more parameters) because only `vmr start` can
// answer these — every test constructs a Server without them, and their
// absence is a fully valid state: adminStatus just omits the fields.
// configPath is made absolute here, at the one call site that still knows
// the process's original working directory.
func (s *Server) WithInstance(configPath string, startedAt time.Time) *Server {
	if abs, err := filepath.Abs(configPath); err == nil {
		configPath = abs
	}
	s.inst = instance{configPath: configPath, startedAt: startedAt}
	return s
}

// vmrVersion is read once: the VCS stamp is baked into the binary and
// cannot change while it runs, and ReadBuildInfo walks an in-binary table
// on every call.
var vmrVersion = sync.OnceValue(func() string { return buildinfo.Read().Short() })

// adminStatus reports process identity, config freshness, per-endpoint
// health and live gauges. Loopback callers only.
func (s *Server) adminStatus(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		core.WriteError(w, http.StatusForbidden, "permission_error", "admin endpoints are loopback-only")
		return
	}
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
	limit, inFlight, waiting := s.rt.Concurrency()
	body := map[string]any{
		"instance": s.instanceBlock(snap, len(out)),
		"models":   out,
		"concurrency": map[string]any{
			"limit": limit, "in_flight": inFlight, "waiting": waiting,
		},
		// Sticky Model's live registry size. sticky.Registry.Len has carried
		// a "for /admin/status or tests" comment since it was written but was
		// never actually wired up — session affinity is otherwise entirely
		// invisible, which makes "why does this conversation keep landing on
		// the same endpoint" unanswerable without reading the audit log.
		"sticky": map[string]any{"entries": s.rt.Sticky.Len()},
		"time":   now,
	}
	if rl, ok := reloadBlock(s.rt.ReloadState()); ok {
		body["reload"] = rl
	}
	core.WriteJSON(w, http.StatusOK, body)
}

// instanceBlock assembles process identity plus the config-freshness
// verdict. Takes the caller's snapshot rather than loading its own: a hot
// reload landing between the two loads would report one config's listen
// address next to another's model count. models is the virtual-model count,
// already computed by the caller as the size of the health map.
func (s *Server) instanceBlock(snap *router.Snapshot, models int) map[string]any {
	inst := map[string]any{
		"pid":     os.Getpid(),
		"listen":  snap.Cfg.Listen,
		"models":  models,
		"version": vmrVersion(),
	}
	if s.inst.configPath != "" {
		inst["config"] = s.inst.configPath
	}
	if !s.inst.startedAt.IsZero() {
		inst["started_at"] = s.inst.startedAt
		inst["uptime_seconds"] = int64(time.Since(s.inst.startedAt).Seconds())
	}
	// config_stale answers the question a rejected — or never-triggered —
	// reload leaves open: is the file on disk still what this process is
	// serving? Compared against the last time the config was *successfully*
	// read (process start, or the last accepted reload), never against the
	// last attempt, or a rejected reload would clear its own warning.
	if s.inst.configPath != "" && !s.inst.startedAt.IsZero() {
		loadedAt := s.inst.startedAt
		if okAt := s.rt.ReloadState().OKAt; okAt.After(loadedAt) {
			loadedAt = okAt
		}
		if stale, mtime := router.ConfigStale(s.inst.configPath, loadedAt); !mtime.IsZero() {
			inst["config_mtime"] = mtime
			inst["config_stale"] = stale
		}
	}
	return inst
}

// reloadBlock renders the last hot-reload attempt. The bool is "there is
// something to report": no reload having happened yet is the normal steady
// state, and it should be an absent block rather than a row of zeroes that
// reads like a failure.
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
