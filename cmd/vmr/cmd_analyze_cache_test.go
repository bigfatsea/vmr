// Ver 2026-09-15, by pi

package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/i18n"
	"vmr/internal/journey"
	"vmr/internal/report"
)

// readDirFiles reads all regular files in dir into a map[filename]content.
func readDirFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		// Ignore internal cache directories, temporary files, and manifest.json (which has wall-clock time)
		if strings.HasPrefix(rel, ".") || strings.HasSuffix(rel, ".tmp") || rel == "manifest.json" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("readDirFiles %s: %v", dir, err)
	}
	return out
}

// assertOutputsConsistent compares two sets of directory outputs with 1e-6 float tolerance on JSON values.
func assertOutputsConsistent(t *testing.T, got, want map[string]string) {
	t.Helper()
	for file, wantContent := range want {
		gotContent, ok := got[file]
		if !ok {
			t.Errorf("missing expected file %s in cached run", file)
			continue
		}
		if strings.HasSuffix(file, ".json") {
			// For JSON files, parse and compare floats with 1e-6 tolerance
			var gotVal, wantVal any
			if err := json.Unmarshal([]byte(gotContent), &gotVal); err == nil {
				if err := json.Unmarshal([]byte(wantContent), &wantVal); err == nil {
					if !deepEqualFloats(gotVal, wantVal, 1e-6) {
						t.Errorf("JSON file %s values differ beyond 1e-6 tolerance", file)
					}
					continue
				}
			}
		}
		if gotContent != wantContent {
			t.Errorf("file %s differs between cold and warm run", file)
		}
	}
}

// deepEqualFloats checks deep equality with a tolerance on float64 values.
func deepEqualFloats(a, b any, tol float64) bool {
	switch va := a.(type) {
	case float64:
		vb, ok := b.(float64)
		if !ok {
			return false
		}
		return math.Abs(va-vb) <= tol
	case map[string]any:
		vb, ok := b.(map[string]any)
		if !ok || len(va) != len(vb) {
			return false
		}
		for k, v := range va {
			// Skip generated_at timestamps in comparisons
			if k == "generated_at" {
				continue
			}
			if !deepEqualFloats(v, vb[k], tol) {
				return false
			}
		}
		return true
	case []any:
		vb, ok := b.([]any)
		if !ok || len(va) != len(vb) {
			return false
		}
		for i := range va {
			if !deepEqualFloats(va[i], vb[i], tol) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

// TestAnalyzeCache_ColdWarmAndNoCache verifies:
// 1. Cold start builds the cache record (.cache/fingerprint.json).
// 2. Second consecutive analyze hits L2/L3 cache, producing identical outputs.
// 3. -no-cache flag forces full re-aggregation and matches cold start outputs.
func TestAnalyzeCache_ColdWarmAndNoCache(t *testing.T) {
	path := crossCheckFixture(t)
	outDir := filepath.Join(t.TempDir(), "reports")

	// 1. Cold start
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, path})
	}); err != nil {
		t.Fatalf("Cold start cmdAnalyze: %v", err)
	}

	rec1, err := report.LoadCacheRecord(outDir)
	if err != nil || rec1 == nil {
		t.Fatalf("Expected .cache/fingerprint.json to exist after cold start: %v", err)
	}
	if rec1.L2Digest == "" || rec1.L3Digest == "" {
		t.Fatalf("Cache record has empty digests: %+v", rec1)
	}

	coldOutputs := readDirFiles(t, outDir)

	// 2. Warm run (hits cache)
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, path})
	}); err != nil {
		t.Fatalf("Warm run cmdAnalyze: %v", err)
	}

	rec2, err := report.LoadCacheRecord(outDir)
	if err != nil || rec2 == nil {
		t.Fatalf("Expected cache record after warm run: %v", err)
	}
	if rec2.L2Digest != rec1.L2Digest || rec2.L3Digest != rec1.L3Digest {
		t.Fatalf("Warm run modified digest unexpectedly: %+v vs %+v", rec2, rec1)
	}

	warmOutputs := readDirFiles(t, outDir)
	assertOutputsConsistent(t, warmOutputs, coldOutputs)

	// 3. -no-cache run
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-no-cache", "-o", outDir, path})
	}); err != nil {
		t.Fatalf("-no-cache cmdAnalyze: %v", err)
	}

	noCacheOutputs := readDirFiles(t, outDir)
	assertOutputsConsistent(t, noCacheOutputs, coldOutputs)
}

// TestAnalyzeCache_InvalidationMatrix tests the entire invalidation matrix (§7.4):
// 1. Audit log changed -> L2 & L3 invalidated.
// 2. Config pricing/exchange rate changed -> L2 & L3 invalidated.
// 3. Report config (self-traffic tags) changed -> L2 & L3 invalidated.
// 4. Analysis parameter (-lang) changed -> L2 & L3 invalidated.
// 4b/4c. Self-traffic exclusion (llm_key tag) / LLM identity (journey/compare
//    modes only) changed -> L2 digest changes.
// 5. Presentation layer modified (.md deleted) -> L2 hits, L3 invalidated and re-renders .md.
// 6. Format version mismatch -> L2 & L3 invalidated.
// 7. Binary version without format change -> L2 & L3 hit.

// baseRun builds the minimal analyzeRun mirroring a default-flags `vmr
// analyze -o <outDir> <path>` invocation's resolved values, so digest-level
// assertions in the invalidation matrix stay in sync with what the real run
// persisted.
func baseRun(path, outDir string) *analyzeRun {
	return &analyzeRun{
		paths:   []string{path},
		outDir:  outDir,
		lang:    i18n.EN,
		noCache: false,
	}
}

func TestAnalyzeCache_InvalidationMatrix(t *testing.T) {
	path1 := crossCheckFixture(t)
	outDir := filepath.Join(t.TempDir(), "reports")

	// Baseline run
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, path1})
	}); err != nil {
		t.Fatalf("Baseline cmdAnalyze: %v", err)
	}
	baseRec, err := report.LoadCacheRecord(outDir)
	if err != nil || baseRec == nil {
		t.Fatalf("Baseline cache record missing: %v", err)
	}

	// Case 1: Audit log changed -> L2 invalidated
	t0 := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	path2 := writeJourneyJSONL(t, []audit.Record{
		journeyRec(t0, []any{journeyMsg("user", "completely different log")}, journeySSE("ok")),
	})
	targetL2LogChanged, _ := computeTargetL2(&analyzeRun{
		paths:   []string{path2},
		outDir:  outDir,
		noCache: false,
	}, "default")
	if _, hit := report.CheckL2Cache(outDir, targetL2LogChanged); hit {
		t.Fatalf("Changing audit log must invalidate L2 cache")
	}

	// Case 2: Config pricing/exchange rate changed -> L2 invalidated
	cfgFile := filepath.Join(t.TempDir(), "config.yaml")
	cfgContent1 := `listen: 127.0.0.1:8800
providers:
  - name: p1
    base_url: {openai-completions: "https://api.example.com"}
    api_key: "sk-test"
models:
  coding:
    endpoints:
      openai-completions:
        - providers: [p1]
          models: [mod1]
exchange_rate:
  CNY: 7.2
`
	if err := os.WriteFile(cfgFile, []byte(cfgContent1), 0o600); err != nil {
		t.Fatal(err)
	}
	outDirCfg := filepath.Join(t.TempDir(), "reports_cfg")
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-c", cfgFile, "-o", outDirCfg, path1})
	}); err != nil {
		t.Fatalf("Analyze with cfg 7.2: %v", err)
	}
	recPricing1, _ := report.LoadCacheRecord(outDirCfg)

	cfgContent2 := strings.Replace(cfgContent1, "CNY: 7.2", "CNY: 8.5", 1)
	if err := os.WriteFile(cfgFile, []byte(cfgContent2), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-c", cfgFile, "-o", outDirCfg, path1})
	}); err != nil {
		t.Fatalf("Analyze with modified config pricing: %v", err)
	}
	recPricing2, _ := report.LoadCacheRecord(outDirCfg)
	if recPricing1.L2Digest == recPricing2.L2Digest {
		t.Fatalf("Changing exchange rate in config.yaml must invalidate L2 digest: got %s", recPricing1.L2Digest)
	}

	// Case 3: Report config changed (e.g. self traffic tags) -> L2 invalidated
	repCfgFile := filepath.Join(t.TempDir(), "report.yaml")
	if err := os.WriteFile(repCfgFile, []byte("self_traffic_client_tags: [test_tag_new]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-report-config", repCfgFile, "-o", outDir, path1})
	}); err != nil {
		t.Fatalf("Analyze with modified report config: %v", err)
	}
	recRepCfg, _ := report.LoadCacheRecord(outDir)
	if recRepCfg.L2Digest == baseRec.L2Digest {
		t.Fatalf("Changing report config self-traffic tags must invalidate L2 digest")
	}

	// Case 4: Analysis parameter changed (-lang zh) -> L2 invalidated
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-lang", "zh", "-o", outDir, path1})
	}); err != nil {
		t.Fatalf("Analyze with -lang zh: %v", err)
	}
	recLang, _ := report.LoadCacheRecord(outDir)
	if recLang.L2Digest == baseRec.L2Digest {
		t.Fatalf("Changing -lang must invalidate L2 digest")
	}

	// Case 4b: effective self-traffic exclusion changed via -llm-key -> the
	// digest changes (the key's own tag is part of the exclusion set even
	// though report.yaml's self_traffic_client_tags didn't move).
	baselineDefault, ok := computeTargetL2(baseRun(path1, outDir), "default")
	if !ok {
		t.Fatalf("computeTargetL2 baseline: not ok")
	}
	withKey := *baseRun(path1, outDir)
	withKey.llmKey = "sk-self-traffic-key"
	targetL2Key, ok := computeTargetL2(&withKey, "default")
	if !ok {
		t.Fatalf("computeTargetL2 with llm key: not ok")
	}
	if targetL2Key == baselineDefault {
		t.Fatalf("Adding -llm-key (self-traffic exclusion) must change the L2 digest")
	}

	// Case 4c: -llm-addr identity on a journey-zoom run -> digest changes, so
	// an L2 hit cannot silently skip a requested LLM interpretation; the same
	// identity on the default suite (which never consumes it) must NOT change
	// the digest.
	withLLM := *baseRun(path1, outDir)
	withLLM.llmAddr, withLLM.llmModel, withLLM.llmAddrExplicit = "127.0.0.1:8800", "coding", true
	targetL2LLM, ok := computeTargetL2(&withLLM, "journey:j-x")
	if !ok {
		t.Fatalf("computeTargetL2 with llm addr: not ok")
	}
	targetL2LLMDefault, ok := computeTargetL2(&withLLM, "default")
	if !ok {
		t.Fatalf("computeTargetL2 with llm addr (default): not ok")
	}
	if targetL2LLM == baselineDefault {
		t.Fatalf("Adding -llm-addr on a -journey run must change the L2 digest")
	}
	if targetL2LLMDefault != baselineDefault {
		t.Fatalf("LLM identity must not affect the default suite's L2 digest (it is never consumed there)")
	}

	// Case 5: Presentation layer modified (delete vmr-report.md) -> L2 hits, L3 re-renders .md
	// Restore default English analysis first
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, path1})
	}); err != nil {
		t.Fatalf("Restore analyze: %v", err)
	}
	reportMDPath := filepath.Join(outDir, "vmr-report.md")
	if err := os.Remove(reportMDPath); err != nil {
		t.Fatalf("Remove vmr-report.md: %v", err)
	}
	// Run again: L2 should hit, and L3 should notice missing .md and re-render it from JSON!
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, path1})
	}); err != nil {
		t.Fatalf("Run after removing .md: %v", err)
	}
	if _, err := os.Stat(reportMDPath); err != nil {
		t.Fatalf("vmr-report.md should have been regenerated by L3 cache refresh: %v", err)
	}

	// Case 6: Format version upgrade -> L2 invalidated
	staleRec := *baseRec
	staleRec.FormatVersion = report.ManifestFormat - 1
	if err := report.SaveCacheRecord(outDir, &staleRec); err != nil {
		t.Fatal(err)
	}
	rawL2, _ := computeTargetL2(&analyzeRun{
		paths:  []string{path1},
		outDir: outDir,
	}, "default")
	if _, hit := report.CheckL2Cache(outDir, rawL2); hit {
		t.Fatalf("Older format version must invalidate L2 cache")
	}

	// Case 7: Binary version change without format change -> All hit
	// (CheckL2Cache and CheckL3Cache check FormatVersion and RendererVersion, not build VCS commit hash)
	if err := report.SaveCacheRecord(outDir, baseRec); err != nil {
		t.Fatal(err)
	}
	if _, hit := report.CheckL2Cache(outDir, rawL2); !hit {
		t.Fatalf("Same format version must hit L2 cache")
	}
}

// TestAnalyzeCache_RenderOnlyOrthogonality tests that -render-only works
// independently from whether L2 is valid, executing JSON->VM->MD without touching audit logs.
func TestAnalyzeCache_RenderOnlyOrthogonality(t *testing.T) {
	path := crossCheckFixture(t)
	outDir := filepath.Join(t.TempDir(), "reports")

	// 1. Create baseline snapshot
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, path})
	}); err != nil {
		t.Fatalf("Initial cmdAnalyze: %v", err)
	}

	reportMD := filepath.Join(outDir, "vmr-report.md")
	origContent, err := os.ReadFile(reportMD)
	if err != nil {
		t.Fatal(err)
	}

	// Delete report.md
	if err := os.Remove(reportMD); err != nil {
		t.Fatal(err)
	}

	// Invalidate L2 cache by corrupting L2Digest in fingerprint.json
	rec, _ := report.LoadCacheRecord(outDir)
	rec.L2Digest = "invalid_l2_digest"
	_ = report.SaveCacheRecord(outDir, rec)

	// Run -render-only: it must succeed purely from disk JSON despite L2 being invalid
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-render-only", "-o", outDir})
	}); err != nil {
		t.Fatalf("-render-only should work even when L2 is invalid: %v", err)
	}

	newContent, err := os.ReadFile(reportMD)
	if err != nil {
		t.Fatalf("vmr-report.md should be recreated by -render-only: %v", err)
	}
	if string(newContent) != string(origContent) {
		t.Fatalf("Re-rendered content differs from original")
	}

	// Test -render-only with -no-cache
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-render-only", "-no-cache", "-o", outDir})
	}); err != nil {
		t.Fatalf("-render-only with -no-cache should work: %v", err)
	}
}

// TestAnalyzeCache_L2HitSweepsOrphanJourneys pins the D20/§3.4 sweep
// contract on the L2-hit path: the orphan sweep is the default-suite full
// run's job, and an L2 hit still IS that run — files landing in
// journeys/details/ between two identical runs (manual copies, crashed zoom
// runs) must not survive the replay. Zoom-mode L2 digests differ from the
// default suite's, so a zoom run's L2 hit can never trigger this sweep.
func TestAnalyzeCache_L2HitSweepsOrphanJourneys(t *testing.T) {
	path := crossCheckFixture(t)
	outDir := filepath.Join(t.TempDir(), "reports")

	// Cold run: establishes the snapshot and its L2 digest.
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, path})
	}); err != nil {
		t.Fatalf("cold start: %v", err)
	}

	// Plant an orphan in journeys/details/ — anything a prior zoom run or a
	// manual copy could have left there.
	orphanJSON := filepath.Join(outDir, "journeys", "details", "j-ghost-20260101T000000-20260101T000001-deadbee1.json")
	orphanMD := strings.TrimSuffix(orphanJSON, ".json") + ".md"
	for _, p := range []string{orphanJSON, orphanMD} {
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatalf("plant orphan: %v", err)
		}
	}

	// Warm run: L2 hit (same inputs/params — verified by the digest not
	// moving), and the orphan must be swept despite the sweep not living in
	// dispatchDefaultSuite on this path.
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, path})
	}); err != nil {
		t.Fatalf("warm run: %v", err)
	}

	if _, err := os.Stat(orphanJSON); !os.IsNotExist(err) {
		t.Fatalf("orphan json survived an L2-hit full run: %v", err)
	}
	if _, err := os.Stat(orphanMD); !os.IsNotExist(err) {
		t.Fatalf("orphan md survived an L2-hit full run: %v", err)
	}

	// The real journeys must be untouched by the sweep.
	entries, err := os.ReadDir(filepath.Join(outDir, "journeys", "details"))
	if err != nil {
		t.Fatalf("read details dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("sweep removed every journey detail, not just the orphan")
	}
}

// TestAnalyzeCache_ZoomArtifactMissingRebuilt (N-B4) asserts that when an
// artifact produced by a zoom mode (-compare or -journey) is deleted between
// runs, L2 cache check detects the missing output and falls back to a full
// rebuild rather than silently succeeding and leaving the artifact missing.
func TestAnalyzeCache_ZoomArtifactMissingRebuilt(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")

	aU1 := journeyMsg("user", "candidate A for zoom cache test")
	aR1 := journeyRec(at(0), []any{sys, aU1}, journeySSE("开工 A"))
	aR2 := journeyRec(at(1), []any{sys, aU1, journeyMsg("assistant", "done A")}, journeySSE("完成 A"))

	bU1 := journeyMsg("user", "candidate B for zoom cache test")
	bR1 := journeyRec(at(10), []any{sys, bU1}, journeySSE("开工 B"))
	bR2 := journeyRec(at(11), []any{sys, bU1, journeyMsg("assistant", "done B")}, journeySSE("完成 B"))

	path := writeJourneyJSONL(t, []audit.Record{aR1, aR2, bR1, bR2})
	outDir := filepath.Join(t.TempDir(), "reports")

	// Initial default run to establish index.
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, path})
	}); err != nil {
		t.Fatalf("setup run: %v", err)
	}

	idx := journey.LoadJourneyIndex(filepath.Join(outDir, "journeys", "index.json"))
	if idx == nil || len(idx.Journeys) < 2 {
		t.Fatalf("expected at least 2 journeys in index, got %+v", idx)
	}
	idA := idx.Journeys[0].ID
	idB := idx.Journeys[1].ID

	// Run compare once
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-compare", idA + "," + idB, path})
	}); err != nil {
		t.Fatalf("initial compare run: %v", err)
	}

	cmpJSON := filepath.Join(outDir, "compares", "compare-"+idA+"-vs-"+idB+".json")
	if _, err := os.Stat(cmpJSON); err != nil {
		t.Fatalf("expected compare JSON to exist: %v", err)
	}

	// Delete compare JSON
	if err := os.Remove(cmpJSON); err != nil {
		t.Fatalf("failed to delete compare JSON: %v", err)
	}

	// Re-run the exact same compare run without -no-cache
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-compare", idA + "," + idB, path})
	}); err != nil {
		t.Fatalf("subsequent compare run: %v", err)
	}

	// It must be rebuilt!
	if _, err := os.Stat(cmpJSON); err != nil {
		t.Fatalf("N-B4 failure: compare JSON was not rebuilt when missing on L2 hit: %v", err)
	}

	// Test single -journey as well
	jJSON := filepath.Join(outDir, "journeys", "details", idA+".json")
	if _, err := os.Stat(jJSON); err != nil {
		t.Fatalf("expected journey JSON to exist: %v", err)
	}
	if err := os.Remove(jJSON); err != nil {
		t.Fatalf("failed to delete journey JSON: %v", err)
	}

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-journey", idA, path})
	}); err != nil {
		t.Fatalf("subsequent journey run: %v", err)
	}

	if _, err := os.Stat(jJSON); err != nil {
		t.Fatalf("N-B4 failure: journey JSON was not rebuilt when missing on L2 hit: %v", err)
	}
}

// TestAnalyzeCache_QuotaJSONInvalidatesL2 pins NEW-D: §2.5 / finance.json's
// provider_quotas are a function of <log_dir>/vmr-quota.json (a live counter
// file the routing half rewrites on every charged request). It changes
// rendered numbers exactly like the audit inputs do, so its content must
// fold into the L2 digest — otherwise an L2 hit on the same logs serves a
// frozen quota snapshot. Only folded when the config actually declares a
// quota limit (so a quota-less config never sees a spurious invalidation).
func TestAnalyzeCache_QuotaJSONInvalidatesL2(t *testing.T) {
	path1 := crossCheckFixture(t)
	logDir := t.TempDir()
	quotaPath := filepath.Join(logDir, "vmr-quota.json")
	writeQuota := func(requests int) {
		body := `{"version":1,"accounts":{"p1":{"requests/1mo":{"period_start":1785000000,"counters":{"fresh":0,"cache_read":0,"cache_write":0,"out":0,"requests":` +
			strconv.Itoa(requests) + `},"estimated":0}}}}` + "\n"
		if err := os.WriteFile(quotaPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cfgQuota := "listen: 127.0.0.1:8800\nlog_dir: " + logDir + `
providers:
  - name: p1
    base_url: {openai-completions: "https://api.example.com"}
    api_key: "sk-test"
    quota:
      limits:
        - metric: requests
          every: 1mo
          since: 2026-07-22
          amount: 18000
models:
  coding:
    endpoints:
      openai-completions:
        - providers: [p1]
          models: [mod1]
`
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgQuota), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, cfgErr := config.Load(cfgPath)
	if cfgErr != nil {
		t.Fatalf("config.Load: %v", cfgErr)
	}

	run := &analyzeRun{paths: []string{path1}, outDir: t.TempDir(), lang: i18n.EN, cfg: cfg}

	writeQuota(10)
	d1, ok := computeTargetL2(run, "default")
	if !ok {
		t.Fatal("computeTargetL2 not ok (quota v1)")
	}
	writeQuota(9000) // the routing half burned more of the plan
	d2, ok := computeTargetL2(run, "default")
	if !ok {
		t.Fatal("computeTargetL2 not ok (quota v2)")
	}
	if d1 == d2 {
		t.Fatalf("vmr-quota.json content change did not invalidate the L2 digest — §2.5 would serve a stale snapshot on the next L2 hit")
	}

	// Negative: a config with NO quota limits must ignore the quota file
	// entirely (no spurious invalidation).
	cfgNoQuota := strings.Replace(cfgQuota, `    quota:
      limits:
        - metric: requests
          every: 1mo
          since: 2026-07-22
          amount: 18000
`, "", 1)
	noQPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(noQPath, []byte(cfgNoQuota), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgNoQ, err := config.Load(noQPath)
	if err != nil {
		t.Fatalf("config.Load (no quota): %v", err)
	}
	runNoQ := &analyzeRun{paths: []string{path1}, outDir: t.TempDir(), lang: i18n.EN, cfg: cfgNoQ}
	writeQuota(1)
	n1, _ := computeTargetL2(runNoQ, "default")
	writeQuota(99999)
	n2, _ := computeTargetL2(runNoQ, "default")
	if n1 != n2 {
		t.Fatalf("quota-less config: vmr-quota.json change must NOT affect the L2 digest")
	}
}
