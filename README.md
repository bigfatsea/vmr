<!-- Ver 2026-08-02 20:22, by Gemini 3.6 Flash -->
<!-- keywords: LLM router, LLM gateway, AI agent gateway, agent-first, OpenAI-compatible proxy, Anthropic API proxy, LLM failover, model routing, load balancing, self-hosted, local-first, single binary, MiniMax, DeepSeek, OpenRouter, Claude Code, LiteLLM alternative -->

# vmr — Virtual Model Router

**The flight recorder for AI agents that run unattended.** vmr sits between your agent and its LLM providers, capturing every request and response byte-for-byte — so when a 3 AM failover, a silent content-policy block, or a lost context window happens, you find out from `vmr report` / `vmr story` afterward, not from a dead session you have to explain to yourself the next morning.

English | [简体中文](README.zh.md)

One stable virtual model name (`coding`, `claude`, `agent`) hides every provider, account, key, priority, and failover rule behind it — point Claude Code, OpenClaw, or any OpenAI/Anthropic SDK at it and you're done. Byte-faithful passthrough (no protocol translation, ever) isn't the headline feature here — it's *why the recording can be trusted*: nothing vmr logs is something vmr itself rewrote first. **→ [Why vmr over LiteLLM](docs/Why_vmr_over_LiteLLM.md)** if you're comparing gateways.

## Why vmr

- **Flight-recorder audit log, agent-shaped** — every request recorded as one JSONL line with both layers (client↔vmr, vmr↔upstream), every failover attempt, and the exact normalizations applied. `vmr report` turns the logs into usage/latency/availability statistics — and it understands agent traffic specifically: requests group into sessions → tasks → turns, each turn's delta gets a 🆕 marker, and a tool-usage report tells you which declared tools are never actually called. `vmr story` goes one level deeper: reconstructs a single agent task's full execution history into a readable narrative (what context entered, what the model did with it, where a history-compaction event lost information), plus a rule-derived behavior profile you can diff between two runs. Old days auto-compress to `.zst` (20–75× smaller) and can auto-expire.
- **Failover that actually fails over** — health tracking with per-error-class cooldowns (rate limit ≠ dead key ≠ content flag ≠ a relay reporting its own forwarding failure), exponential backoff, `Retry-After` respected. Recovery checks always run as a background probe decoupled from real traffic, so one slow or oversized request never blocks concurrent callers waiting on a recovering endpoint. Content-policy rejections switch providers *without* punishing a healthy endpoint.
- **One name, every provider** — swap and reorder upstreams (MiniMax, DeepSeek, OpenRouter, anything protocol-compatible) in a YAML file, hot-reloaded in seconds, no client changes ever. `base_url` path overlap is auto-eliminated — write it with or without `/v1`, vmr always produces the correct URL.
- **Byte-faithful passthrough** — no intermediate representation, no protocol translation, ever. Requests and responses match a direct provider call byte-for-byte, headers included, except the virtual↔real model-name rewrite and a couple of guarded, evidence-based provider quirk repairs. Upstream 3xx redirects are passed through untouched (never silently followed). Unknown API parameters pass through untouched — new provider features work the day they ship, with zero vmr changes.
- **Condition-aware, session-sticky routing** — endpoints declare what they actually support (`capabilities: [image, tools]`, `max_context_tokens`); a request needing something an endpoint doesn't declare skips it, with a built-in fallback so a conservative size estimate can never block a request that would have worked. Multi-turn conversations stay pinned to whichever endpoint's upstream prompt cache is already warm, so smarter routing can't quietly cost you more by switching providers mid-conversation.
- **True streaming** — SSE events are forwarded as they arrive. The normalizer buffers only when it detects a provider's inline-thinking pathology, and resumes live streaming the moment the thinking block closes.
- **Three protocols, one router** — native `POST /v1/chat/completions` (OpenAI Chat Completions), `POST /v1/messages` (Anthropic Messages), and `POST /v1/responses` (OpenAI Responses) ingress, each routed strictly within its own protocol family. No lossy cross-protocol translation — that's a feature, not a gap.
- **Vision-token diet (optional)** — downscale oversized inline image attachments on the way in; off by default, fail-open, content-hash cached on disk so the same image is never reprocessed.
- **Unix-style tool** — one binary, zero database, zero web UI, zero runtime plugins. Config validation refuses to boot (or hot-load) a broken config. Four direct dependencies, full list in `go.mod`.
- **Measured, not assumed** — load-tested at up to 150 req/s: sub-10ms p95 routing/passthrough overhead on 10 of 12 tested scenarios; the only real cost is optional image downscaling. See [`loadtest/`](loadtest/).

```
OpenAI client        ──(/v1/chat/completions)──┐       ┌─> MiniMax / DeepSeek / OpenRouter (OpenAI face)
Anthropic client     ──(/v1/messages)──────────┼─ vmr ─┼─> MiniMax / DeepSeek / OpenRouter (Anthropic face)
OpenAI Responses SDK ──(/v1/responses)─────────┘       └─> DeepSeek / OpenRouter (Responses face)
                                                            failure/cooldown → next endpoint in order
```

## What it looks like

This is real output, not a mockup — from a tiny synthetic session checked into [`examples/sample-audit.jsonl`](examples/sample-audit.jsonl). Run it yourself:

```bash
./vmr report -o /tmp/vmr-example examples/sample-audit.jsonl
./vmr story  -o /tmp/vmr-example examples/sample-audit.jsonl
```

**A failover, caught in the act.** The primary endpoint silently content-blocks the request; vmr retries the same request on the backup endpoint, and the client only ever sees a normal 200. This is `details/*.md`, one file per request:

```
### Attempt 1/2 · openai:coder-primary:coder-large · ❌ · HTTP 403 · 900ms

❌ **error**: content

response body (137B):
{
  "error": {
    "code": "content_flagged",
    "message": "This request was flagged by our safety guardrail and blocked.",
    "type": "guardrail_blocked"
  }
}

### Attempt 2/2 · openai:coder-backup:coder-large-mini · ✅ · HTTP 200 · 2.5s
```

**The aggregate view**, `vmr-report.md`:

```
## §0 Summary

| Requests | Success Rate | Billed Input (fresh)⭐ | Cache Efficiency⭐ | p95 Duration |
|---|---|---|---|---|
| 5 (fallback 1 / trunc 0) | 100.0% | 730 | 54.0% | 3.4s (n=5 ⚠️low-n) |

**Highlights (auto):**
- ⚠️ **Endpoint openai:coder-primary:coder-large error rate 20.0/100** (worst), top cause content ×1
```

## Quick start

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

```bash
cp config.example.yaml config.yaml   # api_key supports ${ENV} expansion
./vmr check -c config.yaml           # validate config, print the routing table
./vmr start -c config.yaml           # foreground; prints a full config summary

# dev mode — quick background run, you supervise (validates first, logs under logs/):
./vmr.sh start          # also: stop / restart / status / logs
./vmr.sh ps             # every vmr instance on this machine: port + config + uptime
./vmr.sh <check|diagnose|report|…>   # any vmr subcommand, forwarded to the binary

# service mode — the OS init system supervises: crash auto-restart + start at login.
# macOS → launchd user agent; Linux → systemd user unit. `install` renders and
# registers the unit, and generates ~/.config/vmr/env (0600) from your current
# shell for every ${VAR} referenced in config.yaml — init systems start with a
# clean environment and would otherwise see empty keys.
./vmr.sh service install     # register + start (idempotent; rerun to update)
./vmr.sh service status      # also: start / stop / restart / logs
./vmr.sh service uninstall   # stop + unregister
# Linux: run `loginctl enable-linger $USER` once if the service must survive logout.
```

Pick one mode at a time — `service install`/`start` stops a dev-mode process automatically. On macOS the service-mode server log lives at `~/Library/Logs/vmr.log` (TCC keeps launchd file ops off external volumes); the audit log follows the config's `log_dir` as usual.

Point your client's base URL at vmr and you're done:

```bash
# OpenAI-protocol client (OPENAI_BASE_URL=http://127.0.0.1:8800/v1)
# add -H "Authorization: Bearer <key>" if vmr's own api_keys is set in config.yaml
curl http://127.0.0.1:8800/v1/chat/completions -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-vmr-local-xxx" \
  -d '{"model":"coding","stream":true,"messages":[{"role":"user","content":"hi"}]}'

# Anthropic-protocol client (e.g. Claude Code: ANTHROPIC_BASE_URL=http://127.0.0.1:8800)
# Anthropic clients send x-api-key instead of Authorization — vmr accepts either
curl http://127.0.0.1:8800/v1/messages -H "Content-Type: application/json" \
  -H "x-api-key: sk-vmr-local-xxx" \
  -d '{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}'

# OpenAI Responses-protocol client (client.responses.create(...), or OPENAI_BASE_URL
# for tools that default to wire_api=responses); requires a provider with a
# Responses-compatible face configured (protocol: openai-responses)
curl http://127.0.0.1:8800/v1/responses -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-vmr-local-xxx" \
  -d '{"model":"coding","input":"hi"}'

# List virtual models (same optional api_keys auth as above)
curl http://127.0.0.1:8800/v1/models -H "Authorization: Bearer sk-vmr-local-xxx"

# Per-endpoint health + concurrency (loopback-only; no api_keys auth required even if configured)
curl http://127.0.0.1:8800/admin/status
```

That's the whole surface a first request needs. Everything past this point — full config reference, protocol/normalization details, the audit log and report format, image downscaling, the complete CLI — lives in the **[User Guide](docs/UserGuide.md)**.

## Learn more

- **[User Guide](docs/UserGuide.md)** — full configuration reference, passthrough/normalization behavior, failover & health details, audit log & `vmr report`, image downscaling, complete CLI reference.
- **[Why vmr over LiteLLM](docs/Why_vmr_over_LiteLLM.md)** — how byte-faithful passthrough compares to translation-based gateways, and when you'd actually want a translation gateway instead.
- **Design doc** (Chinese, two parts) — full architecture and every design decision, with the reasoning behind it: [Part 1 — routing core](docs/VirtualModelRouter_Design_v4_Core.md), [Part 2 — `vmr report`/`vmr story`](docs/VirtualModelRouter_Design_v4_Analytics.md).

## Development

```bash
go test -race ./...
```

Adding a provider: OpenAI/Anthropic-compatible vendors are one config entry, zero code. A new protocol = one package under `internal/adapter/<name>/` implementing the four-method `Adapter` interface + one blank import in `cmd/vmr/main.go`.

## License

[MIT](LICENSE)
