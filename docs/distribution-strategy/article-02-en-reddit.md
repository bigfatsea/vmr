# Why translation gateways are losing your agent's bytes — and what to use instead

> **Target**: Reddit r/LocalLLaMA, r/ClaudeAI
> **Narrative**: Technical deep-dive → concrete example → what to use
> **Length**: ~1000 words
> **Prerequisite**: D1 complete

---

Most people running local models or routing between providers use a translation gateway — LiteLLM, claude-code-router, or one of the many OpenAI-compatible proxies. These tools have one job: make different APIs look the same to your client.

They do that job well. But there's a cost that nobody talks about, and for **agent workloads** it matters more than you think.

---

## What translation gateways actually do to your bytes

Here's a concrete example. Your client sends:

```json
{"model":"claude","reasoning_effort":"high","messages":[...]}
```

1. The gateway parses this into its internal schema. `reasoning_effort` hasn't been added to that schema yet — it's a new parameter.
2. The gateway re-serializes the request for the target provider. `reasoning_effort` is silently dropped.
3. The provider never sees it. Your agent gets a response with default reasoning effort. Nobody knows.

This isn't hypothetical. It happened with `reasoning_effort`, it happened with `cache_control` variants, it happens every time OpenAI or Anthropic adds a new parameter. The gateway's schema is always playing catch-up.

**But the response path has an even worse problem.**

A Chinese provider returns HTTP 200 with `"output_sensitive": true` and an empty `content` — a "soft block." The translation gateway normalizes the response. The empty body is valid JSON — nothing to flag. Your agent receives an empty response, treats it as a valid model output, and continues. Hours of work silently derailed.

A translation gateway can't catch this because **checking the semantic content of a response body is not in its design scope.** Its job is structural translation — JSON → internal format → JSON. What the text in `content` actually says is not its problem.

---

## The alternative: byte-faithful passthrough

VMR (Virtual Model Router) takes the opposite approach. It never translates. It checks which upstream speaks the same protocol as your client, rewrites the model name, and forwards everything else as-is.

This sounds simpler — and it is: one Go binary, zero dependencies. But it means VMR can do things translation gateways structurally cannot:

- **Record both layers of every request** — what your client sent AND what the upstream received. Since they differ only in the model name, the audit log is a complete forensic record.
- **Detect soft-blocking** — when a provider returns 2xx with `"input_sensitive":true` or `"output_sensitive":true` markers, VMR flags it in the audit trail. Translation gateways normalize these markers away.
- **Replay any request** — `vmr replay` resends the exact bytes from any historical request. You can reproduce failures deterministically.
- **Strip thinking leakage** — MiniMax's M3 model leaks ` thinking... response` XML blocks and "Thinking Process:" drafts into the output. VMR strips them at the gateway before your agent sees them.
- **Agent-shaped reporting** — sessions → tasks → turns, with per-turn delta markers and tool-usage analysis. Not flat HTTP logs.

---

## When to use what

| Use a translation gateway when... | Use VMR when... |
|---|---|
| You need providers with fundamentally different native protocols | All your providers speak OpenAI or Anthropic protocol |
| You want a web dashboard and team management | You want a single binary, no database, no UI |
| You're optimizing for maximum provider coverage | You're optimizing for debuggability and correctness |
| You're running a platform for multiple users | You're running your own agents |

---

## The architecture trade-off, stated plainly

Translation gateways and VMR represent two fundamentally different answers to one question: **"What is the job of an LLM router?"**

Translation gateways answer: "Make any API look like any other API." This requires an intermediate representation, which means you lose byte-level fidelity.

VMR answers: "Record and replay everything that happens between your agent and your providers." This requires never translating, which means you can only route within the same protocol family.

Neither answer is wrong. They're optimized for different problems. The issue is that the second answer — "record and replay everything" — is a problem that matters enormously for agent workloads, and almost nobody is solving it.

---

## Installation

Single binary, MIT licensed, personal tool (no SaaS, no enterprise edition):

```bash
brew install bigfatsea/tap/vmr
# Or: https://github.com/bigfatsea/vmr/releases/latest
```

**Repo**: [github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)