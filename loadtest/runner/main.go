// Ver 2026-07-24 13:30, by Sonnet 5

// runner is the one-command version of the manual steps in loadtest/README.md:
// starts loadtest/mockupstream and vmr, generates targets.json (and its two
// cost-regime subsets, see gentargets), fires each load profile in profiles
// through Vegeta — as two separate attacks, plain-request scenarios and
// image-processing scenarios, so image decode/scale/encode's real cost
// doesn't get blended into everyone else's percentiles — runs `vmr report`
// once at the end, and writes a combined Markdown report to
// reports/loadtest-report.md.
// The audit log lives under logs/loadtest/ — a subdirectory of the same
// logs/ tree real vmr instances use, not the shared top level: the audit
// filename (vmr-audit-YYYY-MM-DD.jsonl) has no prefix knob, and this run
// wipes its log dir clean before starting, so mixing with — or clobbering —
// real audit data is a real risk if it pointed at logs/ directly.
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
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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
	vmrAddr       = "127.0.0.1:8801"
	mockAddr      = "127.0.0.1:9900"
	vmrBinary     = "./vmr"
	configPath    = "loadtest/config.yaml"
	logDir        = "logs/loadtest" // must match loadtest/config.yaml's log_dir
	reportsDir    = "reports"
	reportOutPath = "reports/loadtest-report.md"

	// gentargets splits its 11 scenarios into two Vegeta targets files by
	// cost regime — image decode/scale/encode (big_image/multi_image/gif)
	// vs everything else — plus the combined file (kept for manual poking,
	// see loadtest/README.md; unused by this runner).
	targetsPath      = "loadtest/targets.json"
	targetsPlainPath = "loadtest/targets-plain.json"
	targetsImagePath = "loadtest/targets-image.json"
	plainScenarios   = 8 // must match gentargets' non-image scenario count
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

	fmt.Println("== vmr report ==")
	// `vmr report`'s output filenames are fixed (vmr-report.md, etc., no
	// prefix option) — staged in a throwaway temp dir instead of reportsDir
	// directly, so this can never collide with or overwrite a real report
	// already sitting in reports/. Only the synthesized reportOutPath below
	// (client + server view combined, this run's actual deliverable) is
	// kept; the raw vmr-report.* staging output is discarded.
	stagingDir, err := os.MkdirTemp("", "vmr-loadtest-report-*")
	if err != nil {
		return fmt.Errorf("create report staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	logFiles, err := filepath.Glob(filepath.Join(logDir, "vmr-audit-*.jsonl"))
	if err != nil || len(logFiles) == 0 {
		return fmt.Errorf("no audit log files under %s (err=%v)", logDir, err)
	}
	reportArgs := append([]string{"report", "-o", stagingDir}, logFiles...)
	if out, err := exec.Command(vmrBinary, reportArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("vmr report: %w\n%s", err, out)
	}

	byModel, endpoints, err := extractTables(filepath.Join(stagingDir, "vmr-report.md"))
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
	plain   vegetaReport // baseline/stream_normal/.../failover/anthropic_baseline — no image processing
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

// extractTables pulls the "按模型" and "端点可用度" sections out of vmr's own
// generated report.md verbatim — reusing vmr's own rendering instead of
// re-parsing vmr-report.json and reimplementing table formatting.
func extractTables(path string) (byModel, endpoints string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("read %s: %w", path, err)
	}
	md := string(data)
	byModel = extractSection(md, "## 按模型")
	endpoints = extractSection(md, "## 端点可用度")
	if byModel == "" || endpoints == "" {
		return "", "", fmt.Errorf("expected sections not found in %s — vmr-report.md's headings may have changed", path)
	}
	return byModel, endpoints, nil
}

func extractSection(md, heading string) string {
	i := strings.Index(md, heading)
	if i < 0 {
		return ""
	}
	rest := md[i:]
	nl := strings.Index(rest, "\n")
	if nl < 0 {
		return rest
	}
	if next := strings.Index(rest[nl+1:], "\n## "); next >= 0 {
		return strings.TrimSpace(rest[:nl+1+next])
	}
	return strings.TrimSpace(rest)
}

func writeReport(results []roundResult, byModel, endpoints string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- generated by `go run ./loadtest/runner` on %s -->\n\n", time.Now().Format(time.RFC3339))
	fmt.Fprint(&b, "# vmr load test report\n\n")
	fmt.Fprintf(&b, "Design and how to read this: [`docs/VirtualModelRouter_System_Design_v3.md`](../docs/VirtualModelRouter_System_Design_v3.md) §12, [`loadtest/README.md`](../loadtest/README.md). %d load rounds against the same 11 scenarios.\n\n", len(results))

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
	fmt.Fprint(&b, "vmr's own `ttft_ms`/`dur_ms` instrumentation, grouped by virtual model (= scenario) — this is where the per-scenario cost breakdown comes from (§1 of the design doc: no custom report code, this is `vmr report`'s own output, extracted verbatim).\n\n")
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
