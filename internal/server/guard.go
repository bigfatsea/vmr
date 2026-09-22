// Ver 2026-09-16, by Sonnet 5

// Agent Guard's outbound mount point (the Agent Guard spec
// ADR-4/§4.3/ADR-10, M3.4/M3.5/M3.6). chatHandler's own call site is two
// lines (the call, then a blocked-check) — K12's ≤5-line budget for the
// change at chatHandler itself — with the scan/aggregate/record/trust-
// exemption/block-response logic living here instead.
package server

import (
	"net/http"

	"vmr/internal/audit"
	"vmr/internal/guard"
	"vmr/internal/router"
)

// WithGuard wires Agent Guard's online engine into the server —
// nil-safe (a nil g is the same as never calling this at all). Built once
// at startup (cmd/vmr) from the startup config alone; hot reload never
// calls this again. applyOutboundGuard's gate is `snap.Cfg.Guard == nil ||
// s.guard == nil` — the OR means a startup guard: nil (s.guard stays nil
// forever this process) makes a later hot reload that adds guard: a no-op
// until restart (cmd/vmr's setupGuard doc comment). The reverse direction
// IS hot-reload-live: once s.guard is non-nil, applyOutboundGuard reads
// snap.Cfg.Guard fresh every request, so toggling outbound.mode, or
// removing the guard: block entirely (Cfg.Guard flips to nil, gating the
// same as mode: off), takes effect on the very next request.
func (s *Server) WithGuard(g *guard.Guard) *Server {
	s.guard = g
	return s
}

// applyOutboundGuard scans body for credential-shaped content when guard:
// is configured in the CURRENT snapshot, stamps rec.Guard with the
// result, and — under mode: block with a Tier1 hit (M3.6) — writes the 400
// response itself and reports blocked=true so chatHandler returns
// without ever reaching downscaleImages/routing: an early reject here
// never touches Failover or endpoint health, since no attempt has been
// dispatched yet. The former mode: replace (outbound rewrite + response-side restore) was
// removed — see internal/guard's package doc.
//
// Gated on snap.Cfg.Guard != nil, not s.guard != nil: snap is refreshed
// every request (hot-reload safe), while s.guard (the engine) is
// built once at startup — so a config that never declared guard: at
// startup, then hot-reloaded one in, stays off until restart (s.guard is
// nil), but toggling an already-guard-enabled instance's mode (including
// back to off) via hot-reload takes effect immediately. This mirrors
// log_dir's own "some settings need a restart" precedent (config.go).
//
// When snap.Cfg.Guard == nil (the common case today — no caller ships a
// guard: block yet), this reads one pointer field and returns: the "zero
// code-path overhead" guarantee ADR-2 requires is a property of this
// function's very first line, not of some deeper fast path.
//
// route.GuardAllTrusted (M3.5, nil-safe via ModelRoute's zero value) exempts
// a request entirely — no scan, no Record.Guard stamp at all — when every
// candidate endpoint this (protocol, model) route could ever dispatch to
// is a trusted provider (§4.3): trusting an upstream is an online routing
// decision (don't interrupt a request the operator already authorized to
// reach it) — it says nothing about offline visibility. `vmr analyze`
// doesn't import routing config (CLAUDE.md's "two halves, one contract"),
// so it fallback-scans this exact record like any other unstamped one and
// still attributes exposure to that provider in the report — trusted
// upstream, but not blind to what reached it (KNOWN_ISSUES §2.169).
func (s *Server) applyOutboundGuard(w http.ResponseWriter, rec *audit.Record, snap *router.Snapshot, protocol, model string, body []byte) (out []byte, blocked bool) {
	if snap.Cfg.Guard == nil || s.guard == nil {
		return body, false
	}
	if route := snap.Models[protocol][model]; route != nil && route.GuardAllTrusted {
		return body, false
	}
	defer func() {
		if r := recover(); r != nil {
			out = body
			blocked = false
			if rec != nil {
				if rec.Guard == nil {
					rec.Guard = &audit.GuardRecord{Ver: guard.RulesVersion}
				}
				rec.Guard.OutMode = "error"
			}
		}
	}()
	mode := guard.OutMode(snap.Cfg.Guard.Outbound.Mode)
	if mode == guard.OutOff {
		// mode: off is observation-off: no scan, no stamp — this record
		// carries no outbound verdict, and the offline fallback scans its
		// request body exactly like a guard:-absent one. (The completion
		// hook in server.go may later stamp its Guard with inbound-only
		// SanitizedRunes; that stamp is direction-aware —
		// report.guardscan's guardOutboundStamped — and does not suppress
		// the outbound fallback. The former "stamp even under off"
		// behavior produced a full-looking record with no hits, which
		// suppressed the fallback and made off a detection blind spot.)
		return body, false
	}
	result := s.guard.Outbound(body, mode)
	if rec != nil {
		rec.Guard = &audit.GuardRecord{
			// Ver stamps the rule set that actually ran (guard.RulesVersion,
			// baked into s.guard's engine at startup by setupGuard).
			Ver:     guard.RulesVersion,
			OutMode: string(mode),
			Hits:    toAuditHits(result.Hits),
		}
	}
	if mode == guard.OutBlock && result.BlockedBy != "" {
		s.rt.Telemetry.RecordOutcome(false, false)
		router.WriteError(w, http.StatusBadRequest, "security_violation", "blocked by Agent Guard: credential-shaped content detected ("+result.BlockedBy+")")
		return result.Body, true
	}
	return result.Body, false
}

// toAuditHits copies guard.Hit (a type internal/guard can declare without
// importing internal/audit, ADR-1's dependency whitelist) into
// audit.Hit field-for-field. internal/server already imports both
// packages, so this trivial mapping is the cheapest place for it to live.
func toAuditHits(hits []guard.Hit) []audit.Hit {
	if len(hits) == 0 {
		return nil
	}
	out := make([]audit.Hit, len(hits))
	for i, h := range hits {
		out[i] = audit.Hit{Rule: h.Rule, Tier: int(h.Tier), Count: h.Count, FP: h.FP}
	}
	return out
}
