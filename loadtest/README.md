<!-- Ver 2026-09-12 12:00, by Gemini 4.5 -->

# vmr load test — runbook

Design and rationale: [`docs/VirtualModelRouter_Design_v4_Core.md`](../docs/VirtualModelRouter_Design_v4_Core.md), "Performance validation" section. This is a one-off sanity check, not a maintained benchmark suite — run it when you actually want the numbers (e.g. before a release push), not on every commit. Nothing here is wired into CI.

Seventeen scenarios, each its own virtual model in [`config.yaml`](config.yaml), each exercising a code path with a genuinely different cost profile: `baseline` (routing floor), `stream_normal` (true SSE passthrough), `drip_stream` (true SSE slow drip, ~5s per response, creating dozens of concurrent in-flight streams to stress InflightRegistry, watchdog, and concurrent audit writes), `thinking_leak` (the known-worst full-buffer path), `think_tag` (buffer-then-resume path), `big_response` (large non-streaming body), `big_image` / `multi_image` (image decode/scale/encode, single and multi-image), `gif` (confirms the never-rescale fast path stays cheap), `long_history` (large-body parsing/audit cost), `failover` (health/cooldown machinery), `quota` (metering and headroom ranking against real usage), `sticky` (session fingerprint caching and endpoint pinning across requests), `anthropic_baseline` / `anthropic_stream` (Anthropic Messages protocol non-streaming and SSE streaming adapters), `responses_baseline` / `responses_stream` (OpenAI Responses protocol non-streaming and SSE streaming adapters).

## Prerequisites

```bash
go install github.com/tsenart/vegeta@latest   # one-time; needs $GOPATH/bin or $GOBIN on PATH
go build -o vmr ./cmd/vmr                     # from the repo root
```

## Run it

```bash
go run ./loadtest/runner
```

This one command does everything: builds and starts `mockupstream`, starts `./vmr` against `loadtest/config.yaml`, generates `targets.json` (plus its `targets-plain.json`/`targets-stream.json`/`targets-image.json` cost-regime subsets, see below), runs a short 2s warmup round, then fires three escalating Vegeta load rounds — `light` (10 req/s × 10s), `moderate` (50 req/s × 20s), `heavy` (150 req/s × 20s) — across all 17 scenarios. After the last round it samples peak process RSS and CPU usage, stops both processes, asserts that scenario request counts match expected target shares (±20% tolerance), and writes everything — VCS revision stamp, Vegeta client-side percentiles per round, per-round/per-scenario `按模型` server-side breakdown and `端点可用度` computed directly from this run's own audit JSONL (with 0ms sub-millisecond durations preserved), plus process resource consumption — into a single **`reports/loadtest-report.md`** (mode 0600). This is a load test, not a report test: server-side numbers come from parsing the audit log itself (`computeServerStats` in [`runner/main.go`](runner/main.go)), never from running `vmr analyze` — the runner never shells out to it and never imports `internal/report`. A load test's result must not depend on a *different* command's rendering pipeline; run this having never once run `vmr analyze` against anything, and the result is identical.

**Client-side percentiles are reported as three distinct cost-regime groups, not one blended number**:
- `plain` (13 scenarios — cheap routing floor, non-drip streams, adapters, quota, sticky, failover)
- `stream` (1 scenario — `drip_stream`, ~5s long-lived streaming connections)
- `image` (3 scenarios — `big_image`/`multi_image`/`gif`, image decode/scale/encode)

Image processing and long drip streams each represent completely different orders of cost magnitude compared to routing fast paths. Blending them into one combined figure would let their latency quietly drag up the p95/p99/max for plain scenarios. Each round fires the three groups as **separate Vegeta attacks**, each at its proportional share of the round's nominal rate — so splitting changes only how results are bucketed for reporting, not how hard vmr is actually hit; total load per round remains identical.

Generated files live in the same `logs/`/`reports/` directories a real vmr instance uses — not scattered under `loadtest/` — but namespaced so they can never mix with or overwrite real data: the audit log goes to `logs/loadtest/` (its own subdirectory, wiped clean before every run), and the only file written under `reports/` is `reports/loadtest-report.md` (0600). `loadtest/targets*.json` files are deleted automatically as soon as the run finishes. Nothing this produces is committed; don't hand-edit or commit any of it.

To change the load profiles (e.g. push `heavy` further), edit the `profiles` slice at the top of [`runner/main.go`](runner/main.go) — there's nothing else to configure.

## Reading the numbers

- **`loadtest-report.md`'s first table (client-side) is what an external caller experiences** as load increases round over round — split into `plain`, `stream`, and `image` rows per round. Plain scenarios show sub-millisecond median latency even under heavy load; stream shows ~5000ms duration with ~100ms TTFB; image shows decode/scale cost scaling with cache hits. Small sample sizes (< 200 requests) carry a `~` prefix on p99 to denote statistical noise.
- **`按模型` server-side breakdown is bucketed per load round** (excluding warmup), allowing you to observe degradation curves for each scenario across escalating load profiles. Durations of 0ms denote sub-millisecond durations (left-censored by integer millisecond timestamps) and are included rather than filtered out, avoiding upward sample bias.
- **`big_image`/`multi_image` use cache-busting variants interleaved across lines.** `gentargets` generates 50 distinct images cycling across requests, and interleaves lines between `big_image`, `multi_image`, and `gif` so sequential line consumption by Vegeta distributes requests evenly even in short rounds.
- **`drip_stream` creates sustained concurrency.** With ~5s duration per request, heavy round rates push dozens of simultaneous active streams through the router, confirming stream passthrough and in-flight tracking under load.
- **Resource usage at the bottom** reports peak RSS and cumulative CPU time sampled via background process polling every 500ms during the entire sweep.
- **Zero errors expected everywhere** except the two `mock_fail*` endpoints in the failover scenario.

## Poking at a single scenario manually

The runner is the normal path; drop to manual steps only when you want to isolate one scenario:

```bash
# 1. Mock upstream
go run ./loadtest/mockupstream

# 2. vmr against loadtest config (port 8801)
./vmr start -c loadtest/config.yaml

# 3. Generate targets (writes targets.json, targets-plain.json, targets-stream.json, targets-image.json)
go run ./loadtest/gentargets

# 4. Attack
vegeta attack -targets=loadtest/targets-plain.json -format=json -rate=20 -duration=30s | vegeta report

# 5. Optional analyze
./vmr analyze -o /tmp/vmr-loadtest-manual logs/loadtest/vmr-audit-*.jsonl
```

## Cleanup

The runner cleans up its own subprocesses and targets on exit. For manual runs:

```bash
rm -rf logs/loadtest loadtest/targets*.json /tmp/vmr-loadtest-manual
rm -f reports/loadtest-report.md
```
rm -f reports/loadtest-report.md
```
