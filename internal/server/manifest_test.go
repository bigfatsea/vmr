// Ver 2026-10-20, by Claude Sonnet 5.5

package server

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "vmr/internal/adapter/anthropic"
	_ "vmr/internal/adapter/openai"
	_ "vmr/internal/adapter/openairesponses"
)

const manifestYAML = `
listen: 127.0.0.1:18800
%s
providers:
  - {name: p1, base_url: {openai-completions: http://127.0.0.1:1/v1, anthropic-messages: http://127.0.0.1:1, openai-responses: http://127.0.0.1:1/v1}, api_key: k}
  - {name: p2, base_url: {openai-completions: http://127.0.0.1:2/v1}, api_key: k}
model_defaults:
  big:   {capabilities: [text, tools, image, thinking], max_context_tokens: 1000000}
  small: {capabilities: [text, tools], max_context_tokens: 200000}
  plain: {}
models:
  coding:
    meta: {display_name: "Coding", description: "main loop", roles: [default, subagent]}
    endpoints:
      openai-completions:
        - {providers: [p1], models: [big]}
        - {providers: [p2], models: [small]}
  claude:
    meta: {roles: [default]}
    endpoints:
      anthropic-messages:
        - {providers: [p1], models: [big]}
  agent:
    meta: {roles: [reasoning, review]}
    endpoints:
      anthropic-messages:   [{providers: [p1], models: [big]}]
      openai-completions:   [{providers: [p1], models: [big]}]
      openai-responses:     [{providers: [p1], models: [big]}]
  bare:
    endpoints:
      openai-completions:   [{providers: [p2], models: [plain]}]
`

func getManifest(t *testing.T, ts *httptest.Server, hdr map[string]string) (*http.Response, manifestDoc) {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+"/manifest", nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var doc manifestDoc
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
			t.Fatal(err)
		}
	}
	return resp, doc
}

func modelByID(t *testing.T, doc manifestDoc, id string) manifestModelDoc {
	t.Helper()
	for _, m := range doc.Models {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("model %q not in manifest", id)
	return manifestModelDoc{}
}

func TestManifestDerivedFromRoutingTable(t *testing.T) {
	ts := newRouterServer(t, strings.Replace(manifestYAML, "%s", "", 1))
	resp, doc := getManifest(t, ts, nil)
	if resp.StatusCode != 200 || doc.ManifestVersion != 1 || doc.Gateway.Kind != "vmr" {
		t.Fatalf("status %d doc %+v", resp.StatusCode, doc)
	}
	host := strings.TrimPrefix(ts.URL, "http://")
	if doc.Endpoints["openai-completions"] != "http://"+host+"/v1" ||
		doc.Endpoints["anthropic-messages"] != "http://"+host ||
		doc.Endpoints["openai-responses"] != "http://"+host+"/v1" {
		t.Fatalf("endpoints %v", doc.Endpoints)
	}
	if doc.Auth.Required {
		t.Fatal("auth.required without api_keys")
	}

	coding := modelByID(t, doc, "coding")
	if coding.DisplayName != "Coding" || coding.Description != "main loop" || strings.Join(coding.Roles, ",") != "default,subagent" {
		t.Fatalf("meta not published: %+v", coding)
	}
	if strings.Join(coding.Protocols, ",") != "openai-completions" {
		t.Fatalf("protocols %v", coding.Protocols)
	}
	// routable: best of any candidate; guarantee: what every candidate gives
	if coding.Limit == nil || coding.Limit.Context != 1000000 || coding.Limit.ContextFloor != 200000 {
		t.Fatalf("limit %+v", coding.Limit)
	}
	if coding.ToolCall == nil || !*coding.ToolCall || coding.Reasoning == nil || !*coding.Reasoning {
		t.Fatalf("routable abilities %v %v", coding.ToolCall, coding.Reasoning)
	}
	if strings.Join(coding.Modalities.Input, ",") != "text,image" {
		t.Fatalf("modalities %+v", coding.Modalities)
	}
	g := coding.Guarantee
	if g == nil || !g.ToolCall || g.Reasoning || strings.Join(g.Input, ",") != "text" {
		t.Fatalf("guarantee %+v (small lacks image and thinking)", g)
	}

	agent := modelByID(t, doc, "agent")
	if strings.Join(agent.Protocols, ",") != "openai-completions,anthropic-messages,openai-responses" {
		t.Fatalf("protocol order %v", agent.Protocols)
	}
}

func TestManifestOmitsWhatIsUnknown(t *testing.T) {
	ts := newRouterServer(t, strings.Replace(manifestYAML, "%s", "", 1))
	_, doc := getManifest(t, ts, nil)
	bare := modelByID(t, doc, "bare")
	b, _ := json.Marshal(bare)
	for _, key := range []string{"limit", "modalities", "tool_call", "reasoning", "guarantee", "roles", "display_name"} {
		if strings.Contains(string(b), `"`+key+`"`) {
			t.Fatalf("undeclared %q must be omitted, got %s", key, b)
		}
	}
}

func TestManifestETagAndNotModified(t *testing.T) {
	ts := newRouterServer(t, strings.Replace(manifestYAML, "%s", "", 1))
	resp, _ := getManifest(t, ts, nil)
	etag := resp.Header.Get("ETag")
	if etag == "" || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("headers %v", resp.Header)
	}
	for _, inm := range []string{etag, "W/" + etag, `"zzz", ` + etag, "*"} {
		r2, _ := getManifest(t, ts, map[string]string{"If-None-Match": inm})
		if r2.StatusCode != http.StatusNotModified {
			t.Fatalf("If-None-Match %q -> %d", inm, r2.StatusCode)
		}
	}
	if r3, _ := getManifest(t, ts, map[string]string{"If-None-Match": `"other"`}); r3.StatusCode != 200 {
		t.Fatalf("stale etag -> %d", r3.StatusCode)
	}
}

func TestManifestRequiresAPIKeyWhenConfigured(t *testing.T) {
	ts := newRouterServer(t, strings.Replace(manifestYAML, "%s", "api_keys:\n  - sk-vmr-secret-key01", 1))
	if r, _ := getManifest(t, ts, nil); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no key -> %d", r.StatusCode)
	}
	r, doc := getManifest(t, ts, map[string]string{"Authorization": "Bearer sk-vmr-secret-key01"})
	if r.StatusCode != 200 || !doc.Auth.Required {
		t.Fatalf("status %d auth %+v", r.StatusCode, doc.Auth)
	}
	body, _ := json.Marshal(doc)
	if strings.Contains(string(body), "sk-vmr-secret-key01") {
		t.Fatal("the manifest must never contain a key")
	}
}

func TestManifestPublicBaseURL(t *testing.T) {
	ts := newRouterServer(t, strings.Replace(manifestYAML, "%s", "public_base_url: https://vmr.team.lan", 1))
	_, doc := getManifest(t, ts, nil)
	if doc.Endpoints["anthropic-messages"] != "https://vmr.team.lan" || doc.Endpoints["openai-completions"] != "https://vmr.team.lan/v1" {
		t.Fatalf("endpoints %v", doc.Endpoints)
	}
}

func TestManifestOnlyListsProtocolsInUse(t *testing.T) {
	const y = `
listen: 127.0.0.1:18800
providers:
  - {name: p1, base_url: {openai-completions: http://127.0.0.1:1/v1}, api_key: k}
models:
  m: {endpoints: {openai-completions: [{providers: [p1], models: [x]}]}}
`
	_, doc := getManifest(t, newRouterServer(t, y), nil)
	if len(doc.Endpoints) != 1 || doc.Endpoints["openai-completions"] == "" {
		t.Fatalf("endpoints %v", doc.Endpoints)
	}
}

func TestModelsListReportsEveryProtocolAndMeta(t *testing.T) {
	ts := newRouterServer(t, strings.Replace(manifestYAML, "%s", "", 1))
	resp, err := http.Get(ts.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	for _, m := range out.Data {
		if m["id"] == "agent" {
			ps, _ := m["vmr_protocols"].([]any)
			if len(ps) != 3 || m["vmr_protocol"] == nil {
				t.Fatalf("agent entry %v", m)
			}
		}
		if m["id"] == "coding" && m["display_name"] != "Coding" {
			t.Fatalf("coding entry %v", m)
		}
	}
}

func TestManifestRoleUniquePerProtocol(t *testing.T) {
	_, doc := getManifest(t, newRouterServer(t, strings.Replace(manifestYAML, "%s", "", 1)), nil)
	seen := map[string]string{}
	for _, m := range doc.Models {
		for _, p := range m.Protocols {
			for _, r := range m.Roles {
				if prev, dup := seen[p+"/"+r]; dup {
					t.Fatalf("role %s on %s claimed by %s and %s", r, p, prev, m.ID)
				}
				seen[p+"/"+r] = m.ID
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("fixture published no roles; the uniqueness check proved nothing")
	}
}

func TestManifestSkipsProtocolsWithNoEnabledEndpoint(t *testing.T) {
	const y = `
listen: 127.0.0.1:18800
providers:
  - {name: up, base_url: {openai-completions: http://127.0.0.1:1/v1}, api_key: k}
  - {name: down, base_url: {anthropic-messages: http://127.0.0.1:1}, api_key: k, disabled: true}
models:
  m:
    endpoints:
      openai-completions: [{providers: [up], models: [x]}]
      anthropic-messages: [{providers: [down], models: [x]}]
`
	_, doc := getManifest(t, newRouterServer(t, y), nil)
	if _, ok := doc.Endpoints["anthropic-messages"]; ok || len(doc.Endpoints) != 1 {
		t.Fatalf("endpoints %v", doc.Endpoints)
	}
	if got := modelByID(t, doc, "m").Protocols; len(got) != 1 || got[0] != "openai-completions" {
		t.Fatalf("protocols %v", got)
	}
}

func TestManifestWarnsOnceBehindProxyWithoutPublicBaseURL(t *testing.T) {
	var buf bytes.Buffer
	ts := newRouterServerTuned(t, strings.Replace(manifestYAML, "%s", "", 1), func(s *Server) { s.rt.Logger = log.New(&buf, "", 0) })
	hdr := map[string]string{"X-Forwarded-Proto": "https"}
	_, doc := getManifest(t, ts, hdr)
	getManifest(t, ts, hdr)
	if !strings.HasPrefix(doc.Endpoints["anthropic-messages"], "http://") {
		t.Fatalf("forwarded headers must not be trusted: %v", doc.Endpoints)
	}
	if n := strings.Count(buf.String(), "public_base_url is unset"); n != 1 {
		t.Fatalf("warned %d times: %q", n, buf.String())
	}
	buf.Reset()
	getManifest(t, newRouterServerTuned(t, strings.Replace(manifestYAML, "%s", "public_base_url: https://vmr.team.lan", 1), func(s *Server) { s.rt.Logger = log.New(&buf, "", 0) }), hdr)
	if buf.Len() != 0 {
		t.Fatalf("warned although public_base_url is set: %q", buf.String())
	}
}
