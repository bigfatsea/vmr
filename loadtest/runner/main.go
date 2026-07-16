// Ver 2026-07-16 00:00, by Sonnet 5

// runner is the one-command version of the manual steps in loadtest/README.md:
// starts loadtest/mockupstream and vmr, generates targets.json, fires each
// load profile in profiles through Vegeta, runs `vmr report` once at the
// end, and writes a combined Markdown report to loadtest/report.md.
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
	targetsPath   = "loadtest/targets.json"
	logDir        = "loadtest/logs"
	reportOutDir  = "loadtest/report_data"
	reportOutPath = "loadtest/report.md"
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

	fmt.Println("== resetting loadtest/logs ==")
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

	var results []roundResult
	for _, p := range profiles {
		fmt.Printf("== round %q: rate=%d/s duration=%s ==\n", p.name, p.rate, p.duration)
		rep, err := attack(p)
		if err != nil {
			return fmt.Errorf("round %s: %w", p.name, err)
		}
		results = append(results, roundResult{profile: p, report: rep})
	}

	fmt.Println("== stopping vmr and mockupstream ==")
	vmr.Process.Signal(os.Interrupt)
	vmr.Wait()
	mock.Process.Kill()
	mock.Wait()

	fmt.Println("== vmr report ==")
	os.RemoveAll(reportOutDir)
	logFiles, err := filepath.Glob(filepath.Join(logDir, "vmr-audit-*.jsonl"))
	if err != nil || len(logFiles) == 0 {
		return fmt.Errorf("no audit log files under %s (err=%v)", logDir, err)
	}
	reportArgs := append([]string{"report", "-o", reportOutDir}, logFiles...)
	if out, err := exec.Command(vmrBinary, reportArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("vmr report: %w\n%s", err, out)
	}

	byModel, endpoints, err := extractTables(filepath.Join(reportOutDir, "vmr-report.md"))
	if err != nil {
		return err
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
	report  vegetaReport
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

func attack(p loadProfile) (vegetaReport, error) {
	attackCmd := exec.Command("vegeta", "attack",
		"-targets="+targetsPath, "-format=json",
		fmt.Sprintf("-rate=%d", p.rate), "-duration="+p.duration.String())
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
	fmt.Fprintf(&b, "Design and how to read this: [`docs/PerformanceTesting_Design_Sonnet5.md`](../docs/PerformanceTesting_Design_Sonnet5.md), [`loadtest/README.md`](README.md). %d load rounds against the same 11 scenarios.\n\n", len(results))

	fmt.Fprint(&b, "## Client-side view (Vegeta), by load round\n\n")
	fmt.Fprint(&b, "All 11 scenarios mixed together within each round — this is what an external caller experiences as load increases.\n\n")
	fmt.Fprint(&b, "| Round | Rate | Duration | Requests | Success | p50 | p95 | p99 | Max |\n")
	fmt.Fprint(&b, "|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range results {
		fmt.Fprintf(&b, "| %s | %d/s | %s | %d | %.1f%% | %s | %s | %s | %s |\n",
			r.profile.name, r.profile.rate, r.profile.duration,
			r.report.Requests, r.report.Success*100,
			fmtMS(r.report.Latencies.P50), fmtMS(r.report.Latencies.P95),
			fmtMS(r.report.Latencies.P99), fmtMS(r.report.Latencies.Max))
	}
	b.WriteString("\n")
	for _, r := range results {
		if r.report.Success < 1.0 {
			fmt.Fprintf(&b, "⚠️ round %q had non-100%% success — status codes: %v, errors: %v\n\n", r.profile.name, r.report.StatusCodes, r.report.Errors)
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
