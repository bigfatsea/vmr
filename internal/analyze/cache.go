// Ver 2026-09-22 19:15, by Sonnet 5

// vmr analyze's product-level cache layer: the L2 digest
// computation over (input hashes, pricing fingerprint, format version,
// analysis params), the L2/L3 hit checks, and the cache-record writes for
// both the full-run path and -render-only.
package analyze

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

func analyzeModeString(r *Run) string {
	switch {
	case r.MacroOnly:
		return "macro-only"
	case r.ListOnly:
		return "list-only"
	case r.BenchmarkFlag:
		return "benchmark"
	case r.CompareArg != "":
		return "compare:" + r.CompareArg
	case r.JourneyArg != "":
		return "journey:" + r.JourneyArg
	case r.JourneyOnly:
		return "journey-only"
	default:
		return "default"
	}
}

// ComputeTargetL2 computes the L2 product cache digest for the given run and mode.
func ComputeTargetL2(r *Run, mode string) ([32]byte, bool) {
	return computeTargetL2(r, mode)
}

func computeTargetL2(r *Run, mode string) ([32]byte, bool) {
	inHashes, err := report.ComputeInputHashes(r.Paths)
	if err != nil || len(inHashes) == 0 {
		return [32]byte{}, false
	}
	// When quota JSON is available, its content invalidates L2.
	if r.QuotaJSONPath != "" {
		if _, statErr := os.Stat(r.QuotaJSONPath); statErr == nil {
			if qh, hErr := report.ComputeInputHashes([]string{r.QuotaJSONPath}); hErr == nil && len(qh) == 1 {
				inHashes = append(inHashes, qh[0])
			}
		}
	}
	pricingFP := r.PricingFingerprint
	llmAddr, llmModel := "", ""
	if strings.HasPrefix(mode, "journey:") || strings.HasPrefix(mode, "compare:") {
		llmAddr, llmModel = r.LLMOpts.Addr, r.LLMOpts.Model
	}
	paramsFP := report.ComputeAnalysisParamsFingerprint(report.AnalysisParams{
		Lang:               r.Lang.String(),
		TaskProfile:        r.profile().Name(),
		IncludePartial:     r.IncludePartial,
		IncludeSelfTraffic: r.IncludeSelfTraffic,
		SelfTrafficTags:    r.SelfTrafficTags,
		LLMSelfTag:         r.LLMSelfTag,
		LLMAddr:            llmAddr,
		LLMModel:           llmModel,
		DisplayCCY:         r.DisplayCCY,
		RenderAll:          r.RenderAllFlag,
		Details:            r.DetailsOn,
		Mode:               mode,
	})
	return report.ComputeL2Digest(inHashes, pricingFP, report.ManifestFormat, paramsFP), true
}

func tryL2Cache(r *Run, targetL2 [32]byte, mode string) bool {
	if r.NoCache {
		return false
	}
	rec, hit := report.CheckL2Cache(r.OutDir, targetL2)
	if !hit {
		return false
	}
	if zoomArtifactMissing(r, mode) {
		return false
	}
	vmFP, err := report.ComputeVMFingerprintFromManifest(r.OutDir)
	if err != nil {
		return false
	}
	targetL3 := report.ComputeL3Digest(vmFP[:], report.RendererVersion, r.Lang.String())
	l3Hit := report.CheckL3Cache(r.OutDir, targetL3, mode)
	if !l3Hit {
		if err := renderAllFromDisk(r.OutDir, r.Lang); err != nil {
			return false
		}
		rec.VMFingerprint = hex.EncodeToString(vmFP[:])
		rec.L3Digest = hex.EncodeToString(targetL3[:])
		rec.RendererVersion = report.RendererVersion
		_ = report.SaveCacheRecord(r.OutDir, rec)
	}

	if err := dashboard.WriteSkeletons(r.OutDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: dashboard skeleton refresh failed (pages may be stale until next analyze): %v\n", err)
	}
	if err := RebuildComparesIndex(filepath.Join(r.OutDir, "compares"), r.Lang); err != nil {
		fmt.Fprintf(os.Stderr, "compares index rebuild failed (stale until next analyze): %v\n", err)
	}
	// The orphan sweep is the default-suite full run's job, and an
	// L2 hit still IS that run (identical inputs, params, and mode digest).
	// Without this, anything that lands in journeys/details/ between two
	// identical runs — a manual copy, a crashed zoom run's leftovers —
	// survives every L2-hit replay. The active set comes from
	// journeys/index.json, which the hit guarantees belongs to this same
	// snapshot. Zoom modes must NOT sweep (their siblings' details belong to
	// the last full run); their L2 digests differ from the default suite's
	// anyway, so this branch only ever fires on a true full-run hit.
	if mode == "default" {
		if idx := journey.LoadJourneyIndex(filepath.Join(r.OutDir, "journeys", "index.json")); idx != nil {
			ids := make([]string, len(idx.Journeys))
			for i, row := range idx.Journeys {
				ids[i] = row.ID
			}
			_, _ = journey.CleanOrphanJourneys(filepath.Join(r.OutDir, "journeys", "details"), ids)
		}
	}
	if r.Lang == i18n.ZH {
		fmt.Fprintln(os.Stderr, "L2/L3 缓存命中，产物已是最新（-no-cache 可强制重算）")
	} else {
		fmt.Fprintln(os.Stderr, "L2/L3 cache hit: outputs are up to date (-no-cache forces rebuild)")
	}
	return true
}

func zoomArtifactMissing(r *Run, mode string) bool {
	if strings.HasPrefix(mode, "compare:") {
		parts := strings.Split(r.CompareArg, ",")
		if len(parts) != 2 {
			return true
		}
		p0, p1 := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		comparesDir := filepath.Join(r.OutDir, "compares")
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
		arg := strings.TrimSpace(r.JourneyArg)
		detailsDir := filepath.Join(r.OutDir, "journeys", "details")
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

func recordPostAnalyzeCache(r *Run) {
	mode := analyzeModeString(r)
	targetL2, ok := computeTargetL2(r, mode)
	if !ok {
		return
	}
	vmFP, err := report.ComputeVMFingerprintFromManifest(r.OutDir)
	if err != nil {
		return
	}
	targetL3 := report.ComputeL3Digest(vmFP[:], report.RendererVersion, r.Lang.String())
	rec := &report.CacheRecord{
		L2Digest:        hex.EncodeToString(targetL2[:]),
		VMFingerprint:   hex.EncodeToString(vmFP[:]),
		L3Digest:        hex.EncodeToString(targetL3[:]),
		FormatVersion:   report.ManifestFormat,
		RendererVersion: report.RendererVersion,
	}
	_ = report.SaveCacheRecord(r.OutDir, rec)
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
