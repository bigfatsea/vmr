package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/stats"
)

// TestStatsRebuild drives `vmr stats rebuild` over a real audit file:
// closed days are written through the statsfeed projection, today is never
// touched, existing days are kept unless -force, and -dry-run writes
// nothing.
func TestStatsRebuild(t *testing.T) {
	logDir := t.TempDir()
	statsDir := filepath.Join(logDir, "stats")
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	yesterday := now.AddDate(0, 0, -1)

	lg, err := audit.New(logDir)
	if err != nil {
		t.Fatal(err)
	}
	ok := audit.Attempt{Provider: "p1", Model: "m1", Forwarded: true, Response: &audit.Message{Status: 200}, Tokens: &audit.TokenCount{In: 3, Out: 4}}
	for _, r := range []*audit.Record{
		{TS: yesterday.Add(-2 * time.Hour), Model: "vm", Outcome: "ok", Attempts: []audit.Attempt{ok}},
		{TS: yesterday.Add(-1 * time.Hour), Model: "vm", Outcome: "error", Client: audit.Exchange{Response: &audit.Message{Status: 503}}},
		{TS: yesterday.Add(-1 * time.Hour), Outcome: "error"},                                       // pre-probe: not booked
		{TS: yesterday, Model: "vm", Outcome: "ok", ReplayOf: "x:1", Attempts: []audit.Attempt{ok}}, // replay: not booked
		{TS: now.Add(-time.Hour), Model: "vm", Outcome: "ok", Attempts: []audit.Attempt{ok}},        // today: never rebuilt
	} {
		if err := lg.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	path := lg.Path()
	lg.Close()
	day := yesterday.Format("2006-01-02")

	var out bytes.Buffer
	if err := runStatsRebuild([]string{path}, rebuildOpts{dir: statsDir, dryRun: true, now: now}, &out); err != nil {
		t.Fatal(err)
	}
	if stats.DayFileExists(statsDir, day) || !strings.Contains(out.String(), "would write") {
		t.Fatalf("dry run wrote or did not report:\n%s", out.String())
	}

	out.Reset()
	if err := runStatsRebuild([]string{path}, rebuildOpts{dir: statsDir, now: now}, &out); err != nil {
		t.Fatal(err)
	}
	if !stats.DayFileExists(statsDir, day) || stats.DayFileExists(statsDir, now.Format("2006-01-02")) {
		t.Fatalf("rebuild must write yesterday and never today:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "left alone") || !strings.Contains(out.String(), "-today") {
		t.Errorf("skipping today must be said out loud, with the way to include it:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(statsDir, day+".jsonl.zst")); err != nil {
		t.Fatalf("closed day must be compressed: %v", err)
	}

	st, err := stats.Open(statsDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	evs := st.Events(stats.EventsQuery{Limit: 10})
	st.Close()
	if len(evs) != 2 {
		t.Fatalf("rebuilt events = %d, want 2 (pre-probe and replay records are not booked)", len(evs))
	}
	for _, e := range evs {
		if e.Src != stats.SrcAudit {
			t.Errorf("rebuilt event must be marked src=audit: %+v", e)
		}
	}

	out.Reset()
	if err := runStatsRebuild([]string{path}, rebuildOpts{dir: statsDir, now: now}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "exists") {
		t.Errorf("an existing day must be kept without -force:\n%s", out.String())
	}
	out.Reset()
	if err := runStatsRebuild([]string{path}, rebuildOpts{dir: statsDir, force: true, now: now}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "written") {
		t.Errorf("-force must rewrite:\n%s", out.String())
	}
}

// A request that crosses midnight is filed under the day it completed in,
// the same rule the live path follows, so rebuilding next to live days can
// neither double-count nor drop it.
func TestStatsRebuild_FilesByCompletionDay(t *testing.T) {
	logDir := t.TempDir()
	statsDir := filepath.Join(logDir, "stats")
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	lg, err := audit.New(logDir)
	if err != nil {
		t.Fatal(err)
	}
	ok := audit.Attempt{Provider: "p1", Model: "m1", Forwarded: true, Response: &audit.Message{Status: 200}, Tokens: &audit.TokenCount{In: 3, Out: 4}}
	late := time.Date(2026, 10, 16, 23, 59, 30, 0, time.Local)
	for _, r := range []*audit.Record{
		{TS: late, DurMS: 90_000, Model: "vm", Outcome: "ok", Attempts: []audit.Attempt{ok}}, // completes on the 17th
		{TS: late.Add(-time.Hour), DurMS: 1000, Model: "vm", Outcome: "ok", Attempts: []audit.Attempt{ok}},
		{Outcome: "error"}, // no timestamp, no model
	} {
		if err := lg.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	path := lg.Path()
	lg.Close()
	var out bytes.Buffer
	if err := runStatsRebuild([]string{path}, rebuildOpts{dir: statsDir, now: now}, &out); err != nil {
		t.Fatal(err)
	}
	if !stats.DayFileExists(statsDir, "2026-10-17") || !stats.DayFileExists(statsDir, "2026-10-16") {
		t.Fatalf("want one event in each of the 16th and the 17th:\n%s", out.String())
	}
	if strings.Contains(out.String(), "0001-01-01") || !strings.Contains(out.String(), "without a timestamp") {
		t.Errorf("an undated record must be counted, not listed as a day:\n%s", out.String())
	}
}

// TestStatsRebuild_Today: -today also rebuilds today, but only when no vmr
// holds the statistics directory (the deploy window); with a live store
// open it refuses instead of racing the live writer.
func TestStatsRebuild_Today(t *testing.T) {
	logDir := t.TempDir()
	statsDir := filepath.Join(logDir, "stats")
	now := time.Now()
	lg, err := audit.New(logDir)
	if err != nil {
		t.Fatal(err)
	}
	ok := audit.Attempt{Provider: "p1", Model: "m1", Forwarded: true, Response: &audit.Message{Status: 200}, Tokens: &audit.TokenCount{In: 3, Out: 4}}
	if err := lg.Write(&audit.Record{TS: now.Add(-time.Second), Model: "vm", Outcome: "ok", Attempts: []audit.Attempt{ok}}); err != nil {
		t.Fatal(err)
	}
	path := lg.Path()
	lg.Close()
	today := now.Format("2006-01-02")

	var out bytes.Buffer
	if err := runStatsRebuild([]string{path}, rebuildOpts{dir: statsDir, now: now}, &out); err != nil {
		t.Fatal(err)
	}
	if stats.DayFileExists(statsDir, today) {
		t.Fatal("without -today, today must not be written")
	}

	live, err := stats.Open(statsDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A live store creates today's file, so replacing it also needs -force.
	err = runStatsRebuild([]string{path}, rebuildOpts{dir: statsDir, today: true, force: true, now: now}, &out)
	live.Close()
	if runtime.GOOS != "windows" && (err == nil || !strings.Contains(err.Error(), "stop it first")) {
		t.Fatalf("-today with a live store open = %v, want a refusal", err)
	}

	out.Reset()
	if err := runStatsRebuild([]string{path}, rebuildOpts{dir: statsDir, today: true, force: true, now: now}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(statsDir, today+".jsonl")); err != nil {
		t.Fatalf("today must be written plain for the next start to append to: %v\n%s", err, out.String())
	}
	st, err := stats.Open(statsDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if evs := st.Events(stats.EventsQuery{Limit: 10}); len(evs) != 1 || evs[0].Src != stats.SrcAudit {
		t.Errorf("today's rebuilt events = %+v, want one src=audit event", evs)
	}
}
