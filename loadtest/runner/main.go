// Ver 2026-07-24 13:30, by Sonnet 5

// runner is the one-command version of the manual steps in loadtest/README.md:
// starts loadtest/mockupstream and vmr, generates targets.json (and its two
// cost-regime subsets, see gentargets), fires each load profile in profiles
// through Vegeta — as two separate attacks, plain-request scenarios and
// image-processing scenarios, so image decode/scale/encode's real cost
// doesn't get blended into everyone else's percentiles — and writes a
// combined Markdown report to reports/loadtest-report.md.
// The audit log lives under logs/loadtest/ — a subdirectory of the same
// logs/ tree real vmr instances use, not the shared top level: the audit
// filename (vmr-audit-YYYY-MM-DD.jsonl) has no prefix knob, and this run
// wipes its log dir clean before starting, so mixing with — or clobbering —
// real audit data is a real risk if it pointed at logs/ directly.
//
// This tool is deliberately self-contained: the "server-side view" numbers
// come from parsing this run's own audit JSONL directly (computeServerStats
// below), never from running `vmr report` or importing any vmr-internal
// package. A load test measures vmr's HTTP surface under load — it has no
// business depending on a separate command's (internal/report's) rendering
// pipeline succeeding, existing, or keeping a particular output shape. Run
// this having never once run `vmr report` against anything, and the result
// is identical.
//
// Requires: vegeta on PATH (go install github.com/tsenart/vegeta@latest)
// and a built ./vmr binary at the repo root (go build -o vmr ./cmd/vmr).
//
// Usage (from the repo root): go run ./loadtest/runner
//
// Not part of the shipped vmr binary — `go build ./cmd/vmr` never touches
// this directory.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vmr/loadtest/addr"
)

// profiles are the "typical combinations" — escalating rate/duration so the
// report shows whether latency holds up as load increases, not just a
// single snapshot. Adjust here if you want more/less signal; there's
// nothing sacred about these three.
var profiles = []loadProfile{
	{"light", 10, 10 * time.Second},
	{"moderate", 50, 20 * time.Second},
	{"heavy", 150, 20 * time.Second},
}

type loadProfile struct {
	name     string
	rate     int
	duration time.Duration
}

const (
	vmrAddr       = addr.VMR
	mockAddr      = addr.Mock
	vmrBinary     = "./vmr"
	configPath    = "loadtest/config.yaml"
	logDir        = "logs/loadtest" // must match loadtest/config.yaml's log_dir
	reportsDir    = "reports"
	reportOutPath = "reports/loadtest-report.md"

	// gentargets splits its 12 scenarios into two Vegeta targets files by
	// cost regime — image decode/scale/encode (big_image/multi_image/gif)
	// vs everything else — plus the combined file (kept for manual poking,
	// see loadtest/README.md; unused by this runner).
	targetsPath      = "loadtest/targets.json"
	targetsPlainPath = "loadtest/targets-plain.json"
	targetsImagePath = "loadtest/targets-image.json"
	plainScenarios   = 9 // must match gentargets' non-image scenario count
	imageScenarios   = 3 // must match gentargets' image scenario count
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "runner:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", reportOutPath)
}

func run() error {
	if _, err := exec.LookPath("vegeta"); err != nil {
		return fmt.Errorf("vegeta not on PATH — go install github.com/tsenart/vegeta@latest: %w", err)
	}
	if _, err := os.Stat(vmrBinary); err != nil {
		return fmt.Errorf("%s not found — go build -o vmr ./cmd/vmr first: %w", vmrBinary, err)
	}

	fmt.Println("== building mockupstream ==")
	mockBinary := filepath.Join(os.TempDir(), "vmr-loadtest-mockupstream")
	if out, err := exec.Command("go", "build", "-o", mockBinary, "./loadtest/mockupstream").CombinedOutput(); err != nil {
		return fmt.Errorf("build mockupstream: %w\n%s", err, out)
	}
	defer os.Remove(mockBinary)

	fmt.Println("== starting mockupstream ==")
	// A built binary, not `go run` — `go run` leaves an orphaned child process
	// behind on Kill() (it only kills the go-run wrapper, not the compiled
	// subprocess it spawns), same as vmr's own binary below.
	mock := exec.Command(mockBinary)
	mock.Stderr = os.Stderr
	if err := mock.Start(); err != nil {
		return fmt.Errorf("start mockupstream: %w", err)
	}
	defer mock.Process.Kill()
	if err := waitReady(mockAddr, 10*time.Second); err != nil {
		return fmt.Errorf("mockupstream never came up: %w", err)
	}

	fmt.Println("== resetting", logDir, "==")
	// image_cache_dir (loadtest/config.yaml) is deliberately nested under
	// logDir, so this same wipe also clears it — necessary now that
	// gentargets' cache-busting image variants are deterministic (same
	// content every run): without a clean cache dir, a second run would
	// find its own "fresh" variants already warm from the previous run.
	// imgprep.cacheStore recreates the subdirectory on first write, so
	// there's nothing else to set up here.
	os.RemoveAll(logDir)
	os.MkdirAll(logDir, 0o755)

	fmt.Println("== starting vmr ==")
	vmr := exec.Command(vmrBinary, "start", "-c", configPath)
	vmr.Stderr = os.Stderr
	if err := vmr.Start(); err != nil {
		return fmt.Errorf("start vmr: %w", err)
	}
	defer vmr.Process.Kill()
	if err := waitReady(vmrAddr, 10*time.Second); err != nil {
		return fmt.Errorf("vmr never came up: %w", err)
	}

	fmt.Println("== generating targets ==")
	if out, err := exec.Command("go", "run", "./loadtest/gentargets").CombinedOutput(); err != nil {
		return fmt.Errorf("gentargets: %w\n%s", err, out)
	}
	// Regenerated fresh every run (embeds synthetic images/GIFs — big_image/
	// multi_image each as a pool of distinct variants, see gentargets'
	// cacheBustVariants doc comment for why one fixed image would hide the
	// decode/scale/encode cost these two scenarios exist to measure) —
	// never leave any of the three behind in the source directory.
	defer func() {
		os.Remove(targetsPath)
		os.Remove(targetsPlainPath)
		os.Remove(targetsImagePath)
	}()

	var results []roundResult
	for _, p := range profiles {
		// Two separate attacks, not one against the combined file: each
		// scenario's own share of the round's rate is kept the same as a
		// single 11-way attack would give it (scaleRate), so splitting the
		// report doesn't also silently change how hard this round hits
		// vmr — only which bucket each result's percentiles land in.
		plainRate := scaleRate(p.rate, plainScenarios)
		imageRate := scaleRate(p.rate, imageScenarios)
		fmt.Printf("== round %q: plain=%d/s image=%d/s duration=%s ==\n", p.name, plainRate, imageRate, p.duration)
		plainRep, err := attack(targetsPlainPath, plainRate, p.duration)
		if err != nil {
			return fmt.Errorf("round %s (plain): %w", p.name, err)
		}
		imageRep, err := attack(targetsImagePath, imageRate, p.duration)
		if err != nil {
			return fmt.Errorf("round %s (image): %w", p.name, err)
		}
		results = append(results, roundResult{profile: p, plain: plainRep, image: imageRep})
	}

	fmt.Println("== stopping vmr and mockupstream ==")
	vmr.Process.Signal(os.Interrupt)
	vmr.Wait()
	mock.Process.Kill()
	mock.Wait()

	fmt.Println("== computing server-side stats from this run's own audit log ==")
	logFiles, err := filepath.Glob(filepath.Join(logDir, "vmr-audit-*.jsonl"))
	if err != nil || len(logFiles) == 0 {
		return fmt.Errorf("no audit log files under %s (err=%v)", logDir, err)
	}
	byModel, endpoints, err := computeServerStats(logFiles)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(reportsDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", reportsDir, err)
	}
	return writeReport(results, byModel, endpoints)
}

func waitReady(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", addr)
}

type roundResult struct {
	profile loadProfile
	plain   vegetaReport // baseline/stream_normal/.../failover/anthropic_baseline/responses_baseline — no image processing
	image   vegetaReport // big_image/multi_image/gif — the one genuinely expensive code path
}

const totalScenarios = plainScenarios + imageScenarios

// scaleRate gives a group of groupScenarios (out of totalScenarios) the same
// per-scenario request rate a single combined attack across all 11 would
// have given it — round's nominal rate * groupScenarios/totalScenarios,
// rounded to nearest. Keeps the two-attacks split from silently changing how
// hard a round actually hits vmr; only the reporting is split, not the load
// itself. Minimum 1 so a light round never rounds a group down to zero.
func scaleRate(roundRate, groupScenarios int) int {
	r := (roundRate*groupScenarios + totalScenarios/2) / totalScenarios
	if r < 1 {
		r = 1
	}
	return r
}

// vegetaReport mirrors the subset of `vegeta report -type=json`'s schema we
// use — verified against a real run, not guessed from docs.
type vegetaReport struct {
	Latencies struct {
		Mean int64 `json:"mean"`
		P50  int64 `json:"50th"`
		P95  int64 `json:"95th"`
		P99  int64 `json:"99th"`
		Max  int64 `json:"max"`
	} `json:"latencies"`
	Requests    int64          `json:"requests"`
	Throughput  float64        `json:"throughput"`
	Success     float64        `json:"success"`
	StatusCodes map[string]int `json:"status_codes"`
	Errors      []string       `json:"errors"`
}

func attack(targetsPath string, rate int, duration time.Duration) (vegetaReport, error) {
	attackCmd := exec.Command("vegeta", "attack",
		"-targets="+targetsPath, "-format=json",
		fmt.Sprintf("-rate=%d", rate), "-duration="+duration.String())
	var attackOut bytes.Buffer
	attackCmd.Stdout = &attackOut
	attackCmd.Stderr = os.Stderr
	if err := attackCmd.Run(); err != nil {
		return vegetaReport{}, fmt.Errorf("vegeta attack: %w", err)
	}

	reportCmd := exec.Command("vegeta", "report", "-type=json")
	reportCmd.Stdin = &attackOut
	var reportOut bytes.Buffer
	reportCmd.Stdout = &reportOut
	reportCmd.Stderr = os.Stderr
	if err := reportCmd.Run(); err != nil {
		return vegetaReport{}, fmt.Errorf("vegeta report: %w", err)
	}

	var rep vegetaReport
	if err := json.Unmarshal(reportOut.Bytes(), &rep); err != nil {
		return vegetaReport{}, fmt.Errorf("parse vegeta report: %w", err)
	}
	return rep, nil
}

// auditRecord is the minimal subset of one audit JSONL line's fields this
// tool needs (see docs/VirtualModelRouter_Design_v4_Core.md §9.2 for the
// full schema). Deliberately hand-rolled here instead of importing
// vmr/internal/audit.Record: this load test computes its own numbers
// straight from the raw audit log its own vmr instance just wrote, with
// zero dependency on any vmr-internal package for this step — the same
// on-disk JSONL format an external tool (jq, DuckDB, a human) would read
// directly. A malformed or missing field just zero-values here rather than
// failing to compile; that trade is deliberate, see the package doc above.
type auditRecord struct {
	Model    string `json:"model"`
	DurMS    int64  `json:"dur_ms"`
	TTFTMS   int64  `json:"ttft_ms"`
	Attempts []struct {
		Endpoint   string `json:"endpoint"`
		ErrorClass string `json:"error_class"` // "" = this attempt succeeded
	} `json:"attempts"`
}

// modelStats accumulates one scenario (= virtual model)'s raw dur_ms/ttft_ms
// values for this tool's own p50/p95/max.
type modelStats struct {
	requests  int
	dur, ttft []int64
}

type endpointStats struct {
	attempts, ok int
}

// computeServerStats reads the audit JSONL files this load test run's own
// vmr instance just wrote (logFiles, under logDir) and computes the two
// tables loadtest-report.md's "server-side view" shows: per-model
// (=scenario) latency and per-endpoint availability — a scanner over plain
// JSON lines, nothing more. See the package doc for why this replaced an
// earlier version that shelled out to `vmr report` and parsed its output.
func computeServerStats(logFiles []string) (byModel, endpoints string, err error) {
	models := map[string]*modelStats{}
	eps := map[string]*endpointStats{}
	for _, path := range logFiles {
		if err := scanAuditFile(path, models, eps); err != nil {
			return "", "", err
		}
	}
	if len(models) == 0 || len(eps) == 0 {
		return "", "", fmt.Errorf("no records with model/attempts found across %d audit file(s) under %s", len(logFiles), logDir)
	}
	return renderModelStats(models), renderEndpointStats(eps), nil
}

func scanAuditFile(path string, models map[string]*modelStats, eps map[string]*endpointStats) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Audit lines embed full request/response bodies and can run to several
	// MB (image scenarios especially) — bufio.Scanner's 64KB default token
	// cap would silently truncate the scan with ErrTooLong well before that.
	scanner.Buffer(make([]byte, 0, 64<<10), 32<<20)
	for scanner.Scan() {
		var rec auditRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue // a malformed line shouldn't sink the whole load test summary
		}
		ms, ok := models[rec.Model]
		if !ok {
			ms = &modelStats{}
			models[rec.Model] = ms
		}
		ms.requests++
		if rec.DurMS > 0 {
			ms.dur = append(ms.dur, rec.DurMS)
		}
		if rec.TTFTMS > 0 {
			ms.ttft = append(ms.ttft, rec.TTFTMS)
		}
		for _, a := range rec.Attempts {
			if a.Endpoint == "" {
				continue
			}
			es, ok := eps[a.Endpoint]
			if !ok {
				es = &endpointStats{}
				eps[a.Endpoint] = es
			}
			es.attempts++
			if a.ErrorClass == "" {
				es.ok++
			}
		}
	}
	return scanner.Err()
}

// percentile returns sorted's p-th percentile (nearest-rank, p in [0,1]) —
// a self-contained implementation, not internal/report's: this tool
// doesn't need to match internal/report's exact percentile method, only to
// report a stable, documented one of its own. sorted must already be sorted
// ascending.
func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}

// renderModelStats is a deliberately minimal stand-in for vmr-report.md's
// own per-model table (internal/report/aggregate_render.go) — just the
// columns this report's readers actually look at (see loadtest/README.md's
// "reading the numbers" section). Sorted by model name for run-to-run
// stability — this tool's own map iteration would otherwise be
// non-deterministic across runs.
func renderModelStats(models map[string]*modelStats) string {
	names := make([]string, 0, len(models))
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("**按模型**（本次运行自己的审计日志现算，不经过 `vmr report`）\n\n")
	b.WriteString("| 模型 | 请求 | dur p50/p95/max | ttft p50/p95 |\n|---|---|---|---|\n")
	for _, name := range names {
		m := models[name]
		dur := append([]int64(nil), m.dur...)
		sort.Slice(dur, func(i, j int) bool { return dur[i] < dur[j] })
		ttft := append([]int64(nil), m.ttft...)
		sort.Slice(ttft, func(i, j int) bool { return ttft[i] < ttft[j] })
		var maxDur int64
		if len(dur) > 0 {
			maxDur = dur[len(dur)-1]
		}
		fmt.Fprintf(&b, "| %s | %d | %dms/%dms/%dms | %dms/%dms |\n",
			name, m.requests,
			percentile(dur, 0.5), percentile(dur, 0.95), maxDur,
			percentile(ttft, 0.5), percentile(ttft, 0.95))
	}
	return b.String()
}

// renderEndpointStats mirrors renderModelStats' rationale for the
// per-endpoint availability table.
func renderEndpointStats(eps map[string]*endpointStats) string {
	names := make([]string, 0, len(eps))
	for name := range eps {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("**端点可用度**（本次运行自己的审计日志现算——确认没有端点被 failover 卡住或悄悄绕过）\n\n")
	b.WriteString("| 端点 | 尝试 | 成功 | 可用度 |\n|---|---|---|---|\n")
	for _, name := range names {
		e := eps[name]
		var avail float64
		if e.attempts > 0 {
			avail = float64(e.ok) / float64(e.attempts) * 100
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %.1f%% |\n", name, e.attempts, e.ok, avail)
	}
	return b.String()
}

func writeReport(results []roundResult, byModel, endpoints string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- generated by `go run ./loadtest/runner` on %s -->\n\n", time.Now().Format(time.RFC3339))
	fmt.Fprint(&b, "# vmr load test report\n\n")
	fmt.Fprintf(&b, "Design and how to read this: [`docs/VirtualModelRouter_Design_v4_Core.md`](../docs/VirtualModelRouter_Design_v4_Core.md) §12, [`loadtest/README.md`](../loadtest/README.md). %d load rounds against the same %d scenarios.\n\n", len(results), totalScenarios)

	fmt.Fprint(&b, "## Client-side view (Vegeta), by load round\n\n")
	fmt.Fprintf(&b, "Fired as two separate attacks per round — **plain** (%d scenarios: everything except image processing) and **image** (%d scenarios: big_image/multi_image/gif, the only code path that actually decodes/scales/encodes) — each at its proportional share of the round's nominal rate, so this split changes nothing about how hard vmr is hit, only how the results are bucketed. Blending them into one number would let image processing's real cost quietly drag up the \"plain\" p95/p99 for everything else.\n\n", plainScenarios, imageScenarios)
	fmt.Fprint(&b, "| Round | Group | Rate | Duration | Requests | Success | p50 | p95 | p99 | Max |\n")
	fmt.Fprint(&b, "|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range results {
		writeClientRow(&b, r.profile, "plain", scaleRate(r.profile.rate, plainScenarios), r.plain)
		writeClientRow(&b, r.profile, "image", scaleRate(r.profile.rate, imageScenarios), r.image)
	}
	b.WriteString("\n")
	for _, r := range results {
		for _, g := range []struct {
			name string
			rep  vegetaReport
		}{{"plain", r.plain}, {"image", r.image}} {
			if g.rep.Success < 1.0 {
				fmt.Fprintf(&b, "⚠️ round %q (%s) had non-100%% success — status codes: %v, errors: %v\n\n", r.profile.name, g.name, g.rep.StatusCodes, g.rep.Errors)
			}
		}
	}

	fmt.Fprint(&b, "## Server-side view (vmr's own audit log), per scenario, all rounds combined\n\n")
	fmt.Fprint(&b, "vmr's own `ttft_ms`/`dur_ms` instrumentation, grouped by virtual model (= scenario) — this is where the per-scenario cost breakdown comes from, computed directly from this run's own audit JSONL (computeServerStats), not from `vmr report` — this tool never runs it.\n\n")
	b.WriteString(byModel)
	b.WriteString("\n\n")
	b.WriteString(endpoints)
	b.WriteString("\n")

	return os.WriteFile(reportOutPath, []byte(b.String()), 0o644)
}

func fmtMS(ns int64) string {
	return fmt.Sprintf("%.1fms", float64(ns)/1e6)
}

func writeClientRow(b *strings.Builder, p loadProfile, group string, rate int, rep vegetaReport) {
	fmt.Fprintf(b, "| %s | %s | %d/s | %s | %d | %.1f%% | %s | %s | %s | %s |\n",
		p.name, group, rate, p.duration,
		rep.Requests, rep.Success*100,
		fmtMS(rep.Latencies.P50), fmtMS(rep.Latencies.P95),
		fmtMS(rep.Latencies.P99), fmtMS(rep.Latencies.Max))
}
