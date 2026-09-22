// Ver 2026-09-13, by Sonnet 5

package guard

import "fmt"

// RulesVersion is the built-in rule set's version (ADR-11's Record.Guard.Ver
// — a version bump is a documented Prompt Cache invalidation event once
// online rewriting exists; for this offline-only build it just labels
// which rule generation produced a given Finding). Bump alongside any
// change to DefaultRules' patterns.
const RulesVersion = 1

// DefaultRules returns the built-in Tier1+Tier2 rule set: Appendix A of
// the Agent Guard spec, calibrated against this
// repo's own real audit corpus (tools/guard_corpus_scan; see the MVP
// execution report linked from docs/KNOWN_ISSUES.md for the run that
// confirmed Tier1 FP=0 and quantified the bare "sk-" prefix's noise).
//
// Every rule goes through NewRule, so every one carries the left-boundary
// anchor (leftBoundary) by construction — the anchor is what makes "sk-"
// safe to keep even loosely: "task-specific"/"ask-user-question" fail the
// boundary check because the character before their embedded "sk-"/"ask-"
// substring is itself alphanumeric.
//
// Deliberately NOT included here (scope decision, see the execution
// report): generic-api-key, bearer-token, email, cn-mobile,
// cn-resident-id. The spec names these as Tier2 candidates but gives no
// concrete pattern for them, and they shift scope from credential
// exfiltration into general PII detection — a different feature axis.
// Backlogged in docs/ROADMAP.md.
func DefaultRules() []Rule {
	specs := []struct {
		name       string
		tier       Tier
		literal    string
		body       string
		minEntropy float64
	}{
		// --- Tier 1: five-anchor admitted (ADR-5, Appendix A) ---
		{"anthropic-api-key", Tier1, "sk-ant-api03-", `sk-ant-api03-[A-Za-z0-9_-]{93}`, 3.5},
		{"openai-project-key", Tier1, "sk-proj-", `sk-proj-[A-Za-z0-9_-]{74,}`, 3.5},
		{"openai-legacy-key", Tier1, "sk-", `sk-[A-Za-z0-9]{48}`, 3.8},
		{"gcp-api-key", Tier1, "AIza", `AIza[0-9A-Za-z_-]{35}`, 3.5},
		{"aws-access-key", Tier1, "AKIA", `AKIA[0-9A-Z]{16}`, 3.2},
		{"github-pat", Tier1, "ghp_", `ghp_[A-Za-z0-9]{36}`, 3.5},
		{"github-oauth", Tier1, "gho_", `gho_[A-Za-z0-9]{36}`, 3.5},
		{"huggingface-token", Tier1, "hf_", `hf_[A-Za-z0-9]{34}`, 3.5},
		{"slack-bot-token", Tier1, "xoxb-", `xoxb-\d{10,13}-\d{10,13}-[A-Za-z0-9]{24}`, 3.5},
		{"jwt", Tier1, "eyJ", `eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`, 3.5},
		// PEM armor is structural text, not rendered randomness — entropy
		// gate does not apply (MinEntropy 0 disables it, see Rule.MinEntropy).
		//
		// Deliberate limitation, not a bug: this pattern matches only the
		// PEM header line, never the base64 key body that follows. Two
		// different RSA private keys therefore produce the identical
		// Finding.Body and thus the identical Hit.FP (internal/audit's
		// Fingerprint is keyed on rule name + matched bytes) — FP for this
		// one rule identifies "a private key of this type was seen", not
		// "this specific key". Matching the full multi-line block would
		// need real leaked-key samples to calibrate against (this repo's
		// real-corpus scan has zero private-key-block hits to date), so the
		// cost of getting a hand-designed multi-line pattern wrong outweighs
		// fixing a precision gap nothing has exercised yet. See
		// docs/KNOWN_ISSUES.md for the full option comparison; revisit if a
		// real hit ever appears.
		{"private-key-block", Tier1, "-----BEGIN", `-----BEGIN [A-Z ]*PRIVATE KEY-----`, 0},

		// --- Tier 2: audit-only forever (K-G5) ---
		// The bare "sk-" prefix (§2.3): real-corpus scan showed ~8,000 hits
		// on English words ending "sk" plus a hyphen (task-/risk-/disk-/
		// ask-/desk-/mask-) before the boundary anchor was added, and even
		// with the anchor its open-ended body (any 20+ alnum/hyphen/
		// underscore run) is far weaker than openai-legacy-key's exact
		// 48-char body. Never promotable to Tier1 — see K-G5.
		{"generic-sk-prefix", Tier2, "sk-", `sk-[A-Za-z0-9_-]{20,}`, 3.0},
	}

	rules := make([]Rule, 0, len(specs))
	for _, s := range specs {
		r, err := NewRule(s.name, s.tier, s.literal, s.body, s.minEntropy)
		if err != nil {
			// Every spec above is a compile-time constant this package
			// controls; a failure here is a programming error caught by
			// TestDefaultRules_Compile, never a runtime condition.
			panic(fmt.Sprintf("guard: DefaultRules: %v", err))
		}
		rules = append(rules, r)
	}
	return rules
}
