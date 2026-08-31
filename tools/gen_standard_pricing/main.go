// Ver 2026-08-07, by Opus 5

// gen_standard_pricing generates
// internal/pricing/standard_price_generated.yaml from
// docs/data/model_prices_and_context_window.json (a LiteLLM-format
// price-list snapshot, MIT licensed — see that file's own license note and
// docs/VirtualModelRouter_Design_v4_Quota.md's §4.2③ for the attribution
// requirement this comment satisfies). Not part of the vmr binary — a
// one-off/periodic maintenance tool, run by hand:
//
//	go run ./tools/gen_standard_pricing -input docs/data/model_prices_and_context_window.json -output internal/pricing/standard_price_generated.yaml
//
// Two rules that are NOT optional (see the design doc's §4.2① "缺失比过期更
// 危险" and its §9.1 validation checklist):
// - A component absent from the source JSON is OMITTED from the output
// row entirely — never written as 0.0. internal/pricing.Rate depends on
// this distinction (nil = unknown, *float64(0) = explicitly free) to
// safely reject metric: cost accounts with genuinely incomplete pricing
// instead of silently under-charging them.
// - This script only ever writes standard_price_generated.yaml — never
// standard_price_curated.yaml, the hand-maintained file this table is
// merged with at load time (see internal/pricing/embed.go). Overwriting
// curated.yaml here would silently discard every hand-added row the
// next time this script runs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"vmr/internal/pricing"
)

// sourceEntry is the handful of fields this script reads from one
// LiteLLM-format JSON object; every other field (context windows,
// capability flags, deprecation dates, ...) is irrelevant to pricing and
// ignored.
type sourceEntry struct {
	LitellmProvider         string   `json:"litellm_provider"`
	Mode                    string   `json:"mode"`
	InputCostPerToken       *float64 `json:"input_cost_per_token"`
	OutputCostPerToken      *float64 `json:"output_cost_per_token"`
	CacheReadInputTokenCost *float64 `json:"cache_read_input_token_cost"`
	CacheCreationTokenCost  *float64 `json:"cache_creation_input_token_cost"`
}

// primaryVendors is the allowlist of litellm_provider values this script
// keeps — direct API vendors relevant to vmr's routing use case (matches
// CLAUDE.md's "Mainland China focus, global reach" audience and the
// adapters/example providers already in this repo). Deliberately excludes
// reseller/wrapper integrations (bedrock, vertex_ai-*, azure, vercel_ai_gateway,
// snowflake, heroku, oci, watsonx, databricks, ...): those often carry
// different (markup or bundled-discount) pricing than the vendor's own API,
// and including them risks a canonical-key collision silently picking the
// wrong number for a direct-API account. See
// docs/VirtualModelRouter_Design_v4_Quota.md's "标准表以开源参考表的形式维护"
// section for why standard-table coverage is inherently a curated subset,
// not "every row in the source".
var primaryVendors = map[string]bool{
	"openai": true, "anthropic": true, "gemini": true, "deepseek": true,
	"moonshot": true, "dashscope": true, "minimax": true, "volcengine": true,
	"mistral": true, "xai": true, "groq": true, "together_ai": true,
	"fireworks_ai": true, "openrouter": true, "perplexity": true,
	"cohere": true, "cohere_chat": true,
}

// generatedRow is standard_price_generated.yaml's per-model shape — see
// internal/pricing/pricing.go's fileRate for the loader's matching type.
// Pointer fields marshal as an omitted key (not `null`) when nil, via
// yaml.v3's default omitempty-like nil handling for pointers combined with
// the `omitempty` tag below.
type generatedRow struct {
	Key        string   `yaml:"key"`
	InFresh    *float64 `yaml:"in_fresh,omitempty"`
	CacheRead  *float64 `yaml:"cache_read,omitempty"`
	CacheWrite *float64 `yaml:"cache_write,omitempty"`
	Out        *float64 `yaml:"out,omitempty"`
}

// UpstreamURL is the LiteLLM price list this table is derived from — the
// -url default, so a refresh needs no hand-download step (the reason the
// snapshot under docs/data went 20 days stale: every refresh used to start
// with "go find the file and download it").
const UpstreamURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

func main() {
	url := flag.String("url", UpstreamURL, "fetch the source JSON from here instead of -input; set to \"\" to read -input only")
	input := flag.String("input", "docs/data/model_prices_and_context_window.json", "LiteLLM-format source JSON — read when -url is empty, and REWRITTEN with the fetched bytes when -url is used (the snapshot stays in the repo as the reproducible input)")
	output := flag.String("output", "internal/pricing/standard_price_generated.yaml", "output path (always OVERWRITTEN — never standard_price_curated.yaml)")
	today := time.Now().Format("2006-01-02")
	generatedAt := flag.String("generated-at", today, "generation date (YYYY-MM-DD); defaults to today ("+today+")")
	flag.Parse()

	if strings.HasSuffix(*output, "curated.yaml") {
		log.Fatalf("refusing to write to %s: this script only ever writes the GENERATED table, never the hand-maintained curated one", *output)
	}

	data, err := loadSource(*url, *input)
	if err != nil {
		log.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		log.Fatalf("parse %s: %v", *input, err)
	}

	sortedRows, kept, skipped := generateRows(raw)

	genDate := *generatedAt
	if genDate == "" {
		genDate = time.Now().Format("2006-01-02")
	}
	writeYAML(*output, genDate, sortedRows)
	fmt.Fprintf(os.Stderr, "wrote %d rows to %s (%d source entries skipped: wrong mode, non-primary vendor, or no pricing data)\n", kept, *output, skipped)
	reportAmbiguity(*output)
}

// reportAmbiguity prints, for the table just written (merged with the
// hand-maintained curated table beside it), every bare model name carried by
// more than one vendor and how it currently resolves.
//
// Vendor precedence is the one part of price resolution that can change
// WITHOUT anyone editing anything: a refresh that adds a second first-party
// row turns a name that used to resolve into one that does not, silently.
// That is not hypothetical — the 2026-08-31 refresh did it to four models
// this repo routes, when dashscope started reselling DeepSeek/Zhipu/Moonshot.
// Printing the state here makes a flip visible at the moment it is
// introduced. Diff two runs' output and the delta is the answer to "what did
// this refresh change about which vendor's price we use".
//
// Best-effort: a table that cannot be re-read (unusual output path, no
// curated file beside it) prints a note instead of failing the run — the
// generated table is already written and correct at this point.
func reportAmbiguity(outputPath string) {
	merged, err := mergedTable(outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ambiguity report skipped: %v\n", err)
		return
	}
	amb := merged.Ambiguities()
	var pinned, byPrecedence, unresolved int
	var lines []string
	for _, a := range amb {
		switch {
		case a.Pinned():
			pinned++
			continue // an alias decides it; a refresh cannot move it
		case a.Winner != "":
			byPrecedence++
			lines = append(lines, fmt.Sprintf("  %-40s -> %-34s (vendors: %s)", a.Model, a.Winner, strings.Join(a.Vendors, ",")))
		default:
			unresolved++
			lines = append(lines, fmt.Sprintf("  %-40s -> UNRESOLVED%-23s(vendors: %s)", a.Model, "", strings.Join(a.Vendors, ",")))
		}
	}
	fmt.Fprintf(os.Stderr, "\nbare-name collisions: %d total — %d pinned by an alias, %d decided by vendor precedence, %d unresolved\n",
		len(amb), pinned, byPrecedence, unresolved)
	if len(lines) > 0 {
		fmt.Fprintf(os.Stderr, "the %d below are NOT pinned; diff this list across refreshes to see what moved:\n", len(lines))
		fmt.Fprintln(os.Stderr, strings.Join(lines, "\n"))
	}
}

// mergedTable re-reads the generated table just written plus the curated
// table beside it — the same merge internal/pricing.LoadStandard performs at
// runtime. Read from disk rather than through LoadStandard because this
// binary's own go:embed copy was fixed at compile time, i.e. BEFORE this run
// overwrote the file.
func mergedTable(outputPath string) (*pricing.Table, error) {
	generated, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}
	gt, err := pricing.ParseTable(generated)
	if err != nil {
		return nil, err
	}
	curatedPath := filepath.Join(filepath.Dir(outputPath), "standard_price_curated.yaml")
	curated, err := os.ReadFile(curatedPath)
	if err != nil {
		return nil, err
	}
	ct, err := pricing.ParseTable(curated)
	if err != nil {
		return nil, err
	}
	merged := pricing.Merge(gt, ct)
	if err := merged.ValidateAliases(); err != nil {
		// Not fatal to the refresh itself (the table is written), but this is
		// exactly the case a refresh introduces: an alias whose target row the
		// upstream snapshot just dropped.
		fmt.Fprintf(os.Stderr, "WARNING: %v\n", err)
	}
	return merged, nil
}

// loadSource reads the LiteLLM snapshot: from url when it is non-empty
// (and writes the fetched bytes back to input, so the repo keeps the exact
// bytes this run was generated from — a table nobody can reproduce is a
// table nobody can audit), otherwise straight from input. The JSON is
// validated as a decodable object before input is overwritten: a 404 page
// or a truncated transfer must not clobber a good snapshot.
func loadSource(url, input string) ([]byte, error) {
	if url == "" {
		data, err := os.ReadFile(input)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", input, err)
		}
		return data, nil
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w (pass -url \"\" to use the checked-in snapshot instead)", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("fetch %s: response is not a JSON object (%w) — refusing to overwrite %s", url, err, input)
	}
	if err := os.WriteFile(input, data, 0644); err != nil {
		return nil, fmt.Errorf("write snapshot %s: %w", input, err)
	}
	fmt.Fprintf(os.Stderr, "fetched %d bytes from %s into %s\n", len(data), url, input)
	return data, nil
}

// generateRows is main()'s pure transform — extracted so it's unit-testable
// without going through flag parsing/file I/O/log.Fatal. raw is the
// decoded top-level JSON object (canonical LiteLLM-snapshot key ->
// undecoded per-model object); returns the deterministically-sorted output
// rows plus how many source entries were kept vs skipped.
func generateRows(raw map[string]json.RawMessage) (sortedRows []generatedRow, kept, skipped int) {
	rows := map[string]generatedRow{} // canonical key -> row, dedup by first-seen
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic iteration order -> deterministic "first seen wins" dedup

	for _, key := range keys {
		if key == "sample_spec" {
			continue
		}
		var e sourceEntry
		if err := json.Unmarshal(raw[key], &e); err != nil {
			continue // a handful of entries have shapes this loose struct can't decode; skip, don't crash the whole run
		}
		if e.Mode != "chat" {
			skipped++
			continue
		}
		if !primaryVendors[e.LitellmProvider] {
			skipped++
			continue
		}
		if e.InputCostPerToken == nil && e.OutputCostPerToken == nil {
			skipped++ // no pricing data at all — pure metadata entry
			continue
		}
		basename := pricing.ModelBasename(key)
		// Lowercased because internal/pricing.Table matches keys
		// case-insensitively: two source ids differing only in case (LiteLLM
		// carries both together_ai/google/gemma-4-31B-it and
		// together_ai/pearl-ai/gemma-4-31b-it) would otherwise emit two rows
		// the loader then rejects as a duplicate key — a generated table that
		// fails to load at all, which is exactly what happened on the
		// 2026-08-31 refresh.
		canonical := strings.ToLower(e.LitellmProvider + "/" + basename)
		if _, exists := rows[canonical]; exists {
			continue // first-seen wins (sorted key order makes this deterministic)
		}
		rows[canonical] = generatedRow{
			Key:        canonical,
			InFresh:    perMillion(e.InputCostPerToken),
			CacheRead:  perMillion(e.CacheReadInputTokenCost),
			CacheWrite: perMillion(e.CacheCreationTokenCost),
			Out:        perMillion(e.OutputCostPerToken),
		}
		kept++
	}

	sortedRows = make([]generatedRow, 0, len(rows))
	for _, r := range rows {
		sortedRows = append(sortedRows, r)
	}
	sort.Slice(sortedRows, func(i, j int) bool { return sortedRows[i].Key < sortedRows[j].Key })
	return sortedRows, kept, skipped
}

// fileHeader is standard_price_generated.yaml's top-level shape — the exact
// shape internal/pricing.ParseTable reads back (see that file's fileTable
// type), plus the license/attribution note §4.2③ requires travel with the
// data, not just live in this generator's source comment.
type fileHeader struct {
	Currency    string         `yaml:"currency"`
	GeneratedAt string         `yaml:"generated_at"`
	Rates       []generatedRow `yaml:"rates"`
}

func writeYAML(path, generatedAt string, rows []generatedRow) {
	const license = `# Ver 2026-08-07, by Opus 5
#
# GENERATED FILE — do not hand-edit. Produced by
# tools/gen_standard_pricing from docs/data/model_prices_and_context_window.json
# (a LiteLLM-format price-list snapshot, MIT licensed:
# https://github.com/BerriAI/litellm — attribution preserved per
# docs/VirtualModelRouter_Design_v4_Quota.md's §4.2③).
#
# Hand-maintained additions (mainly domestic/first-party vendors this
# source under-covers) belong in standard_price_curated.yaml instead, NEVER
# here — this file is fully overwritten every time the generator runs.
#
# Prices are list prices, not a guarantee of accuracy — verify against the
# vendor's own published pricing before relying on a $ figure this table
# produced. A stale-looking table is more dangerous than a missing one:
# check generated_at below against today's date before trusting these
# numbers for anything that matters.
#
# All amounts are USD per 1,000,000 tokens. A component absent from a row
# means "unknown" (not "free") — see internal/pricing.Rate.Complete.
#
# Aliases are NOT written here. A bare model name -> canonical key mapping
# is a resolution-semantic the hand-maintained standard_price_curated.yaml
# owns (see that file's aliases: block) — a generated table must stay a pure
# price list, or a regenerated snapshot would silently overwrite a hand
# decision. Vendor precedence (LookupPreferredSuffix) already resolves most
# bare names with no alias at all.
`
	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(license); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	defer enc.Close()
	if err := enc.Encode(fileHeader{Currency: "USD", GeneratedAt: generatedAt, Rates: rows}); err != nil {
		log.Fatalf("encode %s: %v", path, err)
	}
}

// perMillion converts a per-token USD rate to per-1M-token, preserving nil
// (missing component, as opposed to an explicit 0.0) — see this file's
// package doc comment for why that distinction must survive the
// conversion untouched.
func perMillion(v *float64) *float64 {
	if v == nil {
		return nil
	}
	scaled := *v * 1_000_000
	return &scaled
}
