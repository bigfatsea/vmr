<!-- Ver 2026-08-05 (rev 4), by Sonnet 5 -->

# vmr — Claude Code project brief

Local-run, single-binary LLM router with a built-in audit/analytics layer (Go) — two
co-equal halves, not routing-plus-an-afterthought (the analytics half's production code
is larger than the routing core's). The routing half hides provider, account, key,
priority, and failover behind a stable Virtual Model name (`coding`, `agent`, `claude`).
Three ingress protocols, **never translated into each other**: `POST /v1/chat/completions`
(OpenAI), `POST /v1/messages` (Anthropic), `POST /v1/responses` (OpenAI Responses) — each
routes only to same-protocol endpoints. Everything else is byte-faithful passthrough.

`vmr report` (aggregate statistics) and `vmr story` (single-task narrative
reconstruction) are the analytics half: read-only, offline consumers of the routing
half's JSONL audit log — neither is on the request path, neither is imported by
`internal/router`/`internal/server`.

**Full design doc (read before any non-trivial change) — `docs/VirtualModelRouter_Design_v4_*.md`,
two main parts (one per half) plus two topic pieces:**
- `_Core.md` (Part 1) — the routing half.
- `_Analytics.md` (Part 2) — the analytics half (`vmr report`/`vmr story`, `internal/{report,story,ctxgraph,chatmsg}`).
- `_Quota.md` — Token-Plan quota-aware routing (`internal/{quota,pricing}` + `router/quota.go`).
  A routing-half subsystem with its own metering/pricing/period model; its only interface to
  Part 1 is one reordering step between `strategy.Sort` and Sticky.
- `_Strategy.md` — the "why": positioning, competitive landscape, and which problems are
  deliberately out of scope. The only one of the four that isn't a "how".

Each has the "why" behind every decision, a decisions-and-tradeoffs table, and (Part 1)
a "decided-not-to-fix" table — check both before "fixing" something that looks odd.
This file only orients; it does not restate any of them.

## Build / test / run

```
go build -o vmr ./cmd/vmr
go test ./...              # add -race for anything touching health/audit/router concurrency
./vmr check -c config.yaml # validate config + print effective routing table (no network I/O)
./vmr start -c config.yaml # foreground
./vmr.sh {start|stop|restart|status|logs}   # dev-mode background supervisor, calls `vmr check` first
```

No Makefile. `.github/workflows/ci.yml` runs three jobs on push/PR: `go vet`/`go build`/
`go test -race` on an ubuntu+macOS matrix, `gofmt -l`, and `shellcheck` over `vmr.sh`/
`vmr-loadtest.sh`. `release.yml` cross-compiles 4 platforms and publishes a GitHub Release
on a `v*` tag. `vmr.sh` never runs `go build` itself — build first.

## Module map (`internal/`)

| Package | Owns |
| --- | --- |
| `core` | `CanonicalRequest`, `ErrorClass`, `Endpoint` (with `Freeze()` — precomputes `HealthKey()`/`Name()` once in `router.BuildSnapshot`; unfrozen literals still work, just uncached) — shared types, no internal deps. Also `FilterClientHeaders` (header blocklist — `server` and `replay` both call this, neither owns it), and Quota-Aware Routing's runtime-shape types (`QuotaSpec`/`Limit`/`TokenWeights`, plus P2.2's `Rate`/`PricingOverride`/`PricingSpec` on `Endpoint.PricingRate`) — plain data only, so `internal/pricing`'s resolution logic (which `core` cannot import, being zero-dep) produces these as its output type rather than `core` depending back on it. `endpointlabel.go` owns the audit log's `protocol:provider:model` label format (`EndpointLabel`/`SplitEndpointLabel`) — deliberately a *different* format from `Endpoint.Name()`'s `/`-joined display identity, and the two must not be unified: the label is an on-disk contract every historical record already carries, `Name()` is free to change shape |
| `fmtutil` | `FmtBytes`/`FmtTokens`/`FmtSeconds`/`FmtPercent` — display formatting shared by `router`'s live log and `report`'s rendering; split out of `core` so neither has to depend on routing-domain types just to print a number. `CapStr` (UTF-8-safe byte truncation, backs up to the nearest rune boundary rather than cutting mid-sequence) landed here rather than in a domain package precisely because it carries no domain knowledge — used for both display (TraceID truncation, response-text previews) and non-display truncation (`report`'s compaction needle matching, `taskseg`'s dialect-detection prefix check). Also `DisplayZone` (= `time.Local`) — the one conversion point every human-facing timestamp rendering/aggregation must go through, see the timezone invariant below |
| `config` | YAML load, `${ENV}` expand, strict validation (`KnownFields`), hot-reload watch. `quota.go` holds `Provider.Quota`'s YAML shape (`QuotaConfig`/`LimitConfig`, `metric: requests`\|`tokens`\|`cost`, plus P2.1's `token_weights`/`model_multipliers`) and its validation — still exactly one `Limit` per provider; multi-window, `rolling`, and `models:` scope remain an explicit "planned for a later batch" load error, never a silent no-op (that's P3). `pricing.go` holds the global `pricing:` block and `Provider.Pricing`'s YAML shape, and `resolvePricing` (called from `validate()`): resolves every `metric: cost` provider's pricing via `internal/pricing`, requiring `pricing.Complete` (the fully-priced rate, not just the common-case one) before accepting the config, and rejects any `overrides` entry `firstDeadOverride` finds unreachable (first-match-wins with no time dimension left — see `internal/pricing`'s row below), and stores the result in `Config.ResolvedPricing` for `router.BuildSnapshot`; also builds `Config.ProviderPricingPolicies`/`PricingTable()` for `vmr report`'s broader, best-effort resolution (every provider with a `pricing:` block, not just cost-metric ones). `PricingConfig.ExchangeRate` is a general "1 USD = X `<code>`" map, not a single USD-keyed escape hatch — it resolves the target `pricing.currency`, any `pricing.supplement`/`pricing.standard` row's own `currency:`, and any `providers[].pricing.overrides` row's own `currency:`, all via the same USD pivot (`pricing.FactorBetween`) |
| `jsonscan` | Zero-dependency JSON byte-range scanning engine (extracted from `internal/adapter`'s `classify.go`/`fingerprint.go` by the architecture review's B1 batch): `RewriteModel`/`RewriteStream`/`RewriteRoles`/`RewriteInputRoles` (top-level `model`/`stream`/`role` field byte-splice rewrites — the mechanism behind `Adapter.BuildRequest`'s model-name rewrite and `role_map`) + the structural primitives they and `internal/adapter`'s `SessionFingerprint`/`TopLevelProbe` both call (`TopLevelValues`/`WalkArrayElements`/`FirstArrayElement`/`ElementRole`/`SkipJSONWS`/`SkipJSONString`/`SkipJSONValue`/`IndexUnescapedQuote`) + `MarshalNoEscape` (moved from `core`). Fuzz-tested (`go test -fuzz`): both the rewrite functions and the scanners' own bounds/non-overlap contract. `internal/adapter` keeps only genuine protocol-domain logic (`DefaultClassify`'s error sniffing, `SessionFingerprint`/`TopLevelProbe`'s field/role semantics) — judged by whether a function needs to know a specific field or role name: if not, it belongs here, not in `adapter`. Zero internal deps (`archtest`-enforced) |
| `adapter`, `adapter/openai`, `adapter/anthropic`, `adapter/openairesponses` | `Adapter` interface (compile-time registered via blank import, lock-free `atomic.Pointer` registry) + shared error classification (`classify.go`'s `DefaultClassify`) — the model-name/role byte-splice rewrite itself lives in `internal/jsonscan` (see that row), called from each protocol adapter's `BuildRequest`. `openairesponses` is the `POST /v1/responses` passthrough adapter (OpenAI Responses and its OpenAI-compatible reimplementations) — same contract as `openai`'s, own request/response shape (`input`/`instructions` in, typed `output[]` items out) |
| `health` | Passive state machine: cooldown, backoff, half-open single-flight |
| `probe` | Minimal echo-nonce request used by both background recovery probes and `vmr diagnose` |
| `strategy` | `Dimension` (ordering, e.g. priority) + `Condition` (elimination, e.g. image/tools capability, lock-free `atomic.Pointer` registry) — two separate interfaces, don't merge them |
| `sticky` | Session-affinity registry (prompt-cache stickiness); its `BackstopTTL` is an alias for `core.StickyBackstopTTL` (canonical value lives in `core` so `config` can validate against it without importing `sticky`) |
| `quota` | Quota-Aware Routing's accounting half (see `docs/VirtualModelRouter_Design_v4_Quota.md`, including its "现状与后续计划" section for what's actually shipped): `Counters`/`Registry` (`quota.go` — `Charge`/`Used`, keyed by provider *name*, deliberately not the API key hash `core.Endpoint.HealthKey()` uses, so rotating a key never resets the current period's count; lazy period reset, no ticker; `Counters.Cost` — P2.2's one exception to "store raw, weight on read": a `metric: cost` charge's $ amount is computed once, in `internal/router/quota.go`'s `ChargeResponse`, and frozen into this field via an ordinary `Charge` call (`AddEstimatedCost` separately tracks how much of it came from a degraded estimate), since the price table it's computed from can itself change over time), `period.go` (`PeriodStart`/`PeriodEnd`, calendar-aware `h`/`d`/`w`/`mo` stepping with month-end clamping), `score.go` (`Headroom`/`ScoreForLimit` — the dimensionless `(1-used_frac)/time_left_frac` ratio the design doc's Core Algorithm section derives), `weight.go` (`BaseAmount`/`ApplyModelMultiplier`/`EstimatedPct` — base(metric), the model-multiplier scale, and the degraded-estimate share, moved here from `internal/router/quota.go` so an offline reader can apply the exact same formula the router charges with, not a re-derivation of it; see `docs/future-strategy/vmr_quota_visibility_devplan_opus-5.md`'s batch 1), `store.go` (atomic `vmr-quota.json` persistence, same temp-file+rename pattern as `imgprep`, plus `LoadFile`/`Bucket` — a lock-free, no-Registry-construction read of that same file for an offline consumer like `vmr report`). Depends only on `core` + stdlib; `config`, `router`, and `report` all import it, it imports none of them |
| `pricing` | Quota-Aware Routing's pricing resolution engine (P2.2 — see `docs/VirtualModelRouter_Design_v4_Quota.md`'s pricing sections): `Rate`/`Table` (`pricing.go` — per-1M-token four-component prices; a nil component means "unknown", never "free", the distinction the whole package is built around), `FactorBetween` (`pricing.go` — pivot-through-USD currency conversion; deliberately not a general arbitrary-currency graph, since every table this package loads is USD-native) plus `fileRate`'s per-row `currency:` (`ParseTableWithRates` normalizes a non-USD supplement row to USD at parse time, before it ever reaches `Resolve`/`EffectiveRate` — the discount-chain recursion below stays currency-unaware on purpose; `fileTable.ExchangeRate` lets a supplement/standard-override file declare its own rate table that wins over the caller-supplied one on a matching key, so the file stays portable across deployments instead of silently depending on whatever `config.yaml` happens to be merging it into), `resolve.go` (`Resolve`/`EffectiveRate`/`Complete` — the three-layer account-override → supplement∪standard → none resolution, plus the 4-step canonical-key auto-resolution; no time dimension — a static, first-match-wins per-model `OverrideRule` list, deliberately dropped the date/hour promotional-window functionality it originally shipped with, see `docs/VirtualModelRouter_Design_v4_Quota.md`'s pricing sections — `EffectiveRate`'s discount-form handling is recursive — "the rate below this rule" can be another override, not always the table `Base`, matching the design doc's literal "discount × 下层解析出的费率" wording), `embed.go` (`go:embed` of `standard_price_generated.yaml`/`standard_price_curated.yaml`, `LoadStandard`), `resolver.go` (`Resolver` — memoized per-provider `RateFor`, the shape `vmr report`'s per-record resolution needs against a large audit log; `WithDisplayFactor` layers `vmr report`'s `-currency` display-currency rescale on top — a purely cosmetic final multiply, never touching the accounting-currency resolution above it). `tools/gen_standard_pricing` regenerates `standard_price_generated.yaml` from `docs/data/model_prices_and_context_window.json` (LiteLLM snapshot, MIT licensed); `standard_price_curated.yaml` is hand-maintained (a handful of domestic first-party vendor rows the generated snapshot doesn't cover), never touched by the generator. Depends only on `core` + stdlib + yaml.v3; both `config` and `report` import it (two independent consumers — `config` for `metric: cost`'s strict load-time resolution, `report` for best-effort $ estimates — see the package's own doc comment) |
| `router` | Failover loop (`router.go`: `Serve`/`tryOne`/`handleErrorResponse`/`forwardSuccess`, the part that must stay small) + `reload.go` (hot-reload outcome + config staleness, for `/admin/status`) + `routehdr.go` (`X-VMR-Route-Reason`/`X-VMR-Failover`) + `snapshot.go` (`BuildSnapshot`/`Install`) + `limiter.go` (concurrency gate) + `transport.go` (`NewUpstreamClient`, streaming forward) + `logfmt.go` (live log line formatting) + `response.go` (response normalization: event splitting, model-name rewrite, `[DONE]` policy, buffered/passthrough decision, plus Quota-Aware Routing's usage-sniffing/byte-counting hooks behind a dedicated `respStream` mutex) + `responsefix.go` (MiniMax-specific quirk knowledge — `<think>`/Thinking-Process stripping, soft-block markers — `response.go` calls into it, never the other way) + `quota.go` (Quota-Aware Routing's decision half: `chargeQuota`/`tokenCharge` sniffing a successful response's usage off `respStream`, then handing it to the exported `ChargeResponse` — metric dispatch + `model_multipliers` scaling + `componentCost` cost pricing through `ep.PricingRate` via `pricing.EffectiveRate` (a deterministic function of the resolved override chain, no time dimension), never re-priced later; `ChargeResponse` is factored out specifically so `internal/replay` can drive the same pipeline from an already-buffered response instead of reimplementing it — `reorderByQuota` reordering same-tier candidates by headroom score between `strategy.Sort` and Sticky, `QuotaStatus` for `/admin/status`; `TokenCounters` (exact-vs-degraded token split from a sniffed/unsniffed usage — the exact-vs-degraded charge decision's one implementation, called directly by `tokenCharge`, `internal/replay`'s `chargeReplay`, and reproduced report-side by `internal/report/tokenest.go`'s `estimateDegradedTokens`, never re-derived independently) |
| `server` | HTTP entry, auth, audit recording, `facts.go` (RequestFacts extraction), `admin.go` (the loopback-only `/admin/status`: process identity, config freshness, reload outcome) — header blocklist lives in `core.FilterClientHeaders`, not here |
| `audit` | JSONL audit log (one line per request, two layers: client↔vmr, vmr↔upstream) + zstd compression/retention. `Write` encodes into a pooled buffer *outside* its lock — don't move that encode inside the lock, it would serialize JSON-encoding potentially-multi-MB records across concurrent requests |
| `diagnose`, `replay` | `vmr diagnose` (real connectivity check) / `vmr replay` (resend one audit record) — reuse the same `adapter.BuildRequest`/`router.NewUpstreamClient` real traffic uses; `replay` also charges quota on a successful response through `router.ChargeResponse` (`router`'s `quota.go` row), usage extracted via `chatmsg.MergeUsageBytes` over its own fully-buffered response instead of `respStream`'s incremental sniffing |
| `imgprep` | Inline image downscale + disk cache |
| `buildinfo` | Build identity from Go's own VCS stamp (`debug.ReadBuildInfo`) — no ldflags, no generated file. Used by `vmr version` and `/admin/status`'s `instance.version` |
| `rundir` | Default dir resolution formula (`~/.vmr` → temp → cwd), shared by log/cache dirs |
| `archtest` | Executable architecture invariants: import boundaries, per-file line-count budgets (a hand-registered whitelist), and per-function line budgets (`func_sizes_test.go` — a *global* default with an explicit exemption table, so an unregistered new function is bounded too; `internal/i18n` is exempt as string tables rather than control flow) |
| `chatmsg` | Shared message/SSE/usage parsing (`Messages`, `ReassembleSSE`/`FinalMessage`, `ExtractUsage`, `CheckToolPairing`, `ExtractEntities`, `ToolResultList` — `CheckToolPairing`'s content-carrying counterpart: which `tool_call` got which `tool_result` text/error status, not just that they paired) — the one package `ctxgraph`/`story`/`report` all depend on, so none of the three re-implements the same parsing |
| `ctxgraph` | Content-addressed manifest/blob-index layer behind both `vmr report` and `vmr story`: `Manifest` (per-request message-hash vector), `Classify` (5-way edit classification: Append/ReplaceTail/Splice/Contract/Fork), `Lineage`/`Scan` (splits a SessKey bucket at every Contract/Fork), `StitchGraph`/`ChainFrom` (cross-lineage reconnection). `ScanCached`/`FileCache` (`cache.go`) is `Scan` plus a file-content-hash-keyed cache of already-parsed Manifests — skips the expensive parse for an unchanged file, never skips a file from the graph itself (predecessor search has no time bound, so narrowing which files feed the graph rebuild isn't safe — see the design doc's `vmr-stories.json` section); `report`/`story` each persist a `FileCache` inside their own index artifact (`vmr-requests.json`'s/`vmr-stories.json`'s `files` field) rather than a separate cache file. Depends on `{audit, core, chatmsg}`; never depends on `report`/`story`/`router`/`server` (`archtest`-enforced) |
| `taskseg` | Agent-dialect `Profile` interface (`RealUserText`/`NoReply`/`ChatID`, `taskseg.go`) shared by `report`'s `session.go` and `story`'s `journey.go` — a top-level leaf, not a subpackage of either, since both depend on it (architecture review's B2 batch merged what used to be `story`'s own private `story/profile` package with a byte-identical copy `report`'s `session.go` carried independently). Two implementations: `OpenClawAware` (the one real-corpus-validated dialect — routing-envelope stripping, tool-result-only rejection, the `NO_REPLY` skip marker, `chat_id` extraction) and `Generic` (template-free fallback: any non-empty user text is real, no skip-marker convention). Deliberately no Detect-based registry yet — a second real profile earns one when real corpus differences justify it, not before. `cmd/vmr`'s `resolveTaskProfile` is the one place both `vmr report`/`vmr story` resolve which `Profile` to use — not two independent defaults. `segment.go` (B3) is the session/task-segmentation algorithm itself, generic over `Profile` rather than agent-specific on its own: `RealUsers`/`IndexRealUsers` (one real-instruction index per request, built once and threaded through the rest instead of each consumer re-running `RealUserText`), `HasNewInstruction`/`LastInstruction` (the delta-scoped new-instruction check and task-title text, both reading that index), `ManifestKeySet`, `IsNewTask` (the one `traceChanged || (!prevNoReply && hasNewInstr)` task-boundary rule), `TaskTitle`, `ResponseSummary`, `Preview` — converged from independent byte-identical-in-spirit implementations `report`/`session.go` and `story`/`journey.go` used to each carry. Depends on `{chatmsg, fmtutil, ctxgraph}` (`ctxgraph` added in B3 for `Hash`/`Manifest`; `fmtutil.CapStr` backs the scaffolding-prefix head check in `openclaw.go`); never depends on `report`/`story`/`router`/`server`/`config` (`archtest`-enforced) |
| `story` | `vmr story`: Journey/Task/Step/Event narrative built from a `ctxgraph` lineage chain (`journey.go`), nine-indicator behavior profile (`metrics.go`, also home to `toolCallRepeats` — the shared exact-repeat-call basis for `DuplicateActionRate`, the `exact_repeat_tool_call` Finding, and the 🔄 retry tag/timeline symbol), rule-derived Step-level "suspect list" Findings — `findings.go`'s `ComputeFindings` (five detectors) plus `findings_toolresult.go`'s four more built on `chatmsg.ToolResultList` (precise tool_call↔tool_result pairing) — same `Code`/display-text-separation pattern as `report.Finding` but Step-located rather than aggregate; report/story `Finding` types are intentionally not shared, `archtest`-enforced. The decision-spine presentation layer on top of it (`render_spine.go`: overview card, per-Task action list, per-Step role tag, ASCII tool-call timeline — pure formatting, no new data), Journey-vs-Journey diff (`compare.go`, `ComparisonExtras`'s rule-derived endpoint/cache/system-prompt/final-context/duration/deliverable facts, plus `computeDivergence`'s structural divergence-point detection — first Step position where two Journeys' tool-use structure differs, light/heavy severity, never a root-cause claim), corpus-level statistics across many Journeys (`corpus.go`/`render_corpus.go`: metric distributions, Finding hit rates, Spearman correlations, Finding-grouped comparisons — `vmr story -corpus`), Markdown rendering (`render_md.go`/`render_compare.go`/`render_corpus.go`). `llm.go` is the optional, always-degradable LLM interpretation layer (`-llm-addr`/`-llm-model`/`-llm-key`/`-llm-dry-run`) shared by three evidence-pack shapes (`EvidencePack` for `-compare`, `llm_single.go`'s `SingleJourneyEvidencePack` for `-journey`, `llm_divergence.go`'s `DivergenceEvidencePack` for `-compare`'s divergence-point section) via a generic `Interpret[T evidencePackKind]` — a plain `net/http` client against a manually-specified already-running VMR instance, no config.yaml provider resolution, no failover. Agent-dialect recognition AND the session/task-segmentation algorithm itself (real-instruction indexing, new-task detection, task titling) come from `internal/taskseg`, not a private implementation — see that row. Depends on `{audit, ctxgraph, chatmsg, core, fmtutil, i18n, taskseg}` — notably **not** `config`, which is why `llm.go` takes a manually-specified address instead of resolving a provider; never depends on `report`/`router`/`server` |
| `report` | `vmr report`: aggregates audit JSONL into `vmr-report.{json,md}` + `vmr-requests.{json,md}` (the `.json` doubles as a `ctxgraph.FileCache`-backed parse cache, see `ctxgraph`'s row) + per-request `details/*.{md,json}`. Data shape in `rows.go` (= the vmr-report.json schema), aggregation pass in `aggregate.go`, session/task grouping in `session.go` (consumes `ctxgraph.Lineage`/`Classify` directly — no private hash/LCP implementation of its own), rendering split `render_doc.go` (running order + `mdTable`) + one `section_*.go` per report section — **a new section is a new file, not more lines in an existing one** (`archtest` budgets enforce it). `cost.go` holds `costFor` (the base(cost) formula, all four components including `cache_read` — P2.2 fix, see that function's comment) and endpoint-label parsing. `pricing.go` (P2.2) is now a display-only summary type (`Pricing`: currency, standard-table generation date, override count) plus `Disclaimer()` — the actual resolution engine is `internal/pricing`'s `Table`/`Resolver`, threaded through `Build`/`BuildCached`'s `pricingSrc` parameter; `cmd/vmr/cmd_report.go`'s `buildPricing` is the composition root that reads `config.yaml` and builds both, since `report` itself must never import `config` (absent/unreachable config.yaml degrades gracefully to the embedded standard table's list prices only, never blocks the rest of the report). `providerquota.go` (P2, quota visibility batch 2) is §2.5's "额度与消耗对照" sub-table: `buildProviderQuotaRows` recomputes this report run's own window consumption from `EndpointsAll` through `internal/quota`'s `BaseAmount`/`ApplyModelMultiplier` (the same formula the router charges with) and places it next to `ProviderQuotaRef.Live` — the router's real-time `<log_dir>/vmr-quota.json` counter, resolved by `cmd/vmr/cmd_report.go`'s `buildProviderQuotas` (same composition-root pattern as `buildPricing`) — the two numbers are deliberately never combined into one, see `ProviderQuotaRow`'s doc comment (`rows.go`). `findings_quota.go`'s `quotaExhaustionFinding` (batch 3) is the one `§7` Finding sourced from live data only, never this report's own recomputed estimate. `tokenest.go`'s `estimateDegradedTokens` is this report run's own reproduction of the router's degraded (non-usage-sniffed) token estimate — same `Facts.EstimatedTokens`/`core.EstimateTextTokens` basis `router.TokenCounters` charges with, threaded into `ProviderQuotaRow.WindowEstimatedPct` (§2.5) so a window mixing sniffed and estimated usage reports its estimated share instead of a systematically-low precise-looking number; falls back to `core.EstimateTextTokens` over the recorded request body when a record's `Facts` is nil (the one case that happens in practice: `internal/replay`-produced records, which never populate `Facts`), matching `chargeReplay`'s own degraded basis exactly. `session.go`'s agent-dialect recognition (real-instruction/no-reply/chat_id judgment) AND its session/task-boundary primitives (real-instruction indexing, new-task detection, task titling) come from `internal/taskseg`, shared with `story` rather than a private implementation. Depends on `{audit, core, ctxgraph, chatmsg, pricing, quota, taskseg}` (the `quota`/`taskseg` dependencies are the same "two consumers, one implementation" precedent `pricing` itself set — both are leaf packages, not in `archtest`'s forbidden-import list) — an `internal/archtest` test enforces it never depends on `router`/`server`/`config` |
| `i18n` | English/Chinese text for every `vmr report`/`vmr story` output string, organized by which produced file each source file's text feeds (`report_*.go` next to `internal/report/section_*.go`, `story_*.go` next to `internal/story/render_*.go`) rather than one catalog directory — wording changes stay next to the section they render. Zero dependencies, same layer as `core`/`fmtutil`. `Lang` zero value is `EN`. Consumed by `report`, `story`, and `cmd/vmr` |

`cmd/vmr/` is the CLI (stdlib `flag`), one file per subcommand: `main.go` (dispatch + usage +
the adapter blank-import registration point), `cmd_start.go`, `cmd_check.go` (also handles a
trailing `log`/`cache` argument — absorbed from the former standalone `cmd_dirs.go`),
`cmd_status.go`, `cmd_diagnose.go`, `cmd_replay.go`, `cmd_version.go`, `summary.go`
(config-summary rendering shared by start/check), `cmd_report.go`, `cmd_story.go`, and
`auditpaths.go` (`resolveInputPaths` — the `<file|glob>...` positional-argument convention,
including the fallback to `<config's log_dir>/vmr-audit-*`, shared by `cmd_report.go`/`cmd_story.go`).

`vmr.sh` owns `start/stop/restart/status/ps/logs` + `service *`; every other subcommand is
forwarded verbatim to the binary (not a whitelist — see the script's `passthrough`).

## Invariants to not accidentally break

- **Byte-faithful passthrough.** No canonical IR, no cross-protocol translation. Only
  three sanctioned deviations: model-name rewrite (virtual ↔ real), a short list of
  evidence-based provider quirk repairs (see Part 1's "Response-side normalization"
  section) — each behind a strict content-based guard, fail-open to "unmodified" on
  any doubt — and `imgprep`'s inline-image downscale, which is the largest of the
  three (a full unmarshal/rewrite/re-marshal, not a byte splice) and only fires when
  `image_downscale` is configured and an image actually needs shrinking; every other
  request path returns the original bytes untouched.
- **No provider SDKs.** Routing only needs URL/key/model-field changes.
- **Compile-time plugin registration only** (blank import), never a runtime plugin system.
- **Config: strict YAML** (`KnownFields`) — unknown keys are load errors, not warnings.
- **`Condition` (elimination) vs `Dimension` (ordering) are separate interfaces** — do not
  add a request parameter to `Dimension.Compare`.
- Adapters never touch response bodies; response normalization lives only in
  `internal/router/response.go` + `responsefix.go`.
- Audit files are 0600 / `reports/` output (including `reports/stories/` from `vmr story`)
  is 0600/0700 — derived report/story artifacts must not loosen that (they carry full
  conversation bodies).
- **Timezone: one display authority, raw data stays untouched, payload timestamps are
  opaque.** Everything human-facing — Markdown/CLI output, aggregation bucketing, and
  filenames alike — renders through `fmtutil.DisplayZone`, never a hardcoded zone;
  persisted records keep their original write-time offset as-is. A timestamp inside an
  LLM/tool payload (messages, model output, tool args/results) is the agent layer's
  content, not vmr's — never parsed or converted, same as any other passthrough byte.
  `internal/story/journey.go`'s `deriveID` is the one documented exception (an id needs
  to reproduce identically regardless of which machine derives it) — see its comment and
  `docs/future-strategy/vmr_timezone_analysis_sonnet-5.md` before "fixing" it.
- `internal/report` is coupled to `audit.Record`'s shape at compile time — changing the
  audit record structure requires updating `internal/report` and its tests in the same change.
- `ctxgraph`/`chatmsg` are the one shared source of truth for message-hashing and message
  parsing respectively — `report`/`story` must consume them, not grow their own private
  copy. A session/lineage grouping bug class lives specifically in *not* doing this:
  a private hash/LCP implementation can silently disagree with `ctxgraph` on where a
  conversation's history actually got reset.
- Registries populated only from `init()` (`adapter.registry`, `strategy.conditions`) use a
  lock-free `atomic.Pointer` read path with a mutex-guarded copy-on-write *write* path — if you
  add a similar registry, guard the write with a mutex too, not just the atomic swap; a bare
  copy-on-write without one silently loses updates under concurrent writers (caught by
  `TestGetConcurrentWithRegister`/`TestEligibleConcurrentWithRegisterCondition` under `-race`).
- `internal/archtest` enforces import boundaries, per-file line budgets, and per-function line
  budgets — run it (`go test ./internal/archtest/...`) after any package-boundary change, any
  `internal/router` file change, or any edit that grows a function. When it trips, split the
  file/function; raising the number in the table is what the failure message tells you not to do.
- **An analytics-half number that claims to reproduce a routing-half number must be pinned by a
  differential test, not by a comment.** Sharing the *formula* (as `internal/quota`'s
  `BaseAmount`/`ApplyModelMultiplier` do between `router` and `report`) is only half of it — the
  *basis* fed into that formula is chosen independently on each side, and a wrong basis reads
  exactly like a right one. `cmd/vmr/quota_parity_test.go` is the pattern: drive both sides over
  the same synthetic records and assert equality — all three quota metrics are covered there, and
  the router side must call the router's own exported entry points (`ChargeResponse`,
  `TokenCounters`), never restate their formulas, or the test re-derives the very thing it exists
  to pin. It lives in `cmd/vmr` because `archtest` forbids
  `report`→`router`, and the composition root is the only place allowed to see both halves.

## Conventions

- **This file holds principles, not implementation detail.** State the rule and point at
  where it's enforced (a file, a function, a design doc); don't restate call sequences,
  exact function names, or file lists that already live in the code — those drift out of
  sync with this file the moment the code changes, and a stale detail here is worse than
  no detail, since it reads as authoritative. If a bullet needs a paragraph to justify
  one line of rule, the justification belongs in a code comment or design doc, linked
  from here.
- Comments: only for non-obvious "why" (hidden constraint, workaround, invariant) —
  match the existing terse style, don't add narration.
- Commit messages: short, imperative, no trailer boilerplate (see `git log --oneline`).
- Chinese-language docs live under `docs/`; this file and code comments are English.
- **Every repo-root `*.example.yaml` ships a `*.example.zh.yaml` sibling** (`config.example.yaml`
  / `config.example.zh.yaml`, `report.example.yaml` / `report.example.zh.yaml`,
  `pricing.example.yaml` / `pricing.example.zh.yaml`) — same YAML keys/structure/example
  values, only the comments translate. These are user-facing config templates a
  Chinese-reading operator copies and edits directly, unlike code comments (English-only,
  see above). When either sibling's content changes, update the other in the same change —
  don't let them drift out of parity.
- Before treating something as a bug: check Part 1's "已识别、暂不落地的清理项" (decided-
  not-to-fix) table and Part 2's "已知限制、暂不处理的事项" table — some odd-looking
  behavior is a documented, deliberate non-fix.
- **No section numbers in cross-references** — in code comments and between docs, cite the
  document name and the section's name ("`docs/..._Core.md`'s Sticky Model section", "the
  decided-not-to-fix table"), never a bare number (`§6.5`, `Appendix C.6`, `F9`). A vaguer,
  name-based reference is *more* durable, not less: numbers renumber every time a doc is
  edited and then silently point at the wrong thing, while a name or a short description
  keeps resolving even after the doc is reorganized. Existing code was swept once for this
  (2026-08); keep new comments to the same rule rather than reintroducing numbers.
- **Docs are current state, not changelogs** — when a doc changes, replace the stale part
  with the new fact; don't append a dated paragraph explaining what changed and why on top
  of the old text. Layer-on-layer accretion preserves the story but defeats the point of a
  concise reference doc, and git history already holds that story. If losing the old
  wording would confuse a future reader, compress it into one short clause instead of
  keeping it in full. This does not apply to documents whose whole purpose is the trail —
  review reports, `docs/KNOWN_ISSUES_*.md`, `CHANGELOG.md`, and code comments recording why
  a superseded approach was wrong are expected to accumulate.
- **`CHANGELOG.md` (Keep a Changelog format) is the source of truth for release notes** — add
  entries under `[Unreleased]` as user/dev-relevant changes land (skip pure dependency-bump
  and doc-churn commits; group under `Added`/`Changed`/`Fixed`). When cutting a release,
  retitle `[Unreleased]` to `## [X.Y] - YYYY-MM-DD` matching the tag about to be pushed (drop
  the leading `v`), add a fresh empty `[Unreleased]` above it, and add the two link-reference
  lines at the bottom. `release.yml`'s release job extracts that section and uses it as the
  GitHub Release body instead of GitHub's auto-generated commit list — the latter is unusable
  here since most commits land directly on `main` rather than through a PR. The release job
  fails on purpose if the pushed tag has no matching section, rather than publish an empty
  release — write the `CHANGELOG.md` entry (and commit it) before tagging, not after.
