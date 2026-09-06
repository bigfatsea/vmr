# NOTES_FOR_LEAD — Group 3A (report-side ViewModel + fixed serializer)

Items for the lead to register/decide (shared files are lead-exclusive, so
they are listed here instead of edited directly).

## 1. Legacy English literal reproduced in viewmodel_cost.go (D4 gap)

`section_cost.go` (legacy §2) composes a pricing-traceability summary line
with two inline English literals — `standard table generated %s` and
`; %d provider rate rule(s) applied` — that never went through
`internal/i18n`. The ViewModel builder (`viewmodel_cost.go`) reproduces them
verbatim to keep the transition byte-equivalence test honest (it is a string
comparison). When 3C deletes the legacy path, these two literals should move
into `internal/i18n/report_cost.go` (new func fields), which will change
`vmr-report.md` bytes — schedule as its own wording change, not as part of
the 3C deletion.

## 2. Pre-existing archtest failure on clean HEAD

`go test ./internal/archtest/` currently fails
`TestArchitecture_DocReferences`:
`docs/KNOWN_ISSUES.md: internal/dashboard/testdata/fmt_cases.json is not a
valid package`. The path is a planned §5.6 cross-language fixture artifact
(design doc §9) that no group has created yet. Verified present on a clean
checkout of this branch's base (stashed working tree) — not introduced by
Group 3A. Either create the fixture path or reword the KNOWN_ISSUES entry.

## 3. vm-prefixed helper duplicates retire with the legacy path

The transition duplicates a set of small pure helpers under a `vm` prefix
(rather than sharing symbols with the to-be-deleted legacy files) so the two
renderers stay independent. After 3C deletes `render_doc.go` + `section_*.go`,
rename the survivors back (drop the `vm` prefix) and delete the legacy
originals; they are:

- viewmodel_doc.go: `vmSummaryCostCell`, `vmSummaryInteractiveShare`,
  `vmHighlights`, `vmTopErrorClassCount`, `vmTopErrorClass`
  (legacy: render_doc.go's `summaryCostCell`, `summaryInteractiveShare`,
  `highlights`, `topErrorClassCount`, `topErrorClass`)
- viewmodel_cost.go: `vmMoney`, `vmCostTotal(Of)`, `vmSkipRow`
  (legacy: section_cost.go's `money`, `costTotal(Of)`, `skipRow`)
- viewmodel_reliability.go: `vmEndpointProtocol`, `vmProtocolBuckets`,
  `vmErrorRateMarker`, `vmTopErrorClassShort` (legacy: section_reliability.go)
- viewmodel_sessions.go: `vmSplitSessionLongTail`,
  `vmFormatSessionTimeRange`, `vmTruncateTitle`,
  `vmSessionsHeadRows`, `vmSessionsLongTailTurnCap`
  (legacy: section_sessions.go)
- viewmodel_compaction.go: `vmRetentionRatio`, `vmEntitySample`
  (legacy: section_compaction.go)
- viewmodel_endpoint_value.go: `vmValueRow`, `vmEndpointValueRows`
  (legacy: section_endpoint_value.go)
- viewmodel_sticky.go: `vmStickyMinBasis`, `vmStickyRow`
  (legacy: section_sticky.go)
- viewmodel_efficiency.go: `vmTwTokens`, `vmToolWasteBytesPerToken`
  (legacy: section_efficiency.go)
- viewmodel_provider.go: `vmQuotaRowProviderCell`,
  `vmTopErrorClassProviderCell`, `vmPeriodRangeCell`
  (legacy: section_provider.go)

`vmSkippedAttemptsNote` (viewmodel_provider.go) instead captures the legacy
`renderSkippedAttemptsNote` (providerquota.go, which survives) — after 3C it
should call that logic directly.

## 4. Docs mentioning the old pairing target

AGENTS.md's module map (and Part 2's §1.2) describe the i18n pairing as
`i18n/report_*.go` ↔ `report/section_*.go`. `internal/archtest/i18n_test.go`
now pairs against `viewmodel_*.go`. Update the doc text when 3C lands (or at
the next docs pass); this group does not touch shared docs.

## 5. Deliberate deviations from design-doc §5.2's sketch

Documented in `internal/report/viewmodel.go`'s package comment: Highlights
render inside §0 (where the legacy document puts them, and where the
byte-equivalence test needs them) rather than as a top-level field;
SectionVM carries an ordered `Blocks` list instead of separate
Intro/Tables (the current sections interleave paragraphs, group headings,
charts and several tables); TableVM omits `Aligns` (every current table is
plain left-aligned pipes — a passthrough field would fail the §10 benefit
criterion). `MacroReportVM` keeps Title/Sections/Disclaimers/Footnotes and
`FootnoteVM{ID, Text}` as sketched.
