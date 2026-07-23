// Ver 2026-07-24 10:00, by Sonnet 5

// Package config loads, expands (${ENV}) and validates the YAML config.
// A config that fails validation is never installed — the caller keeps the
// previous one running.
package config

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"vmr/internal/adapter"
	"vmr/internal/rundir"
	"vmr/internal/sticky"
)

const (
	DefaultMaxRequestBodyMB  = 8
	DefaultConnectTimeout    = 10 * time.Second
	DefaultHeaderTimeout     = 120 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultImageCacheTTLDays = 7 // downscaled-image cache entries unused this many days get evicted
	// DefaultStickyTTL is the global default for how long a Sticky Model
	// affinity preference stays valid, absent an explicit sticky_ttl.
	// Calibrated to the shortest common upstream prompt-cache lifetime
	// (Anthropic's 5-minute default, OpenAI's 5-10 minute window) with a
	// little headroom — see docs/vmr_condition_routing_and_sticky_model_sonnet-5.md
	// §2.4. Endpoints backed by a longer-lived cache (e.g. DeepSeek's disk
	// cache, hours to days) should override it per-endpoint.
	DefaultStickyTTL = 10 * time.Minute
	// DefaultProbeTimeout bounds one active-probe HTTP call (see ProbeMode).
	// Deliberately far under DefaultHeaderTimeout: the whole point of an
	// active probe is a fast, cheap liveness check that never makes real
	// traffic wait on it — if a provider can't answer a one-line prompt
	// within this window, it isn't going to look "recovered" by waiting
	// longer, so there's no reason to borrow the same budget a real request gets.
	DefaultProbeTimeout = 15 * time.Second
	// minAPIKeyLen is the shortest an api_keys entry may be. It exists
	// solely so audit.KeyTag's trailing 8-character window can never be
	// the whole key — a short key would otherwise have its full secret
	// value written, in the clear, into every report and filename its tag
	// ends up in.
	minAPIKeyLen = 16
)

// ProbeMode values (Config.ProbeMode). Unexported validity list lives next to
// validate() below.
const (
	ProbeModeActive  = "active"
	ProbeModePassive = "passive"
)

// Provider has no protocol field: it lives under providers.<protocol>.<name>,
// so the outer map key IS the adapter type. This also lets the same short
// name (e.g. "openrouter") appear once per protocol group without collision —
// no more "_a" suffix hack for a provider's second protocol face.
//
// Proxy is a tri-state switch over this provider's upstream connections:
// false = always direct, whatever the global proxy settings say (the
// domestic-provider case: MiniMax/DeepSeek are reachable directly and a
// proxy would only slow them down or break them); true or absent = follow
// the global http_proxy/https_proxy settings. There is no environment
// fallback anywhere — proxies are explicit config. The difference between
// true and absent: true with no matching global proxy is a validation
// error (a contradiction the config can state on its own), absent just
// means direct when nothing is configured. Note yaml.v3 is YAML 1.2:
// write true/false, not on/off.
type Provider struct {
	BaseURL string            `yaml:"base_url"`
	APIKey  string            `yaml:"api_key"`
	Proxy   *bool             `yaml:"proxy"`
	RoleMap map[string]string `yaml:"role_map"`
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

	// Capabilities and MaxContextTokens drive condition-based routing (see
	// docs/vmr_condition_routing_and_sticky_model_sonnet-5.md §1.1). Both
	// are optional and default to "unconstrained" when absent — a request
	// needing a capability or context size this endpoint doesn't declare
	// simply isn't filtered by that dimension, so configs written before
	// these fields existed see no behavior change. Capabilities is
	// exhaustive once set: list every capability the endpoint actually
	// supports, not just the ones you want VMR to check.
	Capabilities     []string `yaml:"capabilities"`
	MaxContextTokens int64    `yaml:"max_context_tokens"`

	// StickyTTL overrides the global sticky_ttl (below) for this endpoint
	// alone — cache lifetime is a property of the upstream provider, not of
	// the virtual model, so different endpoints behind the same virtual
	// model (e.g. a fast in-memory cache vs. DeepSeek's disk cache) can
	// each declare their own window. nil = inherit the global default.
	StickyTTL *Duration `yaml:"sticky_ttl"`
}

// ImageDownscaleMaxPx is a pointer so "unset" (inherit the global
// image_downscale) and "explicitly 0" (force-disable for this model, even
// if the global setting is on) are distinguishable — a plain int can't
// represent that distinction (§7 image downscale, priority: model > global).
type ModelConfig struct {
	Strategy            []string         `yaml:"strategy"`
	Endpoints           []EndpointConfig `yaml:"endpoints"`
	ImageDownscaleMaxPx *int             `yaml:"image_downscale"`

	// Sticky enables session-affinity routing for this virtual model (see
	// docs/vmr_condition_routing_and_sticky_model_sonnet-5.md §2). A *bool,
	// not bool: nil (field absent) defaults to true — the hashing cost is
	// negligible (§2.3) and multi-turn agent traffic is VMR's primary
	// audience, so stickiness should apply without the user having to
	// remember to opt in. Explicit false opts a genuinely one-shot virtual
	// model out.
	Sticky *bool `yaml:"sticky"`
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
// validated against the adapter registry, so adding a new ingress protocol
// is just "register an adapter" — no schema change here.
type Config struct {
	Listen string `yaml:"listen"`
	// RemovedAPIKey exists only to catch configs still using the removed
	// singular `api_key` field with a migration message instead of the
	// generic strict-decoding error. api_keys is the one auth surface now:
	// the untagged catch-all added nothing api_keys can't do, at the cost of
	// a second code path in authenticate and a second thing to document.
	RemovedAPIKey string `yaml:"api_key"`
	// APIKeys is the list of credentials vmr itself accepts (empty = auth
	// disabled). Each entry gets tagged in the audit trail via audit.KeyTag
	// (the key's own tail, not a separately configured name) so `vmr report`
	// can group a shared instance's traffic by caller after the fact — see
	// config.example.yaml for the naming convention. minAPIKeyLen guards
	// against a key short enough that its whole value becomes the tag.
	APIKeys     []string `yaml:"api_keys"`
	MaxAttempts int      `yaml:"max_attempts"` // 0 = unlimited: try every available endpoint once
	// ProbeMode selects how a half-open endpoint (past its cooldown, but not
	// yet confirmed recovered) gets re-verified: "active" (default) fires a
	// small dedicated probe request in the background and never lets real
	// traffic touch the endpoint until that probe succeeds; "passive" lets
	// the next real request BE the probe, so its own size/duration
	// determines how long the single-flight probe slot (and, under
	// concurrent load, every other request's access to this endpoint) stays
	// locked.
	ProbeMode    string   `yaml:"probe_mode"`
	ProbeTimeout Duration `yaml:"probe_timeout"` // active mode only: per-probe upper bound; default DefaultProbeTimeout
	// MaxRequestBodyMB bounds the inbound client request body vmr will read
	// into memory (http.MaxBytesReader) — a stability cap, unrelated to
	// audit logging (the audit trail records every request in full,
	// whatever size vmr accepted).
	MaxRequestBodyMB int `yaml:"max_request_body_mb"`
	MaxConcurrency   int `yaml:"max_concurrency"` // 0 = unlimited; excess requests wait in memory
	// HTTPProxy/HTTPSProxy are the global upstream proxy settings, selected
	// by the provider base_url's scheme. These are the ONLY way vmr ever
	// uses a proxy: proxy environment variables are deliberately ignored —
	// an implicit knob that silently changes where traffic flows is exactly
	// the kind of surprise a router shouldn't have. To feed a value from
	// the environment, reference it explicitly (https_proxy: ${HTTPS_PROXY});
	// ${VAR} expansion applies like everywhere else in the file. Unset =
	// every provider connects directly. Per-provider exclusion is
	// Provider.Proxy: false, not a second exclusion list.
	HTTPProxy  string `yaml:"http_proxy"`
	HTTPSProxy string `yaml:"https_proxy"`
	// LogDir is where audit JSONL files land; ImageCacheDir holds the
	// image-downscale result cache. Explicit values are used exactly as
	// given (a leading "~/" expands to the home directory; ${VAR} expansion
	// applies too). Unset → the persistent defaults ~/.vmr/logs and
	// ~/.vmr/image_cache (internal/rundir fallback chain). These were
	// VMR_LOG_DIR/VMR_IMG_CACHE_DIR environment variables once — moved into
	// the config for the same reason the proxy settings are config-only:
	// nothing about where vmr writes should depend on implicit environment
	// state. Note: a log_dir change needs a restart (the audit logger opens
	// its directory once at startup); image_cache_dir follows hot reloads.
	LogDir              string `yaml:"log_dir"`
	ImageCacheDir       string `yaml:"image_cache_dir"`
	ImageDownscaleMaxPx int    `yaml:"image_downscale"`      // 0/absent = disabled; else longer-side px cap for inline request images (global default; a model's own setting takes priority, §7)
	ImageCacheTTLDays   int    `yaml:"image_cache_ttl_days"` // downscaled-image cache entries unused this many days are evicted; <=0/absent defaults to DefaultImageCacheTTLDays
	AuditRetentionDays  int    `yaml:"audit_retention_days"` // 0/absent = never delete audit files (compression to .zst on rotation happens regardless)
	// StickyTTL is the global default for how long a Sticky Model affinity
	// preference stays valid (see docs/vmr_condition_routing_and_sticky_model_sonnet-5.md
	// §2.4); <=0/absent defaults to DefaultStickyTTL. Per-endpoint
	// EndpointConfig.StickyTTL overrides this for endpoints whose upstream
	// cache lifetime differs (e.g. DeepSeek's disk cache).
	StickyTTL Duration                          `yaml:"sticky_ttl"`
	Timeouts  Timeouts                          `yaml:"timeouts"`
	Providers map[string]map[string]Provider    `yaml:"providers"`
	Models    map[string]map[string]ModelConfig `yaml:"models"`
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
	// KnownFields: a misspelled key (max_concurency, image_downscale_px, …)
	// must be a load error, not a silently ignored no-op the user believes
	// is in effect — the same fail-fast contract as the rest of validation.
	dec := yaml.NewDecoder(strings.NewReader(expanded))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && err != io.EOF { // io.EOF = empty file; validate reports "no providers" below
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

// expandTilde resolves a leading "~/" (or a bare "~") to the user's home
// directory — the spelling everyone reaches for in a path field. Anything
// else, including "~user" forms, is returned untouched; if the home
// directory cannot be resolved the value stays literal rather than being
// silently rewritten.
func expandTilde(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8800"
	}
	if c.MaxAttempts < 0 {
		c.MaxAttempts = 0
	}
	if c.ProbeMode == "" {
		c.ProbeMode = ProbeModeActive
	}
	if c.ProbeTimeout <= 0 {
		c.ProbeTimeout = Duration(DefaultProbeTimeout)
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
	if c.StickyTTL.D() <= 0 {
		c.StickyTTL = Duration(DefaultStickyTTL)
	}
	if c.MaxRequestBodyMB <= 0 {
		c.MaxRequestBodyMB = DefaultMaxRequestBodyMB
	}
	c.LogDir = expandTilde(c.LogDir)
	if c.LogDir == "" {
		c.LogDir = rundir.Resolve("logs", "vmr_logs", "logs")
	}
	c.ImageCacheDir = expandTilde(c.ImageCacheDir)
	if c.ImageCacheDir == "" {
		c.ImageCacheDir = rundir.Resolve("image_cache", "vmr_image_cache", "image_cache")
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
	if c.RemovedAPIKey != "" {
		return fmt.Errorf("api_key has been removed: move the credential into the api_keys list instead (each entry must be >= %d characters — its tail becomes the caller tag in vmr report)", minAPIKeyLen)
	}
	if c.ProbeMode != ProbeModeActive && c.ProbeMode != ProbeModePassive {
		return fmt.Errorf("probe_mode %q: must be %q or %q", c.ProbeMode, ProbeModeActive, ProbeModePassive)
	}
	// sticky.BackstopTTL is the internal/sticky Registry's own memory-eviction
	// window — an entry idle longer than that is dropped from the map
	// regardless of what any endpoint's own StickyTTL says, so a configured
	// sticky_ttl above it would look accepted but silently stop taking effect
	// once a conversation goes quiet for longer than the backstop (see
	// sticky.BackstopTTL's doc comment). Caught here, at load time, instead
	// of surfacing as "sticky mysteriously stopped working" in production.
	if c.StickyTTL.D() > sticky.BackstopTTL {
		return fmt.Errorf("sticky_ttl %s exceeds the internal memory-eviction backstop (%s): a sticky entry idle longer than the backstop is dropped regardless of this setting, so stickiness would silently stop working before %s elapses — keep sticky_ttl at or under %s",
			c.StickyTTL.D(), sticky.BackstopTTL, c.StickyTTL.D(), sticky.BackstopTTL)
	}
	for i, k := range c.APIKeys {
		if len(k) < minAPIKeyLen {
			return fmt.Errorf("api_keys[%d]: too short (min %d characters) — its tail becomes a report label (see audit.KeyTag), so short keys would expose the whole key", i, minAPIKeyLen)
		}
	}
	// A slice, not a map: map iteration order is randomized, and a name+value
	// pair here is only ever checked for "is this one URL valid" — nothing
	// depends on the order between http_proxy and https_proxy, but the error
	// message when validation fails should still be deterministic.
	for _, proxy := range [...]struct{ name, val string }{
		{"http_proxy", c.HTTPProxy},
		{"https_proxy", c.HTTPSProxy},
	} {
		if proxy.val == "" {
			continue
		}
		u, err := url.Parse(proxy.val)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("invalid %s %q (want e.g. http://127.0.0.1:7890)", proxy.name, proxy.val)
		}
	}
	if CountNested(c.Providers) == 0 {
		return fmt.Errorf("no providers defined")
	}
	if CountNested(c.Models) == 0 {
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
			// proxy: true with nothing to follow is a contradiction the
			// config states entirely on its own (no environment involved),
			// so it is rejected here rather than warned about at startup.
			if p.Proxy != nil && *p.Proxy {
				if mode, _ := c.ProxySpecFor(p); mode != ProxyURL {
					return fmt.Errorf("provider %q: proxy: true but no global proxy is configured for %s base_urls (set https_proxy/http_proxy; ${VAR} expansion works)", name, u.Scheme)
				}
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
				if ep.MaxContextTokens < 0 {
					return fmt.Errorf("model %q endpoint #%d: max_context_tokens must be >= 0", name, i+1)
				}
				if ep.StickyTTL != nil {
					if ep.StickyTTL.D() <= 0 {
						return fmt.Errorf("model %q endpoint #%d: sticky_ttl must be positive", name, i+1)
					}
					if ep.StickyTTL.D() > sticky.BackstopTTL {
						return fmt.Errorf("model %q endpoint #%d: sticky_ttl %s exceeds the internal memory-eviction backstop (%s): a sticky entry idle longer than the backstop is dropped regardless of this setting, so stickiness would silently stop working before %s elapses — keep sticky_ttl at or under %s",
							name, i+1, ep.StickyTTL.D(), sticky.BackstopTTL, ep.StickyTTL.D(), sticky.BackstopTTL)
					}
				}
			}
		}
	}
	return nil
}

// CountNested totals the inner maps of a protocol -> name -> V structure
// (Config.Providers, Config.Models) — exported because validate() isn't the
// only place that needs "how many providers/models total": diagnose and
// cmd/vmr both report the same count in their own output.
func CountNested[V any](m map[string]map[string]V) int {
	n := 0
	for _, byName := range m {
		n += len(byName)
	}
	return n
}

func (c *Config) MaxRequestBodyBytes() int64 { return int64(c.MaxRequestBodyMB) << 20 }

// Proxy resolution modes returned by ProxySpecFor.
const (
	ProxyDirect = "direct" // no proxy applies (provider opted out, or none configured)
	ProxyURL    = "url"    // a global http_proxy/https_proxy from this config applies
)

// ProxySpecFor resolves which proxy applies to p's upstream connections:
// the provider's own proxy: false wins (always direct); otherwise the
// global config proxy matching the base_url's scheme, when set; otherwise
// direct. There is no environment fallback — proxies are explicit config
// only (reference ${HTTPS_PROXY} in the yaml to opt into an env value).
// proxyURL is only non-empty for ProxyURL. The decision is static per
// provider — the router builds one shared http.Client per distinct
// resolution, not a per-request proxy callback.
func (c *Config) ProxySpecFor(p Provider) (mode, proxyURL string) {
	if p.Proxy != nil && !*p.Proxy {
		return ProxyDirect, ""
	}
	cfgProxy := c.HTTPSProxy
	if u, err := url.Parse(p.BaseURL); err == nil && u.Scheme == "http" {
		cfgProxy = c.HTTPProxy
	}
	if cfgProxy != "" {
		return ProxyURL, cfgProxy
	}
	return ProxyDirect, ""
}
