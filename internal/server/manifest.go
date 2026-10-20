// Ver 2026-10-20, by Claude Sonnet 5.5

// GET /manifest: the gateway's self-description for clients that configure
// agents (ACM) or register models inside one (agent extensions).
//
// Everything here is DERIVED from the live routing snapshot plus the purely
// descriptive `meta` / `public_base_url` config fields — there is no second
// source of truth to keep in sync, and the document changes exactly when a
// hot reload changes the routing table (the ETag follows the content).
// Unknown is expressed by omission, never by zero/false: a field is only
// present when at least one candidate endpoint declares it (model_defaults),
// matching the "unstated = unconstrained" invariant of config.ModelDefaultEntry.
//
// The gateway publishes what it offers; it never decides which model a task
// should use — roles are hints for the client's own configuration slots.
package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"vmr/internal/core"
	"vmr/internal/fmtutil"
	"vmr/internal/router"
)

// manifestProtocolOrder is the canonical order protocols are reported in.
var manifestProtocolOrder = []string{core.ProtocolOpenAICompletions, core.ProtocolAnthropicMessages, core.ProtocolOpenAIResponses}

type manifestDoc struct {
	ManifestVersion int                `json:"manifest_version"`
	Gateway         manifestGateway    `json:"gateway"`
	Endpoints       map[string]string  `json:"endpoints"`
	Auth            manifestAuth       `json:"auth"`
	Models          []manifestModelDoc `json:"models"`
}

type manifestGateway struct {
	Kind    string `json:"kind"`
	Version string `json:"version,omitempty"`
}

type manifestAuth struct {
	Required bool `json:"required"`
}

type manifestModelDoc struct {
	ID          string              `json:"id"`
	DisplayName string              `json:"display_name,omitempty"`
	Description string              `json:"description,omitempty"`
	Protocols   []string            `json:"protocols"`
	Roles       []string            `json:"roles,omitempty"`
	Limit       *manifestLimit      `json:"limit,omitempty"`
	Modalities  *manifestModalities `json:"modalities,omitempty"`
	ToolCall    *bool               `json:"tool_call,omitempty"`
	Reasoning   *bool               `json:"reasoning,omitempty"`
	Guarantee   *manifestGuarantee  `json:"guarantee,omitempty"`
}

type manifestLimit struct {
	Context      int64 `json:"context,omitempty"`
	ContextFloor int64 `json:"context_floor,omitempty"`
}

type manifestModalities struct {
	Input []string `json:"input"`
}

type manifestGuarantee struct {
	Input     []string `json:"input"`
	ToolCall  bool     `json:"tool_call"`
	Reasoning bool     `json:"reasoning"`
}

// manifest serves the document with a content ETag (304 on If-None-Match).
func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	snap := s.rt.Snapshot()
	if snap == nil {
		router.WriteError(w, http.StatusServiceUnavailable, "service_unavailable", "router not yet initialized")
		return
	}
	if snap.Cfg.PublicBaseURL == "" && (r.Header.Get("X-Forwarded-Proto") != "" || r.Header.Get("X-Forwarded-Host") != "") && s.fwdWarned.CompareAndSwap(false, true) {
		// Forwarded headers are deliberately not trusted (vmr has no trusted-proxy
		// notion, and a forged one would redirect clients' keys): behind a TLS-terminating
		// proxy the derived endpoints are http://, so the operator has to say so.
		s.rt.Logf("WARN GET /manifest: request came through a proxy (X-Forwarded-* present) but public_base_url is unset; published endpoints use the scheme and Host vmr itself saw (%s://%s) — set public_base_url to the address clients should use", requestScheme(r), r.Host)
	}
	doc := buildManifest(snap, requestScheme(r), r.Host)
	body, err := json.Marshal(doc)
	if err != nil {
		router.WriteError(w, http.StatusInternalServerError, "internal_error", "cannot encode manifest")
		return
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func etagMatches(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimPrefix(strings.TrimSpace(part), "W/")
		if part == etag || part == "*" {
			return true
		}
	}
	return false
}

// buildManifest derives the manifest from the routing snapshot. Direct-routing
// names ("provider:model") are deliberately not listed: they bypass the try-order
// a virtual model encodes and are a debugging path, not part of a team catalog.
func buildManifest(snap *router.Snapshot, scheme, host string) manifestDoc {
	base := strings.TrimSuffix(snap.Cfg.PublicBaseURL, "/")
	if base == "" {
		if host == "" {
			host = "127.0.0.1"
		}
		base = scheme + "://" + host
	}
	protoOf := map[string][]string{} // model -> protocols it answers on
	served := map[string]bool{}
	cands := map[string][]*core.Endpoint{}
	for _, p := range manifestProtocolOrder {
		for _, name := range fmtutil.SortedKeys(snap.Models[p]) {
			if len(snap.Models[p][name].Endpoints) == 0 {
				continue // every provider behind this protocol is disabled: nothing to route to
			}
			protoOf[name] = append(protoOf[name], p)
			served[p] = true
			cands[name] = append(cands[name], snap.Models[p][name].Endpoints...)
		}
	}
	doc := manifestDoc{
		ManifestVersion: 1,
		Gateway:         manifestGateway{Kind: "vmr", Version: vmrVersion()},
		Endpoints:       map[string]string{},
		Auth:            manifestAuth{Required: len(snap.Cfg.APIKeys) > 0},
		Models:          []manifestModelDoc{},
	}
	for _, p := range manifestProtocolOrder {
		if !served[p] {
			continue
		}
		if p == core.ProtocolAnthropicMessages {
			doc.Endpoints[p] = base
		} else {
			doc.Endpoints[p] = base + "/v1"
		}
	}
	for _, name := range fmtutil.SortedKeys(protoOf) {
		m := manifestModelDoc{ID: name, Protocols: protoOf[name]}
		if meta := snap.Cfg.Models[name].Meta; meta != nil {
			m.DisplayName, m.Description = meta.DisplayName, meta.Description
			m.Roles = slices.Clone(meta.Roles)
		}
		fillCapabilities(&m, cands[name])
		doc.Models = append(doc.Models, m)
	}
	return doc
}

// fillCapabilities aggregates what the candidate endpoints DECLARE. "Routable" fields
// (limit.context, modalities, tool_call, reasoning) report that at least one candidate
// declares the ability; "guarantee" reports what holds on every candidate, and only
// exists when every candidate declared its capabilities at all.
func fillCapabilities(m *manifestModelDoc, eps []*core.Endpoint) {
	var maxCtx, minCtx int64
	allCtx := len(eps) > 0
	declared := 0
	union := map[string]bool{}
	var inter map[string]bool
	for _, ep := range eps {
		if ep.MaxContextTokens > 0 {
			maxCtx = max(maxCtx, ep.MaxContextTokens)
			if minCtx == 0 || ep.MaxContextTokens < minCtx {
				minCtx = ep.MaxContextTokens
			}
		} else {
			allCtx = false
		}
		if len(ep.Capabilities) == 0 {
			continue
		}
		declared++
		have := map[string]bool{}
		for _, c := range ep.Capabilities {
			have[c] = true
			union[c] = true
		}
		if inter == nil {
			inter = have
			continue
		}
		for c := range inter {
			if !have[c] {
				delete(inter, c)
			}
		}
	}
	if maxCtx > 0 {
		m.Limit = &manifestLimit{Context: maxCtx}
		if allCtx {
			m.Limit.ContextFloor = minCtx
		}
	}
	if declared == 0 {
		return
	}
	m.Modalities = &manifestModalities{Input: modalities(union)}
	if union["tools"] {
		t := true
		m.ToolCall = &t
	}
	if union["thinking"] {
		t := true
		m.Reasoning = &t
	}
	if declared == len(eps) {
		m.Guarantee = &manifestGuarantee{Input: modalities(inter), ToolCall: inter["tools"], Reasoning: inter["thinking"]}
	}
}

// modalities lists "text" plus the declared non-text inputs, in a fixed order.
func modalities(caps map[string]bool) []string {
	out := []string{"text"}
	for _, c := range []string{"image", "audio", "video"} {
		if caps[c] {
			out = append(out, c)
		}
	}
	return out
}
