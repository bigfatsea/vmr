// Ver 2026-08-13, by Opus 5

// Package core defines the canonical "protocol:provider:model" audit-log endpoint label format.
// This is distinct from human-facing display names (Endpoint.Name() joined by "/").
package core

import "strings"

// EndpointLabel produces the audit-log-standard "protocol:provider:model"
// label — the single source every writer (router.go's tryOne, replay.go's
// Run) must use instead of inlining the same strings.Join literal.
func EndpointLabel(adapterType, provider, model string) string {
	return adapterType + ":" + provider + ":" + model
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
