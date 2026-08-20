// Ver 2026-07-29 14:00, by Sonnet 5
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/ctxgraph"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/pricing"
	"vmr/internal/quota"
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
	summary := &report.Pricing{Currency: standard.Currency, StandardGeneratedAt: standard.GeneratedAt}

	var resolver *pricing.Resolver
	var configRates map[string]float64
	if loadErr != nil {
		// cmdReport already printed one unified warning for cfgErr — a
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
		if cfg.Pricing != nil {
			summary.Currency = cfg.Pricing.Currency
			summary.Supplement = cfg.Pricing.Supplement
			configRates = cfg.Pricing.ExchangeRate
		}
		summary.ProviderOverrides = overrideCount
		if overrideCount > 0 {
			fmt.Fprintf(tw, "pricing: %d provider override rule(s) loaded from %s\n", overrideCount, configPath)
		}
		resolver = pricing.NewResolver(table, perProvider)
	}
	if summary.Currency == "" {
		summary.Currency = "USD"
	}

	if displayCCY != "" && !strings.EqualFold(displayCCY, summary.Currency) {
		rates := map[string]float64{}
		for k, v := range configRates {
			rates[k] = v
		}
		for k, v := range extraRates { // report.yaml's own rates win over config.yaml's on a matching key
			rates[k] = v
		}
		if factor, ok := pricing.FactorBetween(summary.Currency, displayCCY, rates); ok {
			resolver = resolver.WithDisplayFactor(factor)
			summary.Currency = displayCCY
		} else {
			fmt.Fprintf(tw, "pricing: no exchange rate to convert %s -> %s for -currency, showing %s instead (add exchange_rate: {%s: <rate>} to config.yaml's pricing: block or report.yaml)\n", summary.Currency, displayCCY, summary.Currency, displayCCY)
		}
	}
	return resolver, summary
}

// buildProviderQuotas loads declared quota limits from config and live quota state from vmr-quota.json.
// Returns nil if config or live quota is unavailable without failing report generation.
func buildProviderQuotas(cfg *config.Config, loadErr error, configPath string, tw io.Writer, now time.Time) (map[string]report.ProviderQuotaRef, string) {
	if loadErr != nil {
		// cmdReport already printed one unified warning for cfgErr — a
		// second, near-identical one here would just repeat it.
		// configPath is kept in the signature purely so this function's
		// doc comment / callers stay symmetric with buildPricing's.
		return nil, ""
	}
	quotaJSONPath := filepath.Join(cfg.LogDir, "vmr-quota.json")
	live, err := quota.LoadFile(quotaJSONPath)
	if err != nil {
		fmt.Fprintf(tw, "provider quotas: %s not usable (%v) — §2.5's real-time columns render as \"-\"\n", quotaJSONPath, err)
	}
	quotas := map[string]report.ProviderQuotaRef{}
	for _, p := range cfg.Providers {
		if p.Quota == nil || len(p.Quota.Limits) == 0 {
			continue
		}
		// Limits[0] is the only correct read here — config.validateQuota
		// rejects len(Limits) > 1 at load time (P1's "exactly one Limit per
		// provider"; see TestQuota_Reject_MultipleLimits in internal/config),
		// so this can never silently drop a second window. P3 (multi-window
		// quota) will need this whole function rewritten to fold across
		// every Limit, not just widened past index 0.
		lim := p.Quota.Limits[0].Resolved
		spec := &core.QuotaSpec{
			Limits:           []core.Limit{lim},
			TokenWeights:     p.Quota.ResolvedTokenWeights,
			ModelMultipliers: p.Quota.ModelMultipliers,
		}
		ref := report.ProviderQuotaRef{
			Metric: string(lim.Metric),
			Every:  lim.EveryText,
			Amount: lim.Amount,
			Limit:  &lim,
			Spec:   spec,
		}
		// §5.2's stale-period trap: quota.Registry resets lazily, so a
		// bucket still on disk from a period the process wasn't running
		// through must NOT be rendered as "this period's usage" — only a
		// bucket whose stored PeriodStart matches what PeriodStart(lim, now)
		// computes for right now qualifies as Live.
		limitKey := string(lim.Metric) + "/" + lim.EveryText
		periodStart := quota.PeriodStart(lim, now)
		if b, ok := live[p.Name][limitKey]; ok && b.PeriodStartTime().Equal(periodStart) {
			used := quota.BaseAmount(spec, b.C)
			var pct float64
			if lim.Amount > 0 {
				pct = used / lim.Amount * 100
			}
			ref.Live = &report.LiveQuota{
				Used: used, Pct: pct,
				PeriodStart: periodStart, PeriodEndsAt: quota.PeriodEnd(lim, now),
				EstimatedPct: quota.EstimatedPct(lim.Metric, b.C, b.Estimated, b.EstimatedCost),
			}
		} else if _, exists := live[p.Name][limitKey]; !exists && len(live[p.Name]) > 0 {
			// Distinguishes two different-looking "Live is nil" causes
			// that the generic stale-period footnote alone conflates:
			// - limitKey absent, but this provider DOES have other keys
			// on disk → its quota:'s metric/every changed since those
			// were last written (Registry never deletes an old key —
			// it's lazy-reset, not lazy-cleaned), so the OLD bucket is
			// simply keyed differently now. The process is healthy and
			// running; the config just moved out from under it.
			// - limitKey present but period mismatch (the `if` branch's
			// negative), or no data for this provider at all → the
			// existing "process wasn't running through this period" or
			// "never charged yet" story, unchanged.
			ref.LiveConfigChanged = true
		}
		quotas[p.Name] = ref
	}
	return quotas, quotaJSONPath
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

// cmdReport aggregates audit JSONL into internal/report's output:
// vmr-report.json/.md, vmr-requests.json/.md (+ per-tag siblings),
// vmr-requests-failed.jsonl/.md (error-analysis index: outcome ==
// error|canceled plus ok-but-truncated, additive — doesn't remove those
// requests from anything above), and one details/*.md+.json per request.
// Inputs may freely mix live plain .jsonl files and .jsonl.zst files that
// the audit logger's housekeeping sweep has since compressed
// (internal/report decompresses transparently) — e.g.
// `vmr report 'vmr-audit-*.jsonl*'`. With no input files given at all,
// defaults to <-c config.yaml's log_dir>/vmr-audit-* (see resolveInputPaths
// in auditpaths.go) — the common case of "just report on this instance's
// own logs" needs no arguments beyond an optional -c.
// setupDetailWriter creates {outDir}/details and starts the detail-page
// worker pool when detailsOn — Build's onRecord hook (nil when !detailsOn)
// renders+writes each record's detail page during the aggregation pass
// itself, on its own worker pool, so there's no separate third read of the
// audit source for detail export. Build's own success/failure never
// depends on this: a detail-write failure surfaces only when the returned
// *report.DetailWriter's Close is checked, well after vmr-report.json/md
// are already safely on disk — same robustness the old separate-
// WriteDetails-step had, just without the extra pass.
func setupDetailWriter(outDir string, detailsOn bool, lang i18n.Lang, tw io.Writer) (dw *report.DetailWriter, detailDir string, onRecord func(*audit.Record, *report.ReqInfo), err error) {
	detailDir = filepath.Join(outDir, "details")
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

func cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	configPath := fs.String("c", "config.yaml", "config file to resolve log_dir from (when no input files are given) and to resolve pricing from (providers[].pricing / global pricing: block) — see PricingTable's doc comment for the no-config.yaml degrade")
	outDirFlag := fs.String("o", "", "output directory (default: ./reports, or report.yaml's output)")
	detailsFlag := fs.Bool("details", false, "also render one Markdown file per request into {out}/details/ (default: report.yaml's details, or false — the requests index links to each record's detail filename regardless, computed without needing the file to exist; pass -details to materialize them all up front)")
	langFlag := fs.String("lang", "", "output language: en|zh (default: report.yaml's language, or en) — overrides report.yaml")
	currencyFlag := fs.String("currency", "", "display currency for $ cost estimates, e.g. CNY|JPY (default: report.yaml's currency, or whatever currency pricing resolved in — usually -c's config.yaml pricing.currency, or USD); needs a matching rate in config.yaml's pricing.exchange_rate or report.yaml's exchange_rate")
	reportConfigPath := fs.String("report-config", "", "vmr report/vmr story sidecar config yaml; absent => auto-load ./report.yaml if present")
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths, err := resolveInputPaths(fs, *configPath)
	if err != nil {
		return err
	}

	tw := timestampWriter{w: os.Stdout}

	rc := resolveReportConfig(*reportConfigPath, tw)
	lang, err := resolveLanguage(*langFlag, rc, tw)
	if err != nil {
		return err
	}
	outDir := resolveString(*outDirFlag, rc.Output, "reports")
	detailsOn := resolveBool(flagPassed(fs, "details"), *detailsFlag, rc.Details)

	// Single config.Load, shared by buildPricing and
	// buildProviderQuotas below — see either function's own doc comment for
	// why splitting this into two independent loads would be a consistency
	// bug (a config edit landing between them), not just a wasted read. A
	// load failure here is NOT fatal to `vmr report`: both callees degrade
	// independently (pricing falls back to the embedded standard table;
	// the quota section simply doesn't render) — see cfgErr's threading
	// below, never returned as this function's own error.
	cfg, cfgErr := config.Load(*configPath)
	if cfgErr != nil {
		// One unified warning for both degrade paths — buildPricing
		// and buildProviderQuotas used to each print their own near-
		// duplicate of this, so a bare-logs `vmr report` run reliably saw
		// two warnings naming the same unreadable file.
		fmt.Fprintf(tw, "config: %s not usable (%v) — $ estimates use the standard price table only (no account overrides), §2.5 renders without quota references\n", *configPath, cfgErr)
	}

	displayCCY := resolveString(*currencyFlag, rc.Currency, "")
	pricingSrc, pricingInfo := buildPricing(cfg, cfgErr, *configPath, tw, displayCCY, rc.ExchangeRate)

	// 0o700/0o600: report outputs embed full conversation bodies from the
	// 0600 audit files - the derived copies must not loosen that. Created
	// up front now (used to happen after Build succeeded): the detail
	// writer below needs its output directory to exist before Build's
	// aggregation pass starts feeding it records, since detail rendering
	// now happens inside that same pass instead of as a separate step
	// afterward.
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}

	dw, detailDir, onRecord, err := setupDetailWriter(outDir, detailsOn, lang, tw)
	if err != nil {
		return err
	}

	// The gap between this line's timestamp and the first "[1/N]" line below
	// is session analysis (AnalyzeSessions) — a full, currently silent pass
	// over every input file that Build() always runs before its own
	// per-file aggregation loop starts printing. priorCache (from
	// {outDir}/.parse-cache, shared with `vmr story` — see
	// docs/VirtualModelRouter_Design_v4_Analytics.md's vmr-requests.json
	// section) lets that pass skip re-parsing/re-hashing any input file
	// whose content hasn't changed.
	reqPath := filepath.Join(outDir, "vmr-requests.json")
	cacheDir := filepath.Join(outDir, ".parse-cache")
	priorCache := ctxgraph.LoadCacheDir(cacheDir)
	now := time.Now()
	quotas, quotaJSONPath := buildProviderQuotas(cfg, cfgErr, *configPath, tw, now)
	fmt.Fprintf(tw, "session analysis + aggregation: scanning %d file(s)...\n", len(paths))
	rep, sess, cache, err := report.BuildCached(paths, now, tw, pricingInfo, pricingSrc, onRecord, resolveTaskProfile(), priorCache, quotas)
	if err != nil {
		return err
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
	jsonPath := filepath.Join(outDir, "vmr-report.json")
	mdPath := filepath.Join(outDir, "vmr-report.md")
	if err := report.WriteJSON(rep, jsonPath); err != nil {
		return err
	}
	if err := os.WriteFile(mdPath, []byte(report.Markdown(rep, lang)), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(tw, "%d records (%d parse errors) from %d file(s)\n", rep.Meta.Records, rep.Meta.ParseErrors, len(paths))
	fmt.Fprintf(tw, "%s\n", jsonPath)
	fmt.Fprintf(tw, "%s\n", mdPath)

	if dw != nil {
		n, err := dw.Close()
		if err != nil {
			return fmt.Errorf("details: %w", err)
		}
		fmt.Fprintf(tw, "%d detail file(s) (.md) in %s\n", n, detailDir)
	}

	// Requests index (+ per-tag siblings) + json (data only — the parse
	// cache is persisted separately, into cacheDir, right below).
	rows := rep.RequestRows()
	nReq, err := report.WriteRequestsJSON(rows, reqPath)
	if err != nil {
		return fmt.Errorf("requests export: %w", err)
	}
	fmt.Fprintf(tw, "%s (%d rows)\n", reqPath, nReq)
	if err := ctxgraph.SaveCacheDir(cacheDir, cache); err != nil {
		return fmt.Errorf("parse cache: %w", err)
	}
	if err := report.WriteRequestsIndex(rep, sess, outDir, lang); err != nil {
		return fmt.Errorf("requests index: %w", err)
	}
	fmt.Fprintf(tw, "%s\n", filepath.Join(outDir, "vmr-requests.md"))

	// Failed-requests index: a dedicated error-analysis view (outcome ==
	// error|canceled, plus ok-but-truncated), each row linking to its
	// details/*.md. Purely additive — every other report/requests output
	// above is unaffected and still lists these same failed requests
	// inline as before.
	failedRows := report.FailedRequestRows(rows)
	failedJSONLPath := filepath.Join(outDir, "vmr-requests-failed.jsonl")
	nFailed, err := report.WriteRequestsJSONL(failedRows, failedJSONLPath)
	if err != nil {
		return fmt.Errorf("failed-requests export: %w", err)
	}
	fmt.Fprintf(tw, "%s (%d rows)\n", failedJSONLPath, nFailed)
	if err := report.WriteFailedIndex(rows, outDir, lang); err != nil {
		return fmt.Errorf("failed-requests index: %w", err)
	}
	fmt.Fprintf(tw, "%s\n", filepath.Join(outDir, "vmr-requests-failed.md"))
	return nil
}
