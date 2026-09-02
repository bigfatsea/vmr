// Ver 2026-07-29 20:00, by Sonnet 5

package chatmsg

// PairingReport summarizes tool_call/tool_result id pairing within one
// request's raw message list. F9: this causal edge is protocol-given, not
// inferred — a well-formed request body can never carry a tool_call the
// model issued without a matching tool_result answering it (the API would
// reject a continuation that skipped one), so pairing is an invariant of
// the data, not a heuristic outcome. Covers both protocol shapes: OpenAI's
// top-level `tool_calls`/`tool_call_id`, and Anthropic's content-part
// `tool_use`/`tool_result` (`tool_use_id`).
//
// The invariant holds on the IDS AS RECORDED, which is not the same as
// "raw string equality always pairs": the OpenClaw family of clients strips
// underscores when echoing an id back into the next request's history, so
// on that traffic exact-string pairing measures 0% while normalized pairing
// measures 100% (story_report_architecture_opus-5.md §5, verified on five
// real log files). This checker deliberately does NOT normalize — it is the
// strict form, used by internal/story's F9 regression test over synthetic
// fixtures. A caller pairing REAL client traffic wants the two-pass form
// (exact, then underscore-stripped) internal/story's toolResultsFor
// implements.
type PairingReport struct {
	Calls         int      // total tool_call / tool_use blocks found
	Results       int      // total tool_call_id / tool_use_id references found
	OrphanCalls   []string // call ids with no matching result
	OrphanResults []string // result references with no matching call
}

// OK reports whether every call has a matching result and vice versa.
func (r PairingReport) OK() bool {
	return len(r.OrphanCalls) == 0 && len(r.OrphanResults) == 0
}

// CheckToolPairing walks rawMsgs (a decoded request body's "messages"
// array, as found under body["messages"]) and reports whether every
// tool_call/tool_use id has a matching tool_result/tool_use_id reference
// somewhere else in the same list, and vice versa.
func CheckToolPairing(rawMsgs []any) PairingReport {
	calls := map[string]bool{}
	results := map[string]bool{}
	var callOrder, resultOrder []string
	seenCall := func(id string) {
		if id == "" {
			return
		}
		if !calls[id] {
			callOrder = append(callOrder, id)
		}
		calls[id] = true
	}
	seenResult := func(id string) {
		if id == "" {
			return
		}
		if !results[id] {
			resultOrder = append(resultOrder, id)
		}
		results[id] = true
	}

	for _, raw := range rawMsgs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		// OpenAI: assistant message's tool_calls array.
		for _, tc := range ToolCallList(m["tool_calls"]) {
			seenCall(tc.ID)
		}
		// OpenAI: a tool-result message carries tool_call_id at top level.
		if id, _ := m["tool_call_id"].(string); id != "" {
			seenResult(id)
		}
		// Anthropic: tool_use / tool_result live as content parts.
		parts, _ := m["content"].([]any)
		for _, p := range parts {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			switch pm["type"] {
			case "tool_use":
				if id, _ := pm["id"].(string); id != "" {
					seenCall(id)
				}
			case "tool_result":
				if id, _ := pm["tool_use_id"].(string); id != "" {
					seenResult(id)
				}
			}
		}
		// OpenAI Responses: function_call / function_call_output flat Items
		switch m["type"] {
		case "function_call":
			if id, _ := m["call_id"].(string); id != "" {
				seenCall(id)
			}
		case "function_call_output":
			if id, _ := m["call_id"].(string); id != "" {
				seenResult(id)
			}
		}
	}

	report := PairingReport{Calls: len(calls), Results: len(results)}
	for _, id := range callOrder {
		if !results[id] {
			report.OrphanCalls = append(report.OrphanCalls, id)
		}
	}
	for _, id := range resultOrder {
		if !calls[id] {
			report.OrphanResults = append(report.OrphanResults, id)
		}
	}
	return report
}
