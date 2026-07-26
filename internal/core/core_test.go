// Ver 2026-07-16 00:00, by Sonnet 5
package core

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHealthKeyProtocolPrefixAvoidsCollision locks in the reason AdapterType
// is part of HealthKey (see the doc comment on Endpoint.HealthKey): the same
// provider short name can be reused across protocol groups (config.example.yaml
// has "openrouter" under both providers.openai and providers.anthropic), so
// two Endpoints that are genuinely different upstream targets must not share
// a health-registry key just because Provider/Model/APIKey happen to match.
func TestHealthKeyProtocolPrefixAvoidsCollision(t *testing.T) {
	openai := &Endpoint{AdapterType: "openai", Provider: "openrouter", Model: "gpt-5", APIKey: "sk-same"}
	anthropic := &Endpoint{AdapterType: "anthropic", Provider: "openrouter", Model: "gpt-5", APIKey: "sk-same"}
	if openai.HealthKey() == anthropic.HealthKey() {
		t.Errorf("endpoints differing only in AdapterType must not collide: %q", openai.HealthKey())
	}
}

// TestHealthKeyDiffersByAPIKey ensures two endpoints that are otherwise
// identical but authenticate with different keys (e.g. two accounts at the
// same provider) get distinct health state.
func TestHealthKeyDiffersByAPIKey(t *testing.T) {
	a := &Endpoint{AdapterType: "openai", Provider: "acme", Model: "m", APIKey: "key-a"}
	b := &Endpoint{AdapterType: "openai", Provider: "acme", Model: "m", APIKey: "key-b"}
	if a.HealthKey() == b.HealthKey() {
		t.Errorf("endpoints with different API keys must not collide: %q", a.HealthKey())
	}
}

// TestHealthKeyStableForSameFields documents that HealthKey is a pure
// function of Provider/AdapterType/Model/APIKey — required for cooldown
// state to survive a hot reload (a fresh Endpoint value built from the same
// config must resolve to the same registry entry).
func TestHealthKeyStableForSameFields(t *testing.T) {
	a := &Endpoint{AdapterType: "anthropic", Provider: "minimax", Model: "MiniMax-M3", APIKey: "sk-x"}
	b := &Endpoint{AdapterType: "anthropic", Provider: "minimax", Model: "MiniMax-M3", APIKey: "sk-x"}
	if a.HealthKey() != b.HealthKey() {
		t.Errorf("HealthKey must be deterministic: %q != %q", a.HealthKey(), b.HealthKey())
	}
}

// TestNameOmitsAPIKey checks Name() (used in X-VMR-Endpoint and audit logs)
// never leaks the credential, unlike HealthKey which folds in a fingerprint.
func TestNameOmitsAPIKey(t *testing.T) {
	e := &Endpoint{AdapterType: "openai", Provider: "acme", Model: "m", APIKey: "sk-secret"}
	if got, want := e.Name(), "openai/acme/m"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

// TestEndpointHealthKeyWithoutFreezeStillWorks locks in the fallback path:
// an Endpoint built as a bare struct literal (as ~12 call sites across the
// test suite do, never going through router.BuildSnapshot) must still
// return the correct HealthKey()/Name() even though Freeze() was never
// called — router.BuildSnapshot's Freeze() call is a hot-path optimization,
// not a correctness requirement.
func TestEndpointHealthKeyWithoutFreezeStillWorks(t *testing.T) {
	e := &Endpoint{AdapterType: "anthropic", Provider: "minimax", Model: "MiniMax-M3", APIKey: "sk-x"}
	if got, want := e.HealthKey(), e.computeHealthKey(); got != want {
		t.Errorf("un-frozen HealthKey() = %q, want %q (computeHealthKey directly)", got, want)
	}
	if got, want := e.Name(), "anthropic/minimax/MiniMax-M3"; got != want {
		t.Errorf("un-frozen Name() = %q, want %q", got, want)
	}
}

// TestEndpointFreezeMatchesUnfrozen checks Freeze() doesn't change the
// value HealthKey()/Name() report — only how cheaply they report it. Two
// Endpoints built from identical fields, one Freeze()'d and one not, must
// stay indistinguishable to every caller.
func TestEndpointFreezeMatchesUnfrozen(t *testing.T) {
	fields := func() Endpoint {
		return Endpoint{AdapterType: "openai", Provider: "acme", Model: "m", APIKey: "sk-secret"}
	}
	unfrozen := fields()
	frozen := fields()
	frozen.Freeze()

	if unfrozen.HealthKey() != frozen.HealthKey() {
		t.Errorf("HealthKey mismatch: unfrozen=%q frozen=%q", unfrozen.HealthKey(), frozen.HealthKey())
	}
	if unfrozen.Name() != frozen.Name() {
		t.Errorf("Name mismatch: unfrozen=%q frozen=%q", unfrozen.Name(), frozen.Name())
	}
	// Freeze must be idempotent — calling it twice must not change the result.
	frozen.Freeze()
	if frozen.HealthKey() != unfrozen.HealthKey() || frozen.Name() != unfrozen.Name() {
		t.Errorf("Freeze() is not idempotent: HealthKey=%q Name=%q", frozen.HealthKey(), frozen.Name())
	}
}

// TestErrorClassString locks every declared ErrorClass to its string, not
// just the ones classify.go itself produces — the four audit-only values
// (Build/Network/Canceled/Truncated) never reach Health.ReportFailure, but
// report.go buckets error_classes by these exact strings, so a rename here
// would silently fragment that report without any test catching it.
// TestEstimateTextTokens locks in the ascii/wide byte-classification split
// (4 bytes/token ascii, 2 bytes/token wide-UTF-8) this is now the single
// shared implementation for: internal/server/facts.go's pre-routing
// RequestFacts.EstimatedTokens, and internal/report's per-role token
// estimate in detail pages (roleTokens). A regression here silently changes
// both a live routing signal and a reporting number.
func TestEstimateTextTokens(t *testing.T) {
	if got := EstimateTextTokens(nil); got != 0 {
		t.Errorf("empty input = %d, want 0", got)
	}
	// 8 ascii bytes / 4 bytes-per-token = 2.
	if got := EstimateTextTokens([]byte("abcdefgh")); got != 2 {
		t.Errorf("ascii = %d, want 2", got)
	}
	// "中" is 3 UTF-8 bytes, all wide (lead byte >= 0x80); repeated 4x = 12
	// wide bytes / 2 bytes-per-token = 6.
	wide := []byte("中中中中")
	if got := EstimateTextTokens(wide); got != 6 {
		t.Errorf("wide utf-8 = %d, want 6", got)
	}
	// Mixed: wide text must cost more tokens per byte than ascii, not less
	// — this is the "CJK deliberately overestimated" invariant the
	// asciiBytesPerToken/wideBytesPerToken split exists for.
	asciiPerByte := float64(EstimateTextTokens([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))) / 32
	widePerByte := float64(EstimateTextTokens([]byte(strings.Repeat("中", 32)))) / (32 * 3)
	if widePerByte <= asciiPerByte {
		t.Errorf("wide text should estimate more tokens/byte than ascii: wide=%.4f ascii=%.4f", widePerByte, asciiPerByte)
	}
}

func TestErrorClassString(t *testing.T) {
	cases := map[ErrorClass]string{
		ErrClient:    "client",
		ErrAuth:      "auth",
		ErrRateLimit: "rate_limit",
		ErrEndpoint:  "endpoint",
		ErrTransient: "transient",
		ErrContent:   "content",
		ErrBuild:     "build",
		ErrNetwork:   "network",
		ErrCanceled:  "canceled",
		ErrTruncated: "truncated",
	}
	for class, want := range cases {
		if got := class.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", class, got, want)
		}
	}
}

func TestMarshalNoEscapeSkipsHTMLEscaping(t *testing.T) {
	out, err := MarshalNoEscape("a < b & c > d")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"a < b & c > d"` {
		t.Errorf("got %q, want no \\u003c-style escaping", out)
	}
}

// TestWriteJSONSetsStatusAndContentType locks in the shape router and server
// both rely on (formerly two byte-identical local copies, see design doc §11).
func TestWriteJSONSetsStatusAndContentType(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, 201, map[string]any{"ok": true})
	if w.Code != 201 {
		t.Errorf("status: got %d, want 201", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type: %q", got)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not valid JSON: %v (%s)", err, w.Body.String())
	}
	if body["ok"] != true {
		t.Errorf("body: %v", body)
	}
}

// TestWriteErrorEnvelope locks in the error envelope shape both OpenAI
// clients (error.message) and Anthropic clients (type:"error") parse.
func TestWriteErrorEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, 429, "rate_limit_error", "slow down")
	if w.Code != 429 {
		t.Errorf("status: got %d, want 429", w.Code)
	}
	var body struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not valid JSON: %v (%s)", err, w.Body.String())
	}
	if body.Type != "error" || body.Error.Type != "rate_limit_error" || body.Error.Message != "slow down" {
		t.Errorf("envelope: %+v", body)
	}
}

// BenchmarkHealthKey_Unfrozen/Frozen quantifies §4.1's claim: HealthKey()
// re-hashes APIKey with SHA-256 on every call unless Freeze() was called
// once up front (router.BuildSnapshot does this for every real Endpoint).
func BenchmarkHealthKey_Unfrozen(b *testing.B) {
	e := &Endpoint{AdapterType: "anthropic", Provider: "minimax", Model: "MiniMax-M3", APIKey: "sk-some-fairly-long-api-key-value"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.HealthKey()
	}
}

func BenchmarkHealthKey_Frozen(b *testing.B) {
	e := &Endpoint{AdapterType: "anthropic", Provider: "minimax", Model: "MiniMax-M3", APIKey: "sk-some-fairly-long-api-key-value"}
	e.Freeze()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.HealthKey()
	}
}
