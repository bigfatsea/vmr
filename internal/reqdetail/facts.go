// Ver 2026-08-20 00:00, by Sonnet 5

// Package reqdetail renders one audit record's detail page — the shared
// microscopic-tier leaf both internal/report and internal/story sit on top
// of (see docs/future-strategy/story_report_architecture_opus-5.md §7.6a).
// Every function here is a pure function of (audit.Record[, its own/prior
// ctxgraph.Manifest, taskseg.Profile]) — no session/task grouping state,
// no cross-record aggregation. That purity is what lets the same record,
// rendered via any two different code paths, produce byte-identical output
// (see detail_test.go's cross-path consistency test).
//
// This file holds the per-record fact extraction shared with
// internal/report's own aggregation pass (session.go's collect(),
// recextract.go's buildRec2, ingest.go's per-endpoint tallies) — logic that
// used to be implemented once for aggregation and a second, separately
// hand-rolled time for detail rendering. Exported here so report calls the
// one implementation instead of carrying its own copy that could drift.
package reqdetail

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/core"
)

// ErrorClass returns rec's last attempt's structured error class, "" when
// the record never errored (or never reached an attempt at all).
func ErrorClass(rec *audit.Record) string {
	for i := len(rec.Attempts) - 1; i >= 0; i-- {
		if cls := AttemptErrorClass(rec.Attempts[i]); cls != "" {
			return cls
		}
	}
	return ""
}

// AttemptErrorClass returns one attempt's structured error class, falling
// back to parsing the free-text Error field for logs written before
// ErrorClass existed: HTTP-classified failures stored the bare class name
// (no colon) directly in Error, and the four non-HTTP failure paths
// (build/network/canceled/truncated) used a "class: detail" prefix — both
// forms are still exactly recoverable from Error alone. New logs always
// carry ErrorClass and never touch this fallback.
func AttemptErrorClass(a audit.Attempt) string {
	if a.ErrorClass != "" {
		return a.ErrorClass
	}
	if a.Error == "" {
		return ""
	}
	if i := strings.IndexByte(a.Error, ':'); i > 0 {
		return a.Error[:i]
	}
	return a.Error
}

// LastEndpoint is the endpoint of rec's final attempt ("protocol:provider:
// model", the audit-log label format), or "-" when the request never
// reached an upstream.
func LastEndpoint(rec *audit.Record) string {
	if len(rec.Attempts) == 0 {
		return "-"
	}
	return rec.Attempts[len(rec.Attempts)-1].Endpoint
}

// RealModel is the upstream model of rec's final attempt, "none" when the
// request never reached an upstream or that attempt's model is unknown.
func RealModel(rec *audit.Record) string {
	if len(rec.Attempts) == 0 {
		return "none"
	}
	if _, _, m := AttemptUpstream(rec.Attempts[len(rec.Attempts)-1]); m != "" {
		return m
	}
	return "none"
}

// AttemptUpstream returns the attempt's protocol/provider/model, preferring
// the structured fields (new logs) and falling back to
// core.SplitEndpointLabel for logs written before they existed — that
// fallback accepts both the current ":"-joined Endpoint format and the
// "/"-joined form older audit logs used, so this stays correct regardless
// of which era wrote the record. Deliberately not a private SplitN here: an
// inlined "/"-only split would silently return ("", "", "") for a
// colon-joined Endpoint whose structured fields happen to be empty,
// disagreeing with internal/story's own upstream lookup
// (modelusage.go's stepUpstream) on the same record for no reason other
// than the two having separately hand-rolled the same parse.
func AttemptUpstream(a audit.Attempt) (protocol, provider, model string) {
	if a.Protocol != "" || a.Provider != "" || a.Model != "" {
		return a.Protocol, a.Provider, a.Model
	}
	protocol, provider, model, _ = core.SplitEndpointLabel(a.Endpoint)
	return protocol, provider, model
}

// DisplayModel is rec's virtual model name, "(rejected)" when empty (a
// record that never reached model resolution).
func DisplayModel(rec *audit.Record) string {
	if rec.Model == "" {
		return "(rejected)"
	}
	return rec.Model
}

// CountImages tallies a record's inline request images and the subset that
// triggered downscaling.
func CountImages(images []audit.ImageInfo) (total, compressed int) {
	total = len(images)
	for _, img := range images {
		if img.Downscaled {
			compressed++
		}
	}
	return total, compressed
}

// ToolsSig fingerprints a declared tool set: count plus name-list hash —
// the same "count/short-hash" convention ctxgraph.ReqHash8 uses for the
// request coordinate, applied to a tool name list instead.
func ToolsSig(names []string) string {
	return fmt.Sprintf("tools:%d/%s", len(names), toolsHash8(names))
}

// toolsHash8 is ToolsSig's hash half, factored out so
// EnsureToolsEvidence (evidence.go) can name its shared evidence file by
// the exact same content address instead of recomputing a slightly
// different hash and silently drifting from what ToolsSig displays.
func toolsHash8(names []string) string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	sum := md5.Sum([]byte(strings.Join(sorted, ",")))
	return hex.EncodeToString(sum[:4])
}

// RoleChars sums displayed characters (runes) per role across a request
// body's conversation. Anthropic tool_result parts are counted as "tool"
// regardless of the containing message's role, mirroring OpenAI's dedicated
// "tool" role so both protocols yield comparable shares. Returns nil when
// the body isn't a chat object.
func RoleChars(body any) map[string]int64 {
	return roleMeasure(body, func(s string) int64 { return int64(len([]rune(s))) })
}

// RoleTokens is RoleChars' token-estimate sibling: same per-role traversal,
// but each text fragment is sized with core.EstimateTextTokens (the same
// formula behind core.RequestFacts.EstimatedTokens) instead of a raw rune
// count — a token share is a much closer proxy for "what's actually costing
// money in this conversation" than a character share.
func RoleTokens(body any) map[string]int64 {
	return roleMeasure(body, func(s string) int64 { return core.EstimateTextTokens([]byte(s)) })
}

// roleMeasure walks a request body's conversation once, calling measure on
// every role's displayed text and summing the result per role. Shared by
// RoleChars (rune count) and RoleTokens (estimated token count) so the two
// only differ in how a text fragment is sized, not how the tree is walked.
func roleMeasure(body any, measure func(string) int64) map[string]int64 {
	obj, ok := body.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]int64{}
	add := func(role, text string) {
		if text != "" {
			out[role] += measure(text)
		}
	}
	if sys, ok := obj["system"]; ok {
		add("system", chatmsg.RenderContent(sys))
	}
	if instr, ok := obj["instructions"]; ok { // openai-responses
		add("system", chatmsg.RenderContent(instr))
	}
	for _, raw := range chatmsg.RawArray(obj) {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, hasRole := m["role"].(string)
		if !hasRole { // openai-responses non-message Item: function_call/function_call_output/reasoning/...
			itemRole, text := chatmsg.ResponsesItemMessage(m)
			add(itemRole, text)
			continue
		}
		switch c := m["content"].(type) {
		case []any:
			for _, p := range c {
				pm, isMap := p.(map[string]any)
				if isMap && pm["type"] == "tool_result" {
					add("tool", chatmsg.RenderPart(pm))
				} else if isMap {
					add(role, chatmsg.RenderPart(pm))
				} else {
					add(role, jsonIndent(p))
				}
			}
		default:
			add(role, chatmsg.RenderContent(c))
		}
		if rc, _ := m["reasoning_content"].(string); rc != "" {
			add(role, rc)
		}
		for _, tc := range chatmsg.ToolCallList(m["tool_calls"]) {
			add(role, tc.Name+tc.Args)
		}
	}
	return out
}

// Details wraps body in a collapsed-by-default disclosure block. summary is
// HTML-escaped by the caller only if it embeds user content (see
// EscapeHTML). The blank lines around body are required for Markdown to
// render inside <details> on GitHub and VS Code.
func Details(summary, body string) string {
	return "<details><summary>" + summary + "</summary>\n\n" + body + "\n</details>\n"
}

// BodyBytes sizes a recorded body: JSON bodies by re-serialization, string
// bodies (SSE etc.) by length. Truncated bodies undercount; that matches
// what was recorded.
func BodyBytes(body any) int64 {
	return int64(len(BodyRaw(body)))
}

// BodyRaw is BodyBytes' byte-returning counterpart: the recorded body as
// the bytes it occupied on the wire. audit.EncodeBody stores a JSON body as
// json.RawMessage and anything else (SSE text) as a string, so the two
// cases are unwrapped differently — but both must yield the same byte
// sequence a re-marshal would produce, since a consumer needing the bytes
// rather than their count (e.g. token-count estimation) has to reproduce a
// byte-count formula the routing half already applied to this same body.
//
// json.RawMessage is deliberately NOT given a fast path returning its bytes
// unchanged, even though that looks like free savings. Reading a record off
// disk yields map[string]any for a JSON body (json.Unmarshal into an `any`
// field never produces json.RawMessage), so the RawMessage case only arises
// for a record built in-process — and json.Marshal compacts it, while
// returning it verbatim would not. A whitespace-carrying in-process record
// would then measure differently from the identical record after a disk
// round-trip, in the one function whose whole job is that they agree. The
// default branch is not a missed case; it is what keeps the two paths equal.
// It is also not on any hot path: the production shape is map[string]any,
// which has to be marshalled either way.
func BodyRaw(body any) []byte {
	switch b := body.(type) {
	case nil:
		return nil
	case string:
		return []byte(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return nil
		}
		return raw
	}
}
