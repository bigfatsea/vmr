// Ver 2026-08-13 16:39, by Gemini 3.6 Flash
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/fmtutil"
	"vmr/internal/logtee"
	"vmr/internal/quota"
	"vmr/internal/router"
	"vmr/internal/server"
)

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
	line := append([]byte(time.Now().In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05")+" "), p...)
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

// logConfigCheckIssues surfaces operational config problems (config.Check)
// that structural validation lets through — most notably an empty api_key
// from a typo'd or unset ${ENV_VAR}, which loads as valid YAML and only
// fails once a real request 401s. Called on every path that can put a new
// config into service without a human running `vmr check` by hand: initial
// start, and every hot reload (fsnotify/SIGHUP), including the reloads a
// service manager's restart triggers. Warnings only — a Check() issue is
// "can run but may be wrong", not grounds to refuse a config validate()
// already accepted.
func logConfigCheckIssues(logger *log.Logger, issues []config.Issue) {
	for _, is := range issues {
		logger.Printf("WARN config check: %s", is.Message)
	}
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
	auditOn := fs.Bool("audit", true, "write per-request audit records (JSONL, daily files; dir from config log_dir, see 'vmr check log')")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// The tee feeds /log's live stream: every logger line reaches both
	// stderr and the ring buffer, stamped identically (the stampWriter wraps
	// the fan-out, so both copies carry the same timestamp). One instance,
	// one interception point — hot reloads never touch the logger, so the
	// tee survives them untouched.
	tee := logtee.New(logtee.DefaultCapLines)
	logger := log.New(stampWriter{io.MultiWriter(os.Stderr, tee)}, "", 0)
	startTime := time.Now()

	cfg, err := config.Load(*path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logStart(logger, *path, cfg.Listen)
	issues := cfg.Check()
	logConfigCheckIssues(logger, issues)

	// audit.New's startup housekeeping sweep (internal/audit/housekeep.go)
	// reads the retention window at the moment it runs — SetRetentionDays
	// must land before New, not after, or that first sweep compresses old
	// files but never purges them.
	audit.SetRetentionDays(cfg.AuditRetentionDays)
	audit.SetExtraRedactHeaders(cfg.ExtraRedactHeaders)

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

	// Quota registry lives on Router (surviving hot reloads). Load errors are non-fatal (logged).
	qreg := quota.NewRegistry(filepath.Join(cfg.LogDir, "vmr-quota.json"))
	if err := qreg.Load(); err != nil {
		logger.Printf("WARN quota state: %v (starting from zero)", err)
	}
	rt.Quota = qreg
	stopQuotaFlush := qreg.StartFlusher(quota.DefaultFlushInterval)
	defer func() { stopQuotaFlush(); qreg.Flush() }()

	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		return fmt.Errorf("build routes: %w", err)
	}
	rt.Install(snap)

	logConfigSummary(logger, cfg, snap, issues)

	// Hot reload: fsnotify + SIGHUP. A bad config never replaces a good one.
	// Every attempt — rejected or not — gets the same banner treatment (same
	// idiom logStop uses for its own marker) so a reload is never lost in
	// the steady drip of per-request log lines around it; a rejected reload
	// especially so, since that's the one that needs a human's attention.
	// The bar alone (through the normal timestamped logger, not a raw
	// stderr write) is enough separation — no need for blank lines too.
	var reloadMu sync.Mutex // serialize fsnotify vs SIGHUP reloads: concurrent rt.Install races installLimiter (B5)
	reload := func(trigger string) {
		reloadMu.Lock()
		defer reloadMu.Unlock()
		bar := strings.Repeat("=", 50)
		logger.Printf("%s", bar)
		logger.Printf("CONFIG RELOAD  trigger=%s", trigger)
		defer logger.Printf("%s", bar)

		newCfg, err := config.Load(*path)
		if err != nil {
			logger.Printf("rejected, keeping current config: %v", err)
			rt.RecordReload(trigger, err)
			return
		}
		newIssues := newCfg.Check()
		logConfigCheckIssues(logger, newIssues)
		newSnap, err := router.BuildSnapshot(newCfg)
		if err != nil {
			logger.Printf("rejected, keeping current config: %v", err)
			rt.RecordReload(trigger, err)
			return
		}
		rt.Install(newSnap)
		rt.RecordReload(trigger, nil)
		audit.SetRetentionDays(newCfg.AuditRetentionDays)
		audit.SetExtraRedactHeaders(newCfg.ExtraRedactHeaders)
		if newCfg.LogDir != auditDirInUse {
			logger.Printf("log_dir changed: %s -> %s (takes effect on restart; audit keeps writing to the old directory until then)",
				auditDirInUse, newCfg.LogDir)
		}
		logConfigSummary(logger, newCfg, newSnap, newIssues)
	}
	stopWatch, err := config.Watch(*path, func() { reload("fsnotify") }, func(err error) {
		logger.Printf("WARN config watch: %v (hot reload may have stopped working; SIGHUP still works)", err)
	})
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
		Addr: cfg.Listen,
		Handler: server.New(rt, auditLog).WithLogTee(tee).
			WithInstance(*path, startTime).Handler(),
		ReadHeaderTimeout: 10 * time.Second, // drop connections that stall before sending headers
	}
	logger.Printf("vmr listening on %s (%d models)", cfg.Listen, len(cfg.Models))

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
