// Ver 2026-08-15, by Sonnet 5

package router

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes v as the JSON response body with the given status.
// Shared by router and server (which already depends on router) so every
// JSON response (success or error) goes through one encoding path. Moved
// from internal/core: response writing is routing-half
// behavior, not a shared type both halves need — see core's package
// comment for the admission rule this follows.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// WriteError emits an error body that both OpenAI clients (error.message)
// and Anthropic clients (type:"error" envelope) can parse. Shared by router
// and server so a format change only has to be made once.
func WriteError(w http.ResponseWriter, status int, errType, msg string) {
	WriteJSON(w, status, map[string]any{
		"type":  "error",
		"error": map[string]string{"type": errType, "message": msg},
	})
}
