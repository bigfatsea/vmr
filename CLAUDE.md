<!-- Ver 2026-08-15 14:30, by gemini-3.7-flash -->

# vmr — Claude Code project brief

Local-run, single-binary LLM router with a built-in audit/analytics layer (Go), built as
**two co-equal halves** — the analytics half's production code is larger than the routing
core's, and neither is an afterthought to the other.

- **Routing half** — hides provider, account, key, priority, and failover behind a stable
  virtual model name (`coding`, `agent`, `claude`), with error-aware failover,
  prompt-cache session stickiness, and token-plan quota pacing.
- **Analytics half** — `vmr analyze` (unified entry point producing the full navigable suite, or zooming into specific views with `-journey`/`-compare`/`-corpus`; `vmr report` and `vmr story` retained as deprecated transition aliases): read-only, offline consumers of the routing half's JSONL audit log. Neither is on the request path.

Three ingress protocols, **never translated into each other** — `POST /v1/chat/completions`
(OpenAI), `POST /v1/messages` (Anthropic), `POST /v1/responses` (OpenAI Responses). Each
routes only to same-protocol endpoints. Everything else is byte-faithful passthrough.

**Design docs — read the relevant one before any non-trivial change.** Each carries the
"why" behind every decision plus a decisions-and-tradeoffs table; Parts 1 and 2 also carry
a **decided-not-to-fix table** — check it before "fixing" something that looks odd. This
file only orients; it does not restate them.

- `docs/VirtualModelRouter_Design_v4_Core.md` (Part 1) — the routing half.
- `docs/VirtualModelRouter_Design_v4_Analytics.md` (Part 2) — the analytics half.
- `docs/VirtualModelRouter_Design_v4_Quota.md` — token-plan quota-aware routing. A routing-half
  subsystem with its own metering/pricing/period model; its only interface to Part 1 is one
  reordering step between `strategy.Sort` and sticky pinning.
- `docs/VirtualModelRouter_Design_v4_Strategy.md` — the "why": positioning, competitive
  landscape, and what is deliberately out of scope. The only one of the four that isn't a "how".
- `docs/KNOWN_ISSUES_sonnet-5.md` — the cross-cutting index of open issues and deliberate
  non-fixes (the design docs' own tables hold the per-subsystem detail). Check it before
  reporting something as new — and **register there when you decide something too**: a
  tradeoff argued only in a source comment is not tracked. The next reviewer reads the
  accused line, never finds the comment that answers it three files away, and re-proposes
  the thing you already settled.

## Build / test / run

```bash
go build -o vmr ./cmd/vmr
go test ./...              # add -race for anything touching health/audit/router concurrency
./vmr check -c config.yaml # validate config + print effective routing table (no network I/O)
./vmr start -c config.yaml # foreground
./vmr analyze              # full navigable analytics suite (macro report + task journeys)
./vmr.sh {start|stop|restart|status|logs}  # dev supervisor; never builds, build first
```

No Makefile. CI (`.github/workflows/ci.yml`) runs `go vet`/`go build`/`go test -race` on an
ubuntu+macOS matrix, plus `gofmt -l` and `shellcheck` over `vmr.sh`/`vmr-loadtest.sh`.

## Module map (`internal/`)

Leaf packages (zero internal dependencies, `archtest`-enforced):

| Package | Owns |
| --- | --- |
| `core` | Types both halves must agree on: `CanonicalRequest`, `Endpoint`, `ErrorClass`, `QuotaSpec`, `PricingSpec`, `RequestFacts`, plus the audit log's `protocol:provider:model` label (`EndpointLabel`). Its package doc states the admission rule — read it before adding anything here |
| `fmtutil` | Display formatting (`FmtBytes`, `FmtTokens`/`FmtTokensPlain`/`FmtTokensCompact`, `FmtSeconds`, `FmtPercent`), UTF-8-safe `CapStr`, and `DisplayZone` |
| `jsonscan` | JSON byte-range scan/splice engine: `RewriteModel`/`RewriteStream`/`RewriteRoles` and the structural primitives behind them. Fuzz-tested. A function belongs here only if it needs no specific field or role name — otherwise it belongs in `adapter` |
| `i18n` | EN/ZH text for every analytics-half output string, one file per produced section — `i18n/report_*.go` sits next to `internal/report/section_*.go`, `i18n/story_*.go` next to `internal/story`, `i18n/reqdetail_detail.go` next to `internal/reqdetail`, so a wording change stays next to the section it renders. `Lang` zero value is `EN` |

Routing half:

| Package | Owns |
| --- | --- |
| `config` | YAML load, `${ENV}` expansion, strict validation, hot-reload watch. Also resolves `metric: cost` pricing through `internal/pricing` at load time — a `metric: cost` provider whose rate can't be fully resolved is a config error, not a runtime surprise |
| `adapter`, `adapter/{openai,anthropic,openairesponses}` | `Adapter` interface (compile-time blank-import registry) + shared error classification (`DefaultClassify`) + protocol-domain field/role semantics (`SessionFingerprint`, `TopLevelProbe`) |
| `strategy` | `Dimension` (ordering) + `Condition` (elimination) — two separate interfaces |
| `health` | Passive state machine: cooldown, backoff, half-open single-flight |
| `sticky` | Session-affinity registry for prompt-cache stickiness |
| `probe` | Minimal echo-nonce request shared by background recovery probes and `vmr diagnose` |
| `quota` | Quota accounting: `Counters`/`Registry` keyed by provider *name* (rotating a key must not reset the period), calendar-aware periods, headroom scoring, atomic `vmr-quota.json` persistence |
| `pricing` | Three-layer rate resolution (account override → supplement/standard table → unpriced). A nil rate component means *unknown*, never *free* — the whole package is built around that distinction |
| `respnorm` | Response stream normalization: `Wrap` + the buffered/passthrough state machine, model rewrite, SSE splitting, `[DONE]` policy, and the evidence-based vendor quirk repairs. Quota usage sniffing lives here too — a documented tradeoff, see the package doc |
| `router` | Failover loop (`Serve`/`tryOne`), snapshot build/install, concurrency limiter, upstream transport, live log formatting, quota charge dispatch (`ChargeResponse`/`TokenCounters`), and the routing-half HTTP behavior it shares with `server`/`replay`: `FilterClientHeaders` (client-header blocklist) plus `WriteJSON`/`WriteError` |
| `server` | HTTP entry, auth, `RequestFacts` extraction, audit recording, loopback-only `/admin/status`, unauthenticated `/health` (liveness only — it must never grow an instance field, or it becomes an open `/admin/status`) |
| `audit` | JSONL audit log (two layers per request: client↔vmr, vmr↔upstream) + zstd compression/retention |
| `imgprep` | Inline image downscale + disk cache |
| `diagnose`, `replay` | `vmr diagnose` / `vmr replay` — both reuse the same `Adapter.BuildRequest`/`router.NewUpstreamClient` real traffic uses, so what they show is byte-identical to what would really happen |

Analytics half:

| Package | Owns |
| --- | --- |
| `chatmsg` | Message/SSE/usage parsing and tool-call pairing — the one parser `ctxgraph`/`report`/`story` all share |
| `ctxgraph` | Content-addressed manifests, edit classification, conversation lineage and cross-lineage stitching |
| `taskseg` | Agent-dialect `Profile` (`OpenClawAware`, `Generic`) **and** the session/task segmentation algorithm itself, shared by `report` and `story` rather than duplicated in each |
| `reqdetail` | Two things, both pure functions of one `audit.Record` and nothing else: per-record fact extraction (role token/char shares, tool signature, error class, image counts — `report/session.go`'s own aggregation calls these too, not just detail rendering) and the detail page renderer built on top of it (`details/*.md`, plus its deterministic coordinate-hash filename — `FileName`/`FileNameForRecord`/`FileNameForManifest`). Shared by `report` and `story` so a page is byte-identical regardless of which command renders it |
| `report` | `vmr report`: aggregation into `vmr-report.{json,md}` + `vmr-requests.{json,md}`, driving `reqdetail` for `details/*`. A new report section arrives as a new `internal/report/section_*.go` file, not as more lines in an existing one — the `archtest` line budget is what enforces that |
| `story` | `vmr story`: Journey/Task/Step narrative, behavior indicators, findings, journey comparison, corpus statistics, optional LLM interpretation layer |

Shared guards:

| Package | Owns |
| --- | --- |
| `archtest` | Executable architecture invariants: import boundaries, per-file line budgets, per-function line budgets, and documentation-reference integrity |
| `buildinfo`, `rundir` | Build identity from Go's VCS stamp; default runtime dir resolution (`~/.vmr` → temp → cwd) |

`cmd/vmr/` is the CLI (stdlib `flag`), one file per subcommand, and the only composition
root allowed to see both halves at once.

## Invariants to not accidentally break

- **Byte-faithful passthrough.** No canonical IR, no cross-protocol translation. Exactly
  three sanctioned deviations: model-name rewrite, `respnorm`'s evidence-based quirk repairs
  (each behind a content guard, fail-open to "unmodified" on any doubt), and `imgprep`'s
  image downscale (the largest — a real unmarshal/rewrite/re-marshal, not a byte splice).
- **Two halves, one contract.** `report`/`story`/`ctxgraph`/`taskseg`/`chatmsg`/`reqdetail` never
  import `router`/`server`/`config`; the JSONL audit record is the only coupling. `archtest`-enforced.
- **`ctxgraph`/`chatmsg` are the single source of truth** for message hashing and message
  parsing. A private re-implementation can silently disagree with `ctxgraph` about where a
  conversation's history got reset — that is a whole bug class, not a style preference.
- **An analytics number that reproduces a routing number must be pinned by a differential
  test, not a comment.** Sharing the formula is only half of it; the *basis* is chosen
  independently on each side, and a wrong basis reads exactly like a right one. See
  `cmd/vmr/quota_parity_test.go` — the router side must call the router's own exported
  entry points, never restate their formulas.
- **No provider SDKs**, and **compile-time plugin registration only** (blank import) — never
  a runtime plugin system.
- **Config is strict YAML** (`KnownFields`): unknown keys are load errors, not warnings.
- **`Condition` (elimination) and `Dimension` (ordering) stay separate** — do not add a
  request parameter to `Dimension.Compare`.
- **Registries populated from `init()`** use a lock-free atomic read path with a
  mutex-guarded copy-on-write *write* path. A bare copy-on-write without the mutex silently
  loses updates under concurrent writers — guard the write too, not just the atomic swap.
- **Audit files are 0600, `reports/` output is 0600/0700.** Derived artifacts carry full
  conversation bodies and must not loosen that.
- **Timezone: one display authority.** Everything human-facing — Markdown/CLI output,
  aggregation bucketing, filenames — renders through `fmtutil.DisplayZone`; persisted records
  keep their write-time offset. A timestamp inside an LLM/tool payload is passthrough content,
  never parsed or converted. `story`'s `deriveID` is the one documented exception.
- **`internal/report` is coupled to `audit.Record`'s shape at compile time** — changing the
  record structure means updating `report` and its tests in the same change.
- **Run `go test ./internal/archtest/...`** after any package-boundary change, any `router`
  change, or any edit that grows a function. When it trips, split the file/function; raising
  the number in the table is what the failure message tells you not to do.

## Conventions

- **This file holds principles, not implementation detail.** State the rule and point at
  where it's enforced; don't restate call sequences or file lists that already live in the
  code. A stale detail here is worse than no detail — it reads as authoritative.
- **Docs are current state, not changelogs.** Replace the stale part; don't append a dated
  paragraph on top of it. Git history already holds the story. The exceptions are documents
  whose whole purpose *is* the trail: review reports, `CHANGELOG.md`, and code comments
  recording why a superseded approach was wrong.
- **No section numbers in cross-references.** Cite a document and a section *name*
  ("Part 1's Sticky Model section"), never `§6.5` — numbers renumber and then silently point
  at the wrong thing, while a name keeps resolving after a reorganization.
- **Comments: only non-obvious "why"** (hidden constraint, workaround, invariant). Match the
  existing terse style; don't add narration.
- **Commit messages**: short, imperative, **no trailers at all** — including `Co-Authored-By`,
  which tooling defaults tend to add back; strip it. Body only when the change needs a why
  (`git log --oneline`).
- Chinese-language docs live under `docs/`; this file and all code comments are English.
- **Every doc with a `.zh` sibling updates in the same change** — `README.md`,
  `docs/UserGuide.md`, and every repo-root `*.example.yaml`.
  Same keys, structure, and example values; only the prose translates.
- **`CHANGELOG.md` (Keep a Changelog) is the source of truth for release notes.** Add entries
  under `[Unreleased]` as user/developer-visible changes land, grouped `Added`/`Changed`/`Fixed`;
  skip pure dependency-bump and doc-churn commits. `release.yml` extracts that section verbatim
  as the GitHub Release body and fails on purpose if a pushed tag has no matching section —
  write the entry before tagging, not after.
