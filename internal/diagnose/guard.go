// Ver 2026-09-16, by Sonnet 5

// Package diagnose guard probe execution
// (the Agent Guard spec §4.10, M5).
// Implements active security diagnostic probes:
// 1. Tool Call rewrite / tampering detection
// 2. Long-context needle-in-a-haystack silent truncation detection
// 3. Thinking / reasoning_content stripping detection
// 4. Token usage inflation detection
// 5. Invisible steganographic rune injection detection
package diagnose

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"vmr/internal/adapter"
	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/probe"
	"vmr/internal/router"
	"vmr/internal/tokenutil"
)

const (
	probeToolCall   = "tool_call_tamper"
	probeNeedle     = "context_truncation"
	probeThinking   = "thinking_signature"
	probeUsage      = "usage_inflation"
	probeInvisRunes = "invisible_runes"
)

var allGuardProbeTypes = []string{
	probeToolCall,
	probeNeedle,
	probeThinking,
	probeUsage,
	probeInvisRunes,
}

type guardProbeTask struct {
	ep        *core.Endpoint
	probeType string
}

// runGuardProbes is the entry point called by diagnose.Run when --guard is enabled.
func runGuardProbes(ctx context.Context, cfg *config.Config, timeout time.Duration, onResult func(Result), progress io.Writer) []Result {
	endpoints := collectAllEndpoints(cfg)
	if progress != nil {
		fmt.Fprintf(progress, "Agent Guard: probing %d endpoint(s) with %d active security checks...\n", len(endpoints), len(allGuardProbeTypes))
	}
	return runGuardProbesPhase(ctx, cfg, endpoints, timeout, onResult)
}

func collectAllEndpoints(cfg *config.Config) []*core.Endpoint {
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		return nil
	}
	return snap.UniqueEndpoints()
}

// runGuardProbesPhase orchestrates the execution of 5 active security probes
// across all configured endpoints concurrently.
func runGuardProbesPhase(ctx context.Context, cfg *config.Config, endpoints []*core.Endpoint, timeout time.Duration, onResult func(Result)) []Result {
	var tasks []guardProbeTask
	for _, ep := range endpoints {
		for _, pt := range allGuardProbeTypes {
			tasks = append(tasks, guardProbeTask{ep: ep, probeType: pt})
		}
	}
	return runConcurrent(tasks, checkConcurrency, func(t guardProbeTask) Result {
		return testEndpointGuard(ctx, cfg, t.ep, t.probeType, timeout)
	}, onResult)
}

// testEndpointGuard dispatches one security probe against an endpoint.
func testEndpointGuard(ctx context.Context, cfg *config.Config, ep *core.Endpoint, probeType string, timeout time.Duration) Result {
	target := fmt.Sprintf("%s/%s/%s [%s]", ep.AdapterType, ep.Provider, ep.Model, probeType)
	ad, ok := adapter.Get(ep.AdapterType)
	if !ok {
		return Result{Phase: "guard", Target: target, Status: StatusFail, Detail: "unknown adapter " + ep.AdapterType}
	}

	// For thinking probes, exempt non-reasoning models to avoid false positives.
	if probeType == probeThinking && !probe.IsReasoningModel(ep.Model) {
		return Result{Phase: "guard", Target: target, Status: StatusOK, Detail: "exempt: non-reasoning model"}
	}

	reqBody, expectedCmd, needleNonce, estTokens := buildGuardProbePayload(ep, probeType)
	creq := &core.CanonicalRequest{Model: ep.Model, Stream: false, Raw: reqBody, Header: probe.RequiredHeaders(ep.AdapterType)}
	req, _, err := ad.BuildRequest(ctx, ep, creq)
	if err != nil {
		return Result{Phase: "guard", Target: target, Status: StatusFail, Detail: "build request: " + err.Error()}
	}

	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req = req.WithContext(tctx)

	p, _ := cfg.ProviderByName(ep.Provider)
	client := router.NewUpstreamClient(cfg, p, ep.AdapterType)
	start := time.Now()
	resp, err := client.Do(req)
	latency := formatSeconds(time.Since(start))
	if err != nil {
		return Result{Phase: "guard", Target: target, Status: StatusFail, Detail: fmt.Sprintf("network error (%s): %v", latency, err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return Result{Phase: "guard", Target: target, Status: StatusFail, Detail: fmt.Sprintf("%d HTTP error (%s): %s", resp.StatusCode, latency, snippet(body))}
	}

	return verifyGuardProbeResponse(ep, probeType, target, body, latency, expectedCmd, needleNonce, estTokens)
}

func buildGuardProbePayload(ep *core.Endpoint, probeType string) (body json.RawMessage, expectedCmd string, nonce string, estTokens int) {
	switch probeType {
	case probeToolCall:
		body, expectedCmd = probe.ToolCallProbeRequest(ep.AdapterType, ep.Model, "echo 'VMR_GUARD_SAFE_TOKEN'")
	case probeNeedle:
		nonce = probe.NewNonce()
		body = probe.NeedleProbeRequest(ep.AdapterType, ep.Model, nonce, 50)
	case probeThinking:
		body = probe.ThinkingProbeRequest(ep.AdapterType, ep.Model)
	case probeUsage:
		var prompt string
		body, prompt = probe.UsageProbeRequest(ep.AdapterType, ep.Model)
		estTokens = int(tokenutil.EstimateText(prompt))
		if estTokens <= 0 {
			estTokens = 5
		}
	case probeInvisRunes:
		body = probe.InvisibleRuneProbeRequest(ep.AdapterType, ep.Model)
	}
	return
}

func verifyGuardProbeResponse(ep *core.Endpoint, probeType, target string, body []byte, latency, expectedCmd, nonce string, estTokens int) Result {
	switch probeType {
	case probeToolCall:
		ok, detail := probe.VerifyToolCallResponse(ep.AdapterType, body, expectedCmd)
		if !ok {
			return Result{Phase: "guard", Target: target, Status: StatusFail, Detail: fmt.Sprintf("TAMPER DETECTED (%s): %s", latency, detail)}
		}
		return Result{Phase: "guard", Target: target, Status: StatusOK, Detail: fmt.Sprintf("intact (%s)", latency)}

	case probeNeedle:
		if !probe.VerifyNeedleResponse(body, nonce) {
			return Result{Phase: "guard", Target: target, Status: StatusFail, Detail: fmt.Sprintf("SILENT TRUNCATION DETECTED (%s): nonce not recovered", latency)}
		}
		return Result{Phase: "guard", Target: target, Status: StatusOK, Detail: fmt.Sprintf("intact (%s)", latency)}

	case probeThinking:
		ok, detail := probe.VerifyThinkingResponse(ep.AdapterType, body)
		if !ok {
			return Result{Phase: "guard", Target: target, Status: StatusFail, Detail: fmt.Sprintf("THINKING STRIPPED (%s): %s", latency, detail)}
		}
		return Result{Phase: "guard", Target: target, Status: StatusOK, Detail: fmt.Sprintf("intact (%s)", latency)}

	case probeUsage:
		ok, ratio, detail := probe.VerifyUsageResponse(body, estTokens)
		if !ok {
			return Result{Phase: "guard", Target: target, Status: StatusFail, Detail: fmt.Sprintf("USAGE INFLATED (ratio %.1fx, %s): %s", ratio, latency, detail)}
		}
		return Result{Phase: "guard", Target: target, Status: StatusOK, Detail: fmt.Sprintf("normal (%s): %s", latency, detail)}

	case probeInvisRunes:
		ok, detail := probe.VerifyInvisibleRuneResponse(body)
		if !ok {
			return Result{Phase: "guard", Target: target, Status: StatusFail, Detail: fmt.Sprintf("STEGANOGRAPHIC INJECTION DETECTED (%s): %s", latency, detail)}
		}
		return Result{Phase: "guard", Target: target, Status: StatusOK, Detail: fmt.Sprintf("clean (%s)", latency)}

	default:
		return Result{Phase: "guard", Target: target, Status: StatusFail, Detail: "unknown probe type: " + probeType}
	}
}
