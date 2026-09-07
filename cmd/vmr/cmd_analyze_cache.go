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
	story "vmr/internal/journey"
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

func computeTargetL2(r *analyzeRun, mode string) ([32]byte, bool) {
	inHashes, err := report.ComputeInputHashes(r.paths)
	if err != nil || len(inHashes) == 0 {
		return [32]byte{}, false
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
	if err := RebuildComparesIndex(filepath.Join(r.outDir, "compares")); err != nil {
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
		if idx := story.LoadStoryIndex(filepath.Join(r.outDir, "journeys", "index.json")); idx != nil {
			ids := make([]string, len(idx.Journeys))
			for i, row := range idx.Journeys {
				ids[i] = row.ID
			}
			_, _ = story.CleanOrphanJourneys(filepath.Join(r.outDir, "journeys", "details"), ids)
		}
	}
	return true
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
