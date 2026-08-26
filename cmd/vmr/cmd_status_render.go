// Ver 2026-08-23 14:52, by Gemini

package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"vmr/internal/core"
	"vmr/internal/fmtutil"
)

// printStatus formats and renders the /status response to stdout.
func printStatus(st *statusResponse) {
	// Instance line first: with several vmr processes on one machine, "which
	// one did I just reach" has to be answerable before any of the health
	// numbers below mean anything.
	if st.Instance.PID > 0 {
		fmt.Printf("instance: pid=%d listen=%s uptime=%s version=%s config=%s\n",
			st.Instance.PID, st.Instance.Listen,
			uptimeStr(st.Instance.Uptime), st.Instance.Version, st.Instance.Config.Path)
	}
	// The whole point of tracking reload state: say it loudly, at the top,
	// because a process serving a config that no longer matches the file on
	// disk looks perfectly healthy in every line below this one.
	if st.Instance.Config.Stale {
		fmt.Printf("  WARNING: config file modified %s — newer than the config this process is serving\n",
			st.Instance.Config.Mtime.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05"))
		if rl := st.Instance.Config.Reload; rl != nil && !rl.At.IsZero() && !rl.OK {
			fmt.Printf("           last reload (%s at %s) was REJECTED: %s\n",
				rl.Trigger, rl.At.In(fmtutil.DisplayZone).Format("15:04:05"), oneLine(rl.Err))
		} else {
			fmt.Println("           no reload has picked it up — check the log, or send SIGHUP to retry")
		}
	}
	for _, is := range st.Instance.Config.Issues {
		fmt.Printf("  WARNING: %s\n", is.Message)
	}
	if rl := st.Instance.Config.Reload; rl != nil && !rl.At.IsZero() {
		state := "ok"
		if !rl.OK {
			// The warning block above already printed the reason in full;
			// repeating a multi-line YAML error twice in six lines of output
			// is noise, not emphasis.
			state = "REJECTED: " + oneLine(rl.Err)
			if st.Instance.Config.Stale {
				state = "REJECTED (reason above)"
			}
		}
		fmt.Printf("reload: %d applied, last %s at %s %s\n",
			rl.Count, rl.Trigger, rl.At.In(fmtutil.DisplayZone).Format("15:04:05"), state)
	}
	conc := st.Instance.Concurrency
	if conc.Limit > 0 {
		fmt.Printf("concurrency: %d/%d in flight, %d waiting\n",
			conc.InFlight, conc.Limit, conc.Waiting)
	}
	if st.Traffic.Sticky.Entries > 0 {
		fmt.Printf("sticky: %d session(s) pinned\n", st.Traffic.Sticky.Entries)
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
	for _, m := range st.Models {
		fmt.Println(core.ModelLabel(m.ID, m.Protocol))
		if len(m.Capabilities) > 0 {
			fmt.Printf("  capabilities: %s\n", strings.Join(m.Capabilities, ", "))
		}
		if m.MaxContextTokens > 0 {
			fmt.Printf("  max_context_tokens: %d\n", m.MaxContextTokens)
		}
		for _, ep := range m.Endpoints {
			state := "ok"
			if !ep.Available {
				state = fmt.Sprintf("COOLDOWN until %s (%s, fails=%d)",
					ep.CooldownUntil.In(fmtutil.DisplayZone).Format("15:04:05"), ep.LastError, ep.Fails)
			} else if !ep.Serving {
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
