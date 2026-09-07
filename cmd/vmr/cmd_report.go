// Ver 2026-07-29 14:00, by Sonnet 5
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/pricing"
	"vmr/internal/report"
)

// timestampWriter prefixes every line written through it with
// "2006-01-02 15:04:05.000 " (fmtutil.DisplayZone, millisecond precision) — `vmr
// report`'s progress output otherwise has no way to show how long each
// phase/file actually took. One Write() call is assumed to be one
// already-formatted line (true for every fmt.Fprintf call site this wraps),
// so the timestamp lands at the true start of that line, not buffered
// alongside unrelated output.
type timestampWriter struct{ w io.Writer }

func (tw timestampWriter) Write(p []byte) (int, error) {
	if _, err := io.WriteString(tw.w, time.Now().In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05.000")+" "); err != nil {
		return 0, err
	}
	if _, err := tw.w.Write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// buildPricing resolves standard pricing tables and optional provider/global overrides from config.
// Degrades gracefully to embedded standard pricing if config is missing or invalid.
func buildPricing(cfg *config.Config, loadErr error, configPath string, tw io.Writer, displayCCY string, extraRates map[string]float64) (*pricing.Resolver, *report.Pricing) {
	standard, err := pricing.LoadStandard()
	if err != nil {
		fmt.Fprintf(tw, "pricing: embedded standard table failed to load (%v) — no $ estimates\n", err)
		return nil, nil
	}
	// Resolution is always USD now (see internal/pricing.Table's doc
	// comment) — Currency starts at USD unconditionally and only changes
	// below, if -currency/report.yaml requests a different display currency
	// and a conversion rate is available.
	summary := &report.Pricing{Currency: "USD", StandardGeneratedAt: standard.GeneratedAt}

	var resolver *pricing.Resolver
	var configRates map[string]float64
	if loadErr != nil {
		// runReport already printed one unified warning for cfgErr — a
		// second, near-identical one here would just repeat it.
		resolver = pricing.NewResolver(standard, nil)
	} else {
		table := standard
		if t, err := cfg.PricingTable(); err == nil && t != nil {
			table = t
		}
		perProvider := map[string]pricing.ProviderPolicy{}
		overrideCount := 0
		for name, policy := range cfg.ProviderPricingPolicies {
			perProvider[name] = policy
			overrideCount += len(policy.Overrides)
		}
		configRates = cfg.ExchangeRate
		summary.ProviderOverrides = overrideCount
		if overrideCount > 0 {
			fmt.Fprintf(tw, "pricing: %d provider rate rule(s) loaded from %s\n", overrideCount, configPath)
		}
		resolver = pricing.NewResolver(table, perProvider)
	}

	if displayCCY != "" && !strings.EqualFold(displayCCY, summary.Currency) {
		rates, effErr := pricing.EffectiveExchangeRate(configRates)
		if effErr != nil {
			rates = map[string]float64{}
		}
		for k, v := range extraRates { // report.yaml's own rates win over config.yaml's on a matching key
			rates[k] = v
		}
		if factor, ok := pricing.FactorBetween(summary.Currency, displayCCY, rates); ok {
			resolver = resolver.WithDisplayFactor(factor)
			summary.Currency = displayCCY
		} else {
			summary.RequestedCurrency = displayCCY
			fmt.Fprintf(tw, "pricing: no exchange rate to convert %s -> %s for -currency, showing %s instead (add exchange_rate: {%s: <rate>} to config.yaml's top level or report.yaml)\n", summary.Currency, displayCCY, summary.Currency, displayCCY)
		}
	}
	return resolver, summary
}

// resolvePricingForAnalyze builds the pricing resolver the journey half needs
// for zoom views (-journey / -compare), which don't run runReport. cfg/cfgErr
// come from dispatchAnalyze's single config.Load (P-7-7) — shared with
// runReport so both halves price against one config view. Same
// degrade-gracefully contract: an unreadable config falls back to the embedded
// standard table. Warnings go to stderr rather than being discarded, so a
// missing display-currency exchange rate is visible.
func resolvePricingForAnalyze(cfg *config.Config, cfgErr error, configPath, displayCCY string, exchangeRate map[string]float64) (*pricing.Resolver, string) {
	tw := timestampWriter{w: os.Stderr}
	if cfgErr != nil {
		fmt.Fprintf(tw, "config: %s not usable (%v) — $ estimates use the standard price table only (no account overrides)\n", configPath, cfgErr)
	}
	resolver, info := buildPricing(cfg, cfgErr, configPath, tw, displayCCY, exchangeRate)
	ccy := "USD"
	if info != nil && info.Currency != "" {
		ccy = info.Currency
	}
	return resolver, ccy
}

// resolvePricingFingerprint resolves standard pricing and override policies to produce the configuration fingerprint (D8 / §7.2).
func resolvePricingFingerprint(cfg *config.Config, extraRates map[string]float64) []byte {
	standardGen := ""
	if standard, err := pricing.LoadStandard(); err == nil && standard != nil {
		standardGen = standard.GeneratedAt
	}
	rates := map[string]float64{}
	var policies map[string]pricing.ProviderPolicy
	if cfg != nil {
		if t, err := cfg.PricingTable(); err == nil && t != nil && t.GeneratedAt != "" {
			standardGen = t.GeneratedAt
		}
		if cfg.ExchangeRate != nil {
			for k, v := range cfg.ExchangeRate {
				rates[k] = v
			}
		}
		policies = cfg.ProviderPricingPolicies
	}
	for k, v := range extraRates {
		rates[k] = v
	}
	return report.ComputePricingFingerprint(standardGen, rates, policies)
}

// allPathsOutsideDir reports whether EVERY entry in paths resolves
// outside dir — used to flag "the live quota counter's log_dir doesn't
// contain a single one of the audit logs this report is analyzing", the
// cheap same-machine signal that the two might be different instances.
// Deliberately ALL-outside, not ANY-outside: a mixed run (some paths under
// log_dir, some not — e.g. one archived file alongside the live directory)
// is the normal case for analyzing "this instance's logs plus one old
// archive", not a mismatch worth flagging. An empty/unresolvable dir (a
// config.yaml that couldn't set LogDir at all) can't be compared against,
// so returns false rather than a false-positive warning.
func allPathsOutsideDir(paths []string, dir string) bool {
	if dir == "" || len(paths) == 0 {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for _, p := range paths {
		absP, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absDir, absP)
		if err != nil {
			continue
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false // at least one path IS under dir
		}
	}
	return true
}

// runReport aggregates audit JSONL into internal/report's output:
// the five macro/*.json slices + manifest.json (D2 — there is no
// monolithic aggregate JSON), vmr-report.md, requests/index.json,
// requests/failed.jsonl/.md (error-analysis index: outcome == error|canceled
// plus ok-but-truncated), and one requests/details/*.md per request.
// Inputs may freely mix live plain .jsonl files and .jsonl.zst files that
// the audit logger's housekeeping sweep has since compressed
// (internal/report decompresses transparently) — e.g.
// `vmr analyze 'vmr-audit-*.jsonl*'`. With no input files given at all,
// defaults to <-c config.yaml's log_dir>/vmr-audit-* (see resolveInputPaths
// in auditpaths.go) — the common case of "just report on this instance's
// own logs" needs no arguments beyond an optional -c.
// setupDetailWriter creates {outDir}/requests/details and starts the
// detail-page worker pool when detailsOn — Build's onRecord hook (nil when
// !detailsOn) renders+writes each record's detail page during the
// aggregation pass itself, on its own worker pool, so there's no separate
// third read of the audit source for detail export. Build's own
// success/failure never depends on this: a detail-write failure surfaces
// only when the returned *report.DetailWriter's Close is checked, well
// after the macro slices are already safely on disk.
func setupDetailWriter(outDir string, detailsOn bool, lang i18n.Lang, tw io.Writer) (dw *report.DetailWriter, detailDir string, onRecord func(*audit.Record, *report.ReqInfo), err error) {
	detailDir = filepath.Join(outDir, "requests", "details")
	if !detailsOn {
		return nil, detailDir, nil, nil
	}
	dw, err = report.NewDetailWriter(detailDir, lang, resolveTaskProfile())
	if err != nil {
		return nil, detailDir, nil, err
	}
	fmt.Fprintf(tw, "detail export: writing into %s (runs concurrently with the pass below)\n", detailDir)
	return dw, detailDir, dw.Submit, nil
}

// detailDirHasFiles reports whether dir already contains at least one
// entry. A missing directory is "no", not an error.
func detailDirHasFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// detailsPresentFor is the criterion for
// Meta.DetailsEnabled: detailsOn alone covers this run's OWN -details
// writer (guaranteed to finish by the time this command returns, even
// though it hasn't flushed to disk yet at the point runReport reads this);
// the OR checks for detail pages a DIFFERENT half of the same `vmr
// analyze` invocation already wrote and flushed before this command ever
// started — journey's batch materialization under -render-all (P13.1). A
// flag-only check goes stale the moment two halves of one invocation can
// populate details/ independently.
func detailsPresentFor(detailsOn bool, detailDir string) bool {
	return detailsOn || detailDirHasFiles(detailDir)
}

// reportRunOpts bundles the report half's already-resolved parameters —
// every value derived from analyze's flags/report.yaml before the pipeline
// in runReport starts doing anything. Factored out (P9.1) so
// cmdAnalyze drives the exact same pipeline from its own unified flag
// set's resolution.
type reportRunOpts struct {
	configPath string
	// cfg/cfgErr: config.Load already done once by the caller (cmdAnalyze),
	// shared with the journey half's pricing resolver — runReport must not
	// load a second time (P-7-7). cfgErr is non-fatal here; see below.
	cfg               *config.Config
	cfgErr            error
	outDir            string
	detailsOn         bool
	lang              i18n.Lang
	displayCCY        string
	exchangeRate      map[string]float64
	excludeClientTags map[string]bool
	reportConfigPath  string
}

// runReport executes the report half's full pipeline — session analysis,
// aggregation, pricing/quota resolution, and every derived file it writes
// (vmr-report.{json,md}, macro/*.json, requests/*) — against
// already-resolved opts.
func runReport(paths []string, tw timestampWriter, opts reportRunOpts) (*report.Report2, error) {
	// cfg/cfgErr come pre-loaded from cmdAnalyze — one config.Load per
	// analyze run, shared with the journey half (P-7-7). buildPricing and
	// buildProviderQuotas below both consume it; a load failure is NOT
	// fatal to the report half — both callees degrade independently (pricing
	// falls back to the embedded standard table; the quota section simply
	// doesn't render) — see cfgErr's threading below, never returned as
	// this function's own error.
	cfg, cfgErr := opts.cfg, opts.cfgErr
	if cfgErr != nil {
		// One unified warning for both degrade paths — buildPricing
		// and buildProviderQuotas used to each print their own near-
		// duplicate of this, so a bare-logs report run reliably saw
		// two warnings naming the same unreadable file.
		fmt.Fprintf(tw, "config: %s not usable (%v) — $ estimates use the standard price table only (no account overrides), §2.5 renders without quota references\n", opts.configPath, cfgErr)
	}
	pricingSrc, pricingInfo := buildPricing(cfg, cfgErr, opts.configPath, tw, opts.displayCCY, opts.exchangeRate)

	// 0o700/0o600: report outputs embed full conversation bodies from the
	// 0600 audit files - the derived copies must not loosen that. Created up
	// front (the detail writer below needs its output directory to exist
	// before Build's aggregation pass starts feeding it records).
	if err := os.MkdirAll(opts.outDir, 0o700); err != nil {
		return nil, err
	}

	dw, detailDir, onRecord, err := setupDetailWriter(opts.outDir, opts.detailsOn, opts.lang, tw)
	if err != nil {
		return nil, err
	}
	requestsDir := filepath.Join(opts.outDir, "requests")
	if err := os.MkdirAll(requestsDir, 0o700); err != nil {
		return nil, err
	}
	reqPath := filepath.Join(requestsDir, "index.json")
	cacheDir := filepath.Join(opts.outDir, ".cache", "parse")
	priorCache := ctxgraph.LoadCacheDir(cacheDir)
	now := time.Now()
	quotas, quotaJSONPath := buildProviderQuotas(cfg, cfgErr, opts.configPath, tw, now)
	fmt.Fprintf(tw, "session analysis + aggregation: scanning %d file(s)...\n", len(paths))
	rep, sess, cache, err := report.BuildCached(paths, now, tw, pricingInfo, pricingSrc, onRecord, resolveTaskProfile(), priorCache, quotas, opts.excludeClientTags)
	if err != nil {
		return nil, err
	}
	// Name the live-quota counter's own source path in the report, and
	// flag when every input audit log lies outside this instance's log_dir
	// — the one-machine variant of "the live column may be from a different
	// instance than the logs being analyzed" that can happen today (copying
	// a colleague's audit logs onto a machine with its own healthy
	// vmr-quota.json). Only meaningful when the sub-table actually has
	// something to render.
	if quotaJSONPath != "" && len(rep.ProviderQuotas) > 0 {
		rep.Meta.QuotaJSONPath = quotaJSONPath
		rep.Meta.QuotaInputOutsideLogDir = allPathsOutsideDir(paths, cfg.LogDir)
	}
	rep.Meta.ReportConfigPath = opts.reportConfigPath
	rep.Meta.DetailsEnabled = detailsPresentFor(opts.detailsOn, detailDir) // see its own doc comment
	report.LocalizeEfficiency(rep, opts.lang)
	if err := report.WriteMacroSlices(opts.outDir, rep, opts.lang); err != nil {
		return nil, err
	}

	if dw != nil {
		n, err := dw.Close()
		if err != nil {
			return nil, fmt.Errorf("details: %w", err)
		}
		fmt.Fprintf(tw, "%d detail file(s) (.md) in %s\n", n, detailDir)
	}

	_, lineageToJourney := loadStoriesLink(opts.outDir)

	// Requests index (requests/index.json, the machine-readable single
	// source of truth with the session projection and journey cross-links;
	// D7/§3.7 retired the human-readable request markdown indexes) + the
	// parse cache, persisted separately into cacheDir right below.
	rows := rep.RequestRows()
	if err := ctxgraph.SaveCacheDir(cacheDir, cache); err != nil {
		return nil, fmt.Errorf("parse cache: %w", err)
	}
	if err := report.WriteRequestsIndex(rep, sess, requestsDir, opts.lang, lineageToJourney, detailDir); err != nil {
		return nil, fmt.Errorf("requests index: %w", err)
	}
	fmt.Fprintf(tw, "%s (%d rows)\n", reqPath, len(rows))
	// Failed-requests index: a dedicated error-analysis view (outcome ==
	// error|canceled, plus ok-but-truncated), each row linking to its
	// details/*.md. Purely additive — every other report/requests output
	// above is unaffected and still lists these same failed requests
	// inline as before.
	failedRows := report.FailedRequestRows(rows)
	failedJSONLPath := filepath.Join(requestsDir, "failed.jsonl")
	nFailed, err := report.WriteRequestsJSONL(failedRows, failedJSONLPath)
	if err != nil {
		return nil, fmt.Errorf("failed-requests export: %w", err)
	}
	fmt.Fprintf(tw, "%s (%d rows)\n", failedJSONLPath, nFailed)

	// Manifest before rendering (D2/D11): the manifest stamps exactly the
	// slice set, all of which is on disk by this point — and the macro
	// Markdown rebuild reads the manifest (inputs, window, format) plus the
	// slices, so it must exist before any renderer runs. The admission
	// guarantee is untouched: manifest is still written after every stamped
	// artifact and before any reader, including vmr's own renderers.
	if err := writeReportManifest(opts.outDir, rep, opts.lang); err != nil {
		return nil, err
	}

	// Single render path from disk JSON (D11): render vmr-report.md and requests/failed.md
	if err := renderMacroReportFromDisk(opts.outDir, opts.lang); err != nil {
		return nil, fmt.Errorf("render macro report: %w", err)
	}
	mdPath := filepath.Join(opts.outDir, "vmr-report.md")
	fmt.Fprintf(tw, "%d records (%d parse errors) from %d file(s)\n%s\n%s\n",
		rep.Meta.Records, rep.Meta.ParseErrors, len(paths), opts.outDir+"/macro/*.json + manifest.json", mdPath)

	if err := renderFailedIndexFromDisk(requestsDir, opts.lang, detailDir); err != nil {
		return nil, fmt.Errorf("failed-requests index: %w", err)
	}
	fmt.Fprintf(tw, "%s\n", filepath.Join(requestsDir, "failed.md"))
	return rep, nil
}

// writeReportManifest builds and commits the snapshot manifest (§3.4).
// Used by the report half's own exit so the manifest exists before the
// markdown renderers read it; zoom modes commit theirs in finishAnalyze
// instead, which skips re-committing when the report half already did.
func writeReportManifest(outDir string, rep *report.Report2, lang i18n.Lang) error {
	manifest, err := report.BuildManifest(outDir, rep, lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: manifest not written — this snapshot will be treated as invalid by -render-only and the L2 cache until the next analyze: %v\n", err)
		return nil
	}
	if err := report.WriteManifest(outDir, manifest); err != nil {
		fmt.Fprintf(os.Stderr, "warning: manifest not written — this snapshot will be treated as invalid by -render-only and the L2 cache until the next analyze: %v\n", err)
	}
	return nil
}

// renderMacroReportFromDisk reads the manifest plus the five macro slices from
// outDir, builds the MacroReportVM, and serializes it to vmr-report.md (D2/D11).
func renderMacroReportFromDisk(outDir string, lang i18n.Lang) error {
	rep, err := report.LoadReport(outDir)
	if err != nil {
		return fmt.Errorf("load report json: %w", err)
	}
	storiesLink, lineageToJourney := loadStoriesLink(outDir)
	md := report.MacroMarkdown(rep, lang, storiesLink, lineageToJourney)
	mdPath := filepath.Join(outDir, "vmr-report.md")
	return os.WriteFile(mdPath, []byte(md), 0o600)
}

// renderFailedIndexFromDisk reads requests/index.json (or requests/failed.jsonl)
// from requestsDir and renders requests/failed.md (D11).
func renderFailedIndexFromDisk(requestsDir string, lang i18n.Lang, detailDir string) error {
	var rows []report.RequestRow
	indexPath := filepath.Join(requestsDir, "index.json")
	if data, err := os.ReadFile(indexPath); err == nil {
		var idx report.RequestsIndex
		if err := json.Unmarshal(data, &idx); err == nil {
			rows = idx.Requests
		}
	}
	if len(rows) == 0 {
		failedJSONLPath := filepath.Join(requestsDir, "failed.jsonl")
		if data, err := os.ReadFile(failedJSONLPath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				var r report.RequestRow
				if err := json.Unmarshal([]byte(line), &r); err == nil {
					rows = append(rows, r)
				}
			}
		}
	}
	return report.WriteFailedIndex(rows, requestsDir, lang, detailDir)
}
