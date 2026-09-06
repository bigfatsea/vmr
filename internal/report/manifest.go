// Ver 2026-09-06, by Claude
//
// Manifest modeling and atomic writing sequence (§3.4, §8.2, D20).
// Manifest is the sole authoritative token for snapshot admission.
// Readers treat manifest.json as the admission gate: if manifest is missing
// or any slice SHA-256 fingerprint does not match, the snapshot is rejected.
// manifest.json must be written last via CreateTemp + Rename.
package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// ManifestFormat is the current version of the report snapshot manifest (§3.4, D2).
const ManifestFormat = 11

const (
	SliceMacroSummary           = "macro/summary.json"
	SliceMacroFinance           = "macro/finance.json"
	SliceMacroReliability       = "macro/reliability.json"
	SliceMacroWorkloads         = "macro/workloads.json"
	SliceMacroContextEfficiency = "macro/context-efficiency.json"
	SliceRequestsIndex          = "requests/index.json"
	SliceJourneysIndex          = "journeys/index.json"
	SliceJourneysBenchmarks     = "journeys/benchmarks.json"
)

// MacroSlicePaths lists the 5 core macro domain slices.
var MacroSlicePaths = []string{
	SliceMacroSummary,
	SliceMacroFinance,
	SliceMacroReliability,
	SliceMacroWorkloads,
	SliceMacroContextEfficiency,
}

// AllSlicePaths lists all recognized slices covered by snapshot manifests.
var AllSlicePaths = []string{
	SliceMacroSummary,
	SliceMacroFinance,
	SliceMacroReliability,
	SliceMacroWorkloads,
	SliceMacroContextEfficiency,
	SliceRequestsIndex,
	SliceJourneysIndex,
	SliceJourneysBenchmarks,
}

// TimePoint represents a timestamp as both raw epoch milliseconds (for
// sorting/filtering) and a DisplayZone-formatted string (for display) (§3.3, §5.6).
type TimePoint struct {
	TS        int64  `json:"ts"`
	TSDisplay string `json:"ts_display"`
}

// NewTimePoint creates a TimePoint from a time.Time using fmtutil.DisplayZone.
func NewTimePoint(t time.Time) TimePoint {
	return TimePoint{
		TS:        t.UnixMilli(),
		TSDisplay: t.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05"),
	}
}

// ParseTimePoint parses an RFC3339 timestamp into a TimePoint.
func ParseTimePoint(s string) TimePoint {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return TimePoint{TSDisplay: s}
	}
	return NewTimePoint(t)
}

// Time converts the epoch millisecond timestamp back to a time.Time.
func (tp TimePoint) Time() time.Time {
	return time.UnixMilli(tp.TS)
}

// InputFile pairs an input audit log path with its SHA-256 digest (§3.4).
type InputFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// SliceRef identifies an output slice by relative path and content SHA-256 digest (§3.4).
type SliceRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Manifest is the authoritative admission token and consistency record
// for a report snapshot (§3.4, §8.2).
type Manifest struct {
	Format      int                 `json:"format"`
	GeneratedAt TimePoint           `json:"generated_at"`
	TimeRange   [2]string           `json:"time_range"`
	Lang        string              `json:"lang"`
	Timezone    string              `json:"timezone"`
	Inputs      []InputFile         `json:"inputs"`
	Slices      map[string]SliceRef `json:"slices"`
	Footnotes   map[string]string   `json:"footnotes,omitempty"`
	Disclaimers []string            `json:"disclaimers,omitempty"`
}

// HasSlice checks if the manifest contains a reference for the given slice path.
func (m *Manifest) HasSlice(relPath string) bool {
	if m == nil || m.Slices == nil {
		return false
	}
	_, ok := m.Slices[relPath]
	return ok
}

// GetSlice retrieves the SliceRef for a relative path or semantic key.
func (m *Manifest) GetSlice(relPath string) (SliceRef, bool) {
	if m == nil || m.Slices == nil {
		return SliceRef{}, false
	}
	if ref, ok := m.Slices[relPath]; ok {
		return ref, true
	}
	for _, ref := range m.Slices {
		if ref.Path == relPath {
			return ref, true
		}
	}
	return SliceRef{}, false
}

// BuildFootnotesAndDisclaimers extracts structured footnote definitions and
// disclaimers for downstream consumers (§3.3).
func BuildFootnotesAndDisclaimers(rep *Report2, lang i18n.Lang) (map[string]string, []string) {
	footnotes := make(map[string]string)
	var disclaimers []string

	if lang == i18n.ZH {
		footnotes["¹"] = "cache_efficiency 等比值指标的分母 / 总请求数 < 90% 时标注低置信度"
		footnotes["⚠️low-n"] = "样本量 n < 20：分位数与比率受小样本扰动影响较大"
		footnotes["²"] = "来自 <log_dir>/vmr-quota.json 的实时计数器，是路由半区的权威记账"
		footnotes["⭐"] = "已用% ≥ 100% 时的标记：该账户本周期已超出配置的额度上限"
		footnotes["†"] = "本报表窗口消耗与右侧的周期区间没有任何时间交集"
		footnotes["‡"] = "该账户的 quota: metric/every 曾被改过——盘上还留着旧配置写下的计数器"
	} else {
		footnotes["¹"] = "Low confidence: ratio metrics like cache_efficiency have denominator / total requests < 90%"
		footnotes["⚠️low-n"] = "Sample size n < 20: percentiles and rates are subject to small-sample variance"
		footnotes["²"] = "Live counter from <log_dir>/vmr-quota.json — authoritative routing ledger"
		footnotes["⭐"] = "Marks Used% >= 100%: account exceeded configured quota for current period"
		footnotes["†"] = "Window Consumed shares NO time at all with the period range"
		footnotes["‡"] = "Quota metric/every was modified — on-disk counter reflects prior configuration"
	}

	if rep != nil {
		if rep.Pricing != nil {
			if d := rep.Pricing.Disclaimer(lang); d != "" {
				disclaimers = append(disclaimers, d)
			}
			costTx := i18n.Cost(lang)
			if costTx.ScopeFootnote != "" {
				s := strings.TrimSpace(strings.TrimPrefix(costTx.ScopeFootnote, ">"))
				disclaimers = append(disclaimers, s)
			}
		}
		if len(rep.Compactions) > 0 {
			compTx := i18n.Compaction(lang)
			if compTx.Footnote != "" {
				s := strings.TrimSpace(strings.TrimPrefix(compTx.Footnote, ">"))
				disclaimers = append(disclaimers, s)
			}
		}
	}
	return footnotes, disclaimers
}

// BuildManifest constructs a Manifest for the snapshot in dir, computing
// sha256 digests for all generated slices and inputs (§3.4, §8.2).
func BuildManifest(dir string, rep *Report2, lang i18n.Lang) (*Manifest, error) {
	now := time.Now()
	var timeRange [2]string
	var inputs []InputFile
	if rep != nil {
		timeRange = [2]string{rep.Meta.From, rep.Meta.To}
		for _, in := range rep.Meta.Inputs {
			sha, _ := HashFile(in)
			inputs = append(inputs, InputFile{
				Path:   in,
				SHA256: sha,
			})
		}
	}

	slices := make(map[string]SliceRef)
	for _, relPath := range AllSlicePaths {
		fullPath := filepath.Join(dir, relPath)
		if fi, err := os.Stat(fullPath); err == nil && !fi.IsDir() {
			sha, err := HashFile(fullPath)
			if err != nil {
				return nil, fmt.Errorf("hash slice %s: %w", relPath, err)
			}
			slices[relPath] = SliceRef{
				Path:   relPath,
				SHA256: sha,
			}
		}
	}

	footnotes, disclaimers := BuildFootnotesAndDisclaimers(rep, lang)

	m := &Manifest{
		Format:      ManifestFormat,
		GeneratedAt: NewTimePoint(now),
		TimeRange:   timeRange,
		Lang:        lang.String(),
		Timezone:    fmtutil.DisplayZone.String(),
		Inputs:      inputs,
		Slices:      slices,
		Footnotes:   footnotes,
		Disclaimers: disclaimers,
	}
	return m, nil
}

// WriteManifest writes m to <dir>/manifest.json atomically via CreateTemp + Rename (0600).
func WriteManifest(dir string, m *Manifest) error {
	if m == nil {
		return fmt.Errorf("cannot write nil manifest")
	}
	return writeJSONAtomic(dir, "manifest.json", m)
}

// ValidateManifest verifies that manifest.json exists in dir, has the expected Format,
// and that every slice recorded in manifest.json exists and its sha256 matches (§3.4).
func ValidateManifest(dir string) (*Manifest, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}

	if m.Format != ManifestFormat {
		return nil, fmt.Errorf("manifest format mismatch: got %d, want %d", m.Format, ManifestFormat)
	}

	for key, sliceRef := range m.Slices {
		p := sliceRef.Path
		if p == "" {
			p = key
		}
		fullPath := filepath.Join(dir, p)
		actualSHA, err := HashFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("hash slice %q (%s): %w", key, p, err)
		}
		if actualSHA != sliceRef.SHA256 {
			return nil, fmt.Errorf("slice %q (%s) sha256 mismatch: recorded %s, actual %s",
				key, p, sliceRef.SHA256, actualSHA)
		}
	}
	return &m, nil
}

// HashBytes computes the lowercase hex-encoded SHA-256 digest of b.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// HashFile computes the lowercase hex-encoded SHA-256 digest of the file at path.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeJSONAtomic writes data as formatted JSON to filepath.Join(dir, filename)
// atomically using CreateTemp + Rename with 0600 permissions.
func writeJSONAtomic(dir string, filename string, data any) error {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filename, err)
	}
	raw = append(raw, '\n')

	tmpFile, err := os.CreateTemp(dir, filename+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", filename, err)
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp for %s: %w", filename, err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("chmod temp for %s: %w", filename, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp for %s: %w", filename, err)
	}
	target := filepath.Join(dir, filename)
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("rename %s: %w", filename, err)
	}
	return nil
}
