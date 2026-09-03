// Ver 2026-07-30, by Sonnet 5
package replay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/ctxgraph"

	_ "vmr/internal/adapter/openai"
	_ "vmr/internal/adapter/openairesponses"
)

// writeConfig writes a minimal one-provider, one-model config.yaml pointing
// at upstreamURL and returns its path. When withModel is false, the virtual
// model "vm" is omitted so -model resolution must fail (see
// TestRun_ModelResolutionError).
func writeConfig(t *testing.T, dir, upstreamURL string, withModel bool) string {
	t.Helper()
	models := ""
	if withModel {
		models = `
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [upstream-model]}
`
	} else {
		models = `
models:
  other:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [upstream-model]}
`
	}
	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %q}, api_key: real-provider-key}
`, upstreamURL+"/v1") + models
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeAuditLine marshals rec as one audit JSONL line into a new file under
// dir and returns its path.
func writeAuditLine(t *testing.T, dir, name string, recs ...*audit.Record) string {
	t.Helper()
	var buf bytes.Buffer
	for _, r := range recs {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func chatRecord(virtualModel, content string) *audit.Record {
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":%q}]}`, virtualModel, content)
	return &audit.Record{
		Model:    virtualModel,
		Protocol: "openai-completions",
		Stream:   false,
		Client: audit.Exchange{
			Request: audit.Message{
				Method: http.MethodPost, Path: "/v1/chat/completions",
				Headers: http.Header{"User-Agent": {"test-agent"}, "Authorization": {"Bearer clientkey"}},
				Body:    audit.EncodeBody([]byte(body)),
			},
		},
	}
}

// chatRecordAt is chatRecord with an explicit arrival timestamp, for the
// -ts locator tests.
func chatRecordAt(ts time.Time, virtualModel, content string) *audit.Record {
	rec := chatRecord(virtualModel, content)
	rec.TS = ts
	return rec
}

func TestRun_DryRunDoesNotHitNetwork(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hello"))

	var out bytes.Buffer
	err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", DryRun: true,
	}, &out)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "DRY-RUN") || !strings.Contains(got, "upstream-model") {
		t.Errorf("dry-run output missing expected markers: %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("dry-run output should echo the original body content: %q", got)
	}
	// The client's masked credential must never appear verbatim in the
	// printed request (it's replaced by the real provider key at BuildRequest
	// time, and even that must be redacted before printing).
	if strings.Contains(got, "clientkey") || strings.Contains(got, "real-provider-key") {
		t.Errorf("dry-run output leaked a credential: %q", got)
	}
}

func TestRun_RealReplayRewritesModelAndInjectsCredentials(t *testing.T) {
	dir := t.TempDir()
	var gotAuth string
	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp1","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"hello back"}}]}`)
	}))
	defer upstream.Close()

	cfgPath := writeConfig(t, dir, upstream.URL, true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi there"))

	var out bytes.Buffer
	err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1",
	}, &out)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotModel != "upstream-model" {
		t.Errorf("upstream saw model=%q, want upstream-model (virtual name should be rewritten)", gotModel)
	}
	if gotAuth != "Bearer real-provider-key" {
		t.Errorf("upstream saw Authorization=%q, want the provider's real key, not the client's", gotAuth)
	}
	if !strings.Contains(out.String(), "hello back") {
		t.Errorf("stdout missing upstream response body: %q", out.String())
	}
	if !strings.Contains(out.String(), "200") {
		t.Errorf("stdout missing status line: %q", out.String())
	}
}

// TestRun_RealReplayOpenAIResponsesProtocol proves vmr replay needs zero
// protocol-specific code to support a third protocol: it goes through the
// exact same adapter.BuildRequest/router.IngressPath path live traffic
// does (see replay.go's doc comment), both of which are already protocol-
// generic — so an openai-responses audit record replays correctly with no
// changes to this package at all.
func TestRun_RealReplayOpenAIResponsesProtocol(t *testing.T) {
	dir := t.TempDir()
	var gotPath string
	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		gotModel, _ = body["model"].(string)
		if _, hasMessages := body["messages"]; hasMessages {
			t.Errorf("replayed body must stay Responses-shaped (input, not messages): %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp1","model":"upstream-model","output":[]}`)
	}))
	defer upstream.Close()

	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-responses: %q}, api_key: real-provider-key}
models:
  vm:
    endpoints:
      - {protocol: openai-responses, providers: [p1], models: [upstream-model]}
`, upstream.URL)
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := &audit.Record{
		Model: "vm", Protocol: "openai-responses", Stream: false,
		Client: audit.Exchange{
			Request: audit.Message{
				Method: http.MethodPost, Path: "/v1/responses",
				Headers: http.Header{"Authorization": {"Bearer clientkey"}},
				Body:    audit.EncodeBody([]byte(`{"model":"vm","input":"hi there"}`)),
			},
		},
	}
	auditPath := writeAuditLine(t, dir, "audit.jsonl", rec)

	var out bytes.Buffer
	err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1",
	}, &out)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotPath != "/responses" {
		t.Errorf("upstream path=%q, want /responses", gotPath)
	}
	if gotModel != "upstream-model" {
		t.Errorf("upstream saw model=%q, want upstream-model (virtual name should be rewritten)", gotModel)
	}
}

// TestRun_StripsMaskedCredentialLikeHeaders locks in the fix for a real
// leak: a header that audit.Redact masks (e.g. "Api-Key") but
// server.headerBlocklist doesn't block from forwarding on live traffic
// (since live headers are always real) must still be stripped when
// replaying, because the stored value is a placeholder like "***abcd", not
// a real credential — forwarding it verbatim would send garbage to the
// upstream.
func TestRun_StripsMaskedCredentialLikeHeaders(t *testing.T) {
	dir := t.TempDir()
	var gotAPIKeyHeader string
	var sawHeader bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKeyHeader, sawHeader = r.Header.Get("Api-Key"), r.Header.Get("Api-Key") != ""
		fmt.Fprint(w, `{"id":"r","model":"upstream-model","choices":[]}`)
	}))
	defer upstream.Close()
	cfgPath := writeConfig(t, dir, upstream.URL, true)

	rec := chatRecord("vm", "hi")
	rec.Client.Request.Headers = http.Header{
		"User-Agent": {"test-agent"},
		"Api-Key":    {"***abcd"}, // what audit.Redact would have left behind for a real "Api-Key" header
	}
	auditPath := writeAuditLine(t, dir, "audit.jsonl", rec)

	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1",
	}, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sawHeader {
		t.Errorf("upstream received Api-Key=%q, want the masked placeholder stripped entirely", gotAPIKeyHeader)
	}
}

func TestRun_ReadsCompressedAuditFile(t *testing.T) {
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"r","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer upstream.Close()

	cfgPath := writeConfig(t, dir, upstream.URL, true)

	line, err := json.Marshal(chatRecord("vm", "zst test"))
	if err != nil {
		t.Fatal(err)
	}
	zstPath := filepath.Join(dir, "audit.jsonl.zst")
	f, err := os.OpenFile(zstPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	enc.Write(append(line, '\n'))
	enc.Close()
	f.Close()

	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: zstPath, Provider: "p1",
	}, &out); err != nil {
		t.Fatalf("Run on .zst input: %v", err)
	}
	if !strings.Contains(out.String(), `"ok"`) {
		t.Errorf("stdout missing upstream response: %q", out.String())
	}
}

func TestRun_LineSelection(t *testing.T) {
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		msgs, _ := body["messages"].([]any)
		var content string
		if len(msgs) > 0 {
			m, _ := msgs[0].(map[string]any)
			content, _ = m["content"].(string)
		}
		fmt.Fprintf(w, `{"id":"r","model":"upstream-model","choices":[{"message":{"role":"assistant","content":%q}}]}`, "echo:"+content)
	}))
	defer upstream.Close()
	cfgPath := writeConfig(t, dir, upstream.URL, true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl",
		chatRecord("vm", "record-1"), chatRecord("vm", "record-2"), chatRecord("vm", "record-3"))

	// Default (Line: 0) picks the last record.
	var out bytes.Buffer
	if err := Run(context.Background(), Options{ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1"}, &out); err != nil {
		t.Fatalf("Run (default line): %v", err)
	}
	if !strings.Contains(out.String(), "echo:record-3") {
		t.Errorf("default line should replay the last record; got %q", out.String())
	}

	// Explicit -line 2 picks the second record.
	out.Reset()
	if err := Run(context.Background(), Options{ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", Line: 2}, &out); err != nil {
		t.Fatalf("Run (line=2): %v", err)
	}
	if !strings.Contains(out.String(), "echo:record-2") {
		t.Errorf("line=2 should replay the second record; got %q", out.String())
	}
}

func TestRun_SkipsMalformedTrailingLine(t *testing.T) {
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"r","model":"upstream-model","choices":[]}`)
	}))
	defer upstream.Close()
	cfgPath := writeConfig(t, dir, upstream.URL, true)

	good, err := json.Marshal(chatRecord("vm", "last-good"))
	if err != nil {
		t.Fatal(err)
	}
	auditPath := filepath.Join(dir, "audit.jsonl")
	content := string(good) + "\n" + "{not valid json\n"
	if err := os.WriteFile(auditPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := Run(context.Background(), Options{ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", DryRun: true}, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "last-good") {
		t.Errorf("should fall back to the last parsable line; got %q", out.String())
	}
}

func TestRun_RejectsNonObjectBody(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	rec := &audit.Record{
		Model: "vm", Protocol: "openai-completions",
		Client: audit.Exchange{Request: audit.Message{Body: audit.EncodeBody([]byte("not-json-and-not-object"))}},
	}
	auditPath := writeAuditLine(t, dir, "audit.jsonl", rec)

	var out bytes.Buffer
	err := Run(context.Background(), Options{ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", DryRun: true}, &out)
	if err == nil {
		t.Fatal("expected an error for a non-JSON-object body, got nil")
	}
	if !strings.Contains(err.Error(), "not a JSON object") {
		t.Errorf("error = %v, want a message about the body not being a JSON object", err)
	}
}

// TestResolveRoleMap_ReturnsNilByDefault covers the common case: no role_map
// configured means the endpoint must carry nil (not an empty map), so
// BuildRequest skips the role rewrite — same as live traffic.
func TestResolveRoleMap_ReturnsNilByDefault(t *testing.T) {
	cfg, err := config.Parse([]byte(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://a.example/v1}, api_key: k}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [m]}
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveRoleMap(cfg, "openai-completions", "vm", "p1"); got != nil {
		t.Errorf("resolveRoleMap = %v, want nil", got)
	}
}

func TestResolveRoleMap_ReturnsConfiguredMap(t *testing.T) {
	cfg, err := config.Parse([]byte(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://a.example/v1}, api_key: k}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [m], role_map: {developer: system}}
`))
	if err != nil {
		t.Fatal(err)
	}
	got := resolveRoleMap(cfg, "openai-completions", "vm", "p1")
	if got == nil {
		t.Fatal("resolveRoleMap = nil, want non-nil map")
	}
	if got["developer"] != "system" || len(got) != 1 {
		t.Errorf("resolveRoleMap = %v, want {developer: system}", got)
	}
}

func TestResolveRoleMap_UnknownProtocolReturnsNil(t *testing.T) {
	cfg, err := config.Parse([]byte(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://a.example/v1}, api_key: k}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [m], role_map: {developer: system}}
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveRoleMap(cfg, "anthropic-messages", "vm", "p1"); got != nil {
		t.Errorf("resolveRoleMap = %v, want nil for non-matching protocol", got)
	}
}

func TestResolveRoleMap_UnknownProviderReturnsNil(t *testing.T) {
	cfg, err := config.Parse([]byte(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://a.example/v1}, api_key: k}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [m], role_map: {developer: system}}
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveRoleMap(cfg, "openai-completions", "vm", "ghost"); got != nil {
		t.Errorf("resolveRoleMap = %v, want nil for non-matching provider", got)
	}
}

// TestBuildReplayEndpoint_CarriesRoleMap proves the endpoint buildReplayEndpoint
// assembles carries the matching EndpointGroup's role_map — the fix for a
// real gap: ep is hand-built here (never passing through BuildSnapshot, the
// one place live endpoints get their RoleMap copied on), so a configured
// role_map used to be silently dropped and the replayed request would fail
// upstream on an unrewritten role. Replay must be byte-identical to live
// traffic for a role-mapped config too.
func TestBuildReplayEndpoint_CarriesRoleMap(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	yaml := `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://api.example.com/v1}, api_key: real-provider-key}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [upstream-model], role_map: {developer: system}}
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ad, protocol, _, ep, err := buildReplayEndpoint(cfg, Options{Provider: "p1"}, &recordView{Model: "vm", Protocol: "openai-completions"})
	if err != nil {
		t.Fatalf("buildReplayEndpoint: %v", err)
	}
	if ad == nil || protocol != "openai-completions" {
		t.Fatalf("adapter/protocol = %v/%q", ad != nil, protocol)
	}
	if ep.RoleMap == nil {
		t.Fatal("ep.RoleMap = nil, want {developer: system}")
	}
	if ep.RoleMap["developer"] != "system" || len(ep.RoleMap) != 1 {
		t.Errorf("ep.RoleMap = %v, want {developer: system}", ep.RoleMap)
	}
}

func TestRun_ModelResolutionError(t *testing.T) {
	dir := t.TempDir()
	// withModel=false: config only defines virtual model "other", not "vm".
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", false)
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))

	var out bytes.Buffer
	err := Run(context.Background(), Options{ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", DryRun: true}, &out)
	if err == nil {
		t.Fatal("expected a model-resolution error, got nil")
	}
	if !strings.Contains(err.Error(), "-model") {
		t.Errorf("error = %v, want a hint to pass -model explicitly", err)
	}
}

// TestResolveModel_MatchesProviderWithinMultiProviderEntry pins that
// resolveModel matches any provider in a multi-provider entry, not just
// the first.
func TestResolveModel_MatchesProviderWithinMultiProviderEntry(t *testing.T) {
	cfg, err := config.Parse([]byte(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://a.example/v1}, api_key: k}
  - {name: p2, base_url: {openai-completions: https://b.example/v1}, api_key: k}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1, p2], models: [upstream-model]}
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"p1", "p2"} {
		model, err := resolveModel(cfg, "openai-completions", "vm", provider)
		if err != nil {
			t.Errorf("provider %q: resolveModel() error = %v", provider, err)
		}
		if model != "upstream-model" {
			t.Errorf("provider %q: resolveModel() = %q, want upstream-model", provider, model)
		}
	}
	if _, err := resolveModel(cfg, "openai-completions", "vm", "ghost"); err == nil {
		t.Error("provider not in the entry's providers list: want an error, got nil")
	}
}

func TestRun_RequiresProvider(t *testing.T) {
	if err := Run(context.Background(), Options{ConfigPath: "x", AuditPath: "y"}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected an error when -provider is empty")
	}
}

// TestRun_DurMSCountsFullBodyTransfer locks in the fix for a real timing
// bug: DurMS must measure total wall time (matching server.go/router.go's
// meaning of the field), not just time-to-headers. A handler that flushes
// headers immediately and only then sleeps before writing the body would,
// under the old (pre-io.Copy) measurement point, record a near-zero
// duration despite the replay actually taking >=50ms end to end.
func TestRun_DurMSCountsFullBodyTransfer(t *testing.T) {
	const bodyDelay = 50 * time.Millisecond
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush() // headers (and any flushed prefix) land on the wire now
		}
		time.Sleep(bodyDelay) // ...but the body only shows up after this
		fmt.Fprint(w, `{"id":"r","model":"upstream-model","choices":[]}`)
	}))
	defer upstream.Close()
	cfgPath := writeConfig(t, dir, upstream.URL, true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))
	recordPath := filepath.Join(dir, "replay-out.jsonl")

	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", RecordPath: recordPath,
	}, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var rec audit.Record
	if err := json.Unmarshal(bytes.TrimSpace(data), &rec); err != nil {
		t.Fatalf("replay record is not valid JSON: %v\n%s", err, data)
	}
	wantMin := bodyDelay.Milliseconds()
	if rec.DurMS < wantMin {
		t.Errorf("DurMS = %d, want >= %d (must count the body-transfer delay, not just time-to-headers)", rec.DurMS, wantMin)
	}
	if rec.Attempts[0].DurMS < wantMin {
		t.Errorf("Attempts[0].DurMS = %d, want >= %d", rec.Attempts[0].DurMS, wantMin)
	}
}

func TestRun_WritesReplayRecord(t *testing.T) {
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond) // guarantee a measurable, non-zero duration below
		fmt.Fprint(w, `{"id":"r","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer upstream.Close()
	cfgPath := writeConfig(t, dir, upstream.URL, true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))
	recordPath := filepath.Join(dir, "replay-out.jsonl")

	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", RecordPath: recordPath,
	}, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var rec audit.Record
	if err := json.Unmarshal(bytes.TrimSpace(data), &rec); err != nil {
		t.Fatalf("replay record is not valid JSON: %v\n%s", err, data)
	}
	if rec.Outcome != "ok" {
		t.Errorf("Outcome = %q, want ok", rec.Outcome)
	}
	if !strings.HasPrefix(rec.ReplayOf, auditPath+":") {
		t.Errorf("ReplayOf = %q, want prefix %q", rec.ReplayOf, auditPath+":")
	}
	if len(rec.Attempts) != 1 || rec.Attempts[0].Provider != "p1" {
		t.Errorf("Attempts = %+v, want one attempt against provider p1", rec.Attempts)
	}
	if rec.Attempts[0].Response == nil || rec.Attempts[0].Response.Status != http.StatusOK {
		t.Errorf("Attempts[0].Response = %+v, want status 200", rec.Attempts[0].Response)
	}
	if rec.DurMS <= 0 || rec.Attempts[0].DurMS <= 0 {
		t.Errorf("DurMS = %d, Attempts[0].DurMS = %d, want both > 0 (upstream deliberately took >=5ms)", rec.DurMS, rec.Attempts[0].DurMS)
	}
	// On a successful attempt the body is omitted (byte-identical to
	// Client.Response.Body) — same convention router.go's tryOne uses for
	// live traffic.
	if rec.Attempts[0].Response.Body != nil {
		t.Errorf("Attempts[0].Response.Body = %v, want nil on success (mirrors live-traffic convention)", rec.Attempts[0].Response.Body)
	}
	if rec.Client.Request.Path != "/v1/chat/completions" {
		t.Errorf("Client.Request.Path = %q, want the ingress path, not the upstream one", rec.Client.Request.Path)
	}
	// Client.Request.Body must be the pre-rewrite body actually replayed
	// (virtual model name "vm"), not the rewritten outbound body
	// ("upstream-model") that belongs on the Attempt.
	reqBody, ok := rec.Client.Request.Body.(map[string]any)
	if !ok || reqBody["model"] != "vm" {
		t.Errorf("Client.Request.Body = %#v, want the pre-rewrite body with model=\"vm\"", rec.Client.Request.Body)
	}
	if rec.Client.Response == nil || rec.Client.Response.Status != http.StatusOK {
		t.Errorf("Client.Response = %+v, want status 200", rec.Client.Response)
	} else if body, _ := rec.Client.Response.Body.(map[string]any); body["id"] != "r" {
		t.Errorf("Client.Response.Body = %#v, want the upstream response body", rec.Client.Response.Body)
	}
}

// --- -ts locator ---

func TestLoadRecordByTS_MatchesBothNanosecondAndMillisecondForms(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 7, 13, 15, 30, 42, 123456789, time.FixedZone("+08:00", 8*3600))
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecordAt(ts, "vm", "target"))

	// The exact nanosecond string as it appears in the raw audit line.
	nano := ts.Format(time.RFC3339Nano)
	rv, _, err := loadRecordByTS(auditPath, nano)
	if err != nil {
		t.Fatalf("loadRecordByTS(nano): %v", err)
	}
	if !strings.Contains(string(rv.Client.Request.Body), "target") {
		t.Errorf("wrong record matched via nanosecond ts: %s", rv.Client.Request.Body)
	}

	// A millisecond-truncated ts, e.g. one a user hand-copied and rounded —
	// loadRecordByTS matches at millisecond resolution, coarser than
	// vmr-requests.json's own whole-second "ts" column actually needs (see
	// aggregate.go's buildRequestRow), so this must still resolve uniquely.
	milli := ts.Format("2006-01-02T15:04:05.000Z07:00")
	rv, _, err = loadRecordByTS(auditPath, milli)
	if err != nil {
		t.Fatalf("loadRecordByTS(milli): %v", err)
	}
	if !strings.Contains(string(rv.Client.Request.Body), "target") {
		t.Errorf("wrong record matched via millisecond ts: %s", rv.Client.Request.Body)
	}
}

func TestLoadRecordByTS_NoMatch(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 7, 13, 15, 30, 42, 0, time.UTC)
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecordAt(ts, "vm", "hi"))

	other := ts.Add(time.Hour).Format(time.RFC3339)
	if _, _, err := loadRecordByTS(auditPath, other); err == nil {
		t.Error("expected an error for a ts with no matching record")
	}
}

func TestLoadRecordByTS_AmbiguousWithinSameMillisecond(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 7, 13, 15, 30, 42, 100000000, time.UTC) // .100000000
	a := chatRecordAt(base, "vm", "first")
	b := chatRecordAt(base.Add(500*time.Microsecond), "vm", "second") // still .100xxx ms
	auditPath := writeAuditLine(t, dir, "audit.jsonl", a, b)

	_, _, err := loadRecordByTS(auditPath, base.Format("2006-01-02T15:04:05.000Z07:00"))
	if err == nil {
		t.Fatal("expected an ambiguous-match error")
	}
	if !strings.Contains(err.Error(), "-line") {
		t.Errorf("error = %v, want a hint to use -line to disambiguate", err)
	}
}

func TestLoadRecordByTS_BadFormat(t *testing.T) {
	dir := t.TempDir()
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))
	if _, _, err := loadRecordByTS(auditPath, "not-a-timestamp"); err == nil {
		t.Error("expected an error for a malformed -ts value")
	}
}

func TestRun_TSSelectsRecord(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 7, 13, 15, 30, 42, 250000000, time.UTC)
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl",
		chatRecordAt(ts.Add(-time.Second), "vm", "not-this-one"),
		chatRecordAt(ts, "vm", "this-one"))

	var out bytes.Buffer
	err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", DryRun: true,
		TS: ts.Format("2006-01-02T15:04:05.000Z07:00"),
	}, &out)
	if err != nil {
		t.Fatalf("Run -ts: %v", err)
	}
	if !strings.Contains(out.String(), "this-one") || strings.Contains(out.String(), "not-this-one") {
		t.Errorf("dry-run output = %q, want only the record matching -ts", out.String())
	}
}

func TestRun_LineAndTSMutuallyExclusive(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))
	err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", DryRun: true,
		Line: 1, TS: "2026-07-13T15:30:42.000Z",
	}, &bytes.Buffer{})
	if err == nil {
		t.Error("expected an error when -line and -ts are both set")
	}
}

// --- -req locator ---

func TestRun_ReqSelectsRecord(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl",
		chatRecord("vm", "not-this-one"), chatRecord("vm", "this-one"))

	var out bytes.Buffer
	err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", DryRun: true,
		Req: ctxgraph.ReqCoord(auditPath, 2),
	}, &out)
	if err != nil {
		t.Fatalf("Run -req: %v", err)
	}
	if !strings.Contains(out.String(), "this-one") || strings.Contains(out.String(), "not-this-one") {
		t.Errorf("dry-run output = %q, want only the record at line 2", out.String())
	}
}

func TestRun_ReqRejectsMismatchedBasename(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))

	err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", DryRun: true,
		Req: "some-other-file.jsonl:1",
	}, &bytes.Buffer{})
	if err == nil {
		t.Error("expected an error when -req's basename doesn't match the audit file argument")
	}
}

func TestRun_ReqRejectsMalformedCoordinate(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))

	err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", DryRun: true, Req: "not-a-coordinate",
	}, &bytes.Buffer{})
	if err == nil {
		t.Error("expected an error for a malformed -req coordinate")
	}
}

func TestRun_ReqMutuallyExclusiveWithLineAndTS(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))
	req := ctxgraph.ReqCoord(auditPath, 1)

	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", DryRun: true, Req: req, Line: 1,
	}, &bytes.Buffer{}); err == nil {
		t.Error("expected an error when -req and -line are both set")
	}
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", DryRun: true, Req: req, TS: "2026-07-13T15:30:42.000Z",
	}, &bytes.Buffer{}); err == nil {
		t.Error("expected an error when -req and -ts are both set")
	}
}

func TestRun_MissingLocatorRequiresAuditPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	err := Run(context.Background(), Options{ConfigPath: cfgPath, Provider: "p1", DryRun: true}, &bytes.Buffer{})
	if err == nil {
		t.Error("expected an error when no audit file argument is given")
	}
}

// --- -print ---

func TestRunPrint_OutputsRawRecordBytes(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))

	var out bytes.Buffer
	err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Print: true, Line: 1,
	}, &out)
	if err != nil {
		t.Fatalf("Run -print: %v", err)
	}
	raw, err := audit.LineAt(auditPath, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSuffix(out.String(), "\n"); got != string(raw) {
		t.Errorf("-print output = %q, want the raw line %q", got, raw)
	}
}

func TestRunPrint_DoesNotRequireProvider(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))

	err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Print: true, Line: 1,
	}, &bytes.Buffer{})
	if err != nil {
		t.Errorf("Run -print without -provider: %v, want no error", err)
	}
}

func TestRunPrint_WithReq(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "first"), chatRecord("vm", "second"))

	var out bytes.Buffer
	err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Print: true, Req: ctxgraph.ReqCoord(auditPath, 2),
	}, &out)
	if err != nil {
		t.Fatalf("Run -print -req: %v", err)
	}
	if !strings.Contains(out.String(), "second") || strings.Contains(out.String(), "first") {
		t.Errorf("-print -req output = %q, want only line 2's record", out.String())
	}
}

// TestRun_StreamOverrideRewritesBody locks in that -stream changes the bytes
// the upstream actually receives (the body's own top-level "stream" field),
// not just replay-local bookkeeping — the flag used to be silently inert.
func TestRun_StreamOverrideRewritesBody(t *testing.T) {
	dir := t.TempDir()
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = nil
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp1","choices":[]}`)
	}))
	defer upstream.Close()

	cfgPath := writeConfig(t, dir, upstream.URL, true)
	rec := chatRecord("vm", "hi") // record body has no "stream" key; rec.Stream=false
	auditPath := writeAuditLine(t, dir, "audit.jsonl", rec)

	streamOn := true
	recordPath := filepath.Join(dir, "replay-record.jsonl")
	var out bytes.Buffer
	err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1",
		Stream: &streamOn, RecordPath: recordPath,
	}, &out)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if v, ok := gotBody["stream"].(bool); !ok || !v {
		t.Errorf(`upstream body "stream" = %v, want true (flag must rewrite the body)`, gotBody["stream"])
	}
	// The -record line reflects the request as replayed, override included.
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var written audit.Record
	if err := json.Unmarshal(bytes.TrimSpace(data), &written); err != nil {
		t.Fatal(err)
	}
	if !written.Stream {
		t.Error("-record line must carry the overridden stream=true")
	}
	if !strings.Contains(string(data), `\"stream\":true`) && !strings.Contains(string(data), `"stream":true`) {
		t.Errorf("-record client body missing rewritten stream field: %s", data)
	}

	// Explicit -stream false against a body that already says true.
	rec2 := chatRecord("vm", "hi2")
	rec2.Stream = true
	rec2.Client.Request.Body = audit.EncodeBody([]byte(`{"model":"vm","stream":true,"messages":[{"role":"user","content":"hi2"}]}`))
	auditPath2 := writeAuditLine(t, dir, "audit2.jsonl", rec2)
	streamOff := false
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath2, Provider: "p1", Stream: &streamOff,
	}, &out); err != nil {
		t.Fatalf("Run(off): %v", err)
	}
	if v, ok := gotBody["stream"].(bool); !ok || v {
		t.Errorf(`upstream body "stream" = %v, want false`, gotBody["stream"])
	}
}

// TestRun_ReplayRecordIsByteFaithful locks in the fix for P-2-1's replay
// half: writeReplayRecord must serialize the record with
// SetEscapeHTML(false), the same audit.Write does, so < > & in a
// request/response body land on disk as literal bytes rather than the
// \uXXXX forms json.Marshal would otherwise produce. The chatmsg-typing-
// tests' bodies routinely contain comparisons like `1 < 2 && 3 > 2`, and a
// recorded body that has been escape-mangled no longer equals the bytes
// the upstream actually received, breaking the same byte-faithful invariant
// CLAUDE.md requires of every other vmr→disk write.
func TestRun_ReplayRecordIsByteFaithful(t *testing.T) {
	dir := t.TempDir()
	// The JSON-encoded form of the body — what we expect to see verbatim on disk.
	// The interior quotes of `"ok"` are escaped by JSON's encoder to `\"ok\"`,
	// so that's what we look for, not the bare string.
	const lit = `if 1 < 2 && 3 > 2 then \"ok\"`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wrap the literal in a valid JSON body so audit.EncodeBody treats
		// it as a JSON object (the struct path falls back to string-y
		// storage and bypasses the no-escape rewrite). The content field
		// itself is what we want preserved byte-for-byte on disk.
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"r","model":"upstream-model","choices":[{"message":{"role":"assistant","content":%q}}]}`, `if 1 < 2 && 3 > 2 then "ok"`)
	}))
	defer upstream.Close()
	cfgPath := writeConfig(t, dir, upstream.URL, true)
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))
	recordPath := filepath.Join(dir, "replay-out.jsonl")

	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", RecordPath: recordPath,
	}, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	// < > & must appear literally in the file — exactly what audit.Write
	// would have written. json.Marshal would have rewritten them to
	// \u003c / \u003e / \u0026, so a raw byte check catches the bug.
	if !strings.Contains(string(data), lit) {
		t.Errorf("replay record must preserve literal <, >, & in response body (byte-faithful passthrough broken):\n%s", data)
	}
	// json.Marshal's default would rewrite these to \u003c / \u003e /
	// \u0026 — assert none of those escape forms leaked in, since the file
	// IS supposed to carry literal < > &.
	for _, esc := range []string{`\u003c`, `\u003e`, `\u0026`} {
		if strings.Contains(string(data), esc) {
			t.Errorf("replay record HTML-escaped %s — byte-faithful passthrough broken:\n%s", esc, data)
		}
	}

	// Still valid JSON, still reports ok.
	var rec audit.Record
	if err := json.Unmarshal(bytes.TrimSpace(data), &rec); err != nil {
		t.Fatalf("replay record is not valid JSON after no-escape write: %v\n%s", err, data)
	}
	if rec.Outcome != "ok" {
		t.Errorf("Outcome = %q, want ok", rec.Outcome)
	}
}

// --- image downscale on replay ---

// bigImageDataURL renders a solid w×h JPEG (deterministic content, real
// decodable header) and returns it as a "data:image/jpeg;base64,..." URI —
// the inline shape openai-completions requests carry in an image_url block.
func bigImageDataURL(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// imageRecord is an openai-completions audit record whose body carries one
// inline image block (plus a text block, so the message is multimodal the
// way real vision requests are).
func imageRecord(t *testing.T, virtualModel, dataURL string) *audit.Record {
	t.Helper()
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":%q}}]}]}`,
		virtualModel, dataURL)
	rec := chatRecord(virtualModel, "ignored")
	rec.Client.Request.Body = audit.EncodeBody([]byte(body))
	return rec
}

// upstreamImageURL extracts the image data URL the upstream actually
// received from a replayed request body.
func upstreamImageURL(t *testing.T, body map[string]any) string {
	t.Helper()
	msgs, _ := body["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatal("upstream body has no messages")
	}
	content, _ := msgs[0].(map[string]any)["content"].([]any)
	for _, b := range content {
		block, _ := b.(map[string]any)
		if block["type"] == "image_url" {
			iu, _ := block["image_url"].(map[string]any)
			u, _ := iu["url"].(string)
			return u
		}
	}
	t.Fatal("upstream body has no image_url block")
	return ""
}

// TestRun_DownscalesImagesLikeLivePath locks in the fix: the audit trail
// stores the client's ORIGINAL body (server.go records it before
// downscaleImages runs), so a replay that forwarded it verbatim shipped the
// pre-downscale multi-megabyte image live traffic never sent. Replay must
// apply the same downscaling, with the same MaxPx resolution, so what the
// upstream receives is byte-comparable to what the original request sent.
func TestRun_DownscalesImagesLikeLivePath(t *testing.T) {
	dir := t.TempDir()
	dataURL := bigImageDataURL(t, 300, 150) // long side 300, global cap below is 100

	var gotURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		gotURL = upstreamImageURL(t, body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"r","model":"upstream-model","choices":[]}`)
	}))
	defer upstream.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
image_downscale: 100
providers:
  - {name: p1, base_url: {openai-completions: %q}, api_key: real-provider-key}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [upstream-model]}
`, upstream.URL)
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	auditPath := writeAuditLine(t, dir, "audit.jsonl", imageRecord(t, "vm", dataURL))
	recordPath := filepath.Join(dir, "replay-out.jsonl")

	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", RecordPath: recordPath,
	}, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The upstream must receive a downscaled JPEG, not the original image.
	if !strings.HasPrefix(gotURL, "data:image/jpeg;base64,") {
		t.Fatalf("upstream image url = %.80q..., want a data:image/jpeg data URI", gotURL)
	}
	payload := gotURL[len("data:image/jpeg;base64,"):]
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("upstream image payload does not decode as base64: %v", err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(decoded))
	if err != nil {
		t.Fatalf("upstream image payload is not a decodable image (got %s): %v", format, err)
	}
	long := cfg.Width
	if cfg.Height > long {
		long = cfg.Height
	}
	if long > 100 {
		t.Errorf("upstream image long side = %d, want <= 100 (the configured image_downscale cap)", long)
	}
	if len(decoded) >= len(dataURL) {
		t.Errorf("downscaled image (%d bytes) not smaller than the original data URI (%d bytes)", len(decoded), len(dataURL))
	}
	// The original (pre-downscale) bytes must not leak anywhere downstream:
	// neither to the upstream nor into the -record replay audit line, which
	// mirrors what was actually sent (same in-place rv update the -stream
	// rewrite uses).
	recorded, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(recorded), dataURL) {
		t.Error("-record client body carries the ORIGINAL image bytes; rv was not updated in place")
	}
	if !strings.Contains(string(recorded), "image/jpeg") {
		t.Errorf("-record client body missing the downscaled image: %s", recorded)
	}
}

// TestRun_ModelOverrideZeroDisablesDownscale pins the MaxPx resolution
// replay must reproduce from live traffic: a virtual model's explicit
// image_downscale: 0 force-disables downscaling for that model even when the
// global setting is on — a replay honoring only the global value would ship
// bytes live traffic never sent.
func TestRun_ModelOverrideZeroDisablesDownscale(t *testing.T) {
	dir := t.TempDir()
	dataURL := bigImageDataURL(t, 300, 150)

	var gotURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		gotURL = upstreamImageURL(t, body)
		fmt.Fprint(w, `{"id":"r","model":"upstream-model","choices":[]}`)
	}))
	defer upstream.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
image_downscale: 100
providers:
  - {name: p1, base_url: {openai-completions: %q}, api_key: real-provider-key}
models:
  vm:
    image_downscale: 0
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [upstream-model]}
`, upstream.URL)
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	auditPath := writeAuditLine(t, dir, "audit.jsonl", imageRecord(t, "vm", dataURL))

	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1",
	}, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotURL != dataURL {
		t.Errorf("override image_downscale: 0 must force-disable downscaling (global 100 must not win); got url == original: %v", gotURL == dataURL)
	}
}

// TestRun_NoImageRecordUnaffected pins the zero-cost promise: a record with
// no image marker passes through imgprep untouched — the upstream receives
// the original body, changed only by the sanctioned model-name rewrite.
func TestRun_NoImageRecordUnaffected(t *testing.T) {
	dir := t.TempDir()
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = nil
		json.NewDecoder(r.Body).Decode(&gotBody)
		fmt.Fprint(w, `{"id":"r","model":"upstream-model","choices":[]}`)
	}))
	defer upstream.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
image_downscale: 100
providers:
  - {name: p1, base_url: {openai-completions: %q}, api_key: real-provider-key}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [upstream-model]}
`, upstream.URL)
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "plain text"))

	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1",
	}, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("upstream messages = %v, want the single original message", msgs)
	}
	if c, _ := msgs[0].(map[string]any)["content"].(string); c != "plain text" {
		t.Errorf("upstream content = %q, want the original untouched content", c)
	}
	if m, _ := gotBody["model"].(string); m != "upstream-model" {
		t.Errorf("upstream model = %q, want the sanctioned model rewrite only", m)
	}
}

// TestEffectiveImageDownscaleMaxPx unit-pins the MaxPx resolution replay
// shares with server.downscaleImages via the same
// ModelRoute.EffectiveImageDownscaleMaxPx call: model override (even 0)
// wins, absent override inherits the global value, unknown model falls back
// to global, and 0 always means "off", never a built-in default.
func TestEffectiveImageDownscaleMaxPx(t *testing.T) {
	zero, global, override := 0, 100, 512
	cfg := &config.Config{
		ImageDownscaleMaxPx: global,
		Models: map[string]config.VirtualModel{
			"vm-force-off":   {ImageDownscaleMaxPx: &zero},
			"vm-override":    {ImageDownscaleMaxPx: &override},
			"vm-no-override": {},
		},
	}
	for _, tc := range []struct {
		model string
		want  int
	}{
		{"vm-override", 512},    // model override wins over global
		{"vm-force-off", 0},     // explicit 0 force-disables despite global 100
		{"vm-no-override", 100}, // no override inherits global
		{"ghost", 100},          // unknown model falls back to global (nil-route behavior live)
	} {
		if got := effectiveImageDownscaleMaxPx(cfg, tc.model); got != tc.want {
			t.Errorf("effectiveImageDownscaleMaxPx(%q) = %d, want %d", tc.model, got, tc.want)
		}
	}
	// Global 0 = downscaling off entirely; nothing can turn it on except a
	// model override.
	cfg.ImageDownscaleMaxPx = 0
	if got := effectiveImageDownscaleMaxPx(cfg, "vm-no-override"); got != 0 {
		t.Errorf("global 0 with no override = %d, want 0 (0 = disabled, not a built-in default)", got)
	}
	if got := effectiveImageDownscaleMaxPx(cfg, "vm-override"); got != 512 {
		t.Errorf("global 0 with model override = %d, want 512 (model override still wins)", got)
	}
}
