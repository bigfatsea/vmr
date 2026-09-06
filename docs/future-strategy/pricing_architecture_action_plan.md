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
| 3 | Create integration branch `feat/pricing-architecture-simplification` off `main` | Lead | done | commit `08a2117` onward | |
| 4 | Implement CORE-scope changes (config.go/pricing.go/quota.go/provider.go, core.go, internal/pricing/**, examples, mock, check_pricing.go, cmd_check.go) | Lead | done | — | `internal/pricing`, `internal/core`, `internal/config` all green independently; also removed now-dead `configDir`/`resolveConfigRelative`/`providerModels` plumbing (see decision log #10) |
| 5 | Implement DOCS-USER scope (UserGuide.md/.zh pricing section) | Lead | done | — | Full rewrite of "Pricing and cost-metric quota" → "Pricing (for cost estimates)" in both `UserGuide.md` and `UserGuide.zh.md` (was still on the pre-refactor three-layer/`metric: cost` shape in its entirety — this was the single largest stale-doc gap found in the whole audit, see decision log #12); also fixed `metric: cost` mentions scattered across the Quota-Aware Routing and Cost-estimate sections |
| 6 | Build + package tests + archtest after CORE+DOCS-USER | Lead | done | — | `go build ./...`, `go vet ./...`, `gofmt -l .`, `shellcheck vmr.sh vmr-loadtest.sh` all clean |
| 7 | Implement CONSUMERS-scope changes (router/quota+snapshot, internal/quota/**, internal/replay/**, report/providerquota+rows, i18n/report_provider, cmd_status_render, cmd_report_quota, quota_parity_test) | Lead | done | — | includes cmd_report.go/pricing.go (report.Pricing.Supplement removal); found and fixed one CONSUMERS-scope gap during the review pass: `cmd/vmr/cmd_status_render.go` still had two dead `q.Metric == "cost"` branches (unreachable — `metric` can only be `requests`/`tokens` now) — cleaned up, see decision log #11 |
| 4b | Update config.mock.yaml, config.example.yaml/.zh (pricing section rewrite per plan §3.2) | Lead | done | — | `config.mock.yaml` was already in the target two-layer shape (confirmed); `config.example.yaml`/`.zh` were also already migrated but still cross-referenced the throwaway `docs/future-strategy/pricing_architecture_simplification_plan.md` file from two comments each — repointed to `docs/VirtualModelRouter_Design_v4_Quota.md` (a plan doc under `future-strategy/` is not a permanent citation target, see decision log #13) |
| 8 | Full verification (`go build`/`go vet`/`go test -race ./...`/archtest/gofmt/shellcheck) | Lead | done | — | `go test -race ./...`: all 36 packages green (`cmd/vmr` 35.8s, `internal/config` 13.5s, rest cached-or-fast); `go test ./internal/archtest/...` green; `gofmt -l .` and `shellcheck vmr.sh vmr-loadtest.sh` both silent (no issues) |
| 9 | Rewrite `docs/VirtualModelRouter_Design_v4_Quota.md` §7.1/§9.1/§4/decisions | Lead | done | — | §7.1/§9.1/§9.2 had already been patched (by an earlier pass) with inline `[2026-09-06 修订]` changelog-style markers layered on top of the old prose — de-datestamped into plain current-state prose per this repo's own "docs are current state, not changelogs" convention (see decision log #12). §4.2 (①–⑨, the "定价分三层"/"三层解析" narrative) had NOT been touched at all and still described the pre-refactor three-layer + `metric: cost` design as current — fully rewritten to the two-layer/no-cost shape. §12.2's explicit "否决" verdict on the exact proposal decision 6 later adopted was left standing uncorrected (a real self-contradiction) — annotated with a "后于 2026-09-06 反转" pointer. §13/§15 (scope boundaries, current-status summary) also still described `metric: cost` as landed/current — corrected; §15.1/§15.2's terminal-machinery count updated (14 → 11 currently active + 2 permanently cut, `metric: cost` added as the second cut alongside the `Source` abstraction) |
| 10 | Update `docs/VirtualModelRouter_Design_v4_Core.md` pricing-related sections | Lead | done | — | §6.6's 计量/定价 prose was already accurate (a prior pass had done this correctly); found and fixed three module-map-table (§ package list) rows that still described the pre-refactor shape: `internal/config`'s row still said "three-layer resolution" and cited the deleted per-account completeness gate, `internal/pricing`'s row still said "three-layer resolution" naming `supplement∪standard`, `internal/quota`'s row still claimed `Counters.Cost` exists as a field (it's deleted) |
| 11 | `CHANGELOG.md` `[Unreleased]` entry (breaking change + migration guidance) | Lead | done | — | Added two **Breaking** entries (metric: cost removal; top-level `pricing:` block → `exchange_rate:` + two-layer `providers[].pricing`) to the existing Breaking-changes cluster. Also found and fixed ~15 *other* `[Unreleased]` entries from the same pre-this-plan development window that described intermediate/superseded pricing machinery as if it would ship (a `pricing.rates`/`pricing.aliases` top-level inline-table feature that was itself superseded before ever releasing; `core.Endpoint.PricingRate`/`ChargeCost`/`Config.PricingAccounting` — all added and then fully removed within this same Unreleased cycle) — removed those entries outright (release notes for a feature that came and went pre-release would mislead readers) and updated field-name references (`overrides`→`rates`, `map`→`aliases`, `pricing.supplement`→removed) in the ones that are still substantively true. See decision log #12 |
| 12 | `docs/KNOWN_ISSUES.md` registration | Lead | done | — | The 2026-09-06 closure entry and §2.89's "作废" rewrite were already done by an earlier pass and are accurate. Found and fixed four more entries in §1.2/§2.58/§2.58a that stated the old three-layer/`metric: cost` design as current fact (not framed as history) — `pricing.ParseTable`'s NaN/±Inf guard reasoning (used to cite poisoning `Counters.Cost`, which no longer exists), the `internal/config`-imports-`pricing`-at-`validate()` rationale (still correct decision, stale reasoning), a stray `pricing.map`/`overrides` field-name reference, and two report-side entries citing `pricing.supplement`/`metric: cost`'s completeness gate as current mitigations |
| 13 | Final implementation summary (this file, §"Implementation summary") | Lead | done | — | see below |

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
8. **Removed dead `configDir`/`resolveConfigRelative`/`providerModels` plumbing during CORE
   implementation.** These existed solely to resolve `pricing.supplement`'s path relative to
   `config.yaml`'s own directory and to build a per-provider model list for the old
   completeness gate — both consumers deleted by decisions 1/2/6, leaving the plumbing itself
   with zero callers. Deleted rather than left as unused-but-harmless, per this repo's own
   "no half-finished implementations" convention.
9. **Pivoted from parallel `pi`-worker execution to serial single-actor execution.** User
   reviewed this file's original worker-split design and judged the coordination overhead
   (worktrees, task-spec authoring, dispatch, monitoring, merging) not worth it given the
   real dependency structure (Stage 2 strictly needs Stage 1's type changes; genuine
   parallelism was only CORE + a 2-file DOCS-USER task), and explicitly left the choice to
   the lead. Lead now implements the whole plan directly, still inside the
   `feat/pricing-architecture-simplification` integration branch (that safety decision, #2
   above, still stands), still following the CORE→DOCS-USER/CONSUMERS→docs ordering as an
   execution checklist rather than a worker assignment.
10. **Resumed the task after a context reset found the tracker significantly out of sync with
    actual repo state.** On resuming (2026-09-06, later session), tracker items 3–8 were still
    marked pending/in-progress despite commits `870ca6a`/`5e3109d` and uncommitted working-tree
    edits showing CORE, CONSUMERS, and a first pass at the design-doc rewrite already landed.
    Rather than trust the tracker, re-verified actual state directly (`git diff`, `go build`,
    targeted `grep` for every deleted symbol/field name) before continuing — this surfaced the
    real remaining gaps (below), which were materially different from what the tracker implied
    was left. Lesson for future runs of this pattern: update the tracker in the same edit as
    the work, not after — a tracker that lags the actual diff is worse than no tracker.
11. **Found and fixed one CONSUMERS-scope gap the "done" marking had missed**:
    `cmd/vmr/cmd_status_render.go` still branched on `q.Metric == "cost"` in two places — dead
    code, since `metric` can only resolve to `requests`/`tokens` after decision 6 (confirmed via
    `grep -rn "Metric =="` across the repo, which the original `grep -rln "MetricCost"` sweep
    behind decision log #4 didn't catch because this file only tested the string literal
    `"cost"`, never the removed `MetricCost` identifier). Removed both branches and their
    now-inapplicable comments.
12. **Design docs and CHANGELOG required a much larger accuracy pass than the tracker's "done"
    markings implied.** Verifying `docs/VirtualModelRouter_Design_v4_Quota.md` against current
    source (per the user's explicit request to check whether the plan document's own analysis
    still holds) found that an earlier pass had patched only isolated paragraphs (§7.1/§9.1/§9.2,
    each wrapped in a dated `[2026-09-06 修订]` blockquote) while leaving §4.2 — the file's own
    first-principles derivation of the pricing model, ①–⑨ — completely untouched: it still
    described the pre-refactor three-layer table + `metric: cost` design as the *current*
    architecture, and §12.2 carried an explicit "否决" (rejected) verdict on the literal proposal
    decision 6 later adopted, with no note that it had been reversed. `docs/
    VirtualModelRouter_Design_v4_Core.md` and `_Analytics.md` had similar isolated gaps — module-
    map rows describing deleted fields (`Counters.Cost`) or the old three-layer resolution, and
    a `report.Pricing` field-list mention (`supplement`, "alias count") that doesn't match the
    actual struct. `CHANGELOG.md`'s `[Unreleased]` section (not yet released, so still editable
    per this repo's own convention) carried ~15 entries describing pricing/cost machinery that
    was added and then fully removed within this same development cycle — including one entire
    "Added" entry for a feature (top-level inline `pricing.rates`/`aliases`) that never survived
    to a release. Root cause: earlier passes patched *symptoms* (a paragraph that visibly still
    said `metric: cost`) rather than doing a full grep-driven sweep for every deleted symbol/
    field name across every doc the plan's own §7.1 table names. This session did that full
    sweep (Quota.md, Core.md, Analytics.md, CHANGELOG.md, KNOWN_ISSUES.md, CLAUDE.md, UserGuide.
    md/.zh, config.example.yaml/.zh, config.mock.yaml, standard_price_curated.yaml) and fixed
    every hit; see tracker items 5/9/10/11/12 for the per-file breakdown. Also de-datestamped
    the `[2026-09-06 修订]` blockquotes in Quota.md into plain current-state prose — this repo's
    CLAUDE.md explicitly says design docs are "current state, not changelogs," and a permanent
    dated revision marker in the middle of descriptive prose (as opposed to a decision-log table,
    which *is* allowed to carry history) reads as exactly the "layer a correction on top of the
    prior text" pattern that convention rules out.
13. **`config.example.yaml`/`.zh` cross-referenced the throwaway `docs/future-strategy/
    pricing_architecture_simplification_plan.md` file (twice each) instead of the permanent
    design doc.** This file's own header says it should be deleted/archived once merged to
    `main` — a permanent, committed example config citing it as a live reference would leave a
    dead link the moment that cleanup happens. Repointed both to `docs/
    VirtualModelRouter_Design_v4_Quota.md`, consistent with every other cross-reference in the
    same file and with this repo's "cite a document by name, not a transient location"
    convention. Also found and corrected stale `internal/pricing/standard_price_curated.yaml`
    comments (three spots) that cited the now-deleted `metric: cost` completeness gate and the
    old `overrides` field name as if still current.
14. **Left the two `future-strategy/pricing_*` planning docs and `docs/tasks/TASK_SPEC_CORE.md`
    in place rather than deleting them now.** This file's own header (and the plan doc's,
    implicitly) says to delete/archive once merged to `main`; `main` is still untouched per
    decision #2. Deleting now, before the user has reviewed the branch, would be premature —
    noted here so it isn't forgotten post-merge (see Implementation summary below).
15. **Pre-commit double-check pass (user-requested) found four more self-inflicted errors from
    this session's own editing, all fixed**: (a) two dangling `Quota.md` cross-references to
    "§7.1 修订" left over after the §7.1 heading's date-stamp framing was removed earlier in the
    same session — pointed at the actual subsection name instead; (b) §15.1's opening paragraph
    still said "已落地十二项、永久砍掉一项" immediately above a newly-added paragraph saying
    eleven/two — the exact "patch layered on an uncorrected original" anti-pattern this whole
    pass was fixing elsewhere, self-inflicted by appending a correction instead of rewriting the
    paragraph; merged into one consistent statement; (c) §15.3④ still said "标准表/补充表" and
    "补充表是用户自备的完整表" — missed in the earlier `grep -n "补充表"` sweep because that
    line's edit distance from a hit was one word away, not caught by the query used; (d) a
    Markdown lazy-continuation bug in `UserGuide.md`'s `model_multipliers` bullet — a
    `replace_all` edit deleted a trailing sentence and its trailing blank line together, leaving
    "Neither field changes anything…" glued to the end of the bullet with no blank line
    separating it, which CommonMark parses as part of the same list item rather than a new
    paragraph. Caught by re-diffing every changed file line-by-line rather than trusting the
    earlier per-file "done" markings — the same lesson as decision #10, applied to the lead's own
    output this time, not just the tracker's.

## Open items for user review

- **Post-merge cleanup**: once this branch is reviewed and merged to `main`, delete or archive
  `docs/future-strategy/pricing_architecture_simplification_plan.md`, this action-plan file, and
  `docs/tasks/TASK_SPEC_CORE.md` — all three are transient planning artifacts whose content is
  now either implemented (code) or absorbed into the permanent design docs (Quota/Core/
  Analytics) and `KNOWN_ISSUES.md`/`CHANGELOG.md`. Left in place for now per decision #14.

Everything else in this task was decided by the lead per the user's explicit instruction to
proceed autonomously on clear-cut points; no other item needs a decision from the user beyond
reviewing and merging the branch.

## Implementation summary

**Design verification (before implementation).** Re-checked the design plan's claims against
current source at the start of this session: the two-layer pricing model, `metric: cost`
removal rationale, and the exchange-rate/currency-annotation split all held up — no blocking
issues found in the architecture itself. The gaps found were entirely in *documentation
accuracy* (see decision log #12), not in the design.

**What shipped.** On `feat/pricing-architecture-simplification` (off `main`, `main` untouched):

- **Config surface**: top-level `pricing:` block removed outright (`currency`/`exchange_rate`/
  `supplement`/`standard`/`rates`/`aliases`), replaced by a standalone top-level `exchange_rate:`
  map. `providers[].pricing` renamed `map`→`aliases`, `overrides`→`rates`; `currency` is now a
  documented load-time write-in annotation (not a runtime quantity). External `pricing.yaml`
  supplement files are no longer supported in any form.
- **Quota**: `metric: cost` deleted — `quota.limits[].metric` accepts only `requests`/`tokens`;
  a `metric: cost` Limit is a load-time error naming the migration (convert budget ÷ price once,
  express per-model/per-component differences with `model_multipliers`/`token_weights`).
- **Runtime**: `core.Endpoint` no longer carries any pricing field, `core.PricingSpec.Currency`
  removed, `internal/pricing.FoldSpec`/`ResolveOptions.ExchangeRateToTarget` removed,
  `quota.Counters.Cost` and its charge/estimate machinery removed. The routing hot path is now
  structurally zero-price, zero-currency — KNOWN_ISSUES §1.0's "no price data in the hot path"
  red line is no longer a discipline to maintain, it's an invariant nothing can violate by
  construction.
- **Reports**: `vmr report`/`vmr analyze`'s `$` estimates are unaffected in substance — same
  two-layer resolution, same `Resolver`/`RateFor` API, same display-currency mechanism — only
  the config field names and the top-level exchange-rate location changed.
- **New**: `internal/pricing/standard_exchange_rate.yaml`, a built-in default exchange-rate
  table so most deployments never need to declare `exchange_rate:` by hand.
- **Deleted dead code found during this pass**: `configDir`/`resolveConfigRelative`/
  `providerModels` (config-relative-path resolution for the now-gone supplement file);
  `cmd/vmr/cmd_status_render.go`'s two unreachable `metric == "cost"` branches;
  `internal/router/quota_cost_test.go` (11 cost-metric-only tests, wholesale deleted per
  decision log #4).
- **Docs brought to current-state accuracy** (the bulk of this session's own work, see decision
  log #12/#13): `docs/VirtualModelRouter_Design_v4_Quota.md` §4.2/§7.1/§9.1/§9.2/§12.2/§13/§15
  rewritten (not just patched); `_Core.md` and `_Analytics.md` module-map/field-list corrections;
  `docs/UserGuide.md`/`.zh.md`'s entire pricing section rewritten (was still the old three-layer/
  `metric: cost` shape in full); `CHANGELOG.md` `[Unreleased]` reconciled — two new **Breaking**
  entries added, ~15 stale entries from the same pre-release development window fixed or
  removed; `docs/KNOWN_ISSUES.md` §1.2/§2.58/§2.58a corrected; `CLAUDE.md`'s own module-map table
  (a factually wrong claim that `config` enforces a `metric: cost` completeness gate, and that
  `internal/pricing` does three-layer resolution) fixed — this one mattered most, since every
  future session reads it as ground truth; `config.example.yaml`/`.zh`'s two stray cross-
  references to a soon-to-be-deleted planning doc repointed to the permanent design doc;
  `internal/pricing/standard_price_curated.yaml`'s three stale comments fixed.

**Verification**: `go build ./...`, `go vet ./...`, `go test -race ./...` (all 36 packages
green), `go test ./internal/archtest/...`, `gofmt -l .`, `shellcheck vmr.sh vmr-loadtest.sh` —
all clean.

**Not done / explicitly out of scope**: nothing. Every tracker item landed. The two
future-strategy planning docs and `docs/tasks/TASK_SPEC_CORE.md` remain on disk pending the
post-merge cleanup noted above under "Open items for user review".
