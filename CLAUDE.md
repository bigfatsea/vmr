<!-- Ver 2026-07-26 18:00, by Sonnet 5 -->

# vmr — Claude Code project brief

Local-run, single-binary, config-driven LLM router (Go). Clients connect to a stable
Virtual Model name (`coding`, `agent`, `claude`); vmr hides provider, account, key,
priority, and failover behind it. Two ingress protocols, **never translated into each
other**: `POST /v1/chat/completions` (OpenAI) and `POST /v1/messages` (Anthropic) — each
routes only to same-protocol endpoints. Everything else is byte-faithful passthrough.

**Full design doc (read before any non-trivial change):** `docs/VirtualModelRouter_System_Design_v3.md`.
It has the "why" behind every decision, a decisions-and-tradeoffs table, and a
"decided-not-to-fix" table — check both before "fixing" something that looks odd.
This file only orients; it does not restate that doc.

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
| `probe` | Minimal echo-nonce request used by both active-mode recovery probes and `vmr diagnose` |
| `strategy` | `Dimension` (ordering, e.g. priority) + `Condition` (elimination, e.g. image/tools capability, lock-free `atomic.Pointer` registry) — two separate interfaces, don't merge them |
| `sticky` | Session-affinity registry (prompt-cache stickiness); its `BackstopTTL` is an alias for `core.StickyBackstopTTL` (canonical value lives in `core` so `config` can validate against it without importing `sticky`) |
| `router` | Failover loop (`router.go`: `Serve`/`tryOne`/`handleErrorResponse`/`forwardSuccess`, the part that must stay small) + `snapshot.go` (`BuildSnapshot`/`Install`) + `limiter.go` (concurrency gate) + `transport.go` (`NewUpstreamClient`, streaming forward) + `logfmt.go` (live log line formatting) + `response.go` (response normalization: event splitting, model-name rewrite, `[DONE]` policy, buffered/passthrough decision) + `responsefix.go` (MiniMax-specific quirk knowledge — `<think>`/Thinking-Process stripping, soft-block markers — `response.go` calls into it, never the other way) |
| `server` | HTTP entry, auth, audit recording, `facts.go` (RequestFacts extraction) — header blocklist lives in `core.FilterClientHeaders`, not here |
| `audit` | JSONL audit log (one line per request, two layers: client↔vmr, vmr↔upstream) + zstd compression/retention. `Write` encodes into a pooled buffer *outside* its lock — don't move that encode inside the lock, it would serialize JSON-encoding potentially-multi-MB records across concurrent requests |
| `report` | `vmr report`: aggregates audit JSONL into `vmr-report.{json,md}` + `vmr-requests.{jsonl,md}` + per-request `details/*.{md,json}`. Only depends on `{audit, core}` — an `internal/archtest` test enforces it never depends on `router`/`server`/`config` |
| `diagnose`, `replay` | `vmr diagnose` (real connectivity check) / `vmr replay` (resend one audit record) — reuse the same `adapter.BuildRequest`/`router.NewUpstreamClient` real traffic uses |
| `imgprep` | Inline image downscale + disk cache |
| `rundir` | Default dir resolution formula (`~/.vmr` → temp → cwd), shared by log/cache dirs |
| `archtest` | Executable architecture invariants (import boundaries, core-file line-count budgets) |

`cmd/vmr/` is the CLI (stdlib `flag`), one file per subcommand: `main.go` (dispatch + usage +
the adapter blank-import registration point), `cmd_start.go`, `cmd_check.go`, `cmd_status.go`,
`cmd_report.go`, `cmd_dirs.go`, `cmd_diagnose.go`, `cmd_replay.go`, and `summary.go` (config-summary
rendering shared by start/check).

## Invariants to not accidentally break

- **Byte-faithful passthrough.** No canonical IR, no cross-protocol translation. Only
  sanctioned deviations: model-name rewrite (virtual ↔ real) and a short list of
  evidence-based provider quirk repairs (see design doc §5.5) — each behind a strict
  content-based guard, fail-open to "unmodified" on any doubt.
- **No provider SDKs.** Routing only needs URL/key/model-field changes.
- **Compile-time plugin registration only** (blank import), never a runtime plugin system.
- **Config: strict YAML** (`KnownFields`) — unknown keys are load errors, not warnings.
- **`Condition` (elimination) vs `Dimension` (ordering) are separate interfaces** — do not
  add a request parameter to `Dimension.Compare`.
- Adapters never touch response bodies; response normalization lives only in
  `internal/router/response.go` + `responsefix.go`.
- Audit files are 0600 / `reports/` output is 0600/0700 — derived report artifacts must
  not loosen that (they carry full conversation bodies).
- `internal/report` is coupled to `audit.Record`'s shape at compile time — changing the
  audit record structure requires updating `internal/report` and its tests in the same change.
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
- Before treating something as a bug: check design doc §14/§15 ("已识别、暂不落地的清理项")
  — some odd-looking behavior is a documented, deliberate non-fix.
