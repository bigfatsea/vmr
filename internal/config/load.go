// Ver 2026-08-31, by Opus 5

// Config loading entry points: bytes -> expanded -> parsed -> defaulted ->
// validated. Split out of config.go purely for that file's line budget (see
// internal/archtest).
package config

import (
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load reads, expands, parses, defaults and validates the config file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(raw)
}

// Parse loads a config from bytes, with no file of its own. Load is the
// entry point real deployments use.
func Parse(raw []byte) (*Config, error) { return parse(raw) }

func parse(raw []byte) (*Config, error) {
	expanded, emptyRefs, err := expandEnv(string(raw))
	if err != nil {
		return nil, err
	}
	var cfg Config
	// KnownFields: a misspelled key (max_concurency, image_downscale_px, …)
	// must be a load error, not a silently ignored no-op the user believes
	// is in effect — the same fail-fast contract as the rest of validation.
	dec := yaml.NewDecoder(strings.NewReader(expanded))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && err != io.EOF { // io.EOF = empty file; validate reports "no providers" below
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if err := cfg.expandProviderAPIKeys(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.EmptyEnvRefs = emptyRefs
	return &cfg, nil
}
