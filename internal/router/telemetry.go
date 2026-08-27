// Ver 2026-08-23 14:45, by Gemini

package router

import (
	"sync/atomic"

	"vmr/internal/core"
)

// Telemetry holds process-lifetime in-memory traffic counters for /status.
// All fields use fixed atomic counters — zero dynamic map allocation, zero
// hot-reload state drift, zero lock contention on the request hot path.
type Telemetry struct {
	requestsTotal   atomic.Uint64
	reqOpenAI       atomic.Uint64
	reqAnthropic    atomic.Uint64
	reqResponses    atomic.Uint64
	reqStatusOK     atomic.Uint64
	reqStatusCancel atomic.Uint64
	reqStatusError  atomic.Uint64

	tokensIn         atomic.Uint64
	tokensCacheWrite atomic.Uint64
	tokensCacheRead  atomic.Uint64
	tokensReasoning  atomic.Uint64
	tokensOut        atomic.Uint64
}

// TelemetrySnapshot is the JSON-ready snapshot of Telemetry for /status.
type TelemetrySnapshot struct {
	Requests struct {
		Total      uint64            `json:"total"`
		ByProtocol map[string]uint64 `json:"by_protocol"`
		ByStatus   map[string]uint64 `json:"by_status"`
	} `json:"requests"`
	Tokens struct {
		Total struct {
			In         uint64 `json:"in"`
			CacheWrite uint64 `json:"cache_write"`
			CacheRead  uint64 `json:"cache_read"`
			Reasoning  uint64 `json:"reasoning"`
			Out        uint64 `json:"out"`
		} `json:"total"`
	} `json:"tokens"`
}

// RecordRequest registers an ingress request by protocol name.
func (t *Telemetry) RecordRequest(protocol string) {
	if t == nil {
		return
	}
	t.requestsTotal.Add(1)
	switch protocol {
	case core.ProtocolOpenAICompletions:
		t.reqOpenAI.Add(1)
	case core.ProtocolAnthropicMessages:
		t.reqAnthropic.Add(1)
	case core.ProtocolOpenAIResponses:
		t.reqResponses.Add(1)
	}
}

// RecordOutcome registers request completion status.
//
// Semantics note (deliberate, registered in docs/KNOWN_ISSUES_sonnet-5.md
// §2.2): a stream truncated mid-transfer counts here as error, while the
// audit log's top-level outcome for the same request records ok (HTTP 200,
// not canceled) — the truncation is visible there on the attempt's
// ErrorClass instead. The two ledgers answer different questions (did the
// client get an intact response vs did the exchange complete at the HTTP
// layer); do not "fix" one to match the other without revisiting that
// registration.
func (t *Telemetry) RecordOutcome(ok, canceled bool) {
	if t == nil {
		return
	}
	if canceled {
		t.reqStatusCancel.Add(1)
	} else if ok {
		t.reqStatusOK.Add(1)
	} else {
		t.reqStatusError.Add(1)
	}
}

// RecordTokens accumulates the 5-component token counters.
func (t *Telemetry) RecordTokens(in, cacheWrite, cacheRead, reasoning, out int64) {
	if t == nil {
		return
	}
	if in > 0 {
		t.tokensIn.Add(uint64(in))
	}
	if cacheWrite > 0 {
		t.tokensCacheWrite.Add(uint64(cacheWrite))
	}
	if cacheRead > 0 {
		t.tokensCacheRead.Add(uint64(cacheRead))
	}
	if reasoning > 0 {
		t.tokensReasoning.Add(uint64(reasoning))
	}
	if out > 0 {
		t.tokensOut.Add(uint64(out))
	}
}

// Snapshot returns the current snapshot of Telemetry metrics.
func (t *Telemetry) Snapshot() TelemetrySnapshot {
	var snap TelemetrySnapshot
	if t == nil {
		snap.Requests.ByProtocol = map[string]uint64{core.ProtocolOpenAICompletions: 0, core.ProtocolAnthropicMessages: 0, core.ProtocolOpenAIResponses: 0}
		snap.Requests.ByStatus = map[string]uint64{"ok": 0, "canceled": 0, "error": 0}
		return snap
	}
	snap.Requests.Total = t.requestsTotal.Load()
	snap.Requests.ByProtocol = map[string]uint64{
		core.ProtocolOpenAICompletions: t.reqOpenAI.Load(),
		core.ProtocolAnthropicMessages: t.reqAnthropic.Load(),
		core.ProtocolOpenAIResponses:   t.reqResponses.Load(),
	}
	snap.Requests.ByStatus = map[string]uint64{
		"ok":       t.reqStatusOK.Load(),
		"canceled": t.reqStatusCancel.Load(),
		"error":    t.reqStatusError.Load(),
	}
	snap.Tokens.Total.In = t.tokensIn.Load()
	snap.Tokens.Total.CacheWrite = t.tokensCacheWrite.Load()
	snap.Tokens.Total.CacheRead = t.tokensCacheRead.Load()
	snap.Tokens.Total.Reasoning = t.tokensReasoning.Load()
	snap.Tokens.Total.Out = t.tokensOut.Load()
	return snap
}
