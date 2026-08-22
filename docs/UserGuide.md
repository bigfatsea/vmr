<!-- Ver 2026-08-06 14:00, by Sonnet 5 -->

# vmr — User Guide

English | [简体中文](UserGuide.zh.md)

Full configuration reference, protocol behavior, and CLI details. If you just want to get running, see the [README](../README.md) Quick Start first — come back here for everything past that.

## Contents

- [Configuration](#configuration)
  - [Config layout](#config-layout)
  - [Startup and reload checks](#startup-and-reload-checks)
  - [Per-request memory budget](#per-request-memory-budget)
  - [Upstream proxy](#upstream-proxy)
  - [base_url and API versions](#base_url-and-api-versions)
  - [Role remapping (role_map)](#role-remapping-role_map)
  - [Endpoint try-order (priority and strategy)](#endpoint-try-order-priority-and-strategy)
  - [Multi-provider endpoint groups and global fallbacks](#multi-provider-endpoint-groups-and-global-fallbacks)
  - [Environment variables](#environment-variables)
- [Request handling and routing](#request-handling-and-routing)
  - [Passthrough and normalization](#passthrough-and-normalization)
  - [Failover and health](#failover-and-health)
  - [Condition-based routing](#condition-based-routing)
  - [Sticky Model (session affinity)](#sticky-model-session-affinity)
  - [Quota-Aware Routing](#quota-aware-routing)
- [Audit and reporting](#audit-and-reporting)
  - [The audit log](#the-audit-log)
  - [Usage and cost reports (vmr report)](#usage-and-cost-reports-vmr-report)
  - [Agent task narratives (vmr story)](#agent-task-narratives-vmr-story)
- [Image downscaling](#image-downscaling)
  - [Per-model override](#per-model-override)
  - [Downscale result cache](#downscale-result-cache)
  - [Where the audit and cache directories land](#where-the-audit-and-cache-directories-land)
- [CLI and endpoint reference](#cli-and-endpoint-reference)

## Configuration

### Config layout

`providers` is a flat list — one entry per upstream account, however many of the three ingress protocols (`openai` / `anthropic` / `openai-responses`) it actually speaks. `base_url` is itself keyed by protocol, so one entry covers both faces of an account instead of declaring it twice. `models` is keyed by virtual-model name; each entry under `endpoints` carries its own `protocol` field, so one virtual model can mix an openai-protocol candidate list and an anthropic-protocol one under the same name — each independently reachable only from its own ingress. An endpoint-group's `models:` list can name more than one upstream model, each expanding into its own independently health-tracked candidate sharing the rest of that entry's fields:

```yaml
listen: 127.0.0.1:8800
# api_keys:                    # optional: protect vmr itself (Bearer or x-api-key accepted); each
#   - ${VMR_KEY_ALICE}          # entry is tagged in `vmr report` output by its own tail (see
#   - ${VMR_KEY_OPENCLAW}       # "Multiple callers, one instance" below). The old singular api_key
#                               # was removed — configs still using it are rejected as an unknown field
# max_attempts: 0              # cap on upstream tries per request (default 0 = walk every candidate)
# probe_timeout: 15s            # upper bound on one background recovery probe, see Failover and health below
# max_request_body_mb: 8       # inbound request body size cap (stability only; the audit trail always records requests in full, whatever size vmr accepted)
# max_concurrency: 8           # global gate; excess requests wait in memory (default: unlimited) — see "Per-request memory budget" below before leaving this unlimited on a shared instance
# https_proxy: http://127.0.0.1:7890   # proxy server URL for https base_urls — the ONLY way vmr uses a proxy
#                                      # (env vars are ignored; write ${HTTPS_PROXY} to reference one explicitly).
#                                      # Declaring this URL does NOT turn proxying on by itself — see `proxy` below
# http_proxy: http://127.0.0.1:7890    # same for http base_urls (e.g. a LAN llama.cpp server)
# image_downscale: 512         # long-side px cap for inline request images (default: off; a model's own setting overrides this, see below)
# image_cache_ttl_days: 7      # eviction age for cached downscale results (default: 7 days)
# audit_retention_days: 30     # delete audit files older than this (default: keep forever)
# extra_redact_headers:        # additional client request header names to mask in the audit trail,
#   - X-Custom-Token            # same treatment as the built-in Authorization/X-Api-Key/Cookie/etc
#                                # list (case-insensitive). Absent/empty (the default) changes nothing.
# timeouts:
#   connect: 10s               # upstream dial
#   response_header: 120s      # upstream time-to-first-byte
#   stream_idle: 120s          # abort any upstream body (stream, JSON, error) silent for this long

providers:
  - name: openrouter
    base_url: {openai: https://openrouter.ai/api/v1, anthropic: https://openrouter.ai/api/v1}
    api_key: ${OPENROUTER_API_KEY}
    proxy: true              # go through https_proxy/http_proxy above — the recommended
                             # way to opt a foreign provider in (default: false, direct)
  - name: minimax
    base_url: {openai: https://api.minimaxi.com/v1}
    api_key: ${MINIMAX_API_KEY}
    # proxy: false           # not needed here — false is already the default

models:
  coding:                      # openai protocol only → served via /v1/chat/completions
    endpoints:
      - {protocol: openai, providers: [openrouter], models: [z-ai/glm-5.2]}   # no priority field: list order = try order
  claude:                      # anthropic protocol only → served via /v1/messages
    endpoints:
      - {protocol: anthropic, providers: [openrouter], models: [minimax/minimax-m3]}
  agent:                       # openai-responses protocol only → served via /v1/responses
    endpoints:
      - {protocol: openai-responses, providers: [openrouter], models: [z-ai/glm-5.2]}
```

All fields and validation rules: Part 1 §10 of the design doc. Config edits hot-reload within seconds; a broken config is rejected and the running instance keeps its current one. Parsing is strict: an unknown or misspelled key (`max_concurency: 8`) is a load error, never a silently ignored no-op you believe is in effect.

### Startup and reload checks

Beyond the strict parsing above, vmr also runs a set of *operational* checks — the same ones `vmr check`/`vmr diagnose` print — on every start and every hot reload (fsnotify or SIGHUP, including the reload a service manager's auto-restart triggers), and logs each hit as a `WARN config check: ...` line. The main one worth knowing about: an empty `api_key` (a typo'd or unset `${ENV_VAR}`) is syntactically valid YAML, so it loads and hot-reloads without error — every request against that provider then 401s until someone notices. The warning is the only signal; it never blocks the start or reload, since a Check() issue by definition means "this can still run, but may be wrong."

### Per-request memory budget

Three buffer caps, each independently reasonable, multiply: `max_request_body_mb` (default 8MB, the inbound request), the response-normalization buffer (8MB, guards against a runaway upstream when vmr has to buffer a response instead of streaming it), and the audit response copy (16MB, bounds how much of a response the audit trail retains, kept a bit above the normalization buffer since a truncated audit copy loses `vmr report`/`vmr story` information rather than just "smart" normalization). All three are sized off today's ~1M-token context windows (~3-4MB of bytes) with roughly 2x headroom, not arbitrary round numbers. Worst case is roughly the sum — about 32MB — resident per in-flight request, before `bytes.Buffer` growth overhead. `max_concurrency` defaults to unlimited, so that number is only bounded by how many requests actually arrive at once. On a single-user local instance this is background noise; on a shared instance, set `max_concurrency` to something concrete rather than leaving the product of these four numbers open-ended.

A request carrying inline images has a fourth, transient peak on top of that sum, and none of the three caps above bounds it: while `image_downscale` is being applied, vmr decodes each image to an uncompressed bitmap. That bitmap is sized by the image's **pixel count**, not by how many bytes its base64 took — a 4K screenshot arriving as 1.5MB of base64 decodes to roughly 33MB in memory, over 20x its wire size. `max_request_body_mb` caps bytes and cannot see pixels.

Two things keep this bounded. Images are decoded one at a time and each bitmap is released before the next is read, so a request with ten screenshots peaks at one screenshot's worth, not ten. And an image whose declared dimensions exceed ~64 megapixels is never decoded at all — it passes through at full resolution instead. That second limit exists to stop decompression bombs (a flat-color PNG can declare enormous dimensions in a few KB), so it is sized for safety rather than for a memory budget: 64MP works out to roughly 256MB for a single decode, well above the 32MB the three caps above account for. Reaching it requires a deliberately hostile input; ordinary screenshots and photos land one to two orders of magnitude below.

The peak is short-lived — released as soon as downscaling finishes, before the upstream call is even made — and a single-user vision workload never notices it. It does scale with concurrency, though: if you serve vision traffic from a shared instance, set `max_concurrency` more conservatively than the 32MB arithmetic alone suggests. Setting `image_downscale: 0` disables downscaling for that model and removes the peak entirely (vmr still detects and records image metadata, it just never decodes pixels) — at the cost of sending full-resolution images upstream and paying the vision tokens for them.

### Upstream proxy

Explicit config only, default off. `http_proxy`/`https_proxy` above only declare *where* the proxy lives — they don't turn it on for anyone by themselves. Whether a provider actually uses it is decided entirely by that provider's own `proxy: true`/`false` (default `false`, direct — there's no global default to inherit; opt providers in one at a time). When it's `true`, the base_url's scheme picks `https_proxy` or `http_proxy`. Proxy **environment variables are deliberately ignored** — an implicit knob that silently redirects traffic is exactly the surprise a router shouldn't have; to use one, reference it explicitly (`https_proxy: ${HTTPS_PROXY}`). `proxy: true` with no matching proxy URL configured is a config validation error, not a runtime surprise. `vmr check` and the startup summary print each provider's effective proxy (credentials masked). YAML 1.2: write `true`/`false`, not `on`/`off`.

### base_url and API versions

vmr pre-computes each provider's complete upstream URL at initialization by appending the protocol's bare path (`/chat/completions` for OpenAI Chat Completions, `/messages` for Anthropic, `/responses` for OpenAI Responses) directly to `base_url` — no normalization, no overlap detection. `base_url` must therefore already carry the provider's own full API version, whatever that provider calls it: `https://api.example.com/v1`, `https://api.minimaxi.com/anthropic/v1`, `https://ark.example.com/api/coding/v3`. This matters because not every provider versions its OpenAI/Anthropic-compatible surface as `v1` — Volcengine's coding-plan OpenAI endpoint is `v3`, for instance — so vmr never assumes a version on your behalf; get it wrong and the 404 shows up immediately against the exact base_url you wrote. The URL is computed once at config load and stored on the endpoint; the adapter uses it directly, never constructing or normalizing a URL per request.

### Role remapping (role_map)

Some OpenAI-compatible providers reject roles their upstream doesn't recognize — the canonical case is the `developer` role OpenAI introduced for o1/o3-series models, which some gateways (e.g. DashScope/Qianwen) reject outright. `role_map: {developer: system}` under a `models.<name>.endpoints[]` entry rewrites matching `"role"` values inside the top-level `messages` array (or, for a `protocol: openai-responses` entry, the top-level `input` array) before the request leaves vmr, with no client-side change needed. It's a plain old→new string map, applied only to the exact roles listed — every other byte of the request (key order, whitespace, unknown fields, message content) passes through untouched, the same byte-splice approach `RewriteModel` uses for the model field. Scoped to the endpoint-group, not the provider or the virtual model as a whole, since the same account can back several endpoint-groups (different virtual models, different upstream model families) that don't all necessarily need the same rewrite; a model that never sends the mapped role is unaffected either way. Omit `role_map` (or leave it empty) for an entry whose upstream accepts every role as-is — the default.

### Endpoint try-order (priority and strategy)

Each entry under `endpoints:` can set `priority: N` (an int, default 0); endpoints sort by ascending priority before being tried, and ties (the common case — nobody sets it) keep config-file order because the sort is stable. In practice this means just listing endpoints in the order you want them tried is enough; reach for an explicit `priority` only when you want to reorder without reshuffling the list itself. `priority` is one dimension in a virtual model's `strategy` list (`strategy: [priority]` is the default and, as of this writing, the only ordering dimension actually registered — there's nothing else to add to that list yet), so most configs never need to set `strategy` at all.

### Multi-provider endpoint groups and global fallbacks

Two config shapes exist purely to cut repetition in a try-order list that would otherwise repeat the same tail across several accounts or several virtual models — neither changes what's reachable, only how much YAML it takes to say so.

**Multiple providers in one endpoint-group entry**: `providers:` always takes a list — `providers: [p1]` for the common single-account case, `providers: [p1, p2]` when several accounts are interchangeable candidates for the same upstream model(s) — typically several accounts on the same vendor.

```yaml
providers:
  - name: volcengine
    api_key: ${ARK_KEY_1}
    base_url: {openai: https://ark.example.com/v3}
  - name: volcengine2
    api_key: ${ARK_KEY_2}
    base_url: {openai: https://ark.example.com/v3}

models:
  coding:
    endpoints:
      - protocol: openai
        providers: [volcengine, volcengine2]
        models: [deepseek-v4-pro]
        priority: 1
```

This expands into as many independent, individually health-tracked endpoints as `providers` × `models` — outer loop over `models`, inner loop over `providers`, so every named provider is tried for the preferred model before the entry falls through to the next model. Each account keeps its own `quota:`/`pricing:` (declared on its own `providers[]` entry, same as always — merging the try-order line doesn't merge the accounts' quota ledgers); `vmr check` shows the expanded list exactly like a hand-written multi-entry version would.

**Global fallback endpoints**: a top-level `fallback_endpoints:` list, same entry shape as `models.<name>.endpoints[]`, appended to the tail of *every* virtual model's own try-order instead of being pasted onto each one:

```yaml
fallback_endpoints:
  - protocol: openai
    providers: [bai, sensenova]
    models: [deepseek-v4-flash]
    priority: 98

models:
  coding: {endpoints: [...]}   # gets the fallback above appended automatically
  cheap:  {endpoints: [...]}   # so does this one
```

A fallback entry only attaches to a virtual model that already has its own entry point on the fallback's `protocol` — it augments an existing ingress, it never opens a new one a model didn't already declare (an anthropic-only model is untouched by an openai-protocol fallback). A virtual model can opt out entirely with `fallback: false`. Unlike an ordinary endpoint-group, `priority` on a fallback entry is **required and must be > 0** — omitted/0 would silently compete at the same tier as a model's own real endpoints instead of trailing behind them, and that's exactly the kind of surprise `vmr check`'s load-time validation exists to catch instead of a request routing there by accident. `vmr check` prints a fallback-origin endpoint with a trailing `fallback` annotation, and flags (⚠️) a fallback that would silently duplicate an endpoint a model already declares for itself.

### Environment variables

The complete list — vmr reads nothing else from the environment:

| Variable | Effect |
| --- | --- |
| Any `${VAR}` referenced in `config.yaml` | Expanded when the config is loaded (and on every hot reload). Unset variables expand to the empty string. This is the *only* way anything from the environment reaches vmr — API keys, and, if you choose, `${HTTPS_PROXY}` or a directory path. |
| `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` / `ALL_PROXY` | **Ignored.** Proxies are explicit config (`http_proxy`/`https_proxy` above); reference `${HTTPS_PROXY}` in the config to opt in. |

Directories (`log_dir`, `image_cache_dir` — see below) and proxies are config fields, not environment variables: where vmr writes and how it reaches the network shouldn't depend on implicit environment state. In service mode, `vmr.sh service install` snapshots every `${VAR}` your config references from the current shell into `~/.config/vmr/env` (0600, never overwritten) — nothing else needs injecting, since the binary reads its own config directly.

## Request handling and routing

### Passthrough and normalization

**Principle: direct-connection equivalence.** What a client receives through vmr — bytes, headers, transfer pacing — matches a direct provider call. The only deviations:

- the `model` field — rewritten to the real upstream name on the way out, back to your virtual name on the way in (SDKs assume `response.model === request.model`). The outbound rewrite is a byte splice on the top-level `model` value only: every other request byte — key order, whitespace, unknown parameters — reaches the provider exactly as your client sent it;
- two **MiniMax-M3-specific repairs**, each gated on detecting its exact upstream shape: stripping inline `<think>…</think>` reasoning from content (left in place, it gets persisted into history and locks the model into a feedback loop), and stripping the plain-text "Thinking Process:" section emitted under a MiniMax thinking mode that doesn't use `<think>` tags at all. Both repairs are triggered by a literal wording guard, so a response that still looks like the leaked-thinking shape (numbered subsections, long) but didn't match the guard gets tagged `thinking_process_pattern_detected` in `attempts[].norm` instead — bytes untouched, a pure signal for spotting a wording drift before it silently stops being stripped;
- the `data: [DONE]` sentinel — appended **only** for OpenAI-protocol SSE when the upstream omitted it; never duplicated, never injected into Anthropic streams.

Streaming is real: events forward as they arrive; buffering engages only for detected thinking shapes and resumes live streaming once `</think>` closes. Compressed (`Content-Encoding`) bodies pass through untouched. Upstream 3xx redirects are never followed — a 301/302/303 reaches the client with its original status, `Location` header, and body, exactly as a direct call would show (the default `http.Client` policy silently rewrites POST to GET on 301/302/303, which would violate byte-faithful passthrough). Response headers follow the same policy as request headers — everything forwarded except hop-by-hop; error responses return verbatim with status, headers (`Retry-After` included), and body. Every normalization actually applied is recorded in the audit log (`attempts[].norm`), so any byte difference between upstream and client is explained, per request.

Because passthrough is byte-level, a new request/response field on any of the three protocols needs no vmr change to reach the upstream or the client — that's the whole point. What vmr does **not** do: it only routes `POST /v1/chat/completions`, `POST /v1/messages`, and `POST /v1/responses`. Other OpenAI/Anthropic surfaces (`/v1/realtime`, `/v1/images`, `/v1/audio`, …) aren't in scope — point a client at the provider's own base URL for those. The `openai-responses` protocol face is newer and narrower than the other two: as of this writing only DeepSeek and OpenRouter document a Responses-compatible endpoint (MiniMax doesn't yet), and both force stateless usage — `store: true` / a non-null `previous_response_id` gets rejected by the upstream itself, not by vmr (vmr never inspects or strips request fields; it only routes). If you use `previous_response_id` or manually-replayed encrypted reasoning items against an upstream that *does* support them, note that vmr's failover can route a later turn in the same conversation to a different endpoint than the one that created that state — Sticky Model (below) reduces how often that happens but doesn't eliminate it structurally.

### Failover and health

On upstream failure vmr walks the endpoint list in order until one succeeds or all are exhausted (`max_attempts` optionally caps the walk). Health is failure-driven, classified per response so a failure gets the right penalty and the right verdict on whether to keep failing over:

- network/5xx: short cooldown with exponential backoff; 401/quota-exhausted/unknown-model/a relay or gateway reporting its own forwarding failure (as opposed to something wrong with the request itself): long cooldown; 429/503: `Retry-After` honored;
- 400-class **client** errors — a genuinely bad request — return immediately, no failover, no cooldown: every endpoint would reject the same request the same way, so there's nothing switching providers would fix;
- **content-policy blocks** fail over to the next provider but do **not** penalize the blocked endpoint — it rejected one request; it isn't down.

**Recovering a cooled-down endpoint**: once an endpoint's cooldown expires, vmr fires one small dedicated probe request in the background (bounded by `probe_timeout`, default 15s) instead of letting the next real request find out the hard way. Real traffic never touches — and never waits behind — an endpoint that hasn't been confirmed recovered yet; it's simply routed to the next candidate until the probe reports back, however long that takes. The probe asks the model to echo back a one-time token, so a relay/gateway answering with a cached or canned "success" doesn't count as recovered.

```yaml
probe_timeout: 15s      # upper bound on one background recovery probe
```

All-candidates-failed returns the last upstream error verbatim. Streams only fail over before the first byte is written.

### Condition-based routing

Endpoints behind one virtual model don't have to be interchangeable. Declare what each one actually supports, and a request that needs something a given endpoint doesn't declare skips it — rather than being tried against an endpoint that's certain to reject it:

```yaml
models:
  agent:
    capabilities: [text, tools]        # base: every endpoint below inherits this
    max_context_tokens: 128000         # base: ditto
    endpoints:
      - protocol: openai
        providers: [minimax]
        models: [MiniMax-M3]
        capabilities: [image]          # ADDED to the base -> effective: text, tools, image
        max_context_tokens: 1000000    # OVERRIDES the base for this endpoint alone
      - protocol: openai
        providers: [deepseek]
        models: [deepseek-chat]        # declares neither -> inherits the base as-is
```

Both fields are optional at both levels and default to **unconstrained**: a virtual model with no `capabilities` has no base, and an endpoint that doesn't add its own is assumed to support everything the model does (or, absent any declaration anywhere, everything at all) — existing configs behave exactly as before. `capabilities` is *additive* per endpoint (union with the model's base) since `max_context_tokens` is *override-or-inherit* instead (a single number can't be unioned). Once an endpoint's effective capability set is non-empty it's exhaustive (list everything it actually supports, not just what you want checked); `vmr check` prints each virtual model's base and each endpoint's own declared extras/override so a gap is visible before it causes a misroute.

Two different kinds of condition:

- **`image` / `tools`** — hard requirements. A request needing one and finding no eligible candidate fails fast with a `vmr_no_candidates` error naming the missing capability, instead of wasting an attempt on an endpoint guaranteed to reject it. `image` detection is structural (does the request actually contain an `image_url`/`source` content block?), not a text-content guess — a request whose text merely happens to mention the word "image" is never misdetected, and one whose image is in a format vmr's own decoder doesn't recognize still counts as an image (detected ≠ decodable). (`thinking`/`audio`/`video` aren't checked yet — request-side detection for those isn't confirmed across providers, so declaring them today has no effect.)
- **context length** — a coarse, deliberately conservative *estimate*, not a certainty: request bytes classified ASCII (~4 bytes/token) vs. multi-byte UTF-8/CJK (~2 bytes/token, intentionally pessimistic), a flat ~3000 tokens per detected inline image, and detected document/PDF attachments sized by their base64 payload length ÷ 20 — no parsing beyond cheap structural markers. Because it's only an estimate, it can never by itself refuse a request: if every endpoint's declared `max_context_tokens` looks too small, vmr falls back to trying the capability-eligible candidates anyway rather than returning an error on a guess — an overestimate costs at most a wasted attempt, never a request that would have worked.

Full design and the token-estimate calibration: `docs/VirtualModelRouter_Design_v4_Core.md`, "Condition-based Routing" section.

### Sticky Model (session affinity)

Upstream prompt caches are keyed on an exact byte prefix. If a multi-turn agent conversation gets routed to a different endpoint mid-conversation, the provider's cache goes cold and a "better" routing choice can end up costing more, not less — condition-based routing above is one thing that can trigger this (e.g. a context estimate that shrinks below another endpoint's declared ceiling after the agent compacts its history). Sticky Model keeps a conversation on whichever endpoint most recently, successfully served it:

```yaml
sticky_ttl: 10m              # global default: how long a sticky preference stays valid

models:
  agent:
    # sticky: true is the default — omit it. Only a genuinely one-shot
    # virtual model (no multi-turn value to protect) needs sticky: false.
    endpoints:
      - protocol: openai
        providers: [minimax]
        models: [MiniMax-M3]
        # inherits the global 10-minute sticky_ttl
      - protocol: openai
        providers: [deepseek]
        models: [deepseek-chat]
        sticky_ttl: 2h      # DeepSeek's disk-based cache lasts hours to days — override per endpoint
```

- **Identity**: a conversation is fingerprinted from its system prompt *and* first non-system message — both hashed, never logged or otherwise exposed. Two different agents that happen to open with the same line don't collide, because their system prompts (and therefore their actual upstream cache prefixes) differ; hashing only the first user message, without the system prompt, would have missed exactly that case.
- **`sticky_ttl` is per-endpoint, not per-model** — cache lifetime is a property of the upstream provider (Anthropic/OpenAI/MiniMax: roughly 5–10 minutes; DeepSeek: hours to days), so endpoints behind the same virtual model can each declare their own window instead of forcing one value on all of them. The global `sticky_ttl` (default 10 minutes) is the fallback for endpoints that don't override it.
- **`sticky_ttl` (global or per-endpoint) can't exceed 24 hours** — the sticky registry itself drops an idle entry from memory after 24 hours regardless of what any endpoint's TTL says, so a longer setting would load but silently stop taking effect. `vmr check`/`vmr start`/hot reload all reject a config that tries it, with an error naming the offending model/endpoint.
- Affinity only ever reorders within the endpoints that already passed health and condition filtering — an endpoint that's since become unhealthy or lost a required capability is never resurrected just because it was the sticky pick last time.
- The pointer moves on every successful completion, including a failover success, so it always follows wherever the conversation's cache is actually warm — a stale pointer self-corrects on the next successful turn, no separate invalidation logic needed.

Full design (identity choice, TTL research behind the defaults, why this fingerprint is a separate implementation from `vmr report`'s offline session grouping below): `docs/VirtualModelRouter_Design_v4_Core.md`, "Sticky Model" section.

### Quota-Aware Routing

If you're juggling several periodic usage plans (a "coding plan" or "token plan" tied to one provider account) plus maybe some pay-as-you-go endpoints as a fallback, Quota-Aware Routing biases new sessions toward whichever account has the most *slack relative to how much of its billing period has elapsed* — not simply whichever has the most quota left. That distinction matters because reset days rarely line up: a plan that just reset looks "most remaining" under a naive calculation, but a plan that's 90% through its month with unused quota is the one about to waste it. Configure it per provider:

```yaml
providers:
  - name: plan-a
    base_url: {openai: https://example.invalid/v1}
    api_key: ${PLAN_A_KEY}
    quota:
      limits:
        # a provider can carry more than one window (P3) — the tightest
        # constraint decides: a short window acts as a rate-limiting "gate"
        # (it can only suppress the score, never boost it), the longest
        # window on the account is its "bucket" (an underused bucket DOES
        # boost the score — "use it or lose it"). One entry is still the
        # common case and behaves exactly like before.
        - metric: requests       # or: tokens (input + output, equal-weighted)
          every: 1min            # N{min,h,d,w,mo} — also valid: 5h, 2w, 3d
          amount: 60              # a short RPM-style gate
        - metric: requests
          every: 1mo             # the longest window here -> this one is the bucket
          since: 2026-08-01      # period anchor; every later period is derived
                                  # automatically. Omit entirely to anchor at
                                  # config load/reload time itself (no calendar
                                  # alignment) — accepted forms: YYYY-MM-DD,
                                  # RFC3339, or a bare hh:mm[:ss] for min/h
                                  # Limits only
          amount: 90000          # this window's cap, in vmr's OWN observed unit — see below
```

An endpoint with no `quota:` block behaves exactly as before — this is entirely opt-in, per provider.

**Scope so far** (see `docs/VirtualModelRouter_Design_v4_Quota.md`'s "现状与后续计划" section for the full current-status breakdown): any number of `limits:` entries per provider, each `metric: requests`, `metric: tokens`, or `metric: cost` (see [Pricing and cost-metric quota](#pricing-and-cost-metric-quota) below), tumbling (non-rolling) windows only. `rolling: true` remains a **load-time error** naming the field and saying it's planned for a later batch. A Limit can also be scoped to specific upstream models via `models: [name, ...]` — omitted means "every model on this provider"; see the `models:` example below.

**`amount` must be calibrated to what vmr itself observes, not the plan's marketing number.** Some vendors bill "one user turn" as a single unit, but an agentic client (tool calls, retries, multi-step workflows) turns that into anywhere from one to twenty-plus real HTTP requests vmr actually sees and counts. Set `amount` from your own traffic — a few days of `/admin/status`'s `quota` section, or a `vmr report` run — not from the number on the pricing page. Getting this wrong doesn't break anything (an under-provisioned account just gets deprioritized sooner than it should), but it does make the routing decision less accurate than it could be.

For `metric: tokens`, vmr prefers the upstream's own reported usage (exact) and only falls back to a byte-count estimate when that's unavailable (a compressed response, a provider that doesn't report usage, or a stream that got cut off mid-response) — the fallback is deliberately biased to *overestimate*, and how much of an account's running total came from the fallback is visible as `estimated_pct` in `/admin/status`.

**What it does NOT do**: it never removes an endpoint from the candidate list — an exhausted account just sorts to the back of its priority tier, so failover still reaches it if everything else is unavailable. It never overrides Sticky Model — an established conversation keeps its endpoint even if that account's quota has since run low; reordering only ever applies to a new session. And it never triggers on its own — quota exhaustion doesn't cool an endpoint down the way a real 429/402 does (that's still `internal/health`'s job).

`vmr replay -provider NAME ...` charges against the same quota state a live request would, on a successful (`<400`) response — it's replaying real traffic against a real upstream account, so it counts the same way. `-dry-run` never charges (nothing is sent).

**Checking it**: `vmr check` prints each provider's configured limit(s) (and the effective timezone period boundaries are computed in — see the timezone note below); `/admin/status`'s `quota` section and `vmr status` show one row per Limit — its `role` (`bucket` or `gate`, see below), live consumption (`used`/`amount`/`pct`/`headroom`/`period_ends_at`/`estimated_pct`), the raw fresh/cache-read/cache-write/output breakdown, and — when configured — that Limit's own `token_weights`/`model_multipliers`/`models` scope; a response's `X-VMR-Route-Reason` header shows `pick=quota` when reordering actually changed which endpoint went first.

**Bucket vs. gate, when a provider carries more than one Limit.** The Limit with the *longest* period is the account's "bucket" — its unused headroom really is being wasted if it isn't spent ("use it or lose it"), so an underused bucket actively boosts the score. Every other, shorter Limit is a "gate" — a rate limiter the vendor uses to smooth load, with no economic value in maxing it out, so a gate can only ever suppress the score toward its own saturation and never boost it above what the bucket alone would give. The provider's score is the tightest (minimum) of all its Limits' scores. With a single Limit (the common case), it's simply the account's bucket and this degenerates to exactly the P1/P2 behavior described above.

Period boundaries (and every other human-facing timestamp) render in the server's local timezone (`vmr check`'s `timezone:` line shows exactly what that resolves to) — a container with `TZ` unset silently uses UTC, which can be several hours off from what you'd expect with no other symptom, so it's worth checking that line once after deploying.

#### Making the numbers precise: `token_weights` and `model_multipliers` (P2.1, per-Limit since P3)

A plain `metric: tokens` Limit counts fresh input, cache-read, cache-write, and output tokens with **equal weight** — accurate for a straightforward "total tokens" plan, but it *overestimates* consumption on a Credits-style plan where a cache hit is billed at a fraction of a fresh token's price (observed in the market anywhere from 5x to 120x cheaper). An account that's actually only 15% through its real budget can show as "exhausted" under equal weighting, get deprioritized, and waste the unused majority of a plan you already paid for.

```yaml
providers:
  - name: plan-d
    quota:
      limits:
        - metric: tokens
          every: 1mo
          amount: 1249000000
          token_weights: {in_fresh: 1.0, cache_read: 0.1, cache_write: 1.25, out: 4.0}
          model_multipliers: {"*": 1.0, heavy-model: 9}
```

- **`token_weights`** rescales a `metric: tokens` Limit's four components when computing headroom and `/admin/status`'s `used`/`pct` — **per-Limit** (each `limits:` entry has its own; a provider with several windows writes it separately on each one it should affect, since observed plans don't always weight every window the same way), defaults to `1.0` on every component you don't mention, and only takes effect on a Limit whose own `metric` is `tokens` (configuring it on a `requests`/`cost` Limit is a load-time error). This is the right tool when the account's discount ratio is **uniform across all its models** — if it instead varies *by model*, use `metric: cost` with a `pricing:` block instead (see below), because a price that differs per model can't be expressed as one shared ratio.
- **`model_multipliers`** scales *every* component of a charge (including `requests`) by which upstream model actually got hit — `"*"` is a wildcard fallback, an unmatched model with no wildcard is unscaled (`1.0`). Also **per-Limit** since P3, same reasoning as `token_weights`. Unlike `token_weights`, this is applied **the moment the charge is recorded**, not when it's later read back — vmr's internal counters aggregate per (provider, Limit), not per model, so there'd be no way to retroactively figure out which slice of a later read came from which model. A non-integer multiplier scales *exactly* — no rounding (e.g. 1.5x of 3 tokens charges as 4.5, not 4 or 5) — since how a real upstream account itself rounds a fractional multiplier isn't observable from here, rounding one way or the other would just be a guess dressed as precision, and the direction it happened to round in the past (up) compounded into a systematic, config-value-dependent overcharge (2.5x → +20% per charge, 4.5x → +11.1%, 2.9x → +3.4% — nowhere near proportional to how far the value is from an integer). `model_multipliers` only affects `requests`/`tokens` Limits; a `cost` Limit's price differentiation comes entirely from its `pricing:` block, and configuring both on the same Limit is a load-time error.

Neither field changes anything for a Limit that doesn't configure it — `token_weights` unset defaults to the exact equal-weighted sum P1 always used, and `model_multipliers` unset leaves every charge at 1x.

**`models:` — three shapes, one field.** `models:` decides BOTH which upstream models a Limit applies to AND whether they share one pool or each get an independent one:

| Write it as | Which models | Pool |
|---|---|---|
| omitted | every model on the provider | one shared pool |
| `["*"]` | every model on the provider | each model gets its OWN independent pool |
| `[a, b, ...]` | only the named models | each named model gets its OWN independent pool |

The distinguishing rule is simply *whether `models:` was set at all* — presence, in either shape, always means independent per-model accounting; only its absence means a shared pool. There's no separate `mode:` field to keep in sync with it.

```yaml
providers:
  - name: plan-with-submodel-cap
    quota:
      limits:
        - {metric: requests, every: 1mo, amount: 50000}                        # account-wide, one shared pool
        - {metric: requests, every: 1d, amount: 200, models: [premium-model]}  # only premium-model, its own independent pool

  - name: plan-per-model-rpm
    quota:
      limits:
        - {metric: requests, every: 1min, amount: 60, models: ["*"]}  # every model gets its own 60/min, independently
        - {metric: requests, every: 1mo,  amount: 90000}              # account-wide, one shared pool
```

`"*"` is a reserved token, not a glob pattern — `models: ["gpt-*"]` matches nothing (there's no prefix matching), and combining `"*"` with a named entry (`models: ["*", "premium-model"]`) is a load-time error, since the wildcard already covers whatever the named entry would add. An endpoint only interacts with the Limits whose Scope covers its own upstream model (unscoped, wildcard, or a matching name) — a Limit that doesn't cover a given endpoint neither charges against it nor constrains its score, the same as if that Limit didn't exist for that endpoint. `/admin/status`/`vmr status` show one row per model that has actually been charged against a per-model Limit — a `"*"` Limit's row count grows as new models send traffic, not fixed at config time.

#### Pricing and cost-metric quota

`metric: cost` charges an account in real money instead of a request/token count — the right choice for a Credits-style plan whose price-per-token differs *by model*, which `token_weights`' single shared ratio can't express. Pricing comes from two layers: a **standard price list built into the binary** (no configuration needed — sourced from a public LiteLLM-format snapshot, MIT licensed, refreshed periodically) and, on top of it, whatever your own `config.yaml` says is different about your account.

**Recommended default: most deployments need none of this.** If you don't use `metric: cost` anywhere (plain `requests`/`tokens` limits don't touch pricing at all), skip the whole `pricing:` block. If you do use it and you're fine accounting in USD, all you need is `pricing: {currency: USD}` — `exchange_rate` stays empty, and every `providers[].pricing.overrides` rate below can just be written in USD directly. Reach for `exchange_rate` only once an account's real quota cap is denominated in a currency other than USD — a domestic vendor's CNY-priced plan, say.

```yaml
pricing:
  currency: CNY # required once any account uses metric: cost; the standard table itself is USD
  exchange_rate: {CNY: 7.1} # a general "1 USD = X <code>" map — required whenever currency isn't USD
  supplement: ./pricing.yaml # optional: your own additions, same shape as the built-in table, merged in (yours wins on a key clash) — see pricing.example.yaml

providers:
  - name: anthropic # naming the provider after its vendor helps auto-resolution — see below
    quota:
      limits:
        - {metric: cost, every: 1mo, amount: 500} # 500 CNY/month
    pricing:
      map: {my-claude-alias: anthropic/claude-3-7-sonnet-20250219} # only needed when auto-resolution can't guess your model name
      overrides:
        - {model: my-model-x, in_fresh: 1.58, cache_read: 0.32, cache_write: 1.58, out: 9.54} # this account's actual negotiated rate, already in CNY (pricing.currency above)
        - {model: my-model-y, currency: USD, in_fresh: 1, cache_read: 0.1, cache_write: 1.25, out: 4} # or: entered straight from the vendor's own USD invoice, converted via pricing.exchange_rate
        - {model: "*", discount: 0.6} # fallback: 40% off list price for every other model
```

**How a model's price is found**: `providers[].pricing.map` first (explicit local-name → standard-table entry), then three automatic steps — `<provider name>/<model>`, the bare model name, or a *unique* match against the standard table's `*/<model>` suffix. If more than one entry could match that last step, vmr refuses to guess rather than risk picking the wrong vendor's price — add a `map` entry to disambiguate. `vmr check` prints exactly what resolved for each provider so this is never a guessing game on your end either.

**`providers[].pricing.overrides`** is a first-match-wins rule list: each entry is either `discount` (a fraction of whatever the layer below it resolves to — the standard table, or another override further down the list) or an explicit four-component rate (`in_fresh`/`cache_read`/`cache_write`/`out`, all four required together — a partial explicit rate is rejected, since "the rest are free" and "the rest are unspecified" are different claims and vmr won't guess which one you meant). No time dimension — list a model-specific rule *before* a `"*"` wildcard fallback, not after: since a rule's outcome no longer depends on when the request happened, a duplicate/shadowed model pattern is always dead config, and `vmr check`/`vmr start`/a hot reload all reject it at load time rather than silently never applying it. An explicit rate can also carry its own `currency:` — handy for entering a rate straight from the vendor's own invoice without hand-converting it to `pricing.currency` first; a `discount` rule never takes `currency:` (it's a dimensionless multiplier, no currency applies to it).

**Missing beats wrong, always**: a `metric: cost` account is rejected at load time — `vmr check`/`vmr start`/a hot reload all catch it the same way — unless *every* upstream model it's configured to serve resolves a complete four-component rate under *every* possible combination of overrides, not just the common case. An explicit `0.0` counts as priced (some components genuinely are free); an omitted one does not — silently treating a missing `cache_read` price as `0` would make the account look cheaper than it really is, route more traffic to it, and overspend. This is the one place vmr refuses to degrade gracefully, on purpose.

**`pricing.supplement` rows can be non-USD-native too** — a supplement/standard-override file (see `pricing.example.yaml`) can give any row its own `currency:`, straight from a vendor's official native-currency price list, with no manual conversion. The rate used to convert it to USD comes from an `exchange_rate:` block that can live right inside the supplement file itself (**recommended**: keeps that file fully self-contained and portable across deployments, and its resolved USD prices never drift just because some other `config.yaml`'s accounting-currency rate later changes for an unrelated reason), falling back to `config.yaml`'s own `pricing.exchange_rate` for any currency the supplement file doesn't declare a rate for.

**Migrating from the old `pricing.yaml` sidecar** (a pre-P2.2 format, now removed — `-pricing` is no longer a recognized `vmr report` flag; unrelated to today's `pricing.supplement: ./pricing.yaml` above despite sharing a filename — the old sidecar's shape (`in_fresh_per_1m`/`date_range`/`updated_at`, etc.) is not the new supplement's shape): each `rates:` entry becomes a `providers[].pricing.overrides` entry on the matching provider, with an explicit four-component rate (its `in_fresh_per_1m`/`cache_read_per_1m`/`cache_write_per_1m`/`out_per_1m` map directly to `in_fresh`/`cache_read`/`cache_write`/`out`). Its `date_range`/`hour_range` pairs have no equivalent anymore (time-scoped promo/off-peak windows were dropped as a complexity/value mismatch — see `docs/VirtualModelRouter_Design_v4_Quota.md`'s pricing sections) — drop them and keep only the row whose window covers your actual current rate. The old file's top-level `currency`/`exchange_rate`/`updated_at` become the new global `pricing:` block's `currency`/`exchange_rate` (`updated_at` has no equivalent — the standard table carries its own generation date instead). Many rows can simply be **deleted** once you check whether the standard table already covers that model at an acceptable price — the whole point of the built-in table is to make most of that file unnecessary.

`vmr report`'s $ estimates use the same two layers, resolved independently at report-generation time from whatever `config.yaml` is reachable via its `-c` flag (default `./config.yaml`) — no config.yaml in reach degrades gracefully to standard-list-price-only. Its `metric: cost` account resolution and `vmr report`'s own resolution deliberately do NOT require the same completeness — a `vmr report` run degrades a missing price to "no $ column for that row," not a hard error, since report's whole design philosophy is that a pricing gap must never cost you the rest of the report. See [Cost estimate and pricing](#cost-estimate-and-pricing) below for `vmr report`'s own display-currency option, independent of the accounting currency above.

Full design, including everything staged for later batches (multi-window accounts, rolling windows, official usage-API calibration): `docs/VirtualModelRouter_Design_v4_Quota.md`.

## Audit and reporting

### The audit log

On by default: one JSONL line per request with both layers (client↔vmr and every vmr↔upstream attempt), credential-masked headers, applied normalizations, and inline request image metadata (format/dimensions/bytes, plus downscale/cache-hit outcome — captured for every image regardless of whether downscaling is enabled). Bodies are recorded in full, whatever size vmr accepted — there is no separate audit-side truncation cap (`max_request_body_mb` above only bounds what vmr accepts from the client in the first place). Each upstream attempt carries both a human-readable `endpoint` label (`protocol:provider:model`) and the same three fields structured (`protocol`/`provider`/`model`), plus a typed `error_class` alongside the free-text `error`. Credential masking covers `Authorization`/`X-Api-Key`/`Api-Key`/`X-Auth-Token`/`Cookie`/`Set-Cookie`/`Proxy-Authorization` out of the box; a client sending its own custom auth header (e.g. `X-Custom-Token`) under a name vmr doesn't already know needs `extra_redact_headers` (see [Config layout](#config-layout) above) to also get masked instead of landing in the audit file in cleartext.

Each record also carries a `facts` object — vmr's own pre-routing read on the request (`has_image`/`has_tools`/`estimated_tokens`), the exact same values the router used to pick an endpoint, recorded as-is rather than recomputed from the stored body. It's a sibling of the request, not part of it, so the recorded request itself stays byte-faithful to what the client actually sent. Absent entirely (not a zero-valued object) on a request rejected before routing ever ran — bad auth, unparseable JSON.

```bash
./vmr start -c config.yaml                 # writes to config's log_dir (`vmr check -c config.yaml log` to check)
./vmr start -c config.yaml -audit=false    # off
jq '.model, .outcome, .attempts[0].norm' vmr-audit-2026-07-08.jsonl
```

### Usage and cost reports (vmr report)

`vmr analyze` (below) is the recommended entry point — it runs everything this section describes plus [Agent task narratives](#agent-task-narratives-vmr-story) from a single call, sharing one output directory. `vmr report` itself remains fully supported for when you only want the macro report on its own — this section documents what it produces either way, since the two paths render byte-identical output.

```bash
./vmr report "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"   # → vmr-report.json + vmr-report.md + vmr-requests.json (plain + .zst mix ok)
./vmr report                                                          # same, with no glob at all — see below
./vmr analyze                                                         # the report above, PLUS task journeys, in one call — see below
```

**No input files needed for the common case.** `vmr report`, `vmr story`, and `vmr analyze` all accept zero positional arguments: omit the glob entirely and they resolve it themselves from `-c config.yaml`'s own `log_dir` (`<log_dir>/vmr-audit-*`, matching plain `.jsonl` and compressed `.jsonl.zst` alike) — loading the config only to read that one field. So reporting on the instance you're already running is just `./vmr report` (or `./vmr story`, or `./vmr analyze`), full stop; the explicit `$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*` form above is for pointing at a *different* directory (another instance's logs, an archived set of files) or a custom glob.

`vmr report` aggregates tokens *and* bytes (bytes as the fallback when a provider omits usage).

#### Report sections

The Markdown groups into nine numbered sections, each answering one operator question. §2.5 and §5.5 below are enhancement sub-sections nested inside their neighboring numbered section, not sections of their own — the count stays nine.

- **§0 Summary** — headline numbers plus up to 3 auto-generated highlights (low cache efficiency, wasted tool schemas, a misbehaving endpoint).
- **§1 Cost & Token Economy** — cache-hit/fresh/cache-write/reasoning breakdown, per-model cache efficiency, per-role message-character/estimated-token share.
- **§2 Cost estimate** — only rendered once pricing data actually resolves something (see [Cost estimate and pricing](#cost-estimate-and-pricing) below); one table each by model/endpoint/client, plus a summary of which pricing sources fed those numbers (standard table generation date, supplement, override count).
- **§2.5 Provider spend & quota** — a cross-model roll-up per upstream account (config.yaml's `providers[].name`): tokens, cache efficiency, mean latency, reliability (including its dominant error class, e.g. `rate_limit 12(63%)` — is this account hard-quota-exhausted or just being rate-limited), and $ estimate. Useful especially for Token Plan/AFP-style accounts with no resolvable $ price: even with no $ figure, you can still compare raw token consumption against the configured quota's magnitude via the sub-table below it. The main table itself carries no quota column — a declared `quota:` only ever appears in the **"Quota vs. Consumption" sub-table**, which lists every account with a declared `quota:`, placing this report run's own recomputed window consumption next to the router's real-time `<log_dir>/vmr-quota.json` counter (used%/period-elapsed% side by side, plus what share of that period's consumption came from a degraded estimate rather than exact usage) — the two consumption figures are two different time windows shown deliberately unsubtracted; the live column shows `-` when the on-disk counter is still stuck on an earlier period. The recomputed column's accuracy differs by metric, and the table's footnotes say which: for `metric: requests` it reproduces the router's charging exactly (it counts forwarded upstream responses, which is precisely what the router charges on — not client requests, since a request whose every attempt failed was never charged); for `tokens` and `cost` it is an estimate with named drift sources, and it renders `-` rather than a misleading `0` when traffic existed but nothing about it was computable (no parseable usage, or no resolvable price).
- **§3 Reliability** — endpoint availability/error-rate, error-class breakdown, and quirk-fix breakdown (how often each endpoint's successful responses needed a think/thinking-process strip or hit a soft-block detection — the same steps each request's own detail page narrates individually, here as a cross-request frequency count so a pattern doesn't require opening thousands of detail files to notice), each split into one table per ingress protocol actually in use (openai first, anthropic second, any other alphabetically) since every protocol surface routes independently, plus an hourly error-count chart.
- **§4 Latency & throughput** — ttft/duration percentiles by model and by endpoint, both sorted by throughput descending, each with its sample size and a `⚠️low-n` flag under n=20.
- **§5 Workload Distribution** — by virtual model, by workload class (interactive vs. scheduled scaffolding), by endpoint, and by client (the latter two also carrying per-request input/output token percentiles), plus hourly and daily request-volume/input-token Mermaid charts.
- **§5.5 Per-Client Upstream Attribution** — which upstream endpoints (`protocol:provider:model`) each client actually hit and how many tokens landed on each — grouped by client, token-descending within each group, answering "where does this agent's traffic actually land" (the same model name can sit under two different accounts, so grouping by model name alone would erase exactly the cut that matters here).
- **§6 Sessions & tasks** — interactive sessions only, grouped by Chat User (single-shot scheduled sessions live in the request index instead — see [Index files](#index-files) below). §6.5 (sticky effectiveness) and §6.6 (endpoint value) are covered under [Agent-aware analysis](#agent-aware-analysis) below; §6.7 (compaction) covers every standalone history-compaction LLM call this period — which sessions it links, tokens in→out, retention ratio, and a rule-based sample of what got swallowed (no LLM, just the observable facts).
- **§7 Efficiency & waste** — auto findings, plus the full used/never-called tool breakdown per declared-tool shape (see [Agent-aware analysis](#agent-aware-analysis) below).
- **§8 Request index** — a pointer to `vmr-requests.md` (see [Index files](#index-files) below).

Every table stays to a handful of columns; percentiles are true per-bucket values — each bucket keeps its own raw samples and computes p50/p95 directly during the single pass, never derived by re-merging other buckets' already-finalized percentiles (there'd be nothing left to recompute from). An `⭐` marks any column that's a derived/estimated metric rather than a raw upstream value. Hourly/daily activity and the hourly error count render as Mermaid `xychart-beta` charts.

Run progress goes to stdout with a `yyyy-MM-dd HH:mm:ss.SSS` timestamp on every line, so each phase's real cost is visible: session analysis runs first (parallelized across input files — the single largest phase on a busy multi-day corpus — and silent, no per-file lines of its own), then one combined pass does aggregation and detail export together, printing one `[i/N] <path>  done: M records (Ts)` line per file — detail rendering runs on its own worker pool concurrently with the file scan that feeds it, since a record's detail page depends only on its own content, never on anything accumulated from other records. The JSON (`vmr-report.json`) is the data source for any dashboarding you want to build on top — anything the Markdown only summarizes or truncates (e.g. Top-5 tool shapes) is present there in full.

#### Cost estimate and pricing

`vmr report` (`-c config.yaml`, the same flag it uses to find `log_dir`) resolves $ estimates from the same two-layer pricing model `metric: cost` quota uses — the binary's built-in standard price table plus whatever `providers[].pricing`/global `pricing:` your `config.yaml` declares — see [Pricing and cost-metric quota](#pricing-and-cost-metric-quota) above for the full configuration shape (`map`/`overrides`/`discount` all apply here identically). No `config.yaml` in reach degrades gracefully to the standard table's list prices only, no account-specific overrides — it never blocks the rest of the report. Unlike a `metric: cost` quota account (which is rejected at load time if any of its models resolves an incomplete rate), a report row with an unresolved or partial price simply doesn't get a $ column — report's own philosophy is that a pricing gap costs you one number, never the report.

**Display currency, independent of accounting currency.** `-currency CODE` (or `report.yaml`'s `currency`) picks what the report's $ column is actually shown in — e.g. run with `pricing.currency: USD` for accounting but hand someone a report in `-currency CNY`. This is a purely cosmetic final rescale (`internal/pricing.Resolver.WithDisplayFactor`), applied after every number has already been computed in the accounting currency — it never changes what a `metric: cost` account was actually charged. The rate it needs comes from `config.yaml`'s `pricing.exchange_rate` and/or `report.yaml`'s own `exchange_rate` (same "1 USD = X `<code>`" shape — see `report.example.yaml`), the latter winning on a matching key; this is also the only way `-currency` works with no `config.yaml` reachable at all, since `report.yaml` is designed to stand entirely on its own. A `-currency` value with no resolvable rate degrades to showing the accounting currency instead, with a warning — never a hard error.

#### Agent-aware analysis

`vmr report` also understands agent workloads — offline and purely rule-based, no LLM involved (method and evidence: `docs/VirtualModelRouter_Design_v4_Analytics.md`'s `AnalyzeSessions`+`Build` two-pass read section):

- **Session → task → turn grouping.** Requests resending the same growing conversation are fingerprinted (first non-system message; Claude Code's `metadata.user_id` when present) and chained by longest-common-prefix, so concurrent agent sessions untangle even when interleaved in time. Task boundaries come from Traceparent trace-id changes and new user instructions in the delta — cross-validated signals. Compaction calls are detected and linked both ways, so a session and its post-compaction continuation form one thread.
- **`vmr-requests.json`** — a `requests` field with one feature line per request (session/task/turn, trace and chat ids, request shape, tags like `heartbeat`, per-turn tool calls, finish_reason, ok-but-truncated flag, token splits incl. reasoning tokens, delta size, latest instruction), ready for jq/DuckDB/pandas (was one-JSON-object-per-line `vmr-requests.jsonl` before this became a single document), plus a `files` field — a file-hash-keyed cache of already-scanned input files, so a repeat `vmr report` run against a mostly-unchanged log directory skips re-parsing the unchanged ones (same mechanism and rationale as `vmr story`'s `vmr-stories.json`, see below).
- **Sticky effectiveness (§6.5) ⭐** — Sticky Model exists for exactly one reason, keeping an upstream prompt cache warm; this section is the evidence that it works. Within a session, requests that landed back on the *previous request's* endpoint are compared against those that switched, on cache efficiency. Measured by outcome (endpoint continuity), not by mechanism — a sticky pointer that fired but landed cold still counts as a switch. A session's first request has no predecessor: counted, but in neither group. Under 20 usage-bearing samples in either group the tables still render but no verdict is drawn. **It does not explain why a request switched** — sticky TTL expiry, a health cooldown, a condition eliminating the sticky pick, or sticky being off for that model are indistinguishable after the fact. A second table splits by virtual model, since that is the level `sticky` is configured at.
- **Endpoint value (§6.6) ⭐** — not "what did this endpoint cost" (§2 answers that) but "what did it cost per unit of work, and how long did its failures make you wait": cost per 1M output tokens, cost per successful request, failed attempts, and **wall-clock spent on those failed attempts**. An endpoint that is cheap per token but fails often is not cheap — and a per-endpoint spend column can't show it, because the money lands on whichever endpoint eventually succeeded. **Time only, never money**: a failed attempt carries no usage and providers generally don't bill for one, so attaching a currency figure would be an invention.
- **Tool usage report (§7)** — per declared-tool shape: declared tools vs. tools actually called *this turn* (extracted from the response, so history repeats are never double-counted), plus the "declared but never called" list — both folded into a `<details>` block per shape (numbered, alphabetical) so a 60-plus-tool schema doesn't blow out the document — together with the complete tool declaration's per-request byte cost: the direct input for pruning unused tools from an agent's config.

#### Per-request detail files

`vmr report` can also render every record as one human-readable Markdown file under `{out}/details/`, for drilling into a single request: a header line locating it (trace · chat user · tools — values in **bold**), a `VMR pre-routing judgment` block reading the same `facts` object described above — only the capabilities actually detected (`image` and/or `tools`), each rendered as a small backtick-quoted tag (`none` when neither), plus the estimated token count — omitted entirely when the record has no `facts`, then the **full message list** with each message folded uniformly and new messages marked with a 🆕 prefix, the increment summary at the end (`🆕 This turn's increment (vs. the previous turn, +N, #1–#M are prior context)`), then every upstream attempt with a full side-by-side listing of headers and body fields where changes are emoji-marked (🟢 added / 🔴 removed / 🔶 changed) — including, when a `<think>…</think>` block was stripped, the full pre-strip content and its raw SSE (captured going forward only; older logs show a "not captured" note instead), and the client response with the SSE stream reassembled into the actual model output next to the raw event log. Filenames start with a zero-padded timestamp, so name order is time order.

Detail rendering is off by default — a full run over a large corpus would otherwise write out several times its own source data in derived Markdown, most of which never gets opened. Pass `-details` to render every record's page up front. Either way, the requests index below always links to each record's detail filename (it's computed from the record's own timestamp/model/outcome and a short content hash, not read off an existing file — see `vmr replay -req COORD -print` for reading a record's raw JSON directly out of the audit log without rendering anything at all). With `-details` off, a link's target won't exist on disk until you rerun with `-details`.

#### Index files

`vmr-requests.md` (alongside `vmr-report.md`, one level above `details/`) is a pure index: one `## Chat User: <key> · N sessions N tasks N turns` (or `## Scheduled · <class> single-shot sessions × N`) entry per group, each with a one-line summary blockquote and a link to that group's own fully-detailed sibling file. The actual **Chat User → Session → Task → Turn** drill-down — each session a `## l-<hash8> (sNN) · <ts> · N tasks N turns` heading (the session's stable, content-addressed id, joinable against `vmr-stories.json`'s `journeys[].lineages`; `sNN` alongside it is a purely positional label for at-a-glance reference within this one report, not an identity — see [Agent task narratives](#agent-task-narratives-vmr-story) below), with a "→ Task narrative in ..." link right underneath when `vmr story` has already rendered a journey for it in this same output directory, each task a `### tNN · <ts> · N turns` heading with its opening message as a quote block and a turn table (`Turn / Time / msgs / finish / dur / ttft / fresh/cached/out / cache-eff⭐ / File`; every timestamp in the local machine's system default timezone regardless of the source record's own offset) — lives only in that sibling, never duplicated in the index. Siblings: `vmr-requests-<tag>.md` per real `client_key_tag`, `vmr-requests-unresolved.md` for sessions carrying none, `vmr-requests-cron-<class>.md` per scheduled class (`heartbeat`'s file is `vmr-requests-cron-hartbeat.md` specifically; any other scheduled class follows the same `-cron-<class>` pattern). A single-shot scheduled session (heartbeat/dream_diary — exactly one request, no real back-and-forth) always belongs to its class's cron sibling regardless of which client issued it, so near-identical poll turns never drown a real conversation and never appear under two different groupings; a scheduled session with more than one turn (an actual multi-step cron job) is a normal session card under its own caller instead. The index's own flat `# All Requests (chronological)` table at the end still covers every record regardless of grouping. Every "File" column links to the record's Markdown detail file (present only when `-details` was passed for this run — see "Per-request detail files" above).

**Error/truncation index (`vmr-requests-failed.jsonl`/`.md`)**: every `vmr report` run also writes a flat, time-ordered view of just the requests worth investigating — `outcome == error`/`canceled`, plus any `ok`-but-truncated response — so a "what actually went wrong" pass doesn't mean paging through everything that succeeded first. Purely additive: it never removes those requests from `vmr-report.md`, `vmr-requests.md`, or their session groupings, it's a second, filtered way to reach the same records. `.md` is the human-readable index (same one-line-summary-plus-link shape as `vmr-requests.md`'s siblings); `.jsonl` is a flat one-row-per-line dump with no session/task grouping and no file-cache section of its own, for `jq`/scripting.

#### Output language

`vmr report`/`vmr story` render in English by default. Drop a `report.yaml` (`language: zh`) in the current directory to switch to Chinese, or pass `-lang en|zh` on either command to override it for one run — `-lang` always wins over `report.yaml`. `report.yaml` is its own small file, entirely separate from `config.yaml`: it's optional, and is auto-loaded from the current directory if present (`-report-config path` to point at a different one). It can hold a real secret (`llm_key`, see below), so like `config.yaml` it's `.gitignore`'d — `report.example.yaml` in the repo root is the committed template to copy from. This changes the Markdown documents AND `vmr-report.json`/`journey-*.json`/`compare-*.json` alike — every narrative field in them (e.g. `efficiency[].finding`, `compare-*.json`'s `rows[].label`) follows `-lang` the same way the Markdown does. `FindingCode`/`MetricCode`/`EvidenceAnchor` are the stable, language-independent fields a script should key off of instead — they never change with `-lang`, unlike the narrative text next to them.

`report.yaml` isn't just language: `-o`/`-details`/`-include-partial`/`-currency`/`-llm-addr`/`-llm-model`/`-llm-key`/`-llm-cache-dir` all have a matching default field there too (`currency`/`exchange_rate` — see [Cost estimate and pricing](#cost-estimate-and-pricing) above), and an explicit same-named flag still wins — see `report.example.yaml` in the repo root for the full annotated list. `llm_key` can be plaintext (this file is `.gitignore`'d) or a `${VMR_LLM_KEY}`-style reference to an existing environment variable, whichever is more convenient; `llm_cache_dir` has no implicit default path — leave it (and `-llm-cache-dir`) unset anywhere and LLM interpretation results are never cached.

#### Multiple callers, one instance

If several clients share one vmr (a teammate, a second agent, a CI job) and you want their usage told apart after the fact, give each one its own entry under `api_keys` (see [Config layout](#config-layout) above) instead of sharing one key. Every request tags its audit record with that key's own tail (`client_key_tag`, via `KeyTag`: last 8 characters, then — if that window contains a `-` — only what follows the last `-` inside it, so a key ending in `...-alice` reads back as `alice`; keep the meaningful part ≥3-4 characters to avoid two callers' tags colliding). `vmr report` picks this up automatically, no flag needed: for every distinct tag it observed, it writes a `vmr-requests-<tag>.md` detail sibling (see [Index files](#index-files) above) — same directory, so its `details/…` links need no adjusting. `vmr-report.md`/`.json`, `vmr-requests.json`, and `details/` itself are never split or duplicated: a request's detail pair is written once regardless of caller, `vmr-requests.json` always covers every record, and the aggregate report always covers everyone. Nothing changes at all — no extra files, no new columns — until `api_keys` is actually configured. Full design rationale: `docs/VirtualModelRouter_Design_v4_Analytics.md`'s per-request detail files and index section.

Don't want real auth at all (a trusted private network)? Leave `api_keys` unset — the door stays fully open — but a client that voluntarily sends *any* Authorization/x-api-key value still gets it tagged the same way, no vmr-side config needed: just end each client's own value in `-<label>` and it self-identifies to `vmr report`. No 16-character minimum applies in this mode (there's no secret to protect); a client sending nothing still gets an untagged record.

#### Retention and compression

Agent workloads resend the full conversation on every turn, so a day's log can run into gigabytes — mostly repeated across lines, not within one. Each day's file rotates and compresses automatically once it's no longer "today": zstd on the whole file (not per-line) catches that cross-line repetition, typically 20–75× smaller in practice — far beyond what compressing each record on its own could reach, since a single record never sees the previous turn's near-duplicate body. `vmr report` reads `.jsonl` and `.jsonl.zst` interchangeably, so point it at a glob covering both. Set `audit_retention_days` to also delete files past a given age (default: keep forever — nothing is deleted unless you opt in); either way, deletion and compression are both keyed off the date in the filename, so housekeeping never needs to scan or `stat` the whole log directory. Details and the numbers behind this: Part 1 §9.5 of the design doc.

**Don't point two vmr instances at the same `log_dir`.** Each instance's housekeeping sweep decides a file is done for the day (safe to compress and, once past retention, delete) purely from the date in its filename — it has no way to know another process is still appending to that same file. Two instances sharing a `log_dir` and both still running past midnight is the one scenario where this bites: instance A rotates to today's file, sees yesterday's file as "done", and compresses-then-deletes it while instance B (still on yesterday's date, or just slower to rotate) is still writing to that exact inode — B's writes land in a file that no longer exists on disk. Give every instance (including a second local checkout for testing) its own `log_dir`.

### Agent task narratives (vmr story)

Where `vmr report` answers "how much did everything cost, overall" — `vmr story` answers "what actually happened in this one task, step by step." It reads the same audit JSONL, but instead of aggregating across every request, it reconstructs a single agent task's full execution history: what context entered at each turn, what the model did with it, and (when it happened) what a history-compaction event lost. As with `vmr report`, `vmr analyze` is the recommended entry point for this — every `-journey`/`-compare`/`-corpus`/`-render-all` example below works identically as `vmr analyze <same flags>`, zooming into that one view without also running the macro report; `vmr story` itself remains fully supported.

```bash
./vmr story "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"           # list candidate tasks (vmr analyze has no equivalent bare listing mode — see below)
./vmr analyze -journey j-agent-20260716T152238-20260716T153122-42f908fa \
    "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"                   # render one, by id (a prefix is enough) — vmr story -journey works identically
./vmr analyze -journey 'j-agent-*,j-openclaw-*' \
    "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"                   # render every match of either pattern, batched
./vmr analyze -render-all "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"   # render every candidate (task + cron + heartbeat + subagent), including the default suite's excluded heartbeat rows
./vmr analyze -corpus "$(./vmr check -c config.yaml log)/vmr-audit-*.jsonl*"       # corpus-level statistics across every candidate
./vmr analyze                                                                       # default suite: macro report + every non-heartbeat journey, in one call — no glob needed
```

`vmr story` with no `-journey` lists every candidate task with its id, task/turn counts, time range, and a title preview (the opening real instruction) — pick one and pass it to `-journey` (`vmr analyze -list-only` is the equivalent: its own bare invocation renders the default suite instead, see [CLI and endpoint reference](#cli-and-endpoint-reference) below). `-journey` takes a comma-separated list of tokens, each either an id/id-prefix or a shell-style glob (`*`, `?`, `[...]`) matched against the full id — quote it if your shell would otherwise expand the glob itself. A selector resolving to exactly one journey renders it directly (the only form that accepts `-llm-addr`); one resolving to more than one batches through the same pass `-render-all` uses, sharing the underlying file scan instead of re-reading the source files once per candidate. Output goes to `{out}/stories/journey-<id>.md` (the narrative) plus `journey-<id>.json` (the same task's behavior profile, see below) — same 0600/0700 permissions as `details/`, since both carry full conversation content. `-show-ungrouped` prints the source location of the first few records that didn't fingerprint into any session — a triage aid when the candidate list looks shorter than expected.

Every invocation — with or without a selector flag — also writes `{out}/stories/vmr-stories.json`/`.md`: a candidate index (same fields as the terminal listing above, plus which files each candidate's chain touches and, once rendered, a link to its `journey-<id>.md`) that persists what used to only ever print to stdout. `vmr-stories.json`'s `files` field doubles as a file-hash-keyed parse cache — an unchanged input file's already-parsed data is reused instead of re-scanned, so a repeat run against a mostly-unchanged log directory is substantially faster; a changed or brand-new file is always reparsed, and the resulting Journey graph is always rebuilt from the complete file set either way — nothing is silently skipped, only the expensive parsing step is. Design: `docs/VirtualModelRouter_Design_v4_Analytics.md`'s `vmr-stories.json` section.

A task whose own beginning looks like it continues a conversation that started outside the files you gave it (a real, multi-turn-looking opening sitting at the very start of your earliest input file) is skipped by default — its id isn't stable across different file-loading ranges. Pass `-include-partial` to render it anyway; its filename picks up a `-partial` suffix as a visible reminder that the id may change next time you load a different range of history.

#### Reading the narrative

Each task is split into a sequence of turns (`Step`), grouped under the user instruction that started them (`Task`). Every turn shows **Messages** (what's new in context this turn) separately from **LLM Response** (what the model itself produced — reasoning, reply text, and each tool call's full arguments), so "what the agent was given" and "what the agent decided to do" never blur into one generic history view. A history-compaction event (detected structurally, not by matching a specific agent framework's marker text — see the design doc for why) renders as its own annotated boundary: tokens before/after, and which file paths/URLs mentioned just before the compaction never got mentioned again afterward. Nothing here is invented — every number traces back to a request's own recorded usage, and every "swallowed" entity claim is a plain substring check you can verify by opening the folded original content next to it.

#### Decision spine and suspected issues

Each rendered Journey opens with a system-prompt header (each distinct version's effective Step range and a link to its shared evidence blob under `evidence/`, not the full text inline), then an overview card (start/first-error/first-transition/end, plus coarse structural tags like "tool-intensive" or "retry-heavy"), then a small **Model Usage** table (steps and tokens per upstream model/account actually used) plus a switch log (rendered only when a switch actually occurred — which step, from what to what, and whether it happened to land on a step that also triggered a failover), then the decision spine — the report's only per-Step content layer (there is no separate turn-by-turn narrative below it): one block per Task, one sub-block per Step, each carrying that Step's own stated reasoning/reply, every tool call complete with its arguments and paired result, a "→ detail" link to that Step's own full record (rendered on demand — no need to separately run `vmr report -details` first), and — where applicable — the cross-record facts a single record can't show on its own (an edit reclassification other than the routine case, a stitch/compaction boundary, a system-prompt change, a turn the model skipped replying to), with a ⚠️ marker on any Step a rule detector flagged. Each `Step` heading also carries a one-glyph role tag (🔧 action / 📋 plan / 💬 report / 👀 observe / 🔄 retry / ⚠️ error / 🧹 compaction), and a Journey with any tool calls gets an ASCII tool-call timeline at the end (one row per tool, one column per Step) for spotting a retry burst that a linear read would miss. A **Suspected Issues** section lists what those rule detectors actually found — nine zero-LLM-cost, pattern-matching checks (repeated identical tool calls, narration without follow-up action, an error never re-verified, reasoning that names something the next tool call doesn't touch, a stated plan whose later items are never executed, a retry that repeats the exact same failing arguments, a tool result whose entities are never referenced again, a reference kept alive after its source result looked like a failure, and constraint text a compaction boundary silently dropped). Every finding is phrased as "detected N suspected occurrences, recommend manual review," never as a verdict — these are a candidate list for a human to check, not an automated root-cause conclusion.

#### Behavior profile

`journey-<id>.json`, written alongside the `.md`: nine rule-derived, zero-LLM-cost numbers — model time / agent-side execution time / human-idle time split, tool-call distribution, duplicate-action rate, error-recovery count, plan-vs-execute ratio, a per-step context-composition curve (token share by role at each turn, so a context budget's makeup over the task is visible), context-utilization rate (how much of what entered the context ever got referenced again), and compaction count/loss — plus one list-typed addition, **model usage & switches**: which upstream models/accounts this Journey actually used, each one's step count and tokens, and every point where the upstream changed (which step, from what to what — read from the actual request's upstream endpoint, not the client-facing virtual model name, which barely ever changes within a single Journey). These are the same numbers whether the underlying agent is Claude Code, OpenClaw, or anything else — cross-framework comparison was the whole point of collecting them. It also carries a `structure` field: the full Task/Step/Event/ToolCall skeleton — each Step's request-level coordinate (`req`), per-step timing/cost (endpoint, duration, TTFT, token usage), the same graph-level analysis facts the decision spine shows (edit classification, stitch evidence, compaction token/entity counts), and this turn's own decision content (a bounded excerpt of its reply/reasoning and its tool calls' arguments, each paired with whether a matching result was found). Conversation-history messages are references only — a content hash, role, and which step first introduced them — never inlined text; the actual text lives at the audit record a Step's `req` coordinate points to (or that record's rendered detail page under `details/`), the same way a tool call's own result text lives in the *next* Step's referenced messages rather than being duplicated onto the call itself.

#### Comparing two tasks

`-compare <id1,id2>` diffs two tasks' behavior profiles directly (no separate `-journey` render needed first) and writes `compare-<id1>-vs-<id2>.md` + `.json` next to the individual task files. Each row shows both values and the relative change, with a rule-based ⚠️ flag on differences large enough to be worth a second look (a fixed threshold, not a judgment call) — useful for "did switching agent frameworks/prompts actually change how this task got done." The report also includes, purely rule-derived (no LLM needed): the endpoint(s) each side actually used, a per-round prompt-cache hit-ratio curve, each side's effective system-prompt size/stability (with a bounded excerpt — 20,000 characters by default, large enough to cover where "which project context files got loaded"-style declarations actually sat in this project's two real validation Journeys, but it's still a from-the-start prefix, not a guarantee for every possible system prompt), the final round's context composition by role, total wall time next to the existing "net working time" (never presented as an efficiency number on its own), how each side terminated, and — if the task's output was produced via a file-write-shaped tool call — the final deliverable's own content side by side. Two Journeys that share a comparable opening (same task, re-run with a different prompt/model/framework) also get a rule-derived **divergence point**: the first position where the two sides' tool-use structure first differs (a different tool chosen, or the same tool with different arguments), tagged light or heavy severity. It's a structural fact only — "the two runs first differed here" — never a claim about which side did better or why; a trailing disclaimer says so explicitly in the rendered report. A trailing "Evidence Provenance" section lists the source audit file path(s) this comparison actually read, for independent verification.

#### Corpus-level statistics

`-corpus` extends the same rule-derived facts from "two Journeys" to "every candidate Journey found in the input files" — metric distributions (mean/median/min/max/p90 for each of the fourteen behavior-profile numbers `-compare` diffs, including output repetition rate), Finding hit rates, pairwise Spearman rank correlations between metrics (effect size only, no p-values — the corpus sizes this runs on can't support a real significance test), a Finding-grouped comparison (journeys that hit a given Finding code at least once vs. those that didn't, compared on net-working-time median), token context rot analysis across context size buckets (step count, finding density, and error rate by token tier), and frequent 2-gram / 3-gram tool sequence patterns with tail-step error rate attribution. Output goes to `{out}/stories/vmr-story-corpus.md`/`.json`, sharing the same batched file scan `-render-all` uses. Like `-compare`, it carries no success/failure label — VMR has no signal for whether a task was actually accomplished, only rule-derived proxies. `-corpus` doesn't accept `-journey`/`-render-all`/`-compare`/`-llm-addr`.

#### Optional LLM interpretation

Pass `-llm-addr host:port -llm-model name` (an already-running VMR instance's address and its exposed virtual model name — never auto-started; `-llm-key` if that instance requires auth) on a single-match `-journey` or on `-compare` (not `-render-all`/`-corpus`, nor a `-journey` selector matching more than one journey — each Journey would cost its own LLM call, and that cost should stay opt-in per run) to append a clearly-labeled, always-optional interpretation section written by that model. On `-journey`, it first runs up to six semantic defect detectors on bounded transcript excerpts (tool result misinterpretation, semantic oscillation, long-term goal drift, compaction constraint dropped, plan-execution misalignment, and unverified completion claims); findings with HIGH confidence and verbatim evidence anchors are promoted into the decision spine and findings list with `[AI Inferred · HIGH]` badges (and saved in `journey-<id>.json`'s `llm_findings`). It then synthesizes the journey into an overall narrative interpretation prioritizing suspected issues and tool-call patterns. Single-journey analysis makes up to 7 serial LLM calls (cached via `-llm-cache-dir`). On `-compare`, it's a headline sentence, a "candidate root cause | direct evidence | confidence (high/medium/low) | suggested fix" table plus a one-sentence causal chain, a narrative reading of the per-round tool-call sequence, and an explicit "what VMR can't see" caveat — and, when a divergence point was found, a second, separately-cached call scoped to just that point's own evidence window, restricted to labeling *why* the two sides may have diverged as a confidence-tiered guess and never ranking which side did better. The confidence tiering is fixed in the prompt: only a candidate that points at a specific anchor in the evidence table or excerpt may be tagged "high"; anything resting on elimination or intuition alone must be honestly tagged "low" (and still listed — low confidence isn't a reason to omit it). It's fed only the rule-derived facts above plus bounded text excerpts, never the full transcript, and is prompted not to invent any number outside what it's given, nor to treat "not mentioned in this excerpt" as proof of "doesn't exist" beyond the excerpt's boundary. Add `-llm-dry-run` to just print the evidence-pack size estimate and exit without calling anything. Results are cached under `stories/.llm-cache/` (keyed by the journey id(s), the evidence, and the model — switching `-llm-model` never reuses another model's cached answer); any failure (unreachable address, non-2xx, etc.) only skips this section and prints a warning — the rest of the report is unaffected.

Full design (the content-addressed model behind lineage/compaction detection, the fourteen behavior-profile metrics + model usage/switches, known blind spots): `docs/VirtualModelRouter_Design_v4_Analytics.md`.

## Image downscaling

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

### Per-model override

Any virtual model can set its own `image_downscale`, which always wins over the global value; omitting it inherits the global setting. `image_downscale: 0` on a model is an explicit "off" — even with the global setting on — because "not set" and "set to 0" mean different things (inherit vs. force-disable).

### Downscale result cache

The first time a given source image is downscaled to a given target size, the result (JPEG bytes) is cached on disk keyed by **content hash plus target size** — the filename is `<sha256-of-original-bytes>-<maxPx>.jpg`, so the same image downscaled to 512px and 256px (different per-model overrides) are two independent entries that can never collide — under the configured `image_cache_dir` (see below). A later request for the same image reuses the cached bytes verbatim instead of decoding/scaling/re-encoding. Two reasons this matters: it saves CPU (agent workflows resend the full conversation, images included, on every turn), and it protects the upstream's own prompt cache — which is keyed on exact byte/token match, so re-encoding the same image on every request can produce subtly different output bytes and silently defeat that cache, while identical cached bytes always hit it. Entries are evicted by last-hit time (`image_cache_ttl_days`, default 7 days; a hit refreshes the clock, so an image reused throughout a long conversation is never evicted mid-session) alongside a default 50MB total capacity cap (evicting oldest entries first if exceeded), swept lazily off normal cache access rather than a dedicated timer.

### Where the audit and cache directories land

Both are config fields —

```yaml
# log_dir: ~/.vmr/logs                  # audit JSONL directory; used exactly as given (~/ expands); changing it needs a restart
# image_cache_dir: ~/.vmr/image_cache   # downscale-cache directory; same rule; follows hot reload
```

— used exactly as given when set (a leading `~/` expands to the home directory), else the persistent `~/.vmr/logs`/`~/.vmr/image_cache`, else (no resolvable home directory) a `vmr_logs`/`vmr_image_cache` subdirectory of the system temp dir, else `./logs`/`./image_cache` next to the binary. Persistent by default on purpose: macOS purges temp-dir entries not accessed for ~3 days, which would silently delete audit data — the only data source `vmr report` has. Run `vmr check -c config.yaml log` / `vmr check -c config.yaml cache` to see the resolved path without starting the server (also printed by plain `vmr check` and the startup summary). `vmr.sh` queries `vmr check log` itself for the server-log path rather than guessing, so a launchd/systemd-supervised vmr never disagrees with a manually-started one about where its data lives. Neither directory has an environment-variable override — reference `${VAR}` inside `log_dir`/`image_cache_dir` if you want a value from the environment.

## CLI and endpoint reference

| Endpoint / command | Purpose |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI Chat Completions ingress (streaming + non-streaming) |
| `POST /v1/messages` | Anthropic Messages ingress (streaming + non-streaming) |
| `POST /v1/responses` | OpenAI Responses ingress (streaming + non-streaming); requires a `protocol: openai-responses` endpoint |
| `GET /v1/models` | virtual model list (parseable by both SDK families) |
| `GET /health` | liveness only: `{"status":"ok","time":…,"uptime_seconds":…}`. **No credential, any source address** — the one endpoint a container probe, reverse proxy or external monitor can reach without an API key or a 127.0.0.1 source. It reports the current time and uptime rather than a constant `ok` so a cached 200 is distinguishable from a live one. Liveness, never readiness: it stays 200 while every upstream is down, because restarting the router cannot fix an upstream outage — read `/admin/status`'s health block if you want readiness. Nothing about the instance appears here; that is what the next row is for |
| `GET /admin/status` | process identity (pid/listen/version/config path/uptime), config-staleness verdict, reload outcome, per-endpoint health, Sticky Model registry size, concurrency metrics, operational warnings (`issues` when `config.Check()` flags anything), and — for any provider with a `quota:` block — a `quota` section with live consumption (see [Quota-Aware Routing](#quota-aware-routing) above), including whether a background recovery probe currently has an endpoint's single-flight slot (loopback only) — `vmr status` below is the CLI front end for this same payload |
| `vmr start -c config.yaml [-audit=false]` | run the router in the foreground (Ctrl-C to stop); `-audit=false` turns off the JSONL audit log (on by default). `./vmr.sh start` is the background-supervised equivalent and is the one command it shadows — run this one directly for foreground/dev use |
| `vmr check -c config.yaml` | validate config, run the consistency scan (missing api_key, a duplicate endpoint, …), and print the routing table with key status and per-provider effective proxy — flagged values get an inline ⚠️ plus a trailing `=== Failed ===` summary. With a trailing `log`\|`cache` argument, print just that resolved directory instead (`log_dir`/`image_cache_dir` after defaults) — what `vmr.sh` queries internally |
| `vmr status -c config.yaml` | render a running instance's identity (pid / listen / uptime / absolute config path) plus health and concurrency. `-addr host:port` queries whatever instance holds that port without loading a config at all — for when several instances run on one machine, or you don't have that instance's config; `-brief` prints one tab-separated summary line (what `./vmr.sh ps` builds its table from) |
| `vmr analyze [-c config.yaml] [-o dir] [-journey <id\|id-prefix\|glob>[,...] \| -compare <id1,id2> \| -corpus \| -macro-only \| -list-only \| -story-only] [-render-all] [-details] [-include-partial] [-include-self-traffic] [-lang en\|zh] [-currency CODE] [-report-config report.yaml] [glob...]` | the single analysis entry point (P9): one flag set, six mutually exclusive modes beyond the default suite. **No selector** — the default suite — runs the story half first, then the macro report half, sharing the same `-o`: `vmr-report.md` + `vmr-requests.md` + `stories/vmr-stories.md` + every rendered non-noise journey (`heartbeat` candidates still appear in the index, just not pre-rendered — see below), cross-linked. `-render-all` widens the default suite to materialize every non-partial candidate, including heartbeat. **`-journey`**/**`-compare`**/**`-corpus`** each zoom into exactly the single/pairwise/corpus view (see [Agent task narratives](#agent-task-narratives-vmr-story) above for what each renders) — only that story-side view runs, the macro report half does not; `-journey` accepts a comma-separated list of ids/id-prefixes/shell-style globs (`*`/`?`/`[...]`) and renders every match (one match renders directly, more than one batches). **`-macro-only`** runs just the macro report half — no candidate scan, no `stories/` output at all — equivalent to `vmr report`. **`-list-only`** lists candidate journeys without rendering any of them (writes `stories/vmr-stories.{md,json}`, no `journey-*.md`) — equivalent to bare `vmr story`. **`-story-only`** runs just the story half, skipping the macro report — no `vmr-report.*`/`vmr-requests*` written; unlike `-macro-only`/`-list-only` it composes with `-render-all` (`-story-only -render-all` is the exact equivalent of `vmr story -render-all`, which has never touched the macro report half). `-render-all` is rejected outright with `-macro-only`/`-list-only` (they replace the default suite's rendering scope knob entirely) but combines with `-story-only`; `-details` has no effect with `-list-only` (also rejected — it never renders anything, let alone materializes it). `-llm-addr host:port -llm-model name [-llm-key KEY] [-llm-dry-run]` adds an optional LLM interpretation section on a single-match `-journey` or on `-compare` (not `-corpus`, a multi-match `-journey`, `-macro-only`, `-list-only`, `-story-only`, or the default suite — one LLM call per journey makes no sense against a batch, and the other modes never render a journey interactively enough to attach one). Candidates in `vmr-stories.md` are grouped by category (`task`/`cron`/`heartbeat`/`subagent`, from title content markers) — only `heartbeat` collapses into a `<details>` block by default (real-corpus measurement found no heartbeat candidate ever reached 10 requests, while `cron`/`subagent` routinely did — the same threshold decides both the fold and the default render scope, so a row that's visible is always clickable); `vmr-stories.json` lists every candidate regardless. A journey report whose Steps are entirely non-Anthropic protocol (the common case — most deployments route mostly through OpenAI-shaped endpoints) carries a disclosure note under "Suspected Issues": a handful of rule detectors and the decision spine's own tool-result error badge read a field only Anthropic's protocol ever populates, so their absence there means "can't be checked", not "checked, clean" — `-corpus`'s report carries the same disclosure whenever the corpus isn't 100% Anthropic-protocol. `-include-self-traffic` disables the default exclusion of `vmr analyze -llm-addr`'s own analysis requests from both halves' totals — the identification rule is computed once (from `report.yaml`'s `llm_key`, the same tail-transform `api_keys` auth uses, plus an optional explicit `self_traffic_client_tags` list) and shared by every mode. `glob` is optional the same way as below — omit it entirely to analyze `-c config.yaml`'s own `log_dir`; `-lang`/`report.yaml` control output language, `-currency` picks the $ column's display currency (see [Cost estimate and pricing](#cost-estimate-and-pricing) above) |
| `vmr report [-c config.yaml] [-o dir] [-details] [-lang en\|zh] [-currency CODE] [-report-config report.yaml] [-include-self-traffic] [-llm-key KEY] [glob...]` | alias for `vmr analyze -macro-only` — parses its own flags, then forwards into the same dispatch, so output is byte-identical by construction, not just by convention: audit logs (plain or `.zst`) → usage statistics + session/tool analysis + per-request features (`vmr-requests.json`) + an error/truncation index (`vmr-requests-failed.jsonl`/`.md`) + detail files (`-details` to render them — off by default; the requests index links to each one's computed filename regardless); adds the §2 cost-estimate section whenever pricing data resolves anything — the built-in standard table always applies, `-c`'s config.yaml layers account-specific overrides on top if reachable (see [Cost estimate and pricing](#cost-estimate-and-pricing) above). `-llm-key` identifies past `-llm-addr` self-analysis traffic to exclude from totals (`vmr report` never makes a new LLM call itself). `glob` is optional — omit it entirely to report on `-c config.yaml`'s own `log_dir`. Output language defaults to English; `-lang` or a `report.yaml`'s `language:` (see [Output language](#output-language) above) switches to Chinese. `-currency` picks the $ column's display currency, independent of whatever currency it was computed in (see [Cost estimate and pricing](#cost-estimate-and-pricing) above). Remains fully supported — use it when you only want the macro report, without `vmr analyze`'s task-journey rendering |
| `vmr story [-journey <id\|id-prefix\|glob>[,...] \| -render-all \| -compare <id1,id2> \| -corpus] [-lang en\|zh] [-report-config report.yaml] [glob...]` | alias for `vmr analyze` — bare (no selector) forwards to `-list-only`; `-journey`/`-compare`/`-corpus`/`-render-all` forward unchanged. Parses its own flags, then forwards into the same dispatch as every other mode: reconstruct one agent task's full execution history into a readable Markdown narrative; no args lists candidate tasks with their ids (unlike bare `vmr analyze`, which renders the default suite), `-journey` accepts a comma-separated list of ids/id-prefixes/shell-style globs (`*`/`?`/`[...]`) and renders every match — one match renders directly, more than one batches like `-render-all` (which renders every candidate in a single batched pass, with no category filtering — that's an analyze-only default; forwards to `vmr analyze -story-only -render-all`, so it never runs the macro report half either, matching what bare `-render-all` always did here), `-compare id1,id2` diffs two already-built tasks' behavior profiles (rule-derived facts, including a divergence-point detection), `-corpus` computes statistics across every candidate. `-llm-addr host:port -llm-model name [-llm-key KEY] [-llm-dry-run]` adds an optional LLM interpretation section on a single-match `-journey` or on `-compare` (not `-render-all`/`-corpus`, nor a multi-match `-journey`, nor bare `vmr story` — resolved on demand now, so a validation error only surfaces on a mode that would actually use it). `-lang`/`report.yaml` control output language exactly as for `vmr report`; `glob` is optional the same way (see "No input files needed" above). Candidates in `vmr-stories.md` are grouped by category exactly as `vmr analyze` describes above. Remains fully supported — every flag here works identically under `vmr analyze` |
| `vmr version` | print this binary's build identity (git SHA, `-dirty` suffix when built from a modified tree, plus commit time and Go version). No ldflags needed — Go stamps VCS state into any binary built inside a repository and this reads it back at runtime. A running instance reports the same value under `/admin/status` and in `./vmr.sh ps`'s VERSION column, so "is that process running the binary I just built?" is a direct comparison |
| `vmr diagnose [-c config.yaml]` | beyond `check`'s static preview: DNS/TLS/proxy reachability per provider, then a real minimal request per configured endpoint asking for a one-time token echoed back (run concurrently, `-test-timeout` per check, default 15s) — a 200 that doesn't echo it warns instead of passing, catching a relay/gateway that answers with a cached or canned response instead of a fresh completion — plus a routing-order preview annotated with what it found (`-no-test-routing` to skip the live requests, `-json` for scripting; exits non-zero if anything failed) |
| `vmr replay -provider NAME <audit.jsonl>` | rebuild and resend one request from an audit record through the exact same request-building path vmr itself uses — `-dry-run` to print without sending, `-record path` to save the replay as its own audit line, `-model`/`-protocol` to override what the record itself says, `-stream true\|false` to force streaming on/off, `-max-time` to cap the upstream wait. Pick the record with `-req basename:line` (the coordinate published in `vmr-requests.json`'s `"req"` field), `-ts <timestamp>` (matches either `vmr-requests.json`'s or the raw audit log's `ts` field), or `-line N` (default: the last one in the file) — the three are mutually exclusive. With `-req`, the audit file argument is optional and copy-paste from `vmr-requests.json` works as-is: omit it entirely to search the current directory and `-c config.yaml`'s `log_dir` for the coordinate's basename (plain and `.zst` both tried), or pass a directory to search there instead; passing an exact file path keeps the strict basename-match check. `-ts`/`-line` still require the file spelled out — they carry no filename to search with. `-print` (with `-provider` omitted) skips request-building entirely and just prints the resolved record's raw JSON — the read-only counterpart to actually replaying it |
| `./vmr.sh start\|stop\|…` | dev-mode lifecycle (you supervise) |
| `./vmr.sh ps` | list every vmr instance on this machine, not just this checkout's: pid, listen address, uptime, model count, absolute config path. Three steps, each doing what it's actually good at — `pgrep` finds the processes, `lsof` finds the port each holds (the listen address lives in that process's config, not on its command line), and `vmr status -addr … -brief` asks the instance itself for the rest. Without `lsof`, or for a process that doesn't answer `/admin/status`, the row degrades to pid + the `-c` argument as typed, flagged with why — never to a missing instance |
| `./vmr.sh service install\|uninstall\|start\|…` | init-system service (launchd/systemd: crash restart, start at login) |
| `./vmr.sh <any command above> [args]` | any subcommand the script doesn't own is forwarded verbatim to the binary (`./vmr.sh check`, `./vmr.sh diagnose`, `./vmr.sh report …`) — not a whitelist, so a subcommand added to the binary works here immediately. Forwarding does two things: **runs from the caller's original directory** (relative paths, globs and `-o` mean exactly what they'd mean under a bare `vmr`), and **injects `-c <this checkout>/config.yaml`** when you didn't name one, for every subcommand that actually defines `-c` (`start`/`check`/`status`/`diagnose`/`replay`/`report`/`story` — `report`/`story` included, since both resolve `log_dir` from it when no glob is given, see "No input files needed" above). Foreground `vmr start` is the one command the script shadows — its `start` is the background one; run `./vmr start -c config.yaml` directly for the foreground |

Routed responses carry `X-VMR-Endpoint` (the endpoint that served it), `X-VMR-Attempts` (tries used) and `X-VMR-Route-Reason` (why that endpoint: `pick=order|quota|sticky`, `eligible=N/M`, plus `cooldown=` / `conditions=` / `ctx_fallback=1` only when they actually happened). Once any attempt has failed they also carry `X-VMR-Failover` (e.g. `deepseek/deepseek-v4:429, minimax/m2:500`; a build or network failure with no HTTP response is `:err`) — **including on success**, so "that worked, but only on the third failover" is visible in your terminal instead of in the audit log afterwards.

```bash
# Something's misconfigured — find out what before staring at 401s in the logs.
./vmr diagnose -c config.yaml

# A request failed; see exactly what vmr would send without sending it.
./vmr replay -c config.yaml -provider openrouter -dry-run \
    "$(./vmr check -c config.yaml log)/vmr-audit-2026-07-13.jsonl"

# Same request, actually sent, response printed to stdout.
./vmr replay -c config.yaml -provider openrouter \
    "$(./vmr check -c config.yaml log)/vmr-audit-2026-07-13.jsonl"

# Found the failing request in vmr-requests.json/.md instead?
# Point -req straight at its "req" coordinate — no line-counting needed.
./vmr replay -c config.yaml -provider openrouter -dry-run \
    -req vmr-audit-2026-07-13.jsonl:317 \
    "$(./vmr check -c config.yaml log)/vmr-audit-2026-07-13.jsonl"

# Just want to see the record itself, without building or sending anything?
./vmr replay -c config.yaml -print -line 317 \
    "$(./vmr check -c config.yaml log)/vmr-audit-2026-07-13.jsonl"

# Or replay by the exact timestamp shown in vmr-requests.json / vmr-report.md.
./vmr replay -c config.yaml -provider openrouter -dry-run \
    -ts 2026-07-13T15:30:42.100+08:00 \
    "$(./vmr check -c config.yaml log)/vmr-audit-2026-07-13.jsonl"
```
