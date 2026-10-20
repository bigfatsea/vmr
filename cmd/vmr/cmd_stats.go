package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/stats"
	"vmr/internal/statsfeed"
)

// cmdStats dispatches `vmr stats <subcommand>`. Only rebuild exists today.
func cmdStats(args []string) error {
	if len(args) == 0 || args[0] != "rebuild" {
		return fmt.Errorf("usage: vmr stats rebuild [-c config.yaml] [-dir DIR] [-from YYYY-MM-DD] [-to YYYY-MM-DD] [-today] [-force] [-dry-run] [audit.jsonl|glob]...")
	}
	return cmdStatsRebuild(args[1:])
}

// cmdStatsRebuild rebuilds closed statistics days from the audit log,
// through the same statsfeed projection the live completion hook uses, and
// files each event under the day it completed in (stats.DayOf) like the live
// path does. It is safe to run next to a live vmr: without -today it never touches today's
// file (the only one the running process writes), writes each day
// atomically, and the running process re-reads a replaced day on its next
// query. -today also rebuilds today and needs vmr stopped (the deploy
// window), so upgrade day has no gap.
func cmdStatsRebuild(args []string) error {
	fs := flag.NewFlagSet("stats rebuild", flag.ExitOnError)
	cfgPath := fs.String("c", "config.yaml", "config file to resolve log_dir from (audit input default and statistics directory)")
	dir := fs.String("dir", "", "statistics directory (default <log_dir>/stats)")
	from := fs.String("from", "", "first day to rebuild, YYYY-MM-DD (default: earliest day in the audit log)")
	to := fs.String("to", "", "last day to rebuild, YYYY-MM-DD (default: yesterday, or today with -today)")
	today := fs.Bool("today", false, "also rebuild today — vmr must be stopped (takes the statistics lock); use it in the deploy window")
	force := fs.Bool("force", false, "replace days that already have a statistics file")
	dryRun := fs.Bool("dry-run", false, "only report what would be written")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		cfg, err := config.Load(*cfgPath)
		if err != nil {
			return fmt.Errorf("load %s for log_dir (or pass -dir): %w", *cfgPath, err)
		}
		*dir = filepath.Join(cfg.LogDir, "stats")
	}
	paths, err := resolveInputPaths(fs, *cfgPath)
	if err != nil {
		return err
	}
	return runStatsRebuild(paths, rebuildOpts{dir: *dir, from: *from, to: *to, today: *today, force: *force, dryRun: *dryRun, now: time.Now()}, os.Stdout)
}

type rebuildOpts struct {
	dir, from, to string
	today, force  bool
	dryRun        bool
	now           time.Time
}

type rebuildDay struct {
	records int
	events  []stats.Event
}

func runStatsRebuild(paths []string, o rebuildOpts, out io.Writer) error {
	today := o.now.In(time.Local).Format("2006-01-02")
	days := map[string]*rebuildDay{}
	var total, parseErrs, skipped, undated int
	for _, p := range paths {
		rc, err := audit.OpenLogFile(p)
		if err != nil {
			return err
		}
		err = audit.ForEachLine(rc, audit.MaxLogLine, func(line []byte) {
			e, ok, err := statsfeed.FromJSON(line)
			if err != nil {
				parseErrs++
				return
			}
			total++
			if e.TS.IsZero() {
				undated++
				return
			}
			day := stats.DayOf(e)
			if day > today || (day == today && !o.today) || (o.from != "" && day < o.from) || (o.to != "" && day > o.to) {
				return
			}
			d := days[day]
			if d == nil {
				d = &rebuildDay{}
				days[day] = d
			}
			d.records++
			if ok {
				e.Src = stats.SrcAudit
				d.events = append(d.events, e)
			}
		}, func() { skipped++ })
		rc.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}

	keys := make([]string, 0, len(days))
	for k := range days {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "day\taudit records\tevents\taction\t")
	var written, kept int
	for _, day := range keys {
		d := days[day]
		action := "written"
		switch {
		case len(d.events) == 0:
			action = "nothing to book"
		case !o.force && stats.DayFileExists(o.dir, day):
			action = "exists (use -force to replace)"
			kept++
		case o.dryRun:
			action = "would write"
		case day == today:
			if err := stats.WriteToday(o.dir, d.events, o.now); err != nil {
				return err
			}
			action = "written (today)"
			written++
		default:
			if err := stats.WriteDay(o.dir, day, d.events, o.now); err != nil {
				return err
			}
			written++
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t\n", day, d.records, len(d.events), action)
	}
	tw.Flush()
	fmt.Fprintf(out, "\n%d audit records read from %d file(s); %d day(s) written, %d kept; statistics directory %s\n", total, len(paths), written, kept, o.dir)
	if parseErrs > 0 || skipped > 0 || undated > 0 {
		fmt.Fprintf(out, "%d unparseable line(s), %d oversized line(s), %d line(s) without a timestamp skipped\n", parseErrs, skipped, undated)
	}
	if o.dryRun {
		fmt.Fprintln(out, "dry run: nothing was written")
	}
	return nil
}
