// Ver 2026-09-23 02:45, by Claude Opus 5.5

// Config loading entry points: bytes -> unknown-field probe -> AST parsed ->
// env-expanded -> decoded -> defaulted -> validated. Split out of config.go
// purely for that file's line budget (see internal/archtest).
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"vmr/internal/fmtutil"
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

var envRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func parse(raw []byte) (*Config, error) {
	// First pass: validate unknown fields directly against the raw document bytes
	// with KnownFields(true). This ensures line numbers in unknown-field error
	// messages point directly to the user's original file, not a re-marshaled AST.
	// Type errors (e.g. unexpanded ${VAR} into numeric fields) are ignored here
	// because environment expansion has not yet occurred.
	if err := checkUnknownFields(raw); err != nil {
		return nil, err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse yaml: %w%s", err, legacyPricingKeyHint(err))
	}

	empty := map[string]bool{}
	expandNodeEnv(&root, empty)

	var cfg Config
	if err := root.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w%s", err, legacyPricingKeyHint(err))
	}
	if err := cfg.expandProviderAPIKeys(); err != nil {
		return nil, err
	}
	// Checked here, before applyDefaults folds <=0 into the default: that
	// fold is meant for "unset" (0), not "negative" — a negative value is a
	// config mistake and should fail loudly like max_attempts/max_concurrency
	// do, not get silently reinterpreted as "use the default".
	if cfg.MaxRequestBodyMB < 0 {
		return nil, fmt.Errorf("max_request_body_mb must be >= 0 (got %d; 0 = use default)", cfg.MaxRequestBodyMB)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if len(empty) > 0 {
		cfg.EmptyEnvRefs = fmtutil.SortedKeys(empty)
	}
	return &cfg, nil
}

// checkUnknownFields runs a preliminary decode of raw YAML bytes with
// KnownFields(true) to detect unknown field errors with line numbers that
// match the user's original document.
func checkUnknownFields(raw []byte) error {
	var probe Config
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&probe); err != nil && err != io.EOF {
		var te *yaml.TypeError
		if errors.As(err, &te) {
			var unknown []string
			for _, msg := range te.Errors {
				if strings.Contains(msg, "not found") {
					unknown = append(unknown, msg)
				}
			}
			if len(unknown) > 0 {
				filtered := &yaml.TypeError{Errors: unknown}
				return fmt.Errorf("parse yaml: %w%s", filtered, legacyPricingKeyHint(filtered))
			}
			return nil
		}
		return fmt.Errorf("parse yaml: %w%s", err, legacyPricingKeyHint(err))
	}
	return nil
}

// expandNodeEnv recursively traverses the YAML AST and expands ${VAR} in
// scalar values only. Mapping keys are not expanded.
func expandNodeEnv(n *yaml.Node, empty map[string]bool) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.DocumentNode:
		for _, child := range n.Content {
			expandNodeEnv(child, empty)
		}
	case yaml.MappingNode:
		for i := 0; i < len(n.Content); i += 2 {
			// Mapping keys (n.Content[i]) are not replaced; only values are.
			if i+1 < len(n.Content) {
				expandNodeEnv(n.Content[i+1], empty)
			}
		}
	case yaml.SequenceNode:
		for _, item := range n.Content {
			expandNodeEnv(item, empty)
		}
	case yaml.ScalarNode:
		expandScalarEnv(n, empty)
	}
}

// expandScalarEnv replaces ${NAME} references within a scalar node's value.
// Only ${...} syntax is recognized; bare $ stays literal. Unset or empty vars
// expand to "" and are tracked in empty. When a substitution occurs, Tag is
// cleared so re-encoding allows scalar type inference to re-evaluate.
func expandScalarEnv(n *yaml.Node, empty map[string]bool) {
	if !strings.Contains(n.Value, "${") {
		return
	}
	var matched bool
	n.Value = envRe.ReplaceAllStringFunc(n.Value, func(m string) string {
		matched = true
		name := m[2 : len(m)-1]
		v, ok := os.LookupEnv(name)
		if !ok || v == "" {
			empty[name] = true
		}
		return v
	})
	if matched {
		n.Tag = ""
	}
}
