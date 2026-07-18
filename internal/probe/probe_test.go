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

func TestEchoed(t *testing.T) {
	nonce := "VMR-PROBE-deadbeef"
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"openai shape", `{"choices":[{"message":{"role":"assistant","content":"VMR-PROBE-deadbeef"}}]}`, true},
		{"anthropic shape", `{"content":[{"type":"text","text":"Sure: VMR-PROBE-deadbeef"}]}`, true},
		{"wrapped/paraphrased still matches", `{"content":[{"text":"here you go -> VMR-PROBE-deadbeef <-"}]}`, true},
		{"missing entirely", `{"choices":[{"message":{"content":"I can't do that"}}]}`, false},
		{"different nonce present", `{"choices":[{"message":{"content":"VMR-PROBE-cafef00d"}}]}`, false},
		{"empty body", ``, false},
	}
	for _, tc := range cases {
		if got := Echoed([]byte(tc.body), nonce); got != tc.want {
			t.Errorf("%s: Echoed=%v, want %v", tc.name, got, tc.want)
		}
	}
}
