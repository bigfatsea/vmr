// Ver 2026-08-02, by Sonnet 5

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
	"vmr/internal/core"
	"vmr/internal/pricing"
	"vmr/internal/rundir"
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
	// little headroom — see docs/VirtualModelRouter_Design_v4_Core.md's
	// Sticky Model section. Endpoints backed by a longer-lived cache (e.g. DeepSeek's disk
	// cache, hours to days) should override it per-endpoint.
	DefaultStickyTTL = 10 * time.Minute
	// DefaultProbeTimeout bounds one background recovery-probe HTTP call
	// (see probe_timeout on Config). Deliberately far under
	// DefaultHeaderTimeout: the whole point of a probe is a fast, cheap
	// liveness check that never makes real traffic wait on it — if a
	// provider can't answer a one-line prompt within this window, it isn't
	// going to look "recovered" by waiting longer, so there's no reason to
	// borrow the same budget a real request gets.
	DefaultProbeTimeout = 15 * time.Second
	// minAPIKeyLen is the shortest an api_keys entry may be. It exists
	// solely so audit.KeyTag's trailing 8-character window can never be
	// the whole key — a short key would otherwise have its full secret
	// value written, in the clear, into every report and filename its tag
	// ends up in.
	minAPIKeyLen = 16
)

// Provider is a flat, protocol-agnostic account definition: one entry per
// upstream account, however many of the registered ingress protocols
// ("openai"/"anthropic"/"openai-responses"/...) it actually speaks. BaseURL
// is keyed by protocol; a provider must declare at least one, and may
// declare several when the same account speaks several surfaces (e.g.
// MiniMax speaks openai+anthropic, OpenRouter speaks all three) — api_key/
// proxy are shared across whichever protocol faces this account has, since
// they're properties of the
// account, not of a single protocol.
//
// Proxy opts this provider's upstream connections into http_proxy/
// https_proxy: true = proxied (the foreign-provider case — Anthropic/OpenAI/
// OpenRouter from behind the GFW); false/absent = direct (the default, and
// the domestic-provider case: MiniMax/DeepSeek/etc. are reachable directly
// and a proxy would only slow them down or break them). There is no global
// default to inherit and no environment fallback — proxies are explicit,
// per-provider config. true with no matching proxy URL configured is a
// validation error (a contradiction the config can state on its own). Note
// yaml.v3 is YAML 1.2: write true/false, not on/off.
type Provider struct {
	Name    string            `yaml:"name"`
	BaseURL map[string]string `yaml:"base_url"`
	APIKey  string            `yaml:"api_key"`
	Proxy   bool              `yaml:"proxy"`
	// Quota declares this account's usage-plan limit(s) for Quota-Aware
	// Routing (see docs/TokenPlan_Quota_Routing_Design_opus-5.md and
	// docs/TokenPlan_Quota_P1_DevPlan_opus-5.md for what this release
	// actually supports). nil = unmetered — no behavior change from before
	// this field existed. A pointer, not a value, so "absent" and "present
	// but empty" are distinguishable — the latter is a validation error
	// (quota: with no limits: is almost certainly a mistake), not a silent
	// no-op.
	Quota *QuotaConfig `yaml:"quota"`
	// Pricing declares this account's price differences from the standard
	// list price (P2.2) — required (and validated for completeness) when
	// Quota has a metric: cost limit; optional otherwise, in which case it
	// only sharpens vmr report's $ estimates. See ProviderPricingConfig's
	// doc comment (pricing.go).
	Pricing *ProviderPricingConfig `yaml:"pricing"`
}

// EndpointGroup is one try-order entry under a virtual model: a provider, a
// protocol face of it, and one or more upstream model names that all share
// this entry's routing metadata. Models is exhaustive: each name expands
// into its own independent *core.Endpoint (own health/failover state), in
// list order, sharing Capabilities/MaxContextTokens/RoleMap/StickyTTL/
// Priority — the shape that saves repeating those fields once per model when
// several models behind the same account are interchangeable candidates for
// one virtual model.
//
// Provider resolves by name against Config.Providers; Protocol picks which
// of that provider's declared BaseURL entries applies — an EndpointGroup
// referencing a provider that hasn't declared a base_url for Protocol is a
// validation error, not a silent mismatch.
//
// Priority is optional and defaults to 0. Endpoints of equal priority (the
// common case: nobody sets it) keep their config-file order because Sort is
// stable — so listing endpoints in the order you want them tried is enough;
// there is no need to number them.
type EndpointGroup struct {
	Protocol string   `yaml:"protocol"`
	Provider string   `yaml:"provider"`
	Models   []string `yaml:"models"`
	Priority int      `yaml:"priority"`

	// Capabilities and MaxContextTokens drive condition-based routing (see
	// docs/VirtualModelRouter_Design_v4_Core.md's Condition-based Routing
	// section). Both are optional and
	// default to "inherit the virtual model's own base value" (VirtualModel.
	// Capabilities/MaxContextTokens below), which itself defaults to
	// "unconstrained" — a config that sets neither the model-level nor the
	// endpoint-level field sees no behavior change from before these fields
	// existed.
	//
	// Capabilities here is *additive*: it lists capabilities this endpoint
	// supports on top of the model's own base list (e.g. the base already
	// says [text, tools]; a stronger backing model can add "image" here
	// instead of repeating [text, tools, image]) — the effective, exhaustive
	// set used for filtering is the union of the two. MaxContextTokens
	// instead *overrides* the model's base when set (a single number can't
	// be unioned): 0/absent inherits the base value as-is.
	Capabilities     []string `yaml:"capabilities"`
	MaxContextTokens int64    `yaml:"max_context_tokens"`

	// RoleMap rewrites message roles (e.g. {"developer":"system"}) for
	// requests sent through this entry alone — a provider account can back
	// several endpoint-groups (different virtual models, different upstream
	// models) with different role-rejection behavior per model family, so
	// this lives per entry rather than once per provider.
	RoleMap map[string]string `yaml:"role_map"`

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
// represent that distinction (priority: model > global).
//
// A VirtualModel is reachable from whichever ingress protocol(s) its own
// Endpoints declare — the same virtual model name can mix an openai-protocol
// entry and an anthropic-protocol entry in one place, each independently
// reachable only from its own protocol's ingress (POST /v1/chat/completions
// vs POST /v1/messages); see BuildSnapshot.
type VirtualModel struct {
	Strategy            []string        `yaml:"strategy"`
	Endpoints           []EndpointGroup `yaml:"endpoints"`
	ImageDownscaleMaxPx *int            `yaml:"image_downscale"`

	// Capabilities and MaxContextTokens are the *base* condition-routing
	// declaration shared by every endpoint under this virtual model —
	// declaring them once here instead of repeating the same
	// EndpointGroup.Capabilities/MaxContextTokens on each try-order entry is
	// the common case when several backing models are otherwise
	// interchangeable. Both default to "unconstrained" (empty/0) when
	// absent, same as before this field existed. An individual
	// EndpointGroup's own Capabilities is unioned on top of this base
	// (additive: what that specific endpoint supports beyond the group's
	// shared floor); its own MaxContextTokens overrides this base instead
	// when set (a scalar can't be unioned). See EndpointGroup's doc comment.
	Capabilities     []string `yaml:"capabilities"`
	MaxContextTokens int64    `yaml:"max_context_tokens"`

	// Sticky enables session-affinity routing for this virtual model (see
	// docs/VirtualModelRouter_Design_v4_Core.md's Sticky Model section). A *bool,
	// not bool: nil (field absent) defaults to true — the hashing cost is
	// negligible and multi-turn agent traffic is VMR's primary
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

// Providers is a flat list (protocol is per-provider data, not a grouping
// key — see Provider.BaseURL); Models is keyed by virtual-model name alone,
// with protocol carried per EndpointGroup instead (see VirtualModel). Every
// protocol value appearing anywhere (Provider.BaseURL's keys, EndpointGroup.
// Protocol) is validated against the adapter registry, so adding a new
// ingress protocol is still just "register an adapter" — no schema change
// here.
type Config struct {
	Listen string `yaml:"listen"`
	// APIKeys is the list of credentials vmr itself accepts (empty = auth
	// disabled). Each entry gets tagged in the audit trail via audit.KeyTag
	// (the key's own tail, not a separately configured name) so `vmr report`
	// can group a shared instance's traffic by caller after the fact — see
	// config.example.yaml for the naming convention. minAPIKeyLen guards
	// against a key short enough that its whole value becomes the tag.
	APIKeys     []string `yaml:"api_keys"`
	MaxAttempts int      `yaml:"max_attempts"` // 0 = unlimited: try every available endpoint once
	// ProbeTimeout bounds one background recovery probe of a half-open
	// endpoint (past its cooldown, but not yet confirmed recovered): a
	// small dedicated request fires in the background and real traffic
	// never touches the endpoint until that probe succeeds. Per-probe upper
	// bound; default DefaultProbeTimeout.
	ProbeTimeout Duration `yaml:"probe_timeout"`
	// MaxRequestBodyMB bounds the inbound client request body vmr will read
	// into memory (http.MaxBytesReader) — a stability cap, unrelated to
	// audit logging (the audit trail records every request in full,
	// whatever size vmr accepted).
	MaxRequestBodyMB int `yaml:"max_request_body_mb"`
	MaxConcurrency   int `yaml:"max_concurrency"` // 0 = unlimited; excess requests wait in memory
	// HTTPProxy/HTTPSProxy only declare the proxy server's URL, selected by
	// the provider base_url's scheme — they do NOT by themselves turn
	// proxying on for anyone. Whether a provider actually uses that URL is
	// decided entirely by that provider's own Provider.Proxy (default false:
	// direct — there is no global default to inherit; opt providers in one
	// at a time). These are the ONLY way vmr ever learns of a proxy: proxy
	// environment variables are deliberately ignored — an implicit knob that
	// silently changes where traffic flows is exactly the kind of surprise a
	// router shouldn't have. To feed a value from the environment, reference
	// it explicitly (https_proxy: ${HTTPS_PROXY}); ${VAR} expansion applies
	// like everywhere else in the file.
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
	ImageDownscaleMaxPx int    `yaml:"image_downscale"`      // 0/absent = disabled; else longer-side px cap for inline request images (global default; a model's own setting takes priority)
	ImageCacheTTLDays   int    `yaml:"image_cache_ttl_days"` // downscaled-image cache entries unused this many days are evicted; <=0/absent defaults to DefaultImageCacheTTLDays
	AuditRetentionDays  int    `yaml:"audit_retention_days"` // 0/absent = never delete audit files (compression to .zst on rotation happens regardless)
	// ExtraRedactHeaders names additional client request headers to mask in
	// the audit trail the same way the built-in credential list (see
	// audit.credentialHeaders) already masks Authorization/X-Api-Key/etc —
	// for a client's own custom auth header vmr's adapters don't know about,
	// which would otherwise sit in the audit file in cleartext. Matched
	// case-insensitively, same as the built-in list. Absent/empty (the
	// default) changes nothing.
	ExtraRedactHeaders []string `yaml:"extra_redact_headers"`
	// StickyTTL is the global default for how long a Sticky Model affinity
	// preference stays valid (see docs/VirtualModelRouter_Design_v4_Core.md's
	// Sticky Model section); <=0/absent defaults to DefaultStickyTTL. Per-endpoint
	// EndpointGroup.StickyTTL overrides this for endpoints whose upstream
	// cache lifetime differs (e.g. DeepSeek's disk cache).
	StickyTTL Duration                `yaml:"sticky_ttl"`
	Timeouts  Timeouts                `yaml:"timeouts"`
	Providers []Provider              `yaml:"providers"`
	Models    map[string]VirtualModel `yaml:"models"`
	// Pricing is the global pricing block (P2.2) — currency, exchange rate,
	// and an optional user supplement/standard-table override. See
	// PricingConfig's doc comment (pricing.go).
	Pricing *PricingConfig `yaml:"pricing"`

	// ResolvedPricing holds every metric:-cost provider+model's fully
	// resolved pricing.Resolve result, keyed by provider+"\x00"+model —
	// filled by resolvePricing() during validate(), read by
	// router.BuildSnapshot to attach core.Endpoint.PricingRate. Not a yaml
	// field: nil when no provider has a metric: cost limit (the common
	// case — no pricing resolution work was needed at all), non-nil
	// (possibly still empty) otherwise.
	ResolvedPricing map[string]*core.PricingSpec `yaml:"-"`

	// ProviderPricingPolicies holds one pricing.ProviderPolicy per provider
	// — its map/overrides if it declared a pricing: block, plus (for every
	// provider, block or not) the global currency and exchange-rate factor
	// — for `vmr report`'s broader best-effort resolution (see
	// PricingTable's doc comment). A superset of ResolvedPricing's coverage,
	// deliberately: report prices whatever providers an audit log names,
	// and a provider resolving standard-table prices with no conversion
	// factor would be reported in the wrong currency. Not a yaml field; nil
	// when nothing anywhere needed pricing resolved at all (no global
	// pricing: block, no provider pricing: block, no metric: cost Limit).
	ProviderPricingPolicies map[string]pricing.ProviderPolicy `yaml:"-"`

	// pricingTableCache is the merged standard(+supplement) table computed
	// once by resolvePricing() during validate() — PricingTable() returns
	// this instead of re-parsing the embedded YAML on every call. Unset
	// (nil) when resolvePricing() had no reason to build one (no pricing:
	// block anywhere, no metric: cost provider); PricingTable() computes a
	// fresh one on demand in that case.
	pricingTableCache *pricing.Table `yaml:"-"`
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
	for name, m := range c.Models {
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
			c.Models[name] = m
		}
	}
}

func (c *Config) validate() error {
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("invalid listen address %q: %w", c.Listen, err)
	}
	// quotaNow anchors every provider's unset `since` default (see
	// validateQuota/LimitConfig.validate) to the same instant — one
	// config.Parse call resolving every Limit's default anchor consistently,
	// rather than each one reading a slightly different time.Now().
	quotaNow := time.Now()
	// core.StickyBackstopTTL is the internal/sticky Registry's own memory-
	// eviction window — an entry idle longer than that is dropped from the
	// map regardless of what any endpoint's own StickyTTL says, so a
	// configured sticky_ttl above it would look accepted but silently stop
	// taking effect once a conversation goes quiet for longer than the
	// backstop (see core.StickyBackstopTTL's doc comment). Caught here, at
	// load time, instead of surfacing as "sticky mysteriously stopped
	// working" in production.
	if c.StickyTTL.D() > core.StickyBackstopTTL {
		return fmt.Errorf("sticky_ttl %s exceeds the internal memory-eviction backstop (%s): a sticky entry idle longer than the backstop is dropped regardless of this setting, so stickiness would silently stop working before %s elapses — keep sticky_ttl at or under %s",
			c.StickyTTL.D(), core.StickyBackstopTTL, c.StickyTTL.D(), core.StickyBackstopTTL)
	}
	for i, k := range c.APIKeys {
		if len(k) < minAPIKeyLen {
			return fmt.Errorf("api_keys[%d]: too short (min %d characters) — its tail becomes a report label (see audit.KeyTag), so short keys would expose the whole key", i, minAPIKeyLen)
		}
	}
	for i, h := range c.ExtraRedactHeaders {
		if strings.TrimSpace(h) == "" {
			return fmt.Errorf("extra_redact_headers[%d]: empty header name", i)
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
	if len(c.Providers) == 0 {
		return fmt.Errorf("no providers defined")
	}
	if len(c.Models) == 0 {
		return fmt.Errorf("no models defined")
	}
	seenProvider := map[string]bool{}
	for i, p := range c.Providers {
		if p.Name == "" {
			return fmt.Errorf("providers[%d]: missing name", i)
		}
		if seenProvider[p.Name] {
			return fmt.Errorf("providers[%d]: duplicate provider name %q", i, p.Name)
		}
		seenProvider[p.Name] = true
		if len(p.BaseURL) == 0 {
			return fmt.Errorf("provider %q: base_url: at least one protocol required", p.Name)
		}
		for protocol, raw := range p.BaseURL {
			if _, ok := adapter.Get(protocol); !ok {
				return fmt.Errorf("provider %q: base_url.%s: unknown adapter type (available: %v)", p.Name, protocol, adapter.Names())
			}
			u, err := url.Parse(raw)
			if err != nil || u.Scheme == "" || u.Host == "" {
				return fmt.Errorf("provider %q: invalid base_url.%s %q", p.Name, protocol, raw)
			}
			// proxy: true with nothing to follow is a contradiction the
			// config states entirely on its own (no environment involved),
			// so it is rejected here rather than warned about at startup.
			if p.Proxy {
				if mode, _ := c.ProxySpecFor(p, protocol); mode != ProxyURL {
					return fmt.Errorf("provider %q: proxy: true but no matching proxy is configured for %s base_urls (set https_proxy/http_proxy; ${VAR} expansion works)", p.Name, u.Scheme)
				}
			}
		}
		if err := validateQuota(p.Name, p.Quota, quotaNow); err != nil {
			return err
		}
	}
	// providerModels collects, per provider, every upstream model name any
	// virtual model's endpoint group actually configures it to serve —
	// resolvePricing (called after this loop) needs this to know which
	// models a metric: cost provider must have complete pricing for; no
	// other validation step needed this set before P2.2.
	providerModels := map[string]map[string]bool{}
	for name, m := range c.Models {
		if len(m.Endpoints) == 0 {
			return fmt.Errorf("model %q: no endpoints", name)
		}
		if m.MaxContextTokens < 0 {
			return fmt.Errorf("model %q: max_context_tokens must be >= 0", name)
		}
		for i, eg := range m.Endpoints {
			if _, ok := adapter.Get(eg.Protocol); !ok {
				return fmt.Errorf("model %q endpoint group #%d: unknown protocol %q (available: %v)", name, i+1, eg.Protocol, adapter.Names())
			}
			p, ok := c.ProviderByName(eg.Provider)
			if !ok {
				return fmt.Errorf("model %q endpoint group #%d: unknown provider %q", name, i+1, eg.Provider)
			}
			if _, ok := p.BaseURL[eg.Protocol]; !ok {
				return fmt.Errorf("model %q endpoint group #%d: provider %q has no base_url for protocol %q", name, i+1, eg.Provider, eg.Protocol)
			}
			if len(eg.Models) == 0 {
				return fmt.Errorf("model %q endpoint group #%d: models: at least one required", name, i+1)
			}
			for j, mn := range eg.Models {
				if mn == "" {
					return fmt.Errorf("model %q endpoint group #%d: models[%d]: empty", name, i+1, j)
				}
				if providerModels[eg.Provider] == nil {
					providerModels[eg.Provider] = map[string]bool{}
				}
				providerModels[eg.Provider][mn] = true
			}
			if eg.MaxContextTokens < 0 {
				return fmt.Errorf("model %q endpoint group #%d: max_context_tokens must be >= 0", name, i+1)
			}
			if eg.StickyTTL != nil {
				if eg.StickyTTL.D() <= 0 {
					return fmt.Errorf("model %q endpoint group #%d: sticky_ttl must be positive", name, i+1)
				}
				if eg.StickyTTL.D() > core.StickyBackstopTTL {
					return fmt.Errorf("model %q endpoint group #%d: sticky_ttl %s exceeds the internal memory-eviction backstop (%s): a sticky entry idle longer than the backstop is dropped regardless of this setting, so stickiness would silently stop working before %s elapses — keep sticky_ttl at or under %s",
						name, i+1, eg.StickyTTL.D(), core.StickyBackstopTTL, eg.StickyTTL.D(), core.StickyBackstopTTL)
				}
			}
		}
	}
	if err := c.resolvePricing(providerModels); err != nil {
		return err
	}
	return nil
}

// ProviderByName looks up a provider by its declared name. Providers is a
// short, human-sized list — a linear scan is simpler than maintaining a
// parallel index, and nothing on the request hot path calls this:
// BuildSnapshot resolves every reference once at startup/reload, and
// everything downstream reads the resolved core.Endpoint instead.
func (c *Config) ProviderByName(name string) (Provider, bool) {
	for _, p := range c.Providers {
		if p.Name == name {
			return p, true
		}
	}
	return Provider{}, false
}

func (c *Config) MaxRequestBodyBytes() int64 { return int64(c.MaxRequestBodyMB) << 20 }

// Proxy resolution modes returned by ProxySpecFor.
const (
	ProxyDirect = "direct" // no proxy applies (provider opted out, or none configured)
	ProxyURL    = "url"    // a global http_proxy/https_proxy from this config applies
)

// ProxySpecFor resolves which proxy applies to p's connections under
// protocol (p may declare a different-scheme base_url per protocol, so the
// scheme check needs to know which one): p's own Proxy switch decides
// everything — false (the default) means direct, no global fallback to
// inherit. Only when it's true does the base_url's scheme pick http_proxy
// or https_proxy; no configured URL for that scheme still means direct.
// There is no environment fallback — proxies are explicit config only
// (reference ${HTTPS_PROXY} in the yaml to opt into an env value). proxyURL
// is only non-empty for ProxyURL. The decision is static per
// provider+protocol — the router builds one shared http.Client per distinct
// resolution, not a per-request proxy callback.
func (c *Config) ProxySpecFor(p Provider, protocol string) (mode, proxyURL string) {
	if !p.Proxy {
		return ProxyDirect, ""
	}
	cfgProxy := c.HTTPSProxy
	if u, err := url.Parse(p.BaseURL[protocol]); err == nil && u.Scheme == "http" {
		cfgProxy = c.HTTPProxy
	}
	if cfgProxy != "" {
		return ProxyURL, cfgProxy
	}
	return ProxyDirect, ""
}
