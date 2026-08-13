// Ver 2026-08-13, by Opus 5
package core

import "testing"

func TestEndpointLabel(t *testing.T) {
	if got := EndpointLabel("openai", "acct1", "gpt-5"); got != "openai:acct1:gpt-5" {
		t.Errorf("EndpointLabel = %q, want %q", got, "openai:acct1:gpt-5")
	}
}

func TestSplitEndpointLabel_ColonFormat(t *testing.T) {
	protocol, provider, model, ok := SplitEndpointLabel("openai:acct1:gpt-5")
	if !ok || protocol != "openai" || provider != "acct1" || model != "gpt-5" {
		t.Errorf("SplitEndpointLabel(colon) = (%q,%q,%q,%v), want (openai,acct1,gpt-5,true)", protocol, provider, model, ok)
	}
}

func TestSplitEndpointLabel_LegacySlashFormat(t *testing.T) {
	protocol, provider, model, ok := SplitEndpointLabel("openai/acct1/gpt-5")
	if !ok || protocol != "openai" || provider != "acct1" || model != "gpt-5" {
		t.Errorf("SplitEndpointLabel(slash) = (%q,%q,%q,%v), want (openai,acct1,gpt-5,true)", protocol, provider, model, ok)
	}
}

// TestSplitEndpointLabel_ModelContainsColonOrSlash covers the reason
// SplitN caps at 3: a model name itself can legitimately contain ":" or "/"
// (e.g. an OpenRouter-style "z-ai/glm-5.2"), and only the first two
// separators should be treated as structural.
func TestSplitEndpointLabel_ModelContainsColonOrSlash(t *testing.T) {
	_, provider, model, ok := SplitEndpointLabel("openai:acct1:z-ai/glm-5.2")
	if !ok || provider != "acct1" || model != "z-ai/glm-5.2" {
		t.Errorf("colon-joined with a slash inside model = (%q,%q,%v), want (acct1,z-ai/glm-5.2,true)", provider, model, ok)
	}
	_, provider, model, ok = SplitEndpointLabel("openai/acct1/z-ai:v2")
	if !ok || provider != "acct1" || model != "z-ai:v2" {
		t.Errorf("slash-joined with a colon inside model = (%q,%q,%v), want (acct1,z-ai:v2,true)", provider, model, ok)
	}
}

// TestSplitEndpointLabel_LegacySlashFormat_ModelHasTwoColons locks in the
// "earliest separator wins" tie-break: a legacy "/"-joined label whose
// model segment itself contains two or more ":" (an Ollama/vLLM-style
// "registry:port/name:tag" model name) must still resolve via the "/"
// split. Trying ":" first unconditionally would find exactly 3 parts on
// the wrong field boundaries (protocol="openai/vllm/myregistry",
// provider="5000/llama3", model="8b") and stop there, never reaching the
// correct "/" split.
func TestSplitEndpointLabel_LegacySlashFormat_ModelHasTwoColons(t *testing.T) {
	protocol, provider, model, ok := SplitEndpointLabel("openai/vllm/myregistry:5000/llama3:8b")
	if !ok || protocol != "openai" || provider != "vllm" || model != "myregistry:5000/llama3:8b" {
		t.Errorf("SplitEndpointLabel(slash, model has 2 colons) = (%q,%q,%q,%v), want (openai,vllm,myregistry:5000/llama3:8b,true)",
			protocol, provider, model, ok)
	}
}

func TestSplitEndpointLabel_InvalidInput(t *testing.T) {
	cases := []string{"", "-", "openai", "openai:acct1", "openai/acct1"}
	for _, c := range cases {
		if _, _, _, ok := SplitEndpointLabel(c); ok {
			t.Errorf("SplitEndpointLabel(%q) ok=true, want false", c)
		}
	}
}
