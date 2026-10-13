// Ver 2026-08-13, by Opus 5

// Package core defines the canonical "protocol:provider:model" endpoint label
// format, shared by the audit log and every human-facing endpoint name.
package core

import "strings"

// ProviderModel is the one "provider:model" spelling — the direct-routing
// name, live log lines, check/diagnose output and the UI all render a
// provider/model pair through it. Providers cannot contain ':' or '/'
// (config validation), so the first ':' always splits it and the model tail,
// which may itself hold ':' or '/' (ollama tags, OpenRouter vendor prefixes),
// is kept verbatim.
func ProviderModel(provider, model string) string {
	return provider + ":" + model
}

// EndpointLabel produces the audit-log-standard "protocol:provider:model"
// label — the single source every writer (router.go's tryOne, replay.go's
// Run) and Endpoint.Name() must use instead of inlining the same join.
func EndpointLabel(adapterType, provider, model string) string {
	return adapterType + ":" + ProviderModel(provider, model)
}

// SplitEndpointLabel parses an endpoint label ("protocol:provider:model" or legacy "/"-joined).
// It determines the separator by checking which delimiter occurs earliest.
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
