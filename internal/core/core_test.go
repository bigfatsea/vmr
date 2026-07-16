// Ver 2026-07-16 00:00, by Sonnet 5
package core

import (
	"encoding/json"
	"net/http/httptest"
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

func TestErrorClassString(t *testing.T) {
	cases := map[ErrorClass]string{
		ErrClient:    "client",
		ErrAuth:      "auth",
		ErrRateLimit: "rate_limit",
		ErrEndpoint:  "endpoint",
		ErrTransient: "transient",
		ErrContent:   "content",
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
