// Ver 2026-09-21 22:00, by Sonnet 5
//
// Manifest modeling and atomic writing sequence (§3.4, §8.2, D20).
// Manifest is the sole authoritative token for snapshot admission.
// Readers treat manifest.json as the admission gate: if manifest is missing
// or any slice SHA-256 fingerprint does not match, the snapshot is rejected.
// manifest.json must be written last via CreateTemp + Rename.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// ManifestFormat is the current version of the report snapshot manifest
// (§3.4, D2). 11 -> 12: R1 changed three field shapes on the data product
// — macro/summary.json's efficiency[] gained Params (additive, wouldn't
// alone need a bump) but highlights[] changed from []string to
// []Highlight{code,text,params} (breaking), and manifest.json's
// footnotes/disclaimers changed from bare localized strings to
// {code,params} (breaking) — see KNOWN_ISSUES' "数据产品是对外契约" entry
// for the compatibility policy this follows.
const ManifestFormat = 12

const (
	SliceMacroSummary           = "macro/summary.json"
	SliceMacroFinance           = "macro/finance.json"
	SliceMacroReliability       = "macro/reliability.json"
	SliceMacroWorkloads         = "macro/workloads.json"
	SliceMacroContextEfficiency = "macro/context-efficiency.json"
	SliceRequestsIndex          = "requests/index.json"
	SliceJourneysIndex          = "journeys/index.json"
	SliceJourneysBenchmarks     = "journeys/benchmarks.json"
	// SliceMacroGuard is Agent Guard's optional M2 slice (slices.go's
	// GuardSlice) — deliberately NOT in MacroSlicePaths: it is not one of
	// the five core domain slices written as an atomic unit, and a run
	// with Agent Guard unconfigured (every run today) never writes it at
	// all, unlike the five which are always written together.
	SliceMacroGuard = "macro/guard.json"
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
	SliceMacroGuard,
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
	Format      int       `json:"format"`
	GeneratedAt TimePoint `json:"generated_at"`
	TimeRange   [2]string `json:"time_range"`
	// Lang is the language of the LAST MARKDOWN RENDER this snapshot's
	// manifest was stamped after — not "this product's language". The five
	// macro/*.json slices, requests/index.json, and this manifest's own
	// Footnotes/Disclaimers are language-invariant (R1); only vmr-report.md
	// and the other rendered .md files vary by language. Re-rendering in a
	// different language (-render-only -lang) updates this field without
	// touching any JSON slice.
	Lang        string                 `json:"lang"`
	Timezone    string                 `json:"timezone"`
	Inputs      []InputFile            `json:"inputs"`
	Slices      map[string]SliceRef    `json:"slices"`
	Footnotes   map[string]FootnoteRef `json:"footnotes,omitempty"`
	Disclaimers []DisclaimerRef        `json:"disclaimers,omitempty"`
}

// FootnoteRef is one manifest.json footnote definition: a stable code
// instead of pre-localized text, plus any parameters needed to reconstruct
// it (R1 — same Code+Params split as Finding/Highlight). Every current
// footnote is static (no params), but the shape stays uniform with
// DisclaimerRef's rather than special-casing the no-params case.
type FootnoteRef struct {
	Code   string            `json:"code"`
	Params map[string]string `json:"params,omitempty"`
}

// DisclaimerRef is one manifest.json disclaimer: same Code+Params split.
type DisclaimerRef struct {
	Code   string            `json:"code"`
	Params map[string]string `json:"params,omitempty"`
}

// BuildFootnotesAndDisclaimers extracts structured footnote definitions and
// disclaimers for downstream consumers (§3.3). Language-neutral (R1): no
// lang parameter, because a footnote's glyph key and a disclaimer's code
// are exactly the "reproduce it in either language without re-aggregating"
// contract R1 asks for — nothing here should ever need to vary by lang.
func BuildFootnotesAndDisclaimers(rep *Report2) (map[string]FootnoteRef, []DisclaimerRef) {
	footnotes := map[string]FootnoteRef{
		"¹":       {Code: "low_confidence_ratio"},
		"⚠️low-n": {Code: "low_sample_size"},
		"²":       {Code: "quota_live_counter"},
		"⭐":       {Code: "quota_exceeded"},
		"†":       {Code: "quota_window_no_overlap"},
		"‡":       {Code: "quota_metric_changed"},
	}

	var disclaimers []DisclaimerRef
	if rep != nil {
		if rep.Pricing != nil {
			asOf := rep.Pricing.StandardGeneratedAt
			if asOf == "" {
				asOf = "(unknown date)"
			}
			cur := rep.Pricing.Currency
			if cur == "" {
				cur = "USD"
			}
			params := map[string]string{"as_of": asOf, "currency": cur}
			if rep.Pricing.RequestedCurrency != "" && rep.Pricing.RequestedCurrency != rep.Pricing.Currency {
				params["requested_currency"] = rep.Pricing.RequestedCurrency
			}
			disclaimers = append(disclaimers, DisclaimerRef{Code: "pricing_estimate", Params: params})
			disclaimers = append(disclaimers, DisclaimerRef{Code: "cost_scope"})
		}
		if len(rep.Compactions) > 0 {
			disclaimers = append(disclaimers, DisclaimerRef{Code: "compaction_retention"})
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
	footnotes, disclaimers := BuildFootnotesAndDisclaimers(rep)
	if rep != nil {
		timeRange = [2]string{rep.Meta.From, rep.Meta.To}
		for _, in := range rep.Meta.Inputs {
			sha, _ := ctxgraph.HashFile(in)
			inputs = append(inputs, InputFile{
				Path:   in,
				SHA256: sha,
			})
		}
	} else if existing, err := ReadManifest(dir); err == nil && existing != nil {
		// When rep == nil (zoom / benchmark / compare runs updating an existing snapshot),
		// preserve the provenance metadata (timeRange, inputs, footnotes, disclaimers)
		// from the existing manifest if present.
		timeRange = existing.TimeRange
		inputs = existing.Inputs
		if len(existing.Footnotes) > 0 {
			footnotes = existing.Footnotes
		}
		if len(existing.Disclaimers) > 0 {
			disclaimers = existing.Disclaimers
		}
	}

	slices := make(map[string]SliceRef)
	for _, relPath := range AllSlicePaths {
		fullPath := filepath.Join(dir, relPath)
		if fi, err := os.Stat(fullPath); err == nil && !fi.IsDir() {
			sha, err := ctxgraph.HashFile(fullPath)
			if err != nil {
				return nil, fmt.Errorf("hash slice %s: %w", relPath, err)
			}
			slices[relPath] = SliceRef{
				Path:   relPath,
				SHA256: sha,
			}
		}
	}

	// rep != nil means the macro report half ran and WriteMacroSlices
	// succeeded this run, so all five macro slices must be on disk now. A
	// missing one at stamp time means something removed or truncated it
	// between the write and here — refuse to stamp a partial snapshot as
	// valid rather than silently omitting the slice from the manifest (N8).
	if rep != nil {
		if err := requireMacroSlices(slices); err != nil {
			return nil, err
		}
	}

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

// ReadManifest loads dir/manifest.json without validating each slice's sha256.
func ReadManifest(dir string) (*Manifest, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
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

	if len(m.Slices) == 0 {
		return nil, fmt.Errorf("manifest records no slices — not a complete analyze snapshot")
	}

	// A manifest that lists any macro slice must list all five: the macro
	// set is written as a unit (WriteMacroSlices is all-or-error), so a
	// partial set on record is a corrupt manifest, not a valid macro-free
	// snapshot (N8). Zoom/journey-only snapshots legitimately record none.
	if macroSlicePresent(m.Slices) {
		if err := requireMacroSlices(m.Slices); err != nil {
			return nil, err
		}
	}

	for key, sliceRef := range m.Slices {
		p := sliceRef.Path
		if p == "" {
			p = key
		}
		fullPath := filepath.Join(dir, p)
		actualSHA, err := ctxgraph.HashFile(fullPath)
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

// macroSlicePresent reports whether slices records at least one of the five
// macro domain slices.
func macroSlicePresent(slices map[string]SliceRef) bool {
	for _, p := range MacroSlicePaths {
		if _, ok := slices[p]; ok {
			return true
		}
	}
	return false
}

// requireMacroSlices returns an error naming the first macro slice absent
// from slices — the five are a unit (§8.2), so a partial set is invalid.
func requireMacroSlices(slices map[string]SliceRef) error {
	for _, p := range MacroSlicePaths {
		if _, ok := slices[p]; !ok {
			return fmt.Errorf("macro slice %s missing from the snapshot — the five macro slices are written as a unit", p)
		}
	}
	return nil
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
