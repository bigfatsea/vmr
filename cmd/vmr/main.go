// Ver 2026-07-07 17:45, by Fable 5

// vmr — Virtual Model Router. Single binary, config driven.
//
//	vmr start  -c config.yaml   run the router
//	vmr check  -c config.yaml   validate config and print a summary
//	vmr status -c config.yaml   show endpoint health of a running instance
package main

import (
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
	"syscall"
	"time"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/core"
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
       vmr report [-o dir] <audit.jsonl|glob>...`)
}

// cmdReport aggregates audit JSONL files into vmr-report.json + vmr-report.md.
func cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	outDir := fs.String("o", ".", "output directory")
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

	rep, err := report.Build(paths, time.Now())
	if err != nil {
		return err
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

func cmdStart(args []string) error {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	path := fs.String("c", "config.yaml", "path to config file")
	auditOn := fs.Bool("audit", true, "write per-request audit records (JSONL, daily files; dir from $VMR_LOG_DIR or system temp)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	logger := log.New(os.Stderr, "", log.LstdFlags)

	var auditLog *audit.Logger
	if *auditOn {
		var err error
		if auditLog, err = audit.New(audit.Dir()); err != nil {
			return fmt.Errorf("audit log: %w", err)
		}
		defer auditLog.Close()
		logger.Printf("audit log: %s", auditLog.Path())
	} else {
		logger.Printf("audit log disabled (-audit=false)")
	}

	cfg, err := config.Load(*path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	rt := router.New(logger)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		return fmt.Errorf("build routes: %w", err)
	}
	rt.Install(snap)

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
		logger.Printf("reload(%s) ok: %d models, %d providers", trigger, countNested(newCfg.Models), countNested(newCfg.Providers))
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

	srv := &http.Server{Addr: cfg.Listen, Handler: server.New(rt, auditLog).Handler()}
	logger.Printf("vmr listening on %s (%d models)", cfg.Listen, countNested(cfg.Models))
	return srv.ListenAndServe()
}

func countNested[V any](m map[string]map[string]V) int {
	n := 0
	for _, byName := range m {
		n += len(byName)
	}
	return n
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
	fmt.Printf("OK  listen=%s  providers=%d  models=%d\n", cfg.Listen, countNested(cfg.Providers), countNested(cfg.Models))
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
			fmt.Printf("  %s [%s] (strategy=%v)\n", name, protocol, m.Strategy)
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
		proto := ""
		if eps := st.Models[name]; len(eps) > 0 {
			proto = " [" + eps[0].Protocol + "]"
		}
		fmt.Println(name + proto)
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
