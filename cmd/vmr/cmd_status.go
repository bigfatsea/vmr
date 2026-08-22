// Ver 2026-08-02, by Sonnet 5
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/fmtutil"
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

// statusResponse is the /admin/status payload, as much of it as this
// command renders.
type statusResponse struct {
	Instance struct {
		PID         int       `json:"pid"`
		Listen      string    `json:"listen"`
		Config      string    `json:"config"`
		Models      int       `json:"models"`
		Uptime      int64     `json:"uptime_seconds"`
		Started     string    `json:"started_at"`
		Version     string    `json:"version"`
		ConfigStale bool      `json:"config_stale"`
		ConfigMtime time.Time `json:"config_mtime"`
	} `json:"instance"`
	Reload struct {
		At      time.Time `json:"at"`
		Trigger string    `json:"trigger"`
		OK      bool      `json:"ok"`
		Err     string    `json:"error"`
		Count   int       `json:"count"`
	} `json:"reload"`
	Sticky struct {
		Entries int `json:"entries"`
	} `json:"sticky"`
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
		// instead of Available (which stays true through the entire
		// half-open window) to decide the COOLDOWN vs half-open vs ok split.
		// A pointer, not a bare bool: a `vmr status` binary built after this
		// field was added can still query an older, already-running `vmr
		// start` process (e.g. right after a Homebrew tap upgrade, before
		// the service is restarted) whose /admin/status predates it — the
		// JSON response then omits "serving" entirely, and a bare bool
		// would silently decode to false, misrendering every healthy
		// endpoint as half-open. nil means "server didn't send it", not
		// "not serving".
		Serving *bool `json:"serving"`
	} `json:"models"`
	Concurrency struct {
		Limit    int   `json:"limit"`
		InFlight int64 `json:"in_flight"`
		Waiting  int64 `json:"waiting"`
	} `json:"concurrency"`
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
	// Issues is absent (nil slice) when the running config has no
	// consistency/operational issues flagged by config.Check().
	Issues []config.Issue `json:"issues,omitempty"`
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	path := fs.String("c", "config.yaml", "path to config file")
	addr := fs.String("addr", "", "query this host:port directly, without loading a config to find it (for instances whose config this machine may not have)")
	brief := fs.Bool("brief", false, "print one tab-separated line — pid, listen, uptime, models, version, config-stale, config — instead of the health listing (vmr.sh ps uses this)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	target := *addr
	if target == "" {
		cfg, err := config.Load(*path)
		if err != nil {
			return err
		}
		target = cfg.Listen
	}
	st, err := fetchStatus(target)
	if err != nil {
		return err
	}
	if *brief {
		// Tab-separated, fixed field order — pid, listen, uptime, models,
		// version, config-stale flag, config — so the caller can split on a
		// delimiter that cannot appear in a path. Column widths and how to
		// render the stale flag are the caller's business (vmr.sh ps).
		stale := "-"
		if st.Instance.ConfigStale {
			stale = "stale"
		}
		fmt.Printf("%d\t%s\t%s\t%d\t%s\t%s\t%s\n", st.Instance.PID, st.Instance.Listen,
			uptimeStr(st.Instance.Uptime), st.Instance.Models, st.Instance.Version, stale, st.Instance.Config)
		return nil
	}
	printStatus(st)
	return nil
}

// uptimeStr renders whole seconds the same way cmd_start's VMR STOP line
// does (time.Duration's own "3h12m5s"), so a log line and a status line
// never disagree about how long the same process has been up.
func uptimeStr(seconds int64) string {
	return (time.Duration(seconds) * time.Second).String()
}

// numStr formats a float without a trailing ".0" for the common whole-number
// case (an unweighted requests/tokens count) but keeps two decimals for a
// genuinely fractional value (a model_multipliers-scaled amount) — mirrors
// internal/report/render_cells.go's numStr, which the same requests/tokens
// precision issue was already fixed for on the report side.
func numStr(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// oneLine flattens a multi-line error (config.Load's YAML errors are
// routinely three lines) into something that fits one status line, and
// caps it so a pathological error can't scroll the endpoint listing off
// screen — the log has the untruncated version either way.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// dialHost rewrites a wildcard bind address into a loopback one. cfg.Listen
// is routinely "0.0.0.0:8800" and lsof reports the same socket as "*:8800";
// neither is a destination you can portably connect to, and /admin/status
// is loopback-only anyway — so the only address worth trying is 127.0.0.1.
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

func fetchStatus(addr string) (*statusResponse, error) {
	// Bare Transport (nil Proxy): this is a local diagnostic call to vmr's
	// own admin endpoint — it must never route through a proxy, and vmr
	// ignores proxy environment variables everywhere by design.
	statusClient := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{}}
	resp, err := statusClient.Get("http://" + dialHost(addr) + "/admin/status")
	if err != nil {
		return nil, fmt.Errorf("is vmr running on %s? %w", addr, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status endpoint returned %d: %s", resp.StatusCode, body)
	}
	var st statusResponse
	if err := json.Unmarshal(body, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func printStatus(st *statusResponse) {
	// Instance line first: with several vmr processes on one machine, "which
	// one did I just reach" has to be answerable before any of the health
	// numbers below mean anything.
	if st.Instance.PID > 0 {
		fmt.Printf("instance: pid=%d listen=%s uptime=%s version=%s config=%s\n",
			st.Instance.PID, st.Instance.Listen,
			uptimeStr(st.Instance.Uptime), st.Instance.Version, st.Instance.Config)
	}
	// The whole point of tracking reload state: say it loudly, at the top,
	// because a process serving a config that no longer matches the file on
	// disk looks perfectly healthy in every line below this one.
	if st.Instance.ConfigStale {
		fmt.Printf("  WARNING: config file modified %s — newer than the config this process is serving\n",
			st.Instance.ConfigMtime.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05"))
		if !st.Reload.At.IsZero() && !st.Reload.OK {
			fmt.Printf("           last reload (%s at %s) was REJECTED: %s\n",
				st.Reload.Trigger, st.Reload.At.In(fmtutil.DisplayZone).Format("15:04:05"), oneLine(st.Reload.Err))
		} else {
			fmt.Println("           no reload has picked it up — check the log, or send SIGHUP to retry")
		}
	}
	for _, is := range st.Issues {
		fmt.Printf("  WARNING: %s\n", is.Message)
	}
	if !st.Reload.At.IsZero() {
		state := "ok"
		if !st.Reload.OK {
			// The warning block above already printed the reason in full;
			// repeating a multi-line YAML error twice in six lines of output
			// is noise, not emphasis.
			state = "REJECTED: " + oneLine(st.Reload.Err)
			if st.Instance.ConfigStale {
				state = "REJECTED (reason above)"
			}
		}
		fmt.Printf("reload: %d applied, last %s at %s %s\n",
			st.Reload.Count, st.Reload.Trigger, st.Reload.At.In(fmtutil.DisplayZone).Format("15:04:05"), state)
	}
	if st.Concurrency.Limit > 0 {
		fmt.Printf("concurrency: %d/%d in flight, %d waiting\n",
			st.Concurrency.InFlight, st.Concurrency.Limit, st.Concurrency.Waiting)
	}
	if st.Sticky.Entries > 0 {
		fmt.Printf("sticky: %d session(s) pinned\n", st.Sticky.Entries)
	}
	for _, q := range st.Quota {
		estNote := ""
		if q.EstimatedPct > 0 {
			estNote = fmt.Sprintf(", %.0f%% estimated", q.EstimatedPct)
		}
		// metric: cost's used/amount are money, always rendered to 4dp so
		// a $2.5000 balance never reads as a rounded "$2.5". requests/tokens
		// are usually whole numbers but aren't guaranteed to be: a
		// fractional model_multipliers value (e.g. 1.5) folds straight into
		// Used with no rounding (see quota.Counters' doc comment), so
		// numStr renders those with decimals too instead of %.0f silently
		// truncating them back to an integer.
		usedStr, amountStr := fmt.Sprintf("%.4f", q.Used), fmt.Sprintf("%.4f", q.Amount)
		if q.Metric != "cost" {
			usedStr, amountStr = numStr(q.Used), numStr(q.Amount)
		}
		scopeNote := ""
		if len(q.Models) > 0 {
			scopeNote = " models=" + strings.Join(q.Models, ",")
		}
		fmt.Printf("quota %-12s %s/%s [%s]  used=%s/%s (%.1f%%)  headroom=%.2f  resets %s%s%s\n",
			q.Provider, q.Metric, q.Every, q.Role, usedStr, amountStr, q.Pct, q.Headroom,
			q.PeriodEndsAt.In(fmtutil.DisplayZone).Format("2006-01-02 15:04"), estNote, scopeNote)
		tw := q.TokenWeights
		if tw != allDefaultTokenWeights {
			fmt.Printf("  token_weights: in_fresh=%g cache_read=%g cache_write=%g out=%g\n",
				tw.InFresh, tw.CacheRead, tw.CacheWrite, tw.Out)
		}
		if len(q.ModelMultipliers) > 0 {
			parts := make([]string, 0, len(q.ModelMultipliers))
			for _, model := range core.SortedKeys(q.ModelMultipliers) {
				parts = append(parts, fmt.Sprintf("%s=%g", model, q.ModelMultipliers[model]))
			}
			fmt.Printf("  model_multipliers: %s\n", strings.Join(parts, " "))
		}
	}
	for _, name := range core.SortedKeys(st.Models) {
		fmt.Println(name) // key is already "name [protocol]"
		for _, ep := range st.Models[name] {
			// notServing falls back to the pre-Serving-field heuristic
			// (Fails>0) when an older server's response omits "serving"
			// entirely (ep.Serving == nil) — see the Serving field's doc
			// comment for why that's a real, not just theoretical, case.
			notServing := ep.Fails > 0
			if ep.Serving != nil {
				notServing = !*ep.Serving
			}
			state := "ok"
			if !ep.Available {
				state = fmt.Sprintf("COOLDOWN until %s (%s, fails=%d)",
					ep.CooldownUntil.In(fmtutil.DisplayZone).Format("15:04:05"), ep.LastError, ep.Fails)
			} else if notServing {
				// Available (cooldown expired) but not Serving: the
				// half-open window — real traffic isn't routed here yet,
				// only a background probe, whether or not one currently
				// holds the slot (see health.Status.Serving's doc comment).
				probing := ""
				if ep.Probing {
					probing = ", probing" // a background probe currently holds this endpoint's single-flight recovery check
				}
				state = fmt.Sprintf("half-open (%s, fails=%d%s)", ep.LastError, ep.Fails, probing)
			}
			fmt.Printf("  p%-3d %-40s %s\n", ep.Priority, ep.Endpoint, state)
		}
	}
}
