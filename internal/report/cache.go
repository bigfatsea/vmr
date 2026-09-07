// Ver 2026-09-15, by pi

// Package report owns product-level L2/L3 caching and fingerprint verification (§7, D1/D8).
package report

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vmr/internal/ctxgraph"
	"vmr/internal/digest"
	"vmr/internal/fmtutil"
	"vmr/internal/pricing"
)

// RendererVersion is the current version of the report presentation renderer (§7.1, §7.4).
// Bumping this invalidates L3 Markdown presentation cache while leaving L2 data cache intact.
// v2: journey .md spine/evidence links retargeted at requests/details|evidence/ (two
// levels up), journeys/index.md + requests/failed.md wording — a renderer-only change,
// so existing snapshots need this bump for -render-only to pick it up.
const RendererVersion = 2

// ComputeInputHashes hashes all input paths using ctxgraph.HashFile sha256 (no mtime fast path, §7.2).
func ComputeInputHashes(paths []string) ([][]byte, error) {
	hashes := make([][]byte, 0, len(paths))
	for _, p := range paths {
		shaHex, err := ctxgraph.HashFile(p)
		if err != nil {
			return nil, err
		}
		raw, err := hex.DecodeString(shaHex)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, raw)
	}
	return hashes, nil
}

// Cache directory and filename under output root (§7.1).
const (
	CacheDirName  = ".cache"
	CacheFileName = "fingerprint.json"
)

// CacheRecord stores the L2 and L3 fingerprints of a committed analysis snapshot (§7.1).
type CacheRecord struct {
	L2Digest        string `json:"l2_digest"`
	VMFingerprint   string `json:"vm_fingerprint,omitempty"`
	L3Digest        string `json:"l3_digest"`
	FormatVersion   int    `json:"format_version"`
	RendererVersion int    `json:"renderer_version"`
}

// AnalysisParams encapsulates every CLI flag or configuration setting
// that can alter sampling, filtering, currency conversion, or output text (§7.2).
type AnalysisParams struct {
	Lang               string
	TaskProfile        string
	IncludePartial     bool
	IncludeSelfTraffic bool
	SelfTrafficTags    []string
	// LLMSelfTag is the exclusion tag derived from the effective llm_key
	// (audit.KeyTag), empty when no key — part of the effective self-traffic
	// exclusion set alongside SelfTrafficTags (§7.2's analysis-params rule:
	// anything that changes a persisted number goes in the fingerprint).
	LLMSelfTag string
	// LLMAddr/LLMModel identify an -llm-addr interpretation run; they only
	// affect persisted products on the modes that consume them (-journey /
	// -compare — the caller leaves them empty elsewhere), where an L2 hit
	// must not silently skip a requested LLM interpretation.
	LLMAddr    string
	LLMModel   string
	DisplayCCY string
	RenderAll  bool
	Details    bool
	Mode       string
	From       string
	To         string
}

// ComputePricingFingerprint computes the deterministic SHA-256 fingerprint
// over configuration that affects financial numbers: provider overrides,
// top-level exchange rates, and standard table generation stamp (D8 / §7.2).
func ComputePricingFingerprint(standardGen string, exchangeRates map[string]float64, policies map[string]pricing.ProviderPolicy) []byte {
	var components [][]byte
	components = append(components, digest.EncodeString(standardGen))

	// Exchange rates: sorted by currency code
	ccys := fmtutil.SortedKeys(exchangeRates)
	components = append(components, digest.EncodeInt64(int64(len(ccys))))
	for _, ccy := range ccys {
		components = append(components, digest.EncodeString(strings.ToUpper(ccy)))
		components = append(components, digest.EncodeFloat64(exchangeRates[ccy]))
	}

	// Provider pricing policies: sorted by provider name
	pNames := fmtutil.SortedKeys(policies)
	components = append(components, digest.EncodeInt64(int64(len(pNames))))
	for _, pName := range pNames {
		policy := policies[pName]
		components = append(components, digest.EncodeString(pName))

		// Aliases
		aliases := fmtutil.SortedKeys(policy.Aliases)
		components = append(components, digest.EncodeInt64(int64(len(aliases))))
		for _, a := range aliases {
			components = append(components, digest.EncodeString(a))
			components = append(components, digest.EncodeString(policy.Aliases[a]))
		}

		// Overrides in declared rule order (first-match-wins)
		components = append(components, digest.EncodeInt64(int64(len(policy.Overrides))))
		for _, ov := range policy.Overrides {
			components = append(components, digest.EncodeString(ov.Model))
			components = append(components, encodeOptFloat64(ov.Discount))
			components = append(components,
				encodeOptFloat64(ov.Explicit.InFresh),
				encodeOptFloat64(ov.Explicit.CacheRead),
				encodeOptFloat64(ov.Explicit.CacheWrite),
				encodeOptFloat64(ov.Explicit.Out),
			)
		}
	}

	d := digest.Digest(components...)
	return d[:]
}

// encodeOptFloat64 encodes an optional *float64 as 1 byte presence flag + 8 bytes value if present.
func encodeOptFloat64(v *float64) []byte {
	if v == nil {
		return []byte{0}
	}
	b := make([]byte, 9)
	b[0] = 1
	binary.BigEndian.PutUint64(b[1:], math.Float64bits(*v))
	return b
}

// ComputeAnalysisParamsFingerprint computes the deterministic SHA-256 fingerprint
// over analysis sampling and scope parameters (§7.2).
func ComputeAnalysisParamsFingerprint(p AnalysisParams) []byte {
	var components [][]byte
	components = append(components,
		digest.EncodeString(p.Lang),
		digest.EncodeString(p.TaskProfile),
		digest.EncodeBool(p.IncludePartial),
		digest.EncodeBool(p.IncludeSelfTraffic),
	)

	// Sorted self-traffic tags
	sortedTags := make([]string, len(p.SelfTrafficTags))
	copy(sortedTags, p.SelfTrafficTags)
	sort.Strings(sortedTags)
	components = append(components, digest.EncodeInt64(int64(len(sortedTags))))
	for _, tag := range sortedTags {
		components = append(components, digest.EncodeString(tag))
	}

	components = append(components,
		digest.EncodeString(p.DisplayCCY),
		digest.EncodeString(p.LLMSelfTag),
		digest.EncodeString(p.LLMAddr),
		digest.EncodeString(p.LLMModel),
		digest.EncodeBool(p.RenderAll),
		digest.EncodeBool(p.Details),
		digest.EncodeString(p.Mode),
		digest.EncodeString(p.From),
		digest.EncodeString(p.To),
	)

	d := digest.Digest(components...)
	return d[:]
}

// ComputeL2Digest computes the product-level L2 data cache digest (§7.1, §7.2):
// digest.Digest(输入文件哈希按序…, 配置指纹, 格式版本, 分析参数).
func ComputeL2Digest(inputHashes [][]byte, pricingFP []byte, formatVersion int, paramsFP []byte) [32]byte {
	var components [][]byte
	components = append(components, digest.EncodeInt64(int64(len(inputHashes))))
	for _, h := range inputHashes {
		components = append(components, h)
	}
	components = append(components, pricingFP)
	components = append(components, digest.EncodeInt64(int64(formatVersion)))
	components = append(components, paramsFP)
	return digest.Digest(components...)
}

// ComputeVMFingerprint computes the deterministic SHA-256 fingerprint over
// on-disk JSON slices that feed ViewModels.
func ComputeVMFingerprint(sliceHashes [][]byte) [32]byte {
	var components [][]byte
	components = append(components, digest.EncodeInt64(int64(len(sliceHashes))))
	for _, h := range sliceHashes {
		components = append(components, h)
	}
	return digest.Digest(components...)
}

// ComputeVMFingerprintFromManifest constructs the VM data fingerprint by hashing
// all recognized slices recorded in manifest.json in deterministic order.
func ComputeVMFingerprintFromManifest(outDir string) ([32]byte, error) {
	m, err := ValidateManifest(outDir)
	if err != nil {
		return [32]byte{}, fmt.Errorf("validate manifest: %w", err)
	}

	var sliceHashes [][]byte
	for _, rel := range AllSlicePaths {
		if ref, ok := m.Slices[rel]; ok && ref.SHA256 != "" {
			raw, err := hex.DecodeString(ref.SHA256)
			if err == nil && len(raw) == 32 {
				sliceHashes = append(sliceHashes, raw)
			}
		}
	}

	return ComputeVMFingerprint(sliceHashes), nil
}

// ComputeL3Digest computes the presentation-layer L3 cache digest (§7.1, §7.2):
// digest.Digest(ViewModel 指纹, 渲染器版本, 语言).
func ComputeL3Digest(vmFP []byte, rendererVersion int, lang string) [32]byte {
	return digest.Digest(vmFP, digest.EncodeInt64(int64(rendererVersion)), digest.EncodeString(lang))
}

// CachePath returns the full path to the cache fingerprint record.
func CachePath(outDir string) string {
	return filepath.Join(outDir, CacheDirName, CacheFileName)
}

// LoadCacheRecord reads and unmarshals the cache fingerprint from outDir.
func LoadCacheRecord(outDir string) (*CacheRecord, error) {
	path := CachePath(outDir)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rec CacheRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// SaveCacheRecord persists the cache fingerprint atomically to outDir/.cache/fingerprint.json (0600).
func SaveCacheRecord(outDir string, rec *CacheRecord) error {
	cacheDir := filepath.Join(outDir, CacheDirName)
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(cacheDir, "fingerprint-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, CachePath(outDir))
}

// CheckL2Cache checks whether outDir contains a valid, matching L2 cache snapshot.
// It verifies:
// 1. .cache/fingerprint.json exists and L2Digest matches targetL2.
// 2. FormatVersion matches ManifestFormat.
// 3. manifest.json is intact and all slice hashes match on-disk bytes.
func CheckL2Cache(outDir string, targetL2 [32]byte) (*CacheRecord, bool) {
	rec, err := LoadCacheRecord(outDir)
	if err != nil || rec == nil {
		return nil, false
	}
	targetHex := hex.EncodeToString(targetL2[:])
	if rec.L2Digest != targetHex {
		return nil, false
	}
	if rec.FormatVersion != ManifestFormat {
		return nil, false
	}
	m, err := ValidateManifest(outDir)
	if err != nil || m == nil {
		return nil, false
	}
	return rec, true
}

// CheckL3Cache checks whether outDir contains a valid, matching L3 presentation cache.
// It verifies:
// 1. .cache/fingerprint.json exists and L3Digest matches targetL3.
// 2. RendererVersion matches the current binary's RendererVersion.
// 3. Expected markdown files are present on disk.
func CheckL3Cache(outDir string, targetL3 [32]byte, mode string) bool {
	rec, err := LoadCacheRecord(outDir)
	if err != nil || rec == nil {
		return false
	}
	targetHex := hex.EncodeToString(targetL3[:])
	if rec.L3Digest != targetHex {
		return false
	}
	if rec.RendererVersion != RendererVersion {
		return false
	}
	return requiredMarkdownFilesExist(outDir, mode)
}

// requiredMarkdownFilesExist checks that markdown files corresponding to existing
// JSON slices actually exist on disk so an L3 hit doesn't leave missing files.
func requiredMarkdownFilesExist(outDir string, mode string) bool {
	if _, err := os.Stat(filepath.Join(outDir, SliceMacroSummary)); err == nil {
		if _, err := os.Stat(filepath.Join(outDir, "vmr-report.md")); err != nil {
			return false
		}
	}
	requestsDir := filepath.Join(outDir, "requests")
	if _, err := os.Stat(filepath.Join(requestsDir, "failed.jsonl")); err == nil {
		if _, err := os.Stat(filepath.Join(requestsDir, "failed.md")); err != nil {
			return false
		}
	}
	journeysDir := filepath.Join(outDir, "journeys")
	if _, err := os.Stat(filepath.Join(journeysDir, "index.json")); err == nil {
		if _, err := os.Stat(filepath.Join(journeysDir, "index.md")); err != nil {
			return false
		}
	}
	if _, err := os.Stat(filepath.Join(journeysDir, "benchmarks.json")); err == nil {
		if _, err := os.Stat(filepath.Join(journeysDir, "benchmarks.md")); err != nil {
			return false
		}
	}
	return true
}
