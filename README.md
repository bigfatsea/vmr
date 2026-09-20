<!-- Ver 2026-09-12 18:00, by pi -->
<!-- keywords: LLM router, LLM gateway, AI agent gateway, agent-first, OpenAI-compatible proxy, Anthropic API proxy, LLM failover, model routing, load balancing, self-hosted, local-first, single binary, MiniMax, DeepSeek, OpenRouter, Claude Code, LiteLLM alternative, flight recorder, agent audit, request replay -->

# <img src="docs/vmr-logo.svg" alt="vmr" width="24" height="28" align="absmiddle" style="vertical-align: middle; margin-right: 4px;" /> vmr — Zero-Instrumentation Router & Flight Recorder for AI Agents

**vmr** is a single-binary router and flight recorder for AI agents that run unattended. One stable virtual model name (`coding`, `claude`, `agent`) hides every provider, key, and failover rule behind it — point any OpenAI/Anthropic-compatible client's `base_url` at vmr and you're done, **zero SDK modifications or code instrumentation required**.

That same byte-faithfulness — no protocol translation, ever — is what makes the recording trustworthy: nothing vmr logs is something vmr itself rewrote first. Every request becomes a `requests/details/` audit entry, an agent execution narrative (`vmr analyze -journey`), a cross-run divergence diff (`vmr analyze -compare id1,id2`), a structural diff (`vmr diff`), or an exact 1-click replay (`vmr replay`). When a 3 AM failover or a silent content-block happens, you find out from the log afterward, not from a dead session you have to explain to yourself the next morning.

English | [简体中文](README.zh.md)

[ Quick Start ](#quick-start) | [ User Guide ](docs/UserGuide.md)

```
[ Agent App / SDK ] ──(Zero Instrumentation)──> [ vmr Router ] ──(Passthrough)──> [ LLM Providers ]
                                                      │
                                           (Byte-Faithful Audit)
                                                      │
                       ┌──────────────────────────────┼──────────────────────────────┐
                       ▼                              ▼                              ▼
             [ 1-Click Replay ]             [ vmr analyze / details ]       [ journey / compare ]
```

## See It in Action

### 1. In-Flight Failover Evidence (`requests/details/r-*.md`)
Real output from the checked-in [`examples/sample-audit.jsonl`](examples/sample-audit.jsonl) — run `./vmr analyze -details -o /tmp/out examples/sample-audit.jsonl` and compare. The primary endpoint silently content-blocks the request; vmr retries the same payload on the backup endpoint, so the client only sees a 200 OK:

```
### Attempt 1/2 · openai-completions:coder-primary:coder-large · ❌ HTTP 403
{"error": {"code": "content_flagged", "message": "This request was flagged by our safety guardrail and blocked.", "type": "guardrail_blocked"}}

### Attempt 2/2 · openai-completions:coder-backup:coder-large-mini · ✅ HTTP 200 (2.5s)
```

### 2. Agent Execution Narrative & Context Loss (`vmr analyze -journey <id>`)
What a real multi-tool agent run looks like reconstructed into tasks, steps, and context-compaction boundaries:

```
Task 1: Search codebase and outline implementation
  Step 1: user instruction -> 🆕 3 files inspected -> assistant response
  --- ⚠️ Context Compaction Boundary: 18.5K tokens -> 4.2K tokens ---
  Dropped Entities: [internal/core/router.go, https://docs.example.com/api]
```

### 3. Divergence Point & LLM Cause Analysis (`vmr analyze -compare id1,id2`)
Comparing two runs of the same task (e.g. OpenClaw vs Lobster, or DeepSeek vs Claude) pinpoints exactly where and why they parted ways:

```
⚡ Divergence Point detected at Step 1 (DivergenceHeavy)
- Journey A: Step 1 used [memory_search, read]
- Journey B: Step 1 used [web_fetch]

## LLM Interpretation (Model: agent · Divergence Point)
| Candidate Root Cause | Direct Evidence | Confidence | Suggestion |
|---|---|---|---|
| Initial strategy split | Journey A loaded local context; Journey B fetched live web pages | High | Align system prompt instructions for initial tool choice |
```

## Dual Core Pillars

### Pillar A: In-Flight Transparent Router & Reliability
- **Zero Instrumentation**: Drop-in proxy via `base_url` — zero code changes or SDK tracing required. Native OpenAI (`/v1/chat/completions`), Anthropic (`/v1/messages`), and OpenAI Responses (`/v1/responses`) ingress.
- **Error-Class Aware Failover**: Smartly retries across backup endpoints for rate-limits, dead keys, or content blocks; background probes handle endpoint recovery without blocking live traffic.
- **Session-Sticky Prompt Cache Protection**: Pins multi-turn agent conversations to warm upstream prompt caches, preventing routing from inflating token costs.
- **Byte-Faithful Passthrough**: Zero protocol translation or parameter tampering. Upstream vendor features work on day one. Includes evidence-based vendor quirk repairs (MiniMax `<think>` stripping) and audit norm tracking (`soft_block_detected`).
- **Measured, Not Assumed**: Comprehensive load testing across 17 scenarios (plain, stream, and image regimes, stressing sustained SSE concurrency, quota pacing, and sticky session affinity) with p95 router overhead remaining under 10ms. See [`loadtest/`](loadtest/).
- **Live Observability**: Zero-dependency live telemetry (`internal/livestats`) with hourly slim WAL, local rollup archives, nearest-rank TTFT and token generation throughput percentiles (p50/p10), and unified built-in CSR console (`/status.html`, `/models.html`, `/log.html`, `/help.html`).

### Pillar B: Post-Flight Audit, Journeys & Forensic Replay
- **Two-Layer Raw Byte Audit**: Log client-side and upstream-side payloads verbatim for complete transparency without HTML escaping.
- **1-Click Request Replay (`vmr replay`) & Structural Comparison (`vmr diff`)**: Re-issue any historical request using exact raw payloads to reproduce bugs, or diff any two request coordinates across headers, system prompts, tools, and message LCP.
- **Unified Analysis Suite (`vmr analyze`)**: One command, domain slices (`macro/*.json`), and a verified `manifest.json`. Renders complete Markdown reports, web dashboard skeleton pages, and supports fast `-render-only` re-generation and `-no-cache` bypass.
- **Aggregate Report**: Groups raw HTTP calls into sessions -> tasks -> turns, tracks newly-added context (`🆕`), flags declared-but-never-called tool schemas, and models pay-as-you-go equivalent costs.
- **Agent Task Narrative (`vmr analyze -journey <id>`)**: Reconstructs one task's full execution into a step-by-step narrative with prompt cache break attribution, touched workspace artifacts tracking, and compaction loss detection.
- **Behavioral Profiling & Divergence Detection (`vmr analyze -compare id1,id2`)**: Diffs core behavior metrics across runs or agent frameworks, clusters repeated task runs, and pinpoints exact Step-level divergence points with optional LLM cause hypotheses (`-llm-addr`).

## Quick Start

### 1. Installation

```bash
# macOS
brew install bigfatsea/tap/vmr
```

Or download a prebuilt binary for your platform from the [latest release](https://github.com/bigfatsea/vmr/releases/latest) (darwin/linux, amd64/arm64) — no Go toolchain required.

<details>
<summary>Build from source instead</summary>

```bash
go build -o vmr ./cmd/vmr
```
</details>

### 2. Run

```bash
cp config.minimal.yaml config.yaml   # ~10 lines to start; config.example.yaml is the full annotated reference
export DEEPSEEK_API_KEY=sk-...        # the ${ENV} the minimal config references
./vmr check -c config.yaml           # validate config, print the routing table
./vmr start -c config.yaml           # foreground run

# Or background dev mode via vmr.sh:
./vmr.sh start          # also: stop / restart / redeploy / status / logs / ps

# Or OS init service mode (launchd / systemd):
./vmr.sh service install     # register + start
```

### 3. Verify

```bash
./vmr diagnose -c config.yaml   # one screen of red/green: config check, DNS + TLS, a real echo request per endpoint, routing preview
```

### 4. Connect (Zero Code Changes)

Point your client's base URL at vmr:

```bash
# OpenAI protocol
OPENAI_BASE_URL=http://127.0.0.1:8800/v1

# Anthropic protocol (e.g. Claude Code)
ANTHROPIC_BASE_URL=http://127.0.0.1:8800
```

<details>
<summary>Curl & API Test Examples</summary>

```bash
# OpenAI Chat Completions
curl http://127.0.0.1:8800/v1/chat/completions -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-vmr-local-xxx" \
  -d '{"model":"coding","stream":true,"messages":[{"role":"user","content":"hi"}]}'

# Anthropic Messages
curl http://127.0.0.1:8800/v1/messages -H "Content-Type: application/json" \
  -H "x-api-key: sk-vmr-local-xxx" \
  -d '{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}'

# OpenAI Responses
curl http://127.0.0.1:8800/v1/responses -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-vmr-local-xxx" \
  -d '{"model":"coding","input":"hi"}'

# Status & Health (JSON & Web Console)
curl http://127.0.0.1:8800/status
curl http://127.0.0.1:8800/stats
# Unified Built-in Console (Overview, Models, Live Terminal Log & Agent Guides):
# - Overview: http://127.0.0.1:8800/status.html
# - Models & Quotas: http://127.0.0.1:8800/models.html
# - Live Terminal Log: http://127.0.0.1:8800/log.html
# - Agent Configuration Guide: http://127.0.0.1:8800/help.html
```
</details>

### 5. Analyze

```bash
./vmr analyze -c config.yaml   # one call, one output dir: aggregate report + every task journey, cross-linked
```

`-journey <id>`/`-compare id1,id2`/`-benchmark` zoom into a single task narrative, a pairwise behavior diff, or benchmark statistics across every candidate journey instead of the default full suite.

Everything past this point lives in the **[User Guide](docs/UserGuide.md)**.

## Why vmr vs Translation-Based Gateways

| Dimension | Translation Gateways (LiteLLM / Bifrost) | vmr (Router + Flight Recorder) |
|---|---|---|
| **Architecture** | Translates everything to OpenAI format | Byte-faithful passthrough (native 3-protocol ingress) |
| **Setup & Dependencies** | Complex setup, PostgreSQL DB, Web UI | Single binary, zero DB, zero code changes |
| **Audit Logging** | Metadata / Summarized JSON | Two-layer raw byte audit + 1-click `vmr replay` |
| **Agent Forensics** | Flat request logs | Task/Step narratives (`vmr analyze -journey`) & Divergence Diff |

## Learn More

- **[User Guide](docs/UserGuide.md)** — full configuration reference, passthrough/normalization behavior, failover & health details, audit log & `vmr analyze`, complete CLI reference.
- **Design Docs** (Chinese) — [Part 1: Routing Core](docs/VirtualModelRouter_Design_v4_Core.md) and [Part 2: Analytics & Journeys](docs/VirtualModelRouter_Design_v4_Analytics.md), plus three topic pieces: [Quota-Aware Routing](docs/VirtualModelRouter_Design_v4_Quota.md), [Live Stats & Telemetry](docs/VirtualModelRouter_Design_v4_LiveStats.md), and [Strategy & Competitive Landscape](docs/VirtualModelRouter_Design_v4_Strategy.md).

## Development

```bash
go test -race ./...
```

Adding a provider: OpenAI/Anthropic-compatible vendors are one config entry, zero code. A new protocol = one package under `internal/adapter/<name>/` implementing the `Adapter` interface + one blank import in `cmd/vmr/main.go`.

## License

[MIT](LICENSE)
