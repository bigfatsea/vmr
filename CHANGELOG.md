<!-- Ver 2026-08-11, by Sonnet 5 -->

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

## [Unreleased]
### Added
- `vmr report` §2.5: a per-provider-account (config.yaml `providers[].name`) roll-up — tokens, cache efficiency, mean latency, reliability, $ estimate
- `vmr report` §5.5: per-client breakdown of which upstream endpoints (`protocol:provider:model`) it hit and how many tokens landed on each — grouped by client, so the same model name under two different provider accounts no longer gets merged into one number
- `vmr story`: a per-Journey **model usage & switches** table — which upstream models/accounts a task actually used, and every point where the upstream changed mid-task, read from the real endpoint rather than the (nearly always constant) client-facing virtual model name
- `vmr report` §2.5: a "Quota vs. Consumption" sub-table for every account with a declared `quota:` — this report run's own recomputed window consumption placed next to the router's real-time `<log_dir>/vmr-quota.json` counter (used%/elapsed% side by side, plus what share of that period's consumption came from a degraded estimate rather than exact usage), each explicitly labeled with its own time window and never combined into one number
- `vmr report` §7: a `provider_quota_exhaustion` finding — fires from the router's real-time quota counter (never an estimate) when an account is both at/above 90% used for its current period **and** burning faster than the period is elapsing (the same relative condition the router's own `Headroom < 1` routing decision uses), so a healthy short-period account (e.g. `every: 5h`) no longer alerts on every run near cycle end
- `vmr story -compare`/`-corpus`: model switch count (`len(Metrics.ModelSwitches)`) registered as the 13th behavior-profile metric — a routing-environment signal, not an agent-behavior one
- `vmr report` §2.5: a "主要错误类/Top Error" column (e.g. `rate_limit 12(63%)`) on the main table, so an account's dominant error class is visible without cross-referencing §3 — answers "is this account hard-quota-exhausted or just being rate-limited" the error-rate column alone couldn't
- `vmr report` §2.5's "Quota vs. Consumption" sub-table: names its own real-time counter's source path in a footnote, and warns when none of this run's input audit logs resolve under that counter's `log_dir` (a same-machine, different-instance mismatch is otherwise silently misleading); flags a row whose report window and billing period share no time at all (†), a row over its configured quota (⭐), and a row whose `quota:` metric/every changed mid-period (‡, distinct from a plain stale-period `-`)
- `vmr report`: a progress line reporting §5.5's client×endpoint row count, since that section has no Top-N cap by design
- `vmr-report.json`: each endpoint row carries a new `forwarded` count (attempts whose response was actually forwarded — a superset of `ok`, which excludes a 2xx that broke mid-copy). It is the exact basis the router charges quota on, so §2.5's recomputed `requests` figure can reproduce the router's own number rather than approximate it
- `vmr-report.json`: each endpoint row carries `tokens_in_fresh_est`/`tokens_out_est`/`tokens_estimated` — the degraded byte-count token estimate for the requests whose upstream never returned a usage object, kept in separate fields so no existing consumer of the measured token columns silently starts averaging an estimate into them
- `vmr-report.json`: each §2.5 quota row carries `window_estimated_pct` — what share of the recomputed window consumption came from that degraded estimate rather than sniffed usage. Rendered as "X% est." next to the number, the same annotation the live counter column already carried
- Differential tests pinning `vmr report`'s §2.5 recomputed column against what `internal/router` actually charged now cover all three quota metrics (`tokens` and `cost` joined the existing `requests` case), driving the router's own charging entry points rather than restating their formulas
- `vmr-report.json`: each endpoint row carries `cost_estimate_est` — the portion of `cost_estimate` priced from the same degraded byte-count estimate `tokens_in_fresh_est`/`tokens_out_est` record, rather than sniffed usage. Feeds §2.5's `window_estimated_pct` for `metric: cost` accounts the same way the existing tokens-side fields already did for `metric: tokens`

### Changed
- Quota accounting for an account with `model_multipliers` now charges the exact configured multiplier instead of rounding each charge up to the next integer — a fractional multiplier like `2.5` used to be silently charged as `3` (a systematic overcharge of up to ~+100% per charge, magnitude unrelated to the configured value, e.g. `2.5`→+20%, `2.9`→+3.4%). `internal/quota.Counters`'s five raw components (and the matching `estimated`/`Estimated` fields) are now `float64`, so `vmr-quota.json`'s `requests`/`fresh`/`out`/etc. fields may now hold a fractional value for such an account — this is a one-directional upgrade: an older `vmr` binary cannot read a `vmr-quota.json` written by this version if any configured account has a non-integer effective multiplier (it falls back to an empty counter for that file, same as any other corrupt-file recovery, and self-heals at the next period boundary). An account with no `model_multipliers` configured is bit-for-bit unaffected.
- `vmr report` §2.5: the declared `quota:` reference moved out of the main table entirely — it now appears only in the "Quota vs. Consumption" sub-table below, instead of being duplicated in both places through two different number formatters
- `vmr report`: a `config.yaml` read failure now prints one unified warning instead of two near-identical ones (pricing and §2.5 quota references used to each print their own)
- Live router log line redesigned for density: `»` replaces `-> ... (stream)` for a streaming request; the pre-call `req=xxxKB/xxxESTKT` column is replaced by actual token usage (`in`/`ch` cache-hit%/`cw`/`out`) once a response reports it, falling back to `in xxxKT(est)` when it doesn't; capabilities are now pipe-joined and omitted entirely for a pure-text request instead of printing `cap=-`; and the trailing `attempt=N, status=X, dur=Ys` triplet collapses into `STATUS(Ys, Nx)`
- Live router log line's client-tag column widened from 8 to 9 chars — an 8-char client tag used to run straight into the next field with no separating space (e.g. `openclawopenai:agent`); non-streaming requests now use `>` instead of `->`; token counts always render as `X.XXKT`/`X.XMT` (2 decimals below 1000, 1 above) instead of a bare token count below 1000, so a value like `6` tokens no longer prints unit-less next to a `35.4KT` on the line above it; the background probe's log line (`probe    ...`) now shares the same comma-separated `key=value` punctuation as every other live-log line instead of its own space-separated one; a returned-as-is client error (4xx, no cooldown) now reads `status/class=400(...)` instead of a bare `400(...)`
- Startup/hot-reload config summary in the server log now shares `vmr check`'s exact section renderers (`=== Global Settings ===`/`=== Providers ===`/`=== Models ===`, same column widths, same ⚠️ warning markers) instead of a separately-maintained, less detailed format — the two can no longer drift out of sync
- `internal/router`'s response-normalization state machine (`response.go`/`responsefix.go` — event splitting, model-field rewrite, `[DONE]` policy, the buffered/passthrough decision, and the two MiniMax-specific thinking-mode repairs) moved into its own package, `internal/respnorm`, exposing only `Wrap(io.Reader, Options) NormalizerStream` — `router`/`quota.go` now depend on that interface instead of reaching into a router-private type. No behavior change (byte-faithful passthrough, same transforms, same audit trail); the actual payoff is that the state machine can now be fuzz-tested at the pure `io.Reader` level, independent of `Router`/`Snapshot` (`internal/respnorm`'s new `FuzzStream`, covering the "no panic/hang", "opaque never transforms a byte", and "a completed think-strip never leaves a dangling `<think>...</think>` pair" invariants across every `{isSSE, protocol, opaque}` combination) — this file had no fuzz coverage at all despite being the one other hand-written byte-level state machine on the hot path besides `internal/jsonscan`'s scanners. Quota-Aware Routing's usage/byte-count sniffing stays embedded in the same type rather than moving to a separate decorator, a deliberate zero-added-cost tradeoff documented in the new package's doc comment

### Fixed
- `internal/respnorm`'s passthrough streaming path could splice a spurious extra blank line into the response right before an appended `data: [DONE]`, when the upstream's trailing SSE event separator happened to land split across two separate TCP reads (e.g. one read delivers "...\n\n", the next delivers a single trailing "\n" from a `\n\n\n` sequence) — `tailNL` (whether the client-visible stream currently ends with the separator) was computed from only the most-recently-emitted block's own bytes, which a short trailing fragment can't contain a 2-byte separator within even when the accumulated output actually does end with one. A B7 follow-up review's fragmentation-invariance fuzz check (`internal/respnorm`'s `FuzzStream`, chunked vs. whole-shot delivery of the same bytes must produce identical output) caught this within seconds of adding chunked-delivery coverage; whole-shot delivery of the same bytes (what a small fuzzed input naturally gets from `bytes.Reader`) never hit it, which is why it went unnoticed until fragmented delivery was fuzzed. Fixed by tracking the last 2 bytes emitted across ALL blocks (not just the most recent one) instead of re-deriving it from a single block already handed back to the caller
- `report.AnalyzeSessionsCached`/`story.BuildChain`/`story.BuildAll`/`story.PreviewTitle`/`story.PreviewTitles` now return an error instead of letting a nil `taskseg.Profile` reach `prof.RealUserText` — previously that would panic deep inside a per-file/per-chain worker goroutine with no `recover()`, crashing the whole process (potentially with several interleaved goroutine stack traces) instead of surfacing one clean error at the call boundary
- `fmtutil.CapStr` panicked (`slice bounds out of range [:-1]`) when called with a negative cap — no production call site does today, but it's a shared public function in a zero-dependency leaf package, not a private helper with a closed set of callers
- `internal/taskseg`'s OpenClaw dialect's 200-byte trigger check for stripping the "(untrusted metadata)" JSON envelope only matched the `Conversation info` header, while the strip regex itself also matches a standalone `Sender` header — a message carrying only the `Sender` envelope bypassed stripping and leaked raw JSON into `vmr report`/`vmr story` task titles
- `vmr.sh`'s `cmd_start` port-occupancy precheck was silently dead (its regex expected `vmr check`'s listen-address line as `listen=...`, but it prints `listen:`) — restored
- `server.chatHandler` no longer runs image downscaling for a request naming an unknown virtual model; that work was always discarded since `router.Serve` immediately 404s on the same lookup
- README's links to `docs/Why_vmr_over_LiteLLM.{md,zh.md}` were dead (the file had been swept into an untracked `archived/` directory during an unrelated cleanup) — restored to `docs/` and re-tracked
- `vmr report` §2.5's "Quota vs. Consumption" sub-table: a `metric: cost` account with traffic but no resolvable price anywhere in this window now renders its window-consumed cell as `-` instead of a fabricated `0` (indistinguishable from "genuinely spent nothing"); the `requests` metric's window-consumed figure is now `multiplier × forwarded-attempt count`, reproducing the router's charging exactly — previously it multiplied the *request* count, which over-counted by one charge per fully-failed request (the router charges only on a forwarded upstream response)
- `vmr report` §2.5's "Quota vs. Consumption" sub-table, `metric: tokens`: a request whose upstream returned no usage object contributed **0** to the recomputed window consumption while the router had charged a byte-count estimate for it — a systematic under-count. The estimate is now reproduced on the report side too (same formula the router charges with), and the share of the window that came from it is shown as "X% est." next to the number. The previous all-or-nothing guard (render `-` when *every* request on the account was unparseable) never fired on the case that actually matters and is now gone: a window where only *some* requests were unparseable used to render a precise, systematically-low figure with no signal whatsoever
- `vmr report`'s degraded-token reproduction (`tokenest.go`) silently contributed 0 for the request side of any `vmr replay`-produced audit record needing the estimate — `replay` records never populate `Facts` (the field the reproduction normally reads its pre-computed estimate off), unlike every live-traffic record. It now falls back to the same `core.EstimateTextTokens` computation over the recorded request body that `internal/replay`'s own degraded-charge path already uses, so the two agree instead of the report side silently under-counting
- `vmr report`: every request-level metric on an endpoint row (`requests`, token totals, latency samples) was double-counted whenever one request failed over between two API keys of the *same* account — endpoint labels are `protocol:provider:model` with no key component, so a provider configured with several `api_keys` produces several endpoints sharing one label, and the "attribute to the endpoint that served the client" guard matched more than once
- `internal/router`'s exact-vs-degraded token-charge decision had grown three independent implementations (the live path, `vmr replay`, and `vmr report`'s recomputation); the arithmetic is now one exported `router.TokenCounters` that the first two call directly
- `vmr report` §2.5's "Quota vs. Consumption" sub-table: the ⭐ (over-quota) footnote is now only printed when a row actually carries the marker, matching how the ‡ and † footnotes already behaved; the ‡ footnote no longer claims the quota config changed "during the current period" (superseded counter keys are never cleaned up, so a long-ago edit raises the same marker)
- `vmr story`: an endpoint a Step failed over AWAY FROM is no longer invisible in the model-usage table (it only ever read the Step's last attempt) — every attempt's upstream is now counted, though token attribution still goes only to the Step's final resolved endpoint
- `vmr report`'s `attemptUpstream` fallback (used when an audit record's structured Protocol/Provider/Model fields are empty) now goes through `core.SplitEndpointLabel` instead of a private "/"-only split, so a `:`-joined `Endpoint` (the current on-disk format) resolves correctly instead of silently returning an empty triple — it had disagreed with `vmr story`'s own upstream lookup on the exact same record
- `internal/report`'s and `internal/story`'s independently-written `pctStr` percentage formatters (1 decimal vs. 0 decimals, despite a comment claiming they matched) now both alias a new shared `fmtutil.FmtPercent(f, decimals)` — each keeps its own existing precision, but a future edit to the formatting rule can no longer land in only one of the two
- `internal/server/facts.go`'s `indexUnescapedQuote` (used to size a base64 document payload for token-count-based routing decisions) was a byte-identical private copy of `internal/adapter`'s JSON-string-escape-parity scanner; now exported as `jsonscan.IndexUnescapedQuote` (moved there from `adapter` in a later refactor batch, before this ever shipped) and called from both sites instead of maintained twice
- `internal/story`'s `-compare`/`-corpus` behavior-profile metrics (code, display Kind, and value-extraction) were declared twice — once implicitly at each of `compare.go`'s 13 row-building call sites, once explicitly as three hand-maintained lists in `corpus.go` that a comment claimed "mirrored" the first — now a single `metricSpecs` table in `compare.go` that both `Compare` and `-corpus`'s distribution/correlation/rendering code range over, so a future 14th metric can no longer land in `-compare` while silently missing from `-corpus`
- `core.SplitEndpointLabel` tried the current ":"-joined format first unconditionally; a legacy "/"-joined audit-log `Endpoint` whose model segment itself contained two or more ":" (e.g. an Ollama/vLLM-style `registry:port/name:tag`) could satisfy the ":" split with exactly 3 (wrong) parts before ever reaching the correct "/" split — it now picks whichever separator occurs first in the string, which is always the real field boundary since `protocol`/`provider` never contain either character
- `vmr check`'s global `http_proxy`/`https_proxy` lines printed the configured proxy URL raw, including any embedded `user:pass@` credentials — the per-provider `proxy:` line already redacted this the same way `maskAPIKey` redacts a provider's own API key; both now go through the same `redactProxyURL` helper
- `internal/report`'s and `internal/story`'s session/task-boundary algorithm (real-user-instruction detection, new-task/new-instruction detection, task titling, response reassembly, preview truncation) were two independently-maintained implementations of the same rules, differing only in incidental shape — a divergence in either copy would have made the two commands silently disagree on where a task starts. Now a single `internal/taskseg` implementation (`segment.go`) both call through; verified byte-identical output (aside from the report's own generation timestamp) against real audit corpora before and after
- `vmr story`'s Journey title (`deriveTitle`) and its cheap `-list` preview path (`titleFromRecord`) each re-parsed the opening request body and re-ran the real-user-instruction regex a second time even though the main scan had already indexed it — both now read the already-built `taskseg.RealUsers` index (new `taskseg.FirstInstruction` helper) instead of rescanning; `vmr report`'s `sessionTitle` unified onto the same helper. A stitch-boundary title's redundant two-phase fallback comparison (`title == ToolLoopTitle` before substituting a more specific wording) is also gone — a real user instruction that happened to match the placeholder text verbatim could have been wrongly overwritten by it
- `vmr report` §2.5's "Quota vs. Consumption" sub-table, `metric: cost`: a request whose upstream returned no usage object contributed a hardcoded **$0** to the recomputed window cost while the router had charged a degraded byte-count estimate for it — the same false-zero N2 already fixed for `metric: tokens`, just not carried over to cost at the time. A provider whose entire window was unsniffable used to render a misleadingly precise `$0.0000 (0% est.)` instead of either a real number or `-`; a mixed window silently under-counted. `costFor` now prices the same degraded estimate the router charges (Fresh/Out only, no cache components — neither side can tell cache hits apart from an unparseable response), and the estimated share is shown as "X% est." the same way the tokens metric already does
- `internal/report`'s six aggregation Row types (`Row`/`HourRow`/`ClientRow`/`WorkloadRow`/`SessionRow`) each independently declared and accumulated the same ~13-field token/latency/volume core (`requests`/`tokens_*`/`cache_efficiency`/`requests_with_dur`/`dur_ms_p50`/`dur_ms_p95`/`slow_requests`), backing 7 near-identical accumulation closures inside `buildInternal` (~290 lines) — now a shared `TrafficStats` type all five embed, with one `Ingest`/`Finish` pair each type's own (much shorter) wrapper calls into. `EndpointRow` deliberately keeps its own fields (its request-grade counters are `omitempty` where the other five are not — an endpoint can have attempts with zero served requests) but gets the same closures→methods treatment (`IngestAttempt`/`IngestRequest`). `buildInternal` itself dropped from ~625 hand-written lines to ~25 (session analysis + three phase calls: `scanFiles`/`finishBuckets`/`sortBuckets`, now their own methods on a new `aggState`); no `MetricAggregator` interface (a single-threaded batch loop over one record type has no runtime-polymorphism need). Verified byte-identical `vmr-report.json`/`vmr-requests.json`/both Markdown reports (aside from a live quota counter's time-of-run field) against a real audit corpus before and after. `vmr-report.json`'s JSON shape changes in a few narrow, enumerated ways as a result (see `TrafficStats`'s doc comment, `internal/report/rows.go`): `cache_efficiency` on `by_client`/`workloads` rows and `ok`/`errors`/`tokens_in_cached` on `sessions` rows now use the same omitempty convention as the other row types (previously inconsistent across types); `sessions` rows gain `tokens_reasoning`/`dur_ms_p50`/`slow_requests`, and `hours`/`hours_of_day`/`workloads` rows gain `tokens_reasoning` — all previously-absent, all real values when applicable, never a value change for a field that already existed
- B4 follow-up (review pass): `ClientRow`/`WorkloadRow` had (pre-dating B4) collected `ttfts`/`streamMS` raw samples every record without ever reading them back into an exported percentile — the dead `append`s and the two unexported fields are gone; `finishRow`/`finishHour`/`finishSession` no longer route their TTFT/stream percentiles through a mostly-empty `measuresInput{}` (a `finishMeasures` call built for all five raw-sample dimensions when only two were populated), now calling `percentiles()` directly. No behavior or JSON change — both fields were always unexported/unserialized. `ingest.go`/`recextract.go` (the two files B4 split out of `aggregate.go`) also picked up their own `internal/archtest` file-size budgets, which B4 missed registering at split time
- `internal/report` and `internal/story` each carried their own independently-written `fmtTokens` (dense-table K/M-scaled token count), and `internal/report/detail.go`/`internal/router/logfmt.go` each carried their own `fmtTokensPlain`/`fmtTokensK` for their own display context — four implementations where CLAUDE.md's module map already (incorrectly) claimed one shared `fmtutil.FmtTokens` existed. `report`'s and `story`'s versions differed only by accidental drift (decimal-place count, `report`'s alone having a "B" tier for corpus-wide totals) and now converge on one `fmtutil.FmtTokens`; `detail.go`'s space-unit-letter table format and `logfmt.go`'s always-unit live-log format are genuinely different display conventions, not accidents, so they keep their own names (`fmtutil.FmtTokensPlain`/`fmtutil.FmtTokensCompact`) rather than being forced into one function. `vmr story`'s Markdown tables now render a token count's M/B tier at 2 decimals (was 1) and can show a "B" tier past 1e9 tokens (previously topped out at an "M" figure in the thousands) — a cosmetic difference from `report`'s tables converging onto the same formatter
- `core.WriteJSON`/`core.WriteError` were behavior (HTTP response writing), not a type either analytics- or routing-half package needs — `core`'s zero-internal-dependency contract exists for types both halves must agree on the shape of, not for helpers with exactly two callers (`internal/router`, `internal/server`, both routing-half). Moved to `internal/router` (which `server` already depends on) with a new package-doc admission rule on `core` stating this going forward

## [0.5] - 2026-08-11
### Added
- Quota-Aware Routing P1: single-bucket usage balancing across accounts sharing a virtual model (`requests`/`tokens`/`cost` metrics)
- Quota-Aware Routing P2: account weighting, cost-based pricing, model multipliers
- Multi-currency pricing: per-row/override currency, self-contained supplement exchange rates, `vmr report`'s display-currency rescale
- `vmr replay` now charges its real upstream consumption against quota

### Changed
- Simplified `metric: cost` pricing to static per-model rates; added `ErrContextLimit` failover
- `vmr report` now aggregates quirk-repair marker frequency by endpoint
- Refreshed the LiteLLM pricing snapshot and regenerated the standard price table
- Rewrote README/UserGuide for the router+flight-recorder pitch; reorganized architecture-review and issue-tracking docs
- Bumped `github.com/klauspost/compress` 1.19.1→1.19.2 (zstd concurrency/dictionary fixes relevant to the audit log's compression path) and CI Actions (`checkout`, `setup-go`, `upload-artifact`, `download-artifact`, `action-gh-release`) to their latest majors

### Fixed
- A flaky `internal/quota` store test caused by a period-start mismatch between two charges within the same test (not a bug in the quota package itself)

## [0.4] - 2026-08-05
### Added
- `vmr story`: Findings / decision-spine / corpus-stats / divergence-point layers
- File-content-hash cache for `vmr report`/`vmr story` parsing, plus a `report.yaml` sidecar config
- `-journey` comma/glob selectors; decision spine reworked as per-Step blocks carrying full args and why-lines

### Fixed
- Journey id timezone handling: drop the forced UTC, use the record's own write-time offset
- A shellcheck SC2016 false positive in `vmr.sh`'s `write_env_file`

## [0.3] - 2026-08-04
### Added
- OpenAI Responses protocol support (`POST /v1/responses`) — routing, diagnose, load test, report/story
- Multi-language (EN/ZH) output for `vmr report`/`vmr story`
- Homebrew tap (`bigfatsea/homebrew-tap`) — `brew install` now available

### Changed
- Simplified recovery probing and proxy config: dropped unused passive/global-default layers
- Right-sized response buffer caps to context-window scale

### Fixed
- OpenAI Responses response-side content extraction

## [0.2] - 2026-07-31
### Added
- Condition-based routing and Sticky Model session affinity
- Active health-probe mode
- `vmr story`: Journey/Task/Step narrative rendering, `-compare` LLM interpretation layer, `-render-all`
- Per-provider `role_map` for OpenAI-compatible providers that reject `developer`
- Prebuilt binaries, CI and release workflows

### Changed
- Replaced `vmr report` with a nine-section rewrite: cost estimate, Chat User grouping, ~70% faster runtime, multi-currency time-windowed pricing
- Flattened providers to a multi-protocol list; added model-level capability/context base
- `base_url` must now carry its own API version; dropped the overlap-dedup logic that used to compensate

### Fixed
- Upstream-gateway-failure misclassification
- Image-detection false positives
- Sticky TTL configs that exceed the sticky registry's 24h backstop

## [0.1] - 2026-07-13
First public release.

### Added
- Local-first, single-binary LLM router: point an OpenAI or Anthropic client at one stable virtual model name, hiding providers/accounts/keys/priority/failover behind it
- Byte-faithful passthrough for `POST /v1/chat/completions` (OpenAI) and `POST /v1/messages` (Anthropic) — no cross-protocol translation
- Failover: per-error-class cooldowns, exponential backoff, `Retry-After` respected, single-flight recovery probes; content-policy blocks switch providers without penalizing the endpoint
- Flight-recorder audit log: one JSONL line per request, both layers captured, auto-compressed to `.zst` on rotation, auto-expirable
- `vmr report`: usage/latency/availability stats, session→task→turn grouping, tool-usage report
- Optional inline-image downscaling, content-hash cached on disk, off by default

[Unreleased]: https://github.com/bigfatsea/vmr/compare/v0.5...HEAD
[0.5]: https://github.com/bigfatsea/vmr/compare/v0.4...v0.5
[0.4]: https://github.com/bigfatsea/vmr/compare/v0.3...v0.4
[0.3]: https://github.com/bigfatsea/vmr/compare/v0.2...v0.3
[0.2]: https://github.com/bigfatsea/vmr/compare/v0.1...v0.2
[0.1]: https://github.com/bigfatsea/vmr/releases/tag/v0.1
