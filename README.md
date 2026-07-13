<!-- Ver 2026-07-13 04:00, by Sonnet 5 -->
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
- **Flight-recorder audit log** — every request recorded as one JSONL line with both layers (client↔vmr, vmr↔upstream), every failover attempt, error classes, and the exact list of normalizations applied. `vmr report` turns the logs into usage/latency/availability statistics with a cache-hit breakdown — and it understands agent traffic: requests group into sessions → tasks → turns (grouped in the INDEX by chat user), each detail page shows that turn's delta with a 🆕 marker, and a per-shape tool-usage report tells you which declared tools are never actually called. Each dimension has its own pre-aggregated bucket, so every table's p50/p95 is true rather than approximated. Old days auto-compress to `.zst` (20–75× smaller, `vmr report` reads it transparently) and can auto-expire via `audit_retention_days`.
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
# max_request_body_mb: 8       # inbound request body size cap (stability only; the audit trail always records requests in full, whatever size vmr accepted)
# max_concurrency: 8           # global gate; excess requests wait in memory (default: unlimited)
# https_proxy: http://127.0.0.1:7890   # upstream proxy for https base_urls — the ONLY way vmr uses a proxy
#                                      # (env vars are ignored; write ${HTTPS_PROXY} to reference one explicitly)
# http_proxy: http://127.0.0.1:7890    # same for http base_urls (e.g. a LAN llama.cpp server); unset = all direct
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
    minimax:
      base_url: https://api.minimaxi.com/v1
      api_key: ${MINIMAX_API_KEY}
      proxy: false             # always connect directly, whatever https_proxy/env says —
                               # typical when the proxy exists only for foreign providers
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

**Upstream proxy — explicit config only**: a provider's own `proxy: false` always wins (that provider connects directly — the domestic-plus-foreign provider mix this exists for); otherwise the global `http_proxy`/`https_proxy` above applies (chosen by the base_url's scheme); otherwise direct. Proxy **environment variables are deliberately ignored** — an implicit knob that silently redirects traffic is exactly the surprise a router shouldn't have; to use one, reference it explicitly (`https_proxy: ${HTTPS_PROXY}`). `proxy: true` with no matching global proxy is a config validation error, not a runtime surprise. `vmr check` and the startup summary print each provider's effective proxy (credentials masked). YAML 1.2: write `true`/`false`, not `on`/`off`.

### Environment variables

The complete list — vmr reads nothing else from the environment:

| Variable | Effect |
| --- | --- |
| Any `${VAR}` referenced in `config.yaml` | Expanded when the config is loaded (and on every hot reload). Unset variables expand to the empty string. This is the *only* way anything from the environment reaches vmr — API keys, and, if you choose, `${HTTPS_PROXY}` or a directory path. |
| `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` / `ALL_PROXY` | **Ignored.** Proxies are explicit config (`http_proxy`/`https_proxy` above); reference `${HTTPS_PROXY}` in the config to opt in. |

Directories (`log_dir`, `image_cache_dir` — see below) and proxies are config fields, not environment variables: where vmr writes and how it reaches the network shouldn't depend on implicit environment state. In service mode, `vmr.sh service install` snapshots every `${VAR}` your config references from the current shell into `~/.config/vmr/env` (0600, never overwritten) — nothing else needs injecting, since the binary reads its own config directly.

## Passthrough & normalization

**Principle: direct-connection equivalence.** What a client receives through vmr — bytes, headers, transfer pacing — matches a direct provider call. The only deviations:

- the `model` field — rewritten to the real upstream name on the way out, back to your virtual name on the way in (SDKs assume `response.model === request.model`). The outbound rewrite is a byte splice on the top-level `model` value only: every other request byte — key order, whitespace, unknown parameters — reaches the provider exactly as your client sent it;
- two **MiniMax-M3-specific repairs**, each gated on detecting its exact upstream shape: stripping inline `<think>…</think>` reasoning from content (left in place, it gets persisted into history and locks the model into a feedback loop), and stripping the plain-text "Thinking Process:" section emitted under `thinking=medium`;
- the `data: [DONE]` sentinel — appended **only** for OpenAI-protocol SSE when the upstream omitted it; never duplicated, never injected into Anthropic streams.

Streaming is real: events forward as they arrive; buffering engages only for detected thinking shapes and resumes live streaming once `</think>` closes. Compressed (`Content-Encoding`) bodies pass through untouched. Response headers follow the same policy as request headers — everything forwarded except hop-by-hop; error responses return verbatim with status, headers (`Retry-After` included), and body. Every normalization actually applied is recorded in the audit log (`attempts[].norm`), so any byte difference between upstream and client is explained, per request.

Because passthrough is byte-level, a new request/response field on either protocol needs no vmr change to reach the upstream or the client — that's the whole point. What vmr does **not** do: it only routes `POST /v1/chat/completions` and `POST /v1/messages`. Other OpenAI/Anthropic surfaces (`/v1/responses`, `/v1/realtime`, `/v1/images`, `/v1/audio`, …) aren't in scope — point a client at the provider's own base URL for those.

## Failover & health

On upstream failure vmr walks the endpoint list in order until one succeeds or all are exhausted (`max_attempts` optionally caps the walk). Health is failure-driven — no paid active probing:

- network/5xx: short cooldown with exponential backoff; 401/quota-exhausted/unknown-model: long cooldown; 429/503: `Retry-After` honored;
- expired cooldowns admit a **single probe request** (no thundering herd);
- 400-class client errors return immediately — no pointless retries;
- **content-policy blocks** fail over to the next provider but do **not** penalize the blocked endpoint — it rejected one request; it isn't down.

All-candidates-failed returns the last upstream error verbatim. Streams only fail over before the first byte is written.

## Audit log & usage reports

On by default: one JSONL line per request with both layers (client↔vmr and every vmr↔upstream attempt), credential-masked headers, applied normalizations, and inline request image metadata (format/dimensions/bytes, plus downscale/cache-hit outcome — captured for every image regardless of whether downscaling is enabled). Bodies are recorded in full, whatever size vmr accepted — there is no separate audit-side truncation cap (`max_request_body_mb` above only bounds what vmr accepts from the client in the first place). Each upstream attempt carries both a human-readable `endpoint` label (`protocol:provider:model`) and the same three fields structured (`protocol`/`provider`/`model`), plus a typed `error_class` alongside the free-text `error`.

```bash
./vmr start -c config.yaml                 # writes to config's log_dir (`vmr dirs -c config.yaml log` to check)
./vmr start -c config.yaml -audit=false    # off
jq '.model, .outcome, .attempts[0].norm' vmr-audit-2026-07-08.jsonl

./vmr report "$(./vmr dirs -c config.yaml log)/vmr-audit-*.jsonl*"   # → vmr-report.json + vmr-report.md + vmr-requests.jsonl (plain + .zst mix ok)
```

`vmr report` aggregates tokens *and* bytes (bytes as the fallback when a provider omits usage). Each record is pushed once into **every relevant bucket** (`Rows` per date×protocol×model, `Overall`, `ByModel`, `ByDate`, plus the pre-existing `Hours`/`Endpoints` and their all-dates-merged counterparts `HoursOfDay`/`EndpointsAll`) — each bucket computes its own p50/p95 from raw values it collects directly during the single pass, so every table is true rather than approximated. `HoursOfDay`/`EndpointsAll` exist as their own independently-tracked buckets rather than being derived by re-merging the per-date `Hours`/`Endpoints` rows — merging already-finalized buckets doesn't work, since each one frees its raw values right after computing its own percentiles; there'd be nothing left to recompute a cross-date percentile from. Markdown tables share a common column set (`Req/Fall/Trunc / 成功率 / Tokens In/CacheHit/Out / 图片/压缩 / 平均Tokens In/Out / 字节 In / Out / 平均消息数 / p50/p95 首字延迟 / p50/p95 请求耗时 / 平均吞吐 (tok/s)`) — request count, fallback count and truncated-stream count merge into one cell (`-` when zero), and `图片/压缩` shows inline request images seen vs. the subset that triggered downscaling (`-` when the row saw no images). Table-specific columns: `Tool 调用` (工作负载 table, with the share of requests that made at least one call), `错误分布`. Health signals per model: `finish_reason` distribution (`length` = output hit the token cap). Token totals split out cache-read, (Anthropic) cache-write, and reasoning tokens. The hourly activity profile (`hours_of_day[]`) and the workload split (`workloads[]`: interactive work vs. scheduled scaffolding like heartbeats and diary crons) show where the requests and the bill actually come from. Run progress goes to stdout as a file-level line per processed file (`[i/N] <path>  done: M records, K parse errors (Ts)`). The JSON is the data source for any dashboarding you want to build on top.

`vmr report` also understands agent workloads — offline and purely rule-based, no LLM involved (method and evidence: `docs/AgentSessionGrouping_Analysis_Fable5.md`):

- **Session → task → turn grouping.** Requests resending the same growing conversation are fingerprinted (first non-system message; Claude Code's `metadata.user_id` when present) and chained by longest-common-prefix, so concurrent agent sessions untangle even when interleaved in time. Task boundaries come from Traceparent trace-id changes and new user instructions in the delta — cross-validated signals. Compaction calls are detected and linked both ways, so a session and its post-compaction continuation form one thread.
- **`vmr-requests.jsonl`** — one feature line per request (session/task/turn, trace and chat ids, request shape, tags like `heartbeat`, per-turn tool calls, finish_reason, ok-but-truncated flag, token splits incl. reasoning tokens, delta size, latest instruction), ready for jq/DuckDB/pandas.
- **Tool usage report** — per request-shape: declared tools vs. tools actually called *this turn* (extracted from the response, so history repeats are never double-counted), plus the "declared but never called" list rendered as a numbered list grouped alphabetically (so `feishu_*` clusters naturally) — the direct input for pruning unused tools from an agent's config.

`vmr report` also exports every record as one human-readable Markdown file **plus a same-named JSON file** (the raw record, for jq/ad-hoc querying) under `{out}/details/`, for drilling into a single request: a header line locating it (trace · chat user · tools — values in **bold**), then the **full message list** with each message folded uniformly and new messages marked with a 🆕 prefix, the increment summary at the end (`🆕 本轮增量（相对上一轮,+N 条,#1–#M 为历史上下文）`), then every upstream attempt with a full side-by-side listing of headers and body fields where changes are emoji-marked (🟢 added / 🔴 removed / 🔶 changed) — including, when a `<think>…</think>` block was stripped, the full pre-strip content and its raw SSE (captured going forward only; older logs show a "not captured" note instead), and the client response with the SSE stream reassembled into the actual model output next to the raw event log. Filenames start with a zero-padded timestamp, so name order is time order. `vmr-requests-index.md` (alongside `vmr-report.md`, one level above `details/`) is organized by **Chat User** (`chat_id` from the request, with `user:` prefix stripped): each user gets a `## Chat User xxx` section, then each task's first user instruction as a quote block followed by a turn table (`轮 / 时间 / Message / finish / 耗时 / 首字延迟 / Tokens In/CacheHit/Out / 图片/压缩 / 文件` — `Message` reads `M+N` (M = history messages, N = new this turn), `finish` shows `tool_call:<name>` instead of the bare `tool_calls` reason, `耗时` carries the outcome/attempt-count information as trailing annotations instead of separate columns (`❌<outcome>` / `🚫取消` / `⚠️截断` / `🔄尝试x{n}`, any combination), and the file column is two short `md`/`json` links). The flat "全部请求（时间序）" table merges model + upstream into one `VM/API` column (`protocol | virtual_model | provider:model`, e.g. `openai | agent | minimax:MiniMax-M3` — `:` separates provider from upstream model since some providers, like OpenRouter, put a `/` inside the model name itself). Compaction calls, scheduled scaffolding (heartbeat/dream_diary), and non-chat/rejected requests always render under `## Chat User (unresolved)`, folded into compact sub-sections (`### 压缩任务 · compaction 会话 × N`, `### 定时任务 · <class> 单发会话 × N`, `### 其他 · 非聊天体/被拒请求 × N`) instead of one section per firing or a separate top-level heading. Disable detail export with `-details=false`.

Agent workloads resend the full conversation on every turn, so a day's log can run into gigabytes — mostly repeated across lines, not within one. Each day's file rotates and compresses automatically once it's no longer "today": zstd on the whole file (not per-line) catches that cross-line repetition, typically 20–75× smaller in practice — far beyond what compressing each record on its own could reach, since a single record never sees the previous turn's near-duplicate body. `vmr report` reads `.jsonl` and `.jsonl.zst` interchangeably, so point it at a glob covering both. Set `audit_retention_days` to also delete files past a given age (default: keep forever — nothing is deleted unless you opt in); either way, deletion and compression are both keyed off the date in the filename, so housekeeping never needs to scan or `stat` the whole log directory. Details and the numbers behind this: `docs/AuditLogCompression_Analysis_Sonnet5.md`.

## Request image downscaling

Optional, off by default. When enabled, inline base64 image attachments exceeding the configured long-side limit are proportionally resized and re-encoded as JPEG before hitting the upstream — cutting vision-token cost for screenshot-heavy agent workflows. Requests only, never responses, never remote URLs; animated images and anything that fails to decode pass through untouched (fail-open).

Detection is always on, independent of this setting: every inline image in a request — whether or not downscaling is enabled for that virtual model — gets a cheap header-only read (format/dimensions/bytes, no pixel decode) recorded in the audit trail, so `vmr report`'s `图片/压缩` column reflects real image traffic even on models with downscaling off.

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

**Downscale result cache**: the first time a given source image is downscaled to a given target size, the result (JPEG bytes) is cached on disk keyed by **content hash plus target size** — the filename is `<sha256-of-original-bytes>-<maxPx>.jpg`, so the same image downscaled to 512px and 256px (different per-model overrides) are two independent entries that can never collide — under the configured `image_cache_dir` (see below). A later request for the same image reuses the cached bytes verbatim instead of decoding/scaling/re-encoding. Two reasons this matters: it saves CPU (agent workflows resend the full conversation, images included, on every turn), and it protects the upstream's own prompt cache — which is keyed on exact byte/token match, so re-encoding the same image on every request can produce subtly different output bytes and silently defeat that cache, while identical cached bytes always hit it. Entries are evicted by last-hit time (`image_cache_ttl_days`, default 7 days; a hit refreshes the clock, so an image reused throughout a long conversation is never evicted mid-session), swept lazily off normal cache access rather than a dedicated timer.

**Where the audit and cache directories actually land**: both are config fields —

```yaml
# log_dir: ~/.vmr/logs                  # audit JSONL directory; used exactly as given (~/ expands); changing it needs a restart
# image_cache_dir: ~/.vmr/image_cache   # downscale-cache directory; same rule; follows hot reload
```

— used exactly as given when set (a leading `~/` expands to the home directory), else the persistent `~/.vmr/logs`/`~/.vmr/image_cache`, else (no resolvable home directory) a `vmr_logs`/`vmr_image_cache` subdirectory of the system temp dir, else `./logs`/`./image_cache` next to the binary. Persistent by default on purpose: macOS purges temp-dir entries not accessed for ~3 days, which would silently delete audit data — the only data source `vmr report` has. Run `vmr dirs -c config.yaml log` / `vmr dirs -c config.yaml cache` to see the resolved path without starting the server (also printed by `vmr check` and the startup summary). `vmr.sh` queries `vmr dirs` itself for the server-log path rather than guessing, so a launchd/systemd-supervised vmr never disagrees with a manually-started one about where its data lives. Neither directory has an environment-variable override — reference `${VAR}` inside `log_dir`/`image_cache_dir` if you want a value from the environment.

## Endpoints & CLI

| Endpoint / command | Purpose |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI-protocol ingress (streaming + non-streaming) |
| `POST /v1/messages` | Anthropic-protocol ingress (streaming + non-streaming) |
| `GET /v1/models` | virtual model list (parseable by both SDK families) |
| `GET /admin/status` | endpoint health + concurrency metrics (loopback only) |
| `vmr check -c config.yaml` | validate config, print routing table, key status and per-provider effective proxy |
| `vmr status -c config.yaml` | render a running instance's health and concurrency |
| `vmr report [-o dir] <glob>` | audit logs (plain or `.zst`) → usage statistics + session/tool analysis + per-request features (`vmr-requests.jsonl`) + detail files (`-details=false` to skip) |
| `vmr dirs [-c config.yaml] log\|cache` | print the effective audit/cache directory (`log_dir`/`image_cache_dir` after defaults) — what `vmr.sh` queries internally |
| `vmr diagnose [-c config.yaml]` | beyond `check`'s static preview: DNS/TLS/proxy reachability per provider, then a real minimal request per configured endpoint (run concurrently, `-test-timeout` per check, default 10s), plus a routing-order preview annotated with what it found (`-no-test-routing` to skip the live requests, `-json` for scripting; exits non-zero if anything failed) |
| `vmr replay -provider NAME <audit.jsonl>` | rebuild and resend one request from an audit record through the exact same request-building path vmr itself uses — `-dry-run` to print without sending, `-record path` to save the replay as its own audit line, `-model`/`-protocol` to override what the record itself says, `-stream true\|false` to force streaming on/off, `-max-time` to cap the upstream wait. Pick the record with `-detail file` (a `vmr report` `details/*.json` file — no line-counting needed), `-ts <timestamp>` (matches either `vmr-requests.jsonl`'s or the raw audit log's `ts` field), or `-line N` (default: the last one in the file) — the three are mutually exclusive |
| `./vmr.sh start\|stop\|…` | dev-mode lifecycle (you supervise) |
| `./vmr.sh service install\|uninstall\|start\|…` | init-system service (launchd/systemd: crash restart, start at login) |

Routed responses carry `X-VMR-Endpoint` (the endpoint that served it) and `X-VMR-Attempts` (tries used).

```bash
# Something's misconfigured — find out what before staring at 401s in the logs.
./vmr diagnose -c config.yaml

# A request failed; see exactly what vmr would send without sending it.
./vmr replay -c config.yaml -provider openrouter -dry-run \
    "$(./vmr dirs -c config.yaml log)/vmr-audit-2026-07-13.jsonl"

# Same request, actually sent, response printed to stdout.
./vmr replay -c config.yaml -provider openrouter \
    "$(./vmr dirs -c config.yaml log)/vmr-audit-2026-07-13.jsonl"

# Found the failing request in vmr-requests-index.md or vmr-report.md instead?
# Point -detail straight at its details/*.json — no line-counting needed.
./vmr replay -c config.yaml -provider openrouter -dry-run \
    -detail out/details/20260713-153042.100_coding_z-ai-glm-5.2_error.json

# Or replay by the exact timestamp shown in vmr-requests.jsonl / vmr-report.md.
./vmr replay -c config.yaml -provider openrouter -dry-run \
    -ts 2026-07-13T15:30:42.100+08:00 \
    "$(./vmr dirs -c config.yaml log)/vmr-audit-2026-07-13.jsonl"
```

## Development

```bash
go test -race ./...
```

Adding a provider: OpenAI/Anthropic-compatible vendors are one config entry, zero code. A new protocol = one package under `internal/adapter/<name>/` implementing the three-method interface + one blank import in `cmd/vmr/main.go`.

Architecture and every design decision (with the war stories behind them): [design doc](docs/VirtualModelRouter_v2_Fable5.md) (Chinese).

## License

[MIT](LICENSE)
