// Ver 2026-08-05, by Sonnet 5

package fmtutil

import "time"

// DisplayZone is the system default timezone every human-facing rendering
// of a persisted timestamp must convert through — a live router/CLI log
// line, a `vmr report`/`vmr story` Markdown document, or an aggregation
// bucket key (e.g. the byDate/hour-of-day statistics in vmr-report.md).
// time.Local already resolves the OS/container TZ setting, which is
// exactly "the system default timezone"; this var exists so every call
// site is grep-able (`fmtutil.DisplayZone`, never a bare `.Format()` on a
// record's own embedded offset, never a hardcoded FixedZone) and so tests
// can override it deterministically.
//
// Raw JSON/JSONL records (audit.Record.TS, report Meta.GeneratedAt/From/To,
// RequestRow.TS, …) are deliberately NOT converted through this — they keep
// whatever offset time.Now() carried at write time (RFC3339, self-describing,
// no GMT normalization). Only rendering and aggregation cross this boundary.
var DisplayZone = time.Local
