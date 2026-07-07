// Ver 2026-07-07 02:00, by Fable 5

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
	DefaultMaxBodyMB      = 8
	DefaultConnectTimeout = 10 * time.Second
	DefaultHeaderTimeout  = 120 * time.Second
	DefaultIdleTimeout    = 120 * time.Second
)

type Provider struct {
	Type    string `yaml:"type"`
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
}

type EndpointConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	Priority int    `yaml:"priority"`
}

type ModelConfig struct {
	Strategy  []string         `yaml:"strategy"`
	Endpoints []EndpointConfig `yaml:"endpoints"`
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

type Config struct {
	Listen         string                 `yaml:"listen"`
	APIKey         string                 `yaml:"api_key"`
	MaxAttempts    int                    `yaml:"max_attempts"` // 0 = unlimited: try every available endpoint once
	MaxBodyMB      int                    `yaml:"max_body_mb"`
	MaxConcurrency int                    `yaml:"max_concurrency"` // 0 = unlimited; excess requests wait in memory
	Timeouts       Timeouts               `yaml:"timeouts"`
	Providers      map[string]Provider    `yaml:"providers"`
	Models         map[string]ModelConfig `yaml:"models"`
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
	if c.MaxBodyMB <= 0 {
		c.MaxBodyMB = DefaultMaxBodyMB
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
	for name, m := range c.Models {
		if len(m.Strategy) == 0 {
			m.Strategy = []string{"priority"}
			c.Models[name] = m
		}
	}
}

func (c *Config) validate() error {
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("invalid listen address %q: %w", c.Listen, err)
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("no providers defined")
	}
	if len(c.Models) == 0 {
		return fmt.Errorf("no models defined")
	}
	for name, p := range c.Providers {
		if _, ok := adapter.Get(p.Type); !ok {
			return fmt.Errorf("provider %q: unknown adapter type %q (available: %v)", name, p.Type, adapter.Names())
		}
		u, err := url.Parse(p.BaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("provider %q: invalid base_url %q", name, p.BaseURL)
		}
	}
	for name, m := range c.Models {
		if len(m.Endpoints) == 0 {
			return fmt.Errorf("model %q: no endpoints", name)
		}
		for i, ep := range m.Endpoints {
			if _, ok := c.Providers[ep.Provider]; !ok {
				return fmt.Errorf("model %q endpoint #%d: unknown provider %q", name, i+1, ep.Provider)
			}
			if ep.Model == "" {
				return fmt.Errorf("model %q endpoint #%d: missing model", name, i+1)
			}
		}
	}
	return nil
}

func (c *Config) MaxBodyBytes() int64 { return int64(c.MaxBodyMB) << 20 }
