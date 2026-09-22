// Ver 2026-09-14, by Sonnet 5

// Agent Guard's outbound mount point, end to end through the real HTTP
// pipeline (the Agent Guard spec ADR-4, M3.4).
// These ride the same server/router/audit scaffolding every other
// internal/server integration test uses (newAuditedServer/chat/
// readRecords) rather than unit-testing applyOutboundGuard in isolation,
// because M3's own acceptance bar (§5) is explicitly "关闭即恒等" measured
// against the real request path, not against a mocked one.
package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/guard"
	"vmr/internal/router"
)

// credentialBearingReq embeds an AWS-shaped credential (Tier1 — detection
// is anchored-regex only, so its presence/absence in what reaches upstream
// is a clean, unambiguous signal) inside an otherwise ordinary chat request.
const credentialBearingReq = `{"model":"vm","messages":[{"role":"user","content":"my key is AKIAIOSFODNN7EXAMPLE, please store it"}]}`

// credentialBearingReqUpstream is what actually reaches the upstream mock
// under oneProviderYAML: the virtual model name "vm" is rewritten to the
// real upstream model name "upstream-model" — one of CLAUDE.md's five
// sanctioned byte-faithful-passthrough deviations, unrelated to and
// unaffected by guard. "关闭即恒等"/audit_only's "never rewrites" claims
// are about guard's own contribution to the body, so the passthrough
// assertions below compare against this already-model-rewritten form,
// not the client's original literal bytes (that comparison belongs to
// Client.Request.Body, which never sees the model rewrite at all — see
// TestApplyOutboundGuard_ClientRequestBodyStaysOriginal).
const credentialBearingReqUpstream = `{"model":"upstream-model","messages":[{"role":"user","content":"my key is AKIAIOSFODNN7EXAMPLE, please store it"}]}`

// newGuardedServer is newAuditedServer's sibling: same config/router/audit
// wiring, but also calls WithGuard — the one thing newAuditedServer
// deliberately never does, so every other integration test in this
// package keeps exercising the "guard not wired" state even if its YAML
// fixture happens to include a guard: block.
func newGuardedServer(t *testing.T, yaml string) (*httptest.Server, *audit.Logger) {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { al.Close() })
	eng, err := guard.NewEngine(guard.DefaultRules(), guard.RulesVersion)
	if err != nil {
		t.Fatal(err)
	}
	g := guard.NewGuard(eng)
	ts := httptest.NewServer(New(rt, al).WithGuard(g).Handler())
	t.Cleanup(ts.Close)
	return ts, al
}

// bodyCapturingUpstream is a minimal mock that records the exact raw
// bytes it received, for asserting byte-for-byte passthrough — the
// shared `upstream` mock in testhelpers_test.go only extracts the model
// field and discards the rest, which isn't enough to prove nothing
// rewrote the body.
type bodyCapturingUpstream struct {
	srv      *httptest.Server
	lastBody []byte
}

func newBodyCapturingUpstream(t *testing.T) *bodyCapturingUpstream {
	t.Helper()
	u := &bodyCapturingUpstream{}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.lastBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"x","object":"chat.completion","model":"model-one","choices":[]}`))
	}))
	t.Cleanup(u.srv.Close)
	return u
}

// TestApplyOutboundGuard_AbsentConfigIsByteIdentical is M3's own "关闭即
// 恒等" acceptance bar (§5 M3 验收第一条), exercised through the real
// HTTP path: no guard: key in config at all (the overwhelming common
// case today) must mean the upstream receives literally the same bytes
// the client sent, credential-shaped content included, and
// Record.Guard stays nil.
func TestApplyOutboundGuard_AbsentConfigIsByteIdentical(t *testing.T) {
	u := newBodyCapturingUpstream(t)
	ts, al := newAuditedServer(t, oneProviderYAML(u.srv.URL)) // no guard:, and WithGuard never called
	resp, _ := chat(t, ts, credentialBearingReq, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if string(u.lastBody) != credentialBearingReqUpstream {
		t.Errorf("upstream received %q, want %q (model-rewritten, otherwise byte-for-byte unchanged)", u.lastBody, credentialBearingReqUpstream)
	}
	recs := readRecords(t, al)
	if len(recs) != 1 {
		t.Fatalf("records: %d", len(recs))
	}
	if recs[0].Guard != nil {
		t.Errorf("Record.Guard = %+v, want nil when guard: is absent from config", recs[0].Guard)
	}
}

// TestApplyOutboundGuard_ConfiguredButNotWiredIsAlsoByteIdentical covers
// the other half of applyOutboundGuard's gate: a config that DOES declare
// guard: but a server that was never given a *guard.Guard via WithGuard
// (modeling a hot-reloaded-in guard: block before the next restart, per
// setupGuard's own documented limitation in cmd/vmr) must still be a
// pure no-op, not a nil-pointer panic.
func TestApplyOutboundGuard_ConfiguredButNotWiredIsAlsoByteIdentical(t *testing.T) {
	u := newBodyCapturingUpstream(t)
	yaml := oneProviderYAML(u.srv.URL) + "guard:\n  outbound:\n    mode: block\n"
	ts, al := newAuditedServer(t, yaml) // guard: present in config, but WithGuard never called
	resp, _ := chat(t, ts, credentialBearingReq, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (guard not wired must never panic or block)", resp.StatusCode)
	}
	if string(u.lastBody) != credentialBearingReqUpstream {
		t.Errorf("upstream received %q, want %q (model-rewritten, otherwise unchanged)", u.lastBody, credentialBearingReqUpstream)
	}
	recs := readRecords(t, al)
	if recs[0].Guard != nil {
		t.Errorf("Record.Guard = %+v, want nil when the server has no wired *guard.Guard", recs[0].Guard)
	}
}

// TestApplyOutboundGuard_AuditOnlyRecordsWithoutRewriting is M3.4's actual
// new behavior: guard: configured AND wired, mode: audit_only (the
// default) records the hit into Record.Guard.Hits but still forwards the
// original bytes unchanged — M3.4 deliberately never rewrites (that's
// M3.7).
func TestApplyOutboundGuard_AuditOnlyRecordsWithoutRewriting(t *testing.T) {
	u := newBodyCapturingUpstream(t)
	yaml := oneProviderYAML(u.srv.URL) + "guard:\n  outbound:\n    mode: audit_only\n"
	ts, al := newGuardedServer(t, yaml)
	resp, _ := chat(t, ts, credentialBearingReq, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if string(u.lastBody) != credentialBearingReqUpstream {
		t.Errorf("upstream received %q, want %q (model-rewritten, otherwise unchanged -- audit_only never rewrites the guard-relevant content)", u.lastBody, credentialBearingReqUpstream)
	}
	recs := readRecords(t, al)
	if len(recs) != 1 {
		t.Fatalf("records: %d", len(recs))
	}
	gr := recs[0].Guard
	if gr == nil {
		t.Fatal("Record.Guard is nil, want a stamped GuardRecord")
	}
	if gr.OutMode != "audit_only" {
		t.Errorf("OutMode = %q, want audit_only", gr.OutMode)
	}
	if len(gr.Hits) != 1 || gr.Hits[0].Rule != "aws-access-key" {
		t.Fatalf("Hits = %+v, want one aws-access-key hit", gr.Hits)
	}
	if gr.Hits[0].FP == "" {
		t.Error("Hit.FP must not be empty")
	}
}

// TestApplyOutboundGuard_ModeOffIsObservationOff confirms mode: off does
// not scan and does not stamp: Record.Guard stays nil, indistinguishable
// from guard: being entirely absent — the offline fallback path (report's
// guardscan) is what covers off-mode records for detection. (The earlier
// design stamped an empty record under off, which suppressed that
// fallback and made off a detection blind spot.)
func TestApplyOutboundGuard_ModeOffIsObservationOff(t *testing.T) {
	u := newBodyCapturingUpstream(t)
	yaml := oneProviderYAML(u.srv.URL) + "guard:\n  outbound:\n    mode: off\n"
	ts, al := newGuardedServer(t, yaml)
	chat(t, ts, credentialBearingReq, nil)
	recs := readRecords(t, al)
	if recs[0].Guard != nil {
		t.Fatalf("Record.Guard = %+v, want nil under mode: off (no scan, no stamp)", recs[0].Guard)
	}
}

// TestApplyOutboundGuard_CleanRequestNoHits confirms ordinary traffic
// (the overwhelming majority) produces a stamped GuardRecord with no
// Hits, not an absent one — Record.Guard != nil is "guard looked",
// len(Hits) == 0 is "guard found nothing," and these are different facts
// `vmr analyze` needs to tell apart.
func TestApplyOutboundGuard_CleanRequestNoHits(t *testing.T) {
	u := newBodyCapturingUpstream(t)
	yaml := oneProviderYAML(u.srv.URL) + "guard:\n  outbound:\n    mode: audit_only\n"
	ts, al := newGuardedServer(t, yaml)
	chat(t, ts, simpleReq, nil)
	recs := readRecords(t, al)
	gr := recs[0].Guard
	if gr == nil {
		t.Fatal("Record.Guard is nil, want a stamped GuardRecord")
	}
	if len(gr.Hits) != 0 {
		t.Errorf("Hits = %+v, want none for clean traffic", gr.Hits)
	}
}

// TestApplyOutboundGuard_TrustedProviderExemptsEntirely is M3.5's
// contract (§4.3): when the model's only candidate provider is listed in
// guard.trusted_providers, applyOutboundGuard skips scanning entirely —
// no Hits, and critically no Record.Guard stamp at all (same shape as
// mode: off — trust exemption means "guard never even looked here").
func TestApplyOutboundGuard_TrustedProviderExemptsEntirely(t *testing.T) {
	u := newBodyCapturingUpstream(t)
	yaml := `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: ` + u.srv.URL + `}, api_key: k1}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [upstream-model]}
guard:
  trusted_providers: [p1]
  outbound:
    mode: audit_only
`
	ts, al := newGuardedServer(t, yaml)
	chat(t, ts, credentialBearingReq, nil)
	if string(u.lastBody) != credentialBearingReqUpstream {
		t.Errorf("upstream received %q, want %q", u.lastBody, credentialBearingReqUpstream)
	}
	recs := readRecords(t, al)
	if recs[0].Guard != nil {
		t.Errorf("Record.Guard = %+v, want nil -- a fully-trusted candidate set must exempt entirely, not just find nothing", recs[0].Guard)
	}
}

// TestApplyOutboundGuard_PartiallyTrustedStillScans confirms the
// all-or-nothing rule: one untrusted candidate among several means the
// route is not exempt at all (§4.3: "任何一个候选不可信 → 照常伪名化").
func TestApplyOutboundGuard_PartiallyTrustedStillScans(t *testing.T) {
	u1, u2 := newBodyCapturingUpstream(t), newBodyCapturingUpstream(t)
	yaml := `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: ` + u1.srv.URL + `}, api_key: k1}
  - {name: p2, base_url: {openai-completions: ` + u2.srv.URL + `}, api_key: k2}
models:
  vm:
    sticky: false
    endpoints:
      openai-completions:
        - {providers: [p1], models: [model-a], priority: 1}
        - {providers: [p2], models: [model-b], priority: 2}
guard:
  trusted_providers: [p1]
  outbound:
    mode: audit_only
`
	ts, al := newGuardedServer(t, yaml)
	chat(t, ts, credentialBearingReq, nil)
	recs := readRecords(t, al)
	if recs[0].Guard == nil || len(recs[0].Guard.Hits) != 1 {
		t.Errorf("Record.Guard = %+v, want a stamped record with one hit (p2 is untrusted, so the route as a whole is not exempt)", recs[0].Guard)
	}
}

// TestApplyOutboundGuard_ModeBlockRejectsTier1Hit is M3.6's contract
// (ADR-10): mode: block with a Tier1 match returns HTTP 400 and never
// reaches the upstream at all.
func TestApplyOutboundGuard_ModeBlockRejectsTier1Hit(t *testing.T) {
	u := newBodyCapturingUpstream(t)
	yaml := oneProviderYAML(u.srv.URL) + "guard:\n  outbound:\n    mode: block\n"
	ts, al := newGuardedServer(t, yaml)
	resp, respBody := chat(t, ts, credentialBearingReq, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", resp.StatusCode, respBody)
	}
	if u.lastBody != nil {
		t.Errorf("upstream received a request (%q), want none -- mode: block must reject before dispatch", u.lastBody)
	}
	recs := readRecords(t, al)
	if len(recs) != 1 {
		t.Fatalf("records: %d", len(recs))
	}
	if recs[0].Guard == nil || len(recs[0].Guard.Hits) != 1 {
		t.Errorf("Record.Guard = %+v, want a stamped record with the blocking hit even though the request was rejected", recs[0].Guard)
	}
}

// TestApplyOutboundGuard_ModeBlockAllowsCleanRequest confirms mode: block
// only rejects requests that actually match a Tier1 rule -- ordinary
// traffic is unaffected.
func TestApplyOutboundGuard_ModeBlockAllowsCleanRequest(t *testing.T) {
	u := newBodyCapturingUpstream(t)
	yaml := oneProviderYAML(u.srv.URL) + "guard:\n  outbound:\n    mode: block\n"
	ts, _ := newGuardedServer(t, yaml)
	resp, _ := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 for clean traffic under mode: block", resp.StatusCode)
	}
	if u.lastBody == nil {
		t.Error("upstream never received the clean request")
	}
}

// TestApplyOutboundGuard_ModeBlockNeverBlocksOnTier2 pins K-G5 at the
// server-integration level: a Tier2-only match (the generic "sk-" prefix)
// must never trigger mode: block's rejection.
func TestApplyOutboundGuard_ModeBlockNeverBlocksOnTier2(t *testing.T) {
	u := newBodyCapturingUpstream(t)
	yaml := oneProviderYAML(u.srv.URL) + "guard:\n  outbound:\n    mode: block\n"
	ts, al := newGuardedServer(t, yaml)
	// 24 distinct characters (not strings.Repeat's single-char runs, which
	// have zero Shannon entropy and would silently fail the Tier2 rule's
	// own MinEntropy 3.0 gate rather than exercising it).
	tier2Req := `{"model":"vm","messages":[{"role":"user","content":"token sk-0123456789abcdefghijklmn"}]}`
	resp, _ := chat(t, ts, tier2Req, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (Tier2 must never block, K-G5)", resp.StatusCode)
	}
	recs := readRecords(t, al)
	if len(recs[0].Guard.Hits) != 1 || recs[0].Guard.Hits[0].Tier != 2 {
		t.Fatalf("Hits = %+v, want one Tier2 hit", recs[0].Guard.Hits)
	}
}

// TestApplyOutboundGuard_ClientRequestBodyStaysOriginal confirms ADR-12's
// two-layer contract holds even with guard wired: Client.Request.Body
// (recorded before applyOutboundGuard runs, server.go's existing
// rec.Client.Request.Body = audit.EncodeBody(body) line) is untouched by
// this change regardless of guard's verdict.
func TestApplyOutboundGuard_ClientRequestBodyStaysOriginal(t *testing.T) {
	u := newBodyCapturingUpstream(t)
	yaml := oneProviderYAML(u.srv.URL) + "guard:\n  outbound:\n    mode: audit_only\n"
	ts, al := newGuardedServer(t, yaml)
	chat(t, ts, credentialBearingReq, nil)
	recs := readRecords(t, al)
	// Client.Request.Body is audit.Record's json.RawMessage/`any`-typed
	// field (already decoded by readRecords' own json.Unmarshal into
	// audit.Record) -- re-marshal it and compare against the original
	// request re-marshaled the same way, so key ordering differences
	// between the two decode paths don't produce a false failure.
	reencoded, err := json.Marshal(recs[0].Client.Request.Body)
	if err != nil {
		t.Fatalf("re-marshal Client.Request.Body: %v", err)
	}
	var want map[string]any
	json.Unmarshal([]byte(credentialBearingReq), &want)
	wantBytes, _ := json.Marshal(want)
	if string(reencoded) != string(wantBytes) {
		t.Errorf("Client.Request.Body re-marshaled to %s, want %s (semantically unchanged)", reencoded, wantBytes)
	}
}
