// Ver 2026-07-08 20:15, by Sonnet 5

// Package config loads, expands (${ENV}) and validates the YAML config.
// A config that fails validation is never installed — the caller keeps the
// previous one running.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"

	"vmr/internal/adapter"
)

const (
	DefaultMaxRequestBodyMB  = 8
	DefaultConnectTimeout    = 10 * time.Second
	DefaultHeaderTimeout     = 120 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultImageCacheTTLDays = 7 // downscaled-image cache entries unused this many days get evicted
)

// Provider has no protocol field: it lives under providers.<protocol>.<name>,
// so the outer map key IS the adapter type. This also lets the same short
// name (e.g. "openrouter") appear once per protocol group without collision —
// no more "_a" suffix hack for a provider's second protocol face.
type Provider struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
}

// EndpointConfig.Provider resolves within the enclosing model's own protocol
// group (models.<protocol>.<name>.endpoints[].provider -> providers.<protocol>.<provider>),
// so an endpoint can never reference a provider of the wrong protocol — that
// mistake has no syntax to express, rather than being caught by validation.
//
// Priority is optional and defaults to 0. Endpoints of equal priority (the
// common case: nobody sets it) keep their config-file order because Sort is
// stable — so listing endpoints in the order you want them tried is enough;
// there is no need to number them.
type EndpointConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	Priority int    `yaml:"priority"`
}

// ImageDownscaleMaxPx is a pointer so "unset" (inherit the global
// image_downscale) and "explicitly 0" (force-disable for this model, even
// if the global setting is on) are distinguishable — a plain int can't
// represent that distinction (§7 image downscale, priority: model > global).
type ModelConfig struct {
	Strategy            []string         `yaml:"strategy"`
	Endpoints           []EndpointConfig `yaml:"endpoints"`
	ImageDownscaleMaxPx *int             `yaml:"image_downscale"`
}

// Duration accepts Go duration strings ("90s", "2m") in YAML.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(v)
	return nil
}

func (d Duration) D() time.Duration { return time.Duration(d) }

type Timeouts struct {
	Connect        Duration `yaml:"connect"`
	ResponseHeader Duration `yaml:"response_header"`
	StreamIdle     Duration `yaml:"stream_idle"`
}

// Providers and Models are both keyed protocol -> name. The protocol key is
// validated against the adapter registry (same rule that used to apply to
// Provider.Type), so adding a new ingress protocol is still just "register
// an adapter" — no schema change here.
type Config struct {
	Listen      string `yaml:"listen"`
	APIKey      string `yaml:"api_key"`
	MaxAttempts int    `yaml:"max_attempts"` // 0 = unlimited: try every available endpoint once
	// MaxRequestBodyMB bounds the inbound client request body vmr will read
	// into memory (http.MaxBytesReader) — a stability cap, unrelated to
	// audit logging (the audit trail records every request in full,
	// whatever size vmr accepted).
	MaxRequestBodyMB    int                               `yaml:"max_request_body_mb"`
	MaxConcurrency      int                               `yaml:"max_concurrency"`      // 0 = unlimited; excess requests wait in memory
	ImageDownscaleMaxPx int                               `yaml:"image_downscale"`      // 0/absent = disabled; else longer-side px cap for inline request images (global default; a model's own setting takes priority, §7)
	ImageCacheTTLDays   int                               `yaml:"image_cache_ttl_days"` // downscaled-image cache entries unused this many days are evicted; <=0/absent defaults to DefaultImageCacheTTLDays
	AuditRetentionDays  int                               `yaml:"audit_retention_days"` // 0/absent = never delete audit files (compression to .zst on rotation happens regardless)
	Timeouts            Timeouts                          `yaml:"timeouts"`
	Providers           map[string]map[string]Provider    `yaml:"providers"`
	Models              map[string]map[string]ModelConfig `yaml:"models"`
}

// Load reads, expands, parses, defaults and validates the config file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

func Parse(raw []byte) (*Config, error) {
	expanded := expandEnv(string(raw))
	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

var envRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv replaces ${NAME} with the environment value. Only the ${...} form
// is recognized; a bare $ stays literal. Unset variables expand to "".
func expandEnv(s string) string {
	return envRe.ReplaceAllStringFunc(s, func(m string) string {
		return os.Getenv(m[2 : len(m)-1])
	})
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8800"
	}
	if c.MaxAttempts < 0 {
		c.MaxAttempts = 0
	}
	if c.MaxConcurrency < 0 {
		c.MaxConcurrency = 0
	}
	if c.ImageDownscaleMaxPx < 0 {
		c.ImageDownscaleMaxPx = 0
	}
	if c.ImageCacheTTLDays <= 0 {
		c.ImageCacheTTLDays = DefaultImageCacheTTLDays
	}
	if c.AuditRetentionDays < 0 {
		c.AuditRetentionDays = 0
	}
	if c.MaxRequestBodyMB <= 0 {
		c.MaxRequestBodyMB = DefaultMaxRequestBodyMB
	}
	if c.Timeouts.Connect <= 0 {
		c.Timeouts.Connect = Duration(DefaultConnectTimeout)
	}
	if c.Timeouts.ResponseHeader <= 0 {
		c.Timeouts.ResponseHeader = Duration(DefaultHeaderTimeout)
	}
	if c.Timeouts.StreamIdle <= 0 {
		c.Timeouts.StreamIdle = Duration(DefaultIdleTimeout)
	}
	for _, byName := range c.Models {
		for name, m := range byName {
			changed := false
			if len(m.Strategy) == 0 {
				m.Strategy = []string{"priority"}
				changed = true
			}
			if m.ImageDownscaleMaxPx != nil && *m.ImageDownscaleMaxPx < 0 {
				zero := 0
				m.ImageDownscaleMaxPx = &zero
				changed = true
			}
			if changed {
				byName[name] = m
			}
		}
	}
}

func (c *Config) validate() error {
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("invalid listen address %q: %w", c.Listen, err)
	}
	if countNested(c.Providers) == 0 {
		return fmt.Errorf("no providers defined")
	}
	if countNested(c.Models) == 0 {
		return fmt.Errorf("no models defined")
	}
	for protocol, byName := range c.Providers {
		if _, ok := adapter.Get(protocol); !ok {
			return fmt.Errorf("providers.%s: unknown adapter type (available: %v)", protocol, adapter.Names())
		}
		for name, p := range byName {
			u, err := url.Parse(p.BaseURL)
			if err != nil || u.Scheme == "" || u.Host == "" {
				return fmt.Errorf("provider %q: invalid base_url %q", name, p.BaseURL)
			}
		}
	}
	for protocol, byName := range c.Models {
		if _, ok := adapter.Get(protocol); !ok {
			return fmt.Errorf("models.%s: unknown adapter type (available: %v)", protocol, adapter.Names())
		}
		for name, m := range byName {
			if len(m.Endpoints) == 0 {
				return fmt.Errorf("model %q: no endpoints", name)
			}
			for i, ep := range m.Endpoints {
				if _, ok := c.Providers[protocol][ep.Provider]; !ok {
					return fmt.Errorf("model %q endpoint #%d: unknown provider %q in the %s protocol group", name, i+1, ep.Provider, protocol)
				}
				if ep.Model == "" {
					return fmt.Errorf("model %q endpoint #%d: missing model", name, i+1)
				}
			}
		}
	}
	return nil
}

func countNested[V any](m map[string]map[string]V) int {
	n := 0
	for _, byName := range m {
		n += len(byName)
	}
	return n
}

func (c *Config) MaxRequestBodyBytes() int64 { return int64(c.MaxRequestBodyMB) << 20 }
