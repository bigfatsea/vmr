#!/usr/bin/env bash
# Ver 2026-07-31, by Sonnet 5
#
# vmr-loadtest.sh — one-command wrapper around loadtest/runner. Pure glue,
# no new logic: rebuild ./vmr fresh, make sure vegeta is on PATH, then hand
# off to `go run ./loadtest/runner`, which does the real work (starts
# mockupstream + vmr on isolated ports 9900/8801, generates targets, fires
# three escalating Vegeta load rounds at all 12 scenarios, computes the
# per-scenario server-side breakdown directly from the resulting audit log,
# writes reports/loadtest-report.md) and cleans up its own subprocesses on
# exit. The runner never shells out to `vmr report` (an earlier version
# did; see runner/main.go's own history note) — a load test's result must
# not depend on a separate command's rendering pipeline.
#
# Generated files land in the project's normal logs/reports/ directories,
# not loadtest/ itself — namespaced (logs/loadtest/, reports/loadtest-*) so
# they can never mix with or overwrite real data living in those same dirs.
#
# Design/rationale: docs/VirtualModelRouter_Design_v4_Core.md §12
# Full manual steps + how to read the numbers: loadtest/README.md
#
# This is a one-off sanity check (e.g. before a release push), not a CI
# job — nothing here is wired into CI, run it by hand when you want numbers.
#
# Usage:
#   ./vmr-loadtest.sh          run the full sweep (~1 min), then print the report
#   ./vmr-loadtest.sh show     print the last report without rerunning
#   ./vmr-loadtest.sh clean    remove generated artifacts (logs/targets/report)

set -euo pipefail
cd "$(dirname "$0")"

REPORT="reports/loadtest-report.md"

cmd_run() {
  if ! command -v vegeta >/dev/null 2>&1; then
    echo "vegeta not on PATH — install it first:" >&2
    echo "  go install github.com/tsenart/vegeta@latest" >&2
    echo "  (then make sure \$GOPATH/bin or \$GOBIN is on PATH)" >&2
    exit 1
  fi

  echo "== building ./vmr =="
  go build -o vmr ./cmd/vmr

  echo "== running load test (light -> moderate -> heavy) =="
  go run ./loadtest/runner

  echo
  echo "report: $REPORT"
}

cmd_show() {
  if [[ ! -f "$REPORT" ]]; then
    echo "no report yet — run ./vmr-loadtest.sh first" >&2
    exit 1
  fi
  cat "$REPORT"
}

cmd_clean() {
  # Scoped to exactly what this tool writes — never `rm -rf reports/` or
  # `rm -rf logs/`, both hold real, non-loadtest data too.
  rm -rf logs/loadtest loadtest/targets.json loadtest/targets-plain.json loadtest/targets-image.json
  rm -f "$REPORT"
  echo "cleaned loadtest artifacts"
}

case "${1:-run}" in
  run)   cmd_run ;;
  show)  cmd_show ;;
  clean) cmd_clean ;;
  *)
    echo "usage: $0 {run|show|clean}" >&2
    exit 2 ;;
esac
