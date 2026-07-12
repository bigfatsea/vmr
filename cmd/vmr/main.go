// Ver 2026-07-12 03:10, by Fable 5

// vmr — Virtual Model Router. Single binary, config driven.
//
//	vmr start  -c config.yaml   run the router
//	vmr check  -c config.yaml   validate config and print a summary
//	vmr status -c config.yaml   show endpoint health of a running instance
//	vmr report <audit.jsonl>    aggregate audit logs into usage statistics
//	vmr dirs {log|cache}        print the resolved default audit/cache dir (vmr.sh uses this)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/imgprep"
	"vmr/internal/report"
	"vmr/internal/router"
	"vmr/internal/server"
	"vmr/internal/strategy"

	// Adding a provider type = one blank import here.
	_ "vmr/internal/adapter/anthropic"
	_ "vmr/internal/adapter/openai"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "start":
		err = cmdStart(os.Args[2:])
	case "check":
		err = cmdCheck(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "report":
		err = cmdReport(os.Args[2:])
	case "dirs":
		err = cmdDirs(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "vmr:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: vmr <start|check|status> [-c config.yaml]
       vmr report [-o dir] [-details=false] <audit.jsonl|glob>...
       vmr dirs {log|cache}`)
}

// cmdDirs prints the resolved runtime directory for "log" (audit.Dir) or
// "cache" (imgprep.CacheDir) — the single source of truth for the
// env-var-or-temp-dir-or-cwd default formula (internal/rundir). vmr.sh
// queries this instead of keeping its own copy of the fallback logic, so
// dev mode and service mode can never disagree with what the running
// process actually resolves. Independent of config — these two directories
// never depend on config.yaml content.
func cmdDirs(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: vmr dirs {log|cache}")
	}
	switch args[0] {
	case "log":
		fmt.Println(audit.Dir())
	case "cache":
		fmt.Println(imgprep.CacheDir())
	default:
		return fmt.Errorf("usage: vmr dirs {log|cache}")
	}
	return nil
}

// cmdReport aggregates audit JSONL files into vmr-report.json + vmr-report.md.
// Inputs may freely mix live plain .jsonl files and .jsonl.zst files that the
// audit logger's housekeeping sweep has since compressed (internal/report
// decompresses transparently) — e.g. `vmr report 'vmr-audit-*.jsonl*'`.
func cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	outDir := fs.String("o", ".", "output directory")
	detailsOn := fs.Bool("details", true, "also export one Markdown file per request into {out}/details/")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("no input files; usage: vmr report [-o dir] <audit.jsonl|glob>...")
	}
	seen := map[string]bool{}
	var paths []string
	for _, arg := range fs.Args() {
		matches, err := filepath.Glob(arg)
		if err != nil {
			return fmt.Errorf("bad pattern %q: %w", arg, err)
		}
		if len(matches) == 0 {
			return fmt.Errorf("no files match %q", arg)
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				paths = append(paths, m)
			}
		}
	}
	sort.Strings(paths)

	rep, err := report.Build(paths, time.Now(), os.Stdout)
	if err != nil {
		return err
	}
	// Session analysis (grouping, per-request features, tool usage) feeds
	// the report's tools/sessions sections, the requests export, and the
	// detail files' grouped view.
	sess, err := report.AnalyzeSessions(paths)
	if err != nil {
		return fmt.Errorf("session analysis: %w", err)
	}
	rep.Tools = sess.ToolShapes()
	rep.Sessions = sess.SessionRows()
	rep.Workloads = sess.Workloads()
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	jsonPath := filepath.Join(*outDir, "vmr-report.json")
	mdPath := filepath.Join(*outDir, "vmr-report.md")
	reqPath := filepath.Join(*outDir, "vmr-requests.jsonl")
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(mdPath, []byte(report.Markdown(rep)), 0o644); err != nil {
		return err
	}
	nReq, err := report.WriteRequests(sess, reqPath)
	if err != nil {
		return fmt.Errorf("requests export: %w", err)
	}
	fmt.Printf("%d records (%d parse errors) from %d file(s)\n%s\n%s\n%s (%d rows)\n",
		rep.Meta.Records, rep.Meta.ParseErrors, len(paths), jsonPath, mdPath, reqPath, nReq)
	if *detailsOn {
		detailDir := filepath.Join(*outDir, "details")
		n, err := report.WriteDetails(paths, detailDir, sess)
		if err != nil {
			return fmt.Errorf("details: %w", err)
		}
		fmt.Printf("%d detail file(s) (.md + .json) in %s\n%s\n", n, detailDir,
			filepath.Join(*outDir, "vmr-requests-index.md"))
	}
	return nil
}

func configFlag(args []string, cmd string) (string, error) {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	path := fs.String("c", "config.yaml", "path to config file")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	return *path, nil
}

// vmrBanner is a fixed-width, pure-ASCII block-letter "VMR" mark (no
// unicode box-drawing glyphs, so it renders identically in any terminal,
// log viewer, or `less`/`grep` pipeline). Printed once per process start —
// scanning a log file top to bottom, it's the unmistakable marker of "a new
// vmr process began writing here", distinct from the ordinary timestamped
// lines around it.
const vmrBanner = `
==================================================
  ##      ##      ##      ##      ########
  ##      ##      ##      ##      ########
  ##      ##      ####  ####      ##      ##
  ##      ##      ####  ####      ##      ##
  ##      ##      ##  ##  ##      ##      ##
  ##      ##      ##  ##  ##      ##      ##
  ##      ##      ##      ##      ########
  ##      ##      ##      ##      ########
  ##      ##      ##      ##      ##  ##
  ##      ##      ##      ##      ##  ##
    ##  ##        ##      ##      ##    ##
    ##  ##        ##      ##      ##    ##
      ##          ##      ##      ##      ##
      ##          ##      ##      ##      ##
==================================================
`

// logStart prints the hero banner followed by one timestamped, greppable
// marker line carrying the facts that matter for a support/incident
// timeline: pid, config path, listen address.
func logStart(logger *log.Logger, path string, listen string) {
	fmt.Fprint(logger.Writer(), vmrBanner)
	logger.Printf("VMR START pid=%d config=%s listen=%s", os.Getpid(), path, listen)
}

// logStop prints one timestamped, greppable marker line on the way out —
// clean shutdown or abnormal exit alike — so "how long did this process
// run and why did it stop" is answerable from the log file alone.
func logStop(logger *log.Logger, reason string, uptime time.Duration) {
	logger.Printf("==================================================")
	logger.Printf("VMR STOP  pid=%d reason=%q uptime=%s", os.Getpid(), reason, uptime.Round(time.Second))
	logger.Printf("==================================================")
}

func cmdStart(args []string) error {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	path := fs.String("c", "config.yaml", "path to config file")
	auditOn := fs.Bool("audit", true, "write per-request audit records (JSONL, daily files; dir from $VMR_LOG_DIR or 'vmr dirs log')")
	if err := fs.Parse(args); err != nil {
		return err
	}
	logger := log.New(os.Stderr, "", log.LstdFlags)
	startTime := time.Now()

	cfg, err := config.Load(*path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logStart(logger, *path, cfg.Listen)

	// audit.New's startup housekeeping sweep (internal/audit/housekeep.go)
	// reads the retention window at the moment it runs — SetRetentionDays
	// must land before New, not after, or that first sweep compresses old
	// files but never purges them.
	audit.SetRetentionDays(cfg.AuditRetentionDays)

	var auditLog *audit.Logger
	if *auditOn {
		if auditLog, err = audit.New(audit.Dir()); err != nil {
			return fmt.Errorf("audit log: %w", err)
		}
		defer auditLog.Close()
		logger.Printf("audit log: %s", auditLog.Path())
	} else {
		logger.Printf("audit log disabled (-audit=false)")
	}

	rt := router.New(logger)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		return fmt.Errorf("build routes: %w", err)
	}
	rt.Install(snap)

	logConfigSummary(logger, cfg, snap)

	// Hot reload: fsnotify + SIGHUP. A bad config never replaces a good one.
	reload := func(trigger string) {
		newCfg, err := config.Load(*path)
		if err != nil {
			logger.Printf("reload(%s) rejected, keeping current config: %v", trigger, err)
			return
		}
		newSnap, err := router.BuildSnapshot(newCfg)
		if err != nil {
			logger.Printf("reload(%s) rejected, keeping current config: %v", trigger, err)
			return
		}
		rt.Install(newSnap)
		audit.SetRetentionDays(newCfg.AuditRetentionDays)
		logger.Printf("reload(%s) ok", trigger)
		logConfigSummary(logger, newCfg, newSnap)
	}
	stopWatch, err := config.Watch(*path, func() { reload("fsnotify") })
	if err != nil {
		logger.Printf("config watch disabled: %v (SIGHUP still works)", err)
	} else {
		defer stopWatch()
	}
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			reload("SIGHUP")
		}
	}()

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server.New(rt, auditLog).Handler(),
		ReadHeaderTimeout: 10 * time.Second, // drop connections that stall before sending headers
	}
	logger.Printf("vmr listening on %s (%d models)", cfg.Listen, countNested(cfg.Models))

	// vmr.sh (and systemd/launchd) stop the process with SIGTERM; Go doesn't
	// catch that by default, so without this the process just dies mid-request
	// with no trace in the log. Catching it here buys a graceful drain (existing
	// requests finish, srv.Shutdown waits for them) and — the point of this
	// function — a "VMR STOP" marker so the log file shows exactly when and why
	// the process went away, matching every "VMR START" with a corresponding stop.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	select {
	case sig := <-sigCh:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Printf("shutdown: forced close after 10s drain timeout: %v", err)
		}
		logStop(logger, sig.String(), time.Since(startTime))
		return nil
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			logStop(logger, "error: "+err.Error(), time.Since(startTime))
			return err
		}
		logStop(logger, "server closed", time.Since(startTime))
		return nil
	}
}

func countNested[V any](m map[string]map[string]V) int {
	n := 0
	for _, byName := range m {
		n += len(byName)
	}
	return n
}

// logConfigSummary prints what the running instance is actually configured
// to do: limits, timeouts, and every virtual model's endpoints in their
// effective try order with key state. Printed at startup and after each
// successful hot reload, so the console always reflects the live config.
func logConfigSummary(logger *log.Logger, cfg *config.Config, snap *router.Snapshot) {
	orNoLimit := func(v int, unit string) string {
		if v <= 0 {
			return "unlimited"
		}
		return fmt.Sprintf("%d%s", v, unit)
	}
	auth := "off"
	if cfg.APIKey != "" {
		auth = "on"
	}
	imgScale := "off"
	if cfg.ImageDownscaleMaxPx > 0 {
		imgScale = fmt.Sprintf("%dpx", cfg.ImageDownscaleMaxPx)
	}
	retention := "forever"
	if cfg.AuditRetentionDays > 0 {
		retention = fmt.Sprintf("%dd", cfg.AuditRetentionDays)
	}
	logger.Printf("config: listen=%s auth=%s max_attempts=%s max_request_body=%dMB max_concurrency=%s image_downscale=%s image_cache_ttl=%dd audit_retention=%s",
		cfg.Listen, auth, orNoLimit(cfg.MaxAttempts, ""), cfg.MaxRequestBodyMB, orNoLimit(cfg.MaxConcurrency, ""), imgScale, cfg.ImageCacheTTLDays, retention)
	logger.Printf("config: timeouts connect=%s response_header=%s stream_idle=%s",
		cfg.Timeouts.Connect.D(), cfg.Timeouts.ResponseHeader.D(), cfg.Timeouts.StreamIdle.D())

	protocols := make([]string, 0, len(cfg.Models))
	for protocol := range cfg.Models {
		protocols = append(protocols, protocol)
	}
	sort.Strings(protocols)
	for _, protocol := range protocols {
		names := make([]string, 0, len(cfg.Models[protocol]))
		for name := range cfg.Models[protocol] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			route := snap.Models[protocol][name]
			ordered := append([]*core.Endpoint(nil), route.Endpoints...)
			strategy.Sort(ordered, route.Dims)
			parts := make([]string, len(ordered))
			for i, ep := range ordered {
				key := "key:set"
				if ep.APIKey == "" {
					key = "key:EMPTY"
				}
				parts[i] = fmt.Sprintf("%d.%s/%s(%s)", i+1, ep.Provider, ep.Model, key)
			}
			imgOverride := ""
			if route.ImageDownscaleMaxPx != nil {
				imgOverride = fmt.Sprintf(" image_downscale=%dpx", *route.ImageDownscaleMaxPx)
			}
			logger.Printf("config: model %s [%s]%s -> %s", name, protocol, imgOverride, strings.Join(parts, " "))
		}
	}
}

func cmdCheck(args []string) error {
	path, err := configFlag(args, "check")
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("OK  listen=%s  providers=%d  models=%d  image_downscale=%dpx  image_cache_ttl=%dd\n",
		cfg.Listen, countNested(cfg.Providers), countNested(cfg.Models), cfg.ImageDownscaleMaxPx, cfg.ImageCacheTTLDays)
	protocols := make([]string, 0, len(cfg.Models))
	for protocol := range cfg.Models {
		protocols = append(protocols, protocol)
	}
	sort.Strings(protocols)
	for _, protocol := range protocols {
		names := make([]string, 0, len(cfg.Models[protocol]))
		for name := range cfg.Models[protocol] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			m := cfg.Models[protocol][name]
			imgOverride := ""
			if m.ImageDownscaleMaxPx != nil {
				imgOverride = fmt.Sprintf("  image_downscale=%dpx", *m.ImageDownscaleMaxPx)
			}
			fmt.Printf("  %s [%s] (strategy=%v)%s\n", name, protocol, m.Strategy, imgOverride)
			// Print endpoints in the order they'd actually be tried (health
			// ignored — this is a static preview), not raw config priority
			// numbers: with priority omitted (the common case) that order is
			// exactly config-file order, which is the whole point.
			route := snap.Models[protocol][name]
			ordered := append([]*core.Endpoint(nil), route.Endpoints...)
			strategy.Sort(ordered, route.Dims)
			for i, ep := range ordered {
				key := cfg.Providers[protocol][ep.Provider].APIKey
				keyState := "key:set"
				if key == "" {
					keyState = "key:EMPTY"
				}
				fmt.Printf("    %d. %s/%s  [%s]\n", i+1, ep.Provider, ep.Model, keyState)
			}
		}
	}
	return nil
}

func cmdStatus(args []string) error {
	path, err := configFlag(args, "status")
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get("http://" + cfg.Listen + "/admin/status")
	if err != nil {
		return fmt.Errorf("is vmr running on %s? %w", cfg.Listen, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status endpoint returned %d: %s", resp.StatusCode, body)
	}
	var st struct {
		Models map[string][]struct {
			Endpoint      string    `json:"endpoint"`
			Protocol      string    `json:"protocol"`
			Priority      int       `json:"priority"`
			Fails         int       `json:"consecutive_failures"`
			CooldownUntil time.Time `json:"cooldown_until"`
			LastError     string    `json:"last_error"`
			Available     bool      `json:"available"`
		} `json:"models"`
		Concurrency struct {
			Limit    int   `json:"limit"`
			InFlight int64 `json:"in_flight"`
			Waiting  int64 `json:"waiting"`
		} `json:"concurrency"`
	}
	if err := json.Unmarshal(body, &st); err != nil {
		return err
	}
	if st.Concurrency.Limit > 0 {
		fmt.Printf("concurrency: %d/%d in flight, %d waiting\n",
			st.Concurrency.InFlight, st.Concurrency.Limit, st.Concurrency.Waiting)
	}
	names := make([]string, 0, len(st.Models))
	for name := range st.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Println(name) // key is already "name [protocol]"
		for _, ep := range st.Models[name] {
			state := "ok"
			if !ep.Available {
				state = fmt.Sprintf("COOLDOWN until %s (%s, fails=%d)",
					ep.CooldownUntil.Local().Format("15:04:05"), ep.LastError, ep.Fails)
			} else if ep.Fails > 0 {
				state = fmt.Sprintf("half-open (%s, fails=%d)", ep.LastError, ep.Fails)
			}
			fmt.Printf("  p%-3d %-40s %s\n", ep.Priority, ep.Endpoint, state)
		}
	}
	return nil
}
