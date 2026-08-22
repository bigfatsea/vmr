// Ver 2026-08-22, by Sonnet 5

// Provider's type definition — split out of config.go (which was at its
// archtest line budget) when APIKeys was added; pure move, no behavior
// change beyond the new field itself.
package config

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
	// APIKeys is sugar for several independent accounts on the same vendor,
	// expanded at Parse time — see apikeys.go. A plain Go map: iteration
	// order (and so which expanded key ends up "first" when no priority/
	// quota breaks the tie) isn't guaranteed across reloads — see
	// expandProviderAPIKeys's doc comment for why that's fine here.
	APIKeys map[string]string `yaml:"api_keys"`
	Proxy   bool              `yaml:"proxy"`
	// Quota declares this account's usage-plan limit(s) for Quota-Aware
	// Routing (see docs/VirtualModelRouter_Design_v4_Quota.md, and its
	// "现状与后续计划" section for what's actually shipped). nil = unmetered — no behavior change from before
	// this field existed. A pointer, not a value, so "absent" and "present
	// but empty" are distinguishable — the latter is a validation error
	// (quota: with no limits: is almost certainly a mistake), not a silent
	// no-op.
	Quota *QuotaConfig `yaml:"quota"`
	// Pricing declares this account's price differences from the standard
	// list price — required (and validated for completeness) when
	// Quota has a metric: cost limit; optional otherwise, in which case it
	// only sharpens vmr report's $ estimates. See ProviderPricingConfig's
	// doc comment (pricing.go).
	Pricing *ProviderPricingConfig `yaml:"pricing"`
}
