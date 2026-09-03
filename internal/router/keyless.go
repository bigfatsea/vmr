// Ver 2026-09-03, by pi-agent

package router

import (
	"fmt"
	"net"
	"net/http"
	"net/url"

	"vmr/internal/core"
)

// This file holds rejectIfAllKeyless and its host-shape helper, split out of
// router.go for the file's own line budget (see archtest's file_sizes table)
// — no behavioral significance beyond co-location.

// rejectIfAllKeyless answers a request for a model whose every endpoint has
// an empty api_key with one clear vmr-side error instead of letting each
// attempt 401 upstream (raw provider/CDN HTML to the client) and cool the
// endpoints down for 10min+. Checked against the full endpoint set, not the
// health-filtered one: an endpoint that already 401'd is in cooldown and
// would drop out, turning this into a vaguer "no candidates" message.
// Returns true when it handled the response.
//
// An all-empty key set is NOT always a forgotten ${ENV_VAR}: a keyless
// self-hosted upstream (LAN vLLM/llama.cpp/Ollama — KNOWN_ISSUES's no-auth
// entry) is a legitimate config. So the rejection is graded by upstream host
// shape: if EVERY keyless endpoint targets a loopback/private address, the
// request is allowed through — those upstreams answer without auth, and the
// failover loop handles them like any other candidate. One public host
// anywhere in the set keeps the 503: public providers reject keyless calls,
// so an empty key there is almost certainly the forgotten-env-var case this
// guard was written for. A mixed set (at least one endpoint with a key) is
// unaffected — the failover loop already skips to the keyed candidate on a
// 401. A URL that fails to parse counts as public (fail toward rejecting).
func (rt *Router) rejectIfAllKeyless(w http.ResponseWriter, creq *core.CanonicalRequest, eps []*core.Endpoint) bool {
	if len(eps) == 0 {
		return false
	}
	for _, ep := range eps {
		if ep.APIKey != "" {
			return false
		}
	}
	for _, ep := range eps {
		if !isLoopbackOrPrivateHost(hostOf(ep.FullURL)) {
			rt.Telemetry.RecordOutcome(false, false)
			WriteError(w, http.StatusServiceUnavailable, "vmr_no_api_key", fmt.Sprintf(
				"all %d endpoint(s) for model %q have no api_key — set the provider api_key (or the ${ENV_VAR} it references) and reload",
				len(eps), creq.Model))
			return true
		}
	}
	return false
}

// hostOf extracts the host (no port) from a full upstream URL, or "" when it
// does not parse — the caller treats "" as public.
func hostOf(fullURL string) string {
	u, err := url.Parse(fullURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// isLoopbackOrPrivateHost reports whether host is a loopback or private
// address: the "localhost" literal, or an IP that is loopback (127.0.0.0/8,
// ::1) or private (10/8, 172.16/12, 192.168/16, fc00::/7). A name that is
// not an IP (and is not literally "localhost") is not private — a DNS name
// can resolve anywhere, so it must not qualify.
func isLoopbackOrPrivateHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}
