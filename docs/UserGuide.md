<!-- Ver 2026-07-30 23:45, by Sonnet 5 -->

# vmr — User Guide

English | [简体中文](UserGuide.zh.md)

Full configuration reference, protocol behavior, and CLI details. If you just want to get running, see the [README](../README.md) Quick Start first — come back here for everything past that.

## Configuration

`providers` is a flat list — one entry per upstream account, however many of the two ingress protocols (`openai` / `anthropic`) it actually speaks. `base_url` is itself keyed by protocol, so one entry covers both faces of an account instead of declaring it twice. `models` is keyed by virtual-model name; each entry under `endpoints` carries its own `protocol` field, so one virtual model can mix an openai-protocol candidate list and an anthropic-protocol one under the same name — each independently reachable only from its own ingress. An endpoint-group's `models:` list can name more than one upstream model, each expanding into its own independently health-tracked candidate sharing the rest of that entry's fields:

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
# https_proxy: http://127.0.0.1:7890   # proxy server URL for https base_urls — the ONLY way vmr uses a proxy
#                                      # (env vars are ignored; write ${HTTPS_PROXY} to reference one explicitly).
#                                      # Declaring this URL does NOT turn proxying on by itself — see `proxy` below
# http_proxy: http://127.0.0.1:7890    # same for http base_urls (e.g. a LAN llama.cpp server)
# proxy: false                  # global default for providers with no proxy switch of their own (default false).
#                                # Recommended: leave this off and opt individual providers in with their own
#                                # proxy: true (see below) — explicit per-provider intent beats a global switch
#                                # silently deciding for providers added later
# image_downscale: 512         # long-side px cap for inline request images (default: off; a model's own setting overrides this, see below)
# image_cache_ttl_days: 7      # eviction age for cached downscale results (default: 7 days)
# audit_retention_days: 30     # delete audit files older than this (default: keep forever)
# timeouts:
#   connect: 10s               # upstream dial
#   response_header: 120s      # upstream time-to-first-byte
#   stream_idle: 120s          # abort any upstream body (stream, JSON, error) silent for this long

providers:
  - name: openrouter
    base_url: {openai: https://openrouter.ai/api/v1, anthropic: https://openrouter.ai/api/v1}
    api_key: ${OPENROUTER_API_KEY}
    proxy: true              # always go through https_proxy/http_proxy above, whatever the
                             # global proxy default says — the recommended way to opt a
                             # foreign provider in
  - name: minimax
    base_url: {openai: https://api.minimaxi.com/v1}
    api_key: ${MINIMAX_API_KEY}
    # proxy: false           # not needed here — the recommended baseline (global proxy
                             # left off) is already direct-by-default for this provider

models:
  coding:                      # openai protocol only → served via /v1/chat/completions
    endpoints:
      - {protocol: openai, provider: openrouter, models: [z-ai/glm-5.2]}   # no priority field: list order = try order
  claude:                      # anthropic protocol only → served via /v1/messages
    endpoints:
      - {protocol: anthropic, provider: openrouter, models: [minimax/minimax-m3]}
```

All fields and validation rules: Part 1 §10 of the design doc. Config edits hot-reload within seconds; a broken config is rejected and the running instance keeps its current one. Parsing is strict: an unknown or misspelled key (`max_concurency: 8`) is a load error, never a silently ignored no-op you believe is in effect.

**Upstream proxy — explicit config only, default off**: `http_proxy`/`https_proxy` above only declare *where* the proxy lives — they don't turn it on for anyone by themselves. Whether a provider actually uses it is decided by a three-way resolution: a provider's own `proxy: true`/`false` always wins; absent that, it follows the global `proxy` switch (also `false` by default); if that resolves to "on", the base_url's scheme picks `https_proxy` or `http_proxy`. **Recommended shape**: leave the global `proxy` off and opt individual providers in with their own `proxy: true` — explicit per-provider intent, so a provider added later doesn't silently inherit a stale global default. (Flip the global `proxy` to `true` only if you want "proxied" to be the default and carve out exceptions with a provider's own `proxy: false` instead.) Proxy **environment variables are deliberately ignored** — an implicit knob that silently redirects traffic is exactly the surprise a router shouldn't have; to use one, reference it explicitly (`https_proxy: ${HTTPS_PROXY}`). `proxy: true` (global or per-provider) with no matching proxy URL configured is a config validation error, not a runtime surprise. `vmr check` and the startup summary print each provider's effective proxy (credentials masked). YAML 1.2: write `true`/`false`, not `on`/`off`.

**base_url must include the version**: vmr pre-computes each provider's complete upstream URL at initialization by appending the protocol's bare path (`/chat/completions` for OpenAI, `/messages` for Anthropic) directly to `base_url` — no normalization, no overlap detection. `base_url` must therefore already carry the provider's own full API version, whatever that provider calls it: `https://api.example.com/v1`, `https://api.minimaxi.com/anthropic/v1`, `https://ark.example.com/api/coding/v3`. This matters because not every provider versions its OpenAI/Anthropic-compatible surface as `v1` — Volcengine's coding-plan OpenAI endpoint is `v3`, for instance — so vmr never assumes a version on your behalf; get it wrong and the 404 shows up immediately against the exact base_url you wrote. The URL is computed once at config load and stored on the endpoint; the adapter uses it directly, never constructing or normalizing a URL per request.

**`role_map` — per-endpoint-group role remapping**: some OpenAI-compatible providers reject roles their upstream doesn't recognize — the canonical case is the `developer` role OpenAI introduced for o1/o3-series models, which some gateways (e.g. DashScope/Qianwen) reject outright. `role_map: {developer: system}` under a `models.<name>.endpoints[]` entry rewrites matching `"role"` values inside the top-level `messages` array before the request leaves vmr, with no client-side change needed. It's a plain old→new string map, applied only to the exact roles listed — every other byte of the request (key order, whitespace, unknown fields, message content) passes through untouched, the same byte-splice approach `RewriteModel` uses for the model field. Scoped to the endpoint-group, not the provider or the virtual model as a whole, since the same account can back several endpoint-groups (different virtual models, different upstream model families) that don't all necessarily need the same rewrite; a model that never sends the mapped role is unaffected either way. Omit `role_map` (or leave it empty) for an entry whose upstream accepts every role as-is — the default.

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
- two **MiniMax-M3-specific repairs**, each gated on detecting its exact upstream shape: stripping inline `<think>…</think>` reasoning from content (left in place, it gets persisted into history and locks the model into a feedback loop), and stripping the plain-text "Thinking Process:" section emitted under a MiniMax thinking mode that doesn't use `<think>` tags at all. Both repairs are triggered by a literal wording guard, so a response that still looks like the leaked-thinking shape (numbered subsections, long) but didn't match the guard gets tagged `thinking_process_pattern_detected` in `attempts[].norm` instead — bytes untouched, a pure signal for spotting a wording drift before it silently stops being stripped;
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

## Condition-based routing

Endpoints behind one virtual model don't have to be interchangeable. Declare what each one actually supports, and a request that needs something a given endpoint doesn't declare skips it — rather than being tried against an endpoint that's certain to reject it:

```yaml
models:
  agent:
    capabilities: [text, tools]        # base: every endpoint below inherits this
    max_context_tokens: 128000         # base: ditto
    endpoints:
      - protocol: openai
        provider: minimax
        models: [MiniMax-M3]
        capabilities: [image]          # ADDED to the base -> effective: text, tools, image
        max_context_tokens: 1000000    # OVERRIDES the base for this endpoint alone
      - protocol: openai
        provider: deepseek
        models: [deepseek-chat]        # declares neither -> inherits the base as-is
```

Both fields are optional at both levels and default to **unconstrained**: a virtual model with no `capabilities` has no base, and an endpoint that doesn't add its own is assumed to support everything the model does (or, absent any declaration anywhere, everything at all) — existing configs behave exactly as before. `capabilities` is *additive* per endpoint (union with the model's base) since `max_context_tokens` is *override-or-inherit* instead (a single number can't be unioned). Once an endpoint's effective capability set is non-empty it's exhaustive (list everything it actually supports, not just what you want checked); `vmr check` prints each virtual model's base and each endpoint's own declared extras/override so a gap is visible before it causes a misroute.

Two different kinds of condition:

- **`image` / `tools`** — hard requirements. A request needing one and finding no eligible candidate fails fast with a `vmr_no_candidates` error naming the missing capability, instead of wasting an attempt on an endpoint guaranteed to reject it. `image` detection is structural (does the request actually contain an `image_url`/`source` content block?), not a text-content guess — a request whose text merely happens to mention the word "image" is never misdetected, and one whose image is in a format vmr's own decoder doesn't recognize still counts as an image (detected ≠ decodable). (`thinking`/`audio`/`video` aren't checked yet — request-side detection for those isn't confirmed across providers, so declaring them today has no effect.)
- **context length** — a coarse, deliberately conservative *estimate*, not a certainty: request bytes classified ASCII (~4 bytes/token) vs. multi-byte UTF-8/CJK (~2 bytes/token, intentionally pessimistic), a flat ~3000 tokens per detected inline image, and detected document/PDF attachments sized by their base64 payload length ÷ 20 — no parsing beyond cheap structural markers. Because it's only an estimate, it can never by itself refuse a request: if every endpoint's declared `max_context_tokens` looks too small, vmr falls back to trying the capability-eligible candidates anyway rather than returning an error on a guess — an overestimate costs at most a wasted attempt, never a request that would have worked.

Full design and the token-estimate calibration: `docs/VirtualModelRouter_Design_v4_Core.md`, "Condition-based Routing" section.

## Sticky Model (session affinity)

Upstream prompt caches are keyed on an exact byte prefix. If a multi-turn agent conversation gets routed to a different endpoint mid-conversation, the provider's cache goes cold and a "better" routing choice can end up costing more, not less — condition-based routing above is one thing that can trigger this (e.g. a context estimate that shrinks below another endpoint's declared ceiling after the agent compacts its history). Sticky Model keeps a conversation on whichever endpoint most recently, successfully served it:

```yaml
sticky_ttl: 10m              # global default: how long a sticky preference stays valid

models:
  agent:
    # sticky: true is the default — omit it. Only a genuinely one-shot
    # virtual model (no multi-turn value to protect) needs sticky: false.
    endpoints:
      - protocol: openai
        provider: minimax
        models: [MiniMax-M3]
        # inherits the global 10-minute sticky_ttl
      - protocol: openai
        provider: deepseek
        models: [deepseek-chat]
        sticky_ttl: 2h      # DeepSeek's disk-based cache lasts hours to days — override per endpoint
```

- **Identity**: a conversation is fingerprinted from its system prompt *and* first non-system message — both hashed, never logged or otherwise exposed. Two different agents that happen to open with the same line don't collide, because their system prompts (and therefore their actual upstream cache prefixes) differ; hashing only the first user message, without the system prompt, would have missed exactly that case.
- **`sticky_ttl` is per-endpoint, not per-model** — cache lifetime is a property of the upstream provider (Anthropic/OpenAI/MiniMax: roughly 5–10 minutes; DeepSeek: hours to days), so endpoints behind the same virtual model can each declare their own window instead of forcing one value on all of them. The global `sticky_ttl` (default 10 minutes) is the fallback for endpoints that don't override it.
- **`sticky_ttl` (global or per-endpoint) can't exceed 24 hours** — the sticky registry itself drops an idle entry from memory after 24 hours regardless of what any endpoint's TTL says, so a longer setting would load but silently stop taking effect. `vmr check`/`vmr start`/hot reload all reject a config that tries it, with an error naming the offending model/endpoint.
- Affinity only ever reorders within the endpoints that already passed health and condition filtering — an endpoint that's since become unhealthy or lost a required capability is never resurrected just because it was the sticky pick last time.
- The pointer moves on every successful completion, including a failover success, so it always follows wherever the conversation's cache is actually warm — a stale pointer self-corrects on the next successful turn, no separate invalidation logic needed.

Full design (identity choice, TTL research behind the defaults, why this fingerprint is a separate implementation from `vmr report`'s offline session grouping below): `docs/VirtualModelRouter_Design_v4_Core.md`, "Sticky Model" section.

## Audit log & usage reports

On by default: one JSONL line per request with both layers (client↔vmr and every vmr↔upstream attempt), credential-masked headers, applied normalizations, and inline request image metadata (format/dimensions/bytes, plus downscale/cache-hit outcome — captured for every image regardless of whether downscaling is enabled). Bodies are recorded in full, whatever size vmr accepted — there is no separate audit-side truncation cap (`max_request_body_mb` above only bounds what vmr accepts from the client in the first place). Each upstream attempt carries both a human-readable `endpoint` label (`protocol:provider:model`) and the same three fields structured (`protocol`/`provider`/`model`), plus a typed `error_class` alongside the free-text `error`.

Each record also carries a `facts` object — vmr's own pre-routing read on the request (`has_image`/`has_tools`/`estimated_tokens`), the exact same values the router used to pick an endpoint, recorded as-is rather than recomputed from the stored body. It's a sibling of the request, not part of it, so the recorded request itself stays byte-faithful to what the client actually sent. Absent entirely (not a zero-valued object) on a request rejected before routing ever ran — bad auth, unparseable JSON.

```bash
./vmr start -c config.yaml                 # writes to config's log_dir (`vmr check -c config.yaml log` to check)
./vmr start -c config.yaml -audit=false    # off
jq '.model, .outcome, .attempts[0].norm' vmr-audit-2026-07-08.jsonl

./vmr report "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"   # → vmr-report.json + vmr-report.md + vmr-requests.jsonl (plain + .zst mix ok)
```

`vmr report` aggregates tokens *and* bytes (bytes as the fallback when a provider omits usage), and organizes the Markdown around nine numbered sections, each answering one operator question: `§0` summary (headline numbers + up to 3 auto-generated highlights — low cache efficiency, wasted tool schemas, a misbehaving endpoint), `§1` token economics (cache-hit/fresh/cache-write/reasoning breakdown, per-model cache efficiency, per-role message-character/estimated-token share), `§2` cost estimate (only rendered once a pricing sidecar is loaded — see "Cost estimate & pricing.yaml" below — one table each by model/endpoint/client, plus the exact pricing config used embedded verbatim in a collapsed block so a report's $ figures stay traceable even after `pricing.yaml` later changes), `§3` reliability (endpoint availability/error-rate and error-class breakdown, each split into an openai/anthropic table since the two protocol surfaces route independently, plus an hourly error-count chart), `§4` latency & throughput (ttft/duration percentiles by model and by endpoint, both sorted by throughput descending, each with its sample size and a `⚠️low-n` flag under n=20), `§5` workload split (by virtual model, by workload class — interactive vs. scheduled scaffolding —, by endpoint, and by client — the latter two also carrying per-request input/output token percentiles — plus hourly and daily request-volume/input-token Mermaid charts), `§6` sessions & tasks (interactive sessions only, grouped by Chat User — single-shot scheduled sessions live in the request index instead, see below), `§6.5` sticky effectiveness (below), `§6.6` endpoint value (below), `§6.7` compaction (every standalone history-compaction LLM call this period: which sessions it links, tokens in→out, retention ratio, and a rule-based sample of what got swallowed — no LLM, just the observable facts), `§7` efficiency & waste (auto findings, plus the full used/never-called tool breakdown per declared-tool shape), `§8` a pointer to the request index. Every table stays to a handful of columns; percentiles are true per-bucket values — each bucket keeps its own raw samples and computes p50/p95 directly during the single pass, never derived by re-merging other buckets' already-finalized percentiles (there'd be nothing left to recompute from). An `⭐` marks any column that's a derived/estimated metric rather than a raw upstream value. Hourly/daily activity and the hourly error count render as Mermaid `xychart-beta` charts.

Run progress goes to stdout with a `yyyy-MM-dd HH:mm:ss.SSS` timestamp on every line, so each phase's real cost is visible: session analysis runs first (parallelized across input files — the single largest phase on a busy multi-day corpus — and silent, no per-file lines of its own), then one combined pass does aggregation and detail export together, printing one `[i/N] <path>  done: M records (Ts)` line per file — detail rendering runs on its own worker pool concurrently with the file scan that feeds it, since a record's detail page depends only on its own content, never on anything accumulated from other records. The JSON (`vmr-report.json`) is the data source for any dashboarding you want to build on top — anything the Markdown only summarizes or truncates (e.g. Top-5 tool shapes) is present there in full.

**Cost estimate & `pricing.yaml`.** `-pricing pricing.yaml` names a sidecar explicitly; omit the flag and `vmr report` auto-loads `./pricing.yaml` from the current directory if one exists (silently skipped, no error, no $ estimates, if it doesn't). Rates are keyed by provider+model only, not protocol — the same upstream account/model costs the same whether reached over vmr's openai or anthropic ingress, so one rate covers both. Each of the four per-1M-token price fields (`in_fresh_per_1m`/`cache_read_per_1m`/`cache_write_per_1m`/`out_per_1m` — only the first, third, and fourth feed the $ estimate; cache reads are treated as free, matching every provider's own billing) accepts a bare number (the file's own top-level `currency`) or a string with a leading currency code (`"USD0.14"`, `"jpy 1.2"` — case-insensitive, space optional), converted via the top-level `exchange_rate` list (pairs of currencies at an equivalent amount, e.g. `{USD: 1, CNY: 7}` means 1 USD = 7 CNY; chains like USD→EUR→CNY resolve even without a direct USD→CNY entry). A price naming a currency `exchange_rate` never defines — and that isn't the base `currency` either — is a load-time error, not a silent zero. A provider+model can list more than one rate, narrowed by an optional `date_range: [start, end]` (`"2026-07-01"`, yyyy-MM-dd) and/or `hour_range: [start, end]` (`"22:00"`, HH:MM — a start later than end wraps past midnight) for time-varying pricing (an off-peak discount, a promotional window); `vmr report` picks the first rate (in file order) whose window contains a request's own timestamp, so a narrower window must be listed before the catch-all entry it overrides. See the repository's own `pricing.yaml` for a complete, commented example.

`vmr report` also understands agent workloads — offline and purely rule-based, no LLM involved (method and evidence: `docs/VirtualModelRouter_Design_v4_Analytics.md` §2.1):

- **Session → task → turn grouping.** Requests resending the same growing conversation are fingerprinted (first non-system message; Claude Code's `metadata.user_id` when present) and chained by longest-common-prefix, so concurrent agent sessions untangle even when interleaved in time. Task boundaries come from Traceparent trace-id changes and new user instructions in the delta — cross-validated signals. Compaction calls are detected and linked both ways, so a session and its post-compaction continuation form one thread.
- **`vmr-requests.jsonl`** — one feature line per request (session/task/turn, trace and chat ids, request shape, tags like `heartbeat`, per-turn tool calls, finish_reason, ok-but-truncated flag, token splits incl. reasoning tokens, delta size, latest instruction), ready for jq/DuckDB/pandas.
- **Sticky effectiveness (§6.5) ⭐** — Sticky Model exists for exactly one reason, keeping an upstream prompt cache warm; this section is the evidence that it works. Within a session, requests that landed back on the *previous request's* endpoint are compared against those that switched, on cache efficiency. Measured by outcome (endpoint continuity), not by mechanism — a sticky pointer that fired but landed cold still counts as a switch. A session's first request has no predecessor: counted, but in neither group. Under 20 usage-bearing samples in either group the tables still render but no verdict is drawn. **It does not explain why a request switched** — sticky TTL expiry, a health cooldown, a condition eliminating the sticky pick, or sticky being off for that model are indistinguishable after the fact. A second table splits by virtual model, since that is the level `sticky` is configured at.

- **Endpoint value (§6.6) ⭐** — not "what did this endpoint cost" (§2 answers that) but "what did it cost per unit of work, and how long did its failures make you wait": cost per 1M output tokens, cost per successful request, failed attempts, and **wall-clock spent on those failed attempts**. An endpoint that is cheap per token but fails often is not cheap — and a per-endpoint spend column can't show it, because the money lands on whichever endpoint eventually succeeded. **Time only, never money**: a failed attempt carries no usage and providers generally don't bill for one, so attaching a currency figure would be an invention.

- **Tool usage report (§7)** — per declared-tool shape: declared tools vs. tools actually called *this turn* (extracted from the response, so history repeats are never double-counted), plus the "declared but never called" list — both folded into a `<details>` block per shape (numbered, alphabetical) so a 60-plus-tool schema doesn't blow out the document — together with the complete tool declaration's per-request byte cost: the direct input for pruning unused tools from an agent's config.

`vmr report` also exports every record as one human-readable Markdown file **plus a same-named JSON file** (the raw record, for jq/ad-hoc querying) under `{out}/details/`, for drilling into a single request: a header line locating it (trace · chat user · tools — values in **bold**), a `VMR 路由前判断` block reading the same `facts` object described above — only the capabilities actually detected (`image` and/or `tools`), each rendered as a small backtick-quoted tag (`无` when neither), plus the estimated token count — omitted entirely when the record has no `facts`, then the **full message list** with each message folded uniformly and new messages marked with a 🆕 prefix, the increment summary at the end (`🆕 本轮增量（相对上一轮,+N 条,#1–#M 为历史上下文）`), then every upstream attempt with a full side-by-side listing of headers and body fields where changes are emoji-marked (🟢 added / 🔴 removed / 🔶 changed) — including, when a `<think>…</think>` block was stripped, the full pre-strip content and its raw SSE (captured going forward only; older logs show a "not captured" note instead), and the client response with the SSE stream reassembled into the actual model output next to the raw event log. Filenames start with a zero-padded timestamp, so name order is time order. Disable detail export with `-details=false`.

`vmr-requests.md` (alongside `vmr-report.md`, one level above `details/`) is a pure index: one `## Chat User: <key> · N 会话 N 任务 N 轮` (or `## 定时任务 · <class> 单发会话 × N`) entry per group, each with a one-line summary blockquote and a link to that group's own fully-detailed sibling file. The actual **Chat User → Session → Task → Turn** drill-down — each session a `## sNN · <ts> · N 任务 N 轮` heading, each task a `### tNN · <ts> · N 轮` heading with its opening message as a quote block and a turn table (`轮 / 时间 / msgs / finish / dur / ttft / fresh/cached/out / cache-eff⭐ / 文件`; every timestamp in UTC+8 regardless of the source record's own offset) — lives only in that sibling, never duplicated in the index. Siblings: `vmr-requests-<tag>.md` per real `client_key_tag`, `vmr-requests-unresolved.md` for sessions carrying none, `vmr-requests-cron-<class>.md` per scheduled class (`heartbeat`'s file is `vmr-requests-cron-hartbeat.md` specifically; any other scheduled class follows the same `-cron-<class>` pattern). A single-shot scheduled session (heartbeat/dream_diary — exactly one request, no real back-and-forth) always belongs to its class's cron sibling regardless of which client issued it, so near-identical poll turns never drown a real conversation and never appear under two different groupings; a scheduled session with more than one turn (an actual multi-step cron job) is a normal session card under its own caller instead. The index's own flat `# 全部请求（时间序）` table at the end still covers every record regardless of grouping. Every "文件" column links to both the Markdown and the same-named JSON detail file.

**Multiple callers, one instance.** If several clients share one vmr (a teammate, a second agent, a CI job) and you want their usage told apart after the fact, give each one its own entry under `api_keys` (see Configuration above) instead of sharing one key. Every request tags its audit record with that key's own tail (`client_key_tag`, via `KeyTag`: last 8 characters, then — if that window contains a `-` — only what follows the last `-` inside it, so a key ending in `...-alice` reads back as `alice`; keep the meaningful part ≥3-4 characters to avoid two callers' tags colliding). `vmr report` picks this up automatically, no flag needed: for every distinct tag it observed, it writes a `vmr-requests-<tag>.md` detail sibling (see above) — same directory, so its `details/…` links need no adjusting. `vmr-report.md`/`.json`, `vmr-requests.jsonl`, and `details/` itself are never split or duplicated: a request's detail pair is written once regardless of caller, `vmr-requests.jsonl` always covers every record, and the aggregate report always covers everyone. Nothing changes at all — no extra files, no new columns — until `api_keys` is actually configured. Full design rationale: `docs/VirtualModelRouter_Design_v4_Analytics.md` §2.5.

Don't want real auth at all (a trusted private network)? Leave `api_keys` unset — the door stays fully open — but a client that voluntarily sends *any* Authorization/x-api-key value still gets it tagged the same way, no vmr-side config needed: just end each client's own value in `-<label>` and it self-identifies to `vmr report`. No 16-character minimum applies in this mode (there's no secret to protect); a client sending nothing still gets an untagged record.

Agent workloads resend the full conversation on every turn, so a day's log can run into gigabytes — mostly repeated across lines, not within one. Each day's file rotates and compresses automatically once it's no longer "today": zstd on the whole file (not per-line) catches that cross-line repetition, typically 20–75× smaller in practice — far beyond what compressing each record on its own could reach, since a single record never sees the previous turn's near-duplicate body. `vmr report` reads `.jsonl` and `.jsonl.zst` interchangeably, so point it at a glob covering both. Set `audit_retention_days` to also delete files past a given age (default: keep forever — nothing is deleted unless you opt in); either way, deletion and compression are both keyed off the date in the filename, so housekeeping never needs to scan or `stat` the whole log directory. Details and the numbers behind this: Part 1 §9.5 of the design doc.

## Agent task narratives (`vmr story`)

Where `vmr report` answers "how much did everything cost, overall" — `vmr story` answers "what actually happened in this one task, step by step." It reads the same audit JSONL, but instead of aggregating across every request, it reconstructs a single agent task's full execution history: what context entered at each turn, what the model did with it, and (when it happened) what a history-compaction event lost.

```bash
./vmr story "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"           # list candidate tasks
./vmr story -journey j-agent-20260716T152238-20260716T153122-42f908fa \
    "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"                   # render one, by id (a prefix is enough)
./vmr story -render-all "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"   # render every candidate in one pass
```

With no `-journey`, it lists every candidate task with its id, task/turn counts, time range, and a title preview (the opening real instruction) — pick one and pass it to `-journey` as an exact id or a unique prefix. `-render-all` batches every candidate's rendering into one pass, sharing the underlying file scan instead of re-reading the source files once per candidate. Output goes to `{out}/stories/journey-<id>.md` (the narrative) plus `journey-<id>.json` (the same task's behavior profile, see below) — same 0600/0700 permissions as `details/`, since both carry full conversation content. `-show-ungrouped` prints the source location of the first few records that didn't fingerprint into any session — a triage aid when the candidate list looks shorter than expected.

A task whose own beginning looks like it continues a conversation that started outside the files you gave it (a real, multi-turn-looking opening sitting at the very start of your earliest input file) is skipped by default — its id isn't stable across different file-loading ranges. Pass `-include-partial` to render it anyway; its filename picks up a `-partial` suffix as a visible reminder that the id may change next time you load a different range of history.

**Reading the narrative.** Each task is split into a sequence of turns (`Step`), grouped under the user instruction that started them (`Task`). Every turn shows **Messages** (what's new in context this turn) separately from **LLM Response** (what the model itself produced — reasoning, reply text, and each tool call's full arguments), so "what the agent was given" and "what the agent decided to do" never blur into one generic history view. A history-compaction event (detected structurally, not by matching a specific agent framework's marker text — see the design doc for why) renders as its own annotated boundary: tokens before/after, and which file paths/URLs mentioned just before the compaction never got mentioned again afterward. Nothing here is invented — every number traces back to a request's own recorded usage, and every "swallowed" entity claim is a plain substring check you can verify by opening the folded original content next to it.

**Behavior profile** (`journey-<id>.json`, written alongside the `.md`): nine rule-derived, zero-LLM-cost numbers — model time / agent-side execution time / human-idle time split, tool-call distribution, duplicate-action rate, error-recovery count, plan-vs-execute ratio, a per-step context-composition curve (token share by role at each turn, so a context budget's makeup over the task is visible), context-utilization rate (how much of what entered the context ever got referenced again), and compaction count/loss. These are the same numbers whether the underlying agent is Claude Code, OpenClaw, or anything else — cross-framework comparison was the whole point of collecting them.

**Comparing two tasks**: `-compare <id1,id2>` diffs two tasks' behavior profiles directly (no separate `-journey` render needed first) and writes `compare-<id1>-vs-<id2>.md` + `.json` next to the individual task files. Each row shows both values and the relative change, with a rule-based ⚠️ flag on differences large enough to be worth a second look (a fixed threshold, not a judgment call) — useful for "did switching agent frameworks/prompts actually change how this task got done." The report also includes, purely rule-derived (no LLM needed): the endpoint(s) each side actually used, a per-round prompt-cache hit-ratio curve, each side's effective system-prompt size/stability (with a bounded excerpt — 20,000 characters by default, large enough to cover where "which project context files got loaded"-style declarations actually sat in this project's two real validation Journeys, but it's still a from-the-start prefix, not a guarantee for every possible system prompt), the final round's context composition by role, total wall time next to the existing "net working time" (never presented as an efficiency number on its own), how each side terminated, and — if the task's output was produced via a file-write-shaped tool call — the final deliverable's own content side by side. A trailing "证据溯源" (evidence provenance) section lists the source audit file path(s) this comparison actually read, for independent verification.

**Optional LLM interpretation section**: pass `-llm-addr host:port -llm-model name` (an already-running VMR instance's address and its exposed virtual model name — never auto-started; `-llm-key` if that instance requires auth) to append a clearly-labeled, always-optional interpretation section written by that model — a headline sentence, a "candidate root cause | direct evidence | confidence (high/medium/low) | suggested fix" table plus a one-sentence causal chain, a narrative reading of the per-round tool-call sequence, and an explicit "what VMR can't see" caveat. The confidence tiering is fixed in the prompt: only a candidate that points at a specific anchor in the evidence table or excerpt may be tagged "high"; anything resting on elimination or intuition alone must be honestly tagged "low" (and still listed — low confidence isn't a reason to omit it). It's fed only the rule-derived facts above plus two bounded text excerpts (system prompt, final deliverable), never the full transcript, and is prompted not to invent any number outside what it's given, nor to treat "not mentioned in this excerpt" as proof of "doesn't exist" beyond the excerpt's boundary. Add `-llm-dry-run` to just print the evidence-pack size estimate and exit without calling anything. Results are cached under `stories/.llm-cache/` (keyed by both journey ids, the evidence, and the model — switching `-llm-model` never reuses another model's cached answer); any failure (unreachable address, non-2xx, etc.) only skips this section and prints a warning — the rest of the report is unaffected.

Full design (the content-addressed model behind lineage/compaction detection, the nine behavior-profile metrics, known blind spots): `docs/VirtualModelRouter_Design_v4_Analytics.md`.

## Request image downscaling

Optional, off by default. When enabled, inline base64 image attachments exceeding the configured long-side limit are proportionally resized and re-encoded as JPEG before hitting the upstream — cutting vision-token cost for screenshot-heavy agent workflows. Requests only, never responses, never remote URLs; GIFs (animated or not) and anything that fails to decode pass through untouched (fail-open) — see Part 1 §7 of the design doc for why GIF is never rescaled, not even single-frame stills.

Detection is always on, independent of this setting: every inline image in a request — whether or not downscaling is enabled for that virtual model — gets a cheap header-only read (format/dimensions/bytes, no pixel decode) recorded in the audit trail, so `vmr-report.json`'s `images`/`images_compressed` fields reflect real image traffic even on models with downscaling off.

```yaml
image_downscale: 512      # global long-side px cap; 0 or absent = off
image_cache_ttl_days: 7   # eviction age for the on-disk downscale cache (default 7 days, see below)

models:
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

— used exactly as given when set (a leading `~/` expands to the home directory), else the persistent `~/.vmr/logs`/`~/.vmr/image_cache`, else (no resolvable home directory) a `vmr_logs`/`vmr_image_cache` subdirectory of the system temp dir, else `./logs`/`./image_cache` next to the binary. Persistent by default on purpose: macOS purges temp-dir entries not accessed for ~3 days, which would silently delete audit data — the only data source `vmr report` has. Run `vmr check -c config.yaml log` / `vmr check -c config.yaml cache` to see the resolved path without starting the server (also printed by plain `vmr check` and the startup summary). `vmr.sh` queries `vmr check log` itself for the server-log path rather than guessing, so a launchd/systemd-supervised vmr never disagrees with a manually-started one about where its data lives. Neither directory has an environment-variable override — reference `${VAR}` inside `log_dir`/`image_cache_dir` if you want a value from the environment.

## Endpoints & CLI

| Endpoint / command | Purpose |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI-protocol ingress (streaming + non-streaming) |
| `POST /v1/messages` | Anthropic-protocol ingress (streaming + non-streaming) |
| `GET /v1/models` | virtual model list (parseable by both SDK families) |
| `GET /admin/status` | endpoint health + concurrency metrics, including whether a recovery probe (passive or active) currently has an endpoint's single-flight slot (loopback only) |
| `vmr start -c config.yaml [-audit=false]` | run the router in the foreground (Ctrl-C to stop); `-audit=false` turns off the JSONL audit log (on by default). `./vmr.sh start` is the background-supervised equivalent and is the one command it shadows — run this one directly for foreground/dev use |
| `vmr check -c config.yaml` | validate config, run the consistency scan (missing api_key, a proxy that silently falls back to direct, a duplicate endpoint, …), and print the routing table with key status and per-provider effective proxy — flagged values get an inline ⚠️ plus a trailing `=== Failed ===` summary. With a trailing `log`\|`cache` argument, print just that resolved directory instead (`log_dir`/`image_cache_dir` after defaults) — what `vmr.sh` queries internally |
| `vmr status -c config.yaml` | render a running instance's identity (pid / listen / uptime / absolute config path) plus health and concurrency. `-addr host:port` queries whatever instance holds that port without loading a config at all — for when several instances run on one machine, or you don't have that instance's config; `-brief` prints one tab-separated summary line (what `./vmr.sh ps` builds its table from) |
| `vmr report [-o dir] [-pricing pricing.yaml] <glob>` | audit logs (plain or `.zst`) → usage statistics + session/tool analysis + per-request features (`vmr-requests.jsonl`) + detail files (`-details=false` to skip); adds the §2 cost-estimate section once a pricing sidecar is loaded — `-pricing` names one explicitly, or a `./pricing.yaml` in the current directory is auto-loaded when `-pricing` is omitted |
| `vmr story [-journey <id> \| -render-all \| -compare <id1,id2>] <glob>` | reconstruct one agent task's full execution history into a readable Markdown narrative (see "Agent task narratives" below); no args lists candidate tasks with their ids, `-render-all` renders every one in a single batched pass, `-compare id1,id2` diffs two already-built tasks' behavior profiles (rule-derived facts plus, with `-llm-addr host:port -llm-model name [-llm-key KEY] [-llm-dry-run]`, an optional LLM interpretation section) |
| `vmr version` | print this binary's build identity (git SHA, `-dirty` suffix when built from a modified tree, plus commit time and Go version). No ldflags needed — Go stamps VCS state into any binary built inside a repository and this reads it back at runtime. A running instance reports the same value under `/admin/status` and in `./vmr.sh ps`'s VERSION column, so "is that process running the binary I just built?" is a direct comparison |
| `vmr diagnose [-c config.yaml]` | beyond `check`'s static preview: DNS/TLS/proxy reachability per provider, then a real minimal request per configured endpoint asking for a one-time token echoed back (run concurrently, `-test-timeout` per check, default 15s) — a 200 that doesn't echo it warns instead of passing, catching a relay/gateway that answers with a cached or canned response instead of a fresh completion — plus a routing-order preview annotated with what it found (`-no-test-routing` to skip the live requests, `-json` for scripting; exits non-zero if anything failed) |
| `vmr replay -provider NAME <audit.jsonl>` | rebuild and resend one request from an audit record through the exact same request-building path vmr itself uses — `-dry-run` to print without sending, `-record path` to save the replay as its own audit line, `-model`/`-protocol` to override what the record itself says, `-stream true\|false` to force streaming on/off, `-max-time` to cap the upstream wait. Pick the record with `-detail file` (a `vmr report` `details/*.json` file — no line-counting needed), `-ts <timestamp>` (matches either `vmr-requests.jsonl`'s or the raw audit log's `ts` field), or `-line N` (default: the last one in the file) — the three are mutually exclusive |
| `./vmr.sh start\|stop\|…` | dev-mode lifecycle (you supervise) |
| `./vmr.sh ps` | list every vmr instance on this machine, not just this checkout's: pid, listen address, uptime, model count, absolute config path. Three steps, each doing what it's actually good at — `pgrep` finds the processes, `lsof` finds the port each holds (the listen address lives in that process's config, not on its command line), and `vmr status -addr … -brief` asks the instance itself for the rest. Without `lsof`, or for a process that doesn't answer `/admin/status`, the row degrades to pid + the `-c` argument as typed, flagged with why — never to a missing instance |
| `./vmr.sh service install\|uninstall\|start\|…` | init-system service (launchd/systemd: crash restart, start at login) |
| `./vmr.sh <any command above> [args]` | any subcommand the script doesn't own is forwarded verbatim to the binary (`./vmr.sh check`, `./vmr.sh diagnose`, `./vmr.sh report …`) — not a whitelist, so a subcommand added to the binary works here immediately. Forwarding does two things: **runs from the caller's original directory** (relative paths, globs and `-o` mean exactly what they'd mean under a bare `vmr`), and **injects `-c <this checkout>/config.yaml`** when you didn't name one (`report` has no `-c`, so nothing is injected there). Foreground `vmr start` is the one command the script shadows — its `start` is the background one; run `./vmr start -c config.yaml` directly for the foreground |

Routed responses carry `X-VMR-Endpoint` (the endpoint that served it), `X-VMR-Attempts` (tries used) and `X-VMR-Route-Reason` (why that endpoint: `pick=sticky|order`, `eligible=N/M`, plus `cooldown=` / `conditions=` / `ctx_fallback=1` only when they actually happened). Once any attempt has failed they also carry `X-VMR-Failover` (e.g. `deepseek/deepseek-v4:429, minimax/m2:500`; a build or network failure with no HTTP response is `:err`) — **including on success**, so "that worked, but only on the third failover" is visible in your terminal instead of in the audit log afterwards.

```bash
# Something's misconfigured — find out what before staring at 401s in the logs.
./vmr diagnose -c config.yaml

# A request failed; see exactly what vmr would send without sending it.
./vmr replay -c config.yaml -provider openrouter -dry-run \
    "$(./vmr check -c config.yaml log)/vmr-audit-2026-07-13.jsonl"

# Same request, actually sent, response printed to stdout.
./vmr replay -c config.yaml -provider openrouter \
    "$(./vmr check -c config.yaml log)/vmr-audit-2026-07-13.jsonl"

# Found the failing request in vmr-requests.md or vmr-report.md instead?
# Point -detail straight at its details/*.json — no line-counting needed.
./vmr replay -c config.yaml -provider openrouter -dry-run \
    -detail out/details/20260713-153042.100_coding_z-ai-glm-5.2_error.json

# Or replay by the exact timestamp shown in vmr-requests.jsonl / vmr-report.md.
./vmr replay -c config.yaml -provider openrouter -dry-run \
    -ts 2026-07-13T15:30:42.100+08:00 \
    "$(./vmr check -c config.yaml log)/vmr-audit-2026-07-13.jsonl"
```
