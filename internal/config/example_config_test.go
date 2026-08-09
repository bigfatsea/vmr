// Ver 2026-08-09, by Sonnet 5
package config

import (
	"testing"

	_ "vmr/internal/adapter/anthropic"
	_ "vmr/internal/adapter/openai"
	_ "vmr/internal/adapter/openairesponses"
)

// TestLoad_RepoExampleConfig_Parses guards against a regression the P2 dev
// plan's §12 #8 already hit once: config.example.yaml (the repo's own
// canonical "here's a working config" reference, quoted throughout
// UserGuide.md/.zh.md) silently stopped passing `vmr check` — a
// `providers[].proxy: true` line left active while its matching
// `https_proxy` was commented out, a load-time validate() error — and
// nothing in `go test ./...` caught it; it was only found by a manual `vmr
// check` run during a later review. Provider api_key env vars are
// deliberately left unset here: Load succeeding doesn't require real
// credentials (config.validate() never requires Provider.APIKey to be
// non-empty — only cfg.Check()'s best-effort consistency scan flags a
// missing one, which is a separate, non-fatal signal from `vmr check`'s
// own "=== Failed ===" section, not exercised by this test).
func TestLoad_RepoExampleConfig_Parses(t *testing.T) {
	cfg, err := Load("../../config.example.yaml")
	if err != nil {
		t.Fatalf("config.example.yaml failed to load/validate: %v", err)
	}
	if len(cfg.Providers) == 0 {
		t.Fatal("config.example.yaml: no providers parsed")
	}
	if len(cfg.Models) == 0 {
		t.Fatal("config.example.yaml: no models parsed")
	}
}
