// Ver 2026-08-02, by Sonnet 5

// Package config loads, expands (${ENV}) and validates the YAML config.
// A config that fails validation is never installed — the caller keeps the
// previous one running.
package config

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"vmr/internal/core"
	"vmr/internal/fmtutil"
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
	// affinity preference stays valid, absent an explicit ttl.sticky.
	// Calibrated to the shortest common upstream prompt-cache lifetime
	// (Anthropic's 5-minute default, OpenAI's 5-10 minute window) with a
	// little headroom — see docs/VirtualModelRouter_Design_v4_Core.md's
	// Sticky Model section. Endpoints backed by a longer-lived cache (e.g. DeepSeek's disk
	// cache, hours to days) should override it per-endpoint.
	DefaultStickyTTL = 10 * time.Minute
	// DefaultProbeTimeout bounds one background recovery-probe HTTP call
	// (see timeouts.probe). Deliberately far under
	// DefaultHeaderTimeout: the whole point of a probe is a fast, cheap
	// liveness check that never makes real traffic wait on it — if a
	// provider can't answer a one-line prompt within this window, it isn't
	// going to look "recovered" by waiting longer, so there's no reason to
	// borrow the same budget a real request gets.
	DefaultProbeTimeout = 15 * time.Second
	// DefaultAuditRetentionDays is how long audit JSONL files are kept before
	// deletion, absent an explicit ttl.audit_retention. Deliberately finite:
	// the pre-TTL config let 0 mean "never delete", which silently reverted
	// disk-full risk onto every config that left the field unset — a default
	// has to actually default (see TTL.AuditRetention for the migration note).
	DefaultAuditRetentionDays = 90
	// minAPIKeyLen is the shortest an api_keys entry may be. It exists
	// solely so audit.KeyTag's trailing 8-character window can never be
	// the whole key — a short key would otherwise have its full secret
	// value written, in the clear, into every report and filename its tag
	// ends up in.
	minAPIKeyLen = 16
)

// EndpointGroup is one try-order entry under a virtual model: one or more
// providers and one or more upstream model names, all sharing this entry's
// routing metadata. Each (provider, model) pair expands into its own
// independent *core.Endpoint — outer loop over Models, inner loop over
// Providers, so every provider is tried for the preferred model before
// falling through to the next.
//
// An EndpointGroup carries no protocol of its own: it always lives under a
// protocol key of VirtualModel.Endpoints or Config.FallbackEndpoints — the
// same protocol-keyed shape as Provider.BaseURL, so both halves of the
// config read the same way (and no protocol: value can ever disagree with
// the bucket it sits in). The key picks which of each named provider's
// declared BaseURL entries applies — a provider with no base_url for the
// key's protocol is a validation error, not a silent mismatch.
//
// Priority defaults to 0; equal-priority entries keep config-file order
// (Sort is stable). Config.FallbackEndpoints entries are the exception:
// Priority there must be set and positive.
type EndpointGroup struct {
	Providers []string `yaml:"providers"`
	Models    []string `yaml:"models"`
	Priority  int      `yaml:"priority"`

	// RoleMap rewrites message roles (e.g. {"developer":"system"}) for
	// requests sent through this entry alone — a provider account can back
	// several endpoint-groups (different virtual models, different upstream
	// models) with different role-rejection behavior per model family, so
	// this lives per entry rather than once per provider.
	RoleMap map[string]string `yaml:"role_map"`

	// StickyTTL overrides the global ttl.sticky (below) for this endpoint
	// alone — cache lifetime is a property of the upstream provider, not of
	// the virtual model, so different endpoints behind the same virtual
	// model (e.g. a fast in-memory cache vs. DeepSeek's disk cache) can
	// each declare their own window. nil = inherit the global default.
	StickyTTL *Duration `yaml:"sticky_ttl"`

	// SoftBlockFailover, when true, lets a 2xx response that carries a
	// vendor content-policy flag but no real answer (MiniMax's
	// input_sensitive/output_sensitive) fail over to the next candidate,
	// exactly like an ErrContent 4xx would. nil inherits
	// VirtualModel.SoftBlockFailover; an explicit value overrides it (so a
	// model can turn it on globally and one endpoint can still opt out).
	// Off by default — the failover only triggers on marker + empty/near-
	// empty content, but that judgement is still a heuristic on a committed
	// 2xx, so it stays opt-in.
	SoftBlockFailover *bool `yaml:"soft_block_failover"`
}

// ImageDownscaleMaxPx is a pointer so "unset" (inherit the global
// image_downscale) and "explicitly 0" (force-disable for this model, even
// if the global setting is on) are distinguishable — a plain int can't
// represent that distinction (priority: model > global).
//
// A VirtualModel is reachable from whichever ingress protocol(s) its own
// Endpoints declare — the same virtual model name can mix an openai-completions
// bucket and an anthropic-messages bucket in one place, each independently
// reachable only from its own protocol's ingress (POST /v1/chat/completions
// vs POST /v1/messages); see BuildSnapshot.
type VirtualModel struct {
	Strategy []string `yaml:"strategy"`
	// Endpoints is keyed by ingress protocol (same key space as
	// Provider.BaseURL, validated against the adapter registry), each holding
	// that protocol's try-order groups in config order. Keyed by protocol —
	// not a flat list with a per-entry protocol field — because a request
	// only ever consults the bucket of its own ingress (BuildSnapshot splits
	// the buckets into independent routes), so bucket order is irrelevant
	// while the order within a bucket is the try-order.
	Endpoints           map[string][]EndpointGroup `yaml:"endpoints"`
	ImageDownscaleMaxPx *int                       `yaml:"image_downscale"`

	// Capabilities and MaxContextTokens are optional explicit overrides for
	// this virtual model, taking precedence over model_defaults (see
	// ModelDefaultEntry). Both default to "unconstrained" (empty/0) when
	// absent from both here and model_defaults. These override model_defaults
	// per dimension independently: an explicit setting here replaces that
	// field's value for every endpoint under this model (e.g. allowing a
	// "cheap" virtual model to downgrade a large backing model's window).
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

	// Fallback opts this virtual model into Config.FallbackEndpoints; nil
	// defaults to true (same polarity as Sticky above). Explicit false opts
	// out entirely — no partial opt-out of individual entries.
	Fallback *bool `yaml:"fallback"`

	// SoftBlockFailover sets the default for every endpoint under this
	// model (see EndpointGroup.SoftBlockFailover). nil/false = off; an
	// endpoint's own explicit value still wins over this.
	SoftBlockFailover *bool `yaml:"soft_block_failover"`
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

// CalendarDuration is a lifecycle-scale duration for the ttl.* fields, in
// calendar units Go's time.ParseDuration has no concept of: Nd (day), Nw
// (week = 7d), Nmo (month = 30d), Ny (year = 365d), case-insensitive. A bare
// integer means days (the old *_days int fields' spelling, so their numbers
// migrate unchanged).
//
// Zero-value polarity, deliberately uniform across every ttl.* field: 0
// (incl. 0d), absent, and negative all mean "use the default" — applyDefaults
// fills any value <= 0. There is no third "unlimited" reading and no pointer
// tri-state. Permanent-retention semantics were removed outright: forever,
// permanent, and never are load errors — a config that genuinely wants
// retention beyond any horizon writes a concrete large number (90000d), so
// every value in the file stays a duration someone can reason about.
//
// Month/year are fixed-length approximations (30d/365d), not calendar math —
// the same convention quota's every: 1mo uses.
type CalendarDuration time.Duration

var calendarPattern = regexp.MustCompile(`^([+-]?[0-9]+)(d|w|mo|y)?$`)

func (d *CalendarDuration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	v, err := parseCalendarDuration(s)
	if err != nil {
		return err
	}
	*d = CalendarDuration(v)
	return nil
}

func parseCalendarDuration(s string) (time.Duration, error) {
	switch strings.ToLower(s) {
	case "forever", "permanent", "never":
		return 0, fmt.Errorf("invalid duration %q: permanent retention is not supported — 0/absent already means \"use the default\"; for very long retention write a concrete large value (e.g. 90000d ≈ 246 years)", s)
	}
	m := calendarPattern.FindStringSubmatch(strings.ToLower(s))
	if m == nil {
		return 0, fmt.Errorf("invalid duration %q (want a number followed by d/w/mo/y, e.g. \"14d\", \"3mo\", \"2w\"; case-insensitive, bare number = days)", s)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	var unit time.Duration
	switch strings.ToLower(m[2]) {
	case "w":
		unit = 7 * 24 * time.Hour
	case "mo":
		unit = 30 * 24 * time.Hour
	case "y":
		unit = 365 * 24 * time.Hour
	default: // "" (bare number) and "d" are both days
		unit = 24 * time.Hour
	}
	return time.Duration(n) * unit, nil
}

func (d CalendarDuration) D() time.Duration { return time.Duration(d) }

// String renders the value the way it's configured: whole days as "Nd",
// anything else as Go's duration spelling, so a sub-day value never reads as
// the misleading "0d".
func (d CalendarDuration) String() string {
	day := CalendarDuration(24 * time.Hour)
	if d%day == 0 {
		return fmt.Sprintf("%dd", int64(d/day))
	}
	return time.Duration(d).String()
}

// Days reports d in whole days, rounding up so a sub-day value is at least
// one day — the day-granular downstream consumers (audit.SetRetentionDays,
// imgprep's CacheTTLDays) both keep a legacy "0 = never delete / disabled"
// reading, and a sub-day config that truncated to 0 would silently land in
// exactly that. Only meaningful after applyDefaults (a value <= 0 becomes the
// default there, so callers never see it).
func (d CalendarDuration) Days() int {
	return int(math.Ceil(time.Duration(d).Hours() / 24))
}

type Timeouts struct {
	Connect        Duration `yaml:"connect"`
	ResponseHeader Duration `yaml:"response_header"`
	StreamIdle     Duration `yaml:"stream_idle"`
	// Probe bounds one background recovery probe of a half-open endpoint
	// (past its cooldown, but not yet confirmed recovered): a small
	// dedicated request fires in the background and real traffic never
	// touches the endpoint until that probe succeeds. Per-probe upper
	// bound; default DefaultProbeTimeout.
	Probe Duration `yaml:"probe"`
}

// TTL gathers the lifecycle fields — how long state lives before eviction —
// as opposed to Timeouts' "how long one wait may take". All three share the
// same zero-value polarity: 0/absent/negative = use the default (see
// CalendarDuration; Sticky is a plain Go-grammar Duration because it's
// minutes/hours scale, same polarity). No field here has a "permanent"
// reading — that semantics was removed.
type TTL struct {
	// Sticky is the global default for how long a Sticky Model affinity
	// preference stays valid (see docs/VirtualModelRouter_Design_v4_Core.md's
	// Sticky Model section); <=0/absent defaults to DefaultStickyTTL. Per-endpoint
	// EndpointGroup.StickyTTL overrides this for endpoints whose upstream
	// cache lifetime differs (e.g. DeepSeek's disk cache).
	Sticky Duration `yaml:"sticky"`
	// ImageCache is the downscale-cache eviction age (downscaled-image cache
	// entries unused this long are evicted); <=0/absent defaults to
	// DefaultImageCacheTTLDays days.
	ImageCache CalendarDuration `yaml:"image_cache"`
	// AuditRetention is how long audit JSONL files are kept before deletion
	// (compression to .zst on rotation happens regardless). <=0/absent
	// defaults to DefaultAuditRetentionDays. Breaking vs the old
	// audit_retention_days field: 0 there meant "never delete" — that
	// reading is gone; configs relying on it must name a concrete value
	// (90000d).
	AuditRetention CalendarDuration `yaml:"audit_retention"`
}

// ModelDefaultEntry declares capabilities and context ceiling for a real
// upstream model across all virtual models that route to it. Keyed by real
// model name under Config.ModelDefaults ("*" for fallback).
//
// Key semantics:
//   - Capabilities and MaxContextTokens fall back independently per
//     dimension; they are not bound as an all-or-nothing entry.
//   - "Unstated = unconstrained" is an architectural invariant: omitting
//     model_defaults entirely, or having no matching entry for a model,
//     leaves it unconstrained (empty capabilities / 0 max_context_tokens).
//   - Providers restricts this entry to the named provider accounts. When
//     empty or omitted, the declaration applies to all providers.
//   - Exact model key takes precedence over the "*" wildcard (fallback chain,
//     not a union/merge).
type ModelDefaultEntry struct {
	Providers        []string `yaml:"providers"`
	Capabilities     []string `yaml:"capabilities"`
	MaxContextTokens int64    `yaml:"max_context_tokens"`
}

// Providers is a flat list (protocol is per-provider data, not a grouping
// key — see Provider.BaseURL); Models is keyed by virtual-model name alone,
// with protocol as the Endpoints/FallbackEndpoints map key instead (see
// VirtualModel). Every protocol key appearing anywhere (Provider.BaseURL,
// VirtualModel.Endpoints, FallbackEndpoints) is validated against the
// adapter registry, so adding a new ingress protocol is still just
// "register an adapter" — no schema change here.
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
	ImageDownscaleMaxPx int    `yaml:"image_downscale"` // 0/absent = disabled; else longer-side px cap for inline request images (global default; a model's own setting takes priority)
	// ExtraRedactHeaders names additional client request headers to mask in
	// the audit trail the same way the built-in credential list (see
	// audit.credentialHeaders) already masks Authorization/X-Api-Key/etc —
	// for a client's own custom auth header vmr's adapters don't know about,
	// which would otherwise sit in the audit file in cleartext. Matched
	// case-insensitively, same as the built-in list. Absent/empty (the
	// default) changes nothing.
	ExtraRedactHeaders []string `yaml:"extra_redact_headers"`
	Timeouts           Timeouts `yaml:"timeouts"`
	// TTL holds the lifecycle fields (sticky affinity window, image-cache
	// eviction, audit retention) — see its doc comment for the shared
	// zero-value polarity (0/absent/negative = use the default).
	TTL           TTL                          `yaml:"ttl"`
	ModelDefaults map[string]ModelDefaultEntry `yaml:"model_defaults"`
	Providers     []Provider                   `yaml:"providers"`
	Models        map[string]VirtualModel      `yaml:"models"`
	// FallbackEndpoints is appended to the tail of every virtual model's
	// try-order — declare a shared catch-all tier once instead of pasting
	// it onto every VirtualModel.Endpoints bucket. Keyed by protocol like
	// VirtualModel.Endpoints (same EndpointGroup shape; no compatible-layer
	// between the two forms). router.BuildSnapshot only attaches a bucket's
	// entries to a model that already has an entry point on that protocol
	// (augments an existing ingress, never opens a new one), unless
	// VirtualModel.Fallback == false. Priority here is mandatory and must
	// be > 0, unlike an ordinary EndpointGroup: an unset priority defaults
	// to 0 and would silently compete with a model's own real endpoints for
	// the same tier.
	FallbackEndpoints map[string][]EndpointGroup `yaml:"fallback_endpoints"`
	// Pricing is the global pricing block — currency, exchange rate,
	// and an optional user supplement/standard-table override. See
	// PricingConfig's doc comment (pricing.go).
	Pricing *PricingConfig `yaml:"pricing"`

	// ResolvedPricing holds every metric:-cost provider+model's fully
	// resolved pricing.Resolve result, keyed by provider+"\x00"+model —
	// filled by resolvePricing() during validate(), read by
	// router.BuildSnapshot to fold onto core.Endpoint.PricingRate
	// (pricing.FoldSpec, the pre-folded *core.Rate; this map stays in spec
	// form so vmr check can still display the override chain). Not a yaml
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

	// pricingFactorCache / pricingCurrencyCache are the global USD ->
	// pricing.currency factor and that currency's code, cached alongside
	// pricingTableCache because they come from the same buildPricingContext
	// pass and are meaningless apart from the table they scale — see
	// PricingAccounting.
	pricingFactorCache   float64 `yaml:"-"`
	pricingCurrencyCache string  `yaml:"-"`

	// configDir is the directory the config file was Load()ed from — the
	// anchor for relative sidecar paths (see resolveConfigRelative). Empty
	// for a config built from bytes via Parse.
	configDir string `yaml:"-"`

	// EmptyEnvRefs is every ${NAME} the config text referenced that was unset
	// or empty in the environment at load time, sorted. Not a yaml field —
	// populated by Parse from expandEnv. Advisory only (a config can
	// reference a var it doesn't need); cmd_start's startup banner surfaces
	// it because a forgotten `api_key: ${VAR}` is the single most common
	// "loads fine, 401s on the first request" failure.
	EmptyEnvRefs []string `yaml:"-"`
}

var envRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv replaces ${NAME} with the environment value. Only the ${...} form
// is recognized; a bare $ stays literal. Unset variables expand to "".
//
// This runs on the raw YAML text BEFORE parsing (see Parse) — a text-layer
// substitution, not a per-scalar one. A substituted value containing a
// newline, ": ", " #", or starting with "#" doesn't just fill in the scalar it
// was written into: a newline or ": " can restructure the document (a new
// top-level key, a value that swallows the rest of the line), and " #" or a
// leading "#" starts a YAML comment — silently truncating the value with no
// parse error at all (confirmed: `api_key: sk-real${SUFFIX}` with SUFFIX
// containing " #..." parses cleanly with api_key holding only the text before
// the space, and `api_key: ${KEY}` with KEY starting with "#" turns the line
// into a comment, silently expanding to ""). No config author intends either
// outcome, so both are hard load errors rather than a silent
// misinterpretation — the same fail-fast rule this package applies to every
// other "config that would look like it works but doesn't" case (see e.g.
// resolvePricing's currency-factor check).
// Not exhaustive: a value used inside a YAML flow collection (e.g.
// `api_keys: [${VAR}]`) could still inject an extra element via a comma —
// narrower and less common than the three checked here (needs flow-style
// usage, which this codebase's own examples never write), left as a known
// residual gap rather than hand-rolling a full YAML-metacharacter scanner.
// The returned []string is every referenced ${NAME} that was unset or empty
// at expansion time, sorted and deduped — the single most common "forgot to
// set the key" cause (an api_key: ${VAR} whose VAR was never exported), which
// otherwise loads as a valid-YAML empty string and only fails at the first
// 401. Callers surface it (cmd_start's startup banner); it is advisory, never
// a load error — a config can legitimately reference a var it doesn't need.
func expandEnv(s string) (string, []string, error) {
	var badVar string
	empty := map[string]bool{}
	out := envRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1]
		v, ok := os.LookupEnv(name)
		if !ok || v == "" {
			empty[name] = true
		}
		if badVar == "" && (strings.Contains(v, "\n") || strings.Contains(v, ": ") || strings.Contains(v, " #") || strings.HasPrefix(strings.TrimSpace(v), "#")) {
			badVar = name
		}
		return v
	})
	if badVar != "" {
		return "", nil, fmt.Errorf("environment variable %q's value contains a newline, \": \", \" #\", or starts with \"#\" — expanding it into config.yaml could change the document's structure or silently truncate the value at a YAML comment, not just fill in a scalar; remove those characters from the value (or avoid interpolating it) before retrying", badVar)
	}
	if len(empty) == 0 {
		return out, nil, nil
	}
	return out, fmtutil.SortedKeys(empty), nil
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
	if c.Timeouts.Probe <= 0 {
		c.Timeouts.Probe = Duration(DefaultProbeTimeout)
	}
	if c.TTL.Sticky.D() <= 0 {
		c.TTL.Sticky = Duration(DefaultStickyTTL)
	}
	if c.TTL.ImageCache.D() <= 0 {
		c.TTL.ImageCache = CalendarDuration(DefaultImageCacheTTLDays) * CalendarDuration(24*time.Hour)
	}
	if c.TTL.AuditRetention.D() <= 0 {
		c.TTL.AuditRetention = CalendarDuration(DefaultAuditRetentionDays) * CalendarDuration(24*time.Hour)
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
		if changed {
			c.Models[name] = m
		}
	}
}

func (c *Config) validate() error {
	if err := c.validateBasic(); err != nil {
		return err
	}
	quotaNow := time.Now()
	if err := c.validateProviders(quotaNow); err != nil {
		return err
	}
	if err := c.validateModelDefaults(); err != nil {
		return err
	}
	providerModels := map[string]map[string]bool{}
	if err := c.validateModels(providerModels); err != nil {
		return err
	}
	if err := c.validateFallbackEndpoints(providerModels); err != nil {
		return err
	}
	return c.resolvePricing(providerModels)
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
