<!-- Ver 2026-08-26, by ox-alpha -->

# Changelog

All notable changes to this project are documented here, in
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format. This project
tags releases `vX.Y` rather than strict semver (see `internal/buildinfo`'s
doc comment for why there is no other version file to keep in sync) — the
version headers below match the tag with the leading `v` dropped.

`release.yml`'s release job extracts the section matching the pushed tag and
uses it verbatim as the GitHub Release body — GitHub's own auto-generated
notes (bare PR titles) are unreadable on this repo's mostly direct-to-main
workflow, so every tag needs a matching section here before it's pushed, or
the release job fails on purpose rather than publishing something empty.
See this file's row in `CLAUDE.md`'s Conventions section for the write-time
process (write into `[Unreleased]` as you go, retitle it when cutting a tag).

Sections before the most recent release are compressed summaries — the
commits and design docs hold the full reasoning.

## [Unreleased]
### Added
- `config.minimal.yaml` (+ `config.minimal.zh.yaml`) — a ~10-line "just get running" template (one provider, `coding` + `claude` virtual models). README Quick Start now copies this; `config.example.yaml` stays the fully annotated reference
- README Quick Start gained a **Verify** step: `vmr diagnose` (config check + DNS/TLS + a real echo request per endpoint + routing preview) between Run and Connect
- `./vmr.sh redeploy` — dev supervisor shortcut: stops the running instance, rebuilds the binary from current source (`go build -o vmr ./cmd/vmr`), and starts it back up
- `GET /help` + `/help.html` (English), `GET /help.zh` + `/help.zh.html` (中文) — a static, unauthenticated agent configuration guide: per-tool setup snippets (Claude Code, Codex, Aider, OpenCode, …) with the instance's own base URLs baked in from the request Host, plus a live virtual-model list fetched client-side from `/status`. Both language variants carry a top-right switcher link to the other; the `/status.html` dashboard gains a "Connect Your Agent" card linking to it
- `/status` agent-facing model metadata: `models` became a structured array (was a map keyed by the display string `"name [protocol]"`) — one entry per virtual-model × protocol carrying `id`, `protocol`, `capabilities` (union across endpoints; `[]` = unconstrained), `max_context_tokens` (largest across endpoints; `0` = unlimited) and `endpoints[]` with live health plus each endpoint's own capabilities/context override. The dashboard (`/status.html`) renders both levels (unconstrained shows as that word, never an empty badge) and `vmr status` prints the model-level lines
- `/status`: `instance.base_urls` — the client-facing base URL per ingress protocol (all `<scheme>://<host>/v1/`), echoed from the request itself (Host header + whether TLS was used) rather than derived from `listen`: whatever address the caller reached `/status` at is exactly what it should point its client at

### Changed
- **Protocol enum renamed** to match ecosystem tools (Pi Agent et al.): `openai` → `openai-completions`, `anthropic` → `anthropic-messages` (`openai-responses` unchanged). Update `protocol:` values and `base_url` keys in your config — `vmr check` reports the old names as unknown adapter types. New audit logs write the new names; the analytics half (`vmr report`/`story`) transparently normalizes old names when reading pre-existing logs (transitional, to be removed ~2026-10). `vmr replay` reads new-format logs only
- the human-readable model label `"<name> [<protocol>]"` is now defined once (`core.ModelLabel`); `vmr diagnose` route groups and `vmr status` output are byte-identical, just sourced from one place so the surfaces cannot drift apart
- `/status.html` "Connect Your Agent" card redesigned: moved to the bottom (below the model topology), always expanded (no more click-to-toggle), now a two-column layout — left lists every protocol base URL, each prefixed with its protocol badge; right lists every virtual model (deduplicated across protocol groups) with its capabilities and context window
- `/help.html` "Connection Information" card redesigned into the same two-column layout as `/status.html` (Base URLs on left, Virtual Models with capabilities/context on right), replacing the old static table
- `/help.html` + `/help.zh.html` agent snippets now fill live from `/status`. Single-model snippets get their model id, context window and token budgets swapped in place; the four list-shaped configs (Pi `models.json`, `opencode.json`, Continue `config.yaml`/`.json`, Aider `.aider.model.metadata.json`) are regenerated to enumerate **every** virtual model of the matching protocol (Pi splits into `vmr-openai` / `vmr-anthropic` providers). Output-token budgets are tiered by each model's context window (64k out at 200k ctx, 128k at 1M) and never exceed it; reasoning/thinking effort defaults to `high`; the operator's API key is injected once entered. Static defaults (`coding` / `claude`, 200k context, 64k output, `high`, `YOUR_VMR_API_KEY`) remain in the served HTML for no-JS / pre-auth readers, and clearing the key reverts to them
- `/help.html` + `/help.zh.html`: dropped the post-configuration usage commands (`claude --model …`, `codex --model …`, `aider`, `cursor-agent …`, bare `openclaw`/`workbuddy`/`hermes`, the Pi "then run" note) — the guide configures an agent, how to launch it afterwards is each agent's own concern
- Header navigation polish: `/help.html` Dashboard link harmonized to `🎛️ Status` matching `/log.html`; `/log.html`'s Status link moved from the left to the right side alongside other controls; `/help.html` gains a `🌐 中文` language switcher, `/help.zh.html` an `🌐 English` one
- `/help.html` + `/help.zh.html`: the Quick Reference table moved from the page bottom up to right below the protocol picker, and every agent name in it now links to that agent's full guide section further down
- `/status.html` "Virtual Models & Endpoint Topology" redesigned: protocol is now a colored header tag (friendly name matching the Connect Your Agent card) instead of a per-row `PROTOCOL` column, endpoint names drop their redundant `<protocol>/` prefix, the model-header metadata badges (caps / ctx / endpoints) right-align, and the `PRI` pills are colored by priority-tier rank so endpoints in the same failover group share a color across cards. The `FAILS` column header gained a tooltip clarifying it is the current consecutive-failure count (not a cumulative request/failure ledger — that lives in `vmr analyze`)
- a missing `api_key` or an unset/empty `${ENV_VAR}` the config referenced now raises a boxed `CONFIG PROBLEMS` banner on start/reload (naming the unset vars), instead of one quiet `WARN config check:` line that drowns in the config dump. Purely advisory issues still log a single WARN line
- a request for a virtual model whose endpoints are **all** keyless now gets an immediate `vmr_no_api_key` 503 naming the model, instead of a raw upstream 401 (often provider/CDN HTML) plus a 10-minute health cooldown per endpoint

### Fixed
- `/help.html` guide section width mismatch: a stray `</div>` prematurely closed the `.container`, rendering guide cards at full viewport width instead of matching the 960px connection card above it
- `/help.html` auth modal was rendered open on first load — the stylesheet was missing the `.hidden { display: none !important; }` rule the modal (and its error line) rely on, so the "Authentication Required" dialog sat over the page whether or not the instance requires a key. The modal now starts hidden and only opens on a real `/status` 401 (or a click on the reveal link)
- `/help.html` embedded script no longer redeclares `const Auth` — the duplicate declaration was a `SyntaxError` that killed the entire script, so base-URL/snippet placeholder filling never ran; `Auth` also gains the `clearKey()` it referenced but never defined

## [0.6.1] - 2026-08-25
### Added
- `GET /log` + `/log.html` — the live console log streamed over HTTP as an endless `text/plain` connection: replay of the most recent ~512 lines first, then live following, replacing browser-side `tail -f`. Auth-gated via `api_keys` like `/status`; the viewer shell shares the status page's key prompt/storage and the two pages cross-link. Idle connections get a keepalive newline every 30s; slow readers skip lines with a marker instead of slowing the logging hot path
- `vmr smoke` — fire a minimal real request at every configured virtual-model × provider × upstream-model combination **through a running instance**, so the full live path applies (auth, health, conditions, quota metering, audit) and per-model quota rows become visible after a fresh config. `-provider`/`-target-model`/`-model` filter the run, `-json` for scripting; exits non-zero if any smoke failed
- Pinned routing: `X-VMR-Provider` / `X-VMR-Target-Model` headers force a request onto one specific backend within the requested virtual model's own endpoint set — defeating priority/order/quota/sticky, but strictly a narrowing lens post-filtering; the headers are consumed by the router, stripped before forwarding, and reported in `X-VMR-Route-Reason`. This is how `vmr smoke` targets each endpoint

### Fixed
- **buffered-mode truncation no longer loses data (B1)**: when an upstream connection dies mid-body on a buffered response (every non-SSE 200, plus MiniMax thinking streams), the bytes already received are flushed to the client (partial JSON for non-SSE, the streamed tail for an SSE stream already in passthrough; nothing for an SSE stream still buffered mid-`<think>`, so a dangling reasoning block can't leak) and the connection is aborted (`http.ErrAbortHandler`) — the client SDK sees a broken transfer instead of a clean, silent empty/partial 200. The audit trail already marked the attempt truncated
- **quota counters survive config reload with a default `since` (B2)**: an unset `since:` anchored the window at the exact load instant, so every startup or hot-reload recomputed the period boundary and the lazy-reset zeroed the account's accumulated usage. The default anchor is now aligned to a fixed calendar boundary (midnight for min/h/d, Monday for w, the 1st for mo), so any same-day reload keeps the count for any `every` value. A reload that crosses midnight can still shift the grid once, and only for an `every` N that doesn't divide its unit's day (`every: 5h`, `every: 7min`); set an explicit `since:` to pin those
- **LLM-inferred Findings verify their evidence anchor at runtime (B3)**: `vmr analyze`'s optional LLM interpretation layer now drops any Finding whose `EvidenceAnchor` is not a verbatim substring of the real transcript — a hallucinated anchor could previously be promoted to a HIGH-confidence `[AI推测]` Finding
- **`vmr report` Markdown escapes user-derived titles (B4)**: a session or task title containing a `|` (e.g. a quoted shell pipe) or an unclosed `<!--` no longer corrupts the table row or lets an HTML-aware renderer swallow the rest of the file — the same fix `vmr story` already carries
- hot-reload `reload()` is serialized (B5): concurrent fsnotify + SIGHUP triggers could race `rt.Install` and briefly double the effective concurrency limit
- analytics: a mid-task context compaction (stitch boundary with no new instruction) no longer opens a spurious new Task, so `len(j.Tasks)`, `plan_exec_ratio`, per-Task detectors and `-corpus` grouping stop being inflated (B9)
- incident-driven error-classification misses: vendor protocol-constraint rejections (DeepSeek thinking-mode `reasoning_content` pass-back 400; the pre-existing `thought_signature` wording moves there too) are now a new `ErrQuirk` class — switch with zero cooldown instead of dead-ending the failover walk; bai's input-token-exceeded 400 is a context-limit overflow (switch, no cooldown); a relay reporting its own OAuth refresh failure with standard error codes is `ErrAuth` (long cooldown + switch). Genuinely malformed requests keep the single-attempt `ErrClient` semantics
- quota rows' `bucket`/`gate` role is derived per model from its applicable-Limit subset — the same path routing scores with — instead of one global derivation across the provider's whole Limits list, so two same-period Limits on disjoint model scopes no longer mislabel rows
- `vmr smoke`'s probe uses `max_tokens` 4 instead of 1 — some upstreams reject ≤ 2 with a 400

### Changed
- dashboard polish: the requests summary's protocol row relabeled `By Protocol` with the full breakdown on hover; one inline-SVG favicon shared by `/status.html` and `/log.html`, nav links carrying the destination page's emoji; the quota table's ROLE column folded into badge colors (blue = bucket, red = gate, hover explains); percentages render with up to two decimals and K/M/B magnitudes drop trailing zeros

## [0.6] - 2026-08-23
### Added
- Quota-Aware Routing P3: multiple windows per provider (e.g. an RPM gate beside a monthly bucket), merged bucket-vs-gate — the longest-period Limit is the bucket (underuse boosts the score), shorter gates cap at neutral; `every: min` units and bare clock-time `since` for min/h Limits; a Limit's `models:` scope selects both which models it applies to and whether they share one pool or get independent ones (`"*"` is a reserved token, not a glob)
- exact fractional-multiplier charging: `model_multipliers` charge the configured value instead of silently rounding each charge up (quota counters went float64; a one-directional `vmr-quota.json` upgrade)
- config ergonomics: labeled `api_keys:` expands one provider entry into independent per-account providers at load time; `endpoints[].providers` packs several accounts into one try-order entry; top-level `fallback_endpoints:` appends tail fallbacks to every qualifying virtual model
- `vmr analyze`: single entry point for the whole analytics suite; `vmr report`/`vmr story` became thin aliases routed through its dispatch (breaking); six zoom modes (`-journey`/`-compare`/`-corpus`, `-macro-only`/`-list-only`/`-story-only`)
- story depth: Phase 1b LLM semantic detectors (six behavioral-defect Findings anchored to verbatim transcript evidence), Context Rot and Tool Sequence Pattern corpus sections, output-repetition-rate metric, per-Journey model-usage & switches table, a machine-readable `structure` skeleton in `journey-<id>.json`, per-Step "→ detail" links rendered on demand
- stable record coordinates: a `req` field joining `vmr-report.json` and `vmr-stories.json`, `vmr replay -req COORD` and `-print`, `internal/audit.LineAt`; the redundant per-detail-page `.json` copies were removed with them
- shared `internal/reqdetail`: detail filenames derive from the record's own coordinate hash (byte-identical across runs and machines); system prompts and tool sets deduplicated into content-addressed evidence blobs
- parse cache: its own `{out}/.parse-cache/<hash>` shards stamped with a schema version, extended to cover report's aggregation facts — warm full-corpus rerun dropped ~72s → ~16s
- `GET /health`: unauthenticated liveness-only endpoint (`{status, time, uptime_seconds}` + no-store) — deliberately never readiness
- `GET /status.html`: single-page CSR dashboard (auto-refresh, health/cooldown badges, concurrency/system/traffic gauges, quota bars, key prompt modal), embedded via `//go:embed`, zero CDN dependencies
- self-traffic exclusion: analytics exclude the instance's own `-llm-addr` interpretation calls from cost/usage totals by default
- the strategy design doc (`docs/VirtualModelRouter_Design_v4_Strategy.md`) and an archtest documentation-drift guard over every cited path/package/symbol

### Changed
- **Breaking**: `/admin/status` → `/status`, loopback-only restriction dropped, auth unified under `api_keys`; `vmr status` reads the key from config
- **Breaking**: `/status` payload restructured into `instance` / `system` / `traffic` blocks (config sub-object, lock-free atomic traffic/token counters)
- **Breaking**: report `sessions[].id` switched from positional `s01` labels to content-addressed lineage ids, directly joinable against story journeys
- the default analyze suite renders cron/subagent journeys too (only heartbeat stays collapsed); large-batch rendering builds in chunks of 20, bounding peak memory
- detail pages slimmed: raw SSE bodies replaced by a `vmr replay -print -req` pointer, previous-turn history folds into a link, default runs materialize no `details/*.md` (one real day: 47MB → 3MB; `-render-all` pages shrank ~86%)
- response normalization extracted into `internal/respnorm` with `io.Reader`-level fuzz coverage (no behavior change); `jsonscan` scanners hardened against out-of-range indices; the client-header blocklist and JSON/error writers moved from `core` to `router`
- session/task segmentation unified into `internal/taskseg` (report and story were two independently-maintained copies); four drifting token formatters converged on `fmtutil`

### Fixed
- streams aborted by client disconnect audit as `canceled` and no longer trip upstream health penalties
- respnorm: infinite read-after-EOF loop for generic `io.Reader` consumers; spurious extra `[DONE]`/blank line under TCP fragmentation
- report↔router parity: degraded token/cost estimates reproduced instead of silently rendering 0/`-` (estimated share shown), window consumption computed on forwarded attempts, same-account multi-key double counting
- misc: duplicate parse-cache entries across absolute/relative path spellings, OpenClaw envelope/bracket scaffolding leaking into task titles, nil-profile panic paths, proxy credentials redacted in `vmr check`

## [0.5] - 2026-08-11
### Added
- Quota-Aware Routing P1/P2: cross-account usage balancing on a virtual model (`requests`/`tokens`/`cost`), account weighting, cost-based pricing, model multipliers
- multi-currency pricing with display-currency rescale
- `vmr replay` charges its real upstream consumption against quota

### Changed
- `metric: cost` simplified to static per-model rates; added `ErrContextLimit` failover
- README/UserGuide rewritten around the router + flight-recorder positioning

### Fixed
- a flaky quota store test (period-start mismatch between two charges in one test)

## [0.4] - 2026-08-05
### Added
- `vmr story`: Findings, decision spine, corpus statistics, divergence-point layers
- file-content-hash parse cache and the `report.yaml` sidecar config
- `-journey` comma/glob selectors; decision spine reworked into per-Step blocks carrying full args

### Fixed
- journey ids use the record's own write-time offset (forced UTC dropped)

## [0.3] - 2026-08-04
### Added
- OpenAI Responses protocol support (`POST /v1/responses`) across routing, diagnose, load test, report/story
- EN/ZH bilingual output for `vmr report`/`vmr story`
- Homebrew tap (`brew install`)

### Changed
- simplified recovery probing and proxy config; response buffers right-sized to context-window scale

### Fixed
- OpenAI Responses response-side content extraction

## [0.2] - 2026-07-31
### Added
- condition-based routing and Sticky Model session affinity; active health-probe mode
- `vmr story`: Journey/Task/Step narrative, `-compare` LLM interpretation layer, `-render-all`
- per-provider `role_map` for OpenAI-compatible providers rejecting `developer`; prebuilt binaries, CI and release workflows

### Changed
- `vmr report` rewritten as nine sections (cost estimate, Chat User grouping, ~70% faster runtime)
- providers flattened to a multi-protocol list; model-level capability/context base; `base_url` must carry its own API version

### Fixed
- upstream-gateway-failure misclassification, image-detection false positives, sticky TTLs exceeding the registry's 24h backstop

## [0.1] - 2026-07-13
First public release: local-first, single-binary LLM router behind one stable virtual model name — byte-faithful passthrough for the OpenAI and Anthropic protocols, error-class-aware failover (cooldowns, backoff, `Retry-After`, single-flight recovery probes), the JSONL flight-recorder audit log with auto-compression and expiry, `vmr report` usage/latency/session analytics, optional disk-cached inline-image downscaling.

[Unreleased]: https://github.com/bigfatsea/vmr/compare/v0.6.1...HEAD
[0.6.1]: https://github.com/bigfatsea/vmr/compare/v0.6...v0.6.1
[0.6]: https://github.com/bigfatsea/vmr/compare/v0.5...v0.6
[0.5]: https://github.com/bigfatsea/vmr/compare/v0.4...v0.5
[0.4]: https://github.com/bigfatsea/vmr/compare/v0.3...v0.4
[0.3]: https://github.com/bigfatsea/vmr/compare/v0.2...v0.3
[0.2]: https://github.com/bigfatsea/vmr/compare/v0.1...v0.2
[0.1]: https://github.com/bigfatsea/vmr/releases/tag/v0.1
