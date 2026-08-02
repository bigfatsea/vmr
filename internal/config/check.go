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

	"vmr/internal/core"
)

// Issue is one problem Check finds. Provider/Model scope it for callers
// that want to annotate a specific rendered line (vmr check) — Field names
// which one ("api_key" | "probe_timeout" | "endpoint"); all empty means the
// issue is global. Endpoint carries the full "protocol/provider/model" key
// for "endpoint". There is only one severity — every Issue Check returns is
// meant to fail `vmr check`/gate `vmr diagnose`'s connectivity test, not
// merely inform.
type Issue struct {
	Provider string
	Model    string
	Endpoint string
	Field    string
	Message  string
}

// Check runs every consistency check beyond validate(). Shared by `vmr
// check` (renders each Issue inline as a ⚠️ plus a trailing Failed summary)
// and `vmr diagnose` (skips its real connectivity test entirely whenever
// Check finds anything — no point dialing out for a config already known to
// be operationally broken).
func (c *Config) Check() []Issue {
	var issues []Issue
	issues = append(issues, c.checkTimeouts()...)
	issues = append(issues, c.checkProviders()...)
	issues = append(issues, c.checkModels()...)
	return issues
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
