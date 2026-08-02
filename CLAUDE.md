<!-- Ver 2026-07-30 12:00, by Sonnet 5 -->

# vmr — Claude Code project brief

Local-run, single-binary, config-driven LLM router (Go). Clients connect to a stable
Virtual Model name (`coding`, `agent`, `claude`); vmr hides provider, account, key,
priority, and failover behind it. Two ingress protocols, **never translated into each
other**: `POST /v1/chat/completions` (OpenAI) and `POST /v1/messages` (Anthropic) — each
routes only to same-protocol endpoints. Everything else is byte-faithful passthrough.

Beyond routing, two offline tools consume the audit log: `vmr report` (aggregate
statistics) and `vmr story` (single-task narrative reconstruction). Both are read-only
consumers of the JSONL audit format — neither is on the request path, neither is
imported by `internal/router`/`internal/server`.

**Full design doc (read before any non-trivial change) — two parts:**
- `docs/VirtualModelRouter_Design_v4_Core.md` — routing core (this file's main subject).
- `docs/VirtualModelRouter_Design_v4_Analytics.md` — `vmr report`/`vmr story` (`internal/{report,story,ctxgraph,chatmsg}`).

Each has the "why" behind every decision, a decisions-and-tradeoffs table, and (Part 1)
a "decided-not-to-fix" table — check both before "fixing" something that looks odd.
This file only orients; it does not restate either doc.

## Build / test / run

```
go build -o vmr ./cmd/vmr
go test ./...              # add -race for anything touching health/audit/router concurrency
./vmr check -c config.yaml # validate config + print effective routing table (no network I/O)
./vmr start -c config.yaml # foreground
./vmr.sh {start|stop|restart|status|logs}   # dev-mode background supervisor, calls `vmr check` first
```

No Makefile, no CI config in-repo. `vmr.sh` never runs `go build` itself — build first.

## Module map (`internal/`)

| Package | Owns |
| --- | --- |
| `core` | `CanonicalRequest`, `ErrorClass`, `Endpoint` (with `Freeze()` — precomputes `HealthKey()`/`Name()` once in `router.BuildSnapshot`; unfrozen literals still work, just uncached) — shared types, no internal deps. Also `FilterClientHeaders` (header blocklist — `server` and `replay` both call this, neither owns it) |
| `fmtutil` | `FmtBytes`/`FmtTokens`/`FmtSeconds` — display formatting shared by `router`'s live log and `report`'s rendering; split out of `core` so neither has to depend on routing-domain types just to print a number |
| `config` | YAML load, `${ENV}` expand, strict validation (`KnownFields`), hot-reload watch |
| `adapter`, `adapter/openai`, `adapter/anthropic` | `Adapter` interface (compile-time registered via blank import, lock-free `atomic.Pointer` registry) + shared error classification / model-name byte-splice rewrite |
| `health` | Passive state machine: cooldown, backoff, half-open single-flight |
| `probe` | Minimal echo-nonce request used by both background recovery probes and `vmr diagnose` |
| `strategy` | `Dimension` (ordering, e.g. priority) + `Condition` (elimination, e.g. image/tools capability, lock-free `atomic.Pointer` registry) — two separate interfaces, don't merge them |
| `sticky` | Session-affinity registry (prompt-cache stickiness); its `BackstopTTL` is an alias for `core.StickyBackstopTTL` (canonical value lives in `core` so `config` can validate against it without importing `sticky`) |
| `router` | Failover loop (`router.go`: `Serve`/`tryOne`/`handleErrorResponse`/`forwardSuccess`, the part that must stay small) + `reload.go` (hot-reload outcome + config staleness, for `/admin/status`) + `routehdr.go` (`X-VMR-Route-Reason`/`X-VMR-Failover`) + `snapshot.go` (`BuildSnapshot`/`Install`) + `limiter.go` (concurrency gate) + `transport.go` (`NewUpstreamClient`, streaming forward) + `logfmt.go` (live log line formatting) + `response.go` (response normalization: event splitting, model-name rewrite, `[DONE]` policy, buffered/passthrough decision) + `responsefix.go` (MiniMax-specific quirk knowledge — `<think>`/Thinking-Process stripping, soft-block markers — `response.go` calls into it, never the other way) |
| `server` | HTTP entry, auth, audit recording, `facts.go` (RequestFacts extraction), `admin.go` (the loopback-only `/admin/status`: process identity, config freshness, reload outcome) — header blocklist lives in `core.FilterClientHeaders`, not here |
| `audit` | JSONL audit log (one line per request, two layers: client↔vmr, vmr↔upstream) + zstd compression/retention. `Write` encodes into a pooled buffer *outside* its lock — don't move that encode inside the lock, it would serialize JSON-encoding potentially-multi-MB records across concurrent requests |
| `diagnose`, `replay` | `vmr diagnose` (real connectivity check) / `vmr replay` (resend one audit record) — reuse the same `adapter.BuildRequest`/`router.NewUpstreamClient` real traffic uses |
| `imgprep` | Inline image downscale + disk cache |
| `buildinfo` | Build identity from Go's own VCS stamp (`debug.ReadBuildInfo`) — no ldflags, no generated file. Used by `vmr version` and `/admin/status`'s `instance.version` |
| `rundir` | Default dir resolution formula (`~/.vmr` → temp → cwd), shared by log/cache dirs |
| `archtest` | Executable architecture invariants (import boundaries, core-file line-count budgets) |
| `chatmsg` | Shared message/SSE/usage parsing (`Messages`, `ReassembleSSE`/`FinalMessage`, `ExtractUsage`, `CheckToolPairing`, `ExtractEntities`) — the one package `ctxgraph`/`story`/`report` all depend on, so none of the three re-implements the same parsing |
| `ctxgraph` | Content-addressed manifest/blob-index layer behind both `vmr report` and `vmr story`: `Manifest` (per-request message-hash vector), `Classify` (5-way edit classification: Append/ReplaceTail/Splice/Contract/Fork), `Lineage`/`Scan` (splits a SessKey bucket at every Contract/Fork), `StitchGraph`/`ChainFrom` (cross-lineage reconnection). Depends on `{audit, core, chatmsg}`; never depends on `report`/`story`/`router`/`server` (`archtest`-enforced) |
| `story` | `vmr story`: Journey/Task/Step/Event narrative built from a `ctxgraph` lineage chain (`journey.go`), nine-indicator behavior profile (`metrics.go`), Journey-vs-Journey diff (`compare.go`, plus `ComparisonExtras`'s rule-derived endpoint/cache/system-prompt/final-context/duration/deliverable facts), Markdown rendering (`render_md.go`/`render_compare.go`). `llm.go` is `-compare`'s optional, always-degradable LLM interpretation layer (`-llm-addr`/`-llm-model`/`-llm-key`/`-llm-dry-run`) — a plain `net/http` client against a manually-specified already-running VMR instance, no config.yaml provider resolution, no failover. `profile/` subpackage holds the one agent-specific interface (real-instruction/no-reply judgment) with an OpenClaw-aware and a generic fallback implementation. Depends on `{ctxgraph, chatmsg, core, config}`; never depends on `report`/`router`/`server` |
| `report` | `vmr report`: aggregates audit JSONL into `vmr-report.{json,md}` + `vmr-requests.{jsonl,md}` + per-request `details/*.{md,json}`. Data shape in `rows.go` (= the vmr-report.json schema), aggregation pass in `aggregate.go`, session/task grouping in `session.go` (consumes `ctxgraph.Lineage`/`Classify` directly — no private hash/LCP implementation of its own), rendering split `render_doc.go` (running order + `mdTable`) + one `section_*.go` per report section — **a new section is a new file, not more lines in an existing one** (`archtest` budgets enforce it). Depends on `{audit, core, ctxgraph, chatmsg}` — an `internal/archtest` test enforces it never depends on `router`/`server`/`config` |

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
  sanctioned deviations: model-name rewrite (virtual ↔ real) and a short list of
  evidence-based provider quirk repairs (see Part 1's "Response-side normalization"
  section) — each behind a strict content-based guard, fail-open to "unmodified" on
  any doubt.
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
- `internal/archtest` enforces import boundaries and `router.go`'s line-count budget — run it
  (`go test ./internal/archtest/...`) after any package-boundary or `internal/router` file change.

## Conventions

- Comments: only for non-obvious "why" (hidden constraint, workaround, invariant) —
  match the existing terse style, don't add narration.
- Commit messages: short, imperative, no trailer boilerplate (see `git log --oneline`).
- Chinese-language docs live under `docs/`; this file and code comments are English.
- Before treating something as a bug: check Part 1's "已识别、暂不落地的清理项" (decided-
  not-to-fix) table and Part 2's "已知限制、暂不处理的事项" table — some odd-looking
  behavior is a documented, deliberate non-fix.
- **No section numbers in cross-references** — in code comments and between docs, name
  the section ("the sticky-effectiveness section", "the decided-not-to-fix table") or
  describe the fact in one short clause, never `§6.5`/`Appendix C.6`/`F9`. Section numbers
  renumber every time a doc is edited; a name or a short description survives that, a
  number silently goes stale and points at the wrong thing. This applies going forward —
  existing numbered references already in the code aren't being swept in one pass.
