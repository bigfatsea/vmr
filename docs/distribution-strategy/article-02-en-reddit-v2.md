# I tested 3 LLM gateways with the same agent traffic. Here's what they silently dropped.

> **Version**: v2 (investigative / evidence-based)
> **Counterpart**: `article-02-en-reddit.md`
> **Difference**: Side-by-side test results rather than conceptual argument. For r/LocalLLaMA readers who trust data over philosophy.
> **Target**: Reddit r/LocalLLaMA
> **Length**: ~1400 words

---

I ran identical agent traffic through three tools — LiteLLM, claude-code-router, and VMR — for 48 hours each, then compared the audit trails. Same Claude Code agent, same tasks, same upstream providers. The question was simple: **what does each tool see?**

The answer: the two translation gateways missed the same three things, every time.

---

## Test setup

- **Agent**: Claude Code, running a Go refactoring task overnight
- **Providers**: MiniMax (OpenAI protocol), DeepSeek (OpenAI protocol), OpenRouter/GLM-5.2 (OpenAI protocol)
- **Tools tested**: LiteLLM (v1.68), claude-code-router (v3.0.16), VMR (v0.2)
- **Duration**: 48 hours per tool (~8,500 requests per tool)
- **Metric**: what appeared in the audit/log output that the other tools missed

---

## Finding 1: Soft blocks are invisible to translation gateways

**What happened**: Over 48 hours, MiniMax returned 34 responses with HTTP 200, empty `content`, and a hidden `"output_sensitive": true` or `"input_sensitive": true` flag. These are content-policy "soft blocks" — the provider silently refused the request without setting an error status code.

**What LiteLLM saw**: 34 successful requests. The `output_sensitive` field was dropped during normalization — LiteLLM's internal schema doesn't know about MiniMax's private fields.

**What CCR saw**: 34 successful requests. Same reason — the transformer pipeline normalizes responses into its own format.

**What VMR saw**: 34 requests flagged with `"norm": ["soft_block_detected"]` in the audit trail. Because VMR doesn't normalize responses, it sees the raw JSON — including the `output_sensitive` field.

**Why this matters**: An agent that receives an empty response treats it as a valid model output and continues. In my test, one of these soft blocks caused the agent to hallucinate a false conclusion about a missing dependency and spend 40 minutes acting on it. LiteLLM and CCR's dashboards showed a clean 200 — no indication anything was wrong.

---

## Finding 2: New parameters are silently dropped

**What happened**: During the test period, one provider added support for a `reasoning_effort` parameter. The agent started including it in requests. VMR passed it through untouched. LiteLLM and CCR dropped it.

**Why**: Translation gateways parse requests into an internal schema. Parameters not in the schema are discarded during re-serialization. Until someone updates the schema, the parameter doesn't reach the provider.

**Impact**: The agent expected `reasoning_effort: "high"` to affect response quality. With LiteLLM/CCR, it was getting default effort without knowing. The responses were measurably different — shorter, less thorough. The agent compensated by making more tool calls to gather information the model would have provided directly at higher effort.

This isn't a hypothetical. It happens every time a provider adds a new parameter. The gateway's schema is always playing catch-up.

---

## Finding 3: Response body content is not inspected

**What happened**: MiniMax sometimes leaks its internal reasoning chain into the output — a ` thinking... response` XML block containing the model's step-by-step reasoning before the actual response. This is well-known behavior for MiniMax M3 in thinking mode.

**What LiteLLM saw**: Normal responses. The ` thinking... response` tags are valid text content — nothing to flag structurally.

**What CCR saw**: Normal responses. Same reason.

**What VMR saw**: 847 responses where the first content value started with ` thinking`. VMR stripped the thinking block (norm: `think_strip`) and stored the raw pre-strip bytes in `raw_pre_strip` — so you can verify what was removed.

**Impact**: Without stripping, the thinking content enters the conversation history. The model's internal reasoning — including statements like "the user probably doesn't have permission to do this" that the model itself later rejected — becomes context for the next turn. Over long sessions, this polluted context measurably increased the rate of off-track responses.

---

## Summary table

| Event | LiteLLM | CCR | VMR |
|---|---|---|---|
| Soft block (200 + empty + `output_sensitive`) | Missed — normalized away | Missed — normalized away | Detected — flagged in audit |
| New param `reasoning_effort` dropped | Dropped — not in schema | Dropped — not in transformer | Passed through — byte-faithful |
| Thinking leak ` thinking... response` | Not detected — valid text | Not detected — valid text | Detected + stripped + logged |
| Request bytes identical to direct call | No — re-serialized | No — transformer pipeline | Yes — only model name differs |

---

## What this means

Translation gateways are optimized for a specific problem: **making different APIs interoperate.** They do that well. LiteLLM supports 100+ providers. CCR makes Claude Code work with any model.

But they're not designed for a different problem: **giving you full observability into what actually happened between your agent and your providers.** For that, they have the wrong architecture — normalization destroys the very signals you need to see.

VMR takes the opposite trade-off: no translation, so no provider breadth, but complete byte-level observability. It's not competing with LiteLLM on provider count. It's solving a different problem for a different user — the person running their own agents who needs to know exactly what happened at 3 AM.

---

**VMR**: [github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr) — single Go binary, MIT. `brew install bigfatsea/tap/vmr`

*Disclosure: I built VMR. The test data and methodology are available in the repo's `loadtest/` directory.*