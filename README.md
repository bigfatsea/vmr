<!-- Ver 2026-07-08 13:10, by Fable 5 -->
<!-- keywords: LLM router, LLM gateway, OpenAI-compatible proxy, Anthropic API proxy, LLM failover, model routing, load balancing, self-hosted, local-first, single binary, MiniMax, DeepSeek, OpenRouter, Claude Code, LiteLLM alternative -->

# vmr — Virtual Model Router

**One model name. Every provider. Automatic failover. Byte-faithful passthrough.**

English | [简体中文](README.zh.md)

`vmr` is a local-first, single-binary LLM router. Your clients connect to one stable virtual model name (`coding`, `claude`, `agent`) — vmr hides the providers, accounts, API keys, priorities, and failover behind it. When a provider rate-limits, runs out of quota, or goes down at 3 AM, your agent keeps running and you find out from the logs, not from a dead session.

## Why vmr

- **One name, every provider** — point Claude Code, OpenClaw, or any OpenAI/Anthropic SDK at `vmr` once; swap and reorder upstreams (MiniMax, DeepSeek, OpenRouter, anything protocol-compatible) in a YAML file, hot-reloaded in seconds, no client changes ever.
- **Failover that actually fails over** — passive health tracking with per-error-class cooldowns (rate limit ≠ dead key ≠ content flag), exponential backoff, `Retry-After` respected, single-flight recovery probes. Content-policy rejections switch providers *without* punishing a healthy endpoint.
- **Byte-faithful passthrough** — no intermediate representation, no protocol translation, ever. Requests and responses match a direct provider call byte-for-byte, headers included, except the virtual↔real model-name rewrite and a few guarded, evidence-based provider quirk repairs. Unknown API parameters pass through untouched — new provider features work the day they ship.
- **True streaming** — SSE events are forwarded as they arrive. The normalizer buffers only when it detects a provider's inline-thinking pathology, and resumes live streaming the moment the thinking block closes.
- **Two protocols, one router** — native `POST /v1/chat/completions` (OpenAI) and `POST /v1/messages` (Anthropic) ingress, each routed strictly within its own protocol family. No lossy cross-protocol translation — that's a feature, not a gap.
- **Flight-recorder audit log** — every request recorded as one JSONL line with both layers (client↔vmr, vmr↔upstream), every failover attempt, error classes, and the exact list of normalizations applied. `vmr report` turns the logs into usage/latency/availability statistics.
- **Vision-token diet (optional)** — downscale oversized inline image attachments on the way in; one config knob, off by default, fail-open.
- **Unix-style tool** — one binary, zero database, zero web UI, zero runtime plugins. Config validation refuses to boot (or hot-load) a broken config. Dependencies: `yaml.v3`, `fsnotify`, `golang.org/x/image`. That's the whole list.

```
OpenAI client    ──(/v1/chat/completions)──┐         ┌─> MiniMax / DeepSeek / OpenRouter (OpenAI face)
                                           ├── vmr ──┤
Anthropic client ──(/v1/messages)──────────┘         └─> MiniMax / DeepSeek / OpenRouter (Anthropic face)
                                                         failure/cooldown → next endpoint in order
```

## Quick start

```bash
go build -o vmr ./cmd/vmr

cp config.example.yaml config.yaml   # api_key supports ${ENV} expansion
./vmr check -c config.yaml           # validate config, print the routing table
./vmr start -c config.yaml           # foreground; prints a full config summary

# dev mode — quick background run, you supervise (validates first, logs under logs/):
./vmr.sh start          # also: stop / restart / status / logs

# service mode — the OS init system supervises: crash auto-restart + start at login.
# macOS → launchd user agent; Linux → systemd user unit. `install` renders and
# registers the unit, and generates ~/.config/vmr/env (0600) from your current
# shell for every ${VAR} in config.yaml plus proxy vars — init systems start
# with a clean environment and would otherwise see empty keys.
./vmr.sh service install     # register + start (idempotent; rerun to update)
./vmr.sh service status      # also: start / stop / restart / logs
./vmr.sh service uninstall   # stop + unregister
```

Pick one mode at a time — `service install`/`start` stops a dev-mode process automatically. On macOS the service-mode server log lives at `~/Library/Logs/vmr.log` (TCC keeps launchd file ops off external volumes); the audit log follows `VMR_LOG_DIR` as usual.

Point your client's base URL at vmr and you're done:

```bash
# OpenAI-protocol client (OPENAI_BASE_URL=http://127.0.0.1:8800/v1)
curl http://127.0.0.1:8800/v1/chat/completions -H "Content-Type: application/json" \
  -d '{"model":"coding","stream":true,"messages":[{"role":"user","content":"hi"}]}'

# Anthropic-protocol client (e.g. Claude Code: ANTHROPIC_BASE_URL=http://127.0.0.1:8800)
curl http://127.0.0.1:8800/v1/messages -H "Content-Type: application/json" \
  -d '{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}'
```

## Configuration

`providers` and `models` are both nested by protocol: the outer key is the protocol (`openai` / `anthropic`), the inner key the name. A model's endpoints can only reference providers in the same protocol group — mixing protocols in one model has no syntax to write, rather than being a config error to catch. The same short name (`openrouter`) can appear once per protocol group for the two faces of one account:

```yaml
listen: 127.0.0.1:8800
# api_key: sk-vmr-xxx          # optional: protect vmr itself (Bearer or x-api-key)
# max_concurrency: 8           # global gate; excess requests wait in memory (default: unlimited)
# image_downscale: 512         # long-side px cap for inline request images (default: off)

providers:
  openai:
    openrouter:
      base_url: https://openrouter.ai/api/v1
      api_key: ${OPENROUTER_API_KEY}
  anthropic:
    openrouter:                # same account's Anthropic face; same name, no conflict
      base_url: https://openrouter.ai/api/v1
      api_key: ${OPENROUTER_API_KEY}

models:
  openai:
    coding:
      endpoints:
        - {provider: openrouter, model: z-ai/glm-5.2}   # no priority field: list order = try order
  anthropic:
    claude:                    # anthropic protocol → served via /v1/messages
      endpoints:
        - {provider: openrouter, model: minimax/minimax-m3}
```

All fields and validation rules: design doc §10. Config edits hot-reload within seconds; a broken config is rejected and the running instance keeps its current one.

## Passthrough & normalization

**Principle: direct-connection equivalence.** What a client receives through vmr — bytes, headers, transfer pacing — matches a direct provider call. The only deviations:

- the `model` field — rewritten to the real upstream name on the way out, back to your virtual name on the way in (SDKs assume `response.model === request.model`);
- two **MiniMax-M3-specific repairs**, each gated on detecting its exact upstream shape: stripping inline `<think>…</think>` reasoning from content (left in place, it gets persisted into history and locks the model into a feedback loop), and stripping the plain-text "Thinking Process:" section emitted under `thinking=medium`;
- the `data: [DONE]` sentinel — appended **only** for OpenAI-protocol SSE when the upstream omitted it; never duplicated, never injected into Anthropic streams.

Streaming is real: events forward as they arrive; buffering engages only for detected thinking shapes and resumes live streaming once `</think>` closes. Compressed (`Content-Encoding`) bodies pass through untouched. Response headers follow the same policy as request headers — everything forwarded except hop-by-hop; error responses return verbatim with status, headers (`Retry-After` included), and body. Every normalization actually applied is recorded in the audit log (`attempts[].norm`), so any byte difference between upstream and client is explained, per request.

## Failover & health

On upstream failure vmr walks the endpoint list in order until one succeeds or all are exhausted (`max_attempts` optionally caps the walk). Health is failure-driven — no paid active probing:

- network/5xx: short cooldown with exponential backoff; 401/quota-exhausted/unknown-model: long cooldown; 429/503: `Retry-After` honored;
- expired cooldowns admit a **single probe request** (no thundering herd);
- 400-class client errors return immediately — no pointless retries;
- **content-policy blocks** fail over to the next provider but do **not** penalize the blocked endpoint — it rejected one request; it isn't down.

All-candidates-failed returns the last upstream error verbatim. Streams only fail over before the first byte is written.

## Audit log & usage reports

On by default: one JSONL line per request with both layers (client↔vmr and every vmr↔upstream attempt), credential-masked headers, error classes, and applied normalizations. Body recording cap tracks `max_body_mb`, so accepted requests are never truncated in their own audit trail.

```bash
./vmr start -c config.yaml                 # writes to $VMR_LOG_DIR (or system temp dir)
./vmr start -c config.yaml -audit=false    # off
jq '.model, .outcome, .attempts[0].norm' vmr-audit-2026-07-08.jsonl

./vmr report logs/vmr-audit-*.jsonl        # → vmr-report.json + vmr-report.md
```

`vmr report` aggregates tokens *and* bytes (bytes as the fallback when a provider omits usage): per date × protocol × model rows, per-endpoint availability and error distribution, latency percentiles, throughput. The JSON is the data source for any dashboarding you want to build on top.

## Request image downscaling

Optional, off by default. When enabled, inline base64 image attachments exceeding the configured long-side limit are proportionally resized and re-encoded as JPEG before hitting the upstream — cutting vision-token cost for screenshot-heavy agent workflows. Requests only, never responses, never remote URLs; animated images and anything that fails to decode pass through untouched (fail-open).

```yaml
image_downscale: 512   # long-side px cap; 0 or absent = off
```

## Endpoints & CLI

| Endpoint / command | Purpose |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI-protocol ingress (streaming + non-streaming) |
| `POST /v1/messages` | Anthropic-protocol ingress (streaming + non-streaming) |
| `GET /v1/models` | virtual model list (parseable by both SDK families) |
| `GET /admin/status` | endpoint health + concurrency metrics (loopback only) |
| `vmr check -c config.yaml` | validate config, print routing table and key status |
| `vmr status -c config.yaml` | render a running instance's health and concurrency |
| `vmr report [-o dir] <glob>` | audit logs → usage statistics (JSON + Markdown) |
| `./vmr.sh start\|stop\|…` | dev-mode lifecycle (you supervise) |
| `./vmr.sh service install\|uninstall\|start\|…` | init-system service (launchd/systemd: crash restart, start at login) |

Routed responses carry `X-VMR-Endpoint` (the endpoint that served it) and `X-VMR-Attempts` (tries used).

## Development

```bash
go test -race ./...
```

Adding a provider: OpenAI/Anthropic-compatible vendors are one config entry, zero code. A new protocol = one package under `internal/adapter/<name>/` implementing the three-method interface + one blank import in `cmd/vmr/main.go`.

Architecture and every design decision (with the war stories behind them): [design doc](VirtualModelRouter_v2_Fable5.md) (Chinese).

## License

[MIT](LICENSE)
