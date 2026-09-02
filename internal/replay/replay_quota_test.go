// Ver 2026-08-11, by Sonnet 5
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
	"testing"

	"vmr/internal/audit"
)

// writeQuotaConfig is writeConfig plus a quota: block on provider p1, and an
// explicit log_dir so the test can find vmr-quota.json afterward.
func writeQuotaConfig(t *testing.T, dir, upstreamURL, limitYAML string) string {
	t.Helper()
	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
log_dir: %q
providers:
  - name: p1
    base_url: {openai-completions: %q}
    api_key: real-provider-key
    quota:
      limits:
        - %s
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [upstream-model]}
`, dir, upstreamURL+"/v1", limitYAML)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// readQuotaCounters reads <dir>/vmr-quota.json (quota.Registry's on-disk
// format — see internal/quota/store.go's fileFormat) and returns the
// "counters" object for accounts[provider][limitKey], or nil if the file or
// that key doesn't exist. Decoded generically rather than through the quota
// package's own types so this test doesn't need to reconstruct a
// core.Limit/periodStart just to call Registry.Used.
func readQuotaCounters(t *testing.T, dir, provider, limitKey string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "vmr-quota.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var ff struct {
		Accounts map[string]map[string]struct {
			Counters map[string]any `json:"counters"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(data, &ff); err != nil {
		t.Fatalf("vmr-quota.json is not valid: %v\n%s", err, data)
	}
	acct, ok := ff.Accounts[provider]
	if !ok {
		return nil
	}
	b, ok := acct[limitKey]
	if !ok {
		return nil
	}
	return b.Counters
}

func TestRun_ChargesRequestsQuota(t *testing.T) {
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"r","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer upstream.Close()

	cfgPath := writeQuotaConfig(t, dir, upstream.URL, "{metric: requests, every: 1d, amount: 100}")
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))

	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1",
	}, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c := readQuotaCounters(t, dir, "p1", "requests/1d")
	if c == nil {
		t.Fatalf("no quota state recorded for p1/requests/1d")
	}
	if got, _ := c["requests"].(float64); got != 1 {
		t.Errorf("requests = %v, want 1", c["requests"])
	}
}

func TestRun_ChargesTokensQuota_FromSniffedUsage(t *testing.T) {
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"r","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"ok"}}],`+
			`"usage":{"prompt_tokens":100,"completion_tokens":40,"prompt_tokens_details":{"cached_tokens":30}}}`)
	}))
	defer upstream.Close()

	cfgPath := writeQuotaConfig(t, dir, upstream.URL, "{metric: tokens, every: 1d, amount: 1000000}")
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))

	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1",
	}, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c := readQuotaCounters(t, dir, "p1", "tokens/1d")
	if c == nil {
		t.Fatalf("no quota state recorded for p1/tokens/1d")
	}
	// OpenAI's prompt_tokens already includes cached_tokens as a subset —
	// Fresh should be the non-cached remainder (100-30=70), matching
	// chatmsg.Usage.Fresh's formula (see tokenCharge's own doc comment for
	// the same reasoning on the live path).
	if got, _ := c["fresh"].(float64); got != 70 {
		t.Errorf("fresh = %v, want 70 (100 prompt - 30 cached)", c["fresh"])
	}
	if got, _ := c["cache_read"].(float64); got != 30 {
		t.Errorf("cache_read = %v, want 30", c["cache_read"])
	}
	if got, _ := c["out"].(float64); got != 40 {
		t.Errorf("out = %v, want 40", c["out"])
	}
}

func TestRun_TokensQuota_DegradesWhenNoUsageField(t *testing.T) {
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"r","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"ok, no usage here"}}]}`)
	}))
	defer upstream.Close()

	cfgPath := writeQuotaConfig(t, dir, upstream.URL, "{metric: tokens, every: 1d, amount: 1000000}")
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))

	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1",
	}, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c := readQuotaCounters(t, dir, "p1", "tokens/1d")
	if c == nil {
		t.Fatalf("no quota state recorded for p1/tokens/1d")
	}
	// No usage field anywhere in the response -> degraded byte-count
	// estimate, charged entirely to fresh/out (see chargeReplay's doc
	// comment). Exact magnitude isn't the point here (it's an estimate);
	// what matters is that it charged something instead of staying at zero.
	fresh, _ := c["fresh"].(float64)
	out2, _ := c["out"].(float64)
	if fresh <= 0 || out2 <= 0 {
		t.Errorf("degraded charge = fresh=%v out=%v, want both > 0", fresh, out2)
	}
}

func TestRun_ErrorResponseDoesNotChargeQuota(t *testing.T) {
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"rate limited"}`)
	}))
	defer upstream.Close()

	cfgPath := writeQuotaConfig(t, dir, upstream.URL, "{metric: requests, every: 1d, amount: 100}")
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))

	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1",
	}, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if c := readQuotaCounters(t, dir, "p1", "requests/1d"); c != nil {
		t.Errorf("a >=400 response must not charge quota, got counters %+v", c)
	}
}

func TestRun_DryRunDoesNotChargeQuota(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeQuotaConfig(t, dir, "http://127.0.0.1:1/unreachable", "{metric: requests, every: 1d, amount: 100}")
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))

	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1", DryRun: true,
	}, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "vmr-quota.json")); !os.IsNotExist(err) {
		t.Errorf("-dry-run must never touch vmr-quota.json, stat err = %v", err)
	}
}

func TestRun_NoQuotaConfigured_NoStateFileCreated(t *testing.T) {
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"r","model":"upstream-model","choices":[]}`)
	}))
	defer upstream.Close()
	// Same shape as writeQuotaConfig but p1 has no quota: block at all —
	// log_dir still points at dir so a false-positive "file not found"
	// (looking in the wrong place) can't masquerade as a pass.
	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
log_dir: %q
providers:
  - {name: p1, base_url: {openai-completions: %q}, api_key: real-provider-key}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [upstream-model]}
`, dir, upstream.URL+"/v1")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))

	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1",
	}, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "vmr-quota.json")); !os.IsNotExist(err) {
		t.Errorf("a provider with no quota: configured must never create vmr-quota.json, stat err = %v", err)
	}
}

func TestRun_QuotaWithActiveAuditLock_DoesNotOverwriteState(t *testing.T) {
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"r","model":"upstream-model","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer upstream.Close()

	// Simulate active router daemon holding audit lock
	logger, err := audit.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	cfgPath := writeQuotaConfig(t, dir, upstream.URL, "{metric: requests, every: 1d, amount: 100}")
	auditPath := writeAuditLine(t, dir, "audit.jsonl", chatRecord("vm", "hi"))

	var out bytes.Buffer
	if err := Run(context.Background(), Options{
		ConfigPath: cfgPath, AuditPath: auditPath, Provider: "p1",
	}, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should note that daemon is active and not flush state file
	if !bytes.Contains(out.Bytes(), []byte("router daemon is active")) {
		t.Errorf("expected notice about active daemon, got output: %s", out.String())
	}
	// vmr-quota.json should not have been created by replay because daemon was active
	if _, err := os.Stat(filepath.Join(dir, "vmr-quota.json")); !os.IsNotExist(err) {
		t.Errorf("active daemon lock must suppress replay's Flush(), but vmr-quota.json exists")
	}
}
