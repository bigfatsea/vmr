// Ver 2026-09-23 08:10, by Claude Opus 5.5

// Agent Guard's config schema. Guard is a *Guard, not a value: a nil Config.Guard means
// the guard: key is entirely absent from the YAML, which must mean zero
// code path overhead and 100% original byte-faithful passthrough — the online outbound/inbound wiring
// this schema feeds (server/guard.go, router/guard.go) checks that nil
// first, before touching a single byte.
package config

import (
	"fmt"
)

// Guard is the top-level guard: block. A nil Config.Guard is the "off"
// state; once present, every field below has a sane, conservative default
// (applyGuardDefaults) so a minimal `guard: {}` is valid and inert
// (outbound.mode defaults to audit_only, never a stronger posture: an
// aggressive out-of-the-box default buys no real safety).
type Guard struct {
	// TrustedProviders lists providers this instance controls or fully
	// trusts (e.g. an internal vLLM, a direct official endpoint): their
	// responses skip inbound sanitization, and a virtual model whose
	// candidates are all trusted skips the outbound scan (resolved once at
	// snapshot time as ModelRoute.GuardAllTrusted).
	TrustedProviders []string      `yaml:"trusted_providers"`
	Outbound         GuardOutbound `yaml:"outbound"`
	Inbound          GuardInbound  `yaml:"inbound"`
}

const (
	GuardOutboundOff       = "off"
	GuardOutboundAuditOnly = "audit_only"
	GuardOutboundBlock     = "block"
)

var validGuardOutboundModes = map[string]bool{
	GuardOutboundOff: true, GuardOutboundAuditOnly: true, GuardOutboundBlock: true,
}

// GuardOutbound is guard.outbound:. The former
// mode: replace (and its marker/session_ttl/max_entries/restore_scope/
// max_restores_per_response knobs) was removed with the pseudonymization
// machinery it existed to serve — see internal/guard's package doc. The
// former salt knob (and its <rundir>/guard.salt resolution chain) was
// removed when Hit.FP's fingerprint became a deterministic,
// unsalted hash (see KNOWN_ISSUES) — there is nothing left to configure.
type GuardOutbound struct {
	// Mode: off | audit_only | block. audit_only is the only mode
	// calibrated purely from offline evidence;
	// block intervenes on the request path.
	Mode string `yaml:"mode"`
}

// GuardInbound is guard.inbound:. Narrowed to a single
// field: the online Tool Call double gate, its protected-path/command-
// category tables, and the opaque/oversize block policies were all removed
// — a client's own approval gate and sandbox are the correct
// place to judge whether a command should run, and this gateway never had
// more context than they do. Unicode-steganography sanitization is the one
// online inbound capability that remains.
type GuardInbound struct {
	// SanitizeInvisibleRunes is a *bool for the same reason
	// VirtualModel.Sticky/Fallback are (config.go): nil (key absent) must
	// default to true, distinct from an explicit `false` opting
	// out — a plain bool can't represent that distinction. Use
	// SanitizeRunes() to read the resolved value.
	SanitizeInvisibleRunes *bool `yaml:"sanitize_invisible_runes"`
}

// SanitizeRunes reports whether inbound Unicode-steganography sanitation
// is on — nil (unset) defaults to true, same polarity as
// VirtualModel.Sticky/Fallback.
func (i GuardInbound) SanitizeRunes() bool {
	return i.SanitizeInvisibleRunes == nil || *i.SanitizeInvisibleRunes
}

// applyGuardDefaults fills every guard: sub-field applyDefaults' caller
// left unset, once Config.Guard is non-nil. Every default here is the
// conservative end of its range (the rollout path is fixed as off →
// audit_only → block) — enabling guard: at all must never itself turn on an
// intervention stronger than audit-only.
func (c *Config) applyGuardDefaults() {
	if c.Guard == nil {
		return
	}
	g := c.Guard
	if g.Outbound.Mode == "" {
		g.Outbound.Mode = GuardOutboundAuditOnly
	}
	// Inbound.SanitizeInvisibleRunes needs no fill-in here: it's a *bool
	// (nil = default true), resolved on read via
	// GuardInbound.SanitizeRunes(), the same deferred-resolution
	// convention as VirtualModel.Sticky.
}

// validateGuard is guard:'s load-error half — there is no corresponding
// load-warning half for guard: config anymore (checkGuard,
// its one warning case for a stale rules_version, was removed along with
// that decorative field). Runs after validateProviders (needs
// c.Providers settled for the trusted_providers existence check) and after
// applyGuardDefaults (Outbound.Mode is non-empty by the time this runs, so
// an empty string can only mean "user explicitly wrote an empty string,"
// which the enum whitelist already rejects — no separate empty-string
// special case needed). The removed mode: replace fails the whitelist below
// with a mode-specific hint; the seven guard.inbound keys that were removed
// (tool_call_guard_mode/on_block/on_opaque_response/max_tool_arg_bytes/
// on_oversize/protected_paths/blocked_command_categories) no longer exist
// as struct fields at all, so a config still declaring them is rejected by
// KnownFields strict mode with the standard unknown-field message — the
// same treatment the earlier-removed sanitize_level_b/
// max_prefilter_confirms/inject_system_note knobs already get, not a
// special case this function needs to add.
func (c *Config) validateGuard() error {
	if c.Guard == nil {
		return nil
	}
	g := c.Guard

	for i, name := range g.TrustedProviders {
		if _, ok := c.ProviderByName(name); !ok {
			return fmt.Errorf("guard.trusted_providers[%d]: unknown provider %q", i, name)
		}
	}

	if g.Outbound.Mode == "replace" {
		return fmt.Errorf("guard.outbound.mode: %q was removed — use %q (the response-side restore it required was a decryption-oracle risk; see internal/guard's package doc)", "replace", GuardOutboundBlock)
	}
	if !validGuardOutboundModes[g.Outbound.Mode] {
		return fmt.Errorf("guard.outbound.mode: invalid value %q (want off, audit_only, or block)", g.Outbound.Mode)
	}
	return nil
}
