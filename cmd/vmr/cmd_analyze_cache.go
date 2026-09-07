// Ver 2026-09-07, by pi

// vmr analyze's product-level cache layer (Phase 4, D1/§7.3): the L2 digest
// computation over (input hashes, pricing fingerprint, format version,
// analysis params), the L2/L3 hit checks, and the cache-record writes for
// both the full-run path and -render-only. Split out of cmd_analyze.go so
// that file stays "pure CLI-layer routing" and inside its archtest line
// budget; the cache concerns share their test file cmd_analyze_cache_test.go.
package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vmr/internal/dashboard"
	"vmr/internal/i18n"
	"vmr/internal/journey"
	"vmr/internal/report"
)

func analyzeModeString(r *analyzeRun) string {
	switch {
	case r.macroOnly:
		return "macro-only"
	case r.listOnly:
		return "list-only"
	case r.benchmarkFlag:
		return "benchmark"
	case r.compareArg != "":
		return "compare:" + r.compareArg
	case r.journeyArg != "":
		return "journey:" + r.journeyArg
	case r.journeyOnly:
		return "journey-only"
	default:
		return "default"
	}
}

// configHasQuotaLimits reports whether any provider declares a quota limit —
// i.e. whether §2.5 / finance.json's provider_quotas will render at all.
func configHasQuotaLimits(r *analyzeRun) bool {
	if r.cfg == nil {
		return false
	}
	for _, p := range r.cfg.Providers {
		if p.Quota != nil && len(p.Quota.Limits) > 0 {
			return true
		}
	}
	return false
}

func computeTargetL2(r *analyzeRun, mode string) ([32]byte, bool) {
	inHashes, err := report.ComputeInputHashes(r.paths)
	if err != nil || len(inHashes) == 0 {
		return [32]byte{}, false
	}
	// NEW-D: when the config carries quota limits, §2.5 and finance.json's
	// provider_quotas are a function of <log_dir>/vmr-quota.json — a live
	// counter file the routing half rewrites on every charged request, plus
	// wall-clock-derived period progress. It changes rendered numbers exactly
	// like the audit inputs do (§7.2's "does it change any persisted value"
	// test), so its content must invalidate L2. File absent => nothing
	// folded; a later appearance flips the digest, which is correct (the
	// report gains the live column).
	if r.cfg != nil && r.cfg.LogDir != "" && configHasQuotaLimits(r) {
		qp := filepath.Join(r.cfg.LogDir, "vmr-quota.json")
		if _, statErr := os.Stat(qp); statErr == nil {
			if qh, hErr := report.ComputeInputHashes([]string{qp}); hErr == nil && len(qh) == 1 {
				inHashes = append(inHashes, qh[0])
			}
		}
	}
	pricingFP := resolvePricingFingerprint(r.cfg, r.exchangeRate)
	// LLM identity rides the params fingerprint only on the modes that
	// consume it: on -journey/-compare an L2 hit must not silently swallow a
	// requested -llm-addr interpretation, while on the batch shapes the
	// resolved value is deliberately ignored (never consulted downstream).
	llmAddr, llmModel := "", ""
	if strings.HasPrefix(mode, "journey:") || strings.HasPrefix(mode, "compare:") {
		llmAddr, llmModel = r.llmAddr, r.llmModel
	}
	paramsFP := report.ComputeAnalysisParamsFingerprint(report.AnalysisParams{
		Lang:               r.lang.String(),
		TaskProfile:        resolveTaskProfile().Name(),
		IncludePartial:     r.includePartial,
		IncludeSelfTraffic: r.includeSelfTraffic,
		SelfTrafficTags:    r.selfTrafficTags,
		LLMSelfTag:         llmSelfTag(r.llmKey),
		LLMAddr:            llmAddr,
		LLMModel:           llmModel,
		DisplayCCY:         r.displayCCY,
		RenderAll:          r.renderAllFlag,
		Details:            r.detailsOn,
		Mode:               mode,
	})
	return report.ComputeL2Digest(inHashes, pricingFP, report.ManifestFormat, paramsFP), true
}

func tryL2Cache(r *analyzeRun, targetL2 [32]byte, mode string) bool {
	if r.noCache {
		return false
	}
	rec, hit := report.CheckL2Cache(r.outDir, targetL2)
	if !hit {
		return false
	}
	if zoomArtifactMissing(r, mode) {
		return false
	}
	vmFP, err := report.ComputeVMFingerprintFromManifest(r.outDir)
	if err != nil {
		return false
	}
	targetL3 := report.ComputeL3Digest(vmFP[:], report.RendererVersion, r.lang.String())
	l3Hit := report.CheckL3Cache(r.outDir, targetL3, mode)
	if !l3Hit {
		if err := renderAllFromDisk(r.outDir, r.lang); err != nil {
			return false
		}
		rec.VMFingerprint = hex.EncodeToString(vmFP[:])
		rec.L3Digest = hex.EncodeToString(targetL3[:])
		rec.RendererVersion = report.RendererVersion
		_ = report.SaveCacheRecord(r.outDir, rec)
	}

	if err := dashboard.WriteSkeletons(r.outDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: dashboard skeleton refresh failed (pages may be stale until next analyze): %v\n", err)
	}
	if err := RebuildComparesIndex(filepath.Join(r.outDir, "compares"), r.lang); err != nil {
		fmt.Fprintf(os.Stderr, "compares index rebuild failed (stale until next analyze): %v\n", err)
	}
	// D20/§3.4: the orphan sweep is the default-suite full run's job, and an
	// L2 hit still IS that run (identical inputs, params, and mode digest).
	// Without this, anything that lands in journeys/details/ between two
	// identical runs — a manual copy, a crashed zoom run's leftovers —
	// survives every L2-hit replay. The active set comes from
	// journeys/index.json, which the hit guarantees belongs to this same
	// snapshot. Zoom modes must NOT sweep (their siblings' details belong to
	// the last full run); their L2 digests differ from the default suite's
	// anyway, so this branch only ever fires on a true full-run hit.
	if mode == "default" {
		if idx := journey.LoadJourneyIndex(filepath.Join(r.outDir, "journeys", "index.json")); idx != nil {
			ids := make([]string, len(idx.Journeys))
			for i, row := range idx.Journeys {
				ids[i] = row.ID
			}
			_, _ = journey.CleanOrphanJourneys(filepath.Join(r.outDir, "journeys", "details"), ids)
		}
	}
	if r.lang == i18n.ZH {
		fmt.Fprintln(os.Stderr, "L2/L3 缓存命中，产物已是最新（-no-cache 可强制重算）")
	} else {
		fmt.Fprintln(os.Stderr, "L2/L3 cache hit: outputs are up to date (-no-cache forces rebuild)")
	}
	return true
}

func zoomArtifactMissing(r *analyzeRun, mode string) bool {
	if strings.HasPrefix(mode, "compare:") {
		parts := strings.Split(r.compareArg, ",")
		if len(parts) != 2 {
			return true
		}
		p0, p1 := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		comparesDir := filepath.Join(r.outDir, "compares")
		if strings.ContainsAny(p0+p1, "*?[]") {
			matches, err := filepath.Glob(filepath.Join(comparesDir, "compare-"+p0+"-vs-"+p1+".json"))
			return err != nil || len(matches) == 0
		}
		target := filepath.Join(comparesDir, "compare-"+p0+"-vs-"+p1+".json")
		if _, err := os.Stat(target); err == nil {
			return false
		}
		matches, err := filepath.Glob(filepath.Join(comparesDir, "compare-"+p0+"*-vs-"+p1+"*.json"))
		return err != nil || len(matches) == 0
	}
	if strings.HasPrefix(mode, "journey:") {
		arg := strings.TrimSpace(r.journeyArg)
		detailsDir := filepath.Join(r.outDir, "journeys", "details")
		tokens := strings.Split(arg, ",")
		for _, tok := range tokens {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			patterns := []string{tok + ".json", tok + "*.json"}
			if !strings.HasPrefix(tok, "j-") {
				patterns = append(patterns, "j-"+tok+".json", "j-"+tok+"*.json")
			}
			found := false
			for _, pat := range patterns {
				matches, err := filepath.Glob(filepath.Join(detailsDir, pat))
				if err == nil && len(matches) > 0 {
					found = true
					break
				}
			}
			if !found {
				return true
			}
		}
		return false
	}
	return false
}

func recordPostAnalyzeCache(r *analyzeRun) {
	mode := analyzeModeString(r)
	targetL2, ok := computeTargetL2(r, mode)
	if !ok {
		return
	}
	vmFP, err := report.ComputeVMFingerprintFromManifest(r.outDir)
	if err != nil {
		return
	}
	targetL3 := report.ComputeL3Digest(vmFP[:], report.RendererVersion, r.lang.String())
	rec := &report.CacheRecord{
		L2Digest:        hex.EncodeToString(targetL2[:]),
		VMFingerprint:   hex.EncodeToString(vmFP[:]),
		L3Digest:        hex.EncodeToString(targetL3[:]),
		FormatVersion:   report.ManifestFormat,
		RendererVersion: report.RendererVersion,
	}
	_ = report.SaveCacheRecord(r.outDir, rec)
}

func tryRenderOnlyL3Cache(outDir string, requestedLang string, langPassed bool) bool {
	m, err := report.ValidateManifest(outDir)
	if err != nil {
		return false
	}
	manifestLang, err := i18n.Parse(m.Lang)
	if err != nil {
		manifestLang = i18n.EN
	}
	if langPassed {
		reqLang, err := i18n.Parse(requestedLang)
		if err != nil || reqLang != manifestLang {
			return false
		}
	}
	vmFP, err := report.ComputeVMFingerprintFromManifest(outDir)
	if err != nil {
		return false
	}
	targetL3 := report.ComputeL3Digest(vmFP[:], report.RendererVersion, manifestLang.String())
	if !report.CheckL3Cache(outDir, targetL3, "default") {
		return false
	}
	_ = dashboard.WriteSkeletons(outDir)
	if manifestLang == i18n.ZH {
		fmt.Fprintln(os.Stderr, "L3 缓存命中，产物已是最新（-no-cache 可强制重绘）")
	} else {
		fmt.Fprintln(os.Stderr, "L3 cache hit: outputs are up to date (-no-cache forces rebuild)")
	}
	return true
}

func recordRenderOnlyL3Cache(outDir string) {
	m, err := report.ValidateManifest(outDir)
	if err != nil {
		return
	}
	manifestLang, err := i18n.Parse(m.Lang)
	if err != nil {
		manifestLang = i18n.EN
	}
	vmFP, err := report.ComputeVMFingerprintFromManifest(outDir)
	if err != nil {
		return
	}
	targetL3 := report.ComputeL3Digest(vmFP[:], report.RendererVersion, manifestLang.String())
	rec, _ := report.LoadCacheRecord(outDir)
	if rec == nil {
		rec = &report.CacheRecord{FormatVersion: report.ManifestFormat}
	}
	rec.VMFingerprint = hex.EncodeToString(vmFP[:])
	rec.L3Digest = hex.EncodeToString(targetL3[:])
	rec.RendererVersion = report.RendererVersion
	_ = report.SaveCacheRecord(outDir, rec)
}
