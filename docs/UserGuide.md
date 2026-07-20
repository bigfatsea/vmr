<!-- Ver 2026-07-20 01:00, by Sonnet 5 -->

# vmr — User Guide

English | [简体中文](UserGuide.zh.md)

Full configuration reference, protocol behavior, and CLI details. If you just want to get running, see the [README](../README.md) Quick Start first — come back here for everything past that.

## Configuration

`providers` and `models` are both nested by protocol: the outer key is the protocol (`openai` / `anthropic`), the inner key the name. A model's endpoints can only reference providers in the same protocol group — mixing protocols in one model has no syntax to write, rather than being a config error to catch. The same short name (`openrouter`) can appear once per protocol group for the two faces of one account:

```yaml
listen: 127.0.0.1:8800
# api_keys:                    # optional: protect vmr itself (Bearer or x-api-key accepted); each
#   - ${VMR_KEY_ALICE}          # entry is tagged in `vmr report` output by its own tail (see
#   - ${VMR_KEY_OPENCLAW}       # "Multiple callers, one instance" below). The old singular api_key
#                               # was removed — configs still using it are rejected with a migration hint
# max_attempts: 0              # cap on upstream tries per request (default 0 = walk every candidate)
# probe_mode: active            # active (default) | passive — how a cooled-down endpoint gets re-verified before real traffic returns to it, see Failover & health below
# probe_timeout: 15s            # active mode only: upper bound on one background recovery probe
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

All fields and validation rules: design doc §10. Config edits hot-reload within seconds; a broken config is rejected and the running instance keeps its current one. Parsing is strict: an unknown or misspelled key (`max_concurency: 8`) is a load error, never a silently ignored no-op you believe is in effect.

**Upstream proxy — explicit config only**: a provider's own `proxy: false` always wins (that provider connects directly — the domestic-plus-foreign provider mix this exists for); otherwise the global `http_proxy`/`https_proxy` above applies (chosen by the base_url's scheme); otherwise direct. Proxy **environment variables are deliberately ignored** — an implicit knob that silently redirects traffic is exactly the surprise a router shouldn't have; to use one, reference it explicitly (`https_proxy: ${HTTPS_PROXY}`). `proxy: true` with no matching global proxy is a config validation error, not a runtime surprise. `vmr check` and the startup summary print each provider's effective proxy (credentials masked). YAML 1.2: write `true`/`false`, not `on`/`off`.

**base_url path overlap elimination**: vmr pre-computes each provider's complete upstream URL at initialization by joining the `base_url` with the protocol's path suffix (`/v1/chat/completions` for OpenAI, `/v1/messages` for Anthropic), detecting and removing any overlap between the tail of `base_url` and the head of the suffix. So `https://api.example.com/v1` + `/v1/chat/completions` → `https://api.example.com/v1/chat/completions` (not `…/v1/v1/…`). Write `base_url` with or without `/v1`, or even the full path — the result is always correct. This covers both OpenAI-compatible and Anthropic-compatible endpoints, since both use the `/v1` convention. The URL is computed once at config load and stored on the endpoint; the adapter uses it directly, never constructing or normalizing a URL per request.

**`role_map` — per-provider role remapping**: some OpenAI-compatible providers reject roles their upstream doesn't recognize — the canonical case is the `developer` role OpenAI introduced for o1/o3-series models, which some gateways (e.g. DashScope/Qianwen) reject outright. `role_map: {developer: system}` under a provider rewrites matching `"role"` values inside the top-level `messages` array before the request leaves vmr, with no client-side change needed. It's a plain old→new string map, applied only to the exact roles listed — every other byte of the request (key order, whitespace, unknown fields, message content) passes through untouched, the same byte-splice approach `RewriteModel` uses for the model field. Scoped to the provider (like `base_url`/`api_key`), not the virtual model, since rejecting a role is normally a property of the upstream gateway itself, not of any one model behind it; a model that never sends the mapped role is unaffected either way. Omit `role_map` (or leave it empty) for providers that accept every role as-is — the default.

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
- two **MiniMax-M3-specific repairs**, each gated on detecting its exact upstream shape: stripping inline `<think>…</think>` reasoning from content (left in place, it gets persisted into history and locks the model into a feedback loop), and stripping the plain-text "Thinking Process:" section emitted under a MiniMax thinking mode that doesn't use `<think>` tags at all;
- the `data: [DONE]` sentinel — appended **only** for OpenAI-protocol SSE when the upstream omitted it; never duplicated, never injected into Anthropic streams.

Streaming is real: events forward as they arrive; buffering engages only for detected thinking shapes and resumes live streaming once `</think>` closes. Compressed (`Content-Encoding`) bodies pass through untouched. Upstream 3xx redirects are never followed — a 301/302/303 reaches the client with its original status, `Location` header, and body, exactly as a direct call would show (the default `http.Client` policy silently rewrites POST to GET on 301/302/303, which would violate byte-faithful passthrough). Response headers follow the same policy as request headers — everything forwarded except hop-by-hop; error responses return verbatim with status, headers (`Retry-After` included), and body. Every normalization actually applied is recorded in the audit log (`attempts[].norm`), so any byte difference between upstream and client is explained, per request.

Because passthrough is byte-level, a new request/response field on either protocol needs no vmr change to reach the upstream or the client — that's the whole point. What vmr does **not** do: it only routes `POST /v1/chat/completions` and `POST /v1/messages`. Other OpenAI/Anthropic surfaces (`/v1/responses`, `/v1/realtime`, `/v1/images`, `/v1/audio`, …) aren't in scope — point a client at the provider's own base URL for those.

## Failover & health

On upstream failure vmr walks the endpoint list in order until one succeeds or all are exhausted (`max_attempts` optionally caps the walk). Health is failure-driven, classified per response so a failure gets the right penalty and the right verdict on whether to keep failing over:

- network/5xx: short cooldown with exponential backoff; 401/quota-exhausted/unknown-model/a relay or gateway reporting its own forwarding failure (as opposed to something wrong with the request itself): long cooldown; 429/503: `Retry-After` honored;
- 400-class **client** errors — a genuinely bad request — return immediately, no failover, no cooldown: every endpoint would reject the same request the same way, so there's nothing switching providers would fix;
- **content-policy blocks** fail over to the next provider but do **not** penalize the blocked endpoint — it rejected one request; it isn't down.

**Recovering a cooled-down endpoint** (`probe_mode`, default `active`):

- `active` (default): once an endpoint's cooldown expires, vmr fires one small dedicated probe request in the background (bounded by `probe_timeout`, default 15s) instead of letting the next real request find out the hard way. Real traffic never touches — and never waits behind — an endpoint that hasn't been confirmed recovered yet; it's simply routed to the next candidate until the probe reports back, however long that takes. The probe asks the model to echo back a one-time token, so a relay/gateway answering with a cached or canned "success" doesn't count as recovered.
- `passive`: the classic behavior — the next real request past the cooldown *is* the probe (single-flight, so a thundering herd can't pile onto a just-recovering endpoint). Its own size and duration decide how long that recovery check takes; under concurrent load, every other request targeting the same endpoint is diverted to the next candidate for as long as it runs. Switch to this if you'd rather not spend the extra probe request, or you never run vmr under heavy concurrent load in the first place.

```yaml
probe_mode: active      # active (default) | passive
probe_timeout: 15s      # active mode only: upper bound on one background probe
```

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

`vmr report` also understands agent workloads — offline and purely rule-based, no LLM involved (method and evidence: design doc §9.4, "Agent 会话分析"):

- **Session → task → turn grouping.** Requests resending the same growing conversation are fingerprinted (first non-system message; Claude Code's `metadata.user_id` when present) and chained by longest-common-prefix, so concurrent agent sessions untangle even when interleaved in time. Task boundaries come from Traceparent trace-id changes and new user instructions in the delta — cross-validated signals. Compaction calls are detected and linked both ways, so a session and its post-compaction continuation form one thread.
- **`vmr-requests.jsonl`** — one feature line per request (session/task/turn, trace and chat ids, request shape, tags like `heartbeat`, per-turn tool calls, finish_reason, ok-but-truncated flag, token splits incl. reasoning tokens, delta size, latest instruction), ready for jq/DuckDB/pandas.
- **Tool usage report** — per request-shape: declared tools vs. tools actually called *this turn* (extracted from the response, so history repeats are never double-counted), plus the "declared but never called" list rendered as a numbered list grouped alphabetically (so `feishu_*` clusters naturally), together with the complete tool declaration's per-request byte cost — the direct input for pruning unused tools from an agent's config.

`vmr report` also exports every record as one human-readable Markdown file **plus a same-named JSON file** (the raw record, for jq/ad-hoc querying) under `{out}/details/`, for drilling into a single request: a header line locating it (trace · chat user · tools — values in **bold**), then the **full message list** with each message folded uniformly and new messages marked with a 🆕 prefix, the increment summary at the end (`🆕 本轮增量（相对上一轮,+N 条,#1–#M 为历史上下文）`), then every upstream attempt with a full side-by-side listing of headers and body fields where changes are emoji-marked (🟢 added / 🔴 removed / 🔶 changed) — including, when a `<think>…</think>` block was stripped, the full pre-strip content and its raw SSE (captured going forward only; older logs show a "not captured" note instead), and the client response with the SSE stream reassembled into the actual model output next to the raw event log. Filenames start with a zero-padded timestamp, so name order is time order. `vmr-requests-index.md` (alongside `vmr-report.md`, one level above `details/`) is organized by **Chat User** (`chat_id` from the request, with `user:` prefix stripped): each user gets a `## Chat User xxx` section, then each task's first user instruction as a quote block followed by a turn table (`轮 / 时间 / Message / finish / 耗时 / 首字延迟 / Tokens In/CacheHit/Out / 图片/压缩 / 文件` — `Message` reads `M+N` (M = history messages, N = new this turn), `finish` shows `tool_call:<name>` instead of the bare `tool_calls` reason, `耗时` carries the outcome/attempt-count information as trailing annotations instead of separate columns (`❌<outcome>` / `🚫取消` / `⚠️截断` / `🔄尝试x{n}`, any combination), and the file column is two short `md`/`json` links). The flat "全部请求（时间序）" table merges model + upstream into one `VM/API` column (`protocol | virtual_model | provider:model`, e.g. `openai | agent | minimax:MiniMax-M3` — `:` separates provider from upstream model since some providers, like OpenRouter, put a `/` inside the model name itself). Compaction calls, scheduled scaffolding (heartbeat/dream_diary), and non-chat/rejected requests always render under `## Chat User (unresolved)`, folded into compact sub-sections (`### 压缩任务 · compaction 会话 × N`, `### 定时任务 · <class> 单发会话 × N`, `### 其他 · 非聊天体/被拒请求 × N`) instead of one section per firing or a separate top-level heading. Disable detail export with `-details=false`.

**Multiple callers, one instance.** If several clients share one vmr (a teammate, a second agent, a CI job) and you want their usage told apart after the fact, give each one its own entry under `api_keys` (see Configuration above) instead of sharing one key. Every request tags its audit record with that key's own tail (`client_key_tag`, via `KeyTag`: last 8 characters, then — if that window contains a `-` — only what follows the last `-` inside it, so a key ending in `...-alice` reads back as `alice`; keep the meaningful part ≥3-4 characters to avoid two callers' tags colliding). `vmr report` picks this up automatically, no flag needed: for every distinct tag it observed, it writes one extra `vmr-requests-<tag>.jsonl` and `vmr-requests-index-<tag>.md` next to the usual ones — same directory, so the tag file's `details/…` links need no adjusting. `vmr-report.md`/`.json` and `details/` themselves are never split or duplicated: a request's detail pair is written once regardless of caller, and the aggregate report always covers everyone. Nothing changes at all — no extra files, no new columns — until `api_keys` is actually configured. Full design rationale: design doc §9.4, "按调用方（`client_key_tag`）分组导出".

Don't want real auth at all (a trusted private network)? Leave `api_keys` unset — the door stays fully open — but a client that voluntarily sends *any* Authorization/x-api-key value still gets it tagged the same way, no vmr-side config needed: just end each client's own value in `-<label>` and it self-identifies to `vmr report`. No 16-character minimum applies in this mode (there's no secret to protect); a client sending nothing still gets an untagged record.

Agent workloads resend the full conversation on every turn, so a day's log can run into gigabytes — mostly repeated across lines, not within one. Each day's file rotates and compresses automatically once it's no longer "today": zstd on the whole file (not per-line) catches that cross-line repetition, typically 20–75× smaller in practice — far beyond what compressing each record on its own could reach, since a single record never sees the previous turn's near-duplicate body. `vmr report` reads `.jsonl` and `.jsonl.zst` interchangeably, so point it at a glob covering both. Set `audit_retention_days` to also delete files past a given age (default: keep forever — nothing is deleted unless you opt in); either way, deletion and compression are both keyed off the date in the filename, so housekeeping never needs to scan or `stat` the whole log directory. Details and the numbers behind this: design doc §9.5.

## Request image downscaling

Optional, off by default. When enabled, inline base64 image attachments exceeding the configured long-side limit are proportionally resized and re-encoded as JPEG before hitting the upstream — cutting vision-token cost for screenshot-heavy agent workflows. Requests only, never responses, never remote URLs; GIFs (animated or not) and anything that fails to decode pass through untouched (fail-open) — see design doc §7 for why GIF is never rescaled, not even single-frame stills.

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
| `GET /admin/status` | endpoint health + concurrency metrics, including whether a recovery probe (passive or active) currently has an endpoint's single-flight slot (loopback only) |
| `vmr check -c config.yaml` | validate config, print routing table, key status and per-provider effective proxy |
| `vmr status -c config.yaml` | render a running instance's health and concurrency |
| `vmr report [-o dir] <glob>` | audit logs (plain or `.zst`) → usage statistics + session/tool analysis + per-request features (`vmr-requests.jsonl`) + detail files (`-details=false` to skip) |
| `vmr dirs [-c config.yaml] log\|cache` | print the effective audit/cache directory (`log_dir`/`image_cache_dir` after defaults) — what `vmr.sh` queries internally |
| `vmr diagnose [-c config.yaml]` | beyond `check`'s static preview: DNS/TLS/proxy reachability per provider, then a real minimal request per configured endpoint asking for a one-time token echoed back (run concurrently, `-test-timeout` per check, default 10s) — a 200 that doesn't echo it warns instead of passing, catching a relay/gateway that answers with a cached or canned response instead of a fresh completion — plus a routing-order preview annotated with what it found (`-no-test-routing` to skip the live requests, `-json` for scripting; exits non-zero if anything failed) |
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
