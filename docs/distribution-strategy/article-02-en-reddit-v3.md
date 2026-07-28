# Two philosophies for LLM routing: translation vs passthrough

> **Version**: v3 (academic / analytical comparison)
> **Counterpart**: `article-02-en-reddit.md`
> **Difference**: Taxonomy-style analysis of two design philosophies rather than arguing for one over the other. For r/LocalLLaMA readers who enjoy architectural discussions.
> **Target**: Reddit r/LocalLLaMA
> **Length**: ~1200 words

---

There are exactly two ways to build an LLM router. Every tool in this space — from 55k-star LiteLLM to 2-star VMR — picks one. The choice determines what your tool can and cannot do, and you can't have both.

---

## The two architectures

### Translation routing (LiteLLM, Portkey, claude-code-router, Bifrost, Kong AI Gateway)

```
Client → [parse into internal schema] → [re-serialize for target provider] → Upstream
```

The router maintains an internal canonical representation of a chat completion request. Inbound requests are parsed into this format. Outbound requests are serialized from it. The schema must explicitly model every parameter the router knows about.

**Strengths**:
- Routes between fundamentally different protocols (OpenAI ↔ Anthropic ↔ Gemini)
- Can normalize behavior across providers (retry logic, error formatting)
- Provider count scales with schema coverage, not protocol compatibility

**Weaknesses**:
- Unknown parameters are silently dropped (schema must be updated before new features work)
- Response content is viewed through the lens of structural normalization — semantic anomalies (soft blocking, thinking leakage) are invisible
- The request the upstream receives is not byte-identical to what the client sent
- The audit record contains the translated versions, not the originals

### Passthrough routing (VMR, and to some extent simple reverse proxies like nginx)

```
Client → [rewrite model name] → Upstream
```

The router modifies exactly one field (the model name) and forwards everything else as raw bytes. There is no internal representation.

**Strengths**:
- New provider features work immediately (zero schema updates)
- The upstream receives byte-identical requests (modulo the model name)
- The audit record is a complete forensic record — both layers stored verbatim
- Response content can be inspected at the byte level (quirk detection, content anomaly flagging)

**Weaknesses**:
- Cannot route between different protocols — OpenAI-in stays OpenAI-out
- Cannot normalize behavior across providers
- Provider count is limited to "everything that speaks the same protocol"

---

## What each architecture can observe

This is the table that matters for agent workloads:

| Observable Event | Translation Router | Passthrough Router |
|---|---|---|
| HTTP status code distribution | ✅ | ✅ |
| Latency percentiles | ✅ | ✅ |
| Token usage per provider | ✅ | ✅ |
| Cost estimation | ✅ | ✅ |
| **New parameter silently dropped by upstream?** | ❌ (router dropped it first) | ✅ (was it in the client's request? then it's in the upstream's request) |
| **Response body contains soft-block markers?** | ❌ (private fields normalized away) | ✅ (raw bytes preserved) |
| **Model thinking leaked into output?** | ❌ (valid text — nothing structural to flag) | ✅ (byte-level pattern matching possible) |
| **Exact bytes the upstream received** | ❌ (re-serialized) | ✅ (only model name differs) |
| **Deterministic replay of any request** | ❌ (cannot reconstruct the translated version) | ✅ (original bytes preserved) |

The top four rows are table stakes — every gateway does them. The bottom five are only possible with passthrough, because they require **not normalizing**.

---

## Why passthrough routers are rare

If passthrough is so good for observability, why does almost everyone build translation routers?

**Reason 1: The market demands breadth.** "One API for all models" is a compelling product promise. It's what LiteLLM's 55k stars are built on. Passthrough can never make that promise.

**Reason 2: Passthrough is deceptively simple.** "Just forward the bytes" sounds like you could do it with nginx. And for basic proxying, you can. The complexity comes from everything around the passthrough — error classification, health state machines, response normalization for known quirks, session affinity for prompt cache preservation, and building an audit system that makes the raw bytes actually useful for debugging.

**Reason 3: The problems passthrough solves are less visible.** Nobody searches for "my LLM gateway silently drops new parameters" because they don't know it's happening. The pain is invisible until you have a tool that makes it visible — and then you can't go back.

---

## When to use which

**Use a translation router when**:
- You need to route between fundamentally different protocols
- Provider coverage breadth is your primary requirement
- You're building a platform that serves multiple users with different provider preferences
- Observability means "dashboards and alerts" — not "forensic audit trails"

**Use a passthrough router when**:
- All your providers speak the same protocol (OpenAI or Anthropic)
- You're running agents unattended and need to debug failures after the fact
- You want to know exactly what bytes your providers are sending and receiving
- You need deterministic replay for debugging

**The two aren't mutually exclusive.** You can run a translation gateway for broad provider access, and point a passthrough router at specific high-value agent workloads that need deep observability.

---

## Reference implementation

VMR is a passthrough router for OpenAI and Anthropic protocols. Single Go binary, MIT licensed:

```bash
brew install bigfatsea/tap/vmr
```

[github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)

*Disclosure: I built VMR. This post is about the architectural trade-off, not about which tool to use.*