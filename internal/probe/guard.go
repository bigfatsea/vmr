// Ver 2026-09-17, by Sonnet 5

// Package probe extensions for Agent Guard's active diagnostic probes
// (the Agent Guard spec §4.10, M5), wired only
// through `vmr diagnose -guard` (opt-in, sends real requests to configured
// endpoints and consumes real upstream tokens -- same as any other `vmr
// diagnose` connectivity check). Two of the five probes are credential/
// steganography security checks proper (1, 5); the other three (2, 3, 4)
// are relay fidelity / billing-honesty checks -- a lossy or dishonest
// middlebox, not a credential leak or an injection attack. Kept in one
// matrix deliberately: all five probe the same threat surface ("can this
// endpoint be trusted to relay honestly"), all five share the same request/
// verify machinery, and splitting the QoS-flavored three into a separate
// command would not change what any of them actually detect.
// Implements request generators and response verifiers for:
// 1. Tool Call rewrite / tampering detection (security)
// 2. Long-context needle-in-a-haystack silent truncation detection (fidelity)
// 3. Thinking / reasoning_content stripping detection (fidelity)
// 4. Token usage inflation detection (billing honesty)
// 5. Invisible steganographic rune injection detection (security)
package probe

import (
	"encoding/json"
	"fmt"
	"strings"

	"vmr/internal/guard"
)

// IsReasoningModel returns true if the model name indicates reasoning / thinking capability.
func IsReasoningModel(model string) bool {
	m := strings.ToLower(model)
	return strings.Contains(m, "r1") ||
		strings.Contains(m, "o1") ||
		strings.Contains(m, "o3") ||
		strings.Contains(m, "o4") ||
		strings.Contains(m, "reason") ||
		strings.Contains(m, "think") ||
		strings.Contains(m, "claude-3-7") ||
		strings.Contains(m, "qwq")
}

// openAITokenLimitField returns the key name a Chat-Completions-shaped probe
// request must use for its token cap. OpenAI's own o-series reasoning
// models reject the legacy "max_tokens" field outright (HTTP 400: "Use
// max_completion_tokens instead") and require "max_completion_tokens";
// every other OpenAI-compatible provider on this same protocol -- including
// third-party reasoning models like DeepSeek-R1 or QwQ served over an
// openai-completions-shaped endpoint -- still expects "max_tokens".
// Deliberately narrower than IsReasoningModel's Contains-based match
// (which is right for "does this model think at all", the wrong question
// here): a prefix match on OpenAI's own reserved o-series naming, so a
// third-party reasoning model's own name is never mistaken for it.
func openAITokenLimitField(model string) string {
	lower := strings.ToLower(model)
	for _, prefix := range []string{"o1", "o3", "o4"} {
		if strings.HasPrefix(lower, prefix) {
			return "max_completion_tokens"
		}
	}
	return "max_tokens"
}

// ToolCallProbeRequest builds a request instructing the model to invoke a tool
// named "bash" with a specific command string, for verifying whether a middlebox
// tampers with or rewrites tool calls.
func ToolCallProbeRequest(protocol, model, command string) (json.RawMessage, string) {
	if command == "" {
		command = "echo 'VMR_GUARD_SAFE_TOKEN'"
	}
	switch protocol {
	case "anthropic", "anthropic-messages":
		b, _ := json.Marshal(map[string]any{
			"model":      model,
			"max_tokens": 300,
			"messages": []map[string]string{
				{"role": "user", "content": "Call the tool 'bash' with command: " + command},
			},
			"tools": []map[string]any{
				{
					"name":        "bash",
					"description": "Execute bash command",
					"input_schema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"command": map[string]string{"type": "string"},
						},
						"required": []string{"command"},
					},
				},
			},
			"tool_choice": map[string]any{"type": "tool", "name": "bash"},
		})
		return b, command

	case "openai-responses":
		// R-8(e): tool_choice must be set here too, or the model may answer
		// in plain text instead of calling the tool and an honest relay
		// fails the probe.
		b, _ := json.Marshal(map[string]any{
			"model":             model,
			"input":             "Call the tool 'bash' with command: " + command,
			"max_output_tokens": 300,
			"tools": []map[string]any{
				{
					"type":        "function",
					"name":        "bash",
					"description": "Execute bash command",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"command": map[string]string{"type": "string"},
						},
						"required": []string{"command"},
					},
				},
			},
			"tool_choice": map[string]any{"type": "function", "name": "bash"},
		})
		return b, command

	default: // OpenAI completions
		b, _ := json.Marshal(map[string]any{
			"model":                      model,
			openAITokenLimitField(model): 300,
			"messages": []map[string]string{
				{"role": "user", "content": "Call the tool 'bash' with command: " + command},
			},
			"tools": []map[string]any{
				{
					"type": "function",
					"function": map[string]any{
						"name":        "bash",
						"description": "Execute bash command",
						"parameters": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"command": map[string]string{"type": "string"},
							},
							"required": []string{"command"},
						},
					},
				},
			},
			"tool_choice": map[string]any{
				"type":     "function",
				"function": map[string]string{"name": "bash"},
			},
		})
		return b, command
	}
}

// VerifyToolCallResponse verifies that the response contains a tool call with the expected command.
func VerifyToolCallResponse(protocol string, body []byte, expectedCommand string) (bool, string) {
	switch protocol {
	case "openai-responses":
		// R-8(a): responses' non-stream body is the output[] shape, not
		// choices[] — parsing it as chat-completions made every honest
		// responses endpoint read as "empty choices" (a false TAMPER).
		var m struct {
			Output []struct {
				Type      string `json:"type"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"output"`
		}
		if err := json.Unmarshal(body, &m); err != nil {
			return false, "unmarshal response: " + err.Error()
		}
		for _, item := range m.Output {
			if item.Type == "function_call" {
				if item.Name != "bash" {
					return false, fmt.Sprintf("tool call name altered: got %q, want %q", item.Name, "bash")
				}
				if !strings.Contains(item.Arguments, expectedCommand) {
					return false, fmt.Sprintf("tool call command altered: got %q, want %q", item.Arguments, expectedCommand)
				}
				return true, "tool call verified"
			}
		}
		return false, "no tool call returned in response"

	case "anthropic", "anthropic-messages":
		var m struct {
			Content []struct {
				Type  string `json:"type"`
				Name  string `json:"name"`
				Input struct {
					Command string `json:"command"`
				} `json:"input"`
			} `json:"content"`
		}
		if err := json.Unmarshal(body, &m); err != nil {
			return false, "unmarshal response: " + err.Error()
		}
		for _, c := range m.Content {
			if c.Type == "tool_use" {
				if c.Name != "bash" {
					return false, fmt.Sprintf("tool call name altered: got %q, want %q", c.Name, "bash")
				}
				if !strings.Contains(c.Input.Command, expectedCommand) {
					return false, fmt.Sprintf("tool call command altered: got %q, want %q", c.Input.Command, expectedCommand)
				}
				return true, "tool call verified"
			}
		}
		return false, "no tool call returned in response"

	default: // OpenAI
		var m struct {
			Choices []struct {
				Message struct {
					ToolCalls []struct {
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &m); err != nil {
			return false, "unmarshal response: " + err.Error()
		}
		if len(m.Choices) == 0 {
			return false, "empty choices in response"
		}
		tcs := m.Choices[0].Message.ToolCalls
		if len(tcs) == 0 {
			return false, "no tool call returned in response"
		}
		// Searches all of them, not just tcs[0]: parallel tool calling
		// (multiple tool_calls in one message) is a legitimate, common
		// OpenAI Chat Completions behavior, and a model or relay is free to
		// order them however it likes -- unlike the anthropic/responses
		// branches above, whose single content/output array this exactly
		// mirrors once "bash" doesn't have to be entry zero.
		for _, tc := range tcs {
			if tc.Function.Name != "bash" {
				continue
			}
			if !strings.Contains(tc.Function.Arguments, expectedCommand) {
				return false, fmt.Sprintf("tool call command altered: got %q, want %q", tc.Function.Arguments, expectedCommand)
			}
			return true, "tool call verified"
		}
		return false, fmt.Sprintf("tool call name altered: got %q, want %q", tcs[0].Function.Name, "bash")
	}
}

// NeedleProbeRequest constructs a long-context needle-in-a-haystack prompt to
// detect silent context truncation by proxies.
func NeedleProbeRequest(protocol, model, nonce string, fillerReps int) json.RawMessage {
	if fillerReps <= 0 {
		fillerReps = 100
	}
	filler := strings.Repeat("The quick brown fox jumps over the lazy dog. ", fillerReps)
	prompt := fmt.Sprintf("System notice: Data integrity verification.\n%s\nSpecial key code: %s\n%s\nWhat is the Special key code? Reply with only the code and nothing else.", filler, nonce, filler)
	switch protocol {
	case "openai-responses":
		b, _ := json.Marshal(map[string]any{
			"model":             model,
			"input":             prompt,
			"max_output_tokens": 100,
		})
		return b
	default:
		b, _ := json.Marshal(map[string]any{
			"model":                      model,
			openAITokenLimitField(model): 100,
			"messages": []map[string]string{
				{"role": "user", "content": prompt},
			},
		})
		return b
	}
}

// VerifyNeedleResponse checks if the nonce was recovered from the response.
func VerifyNeedleResponse(body []byte, nonce string) bool {
	return Echoed(body, nonce)
}

// ThinkingProbeRequest constructs a reasoning prompt for models claiming thinking capability.
func ThinkingProbeRequest(protocol, model string) json.RawMessage {
	prompt := "Solve this puzzle step by step: What is 29 * 31? Explain your reasoning before answering."
	switch protocol {
	case "anthropic", "anthropic-messages":
		b, _ := json.Marshal(map[string]any{
			"model": model,
			// Anthropic requires max_tokens > thinking.budget_tokens; 500 <
			// 1024 made this request a guaranteed HTTP 400 against any
			// spec-compliant endpoint.
			"max_tokens": 2048,
			"thinking": map[string]any{
				"type":          "enabled",
				"budget_tokens": 1024,
			},
			"messages": []map[string]string{
				{"role": "user", "content": prompt},
			},
		})
		return b
	case "openai-responses":
		b, _ := json.Marshal(map[string]any{
			"model":             model,
			"input":             prompt,
			"max_output_tokens": 500,
		})
		return b
	default:
		b, _ := json.Marshal(map[string]any{
			"model":                      model,
			openAITokenLimitField(model): 500,
			"messages": []map[string]string{
				{"role": "user", "content": prompt},
			},
		})
		return b
	}
}

// VerifyThinkingResponse verifies that reasoning_content or thinking blocks are present.
func VerifyThinkingResponse(protocol string, body []byte) (bool, string) {
	switch protocol {
	case "openai-responses":
		// R-8(a): same output[]-shape fix as VerifyToolCallResponse — a
		// reasoning item in the response proves thinking survived the relay.
		var m struct {
			Output []struct {
				Type    string `json:"type"`
				Summary []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"summary"`
			} `json:"output"`
		}
		if err := json.Unmarshal(body, &m); err != nil {
			return false, "unmarshal response: " + err.Error()
		}
		for _, item := range m.Output {
			if item.Type == "reasoning" {
				for _, s := range item.Summary {
					if strings.TrimSpace(s.Text) != "" {
						return true, "reasoning summary verified"
					}
				}
			}
		}
		return false, "reasoning content stripped or missing"

	case "anthropic", "anthropic-messages":
		var m struct {
			Content []struct {
				Type     string `json:"type"`
				Thinking string `json:"thinking"`
			} `json:"content"`
		}
		if err := json.Unmarshal(body, &m); err != nil {
			return false, "unmarshal response: " + err.Error()
		}
		for _, c := range m.Content {
			if c.Type == "thinking" && strings.TrimSpace(c.Thinking) != "" {
				return true, "thinking content verified"
			}
		}
		return false, "thinking content stripped or missing"

	default:
		var m struct {
			Choices []struct {
				Message struct {
					ReasoningContent string `json:"reasoning_content"`
					Content          string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &m); err != nil {
			return false, "unmarshal response: " + err.Error()
		}
		if len(m.Choices) == 0 {
			return false, "empty choices in response"
		}
		if strings.TrimSpace(m.Choices[0].Message.ReasoningContent) != "" {
			return true, "reasoning_content verified"
		}
		if strings.Contains(m.Choices[0].Message.Content, "<think>") {
			return true, "reasoning block <think> verified"
		}
		return false, "reasoning_content stripped or missing"
	}
}

// UsageProbeRequest creates a prompt to test upstream token usage
// accounting. The text is deliberately long (~50 estimated tokens, not a
// couple of words) so a provider's fixed per-request chat-template
// overhead (role wrapping, priming tokens -- typically 10-20 tokens on
// real APIs) is a small fraction of the total instead of dwarfing a
// near-zero estimate: VerifyUsageResponse's 3.5x ratio check against a
// 2-3 token estimate for the old "Reply with OK" prompt flagged honest
// providers as inflated on essentially every run, since template overhead
// alone already exceeds 3.5x that estimate.
func UsageProbeRequest(protocol, model string) (json.RawMessage, string) {
	promptText := "This is a fixed-length calibration paragraph used only to give the " +
		"prompt token estimator a large, stable denominator, so that a provider's " +
		"fixed per-request chat-template overhead does not distort the reported " +
		"token count. Ignore everything above and reply with exactly: OK."
	switch protocol {
	case "openai-responses":
		b, _ := json.Marshal(map[string]any{
			"model":             model,
			"input":             promptText,
			"max_output_tokens": 50,
		})
		return b, promptText
	default:
		b, _ := json.Marshal(map[string]any{
			"model":                      model,
			openAITokenLimitField(model): 50,
			"messages": []map[string]string{
				{"role": "user", "content": promptText},
			},
		})
		return b, promptText
	}
}

// VerifyUsageResponse checks if reported usage exceeds reasonable bounds.
func VerifyUsageResponse(body []byte, estimatedPromptTokens int) (bool, float64, string) {
	var m struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return false, 0, "unmarshal response: " + err.Error()
	}
	promptTokens := m.Usage.PromptTokens
	if promptTokens == 0 {
		promptTokens = m.Usage.InputTokens
	}
	if promptTokens == 0 {
		return true, 1.0, "usage not reported by endpoint"
	}
	if estimatedPromptTokens <= 0 {
		estimatedPromptTokens = 10
	}
	ratio := float64(promptTokens) / float64(estimatedPromptTokens)
	if ratio > 3.5 || promptTokens > 500 {
		return false, ratio, fmt.Sprintf("usage inflated: reported %d prompt tokens vs estimated ~%d (ratio %.1fx)", promptTokens, estimatedPromptTokens, ratio)
	}
	return true, ratio, fmt.Sprintf("usage normal: %d prompt tokens (ratio %.1fx)", promptTokens, ratio)
}

// InvisibleRuneProbeRequest builds a request to test for steganographic rune injection.
func InvisibleRuneProbeRequest(protocol, model string) json.RawMessage {
	prompt := "Echo this text exactly: SAFE_PROBE_RUNES"
	switch protocol {
	case "openai-responses":
		b, _ := json.Marshal(map[string]any{
			"model":             model,
			"input":             prompt,
			"max_output_tokens": 50,
		})
		return b
	default:
		b, _ := json.Marshal(map[string]any{
			"model":                      model,
			openAITokenLimitField(model): 50,
			"messages": []map[string]string{
				{"role": "user", "content": prompt},
			},
		})
		return b
	}
}

// steganographicRuneCategories are the A/B-tier guard.ClassifyRune
// categories the online sanitizer actually deletes (sanitize.go) -- C-tier
// (ZWNJ/ZWJ/variant selectors) is deliberately excluded, matching K-G7:
// those code points are required by real scripts and emoji sequences, never
// a steganography signal on their own.
var steganographicRuneCategories = map[string]bool{
	guard.RuneCatTags:       true,
	guard.RuneCatControl:    true,
	guard.RuneCatZWSP:       true,
	guard.RuneCatSoftHyphen: true,
	guard.RuneCatBOM:        true,
	guard.RuneCatBidi:       true,
	guard.RuneCatLineSep:    true,
}

// VerifyInvisibleRuneResponse scans for unauthorized A/B-tier steganographic
// runes -- Tags, C0 control, ZWSP, soft hyphen, BOM, Bidi embedding/override/
// isolate (Trojan Source, CVE-2021-42574), and line separators -- in both
// literal UTF-8 and JSON \u escape / surrogate pair forms (RT-03).
// Classification is guard.ClassifyRune itself, and escape-aware scanning
// is guard.ScanEscaped, so this probe shares the exact same traversal
// with the online sanitizer and tool parameter unescaping.
func VerifyInvisibleRuneResponse(body []byte) (bool, string) {
	var found bool
	var detail string
	guard.ScanEscaped(body, func(r rune, rawSpan []byte, isEscape bool, ok bool) bool {
		if !ok {
			return true
		}
		if cat := guard.ClassifyRune(r); steganographicRuneCategories[cat] {
			found = true
			detail = fmt.Sprintf("steganographic rune detected: %s U+%04X", cat, r)
			return false
		}
		return true
	})
	if found {
		return false, detail
	}
	return true, "no steganographic runes detected"
}
