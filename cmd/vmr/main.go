// Ver 2026-07-16 00:00, by Sonnet 5

// vmr — Virtual Model Router. Single binary, config driven.
//
//	vmr start    -c config.yaml   run the router
//	vmr check    -c config.yaml   validate config and print a summary
//	vmr status   -c config.yaml   show endpoint health of a running instance
//	vmr report   <audit.jsonl>    aggregate audit logs into usage statistics
//	vmr dirs     {log|cache}      print the config's effective log_dir / image_cache_dir (vmr.sh uses this)
//	vmr diagnose -c config.yaml   validate config, test DNS/TLS/connectivity to every provider, preview routing
//	vmr replay   -provider NAME <audit.jsonl>   rebuild and resend one request from an audit record (or -detail FILE, no audit file needed)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/diagnose"
	"vmr/internal/replay"
	"vmr/internal/report"
	"vmr/internal/router"
	"vmr/internal/server"

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
	case "replay":
		err = cmdReplay(os.Args[2:])
	case "diagnose":
		err = cmdDiagnose(os.Args[2:])
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
       vmr report [-o dir] [-details=false] <audit.jsonl|glob>...   (default -o: ./reports)
       vmr dirs [-c config.yaml] {log|cache}
       vmr diagnose [-c config.yaml] [-no-test-routing] [-json]
       vmr replay [-c config.yaml] -provider NAME [-line N | -ts TS] [flags] <audit.jsonl|.jsonl.zst>
       vmr replay [-c config.yaml] -provider NAME -detail FILE [flags]`)
}

// cmdDirs prints the effective runtime directory for "log" (config
// log_dir) or "cache" (config image_cache_dir) — the resolved value after
// defaults, exactly what a `vmr start` with the same config would use.
// vmr.sh queries this instead of keeping its own copy of the resolution
// logic, so its server-log placement can never disagree with where the
// running process actually writes.
func cmdDirs(args []string) error {
	fs := flag.NewFlagSet("dirs", flag.ExitOnError)
	path := fs.String("c", "config.yaml", "path to config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: vmr dirs [-c config.yaml] {log|cache}")
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	switch fs.Arg(0) {
	case "log":
		fmt.Println(cfg.LogDir)
	case "cache":
		fmt.Println(cfg.ImageCacheDir)
	default:
		return fmt.Errorf("usage: vmr dirs [-c config.yaml] {log|cache}")
	}
	return nil
}

// cmdReport aggregates audit JSONL files into vmr-report.json + vmr-report.md.
// Inputs may freely mix live plain .jsonl files and .jsonl.zst files that the
// audit logger's housekeeping sweep has since compressed (internal/report
// decompresses transparently) — e.g. `vmr report 'vmr-audit-*.jsonl*'`.
func cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	outDir := fs.String("o", "reports", "output directory (default: ./reports; an explicit -o is used as-is, no reports/ subdir added)")
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
	// detail files' grouped view. It is a value-add on top of the aggregate
	// stats report.Build already computed, not a prerequisite for them — a
	// bug in the session-grouping heuristics (a real risk given how much
	// pattern-matching it does, see internal/report/session.go) must not
	// take down the basic cost/usage numbers along with it. A nil sess
	// below skips only the sections that need it.
	sess, err := report.AnalyzeSessions(paths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vmr report: session analysis failed, continuing without sessions/tools/workloads/requests export: %v\n", err)
		sess = nil
	} else {
		rep.Tools = sess.ToolShapes()
		rep.Sessions = sess.SessionRows()
		rep.Workloads = sess.Workloads()
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	jsonPath := filepath.Join(*outDir, "vmr-report.json")
	mdPath := filepath.Join(*outDir, "vmr-report.md")
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
	fmt.Printf("%d records (%d parse errors) from %d file(s)\n%s\n%s\n",
		rep.Meta.Records, rep.Meta.ParseErrors, len(paths), jsonPath, mdPath)
	if sess == nil {
		return nil
	}
	reqPath := filepath.Join(*outDir, "vmr-requests.jsonl")
	nReq, err := report.WriteRequests(sess, reqPath)
	if err != nil {
		return fmt.Errorf("requests export: %w", err)
	}
	fmt.Printf("%s (%d rows)\n", reqPath, nReq)
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

// cmdReplay rebuilds and resends one request from an audit record — see
// internal/replay for the mechanics (same adapter.BuildRequest path vmr
// itself uses, so the replayed request matches what vmr originally sent).
func cmdReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	cfgPath := fs.String("c", "config.yaml", "path to config file")
	line := fs.Int("line", 0, "1-based line number to replay (default: the last parsable record in the file); mutually exclusive with -ts and -detail")
	ts := fs.String("ts", "", "replay the record whose timestamp matches this (millisecond precision; accepts either vmr-requests.jsonl's \"ts\" or the raw audit.jsonl \"ts\" field verbatim); mutually exclusive with -line and -detail")
	detail := fs.String("detail", "", "replay the one record in this vmr-report details/*.json file — no audit file argument needed; mutually exclusive with -line and -ts")
	provider := fs.String("provider", "", "provider to replay against (required; providers.<protocol>.<name>)")
	model := fs.String("model", "", "override the upstream model name (default: resolved from config for -provider under the record's virtual model)")
	protocol := fs.String("protocol", "", "override the protocol (default: the record's own protocol)")
	streamFlag := fs.String("stream", "", "force stream on/off: true|false (default: the record's own value)")
	dryRun := fs.Bool("dry-run", false, "print the request that would be sent, without sending it")
	recordPath := fs.String("record", "", "append the replay's request/response to this audit JSONL file")
	maxTime := fs.Duration("max-time", 0, "upstream timeout for this replay (default: config timeouts.response_header/stream_idle)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *detail != "" {
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: vmr replay [-c config.yaml] -provider NAME -detail FILE [flags]  (no audit file argument — -detail already selects one record)")
		}
	} else if fs.NArg() != 1 {
		return fmt.Errorf("usage: vmr replay [-c config.yaml] -provider NAME [-line N | -ts TS] [flags] <audit.jsonl|.jsonl.zst>")
	}
	opts := replay.Options{
		ConfigPath: *cfgPath,
		Line:       *line,
		TS:         *ts,
		DetailPath: *detail,
		Provider:   *provider,
		Model:      *model,
		Protocol:   *protocol,
		DryRun:     *dryRun,
		RecordPath: *recordPath,
		MaxTime:    *maxTime,
	}
	if fs.NArg() == 1 {
		opts.AuditPath = fs.Arg(0)
	}
	if *streamFlag != "" {
		b, err := strconv.ParseBool(*streamFlag)
		if err != nil {
			return fmt.Errorf("-stream: %w", err)
		}
		opts.Stream = &b
	}
	return replay.Run(context.Background(), opts, os.Stdout)
}

// cmdDiagnose validates config and, unless -no-test-routing is set, dials
// every configured provider with a real minimal request — see
// internal/diagnose for what vmr check (a static preview) doesn't cover.
func cmdDiagnose(args []string) error {
	fs := flag.NewFlagSet("diagnose", flag.ExitOnError)
	cfgPath := fs.String("c", "config.yaml", "path to config file")
	noTestRouting := fs.Bool("no-test-routing", false, "skip phase 3 (real connectivity test); only validate config and environment")
	testTimeout := fs.Duration("test-timeout", 10*time.Second, "per-endpoint timeout for the connectivity test")
	jsonOut := fs.Bool("json", false, "print results as a JSON array instead of the human-readable listing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rep, err := diagnose.Run(context.Background(), diagnose.Options{
		ConfigPath:  *cfgPath,
		TestRouting: !*noTestRouting,
		TestTimeout: *testTimeout,
	})
	if rep == nil {
		return err // config load itself failed; nothing to print
	}
	if *jsonOut {
		data, err := json.MarshalIndent(rep.Results, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Print(diagnose.FormatTable(rep))
	}
	if n := rep.FailCount(); n > 0 {
		return fmt.Errorf("%d failing check(s)", n)
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
	auditOn := fs.Bool("audit", true, "write per-request audit records (JSONL, daily files; dir from config log_dir, see 'vmr dirs log')")
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
		if auditLog, err = audit.New(cfg.LogDir); err != nil {
			return fmt.Errorf("audit log: %w", err)
		}
		defer auditLog.Close()
		logger.Printf("audit log: %s", auditLog.Path())
	} else {
		logger.Printf("audit log disabled (-audit=false)")
	}
	// The audit logger keeps the directory it opened with for the process
	// lifetime; remember it so a hot reload that moves log_dir can say
	// "restart required" instead of silently keeping the old directory.
	auditDirInUse := cfg.LogDir

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
		if newCfg.LogDir != auditDirInUse {
			logger.Printf("reload(%s): log_dir changed (%s -> %s) — takes effect on restart; audit keeps writing to the old directory until then",
				trigger, auditDirInUse, newCfg.LogDir)
		}
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
	if cfg.APIKey != "" || len(cfg.APIKeys) > 0 {
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
	logger.Printf("config: dirs log=%s image_cache=%s", cfg.LogDir, cfg.ImageCacheDir)
	for _, line := range providerProxyLines(cfg) {
		logger.Printf("config: %s", line)
	}

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
			ordered := route.EffectiveOrder()
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

// providerProxyLines renders one line per provider describing the proxy it
// will actually use (a config proxy, or direct) — the answer to "why did
// this provider('s traffic) go through the proxy" without tcpdump.
// Credentials inside proxy URLs are masked (url.Redacted). Proxy
// environment variables play no part: proxies are explicit config, and
// "proxy: true with nothing configured" is a validation error long before
// this renders.
func providerProxyLines(cfg *config.Config) []string {
	redact := func(raw string) string {
		if u, err := url.Parse(raw); err == nil {
			return u.Redacted()
		}
		return raw
	}
	var lines []string
	protocols := make([]string, 0, len(cfg.Providers))
	for protocol := range cfg.Providers {
		protocols = append(protocols, protocol)
	}
	sort.Strings(protocols)
	for _, protocol := range protocols {
		names := make([]string, 0, len(cfg.Providers[protocol]))
		for name := range cfg.Providers[protocol] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			p := cfg.Providers[protocol][name]
			desc := "direct"
			if p.Proxy != nil && !*p.Proxy {
				desc = "direct (proxy: false)"
			}
			if mode, proxyURL := cfg.ProxySpecFor(p); mode == config.ProxyURL {
				desc = redact(proxyURL)
			}
			lines = append(lines, fmt.Sprintf("provider %s/%s proxy=%s", protocol, name, desc))
		}
	}
	return lines
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
	fmt.Printf("  dirs log=%s image_cache=%s\n", cfg.LogDir, cfg.ImageCacheDir)
	for _, line := range providerProxyLines(cfg) {
		fmt.Println("  " + line)
	}
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
			ordered := route.EffectiveOrder()
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
	// Bare Transport (nil Proxy): this is a local diagnostic call to vmr's
	// own admin endpoint — it must never route through a proxy, and vmr
	// ignores proxy environment variables everywhere by design (§10).
	statusClient := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{}}
	resp, err := statusClient.Get("http://" + cfg.Listen + "/admin/status")
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
