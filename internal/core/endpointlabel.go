// Ver 2026-08-13, by Opus 5

// The audit-log endpoint label format — the one that names an upstream
// attempt in audit.Attempt.Endpoint (and, via it, ctxgraph.Manifest.Endpoint
// / report.EndpointRow.Endpoint / story's per-Step upstream lookup): before
// this file, two production sites (internal/router/router.go,
// internal/replay/replay.go) each inlined the same
// strings.Join([]string{adapterType, provider, model}, ":") literal — the
// format had no single authoritative definition. See
// docs/future-strategy/vmr_quota_visibility_devplan_opus-5.md's batch 5 for
// the full inventory (2 production sites, 4 parsing sites) and why this
// batch only unifies the DEFINITION, not every parsing call site's existing
// behavior — see SplitEndpointLabel's own doc comment for the one parsing
// quirk (internal/report/cost.go's splitEndpointProviderModel) this
// deliberately does not touch.
//
// This is a DIFFERENT format from Endpoint.Name() (core.go's own
// AdapterType+"/"+Provider+"/"+Model, "/"-joined) — that one is a
// human-facing display identity used by internal/server/admin.go's
// /admin/status, never written to the audit log. The two formats coexist on
// purpose and must not be unified into one: this file's colon-joined format
// is a stable on-disk contract every historical audit record already uses,
// while Name() is free to change its display shape without touching a
// single byte on disk.
package core

import "strings"

// EndpointLabel produces the audit-log-standard "protocol:provider:model"
// label — the single source every writer (router.go's tryOne, replay.go's
// Run) must use instead of inlining the same strings.Join literal.
func EndpointLabel(adapterType, provider, model string) string {
	return adapterType + ":" + provider + ":" + model
}

// SplitEndpointLabel parses EndpointLabel's format, falling back to the
// "/"-joined form older audit logs used before this format existed.
// SplitN(..., 3): the model component itself may legitimately contain ":"
// or "/" (e.g. "z-ai/glm-5.2"), so only the first two separators are
// significant. ok=false for anything that isn't a clean 3-part split either
// way (including ctxgraph.Manifest.Endpoint's "-" sentinel for "no attempt
// was ever made").
//
// Which separator is structural (not just trying ":" first, unconditionally)
// is decided by which one occurs FIRST in label: adapterType is always one
// of a small fixed set ("openai"/"anthropic"/"openai-responses") and never
// itself contains "/" or ":" in either format, so the earliest separator in
// the string is always the real protocol/provider boundary, never one
// embedded inside the model name. Trying ":" first unconditionally used to
// misparse a "/"-joined legacy label whose model segment happened to
// contain two or more ":" (e.g. an Ollama/vLLM-style "registry:port/name:tag"
// model) — its ":"-split would find exactly 3 parts and "succeed", just at
// the wrong field boundaries, before ever reaching the correct "/" split.
//
// NOT currently wired into internal/report/cost.go's
// splitEndpointProviderModel (colon-only, backs §2's $ cost estimates) — see
// this package's own file doc comment: widening that one call site to also
// accept "/" would change historical reports' $ numbers for old-format
// logs, a distinct, separately-reviewed change (flagged in the P2.2 dev
// plan's risk table), not a side effect of centralizing the definition.
func SplitEndpointLabel(label string) (protocol, provider, model string, ok bool) {
	colonIdx := strings.IndexByte(label, ':')
	slashIdx := strings.IndexByte(label, '/')
	sep := "/"
	if colonIdx >= 0 && (slashIdx < 0 || colonIdx < slashIdx) {
		sep = ":"
	}
	if parts := strings.SplitN(label, sep, 3); len(parts) == 3 {
		return parts[0], parts[1], parts[2], true
	}
	return "", "", "", false
}
