// Ver 2026-07-13 19:00, by Sonnet 5
package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"vmr/internal/audit"

	_ "vmr/internal/adapter/openai"
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
  openai:
    vm:
      endpoints:
        - {provider: p1, model: upstream-model}
`
	} else {
		models = `
models:
  openai:
    other:
      endpoints:
        - {provider: p1, model: upstream-model}
`
	}
	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai:
    p1: {base_url: %q, api_key: real-provider-key}
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
		Protocol: "openai",
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
		Model: "vm", Protocol: "openai",
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

	// The millisecond-truncated form vmr-requests.jsonl's own "ts" column
	// uses (internal/report/export.go: "2006-01-02T15:04:05.000Z07:00").
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

// --- -detail locator ---

// writeDetailFile mimics what internal/report/detail.go's WriteDetails
// actually produces: json.MarshalIndent(&rec, "", "  ") into its own file,
// one record, no JSONL framing.
func writeDetailFile(t *testing.T, dir, name string, rec *audit.Record) string {
	t.Helper()
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRun_DetailFileSelectsRecord(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	detailPath := writeDetailFile(t, dir, "20260713-153042.100_vm_upstream-model_ok.json", chatRecord("vm", "from-detail-file"))

	var out bytes.Buffer
	err := Run(context.Background(), Options{
		ConfigPath: cfgPath, Provider: "p1", DryRun: true, DetailPath: detailPath,
	}, &out)
	if err != nil {
		t.Fatalf("Run -detail: %v", err)
	}
	if !strings.Contains(out.String(), "from-detail-file") {
		t.Errorf("dry-run output = %q, want the detail file's body", out.String())
	}
}

func TestRun_DetailFileRejectsAuditPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	detailPath := writeDetailFile(t, dir, "detail.json", chatRecord("vm", "hi"))
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))

	err := Run(context.Background(), Options{
		ConfigPath: cfgPath, Provider: "p1", DryRun: true, DetailPath: detailPath, AuditPath: auditPath,
	}, &bytes.Buffer{})
	if err == nil {
		t.Error("expected an error when -detail is combined with an audit file")
	}
}

func TestRun_DetailFileRejectsLineOrTS(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	detailPath := writeDetailFile(t, dir, "detail.json", chatRecord("vm", "hi"))

	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, Provider: "p1", DryRun: true, DetailPath: detailPath, Line: 3,
	}, &bytes.Buffer{}); err == nil {
		t.Error("expected an error when -detail is combined with -line")
	}
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, Provider: "p1", DryRun: true, DetailPath: detailPath, TS: "2026-07-13T15:30:42.000Z",
	}, &bytes.Buffer{}); err == nil {
		t.Error("expected an error when -detail is combined with -ts")
	}
}

func TestRun_MissingLocatorRequiresAuditPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, "http://127.0.0.1:1/unreachable", true)
	err := Run(context.Background(), Options{ConfigPath: cfgPath, Provider: "p1", DryRun: true}, &bytes.Buffer{})
	if err == nil {
		t.Error("expected an error when neither -detail nor an audit file argument is given")
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
