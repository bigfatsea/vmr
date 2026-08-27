// Ver 2026-07-18 22:45, by Sonnet 5
package probe

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequest_ShapeAndNonce(t *testing.T) {
	body, nonce := Request("some-model")
	if nonce == "" || !strings.HasPrefix(nonce, "VMR-PROBE-") {
		t.Fatalf("unexpected nonce: %q", nonce)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body is not valid JSON: %v (%s)", err, body)
	}
	if m["model"] != "some-model" {
		t.Errorf("model = %v, want %q", m["model"], "some-model")
	}
	if _, ok := m["max_tokens"]; !ok {
		t.Error("max_tokens missing — some providers reject a request without it")
	}
	msgs, ok := m["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages: %v", m["messages"])
	}
	content, _ := msgs[0].(map[string]any)["content"].(string)
	if !strings.Contains(content, nonce) {
		t.Errorf("prompt content does not mention the nonce: %q", content)
	}
}

func TestRequest_TwoCallsGetDifferentNonces(t *testing.T) {
	_, n1 := Request("m")
	_, n2 := Request("m")
	if n1 == n2 {
		t.Error("two probes got the same nonce — the echo check can't tell a fresh response from a stale one")
	}
}

func TestRoleCompatRequest_ShapeAndNonce(t *testing.T) {
	body, nonce := RoleCompatRequest("some-model", "developer")
	if nonce == "" || !strings.HasPrefix(nonce, "VMR-PROBE-") {
		t.Fatalf("unexpected nonce: %q", nonce)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body is not valid JSON: %v (%s)", err, body)
	}
	msgs, ok := m["messages"].([]any)
	if !ok || len(msgs) != 2 {
		// Two messages, not Request's one: some providers reject a request
		// whose only message isn't role "user", a shape problem this test
		// must not be confused with a role-support problem.
		t.Fatalf("messages: %v, want 2 entries", m["messages"])
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "developer" {
		t.Errorf("messages[0].role = %v, want %q", first["role"], "developer")
	}
	last, _ := msgs[len(msgs)-1].(map[string]any)
	if last["role"] != "user" {
		t.Errorf("messages[last].role = %v, want %q", last["role"], "user")
	}
	content, _ := last["content"].(string)
	if !strings.Contains(content, nonce) {
		t.Errorf("last message content does not mention the nonce: %q", content)
	}
}

func TestResponsesRequest_ShapeAndNonce(t *testing.T) {
	body, nonce := ResponsesRequest("some-model")
	if nonce == "" || !strings.HasPrefix(nonce, "VMR-PROBE-") {
		t.Fatalf("unexpected nonce: %q", nonce)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body is not valid JSON: %v (%s)", err, body)
	}
	if m["model"] != "some-model" {
		t.Errorf("model = %v, want %q", m["model"], "some-model")
	}
	if _, ok := m["messages"]; ok {
		t.Error("messages must not be present — a Chat-Completions-shaped body sent to a Responses endpoint would be rejected as missing \"input\"")
	}
	input, ok := m["input"].(string)
	if !ok {
		t.Fatalf("input: %v, want a string", m["input"])
	}
	if !strings.Contains(input, nonce) {
		t.Errorf("input does not mention the nonce: %q", input)
	}
}

func TestResponsesRequest_TwoCallsGetDifferentNonces(t *testing.T) {
	_, n1 := ResponsesRequest("m")
	_, n2 := ResponsesRequest("m")
	if n1 == n2 {
		t.Error("two probes got the same nonce — the echo check can't tell a fresh response from a stale one")
	}
}

func TestEchoed(t *testing.T) {
	t.Parallel()
	nonce := "VMR-PROBE-deadbeef"
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"openai-completions shape", `{"choices":[{"message":{"role":"assistant","content":"VMR-PROBE-deadbeef"}}]}`, true},
		{"anthropic shape", `{"content":[{"type":"text","text":"Sure: VMR-PROBE-deadbeef"}]}`, true},
		{"wrapped/paraphrased still matches", `{"content":[{"text":"here you go -> VMR-PROBE-deadbeef <-"}]}`, true},
		{"missing entirely", `{"choices":[{"message":{"content":"I can't do that"}}]}`, false},
		{"different nonce present", `{"choices":[{"message":{"content":"VMR-PROBE-cafef00d"}}]}`, false},
		{"empty body", ``, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Echoed([]byte(tc.body), nonce); got != tc.want {
				t.Errorf("Echoed=%v, want %v", got, tc.want)
			}
		})
	}
}
