// Ver 2026-07-24 15:00, by Sonnet 5

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
	"vmr/internal/core"
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
	// 0o700/0o600: report outputs embed full conversation bodies from the
	// 0600 audit files — the derived copies must not loosen that.
	if err := os.MkdirAll(*outDir, 0o700); err != nil {
		return err
	}
	jsonPath := filepath.Join(*outDir, "vmr-report.json")
	mdPath := filepath.Join(*outDir, "vmr-report.md")
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(mdPath, []byte(report.Markdown(rep)), 0o600); err != nil {
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
	testTimeout := fs.Duration("test-timeout", 15*time.Second, "per-endpoint timeout for the connectivity test")
	jsonOut := fs.Bool("json", false, "print results as a JSON array instead of the human-readable listing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rep, err := diagnose.Run(context.Background(), diagnose.Options{
		ConfigPath:  *cfgPath,
		TestRouting: !*noTestRouting,
		TestTimeout: *testTimeout,
		// Progress always goes to stderr, in both output modes: it's pure
		// "this is still running" narration, never part of the reported
		// data, so it can't corrupt -json's stdout even when both streams
		// share a terminal. Redirect stderr away (2>/dev/null) to silence it.
		Progress: os.Stderr,
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
 _    ____  _______
| |  / /  |/  / __ \
| | / / /|_/ / /_/ /
| |/ / /  / / _, _/
|___/_/  /_/_/ |_|

  Virtual Model Router
`

// stampWriter prepends a "YYYY-MM-DD HH:MM:SS " timestamp (ISO-ish, unlike
// log.LstdFlags' fixed "2006/01/02 15:04:05") to every write. log.Logger
// calls Write exactly once per line — the fully formatted message, already
// newline-terminated — so wrapping the writer stamps every logger.Printf
// call site uniformly without editing any of them individually.
type stampWriter struct{ w io.Writer }

func (s stampWriter) Write(p []byte) (int, error) {
	line := append([]byte(time.Now().Format("2006-01-02 15:04:05")+" "), p...)
	if _, err := s.w.Write(line); err != nil {
		return 0, err
	}
	return len(p), nil
}

// logStart prints the hero banner followed by one timestamped, greppable
// marker line carrying the facts that matter for a support/incident
// timeline: pid, config path, listen address. The banner is written
// straight to stderr (bypassing the logger's stamping) — ASCII art doesn't
// want a timestamp glued to its first line.
func logStart(logger *log.Logger, path string, listen string) {
	fmt.Fprint(os.Stderr, vmrBanner)
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
	logger := log.New(stampWriter{os.Stderr}, "", 0)
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
	// Every attempt — rejected or not — gets the same banner treatment (same
	// idiom logStop uses for its own marker) so a reload is never lost in
	// the steady drip of per-request log lines around it; a rejected reload
	// especially so, since that's the one that needs a human's attention.
	// The bar alone (through the normal timestamped logger, not a raw
	// stderr write) is enough separation — no need for blank lines too.
	reload := func(trigger string) {
		bar := strings.Repeat("=", 50)
		logger.Printf("%s", bar)
		logger.Printf("CONFIG RELOAD  trigger=%s", trigger)
		defer logger.Printf("%s", bar)

		newCfg, err := config.Load(*path)
		if err != nil {
			logger.Printf("rejected, keeping current config: %v", err)
			return
		}
		newSnap, err := router.BuildSnapshot(newCfg)
		if err != nil {
			logger.Printf("rejected, keeping current config: %v", err)
			return
		}
		rt.Install(newSnap)
		audit.SetRetentionDays(newCfg.AuditRetentionDays)
		if newCfg.LogDir != auditDirInUse {
			logger.Printf("log_dir changed: %s -> %s (takes effect on restart; audit keeps writing to the old directory until then)",
				auditDirInUse, newCfg.LogDir)
		}
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
	logger.Printf("vmr listening on %s (%d models)", cfg.Listen, config.CountNested(cfg.Models))

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

// logConfigSummary prints what the running instance is actually configured
// to do: limits, timeouts, and every virtual model's endpoints in their
// effective try order with key state. Printed at startup and after each
// successful hot reload, so the console always reflects the live config.
//
// Each of the three sections (global/provider/model) is built as one
// multi-line string and emitted through a single logger.Printf call.
// log.Logger writes its formatted message in exactly one Write, and
// stampWriter stamps once per Write — so the timestamp lands only on the
// section header line, while the indented detail lines stay unstamped and
// readable as a block.
func logConfigSummary(logger *log.Logger, cfg *config.Config, snap *router.Snapshot) {
	orNoLimit := func(v int, unit string) string {
		if v <= 0 {
			return "unlimited"
		}
		return fmt.Sprintf("%d%s", v, unit)
	}
	auth := "off"
	if len(cfg.APIKeys) > 0 {
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

	const globalKeyWidth = 17 // len("max_request_body"), the widest field name below
	field := func(indent int, key string, val any) string {
		return fmt.Sprintf("\n%s%-*s = %v", strings.Repeat(" ", indent), globalKeyWidth, key, val)
	}

	var global strings.Builder
	global.WriteString("global config:")
	global.WriteString(field(4, "listen", cfg.Listen))
	global.WriteString(field(4, "auth", auth))
	global.WriteString(field(4, "max_attempts", orNoLimit(cfg.MaxAttempts, "")))
	global.WriteString(field(4, "max_request_body", fmt.Sprintf("%dMB", cfg.MaxRequestBodyMB)))
	global.WriteString(field(4, "max_concurrency", orNoLimit(cfg.MaxConcurrency, "")))
	global.WriteString(field(4, "image_downscale", imgScale))
	global.WriteString(field(4, "image_cache_ttl", fmt.Sprintf("%dd", cfg.ImageCacheTTLDays)))
	global.WriteString(field(4, "audit_retention", retention))
	global.WriteString(field(4, "probe_mode", cfg.ProbeMode))
	global.WriteString(field(4, "probe_timeout", cfg.ProbeTimeout.D()))
	global.WriteString("\n    timeouts")
	global.WriteString(field(8, "connect", cfg.Timeouts.Connect.D()))
	global.WriteString(field(8, "response_header", cfg.Timeouts.ResponseHeader.D()))
	global.WriteString(field(8, "stream_idle", cfg.Timeouts.StreamIdle.D()))
	global.WriteString("\n    dirs")
	global.WriteString(field(8, "log", cfg.LogDir))
	global.WriteString(field(8, "image_cache", cfg.ImageCacheDir))
	logger.Printf("%s", global.String())

	if entries := providerProxyEntries(cfg); len(entries) > 0 {
		nameWidth := 0
		for _, e := range entries {
			if len(e.Name) > nameWidth {
				nameWidth = len(e.Name)
			}
		}
		var provider strings.Builder
		provider.WriteString("provider config:")
		for _, e := range entries {
			marker := ""
			if e.IsProxied {
				marker = " (proxy)"
			}
			fmt.Fprintf(&provider, "\n    %-*s base_url=%s%s", nameWidth, e.Name, e.BaseURL, marker)
		}
		logger.Printf("%s", provider.String())
	}

	var model strings.Builder
	model.WriteString("model config:")
	for _, protocol := range core.SortedKeys(cfg.Models) {
		for _, name := range core.SortedKeys(cfg.Models[protocol]) {
			route := snap.Models[protocol][name]
			imgOverride := ""
			if route.ImageDownscaleMaxPx != nil {
				imgOverride = fmt.Sprintf(" (image_downscale=%dpx)", *route.ImageDownscaleMaxPx)
			}
			fmt.Fprintf(&model, "\n    %s/%s%s", protocol, name, imgOverride)
			for i, ep := range route.EffectiveOrder() {
				fmt.Fprintf(&model, "\n        %d.%s/%s, max_context_tokens=%s, capabilities=%s",
					i+1, ep.Provider, ep.Model, fmtMaxContextTokens(ep.MaxContextTokens), fmtCapabilities(ep.Capabilities))
			}
		}
	}
	logger.Printf("%s", model.String())
}

// fmtMaxContextTokens renders an endpoint's declared context ceiling for the
// "model config:" block — "<empty>" for the unconstrained zero value (see
// core.Endpoint.MaxContextTokens), "<N>k" for the common round-thousands
// case (128000 -> "128k"), the raw integer otherwise (an odd, non-round
// value is rare enough not to deserve special-casing).
func fmtMaxContextTokens(n int64) string {
	if n <= 0 {
		return "<empty>"
	}
	if n%1000 == 0 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

// fmtCapabilities renders an endpoint's declared capabilities for the
// "model config:" block — "<empty>" when unset (unconstrained: this
// endpoint is assumed to support everything, see core.Endpoint.HasCapability),
// else the declared list joined with "/".
func fmtCapabilities(caps []string) string {
	if len(caps) == 0 {
		return "<empty>"
	}
	return strings.Join(caps, "/")
}

// providerProxyEntry is one provider's resolved proxy setting, keyed by its
// "protocol/name" for display. BaseURL/IsProxied feed logConfigSummary's
// "provider config:" block (base_url=<url>, "(proxy)" marker only); Proxy is
// the older human-readable description (direct / direct (proxy: false) /
// redacted proxy URL) providerProxyLines still renders for `vmr check`.
type providerProxyEntry struct {
	Name      string
	BaseURL   string
	Proxy     string
	IsProxied bool
}

// providerProxyEntries resolves the proxy each provider will actually use (a
// config proxy, or direct) — the answer to "why did this provider('s
// traffic) go through the proxy" without tcpdump. Credentials inside proxy
// URLs are masked (url.Redacted). Proxy environment variables play no part:
// proxies are explicit config, and "proxy: true with nothing configured" is
// a validation error long before this renders.
func providerProxyEntries(cfg *config.Config) []providerProxyEntry {
	redact := func(raw string) string {
		if u, err := url.Parse(raw); err == nil {
			return u.Redacted()
		}
		return raw
	}
	var entries []providerProxyEntry
	for _, protocol := range core.SortedKeys(cfg.Providers) {
		for _, name := range core.SortedKeys(cfg.Providers[protocol]) {
			p := cfg.Providers[protocol][name]
			desc := "direct"
			if p.Proxy != nil && !*p.Proxy {
				desc = "direct (proxy: false)"
			}
			isProxied := false
			if mode, proxyURL := cfg.ProxySpecFor(p); mode == config.ProxyURL {
				desc = redact(proxyURL)
				isProxied = true
			}
			entries = append(entries, providerProxyEntry{
				Name: protocol + "/" + name, BaseURL: p.BaseURL, Proxy: desc, IsProxied: isProxied,
			})
		}
	}
	return entries
}

// providerProxyLines renders providerProxyEntries as flat "provider a/b
// proxy=c" lines, kept for cmdCheck's one-line-per-provider "vmr check" output.
func providerProxyLines(cfg *config.Config) []string {
	entries := providerProxyEntries(cfg)
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = fmt.Sprintf("provider %s proxy=%s", e.Name, e.Proxy)
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
	fmt.Printf("OK  listen=%s  providers=%d  models=%d  image_downscale=%dpx  image_cache_ttl=%dd  probe_mode=%s  probe_timeout=%s\n",
		cfg.Listen, config.CountNested(cfg.Providers), config.CountNested(cfg.Models), cfg.ImageDownscaleMaxPx, cfg.ImageCacheTTLDays, cfg.ProbeMode, cfg.ProbeTimeout.D())
	fmt.Printf("  dirs log=%s image_cache=%s\n", cfg.LogDir, cfg.ImageCacheDir)
	for _, line := range providerProxyLines(cfg) {
		fmt.Println("  " + line)
	}
	for _, protocol := range core.SortedKeys(cfg.Models) {
		for _, name := range core.SortedKeys(cfg.Models[protocol]) {
			m := cfg.Models[protocol][name]
			imgOverride := ""
			if m.ImageDownscaleMaxPx != nil {
				imgOverride = fmt.Sprintf("  image_downscale=%dpx", *m.ImageDownscaleMaxPx)
			}
			route := snap.Models[protocol][name]
			fmt.Printf("  %s [%s] (strategy=%v sticky=%v)%s\n", name, protocol, m.Strategy, route.Sticky, imgOverride)
			// Print endpoints in the order they'd actually be tried (health
			// ignored — this is a static preview), not raw config priority
			// numbers: with priority omitted (the common case) that order is
			// exactly config-file order, which is the whole point.
			ordered := route.EffectiveOrder()
			for i, ep := range ordered {
				key := cfg.Providers[protocol][ep.Provider].APIKey
				keyState := "key:set"
				if key == "" {
					keyState = "key:EMPTY"
				}
				// Condition-routing/Sticky Model declarations, printed only
				// when they actually constrain something — an endpoint with
				// none of these set behaves exactly as before they existed,
				// and the check output should look exactly as before too
				// (see docs/VirtualModelRouter_System_Design_v3.md §6.4:
				// absent = unconstrained/inherit, never a new limit).
				extra := ""
				if len(ep.Capabilities) > 0 {
					extra += fmt.Sprintf("  capabilities=%v", ep.Capabilities)
				}
				if ep.MaxContextTokens > 0 {
					extra += fmt.Sprintf("  max_context_tokens=%d", ep.MaxContextTokens)
				}
				if route.Sticky {
					extra += fmt.Sprintf("  sticky_ttl=%s", ep.StickyTTL)
				}
				fmt.Printf("    %d. %s/%s  [%s]%s\n", i+1, ep.Provider, ep.Model, keyState, extra)
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
			Probing       bool      `json:"probing"`
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
	for _, name := range core.SortedKeys(st.Models) {
		fmt.Println(name) // key is already "name [protocol]"
		for _, ep := range st.Models[name] {
			state := "ok"
			if !ep.Available {
				state = fmt.Sprintf("COOLDOWN until %s (%s, fails=%d)",
					ep.CooldownUntil.Local().Format("15:04:05"), ep.LastError, ep.Fails)
			} else if ep.Fails > 0 {
				probing := ""
				if ep.Probing {
					probing = ", probing" // a passive-mode real request or an active-mode background probe currently holds this endpoint's single-flight recovery check
				}
				state = fmt.Sprintf("half-open (%s, fails=%d%s)", ep.LastError, ep.Fails, probing)
			}
			fmt.Printf("  p%-3d %-40s %s\n", ep.Priority, ep.Endpoint, state)
		}
	}
	return nil
}
