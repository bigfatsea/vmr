<!-- Ver 2026-07-19 00:00, by Sonnet 5 -->
<!-- keywords: LLM router, LLM gateway, AI agent gateway, agent-first, OpenAI-compatible proxy, Anthropic API proxy, LLM failover, model routing, load balancing, self-hosted, local-first, single binary, MiniMax, DeepSeek, OpenRouter, Claude Code, LiteLLM alternative -->

# vmr — Virtual Model Router

**The transparent LLM router for AI agents.** One model name, every provider, automatic failover — byte-faithful, so your agent's requests reach the provider exactly as written.

English | [简体中文](README.zh.md)

Agents run unattended: no one's watching the terminal at 3 AM when a provider rate-limits, runs out of quota, or goes down. `vmr` sits between your agent runtime and your LLM providers so that when it happens, your agent fails over and keeps working — and you find out from the logs afterward, not from a dead session you have to explain to yourself the next morning.

Point Claude Code, OpenClaw, or any OpenAI/Anthropic SDK at one stable virtual model name (`coding`, `claude`, `agent`). vmr hides the providers, accounts, API keys, priorities, and failover behind it — and unlike SDK-translation gateways, it never rewrites your request into some canonical internal shape first. **→ [Why vmr over LiteLLM](docs/Why_vmr_over_LiteLLM.md)** if you're comparing.

## Why vmr

- **One name, every provider** — swap and reorder upstreams (MiniMax, DeepSeek, OpenRouter, anything protocol-compatible) in a YAML file, hot-reloaded in seconds, no client changes ever.
- **Byte-faithful passthrough** — no intermediate representation, no protocol translation, ever. Requests and responses match a direct provider call byte-for-byte, headers included, except the virtual↔real model-name rewrite and a couple of guarded, evidence-based provider quirk repairs. Unknown API parameters pass through untouched — new provider features work the day they ship, with zero vmr changes.
- **Failover that actually fails over** — health tracking with per-error-class cooldowns (rate limit ≠ dead key ≠ content flag ≠ a relay reporting its own forwarding failure), exponential backoff, `Retry-After` respected. Recovery checks default to a background probe decoupled from real traffic, so one slow or oversized request never blocks concurrent callers waiting on a recovering endpoint (single-flight passive probing still available via `probe_mode: passive`). Content-policy rejections switch providers *without* punishing a healthy endpoint.
- **True streaming** — SSE events are forwarded as they arrive. The normalizer buffers only when it detects a provider's inline-thinking pathology, and resumes live streaming the moment the thinking block closes.
- **Two protocols, one router** — native `POST /v1/chat/completions` (OpenAI) and `POST /v1/messages` (Anthropic) ingress, each routed strictly within its own protocol family. No lossy cross-protocol translation — that's a feature, not a gap.
- **Flight-recorder audit log, agent-shaped** — every request recorded as one JSONL line with both layers (client↔vmr, vmr↔upstream), every failover attempt, and the exact normalizations applied. `vmr report` turns the logs into usage/latency/availability statistics — and it understands agent traffic specifically: requests group into sessions → tasks → turns, each turn's delta gets a 🆕 marker, and a tool-usage report tells you which declared tools are never actually called. Old days auto-compress to `.zst` (20–75× smaller) and can auto-expire.
- **Vision-token diet (optional)** — downscale oversized inline image attachments on the way in; off by default, fail-open, content-hash cached on disk so the same image is never reprocessed.
- **Unix-style tool** — one binary, zero database, zero web UI, zero runtime plugins. Config validation refuses to boot (or hot-load) a broken config. Four direct dependencies, full list in `go.mod`.
- **Measured, not assumed** — load-tested at up to 150 req/s: sub-6ms p95 routing/passthrough overhead on 9 of 11 tested scenarios; the only real cost is optional image downscaling. See [`loadtest/`](loadtest/).

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

# List virtual models (same optional api_keys auth as above)
curl http://127.0.0.1:8800/v1/models -H "Authorization: Bearer sk-vmr-local-xxx"

# Per-endpoint health + concurrency (loopback-only; no api_keys auth required even if configured)
curl http://127.0.0.1:8800/admin/status
```

That's the whole surface a first request needs. Everything past this point — full config reference, protocol/normalization details, the audit log and report format, image downscaling, the complete CLI — lives in the **[User Guide](docs/UserGuide.md)**.

## Learn more

- **[User Guide](docs/UserGuide.md)** — full configuration reference, passthrough/normalization behavior, failover & health details, audit log & `vmr report`, image downscaling, complete CLI reference.
- **[Why vmr over LiteLLM](docs/Why_vmr_over_LiteLLM.md)** — how byte-faithful passthrough compares to translation-based gateways, and when you'd actually want a translation gateway instead.
- **[Design doc](docs/VirtualModelRouter_System_Design_v2.md)** (Chinese) — full architecture and every design decision, with the reasoning behind it.

## Development

```bash
go test -race ./...
```

Adding a provider: OpenAI/Anthropic-compatible vendors are one config entry, zero code. A new protocol = one package under `internal/adapter/<name>/` implementing the three-method interface + one blank import in `cmd/vmr/main.go`.

## License

[MIT](LICENSE)
