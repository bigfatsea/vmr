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
// below), never from running `vmr analyze` or importing any vmr-internal
// package. A load test measures vmr's HTTP surface under load — it has no
// business depending on a separate command's (internal/report's) rendering
// pipeline succeeding, existing, or keeping a particular output shape. Run
// this having never once run `vmr analyze` against anything, and the result
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
	"debug/buildinfo"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	vmrAddr          = addr.VMR
	mockAddr         = addr.Mock
	vmrBinary        = "./vmr"
	configPath       = "loadtest/config.yaml"
	logDir           = "logs/loadtest" // must match loadtest/config.yaml's log_dir
	reportsDir       = "reports"
	reportOutPath    = "reports/loadtest-report.md"
	targetsPath      = "loadtest/targets.json"
	targetsPlainPath = "loadtest/targets-plain.json"
	targetsImagePath = "loadtest/targets-image.json"
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

	vcsRevision := getVCSRevision(vmrBinary)

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

	sampler := startResourceSampler(vmr.Process.Pid, 500*time.Millisecond)
	defer sampler.stop()

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

	plainTargets, err := parseTargets(targetsPlainPath)
	if err != nil {
		return fmt.Errorf("parse %s: %w", targetsPlainPath, err)
	}
	imageTargets, err := parseTargets(targetsImagePath)
	if err != nil {
		return fmt.Errorf("parse %s: %w", targetsImagePath, err)
	}
	totalScenarios := plainTargets.ScenarioCount() + imageTargets.ScenarioCount()

	// Warmup round: exercises both target sets at low rate to absorb lazy-init
	// and initial state machine costs before formal measurements start.
	// Server-side audit records with timestamps prior to round 1 are excluded.
	fmt.Println("== warmup (2s low-rate) ==")
	warmupPlainRate := (plainTargets.ScenarioCount() + 1) / 2
	if warmupPlainRate < 2 {
		warmupPlainRate = 2
	}
	warmupImageRate := (imageTargets.ScenarioCount() + 1) / 2
	if warmupImageRate < 2 {
		warmupImageRate = 2
	}
	if _, err := attack(targetsPlainPath, warmupPlainRate, 2*time.Second); err != nil {
		return fmt.Errorf("warmup (plain): %w", err)
	}
	if _, err := attack(targetsImagePath, warmupImageRate, 2*time.Second); err != nil {
		return fmt.Errorf("warmup (image): %w", err)
	}

	var results []roundResult
	for _, p := range profiles {
		// Two separate attacks, not one against the combined file: each
		// scenario's own share of the round's rate is kept the same as a
		// combined attack would give it (scaleRate), so splitting the
		// report doesn't silently change how hard this round hits vmr.
		plainRate := scaleRate(p.rate, plainTargets.ScenarioCount(), totalScenarios)
		imageRate := scaleRate(p.rate, imageTargets.ScenarioCount(), totalScenarios)
		fmt.Printf("== round %q: plain=%d/s image=%d/s duration=%s ==\n", p.name, plainRate, imageRate, p.duration)
		roundStart := time.Now()
		plainRep, err := attack(targetsPlainPath, plainRate, p.duration)
		if err != nil {
			return fmt.Errorf("round %s (plain): %w", p.name, err)
		}
		imageRep, err := attack(targetsImagePath, imageRate, p.duration)
		if err != nil {
			return fmt.Errorf("round %s (image): %w", p.name, err)
		}
		roundEnd := time.Now()
		results = append(results, roundResult{
			profile:   p,
			startTime: roundStart,
			endTime:   roundEnd,
			plain:     plainRep,
			image:     imageRep,
		})
	}

	fmt.Println("== stopping vmr and mockupstream ==")
	resReport := sampler.stop()
	vmr.Process.Signal(os.Interrupt)
	vmr.Wait()
	mock.Process.Kill()
	mock.Wait()

	fmt.Println("== computing server-side stats from this run's own audit log ==")
	logFiles, err := filepath.Glob(filepath.Join(logDir, "vmr-audit-*.jsonl"))
	if err != nil || len(logFiles) == 0 {
		return fmt.Errorf("no audit log files under %s (err=%v)", logDir, err)
	}
	byModel, endpoints, roundModels, err := computeServerStats(logFiles, results)
	if err != nil {
		return err
	}

	// Consistency assertion: verify actual bucketed audit requests against
	// target line shares and total Vegeta requests within ±20% tolerance.
	if err := assertScenarioConsistency(plainTargets, imageTargets, results, roundModels); err != nil {
		return err
	}

	if err := os.MkdirAll(reportsDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", reportsDir, err)
	}
	return writeReport(results, byModel, endpoints, plainTargets, imageTargets, vcsRevision, resReport)
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
	profile   loadProfile
	startTime time.Time
	endTime   time.Time
	plain     vegetaReport
	image     vegetaReport
}

// scaleRate gives a group of groupScenarios (out of totalScenarios) the same
// per-scenario request rate a single combined attack would have given it —
// round's nominal rate * groupScenarios/totalScenarios, rounded to nearest.
// Minimum 1 so a light round never rounds a group down to zero.
func scaleRate(roundRate, groupScenarios, totalScenarios int) int {
	if totalScenarios <= 0 {
		return roundRate
	}
	r := (roundRate*groupScenarios + totalScenarios/2) / totalScenarios
	if r < 1 {
		r = 1
	}
	return r
}

// targetsInfo holds dynamically parsed metadata from a Vegeta targets file.
type targetsInfo struct {
	path       string
	totalLines int
	models     []string           // distinct models in encounter order
	modelLines map[string]int     // model -> target line count
	shares     map[string]float64 // model -> line count / totalLines
}

func (t *targetsInfo) ScenarioCount() int {
	return len(t.models)
}

// parseTargets reads a Vegeta JSON-lines targets file and computes scenario shares.
func parseTargets(path string) (*targetsInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	info := &targetsInfo{
		path:       path,
		modelLines: make(map[string]int),
		shares:     make(map[string]float64),
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), 32<<20)

	type targetLine struct {
		Body string `json:"body"`
	}
	type bodyModel struct {
		Model string `json:"model"`
	}

	for scanner.Scan() {
		info.totalLines++
		var line targetLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			return nil, fmt.Errorf("unmarshal target line %d in %s: %w", info.totalLines, path, err)
		}
		rawBody, err := base64.StdEncoding.DecodeString(line.Body)
		if err != nil {
			return nil, fmt.Errorf("decode target body at line %d in %s: %w", info.totalLines, path, err)
		}
		var bm bodyModel
		if err := json.Unmarshal(rawBody, &bm); err != nil {
			return nil, fmt.Errorf("unmarshal target body JSON at line %d in %s: %w", info.totalLines, path, err)
		}
		if bm.Model == "" {
			return nil, fmt.Errorf("target body at line %d in %s missing model field", info.totalLines, path)
		}
		if info.modelLines[bm.Model] == 0 {
			info.models = append(info.models, bm.Model)
		}
		info.modelLines[bm.Model]++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	if info.totalLines == 0 {
		return nil, fmt.Errorf("targets file %s is empty", path)
	}
	for _, m := range info.models {
		info.shares[m] = float64(info.modelLines[m]) / float64(info.totalLines)
	}
	return info, nil
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
// vmr/internal/audit.Record to remain completely self-contained.
type auditRecord struct {
	TS       time.Time `json:"ts"`
	Model    string    `json:"model"`
	DurMS    int64     `json:"dur_ms"`
	TTFTMS   int64     `json:"ttft_ms"`
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

// computeServerStats reads the audit JSONL files, buckets records by profile
// round using request arrival ts (excluding warmup), and generates markdown tables.
func computeServerStats(logFiles []string, results []roundResult) (byModel, endpoints string, roundModels []map[string]*modelStats, err error) {
	roundModels = make([]map[string]*modelStats, len(results))
	for i := range roundModels {
		roundModels[i] = make(map[string]*modelStats)
	}
	eps := make(map[string]*endpointStats)
	for _, path := range logFiles {
		if err := scanAuditFile(path, results, roundModels, eps); err != nil {
			return "", "", nil, err
		}
	}
	var totalRecords int
	for _, rm := range roundModels {
		totalRecords += len(rm)
	}
	if totalRecords == 0 || len(eps) == 0 {
		return "", "", nil, fmt.Errorf("no formal records with model/attempts found across %d audit file(s) under %s", len(logFiles), logDir)
	}
	return renderModelStats(results, roundModels), renderEndpointStats(eps), roundModels, nil
}

func scanAuditFile(path string, results []roundResult, roundModels []map[string]*modelStats, eps map[string]*endpointStats) error {
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
		if len(results) == 0 || rec.TS.Before(results[0].startTime) {
			continue // exclude warmup requests prior to round 1
		}
		roundIdx := -1
		for i := 0; i < len(results); i++ {
			if !rec.TS.Before(results[i].startTime) {
				if i == len(results)-1 || rec.TS.Before(results[i+1].startTime) {
					roundIdx = i
					break
				}
			}
		}
		if roundIdx < 0 {
			continue
		}

		ms, ok := roundModels[roundIdx][rec.Model]
		if !ok {
			ms = &modelStats{}
			roundModels[roundIdx][rec.Model] = ms
		}
		ms.requests++
		// 0ms is sub-ms (<=1ms left-censored), included rather than dropped.
		if rec.DurMS >= 0 {
			ms.dur = append(ms.dur, rec.DurMS)
		}
		if rec.TTFTMS >= 0 {
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

// percentile returns sorted's p-th percentile using floor(p*(n-1))
// (PERCENTILE.INC without interpolation, p in [0,1]). A self-contained
// implementation, not internal/report's: this tool deliberately does not
// import internal/report to avoid coupling to its rendering pipeline.
// sorted must already be sorted ascending.
func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}

// renderModelStats renders per-model latency broken down by load round,
// allowing degradation across escalating load profiles to be seen at a glance.
func renderModelStats(results []roundResult, roundModels []map[string]*modelStats) string {
	allModels := make(map[string]bool)
	for _, rm := range roundModels {
		for name := range rm {
			allModels[name] = true
		}
	}
	names := make([]string, 0, len(allModels))
	for name := range allModels {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("**按模型 · 按负载轮**（本次运行自己的审计日志现算，按请求 ts 分桶进各轮，不经过 `vmr analyze`；首轮前预热已排除）\n\n")
	b.WriteString("| 模型 | 轮次 | 请求 | dur p50/p95/max (0ms = sub-ms, included) | ttft p50/p95 (0ms = sub-ms, included) |\n|---|---|---|---|---|\n")
	for _, name := range names {
		for i, r := range results {
			rm := roundModels[i]
			m := rm[name]
			if m == nil || m.requests == 0 {
				fmt.Fprintf(&b, "| %s | %s | 0 | - | - |\n", name, r.profile.name)
				continue
			}
			dur := append([]int64(nil), m.dur...)
			sort.Slice(dur, func(i, j int) bool { return dur[i] < dur[j] })
			ttft := append([]int64(nil), m.ttft...)
			sort.Slice(ttft, func(i, j int) bool { return ttft[i] < ttft[j] })
			var maxDur int64
			if len(dur) > 0 {
				maxDur = dur[len(dur)-1]
			}
			durStr := fmt.Sprintf("%dms/%dms/%dms", percentile(dur, 0.5), percentile(dur, 0.95), maxDur)
			ttftStr := fmt.Sprintf("%dms/%dms", percentile(ttft, 0.5), percentile(ttft, 0.95))
			fmt.Fprintf(&b, "| %s | %s | %d | %s | %s |\n", name, r.profile.name, m.requests, durStr, ttftStr)
		}
	}
	return b.String()
}

// renderEndpointStats renders per-endpoint availability across all formal rounds.
func renderEndpointStats(eps map[string]*endpointStats) string {
	names := make([]string, 0, len(eps))
	for name := range eps {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("**端点可用度**（本次运行自己的审计日志现算——确认没有端点被 failover 卡住或悄悄绕过；首轮前预热已排除）\n\n")
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

// assertScenarioConsistency asserts that actual request counts for every scenario
// match expected counts computed from target line shares within ±20% tolerance.
func assertScenarioConsistency(
	plainTargets, imageTargets *targetsInfo,
	results []roundResult,
	roundModels []map[string]*modelStats,
) error {
	var totalPlainReq, totalImageReq int64
	for _, r := range results {
		totalPlainReq += r.plain.Requests
		totalImageReq += r.image.Requests
	}

	actualRequests := make(map[string]int64)
	for _, rm := range roundModels {
		for model, stats := range rm {
			actualRequests[model] += int64(stats.requests)
		}
	}

	checkGroup := func(groupName string, targets *targetsInfo, totalReq int64) error {
		for _, model := range targets.models {
			share := targets.shares[model]
			expected := float64(totalReq) * share
			actual := actualRequests[model]
			if actual == 0 {
				return fmt.Errorf("consistency assertion failed: scenario %q in %s group missing from audit records (expected ~%.1f requests)",
					model, groupName, expected)
			}
			if expected <= 0 {
				return fmt.Errorf("consistency assertion failed: scenario %q in %s group has invalid expected count (%.1f)",
					model, groupName, expected)
			}
			diffRatio := (float64(actual) - expected) / expected
			if math.Abs(diffRatio) > 0.20 {
				return fmt.Errorf("consistency assertion failed: scenario %q in %s group request count mismatch: expected ~%.1f, got %d (diff: %+.1f%%, tolerance: ±20%%)",
					model, groupName, expected, actual, diffRatio*100)
			}
		}
		return nil
	}

	if err := checkGroup("plain", plainTargets, totalPlainReq); err != nil {
		return err
	}
	if err := checkGroup("image", imageTargets, totalImageReq); err != nil {
		return err
	}

	allKnown := make(map[string]bool)
	for _, m := range plainTargets.models {
		allKnown[m] = true
	}
	for _, m := range imageTargets.models {
		allKnown[m] = true
	}
	for model := range actualRequests {
		if !allKnown[model] {
			return fmt.Errorf("consistency assertion failed: unexpected scenario %q in audit log", model)
		}
	}
	return nil
}

func getVCSRevision(binaryPath string) string {
	bi, err := buildinfo.ReadFile(binaryPath)
	if err != nil {
		return "unknown"
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return "unknown"
}

type resourceStats struct {
	PeakRSSKB   int64
	LastCPURaw  string
	LastCPUTime time.Duration
	Samples     int
}

type resourceSampler struct {
	pid      int
	ticker   *time.Ticker
	stopCh   chan struct{}
	doneCh   chan struct{}
	mu       sync.Mutex
	stopOnce sync.Once
	stats    resourceStats
}

func startResourceSampler(pid int, interval time.Duration) *resourceSampler {
	s := &resourceSampler{
		pid:    pid,
		ticker: time.NewTicker(interval),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	s.sample()
	go func() {
		defer close(s.doneCh)
		for {
			select {
			case <-s.ticker.C:
				s.sample()
			case <-s.stopCh:
				s.ticker.Stop()
				s.sample()
				return
			}
		}
	}()
	return s
}

func (s *resourceSampler) stop() resourceStats {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		<-s.doneCh
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

func (s *resourceSampler) sample() {
	cmd := exec.Command("ps", "-o", "rss=,time=", "-p", strconv.Itoa(s.pid))
	out, err := cmd.Output()
	if err != nil {
		return
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return
	}
	rssKB, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return
	}
	cpuRaw := fields[1]
	cpuDur, _ := parseCPUTime(cpuRaw)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.Samples++
	if rssKB > s.stats.PeakRSSKB {
		s.stats.PeakRSSKB = rssKB
	}
	s.stats.LastCPURaw = cpuRaw
	s.stats.LastCPUTime = cpuDur
}

func parseCPUTime(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty cpu time")
	}
	var days int
	if idx := strings.Index(s, "-"); idx != -1 {
		fmt.Sscanf(s[:idx], "%d", &days)
		s = s[idx+1:]
	}
	parts := strings.Split(s, ":")
	var hours, mins int
	var secs float64
	switch len(parts) {
	case 2:
		fmt.Sscanf(parts[0], "%d", &mins)
		fmt.Sscanf(parts[1], "%f", &secs)
	case 3:
		fmt.Sscanf(parts[0], "%d", &hours)
		fmt.Sscanf(parts[1], "%d", &mins)
		fmt.Sscanf(parts[2], "%f", &secs)
	default:
		return 0, fmt.Errorf("unrecognized time format: %q", s)
	}
	totalSecs := float64(days*86400+hours*3600+mins*60) + secs
	return time.Duration(totalSecs * float64(time.Second)), nil
}

func writeReport(
	results []roundResult,
	byModel, endpoints string,
	plainTargets, imageTargets *targetsInfo,
	vcsRevision string,
	resReport resourceStats,
) error {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- generated by `go run ./loadtest/runner` on %s -->\n\n", time.Now().Format(time.RFC3339))
	fmt.Fprint(&b, "# vmr load test report\n\n")
	totalScenarios := plainTargets.ScenarioCount() + imageTargets.ScenarioCount()
	fmt.Fprintf(&b, "Design and how to read this: [`docs/VirtualModelRouter_Design_v4_Core.md`](../docs/VirtualModelRouter_Design_v4_Core.md) §12, [`loadtest/README.md`](../loadtest/README.md). %d load rounds against the same %d scenarios (binary revision: `%s`).\n\n",
		len(results), totalScenarios, vcsRevision)

	fmt.Fprint(&b, "## Client-side view (Vegeta), by load round\n\n")
	imageScenariosDesc := strings.Join(imageTargets.models, "/")
	fmt.Fprintf(&b, "Fired as two separate attacks per round — **plain** (%d scenarios: everything except image processing) and **image** (%d scenarios: %s, the only code path that actually decodes/scales/encodes) — each at its proportional share of the round's nominal rate, so this split changes nothing about how hard vmr is hit, only how the results are bucketed. Blending them into one number would let image processing's real cost quietly drag up the \"plain\" p95/p99 for everything else.\n\n",
		plainTargets.ScenarioCount(), imageTargets.ScenarioCount(), imageScenariosDesc)
	fmt.Fprint(&b, "| Round | Group | Rate | Duration | Requests | Success | p50 | p95 | p99 | Max |\n")
	fmt.Fprint(&b, "|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range results {
		writeClientRow(&b, r.profile, "plain", scaleRate(r.profile.rate, plainTargets.ScenarioCount(), totalScenarios), r.plain)
		writeClientRow(&b, r.profile, "image", scaleRate(r.profile.rate, imageTargets.ScenarioCount(), totalScenarios), r.image)
	}
	b.WriteString("\n*Note: `~` prefix on p99 indicates sample size < 200 requests (statistically noisy).*\n\n")
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

	fmt.Fprint(&b, "## Server-side view (vmr's own audit log), per scenario, by load round\n\n")
	fmt.Fprint(&b, "vmr's own `ttft_ms`/`dur_ms` instrumentation, grouped by virtual model (= scenario) and bucketed into load rounds by request timestamp (warmup before round 1 excluded; 0ms = sub-ms, included) — this is where the per-scenario cost breakdown comes from, computed directly from this run's own audit JSONL (computeServerStats), not from `vmr analyze` — this tool never runs it.\n\n")
	b.WriteString(byModel)
	b.WriteString("\n\n")
	b.WriteString(endpoints)
	b.WriteString("\n\n")

	fmt.Fprint(&b, "## Resource usage (vmr process)\n\n")
	fmt.Fprintf(&b, "Sampled every 500ms over the full sweep (%d samples):\n", resReport.Samples)
	peakMB := float64(resReport.PeakRSSKB) / 1024.0
	cpuDurStr := fmt.Sprintf("%.2fs", resReport.LastCPUTime.Seconds())
	if resReport.LastCPURaw != "" {
		cpuDurStr += fmt.Sprintf(" (ps: %s)", resReport.LastCPURaw)
	}
	fmt.Fprintf(&b, "- **Peak RSS**: %.1f MB (%d KB)\n", peakMB, resReport.PeakRSSKB)
	fmt.Fprintf(&b, "- **Total CPU time**: %s\n", cpuDurStr)

	return os.WriteFile(reportOutPath, []byte(b.String()), 0o600)
}

func fmtMS(ns int64) string {
	return fmt.Sprintf("%.1fms", float64(ns)/1e6)
}

func writeClientRow(b *strings.Builder, p loadProfile, group string, rate int, rep vegetaReport) {
	p99 := fmtMS(rep.Latencies.P99)
	if rep.Requests < 200 {
		p99 = "~" + p99
	}
	fmt.Fprintf(b, "| %s | %s | %d/s | %s | %d | %.1f%% | %s | %s | %s | %s |\n",
		p.name, group, rate, p.duration,
		rep.Requests, rep.Success*100,
		fmtMS(rep.Latencies.P50), fmtMS(rep.Latencies.P95),
		p99, fmtMS(rep.Latencies.Max))
}
