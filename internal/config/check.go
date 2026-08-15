// Ver 2026-08-02, by Sonnet 5

// Consistency/operational checks beyond validate(): things that don't stop
// a config from loading and building a routing table (BuildSnapshot still
// succeeds), but make it silently broken in a way worth surfacing before
// real network I/O. Split out of config.go's structural validate() rather
// than folded into it because these are exactly the checks `vmr check`
// wants to keep rendering the full report around (inline ⚠️ + a trailing
// Failed summary) instead of aborting on the first one, the way a Load()
// error necessarily does.
package config

import (
	"fmt"
	"net"

	"vmr/internal/core"
)

// Severity distinguishes an Issue that should fail `vmr check` and gate
// `vmr diagnose`'s network phase (SeverityError, the zero value — every
// pre-existing check keeps failing exactly as before without touching its
// call sites) from one that's worth surfacing but must never block anything
// (SeverityWarning) — a config choice that's operationally risky but fully
// intentional, like a non-loopback listen with no api_keys.
type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
)

func (s Severity) String() string {
	switch s {
	case SeverityWarning:
		return "warning"
	default:
		return "error"
	}
}

func (s Severity) MarshalText() ([]byte, error) {
	return []byte(s.String()), nil
}

func (s *Severity) UnmarshalText(text []byte) error {
	switch string(text) {
	case "warning":
		*s = SeverityWarning
	default:
		*s = SeverityError
	}
	return nil
}

// Issue is one problem Check finds. Provider/Model scope it for callers
// that want to annotate a specific rendered line (vmr check) — Field names
// which one ("api_key" | "probe_timeout" | "endpoint" | "listen"); all empty
// means the issue is global. Endpoint carries the full
// "protocol/provider/model" key for "endpoint".
type Issue struct {
	Provider string   `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
	Endpoint string   `json:"endpoint,omitempty"`
	Field    string   `json:"field,omitempty"`
	Message  string   `json:"message"`
	Severity Severity `json:"severity"`
}

// Check runs every consistency check beyond validate(). Shared by `vmr
// check` (renders each Issue inline as a ⚠️, plus a trailing Failed summary
// if any SeverityError issue is present — SeverityWarning issues are listed
// but never fail the command) and `vmr diagnose` (skips its real
// connectivity test entirely whenever a SeverityError issue is present — no
// point dialing out for a config already known to be operationally broken;
// a SeverityWarning issue doesn't gate anything, since it's not "broken").
func (c *Config) Check() []Issue {
	var issues []Issue
	issues = append(issues, c.checkTimeouts()...)
	issues = append(issues, c.checkListenExposure()...)
	issues = append(issues, c.checkProviders()...)
	issues = append(issues, c.checkModels()...)
	return issues
}

// HasErrors reports whether issues contains at least one SeverityError
// entry, ignoring any SeverityWarning ones — the single predicate `vmr
// check`'s Failed summary and `vmr diagnose`'s network-phase skip both key
// off, so the two callers can't drift on what counts as "actually broken".
func HasErrors(issues []Issue) bool {
	for _, is := range issues {
		if is.Severity == SeverityError {
			return true
		}
	}
	return false
}

// checkListenExposure flags a non-loopback listen address with no
// api_keys configured: validate() only checks that Listen is syntactically
// a valid host:port (BuildSnapshot still succeeds), so binding
// 0.0.0.0/a LAN IP with an empty api_keys list loads cleanly and silently
// turns vmr into an open proxy holding every configured upstream credential.
// A host that fails to parse as an IP (a hostname, or an empty host like
// ":8800") is treated as exposed too — erring toward the warning, since the
// common cases this actually needs to catch (0.0.0.0, a LAN IP, empty host)
// all fail the loopback check the same way admin.go's own loopback gate does.
func (c *Config) checkListenExposure() []Issue {
	if len(c.APIKeys) > 0 {
		return nil
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return nil // validate() already rejects this; nothing new to say here
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return []Issue{{Field: "listen", Severity: SeverityWarning, Message: fmt.Sprintf(
		"listen (%s) is not loopback-only and no api_keys are configured — vmr is an open proxy exposing every configured upstream credential to anyone who can reach this address",
		c.Listen)}}
}

// checkTimeouts flags a probe_timeout that isn't safely under
// response_header: the whole point of a background probe is a fast, cheap
// liveness check real traffic never waits on (see DefaultProbeTimeout's doc
// comment) — a probe_timeout at or above the response_header budget defeats
// that, letting a stuck probe hold an endpoint half-open for as long as a
// real request would.
func (c *Config) checkTimeouts() []Issue {
	if c.ProbeTimeout.D() >= c.Timeouts.ResponseHeader.D() {
		return []Issue{{Field: "probe_timeout", Message: fmt.Sprintf(
			"probe_timeout (%s) should stay under response_header timeout (%s), or a background probe recovery check can hang as long as real traffic waits for a response",
			c.ProbeTimeout.D(), c.Timeouts.ResponseHeader.D())}}
	}
	return nil
}

// checkProviders flags an empty api_key — validate() never checks this (an
// empty upstream credential is syntactically valid YAML), so a typo'd or
// forgotten ${ENV_VAR} silently loads as "no key" and only fails once a
// real request 401s. (Provider.Proxy has no global default to inherit — a
// provider's proxy: true with no matching proxy URL is already a hard
// validate() error, not a Check-time gap.)
func (c *Config) checkProviders() []Issue {
	var issues []Issue
	for _, p := range c.Providers {
		if p.APIKey == "" {
			issues = append(issues, Issue{Provider: p.Name, Field: "api_key", Message: fmt.Sprintf("provider %q: api_key missing", p.Name)})
		}
	}
	return issues
}

// checkModels flags the same (protocol, provider, upstream model) endpoint
// declared more than once under one virtual model — almost always a
// copy-paste mistake (the duplicate is dead weight: failover only ever
// walks distinct health-tracked endpoints, so the repeat never adds real
// redundancy).
func (c *Config) checkModels() []Issue {
	var issues []Issue
	for _, name := range core.SortedKeys(c.Models) {
		seen := map[string]bool{}
		for _, eg := range c.Models[name].Endpoints {
			for _, mn := range eg.Models {
				key := eg.Protocol + "/" + eg.Provider + "/" + mn
				if seen[key] {
					issues = append(issues, Issue{Model: name, Endpoint: key, Field: "endpoint", Message: fmt.Sprintf(
						"model %q: endpoint %s declared more than once", name, key)})
					continue
				}
				seen[key] = true
			}
		}
	}
	return issues
}
