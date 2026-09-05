<!-- Ver 2026-09-06, by Sonnet 5 -->

# Pricing Architecture Simplification — Action Plan

Tracks execution of `pricing_architecture_simplification_plan.md` (design final draft,
2026-09-06). This file is the single source of truth for status, decisions, and the final
implementation summary while the work is in flight. Delete or archive once merged to `main`
and the design docs/CHANGELOG/KNOWN_ISSUES updates described below have landed.

Design doc verification verdict (2026-09-06): **architecture sound, no blocking issues.**
Every source-code claim in the plan (four pricing entry points, Table-vs-Provider naming
split, `Rate.Cost`'s nil-as-0 behavior, `Counters.Cost`'s "must be frozen at charge time"
comment, `NewResolver`'s `tableFactor`/`currency` params, KNOWN_ISSUES §1.0's hot-path red
line already resolved 2026-09-05) checked out against current source. Gaps found were in the
plan's §7.1 file inventory and workstream split, not in the six core decisions — folded into
the task breakdown below.

## Orchestration model

**Revised 2026-09-06 (Decision Log #9): serial, single-actor execution — no `pi` workers.**
User reviewed the parallel-worker plan below and judged the real parallelism payoff too
small to justify worktree/dispatch/merge overhead (Stage 2 strictly depends on Stage 1's
type changes; the only true parallel slice was CORE + a 2-file DOCS-USER task), and left the
call to the lead. Lead now implements everything directly and serially in one working tree.
`TASK_SPEC_CORE.md` (already written) is kept and used as the lead's own implementation
checklist instead of a worker dispatch doc; no separate DOCS-USER/CONSUMERS spec files are
written since the lead carries full context already.

Still in force from the original design: **integration branch, not direct `main` work**
(`feat/pricing-architecture-simplification`, created off `main`; `main` untouched until the
user reviews), and the file/stage ordering below (now read as an execution checklist for one
actor instead of a worker assignment table) — CORE-scope files first (they define the new
type shapes), CONSUMERS-scope files second (they consume those shapes), design docs/
CHANGELOG/KNOWN_ISSUES last (need the final landed state to describe accurately).

Original parallel-worker design (superseded, kept for record):

| Stage | Worker | Branch | Worktree | File ownership |
| --- | --- | --- | --- | --- |
| 1 (parallel) | CORE | `feat/pricing-core` | `../vmr-wt-core` | `internal/config/**`, `internal/core/core.go`, `internal/pricing/**`, `config.mock.yaml`, `config.example.yaml`/`.zh`, `cmd/vmr/check_pricing.go`, `cmd/vmr/cmd_check.go` (+ their `*_test.go`), deletion of `pricing.mock.yaml`/`pricing.example.yaml`/`.zh` |
| 1 (parallel) | DOCS-USER | `feat/pricing-docsuser` | `../vmr-wt-docsuser` | `docs/UserGuide.md`, `docs/UserGuide.zh.md` |
| 2 (after Stage 1 merges) | CONSUMERS | `feat/pricing-consumers` | `../vmr-wt-consumers` | `internal/router/quota.go`, `internal/router/snapshot.go`, `internal/quota/**`, `internal/replay/**`, `internal/report/providerquota.go`, `internal/report/rows.go`, `internal/i18n/report_provider.go`, `cmd/vmr/cmd_status_render.go`, `cmd/vmr/cmd_report_quota.go`, `cmd/vmr/quota_parity_test.go` (+ their `*_test.go`) |
| 3 (lead only) | — | (integration branch, main worktree) | — | `docs/VirtualModelRouter_Design_v4_Quota.md`, `docs/VirtualModelRouter_Design_v4_Core.md`, `CHANGELOG.md`, `docs/KNOWN_ISSUES.md`, this file |

Explicitly out of scope for the whole task (plan says "unchanged"; do not touch):
`internal/audit/audit.go`, `internal/report/cost.go`, `internal/story/cost.go`,
`cmd/vmr/cost_basis_parity_test.go`.

## Progress tracker

| # | Task | Owner | Status | Branch / PID | Notes |
| --- | --- | --- | --- | --- | --- |
| 0 | Clear stale WIP from working tree | Lead | done | commit `4223585` on `main` | Pre-existing uncommitted inline `pricing.rates`/`aliases` WIP, superseded by decision 1/2; committed standalone per user instruction, not discarded |
| 1 | Write Action Plan (this file) | Lead | done | — | |
| 2 | Write TASK_SPEC_CORE.md (now used as lead's own checklist) | Lead | done | — | |
| 3 | Create integration branch `feat/pricing-architecture-simplification` off `main` | Lead | pending | — | |
| 4 | Implement CORE-scope changes (config.go/pricing.go/quota.go/provider.go, core.go, internal/pricing/**, examples, mock, check_pricing.go, cmd_check.go) | Lead | pending | — | |
| 5 | Implement DOCS-USER scope (UserGuide.md/.zh pricing section) | Lead | pending | — | |
| 6 | Build + package tests + archtest after CORE+DOCS-USER | Lead | pending | — | |
| 7 | Implement CONSUMERS-scope changes (router/quota+snapshot, internal/quota/**, internal/replay/**, report/providerquota+rows, i18n/report_provider, cmd_status_render, cmd_report_quota, quota_parity_test) | Lead | pending | — | |
| 8 | Full verification (`go build`/`go vet`/`go test -race ./...`/archtest/gofmt/shellcheck) | Lead | pending | — | |
| 9 | Rewrite `docs/VirtualModelRouter_Design_v4_Quota.md` §7.1/§9.1/§4/decisions | Lead | pending | — | |
| 10 | Update `docs/VirtualModelRouter_Design_v4_Core.md` pricing-related sections | Lead | pending | — | |
| 11 | `CHANGELOG.md` `[Unreleased]` entry (breaking change + migration guidance) | Lead | pending | — | |
| 12 | `docs/KNOWN_ISSUES.md` registration | Lead | pending | — | |
| 13 | Final implementation summary (this file, §"Implementation summary") | Lead | pending | — | |

Status values: `pending` / `dispatched [PID]` / `in progress` / `blocked` / `done`.

## Decision log

Decisions made without interrupting the user, recorded here for later review.

1. **Workstream split is package-boundary, not the plan's literal A/B.** The plan's own §7
   text notes A and B converge on `internal/config` and recommends landing them as two
   sequential steps, not parallel — but its §7.1 table still labels files "A"/"B"/"A+B" as if
   a two-worker parallel split were intended. Splitting by package (CORE takes every file in
   `internal/config`+`internal/core`+`internal/pricing` regardless of which decision drives
   it; CONSUMERS takes every downstream package) avoids two workers fighting over the same
   file and matches the repo's own archtest package-isolation philosophy. Confirmed safe:
   Stage 1's CORE and DOCS-USER file sets are disjoint; CONSUMERS depends on CORE's new type
   shapes (`core.MetricCost` removed, `Endpoint.PricingRate` removed) so it must run in Stage 2.
2. **Integration branch, not direct `main` merge.** This is a breaking change (removes
   `metric: cost` entirely, restructures all pricing config). Local commits are cheap to
   redo, but merging a multi-file breaking rewrite onto `main` without the user seeing it
   first is not — `feat/pricing-architecture-simplification` holds all the work; `main` is
   untouched until the user says otherwise.
3. **Stale uncommitted WIP (top-level inline `pricing.rates`/`aliases`) committed standalone,
   not discarded.** User confirmed (mid-task) it predates and is unrelated to this plan, and
   asked for it to be committed to keep the tree clean rather than stashed. Commit `4223585`
   on `main`. It will be superseded/removed by CORE's decision-1/2 work (top-level `pricing:`
   block deleted entirely) — that removal is expected, not a regression to flag.
4. **§7.1's file inventory expanded** based on `grep -rln "MetricCost"` across the repo (see
   debrief). Full list folded into CONSUMERS' whitelist below; `internal/router/quota_cost_test.go`
   is wholesale-deletable (all 11 tests are cost-metric-only, verified by reading its test names).
5. **Design-doc rewrite added as its own lead-only workstream (#3).** Missing from the plan's
   §7.1 table, but required by this repo's own CLAUDE.md convention ("Docs are current state,
   not changelogs") — `docs/VirtualModelRouter_Design_v4_Quota.md` §7.1/§9.1 and
   `_Core.md`'s pricing-adjacent sections describe the pre-refactor shape and would go stale
   otherwise.
6. **`config.mock.yaml` added to CORE's scope** (uses old `pricing.currency`/`supplement`/
   `map`/`overrides` shape; missing from the plan's §7.1 table).
7. **DOCS-USER runs against the plan document's pinned final shape (§3.2/§7.2), not against
   CORE's actual output** — the two run in parallel so DOCS-USER cannot see CORE's real diff.
   Risk of drift if CORE deviates from the plan doc during implementation; mitigated by
   tracker item #16, a lead reconciliation pass after both merge.
8. *(placeholder — append further self-decided calls here as they occur during execution,
   newest last)*
9. **Pivoted from parallel `pi`-worker execution to serial single-actor execution.** User
   reviewed this file's original worker-split design and judged the coordination overhead
   (worktrees, task-spec authoring, dispatch, monitoring, merging) not worth it given the
   real dependency structure (Stage 2 strictly needs Stage 1's type changes; genuine
   parallelism was only CORE + a 2-file DOCS-USER task), and explicitly left the choice to
   the lead. Lead now implements the whole plan directly, still inside the
   `feat/pricing-architecture-simplification` integration branch (that safety decision, #2
   above, still stands), still following the CORE→DOCS-USER/CONSUMERS→docs ordering as an
   execution checklist rather than a worker assignment.

## Open items for user review

*(non-blocking calls the lead made unilaterally that the user may want to revisit later —
populate during execution; empty for now)*

## Implementation summary

*(filled in after tracker item #18 completes)*
