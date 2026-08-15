// Ver 2026-08-15, by Sonnet 5

package router

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestWriteJSONSetsStatusAndContentType locks in the shape router and
// server both rely on — moved from internal/core/core_test.go alongside
// WriteJSON.
func TestWriteJSONSetsStatusAndContentType(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
