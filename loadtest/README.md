<!-- Ver 2026-07-20 00:05, by Sonnet 5 -->

# vmr load test — runbook

Design and rationale: [`docs/PerformanceTesting_Design_Sonnet5.md`](../docs/PerformanceTesting_Design_Sonnet5.md). This is a one-off sanity check, not a maintained benchmark suite — run it when you actually want the numbers (e.g. before a release push), not on every commit. Nothing here is wired into CI.

Eleven scenarios, each its own virtual model in [`config.yaml`](config.yaml), each exercising a code path with a genuinely different cost profile: `baseline` (routing floor), `stream_normal` (true SSE passthrough), `thinking_leak` (the known-worst full-buffer path), `think_tag` (buffer-then-resume path), `big_response` (large non-streaming body), `big_image` / `multi_image` (image decode/scale/encode, single and multi-image), `gif` (confirms the never-rescale fast path stays cheap), `long_history` (large-body parsing/audit cost), `failover` (health/cooldown machinery), `anthropic_baseline` (the other protocol adapter).

## Prerequisites

```bash
go install github.com/tsenart/vegeta@latest   # one-time; needs $GOPATH/bin or $GOBIN on PATH
go build -o vmr ./cmd/vmr                     # from the repo root
```

## Run it

```bash
go run ./loadtest/runner
```

This one command does everything: builds and starts `mockupstream`, starts `./vmr` against `loadtest/config.yaml`, generates `targets.json`, then fires three escalating Vegeta load rounds — `light` (10 req/s × 10s), `moderate` (50 req/s × 20s), `heavy` (150 req/s × 20s) — at all 11 scenarios combined. After the last round it stops both processes, runs `./vmr report` on the combined audit log, and writes everything — Vegeta's client-side percentiles per round, plus vmr's own per-scenario `按模型`/`端点可用度` tables — into a single **`reports/loadtest-report.md`**.

Generated files live in the same `logs/`/`reports/` directories a real vmr instance uses — not scattered under `loadtest/` — but namespaced so they can never mix with or overwrite real data: the audit log goes to `logs/loadtest/` (its own subdirectory, wiped clean before every run), and the only file written under `reports/` is `reports/loadtest-report.md` (the `loadtest-` prefix is deliberate: `vmr report`'s own output filenames are fixed and unprefixed, so this runner stages that raw output in a throwaway temp dir and never lets it touch `reports/` directly). `loadtest/targets.json` is deleted again as soon as the run finishes — it's regenerated (with fresh synthetic images) every time, never worth keeping around. Nothing this produces is committed; don't hand-edit or commit any of it.

To change the load profiles (e.g. push `heavy` further), edit the `profiles` slice at the top of [`runner/main.go`](runner/main.go) — there's nothing else to configure.

## Reading the numbers

- **`report.md`'s first table (client-side) is what an external caller experiences** as load increases round over round. Its second half (server-side, from `vmr report`) is where the per-scenario cost breakdown lives — `thinking_leak`/`big_image`/`multi_image` should stand out from the ~1ms floor everything else sits at; that gap is vmr's real, measured CPU cost for those code paths, not noise.
- **`thinking_leak` should show close to zero TTFB benefit from streaming** (`stream: true` in the request, but the response only shows up once fully generated) — that's the known, accepted cost documented in the design doc, not a bug. Compare its `dur_ms` p50/p95 against `stream_normal`'s to see the gap in absolute terms.
- **`failover`'s numbers reflect steady state, not "every request pays for 3 attempts."** `mock_fail1`/`mock_fail2` always return 500, so after the first failure each enters a short exponential-backoff cooldown (health.go) and gets skipped by later requests until it expires — a sustained run mostly measures both failing endpoints cooling down, most requests going straight to `mock_ok`. That's realistic (a genuinely dead endpoint shouldn't be retried every single request either).
- **Zero errors expected everywhere** except the two `mock_fail*` endpoints (which are supposed to fail — that's the failover scenario's setup, not a bug). Any other non-zero error rate is a correctness signal worth investigating on its own, not just a performance one.
- If everything above looks unremarkable, that's the expected, good outcome — stop here. Only reach for `go test -bench` on the specific hot function if a specific scenario's numbers actually look wrong (see the design doc §5).

## Poking at a single scenario manually

The runner is the normal path; drop to manual steps only when you want to isolate one scenario (e.g. with curl, or a debugger attached to vmr) instead of running the full multi-round sweep:

```bash
# 1. Mock upstream — never touches a real provider, just simulates response shapes.
go run ./loadtest/mockupstream

# 2. vmr itself, pointed at the mock via loadtest/config.yaml (separate port: 8801).
./vmr start -c loadtest/config.yaml

# 3. Generate the attack targets (embeds synthetic images/GIFs — this is why
#    targets.json isn't checked in).
go run ./loadtest/gentargets

# 4. Fire at everything, or edit targets.json down to one line first to
#    isolate a single scenario.
vegeta attack -targets=loadtest/targets.json -format=json -rate=20 -duration=30s | vegeta report

# 5. vmr's own per-scenario view — no new tooling needed, it already buckets
#    by virtual model and every request already has ttft_ms/dur_ms in the audit log.
#    -o an explicit scratch dir here, not the default ./reports — that's
#    where real report data lives, and vmr report's output filenames are
#    fixed (vmr-report.md etc.), so an unqualified run would overwrite it.
./vmr report -o /tmp/vmr-loadtest-manual logs/loadtest/vmr-audit-*.jsonl
cat /tmp/vmr-loadtest-manual/vmr-report.md   # "按模型" (ByModel) table = per-scenario p50/p95
```

## Cleanup

The runner cleans up its own subprocesses and stops on its own; nothing to kill manually after `go run ./loadtest/runner` exits. If you ran the manual steps above instead:

```bash
# Ctrl-C the mockupstream and vmr processes from steps 1-2.
rm -rf logs/loadtest loadtest/targets.json /tmp/vmr-loadtest-manual
# Only remove reports/loadtest-report.md, never the whole reports/ dir —
# that's where real vmr report output lives too.
rm -f reports/loadtest-report.md
```
