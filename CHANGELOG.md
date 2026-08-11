<!-- Ver 2026-08-11, by Sonnet 5 -->

# Changelog

All notable changes to this project are documented here, in
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format. This project
tags releases `vX.Y` rather than strict semver (see `internal/buildinfo`'s
doc comment for why there is no other version file to keep in sync) — the
version headers below match the tag with the leading `v` dropped.

`release.yml`'s release job extracts the section matching the pushed tag and
uses it verbatim as the GitHub Release body — GitHub's own auto-generated
notes (bare PR titles) are unreadable on this repo's mostly direct-to-main
workflow, so every tag needs a matching section here before it's pushed, or
the release job fails on purpose rather than publishing something empty.
See this file's row in `CLAUDE.md`'s Conventions section for the write-time
process (write into `[Unreleased]` as you go, retitle it when cutting a tag).

## [Unreleased]
### Fixed
- `vmr.sh`'s `cmd_start` port-occupancy precheck was silently dead (its regex expected `vmr check`'s listen-address line as `listen=...`, but it prints `listen:`) — restored
- `server.chatHandler` no longer runs image downscaling for a request naming an unknown virtual model; that work was always discarded since `router.Serve` immediately 404s on the same lookup
- README's links to `docs/Why_vmr_over_LiteLLM.{md,zh.md}` were dead (the file had been swept into an untracked `archived/` directory during an unrelated cleanup) — restored to `docs/` and re-tracked

## [0.5] - 2026-08-11
### Added
- Quota-Aware Routing P1: single-bucket usage balancing across accounts sharing a virtual model (`requests`/`tokens`/`cost` metrics)
- Quota-Aware Routing P2: account weighting, cost-based pricing, model multipliers
- Multi-currency pricing: per-row/override currency, self-contained supplement exchange rates, `vmr report`'s display-currency rescale
- `vmr replay` now charges its real upstream consumption against quota

### Changed
- Simplified `metric: cost` pricing to static per-model rates; added `ErrContextLimit` failover
- `vmr report` now aggregates quirk-repair marker frequency by endpoint
- Refreshed the LiteLLM pricing snapshot and regenerated the standard price table
- Rewrote README/UserGuide for the router+flight-recorder pitch; reorganized architecture-review and issue-tracking docs
- Bumped `github.com/klauspost/compress` 1.19.1→1.19.2 (zstd concurrency/dictionary fixes relevant to the audit log's compression path) and CI Actions (`checkout`, `setup-go`, `upload-artifact`, `download-artifact`, `action-gh-release`) to their latest majors

### Fixed
- A flaky `internal/quota` store test caused by a period-start mismatch between two charges within the same test (not a bug in the quota package itself)

## [0.4] - 2026-08-05
### Added
- `vmr story`: Findings / decision-spine / corpus-stats / divergence-point layers
- File-content-hash cache for `vmr report`/`vmr story` parsing, plus a `report.yaml` sidecar config
- `-journey` comma/glob selectors; decision spine reworked as per-Step blocks carrying full args and why-lines

### Fixed
- Journey id timezone handling: drop the forced UTC, use the record's own write-time offset
- A shellcheck SC2016 false positive in `vmr.sh`'s `write_env_file`

## [0.3] - 2026-08-04
### Added
- OpenAI Responses protocol support (`POST /v1/responses`) — routing, diagnose, load test, report/story
- Multi-language (EN/ZH) output for `vmr report`/`vmr story`
- Homebrew tap (`bigfatsea/homebrew-tap`) — `brew install` now available

### Changed
- Simplified recovery probing and proxy config: dropped unused passive/global-default layers
- Right-sized response buffer caps to context-window scale

### Fixed
- OpenAI Responses response-side content extraction

## [0.2] - 2026-07-31
### Added
- Condition-based routing and Sticky Model session affinity
- Active health-probe mode
- `vmr story`: Journey/Task/Step narrative rendering, `-compare` LLM interpretation layer, `-render-all`
- Per-provider `role_map` for OpenAI-compatible providers that reject `developer`
- Prebuilt binaries, CI and release workflows

### Changed
- Replaced `vmr report` with a nine-section rewrite: cost estimate, Chat User grouping, ~70% faster runtime, multi-currency time-windowed pricing
- Flattened providers to a multi-protocol list; added model-level capability/context base
- `base_url` must now carry its own API version; dropped the overlap-dedup logic that used to compensate

### Fixed
- Upstream-gateway-failure misclassification
- Image-detection false positives
- Sticky TTL configs that exceed the sticky registry's 24h backstop

## [0.1] - 2026-07-13
First public release.

### Added
- Local-first, single-binary LLM router: point an OpenAI or Anthropic client at one stable virtual model name, hiding providers/accounts/keys/priority/failover behind it
- Byte-faithful passthrough for `POST /v1/chat/completions` (OpenAI) and `POST /v1/messages` (Anthropic) — no cross-protocol translation
- Failover: per-error-class cooldowns, exponential backoff, `Retry-After` respected, single-flight recovery probes; content-policy blocks switch providers without penalizing the endpoint
- Flight-recorder audit log: one JSONL line per request, both layers captured, auto-compressed to `.zst` on rotation, auto-expirable
- `vmr report`: usage/latency/availability stats, session→task→turn grouping, tool-usage report
- Optional inline-image downscaling, content-hash cached on disk, off by default

[Unreleased]: https://github.com/bigfatsea/vmr/compare/v0.5...HEAD
[0.5]: https://github.com/bigfatsea/vmr/compare/v0.4...v0.5
[0.4]: https://github.com/bigfatsea/vmr/compare/v0.3...v0.4
[0.3]: https://github.com/bigfatsea/vmr/compare/v0.2...v0.3
[0.2]: https://github.com/bigfatsea/vmr/compare/v0.1...v0.2
[0.1]: https://github.com/bigfatsea/vmr/releases/tag/v0.1
