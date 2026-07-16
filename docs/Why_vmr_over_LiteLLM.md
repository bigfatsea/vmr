<!-- Ver 2026-07-16 17:00, by Sonnet 5 -->

# Why vmr over LiteLLM (or Portkey, or any other translation gateway)

English | [简体中文](Why_vmr_over_LiteLLM.zh.md)

**A universal travel plug adapter changes the shape of the plug so it fits a different socket — the electricity itself passes through unconverted. A voltage transformer changes the electricity itself. LiteLLM is a transformer; vmr is a plug adapter.**

## The one-sentence version

LiteLLM (and most "universal LLM gateways") work by translating every request into one canonical internal format, then translating it back out to whichever provider you picked. vmr never translates anything — it looks at the protocol your client already speaks (OpenAI or Anthropic), picks an upstream that speaks the same protocol, and forwards your bytes essentially untouched. One approach optimizes for "support every provider, even ones with a totally different shape." The other optimizes for "never be the reason your agent's request looks different from what you actually sent."

If you're running an AI agent — something that calls the API unattended, retries on its own, and can't ask a human "hey does this response look right to you?" — that second property matters more than it sounds like it should.

## What actually breaks with translation

Say a provider ships a new parameter tomorrow — `reasoning_effort`, a new `cache_control` block shape, a new `tool_choice` mode, whatever. A translation gateway has to explicitly learn that field before it can pass it through: parse it into its internal schema, map it back out on the way to the provider. Until someone patches the gateway, that field either gets silently dropped or the request fails outright.

vmr doesn't have an internal schema for your request to survive translation into. It splices exactly one field (`model`, so your virtual name becomes the real upstream name) and forwards every other byte — key order, whitespace, unknown parameters — exactly as your client sent them. A provider's brand-new parameter works through vmr on day one, with zero vmr code change, because vmr never had an opinion about what fields exist in the first place.

This isn't hypothetical. This very codebase has two narrow, explicitly-scoped repairs for MiniMax-specific quirks (stripping a `<think>` tag that would otherwise poison conversation history, and stripping a plain-text "thinking" section MiniMax emits under certain modes) — and *nothing else*. That's the entire list of byte-level deviations from a direct connection, after building this against several providers in real production use. Everything else — every OpenAI/Anthropic field, known or not-yet-invented — just goes through.

## Side by side

| | vmr | LiteLLM |
|---|---|---|
| Request handling | Byte-faithful passthrough — no internal schema | Translates every request into a canonical format |
| New provider params (e.g. `reasoning_effort`) | Work day one, no code change | Need explicit support before they're forwarded |
| Response streaming | True passthrough — SSE chunks forwarded as they arrive | Re-serialized through the canonical format |
| Deployment | Single Go binary, 4 direct dependencies, zero DB | Python; full feature set typically wants Postgres + a web dashboard |
| Config | One YAML file | Config file + optional DB-backed admin UI |
| Provider coverage | Any OpenAI/Anthropic-*protocol*-compatible endpoint | 100+ providers, including ones with genuinely different native protocols |
| Agent-session awareness | Built in: groups requests into sessions → tasks → turns, flags unused declared tools, tracks per-turn deltas | Not a design focus |
| Audit trail | Every request, every failover attempt, every byte-level normalization applied — one JSONL line, replayable | Logging exists, not agent-session-shaped |
| Best fit | One (or a few) agent runtimes you trust, running unattended, where byte fidelity matters more than provider count | Centralized team gateway serving many providers/users with spend management and RBAC out of the box |

Simplicity isn't just a design preference here — it's cheap in practice too: load-tested at up to 150 req/s, vmr's own routing overhead sits at single-digit milliseconds (p95) on 9 of 11 tested scenarios ([`loadtest/`](../loadtest/)).

## When you actually want LiteLLM instead

Being straight about this: vmr is not a universal replacement.

- **You need providers with genuinely different protocols** — not just a different base URL, but a fundamentally different request/response shape LiteLLM already has a translator for (some embeddings-only APIs, some local-model servers with bespoke formats). Passthrough only works between protocol-compatible endpoints; that's the tradeoff for never touching your bytes.
- **You need a hosted, multi-tenant gateway** with built-in spend tracking, per-team budgets, and an admin dashboard, serving many different downstream users who shouldn't share config files. That's a product LiteLLM already built; vmr deliberately doesn't have a web UI or a database.
- **You want the safety net of a widely-adopted, heavily-maintained translation layer** even if it occasionally lags a day behind a provider's newest field. Reasonable choice if provider breadth matters more to you than byte-for-byte fidelity.

If none of those describe your situation — if what you actually have is one agent (or a small, trusted fleet of them) hitting a handful of OpenAI/Anthropic-compatible providers, and what you want is "my client's request reaches the provider exactly as written, and I can see exactly what happened when it didn't work" — that's the one job vmr does, and it does only that job.

---

See also: [design doc](VirtualModelRouter_System_Design_v2.md) for the full architecture and every design decision behind it, [User Guide](UserGuide.md) for configuration and CLI reference.
