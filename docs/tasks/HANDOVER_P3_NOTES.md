# Phase 3 Worker 移交笔记合并（3A + 3B，主控整理）

## 3A (report ViewModel)
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

## 3B (journey ViewModel)
# NOTES_FOR_LEAD — Group 3B (journey ViewModel / spine reconstruction)

## 1. Data-layer extensions beyond "render_md/render_spine minimal adaptation"

The byte-equivalence requirement (old `RenderMarkdown(*Journey)` ≡ new
`RenderMarkdownFromSummary(JourneySummary)`) forced the published
JourneySummary to become truly rendering-complete. These are **additive,
omitempty** fields (design §7.4: additive = non-breaking), all inside
`internal/journey/**`:

- `StepStructure`: `model`, `protocol`, `outcome`, `instruction`,
  `has_error_marker`, `resp_is_reasoning`, `reasoning_ref`,
  `sys_hash`/`sys_chars`, and `ToolCallRef.repeat`.
  - `repeat` stamps `toolCallRepeats`' per-call flag at build time: the exact
    repeat identity hashes FULL arguments, which the truncated bodies blob
    table cannot reproduce — stamping avoids that entire divergence class
    (AGENTS.md's "stamp the fact where it acts" pattern).
  - `has_error_marker` stamps the `isErrorMarker` scan over the Step's own
    NewEvents — the only thing the renderer ever needed event *text* for, so
    the text itself stays referenced-only instead of dragging every message
    body into the blob table for a boolean.
  - `model`/`outcome`/`endpoint` recompute the detail-page filename via
    `reqdetail.FileName` exactly as `FileNameForManifest` does.
- `JourneySummary` (now in its own `summary.go`, moved out of metrics.go which
  was at its 470-line archtest budget): `break` (head lineage's unresolved
  break, as an `EditRef`) and `deliverable` (stamped `deliverableStats` — its
  file-write detection also reads full, untruncated arguments).

## 2. Published JSON carries the bodies blob table TWICE (recommend fix in 3C/4)

`JourneyStructure.Bodies` still has `json:"bodies,omitempty"` while
`NewJourneySummary` copies the same map to the top-level `JourneySummary.Bodies`
— so `j-<id>.json` serializes the full blob table under both `structure.bodies`
and `bodies` (~2x blob bytes). The design (§3.6) puts bodies once, top level.
Fix is `json:"-"` on `JourneyStructure.Bodies` + moving the
`TestBuildStructure_VolumeBoundedByStepsNotProseLength` size guard to marshal
`JourneySummary` instead. Left untouched here: it's a published-schema change
outside 3B's mandate and nothing in 3B depends on it (the VM reads the
top-level `Bodies` only).

## 3. Dead code observed but NOT in 2B's handover list

`internal/journey/mdlite.go` + `mdlite_test.go`: `mdToHTML` has zero
production callers since 2B deleted `render_compare_html.go`. Not deleted here
(3B's Task 4 enumerates its deletions explicitly); recommend 3C delete it
alongside the old render path.

## 4. Retired per Task 4

`internal/i18n/story_html.go`, `story_compare_html.go` (+ each `_test.go`),
`journey.ComputePointOfNoReturn` (+ file/types/tests — the whole
pointofnoreturn.go cluster had zero production consumers),
`journey.JourneySeverity`/`pickDriver`/`findingLevel`/severity consts. The two
maps `criticalFindings`/`lowConfidenceFindings` survived (their only live
consumer is findings display-tier grouping) and moved to their own
`internal/journey/finding_tier.go` next to `findingTrustTier`, which both
render paths read. `PartialBanner` moved from the retired `StoryHTMLText` into
`StoryText` (same sentence, same value, both languages).

## 5. Task 2 (fixed serializer) — sharing adjudication

Journey-side `SerializeJourneyVM` is fully self-contained per the group's red
line. One candidate for future下沉 surfaced and was NOT acted on: the
report-side cell-based `TableVM` (Headers/Aligns/Rows) and journey-side
`VMTable` (pre-localized header+separator block + whole-row strings) could
share a cell-join pure function, but the two halves' table semantics genuinely
differ (§11.1 item 1) and unifying them would couple their i18n header
conventions. Lead's call if a third consumer ever appears.

## 6. Pre-existing archtest failure (not 3B's)

`go test ./internal/archtest/` fails `TestArchitecture_DocReferences` on this
branch **before any 3B change** (verified by stashing): it rejects
`docs/KNOWN_ISSUES.md`'s reference `internal/dashboard/testdata/fmt_cases.json`
as "not a valid package" (it's a data file, not a package). Both
`docs/KNOWN_ISSUES.md` and `internal/archtest/**` are outside 3B's whitelist.
Everything else under archtest (file sizes, func sizes, import boundaries,
i18n pairing) passes.

## 7. Test-coverage map for §9 (what replaced what)

- `structure_test.go TestBuildStructure_LosslessReconstruction`: rewritten as
  the true self-contained assertion — file `j-<id>.json` in, `.md` out,
  byte-equal to the old render path; audit log is not an input.
- `golden_test.go TestGoldenMarkdown`: sunk to VM structure
  (`testdata/golden_vm*.json`, UPDATE_GOLDEN regenerates) while keeping the
  serialized `.md` byte-golden as serializer smoke.
- `viewmodel_test.go`: old/new byte-equivalence matrix (golden/rich/syschange
  fixtures × EN/ZH × linkDetails on/off, through a real JSON round trip);
  §9's match↔spine-pairing guard and every-step-rendered guard; §9's bodies
  no-orphans/no-dangling guard lives in `structure_test.go`
  (`TestBuildStructure_BodiesNoOrphansNoDangling`, summary-level through the
  real pipeline; 1C's structure-level `BodiesIntegrity` remains).
- Old render path (`render_md*.go`, `render_spine*.go`) kept intact for 3C to
  delete; only its `PartialBanner` source moved to `StoryText`.
