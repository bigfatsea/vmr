// Ver 2026-08-27, by Sonnet 5

package audit

import (
	"testing"

	"vmr/internal/core"
)

func TestCanonicalProtocol(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"openai", core.ProtocolOpenAICompletions},
		{"anthropic", core.ProtocolAnthropicMessages},
		{core.ProtocolOpenAICompletions, core.ProtocolOpenAICompletions},
		{core.ProtocolAnthropicMessages, core.ProtocolAnthropicMessages},
		{core.ProtocolOpenAIResponses, core.ProtocolOpenAIResponses},
		{"unknown", "unknown"},
		{"", ""},
	}

	for _, tt := range tests {
		got := CanonicalProtocol(tt.input)
		if got != tt.want {
			t.Errorf("CanonicalProtocol(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeEndpointLabel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"openai:provider:model", "openai-completions:provider:model"},
		{"anthropic:provider:model", "anthropic-messages:provider:model"},
		{"openai/provider/model", "openai-completions/provider/model"},
		{"anthropic/provider/model", "anthropic-messages/provider/model"},
		{"openai-completions:provider:model", "openai-completions:provider:model"},
		{"openai-responses:provider:model", "openai-responses:provider:model"},
		{"custom:provider:model", "custom:provider:model"},
		{"no-separator", "no-separator"},
		{"", ""},
		{"openai:provider:model:with:colons", "openai-completions:provider:model:with:colons"},
		{"openai/provider/model/with/slashes", "openai-completions/provider/model/with/slashes"},
	}

	for _, tt := range tests {
		got := NormalizeEndpointLabel(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeEndpointLabel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
