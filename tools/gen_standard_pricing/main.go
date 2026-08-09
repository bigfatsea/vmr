// Ver 2026-08-07, by Opus 5

// gen_standard_pricing generates
// internal/pricing/standard_price_generated.yaml from
// docs/data/model_prices_and_context_window.json (a LiteLLM-format
// price-list snapshot, MIT licensed — see that file's own license note and
// docs/TokenPlan_Quota_Routing_Design_opus-5.md's §4.2③ for the attribution
// requirement this comment satisfies). Not part of the vmr binary — a
// one-off/periodic maintenance tool, run by hand:
//
//	go run ./tools/gen_standard_pricing -input docs/data/model_prices_and_context_window.json -output internal/pricing/standard_price_generated.yaml
//
// Two rules that are NOT optional (see the design doc's §4.2① "缺失比过期更
// 危险" and its §9.1 validation checklist):
//   - A component absent from the source JSON is OMITTED from the output
//     row entirely — never written as 0.0. internal/pricing.Rate depends on
//     this distinction (nil = unknown, *float64(0) = explicitly free) to
//     safely reject metric: cost accounts with genuinely incomplete pricing
//     instead of silently under-charging them.
//   - This script only ever writes standard_price_generated.yaml — never
//     standard_price_curated.yaml, the hand-maintained file this table is
//     merged with at load time (see internal/pricing/embed.go). Overwriting
//     curated.yaml here would silently discard every hand-added row the
//     next time this script runs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
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
// docs/TokenPlan_Quota_P2_DevPlan_opus-5.md §6 for why standard-table
// coverage is inherently a curated subset, not "every row in the source".
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

func main() {
	input := flag.String("input", "docs/data/model_prices_and_context_window.json", "LiteLLM-format source JSON")
	output := flag.String("output", "internal/pricing/standard_price_generated.yaml", "output path (always OVERWRITTEN — never standard_price_curated.yaml)")
	generatedAt := flag.String("generated-at", "", "generation date (YYYY-MM-DD); defaults to today")
	flag.Parse()

	if strings.HasSuffix(*output, "curated.yaml") {
		log.Fatalf("refusing to write to %s: this script only ever writes the GENERATED table, never the hand-maintained curated one", *output)
	}

	data, err := os.ReadFile(*input)
	if err != nil {
		log.Fatalf("read %s: %v", *input, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		log.Fatalf("parse %s: %v", *input, err)
	}

	sortedRows, kept, skipped := generateRows(raw)

	if *generatedAt == "" {
		log.Fatal("-generated-at is required (e.g. -generated-at 2026-08-07) — see design doc's §4.2③ guardrail #1: a stale-looking table is more dangerous than a missing one, so the generation date must always be explicit, never silently defaulted to \"whenever this ran\"")
	}
	writeYAML(*output, *generatedAt, sortedRows)
	fmt.Fprintf(os.Stderr, "wrote %d rows to %s (%d source entries skipped: wrong mode, non-primary vendor, or no pricing data)\n", kept, *output, skipped)
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
		basename := key
		if i := strings.LastIndex(key, "/"); i >= 0 {
			basename = key[i+1:]
		}
		canonical := e.LitellmProvider + "/" + basename
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
# docs/TokenPlan_Quota_Routing_Design_opus-5.md's §4.2③).
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
