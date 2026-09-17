// Ver 2026-09-14, by Sonnet 5

package config

import (
	"strings"
	"testing"
)

func TestGuard_AbsentMeansNil(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Guard != nil {
		t.Errorf("Guard = %+v, want nil when guard: is absent", cfg.Guard)
	}
}

func TestGuard_EmptyBlockGetsConservativeDefaults(t *testing.T) {
	cfg, err := Parse([]byte(validYAML + "guard: {}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	g := cfg.Guard
	if g == nil {
		t.Fatal("Guard is nil, want a defaulted struct")
	}
	if g.Outbound.Mode != GuardOutboundAuditOnly {
		t.Errorf("Outbound.Mode = %q, want %q (never a stronger default)", g.Outbound.Mode, GuardOutboundAuditOnly)
	}
	if !g.Inbound.SanitizeRunes() {
		t.Error("Inbound.SanitizeRunes() = false, want true by default")
	}
}

func TestGuard_ExplicitSanitizeFalseIsRespected(t *testing.T) {
	cfg, err := Parse([]byte(validYAML + "guard:\n  inbound:\n    sanitize_invisible_runes: false\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Guard.Inbound.SanitizeRunes() {
		t.Error("SanitizeRunes() = true, want false when explicitly disabled")
	}
}

func TestGuard_UnknownFieldRejected(t *testing.T) {
	if _, err := Parse([]byte(validYAML + "guard:\n  outbound:\n    modeeee: block\n")); err == nil {
		t.Error("Parse should reject an unknown guard field (KnownFields strict mode)")
	}
}

func TestGuard_InvalidEnumsRejected(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"outbound.mode", "guard:\n  outbound:\n    mode: aggressive\n"},
		{"outbound.mode: replace (removed)", "guard:\n  outbound:\n    mode: replace\n"},
		{"inbound.sanitize_level_b (removed)", "guard:\n  inbound:\n    sanitize_level_b: strip\n"},
		{"inbound.max_prefilter_confirms (removed)", "guard:\n  inbound:\n    max_prefilter_confirms: 32\n"},
		{"outbound.inject_system_note (removed)", "guard:\n  outbound:\n    inject_system_note: true\n"},
		// ADR-15: the entire online Tool Call gate was removed, taking these
		// seven guard.inbound keys with it (fields no longer exist at all,
		// so KnownFields' generic unknown-field message covers them, same
		// treatment as the three removed knobs above).
		{"inbound.tool_call_guard_mode (removed)", "guard:\n  inbound:\n    tool_call_guard_mode: audit_only\n"},
		{"inbound.on_block (removed)", "guard:\n  inbound:\n    on_block: error_event\n"},
		{"inbound.on_opaque_response (removed)", "guard:\n  inbound:\n    on_opaque_response: block\n"},
		{"inbound.on_oversize (removed)", "guard:\n  inbound:\n    on_oversize: block\n"},
		{"inbound.max_tool_arg_bytes (removed)", "guard:\n  inbound:\n    max_tool_arg_bytes: 1024\n"},
		{"inbound.protected_paths (removed)", "guard:\n  inbound:\n    protected_paths: [\"~/.ssh/*\"]\n"},
		{"inbound.blocked_command_categories (removed)", "guard:\n  inbound:\n    blocked_command_categories: [pipe_to_shell]\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Parse([]byte(validYAML + c.yaml)); err == nil {
				t.Errorf("Parse should reject invalid %s", c.name)
			}
		})
	}
}

func TestGuard_TrustedProvidersMustExist(t *testing.T) {
	if _, err := Parse([]byte(validYAML + "guard:\n  trusted_providers: [does-not-exist]\n")); err == nil {
		t.Error("Parse should reject an unknown provider in trusted_providers")
	}
	cfg, err := Parse([]byte(validYAML + "guard:\n  trusted_providers: [p1]\n"))
	if err != nil {
		t.Fatalf("Parse with a real provider name should succeed: %v", err)
	}
	if len(cfg.Guard.TrustedProviders) != 1 || cfg.Guard.TrustedProviders[0] != "p1" {
		t.Errorf("TrustedProviders = %v, want [p1]", cfg.Guard.TrustedProviders)
	}
}

// TestGuard_ReplaceModeIsLoadError pins the removal of mode: replace: the
// removed value fails validateGuard with a mode-specific hint (not the
// generic whitelist message), and its companion knobs (restore_scope et al.)
// are now unknown fields rejected by strict YAML.
func TestGuard_ReplaceModeIsLoadError(t *testing.T) {
	_, err := Parse([]byte(validYAML + "guard:\n  outbound:\n    mode: replace\n"))
	if err == nil {
		t.Fatal("Parse should reject the removed mode: replace")
	}
	if !strings.Contains(err.Error(), "was removed") {
		t.Errorf("err = %v, want the mode-specific removal hint", err)
	}
	for _, knob := range []string{"rules_version"} {
		if _, err := Parse([]byte(validYAML + "guard:\n  " + knob + ": 1\n")); err == nil {
			t.Errorf("Parse should reject removed guard knob %q as an unknown field", knob)
		}
	}
	for _, knob := range []string{"restore_scope", "marker", "session_ttl", "max_entries", "max_restores_per_response", "salt"} {
		if _, err := Parse([]byte(validYAML + "guard:\n  outbound:\n    " + knob + ": x\n")); err == nil {
			t.Errorf("Parse should reject removed guard knob %q as an unknown field", knob)
		}
	}
}
