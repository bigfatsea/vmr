// Ver 2026-09, by pi-agent

// protocol_coverage_test enumerates every registered protocol adapter ×
// typical response shape and verifies that chatmsg's usage-parsing and
// shape-recognition counting logic handles them correctly — without
// depending on a full audit pipeline (unit test, not end-to-end).
//
// Driven by adapter.Names() so a new protocol is automatically covered
// (the test iterates over all three current protocols — openai-completions,
// anthropic-messages, openai-responses — and any future additions).
package main

import (
	"testing"

	"vmr/internal/adapter"
	"vmr/internal/chatmsg"
	"vmr/internal/core"
)

// protocolCoverageShape is one (protocol, response body) → expected usage
// tuple. body is the raw response body as chatmsg would receive it (a
// map[string]any for JSON, a string for SSE streams).
type protocolCoverageShape struct {
	name     string
	body     any
	protocol string
	wantIn   int64
	wantOut  int64
	wantOK   bool
	// inOK/outOK: only set when the protocol's side rule makes them
	// interesting (anthropic truncated stream, etc.) — zero value means
	// "either value is OK as long as wantOK matches".
	wantInOK, wantOutOK *bool
	// wantPartsInc / wantHoldersInc: how many unrecognized counter
	// increments this shape is expected to trigger.
	wantPartsInc   int64
	wantHoldersInc int64
}

func ptr[I int64 | bool](v I) *I { return &v }

func TestChatmsgProtocolCoverage(t *testing.T) {
	protocols := adapter.Names()
	if len(protocols) == 0 {
		t.Fatal("adapter.Names() returned empty — no protocols registered")
	}

	shapes := buildProtocolShapes()
	for _, p := range protocols {
		prot := p // capture
		t.Run(prot, func(t *testing.T) {
			cases, ok := shapes[prot]
			if !ok {
				t.Fatalf("protocol %q has no coverage fixture — add one to buildProtocolShapes", prot)
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					chatmsg.ResetUnrecognizedShapeCounts()
					u, ok := chatmsg.ExtractUsageWithProtocol(tc.body, tc.protocol)
					if u.In != tc.wantIn || u.Out != tc.wantOut {
						t.Errorf("In/Out = %d/%d, want %d/%d", u.In, u.Out, tc.wantIn, tc.wantOut)
					}
					if ok != tc.wantOK {
						t.Errorf("ok = %v, want %v", ok, tc.wantOK)
					}
					if tc.wantInOK != nil && tc.wantOutOK != nil {
						_, inOK, outOK := chatmsg.ExtractUsageSides(tc.body, tc.protocol)
						if inOK != *tc.wantInOK {
							t.Errorf("inOK = %v, want %v", inOK, *tc.wantInOK)
						}
						if outOK != *tc.wantOutOK {
							t.Errorf("outOK = %v, want %v", outOK, *tc.wantOutOK)
						}
					}
					parts, holders := chatmsg.UnrecognizedShapeCounts()
					if parts != tc.wantPartsInc {
						t.Errorf("unrecognized parts = %d, want %d", parts, tc.wantPartsInc)
					}
					if holders != tc.wantHoldersInc {
						t.Errorf("unrecognized holders = %d, want %d", holders, tc.wantHoldersInc)
					}
				})
			}
		})
	}
}

// buildProtocolShapes returns the per-protocol test-case table. Each
// fixture exercises what chatmsg.ExtractUsageWithProtocol does with the
// given body and protocol constant.
func buildProtocolShapes() map[string][]protocolCoverageShape {
	oai := core.ProtocolOpenAICompletions
	an := core.ProtocolAnthropicMessages
	rsp := core.ProtocolOpenAIResponses

	return map[string][]protocolCoverageShape{
		oai: {
			{
				name: "normal_json",
				body: map[string]any{
					"usage": map[string]any{"prompt_tokens": float64(10), "completion_tokens": float64(20)},
				},
				protocol: oai, wantIn: 10, wantOut: 20, wantOK: true,
			},
			{
				name:     "sse_with_usage",
				body:     "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":6,\"completion_tokens\":3}}\n\ndata: [DONE]\n",
				protocol: oai, wantIn: 6, wantOut: 3, wantOK: true,
			},
			{
				name:     "truncated_stream_no_usage",
				body:     "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n",
				protocol: oai, wantIn: 0, wantOut: 0, wantOK: false,
			},
			{
				name: "error_4xx",
				body: map[string]any{
					"error": map[string]any{"message": "rate limited", "type": "rate_limit_error"},
				},
				protocol: oai, wantIn: 0, wantOut: 0, wantOK: false,
			},
			{
				name: "softblock_sensitive",
				body: map[string]any{
					"choices": []any{
						map[string]any{
							"message": map[string]any{
								"role":    "assistant",
								"content": "cannot answer this request",
							},
						},
					},
					"output_sensitive": true,
				},
				protocol: oai, wantIn: 0, wantOut: 0, wantOK: false,
			},
		},
		an: {
			{
				name: "normal_json",
				body: map[string]any{
					"usage": map[string]any{"input_tokens": float64(5), "output_tokens": float64(7)},
				},
				protocol: an, wantIn: 5, wantOut: 7, wantOK: true,
			},
			{
				name:     "sse_message_start_delta",
				body:     "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":41,\"output_tokens\":1}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":9}}\n\ndata: [DONE]\n",
				protocol: an, wantIn: 41, wantOut: 9, wantOK: true,
			},
			{
				// Truncated anthropic stream: message_start only → inOK
				// true, outOK false (the placeholder output_tokens=1 is
				// suppressed).
				name:     "truncated_message_start_only",
				body:     "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":41,\"output_tokens\":1}}}\n",
				protocol: an, wantIn: 41, wantOut: 1, wantOK: true,
				wantInOK: ptr(true), wantOutOK: ptr(false),
			},
			{
				name: "error_4xx",
				body: map[string]any{
					"error": map[string]any{"message": "rate limited", "type": "rate_limit_error"},
				},
				protocol: an, wantIn: 0, wantOut: 0, wantOK: false,
			},
			{
				name: "softblock_sensitive",
				body: map[string]any{
					"content": []any{
						map[string]any{"type": "text", "text": "I cannot answer that"},
					},
					"stop_reason": "end_turn",
				},
				protocol: an, wantIn: 0, wantOut: 0, wantOK: false,
			},
		},
		rsp: {
			{
				name: "normal_json",
				body: map[string]any{
					"usage": map[string]any{"input_tokens": float64(4), "output_tokens": float64(6)},
				},
				protocol: rsp, wantIn: 4, wantOut: 6, wantOK: true,
			},
			{
				name:     "sse_response_completed",
				body:     "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":4,\"output_tokens\":6}}}\n\ndata: [DONE]\n",
				protocol: rsp, wantIn: 4, wantOut: 6, wantOK: true,
			},
			{
				name:     "truncated_stream_no_usage",
				body:     "data: {\"type\":\"response.output_text.delta\",\"delta\":\"x\"}\n\ndata: [DONE]\n",
				protocol: rsp, wantIn: 0, wantOut: 0, wantOK: false,
			},
			{
				name: "error_4xx",
				body: map[string]any{
					"error": map[string]any{"message": "rate limited", "type": "rate_limit_error"},
				},
				protocol: rsp, wantIn: 0, wantOut: 0, wantOK: false,
			},
			{
				name: "softblock_sensitive",
				body: map[string]any{
					"output": []any{
						map[string]any{
							"type": "message",
							"role": "assistant",
							"content": []any{
								map[string]any{"type": "output_text", "text": "I cannot answer that"},
							},
						},
					},
					"status": "completed",
				},
				protocol: rsp, wantIn: 0, wantOut: 0, wantOK: false,
			},
		},
	}
}
