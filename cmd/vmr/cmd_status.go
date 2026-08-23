// Ver 2026-08-23 14:52, by Gemini
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"vmr/internal/config"
)

// quotaTokenWeightsView mirrors router.TokenWeightsView's JSON shape — a
// named type (rather than inlining it, the way every other nested struct in
// statusResponse below is) specifically because its zero-config default
// value gets compared against a literal in the rendering code further down;
// an anonymous struct there would mean hand-copying this same field list a
// second time as a struct literal, with no compiler help keeping the two in
// sync if a field is ever added or renamed.
type quotaTokenWeightsView struct {
	InFresh    float64 `json:"in_fresh"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Out        float64 `json:"out"`
}

// allDefaultTokenWeights is the all-1.0 zero-config default every
// TokenWeights view resolves to when an account never configured
// token_weights — see printStatus's quota loop below, the one place this
// is compared against.
var allDefaultTokenWeights = quotaTokenWeightsView{InFresh: 1, CacheRead: 1, CacheWrite: 1, Out: 1}

// statusResponse is the /status payload, as much of it as this
// command renders.
type statusResponse struct {
	Instance struct {
		PID         int    `json:"pid"`
		Listen      string `json:"listen"`
		Models      int    `json:"models"`
		Uptime      int64  `json:"uptime_seconds"`
		UptimeStr   string `json:"uptime"`
		Started     string `json:"started_at"`
		Version     string `json:"version"`
		GoVersion   string `json:"go_version"`
		OSArch      string `json:"os_arch"`
		Cwd         string `json:"cwd"`
		Executable  string `json:"executable"`
		Concurrency struct {
			Limit    int   `json:"limit"`
			InFlight int64 `json:"in_flight"`
			Waiting  int64 `json:"waiting"`
		} `json:"concurrency"`
		Config struct {
			Path   string    `json:"path"`
			Mtime  time.Time `json:"mtime"`
			Stale  bool      `json:"stale"`
			Reload *struct {
				At      time.Time `json:"at"`
				Trigger string    `json:"trigger"`
				OK      bool      `json:"ok"`
				Err     string    `json:"error"`
				Count   int       `json:"count"`
			} `json:"reload,omitempty"`
			Issues []config.Issue `json:"issues,omitempty"`
		} `json:"config"`
	} `json:"instance"`
	System struct {
		Goroutines int `json:"goroutines"`
		Memory     struct {
			HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
			HeapAlloc      string `json:"heap_alloc"`
			SysBytes       uint64 `json:"sys_bytes"`
			Sys            string `json:"sys"`
		} `json:"memory"`
		Disk struct {
			FreeSpaceBytes uint64 `json:"free_space_bytes"`
			FreeSpace      string `json:"free_space"`
		} `json:"disk"`
	} `json:"system"`
	Traffic struct {
		Requests struct {
			Total      uint64            `json:"total"`
			ByProtocol map[string]uint64 `json:"by_protocol"`
			ByStatus   map[string]uint64 `json:"by_status"`
		} `json:"requests"`
		Tokens struct {
			Total struct {
				In         uint64 `json:"in"`
				CacheWrite uint64 `json:"cache_write"`
				CacheRead  uint64 `json:"cache_read"`
				Reasoning  uint64 `json:"reasoning"`
				Out        uint64 `json:"out"`
			} `json:"total"`
		} `json:"tokens"`
		Sticky struct {
			Entries int `json:"entries"`
		} `json:"sticky"`
	} `json:"traffic"`
	Models map[string][]struct {
		Endpoint      string    `json:"endpoint"`
		Protocol      string    `json:"protocol"`
		Priority      int       `json:"priority"`
		Fails         int       `json:"consecutive_failures"`
		CooldownUntil time.Time `json:"cooldown_until"`
		LastError     string    `json:"last_error"`
		Available     bool      `json:"available"`
		Probing       bool      `json:"probing"`
		// Serving is health.Status.Serving — see its doc comment. Used below
		// instead of Available (which stays true through the entire half-open
		// window) to decide the COOLDOWN vs half-open vs ok split. A bare bool,
		// not a pointer: CLI and server must be the same vmr version (a shape
		// mismatch hard-errors at instance.config long before this field is
		// reached), so there is no older-server case to defend against.
		Serving bool `json:"serving"`
	} `json:"models"`
	// Quota is absent (nil slice) from the JSON entirely when no provider
	// has quota: configured — server/admin.go only sets the "quota" key
	// when router.Router.QuotaStatus() returns a non-empty slice, so a
	// plain instance sees no behavior change here either.
	Quota []struct {
		Provider         string                `json:"provider"`
		Metric           string                `json:"metric"`
		Every            string                `json:"every"`
		Models           []string              `json:"models,omitempty"`
		Role             string                `json:"role"`
		Amount           float64               `json:"amount"`
		Used             float64               `json:"used"`
		Pct              float64               `json:"pct"`
		Headroom         float64               `json:"headroom"`
		PeriodStart      time.Time             `json:"period_start"`
		PeriodEndsAt     time.Time             `json:"period_ends_at"`
		EstimatedPct     float64               `json:"estimated_pct"`
		TokenWeights     quotaTokenWeightsView `json:"token_weights"`
		ModelMultipliers map[string]float64    `json:"model_multipliers,omitempty"`
	} `json:"quota"`
	Audit *struct {
		Enabled             bool   `json:"enabled"`
		ActiveFileSizeBytes int64  `json:"active_file_size_bytes"`
		ActiveFileSize      string `json:"active_file_size"`
		TotalSizeBytes      int64  `json:"total_size_bytes"`
		TotalSize           string `json:"total_size"`
		RetentionDays       int    `json:"retention_days"`
	} `json:"audit,omitempty"`
	ImageCache *struct {
		Enabled       bool   `json:"enabled"`
		SizeBytes     int64  `json:"size_bytes"`
		Size          string `json:"size"`
		CapacityBytes int64  `json:"capacity_bytes"`
		Capacity      string `json:"capacity"`
	} `json:"image_cache,omitempty"`
	CurrentTime string `json:"current_time"`
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	path := fs.String("c", "config.yaml", "path to config file")
	addr := fs.String("addr", "", "query this host:port directly, without loading a config to find it (for instances whose config this machine may not have)")
	keyFlag := fs.String("key", "", "API key to authenticate with (if the instance requires auth)")
	brief := fs.Bool("brief", false, "print one tab-separated line — pid, listen, uptime, models, version, config-stale, config — instead of the health listing (vmr.sh ps uses this)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	target := *addr
	apiKey := *keyFlag
	if target == "" {
		cfg, err := config.Load(*path)
		if err != nil {
			return err
		}
		target = cfg.Listen
		if apiKey == "" && len(cfg.APIKeys) > 0 {
			apiKey = cfg.APIKeys[0]
		}
	} else if apiKey == "" {
		// Target given via -addr: try to load config as fallback key source (e.g. for vmr.sh ps)
		if cfg, err := config.Load(*path); err == nil && len(cfg.APIKeys) > 0 {
			apiKey = cfg.APIKeys[0]
		}
	}
	st, err := fetchStatus(target, apiKey)
	if err != nil {
		return err
	}
	if *brief {
		// Tab-separated, fixed field order — pid, listen, uptime, models,
		// version, config-stale flag, config — so the caller can split on a
		// delimiter that cannot appear in a path. Column widths and how to
		// render the stale flag are the caller's business (vmr.sh ps).
		stale := "-"
		if st.Instance.Config.Stale {
			stale = "stale"
		}
		fmt.Printf("%d\t%s\t%s\t%d\t%s\t%s\t%s\n", st.Instance.PID, st.Instance.Listen,
			uptimeStr(st.Instance.Uptime), st.Instance.Models, st.Instance.Version, stale, st.Instance.Config.Path)
		return nil
	}
	printStatus(st)
	return nil
}

// dialHost rewrites a wildcard bind address into a loopback one. cfg.Listen
// is routinely "0.0.0.0:8800" and lsof reports the same socket as "*:8800";
// neither is a destination you can portably connect to, so the local address
// to dial is 127.0.0.1.
func dialHost(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr // not host:port at all — pass it through and let the dial fail with the real reason
	}
	switch host {
	case "", "*", "0.0.0.0", "::", "[::]":
		return net.JoinHostPort("127.0.0.1", port)
	}
	return addr
}

func fetchStatus(addr string, apiKey string) (*statusResponse, error) {
	// Bare Transport (nil Proxy): this is a local diagnostic call to vmr's
	// own status endpoint — it must never route through a proxy, and vmr
	// ignores proxy environment variables everywhere by design.
	statusClient := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{}}
	req, err := http.NewRequest("GET", "http://"+dialHost(addr)+"/status", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := statusClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("is vmr running on %s? %w", addr, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("status endpoint returned 401: authentication required (provide config with -c, or pass -key)")
		}
		return nil, fmt.Errorf("status endpoint returned %d: %s", resp.StatusCode, body)
	}
	var st statusResponse
	if err := json.Unmarshal(body, &st); err != nil {
		// A shape mismatch means the server is running a different vmr
		// version than this binary (CLI and server must match - no compat
		// layer): name it, because the raw Go error names internal struct
		// fields the user cannot act on.
		return nil, fmt.Errorf("could not parse /status response from %s: %w (server and client vmr versions differ - restart the server with the matching binary)", addr, err)
	}
	return &st, nil
}
