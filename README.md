<!-- Ver 2026-07-10 00:30, by Sonnet 5 -->
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
- **Flight-recorder audit log** — every request recorded as one JSONL line with both layers (client↔vmr, vmr↔upstream), every failover attempt, error classes, and the exact list of normalizations applied. `vmr report` turns the logs into usage/latency/availability statistics, including a cache-hit breakdown of input tokens. Old days auto-compress to `.zst` (20–75× smaller, `vmr report` reads it transparently) and can auto-expire via `audit_retention_days`.
- **Vision-token diet (optional)** — downscale oversized inline image attachments on the way in; a global knob plus a per-virtual-model override, off by default, fail-open; downscaled results are content-hash cached on disk so the same image is never reprocessed and never breaks an upstream prompt cache.
- **Unix-style tool** — one binary, zero database, zero web UI, zero runtime plugins. Config validation refuses to boot (or hot-load) a broken config. Dependencies: `yaml.v3`, `fsnotify`, `golang.org/x/image`, `klauspost/compress` (zstd for audit-log compression). That's the whole list.

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
# Linux: run `loginctl enable-linger $USER` once if the service must survive logout.
```

Pick one mode at a time — `service install`/`start` stops a dev-mode process automatically. On macOS the service-mode server log lives at `~/Library/Logs/vmr.log` (TCC keeps launchd file ops off external volumes); the audit log follows `VMR_LOG_DIR` as usual.

Point your client's base URL at vmr and you're done:

```bash
# OpenAI-protocol client (OPENAI_BASE_URL=http://127.0.0.1:8800/v1)
# add -H "Authorization: Bearer <api_key>" if vmr's own api_key is set in config.yaml
curl http://127.0.0.1:8800/v1/chat/completions -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-vmr-local-xxx" \
  -d '{"model":"coding","stream":true,"messages":[{"role":"user","content":"hi"}]}'

# Anthropic-protocol client (e.g. Claude Code: ANTHROPIC_BASE_URL=http://127.0.0.1:8800)
# Anthropic clients send x-api-key instead of Authorization — vmr accepts either
curl http://127.0.0.1:8800/v1/messages -H "Content-Type: application/json" \
  -H "x-api-key: sk-vmr-local-xxx" \
  -d '{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}'

# List virtual models (same optional api_key as above)
curl http://127.0.0.1:8800/v1/models -H "Authorization: Bearer sk-vmr-local-xxx"

# Per-endpoint health + concurrency (loopback-only; no api_key required even if one is set)
curl http://127.0.0.1:8800/admin/status
```

## Configuration

`providers` and `models` are both nested by protocol: the outer key is the protocol (`openai` / `anthropic`), the inner key the name. A model's endpoints can only reference providers in the same protocol group — mixing protocols in one model has no syntax to write, rather than being a config error to catch. The same short name (`openrouter`) can appear once per protocol group for the two faces of one account:

```yaml
listen: 127.0.0.1:8800
# api_key: sk-vmr-xxx          # optional: protect vmr itself (Bearer or x-api-key)
# max_attempts: 0              # cap on upstream tries per request (default 0 = walk every candidate)
# max_body_mb: 8               # request body buffer limit; also caps audit body recording
# max_concurrency: 8           # global gate; excess requests wait in memory (default: unlimited)
# image_downscale: 512         # long-side px cap for inline request images (default: off; a model's own setting overrides this, see below)
# image_cache_ttl_days: 7      # eviction age for cached downscale results (default: 7 days)
# audit_retention_days: 30     # delete audit files older than this (default: keep forever)
# timeouts:
#   connect: 10s               # upstream dial
#   response_header: 120s      # upstream time-to-first-byte
#   stream_idle: 120s          # abort any upstream body (stream, JSON, error) silent for this long

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
./vmr start -c config.yaml                 # writes to $VMR_LOG_DIR, or `vmr dirs log` if unset
./vmr start -c config.yaml -audit=false    # off
jq '.model, .outcome, .attempts[0].norm' vmr-audit-2026-07-08.jsonl

./vmr report "$(./vmr dirs log)/vmr-audit-*.jsonl*"   # → vmr-report.json + vmr-report.md (plain + .zst mix ok)
```

`vmr report` aggregates tokens *and* bytes (bytes as the fallback when a provider omits usage): per date × protocol × model rows, per-endpoint availability and error distribution, latency percentiles, throughput. Token totals also split out cache-read and (Anthropic) cache-write tokens, so you can see how much of your input-token bill is actually cache hits. The JSON is the data source for any dashboarding you want to build on top.

Agent workloads resend the full conversation on every turn, so a day's log can run into gigabytes — mostly repeated across lines, not within one. Each day's file rotates and compresses automatically once it's no longer "today": zstd on the whole file (not per-line) catches that cross-line repetition, typically 20–75× smaller in practice — far beyond what compressing each record on its own could reach, since a single record never sees the previous turn's near-duplicate body. `vmr report` reads `.jsonl` and `.jsonl.zst` interchangeably, so point it at a glob covering both. Set `audit_retention_days` to also delete files past a given age (default: keep forever — nothing is deleted unless you opt in); either way, deletion and compression are both keyed off the date in the filename, so housekeeping never needs to scan or `stat` the whole log directory. Details and the numbers behind this: `docs/AuditLogCompression_Analysis_Sonnet5.md`.

## Request image downscaling

Optional, off by default. When enabled, inline base64 image attachments exceeding the configured long-side limit are proportionally resized and re-encoded as JPEG before hitting the upstream — cutting vision-token cost for screenshot-heavy agent workflows. Requests only, never responses, never remote URLs; animated images and anything that fails to decode pass through untouched (fail-open).

```yaml
image_downscale: 512      # global long-side px cap; 0 or absent = off
image_cache_ttl_days: 7   # eviction age for the on-disk downscale cache (default 7 days, see below)

models:
  openai:
    coding:
      image_downscale: 1024   # overrides the global value, only for this virtual model
      endpoints: [...]
    cheap:
      image_downscale: 0      # explicitly off, even though the global setting is on
      endpoints: [...]
```

**Per-model override**: any virtual model can set its own `image_downscale`, which always wins over the global value; omitting it inherits the global setting. `image_downscale: 0` on a model is an explicit "off" — even with the global setting on — because "not set" and "set to 0" mean different things (inherit vs. force-disable).

**Downscale result cache**: the first time a given source image is downscaled to a given target size, the result (JPEG bytes) is cached on disk keyed by content hash, under `$VMR_IMG_CACHE_DIR` if set, else `vmr dirs cache`'s default. A later request for the same image reuses the cached bytes verbatim instead of decoding/scaling/re-encoding. Two reasons this matters: it saves CPU (agent workflows resend the full conversation, images included, on every turn), and it protects the upstream's own prompt cache — which is keyed on exact byte/token match, so re-encoding the same image on every request can produce subtly different output bytes and silently defeat that cache, while identical cached bytes always hit it. Entries are evicted by last-hit time (`image_cache_ttl_days`, default 7 days; a hit refreshes the clock, so an image reused throughout a long conversation is never evicted mid-session), swept lazily off normal cache access rather than a dedicated timer.

**Where the audit and cache directories actually land**: both default to `$VMR_LOG_DIR`/`$VMR_IMG_CACHE_DIR` if set (used exactly as given), else a `vmr_logs`/`vmr_image_cache` subdirectory of the system temp dir, else (only if the OS somehow can't supply a temp dir at all) `./logs`/`./image_cache` next to the binary. Run `vmr dirs log` / `vmr dirs cache` to see the resolved path without starting the server. `vmr.sh` uses this same formula — by calling `vmr dirs` itself rather than guessing — for **both** dev mode and `service install`, so a launchd/systemd-supervised vmr never disagrees with a manually-started one about where its data lives.

## Endpoints & CLI

| Endpoint / command | Purpose |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI-protocol ingress (streaming + non-streaming) |
| `POST /v1/messages` | Anthropic-protocol ingress (streaming + non-streaming) |
| `GET /v1/models` | virtual model list (parseable by both SDK families) |
| `GET /admin/status` | endpoint health + concurrency metrics (loopback only) |
| `vmr check -c config.yaml` | validate config, print routing table and key status |
| `vmr status -c config.yaml` | render a running instance's health and concurrency |
| `vmr report [-o dir] <glob>` | audit logs (plain or `.zst`) → usage statistics (JSON + Markdown) |
| `vmr dirs log\|cache` | print the resolved default audit/cache directory (what `vmr.sh` queries internally) |
| `./vmr.sh start\|stop\|…` | dev-mode lifecycle (you supervise) |
| `./vmr.sh service install\|uninstall\|start\|…` | init-system service (launchd/systemd: crash restart, start at login) |

Routed responses carry `X-VMR-Endpoint` (the endpoint that served it) and `X-VMR-Attempts` (tries used).

## Development

```bash
go test -race ./...
```

Adding a provider: OpenAI/Anthropic-compatible vendors are one config entry, zero code. A new protocol = one package under `internal/adapter/<name>/` implementing the three-method interface + one blank import in `cmd/vmr/main.go`.

Architecture and every design decision (with the war stories behind them): [design doc](docs/VirtualModelRouter_v2_Fable5.md) (Chinese).

## License

[MIT](LICENSE)
