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
