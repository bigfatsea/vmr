// Ver 2026-08-07, by Opus 5
package main

import (
	"encoding/json"
	"testing"
)

func f(v float64) *float64 { return &v }

func TestPerMillion(t *testing.T) {
	if got := perMillion(nil); got != nil {
		t.Fatalf("perMillion(nil) = %v, want nil (missing must stay missing)", got)
	}
	got := perMillion(f(2.8e-07))
	if got == nil || *got != 0.28 {
		t.Fatalf("perMillion(2.8e-07) = %v, want 0.28", got)
	}
	zero := perMillion(f(0))
	if zero == nil || *zero != 0 {
		t.Fatalf("perMillion(0) = %v, want non-nil pointer to 0.0 (explicit zero, not missing)", zero)
	}
}

func rawEntries(t *testing.T, entries map[string]any) map[string]json.RawMessage {
	t.Helper()
	out := map[string]json.RawMessage{}
	for k, v := range entries {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		out[k] = b
	}
	return out
}

func TestGenerateRows_SkipsNonChatMode(t *testing.T) {
	raw := rawEntries(t, map[string]any{
		"dall-e-3": map[string]any{"litellm_provider": "openai", "mode": "image_generation", "output_cost_per_image": 0.04},
	})
	rows, kept, skipped := generateRows(raw)
	if kept != 0 || skipped != 1 || len(rows) != 0 {
		t.Fatalf("kept=%d skipped=%d rows=%v, want kept=0 skipped=1", kept, skipped, rows)
	}
}

func TestGenerateRows_SkipsNonPrimaryVendor(t *testing.T) {
	raw := rawEntries(t, map[string]any{
		"azure/gpt-4o": map[string]any{"litellm_provider": "azure", "mode": "chat", "input_cost_per_token": 2.5e-06, "output_cost_per_token": 1e-05},
	})
	rows, kept, skipped := generateRows(raw)
	if kept != 0 || skipped != 1 || len(rows) != 0 {
		t.Fatalf("kept=%d skipped=%d rows=%v, want a reseller/wrapper vendor filtered out", kept, skipped, rows)
	}
}

func TestGenerateRows_SkipsNoPricingData(t *testing.T) {
	raw := rawEntries(t, map[string]any{
		"gpt-4o": map[string]any{"litellm_provider": "openai", "mode": "chat", "max_input_tokens": 128000},
	})
	_, kept, skipped := generateRows(raw)
	if kept != 0 || skipped != 1 {
		t.Fatalf("kept=%d skipped=%d, want a pure-metadata entry (no cost fields) skipped", kept, skipped)
	}
}

func TestGenerateRows_SkipsSampleSpec(t *testing.T) {
	raw := rawEntries(t, map[string]any{
		"sample_spec": map[string]any{"litellm_provider": "one of ...", "input_cost_per_token": 0.0},
	})
	rows, kept, skipped := generateRows(raw)
	if kept != 0 || skipped != 0 || len(rows) != 0 {
		t.Fatalf("kept=%d skipped=%d rows=%v, want sample_spec silently ignored (not even counted as skipped)", kept, skipped, rows)
	}
}

func TestGenerateRows_MissingComponentStaysNil(t *testing.T) {
	raw := rawEntries(t, map[string]any{
		"deepseek-chat": map[string]any{
			"litellm_provider": "deepseek", "mode": "chat",
			"input_cost_per_token": 2.8e-07, "output_cost_per_token": 4.2e-07,
			"cache_read_input_token_cost": 2.8e-08,
			// cache_creation_input_token_cost deliberately absent
		},
	})
	rows, kept, _ := generateRows(raw)
	if kept != 1 || len(rows) != 1 {
		t.Fatalf("kept=%d rows=%v, want exactly 1 row", kept, rows)
	}
	r := rows[0]
	if r.Key != "deepseek/deepseek-chat" {
		t.Fatalf("Key = %q, want deepseek/deepseek-chat", r.Key)
	}
	if r.CacheWrite != nil {
		t.Fatalf("CacheWrite = %v, want nil (source JSON never had this field — missing must not become 0.0)", *r.CacheWrite)
	}
	if r.InFresh == nil || *r.InFresh != 0.28 {
		t.Fatalf("InFresh = %v, want 0.28 (2.8e-07 * 1e6)", r.InFresh)
	}
}

func TestGenerateRows_ExplicitZeroPreserved(t *testing.T) {
	raw := rawEntries(t, map[string]any{
		"some-model": map[string]any{
			"litellm_provider": "openai", "mode": "chat",
			"input_cost_per_token": 1e-06, "output_cost_per_token": 4e-06,
			"cache_read_input_token_cost":     0.0, // explicit free, not absent
			"cache_creation_input_token_cost": 0.0,
		},
	})
	rows, _, _ := generateRows(raw)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.CacheRead == nil || *r.CacheRead != 0 {
		t.Fatalf("CacheRead = %v, want a non-nil pointer to 0.0 (explicit zero, distinguishable from absent)", r.CacheRead)
	}
}

func TestGenerateRows_DedupFirstSeenWins(t *testing.T) {
	// Both entries resolve to the same canonical key ("openai/gpt-4o") —
	// sorted key order makes "gpt-4o" (alphabetically first) win over
	// "openai/gpt-4o".
	raw := rawEntries(t, map[string]any{
		"gpt-4o": map[string]any{
			"litellm_provider": "openai", "mode": "chat",
			"input_cost_per_token": 1.0e-06, "output_cost_per_token": 2.0e-06,
		},
		"openai/gpt-4o": map[string]any{
			"litellm_provider": "openai", "mode": "chat",
			"input_cost_per_token": 9.0e-06, "output_cost_per_token": 9.0e-06,
		},
	})
	rows, kept, _ := generateRows(raw)
	if kept != 1 || len(rows) != 1 {
		t.Fatalf("kept=%d rows=%v, want exactly 1 deduplicated row", kept, rows)
	}
	if *rows[0].InFresh != 1.0 {
		t.Fatalf("InFresh = %v, want 1.0 (the alphabetically-first \"gpt-4o\" entry, first-seen-wins)", *rows[0].InFresh)
	}
}

func TestGenerateRows_BasenameStripsExistingPrefix(t *testing.T) {
	raw := rawEntries(t, map[string]any{
		"vercel_ai_gateway/anthropic/claude-3-5-sonnet": map[string]any{
			"litellm_provider": "openrouter", "mode": "chat",
			"input_cost_per_token": 1e-06, "output_cost_per_token": 2e-06,
		},
	})
	rows, _, _ := generateRows(raw)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	// basename = the segment after the LAST "/" — "claude-3-5-sonnet", not
	// the whole "vercel_ai_gateway/anthropic/claude-3-5-sonnet" path.
	if rows[0].Key != "openrouter/claude-3-5-sonnet" {
		t.Fatalf("Key = %q, want openrouter/claude-3-5-sonnet", rows[0].Key)
	}
}

func TestGenerateRows_DeterministicOutputOrder(t *testing.T) {
	raw := rawEntries(t, map[string]any{
		"z-model": map[string]any{"litellm_provider": "openai", "mode": "chat", "input_cost_per_token": 1e-06},
		"a-model": map[string]any{"litellm_provider": "openai", "mode": "chat", "input_cost_per_token": 1e-06},
	})
	rows, _, _ := generateRows(raw)
	if len(rows) != 2 || rows[0].Key != "openai/a-model" || rows[1].Key != "openai/z-model" {
		t.Fatalf("rows = %v, want sorted by canonical key", rows)
	}
}
