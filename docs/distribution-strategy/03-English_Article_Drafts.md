# VMR Article Drafts — English

> Drafts for English-language channels. Replace `[actual data]` placeholders before publishing.
>
> **Prerequisite**: D1 (pre-built binaries + `vmr version`) must be complete. Installation instructions must use `brew install` or direct binary download — never `go build`.

---

## Article 1 (Show HN): VMR — a flight recorder for AI agents that never rewrites your requests

**Target**: Hacker News (Show HN), Lobsters
**Narrative**: Personal pain → what I built → why existing tools can't do this → architecture trade-off → try it
**Length**: ~1500 words

---

### Body

My Claude Code agent ran for six hours last night. It completed most of its task — then silently derailed around 3 AM. Not because of a crash. Not because of a rate limit. Because one provider returned HTTP 200 with an empty body, and the agent treated it as a valid model response and kept going.

I only found out the next morning because I had a tool that records **both layers of every request** — what the client sent to the router, and what the router sent to the upstream provider — and flags anomalies in the response content.

That tool is VMR. I built it because existing LLM gateways can't do this. Not won't. Can't — their architecture prevents it.

---

#### The problem with translation-based gateways

Most LLM gateways (LiteLLM, Portkey, claude-code-router, Bifrost) are **translation gateways**. They parse your request into an internal canonical format, then re-serialize it for whichever provider you're routing to.

This is the right architecture if your product promise is "one API to access 1000+ models" — you need an intermediate representation to bridge fundamentally different APIs.

But it comes with a cost: **you lose the ability to see what actually happened.** The request your client sent is not the bytes that hit the upstream. The response the upstream returned is not the bytes your client received. Both have been through a translation layer that may have: dropped unknown parameters, reordered fields, normalized whitespace, or silently swallowed provider-specific response quirks.

For an AI agent running unattended — where a single bad response can derail hours of work — this is not a minor implementation detail. It's the difference between being able to debug a failure and being unable to even see it happened.

---

#### What VMR does differently

VMR never translates. It has no internal canonical format. When a request arrives at `POST /v1/chat/completions`, VMR looks at which upstream speaks the same protocol, rewrites exactly one field (the model name), and forwards every other byte untouched.

This means:

1. **New provider features work on day one.** When OpenAI ships a new parameter tomorrow, VMR passes it through with zero changes — because it never had an opinion about which fields should exist.

2. **The audit log contains the real bytes.** Every request records both layers: client↔VMR and VMR↔upstream. Including every failover attempt and every normalization applied. This is not "we logged the metadata" — this is "we logged exactly what the upstream received and returned."

3. **You can replay any request.** `vmr replay -ts "2026-07-24T03:14:22+08:00"` resends that exact request to see what the upstream returns now. Useful both for debugging and for verifying that a fix actually fixed things.

4. **Agent-shaped reporting.** `vmr report` groups requests into sessions → tasks → turns. It marks each turn's delta with a 🆕 prefix. It tells you which declared tools are never actually called. This is not "an HTTP log viewer with an agent label slapped on" — the grouping logic understands conversation structure.

---

#### The five things VMR can do that no translation gateway can

This is not "VMR does it better." This is "translation gateways structurally cannot do these things because they require not translating":

| Capability | Why it requires byte-faithful passthrough |
|---|---|
| **Two-layer complete byte recording** (client↔VMR, VMR↔upstream) | If you translate, the bytes you record are the translated version — not what the upstream actually received |
| **Agent semantic grouping** (sessions → tasks → turns) | Requires understanding the conversation structure embedded in the message bytes — which translation may alter |
| **Single-request replay** | You can only replay what you actually sent — if you translated it first, that's what you'd replay |
| **Session affinity** (prompt cache preservation) | If you rewrite requests, the upstream's prompt cache may not recognize the conversation |
| **Provider quirk fixes** (thinking-leak stripping, soft-block detection) | These require byte-level awareness of response content — translation layers filter this out |

Failover, request logging, and cost tracking are table stakes now — multiple tools have them. But none of them can show you the raw bytes the upstream sent back at 3 AM, or replay that exact moment.

---

#### The philosophy: a flight recorder, not a control panel

VMR is not designed to manage your agents. It's designed to **record what they do** so that when something goes wrong, you can find out what happened and reproduce it.

- Single Go binary, zero dependencies, zero database
- MIT licensed, no SaaS, no enterprise edition planned
- Config is one YAML file, hot-reloaded in seconds
- Load-tested at 150 req/s: sub-10ms p95 routing overhead on 9/11 scenarios

---

#### Try it

```bash
# macOS
brew install bigfatsea/tap/vmr

# Or download the binary directly (macOS / Linux × amd64 / arm64)
# https://github.com/bigfatsea/vmr/releases/latest

# Start from the example config
curl -o config.yaml https://raw.githubusercontent.com/bigfatsea/vmr/main/config.example.yaml
# Edit config.yaml, fill in your API keys (${ENV} expansion supported)

vmr check -c config.yaml   # validate and print the routing table
vmr start -c config.yaml   # foreground, prints a full config summary

# Point your agent at vmr
export ANTHROPIC_BASE_URL=http://127.0.0.1:8800
```

Run your agent as usual. Wait a day. Run `vmr report`. You will learn things about your agent's behavior that no other tool can tell you.

**Repo**: [github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)

---

### Screenshot recommendations

1. **Hero**: `vmr report` session grouping view — the tree structure with sessions/tasks/turns and 🆕 markers
2. **Middle**: An audit record with two-layer body comparison — annotated with what each layer shows
3. **Footer**: `vmr start` summary — showing the routing table and health status

---

## Article 2 (Reddit r/LocalLLaMA): Why translation gateways are losing your agent's bytes — and what to use instead

**Target**: Reddit r/LocalLLaMA, r/ClaudeAI
**Narrative**: Technical deep-dive → concrete example → what to use
**Length**: ~1000 words

---

### Body

Most people running local models or routing between providers use a translation gateway — LiteLLM, claude-code-router, or one of the many OpenAI-compatible proxies. These tools have one job: make different APIs look the same to your client.

They do that job well. But there's a cost that nobody talks about, and for agent workloads it matters more than you think.

**Translation gateways normalize your bytes.** Here's what that means in practice:

1. Your client sends `{"model":"claude","reasoning_effort":"high","messages":[...]}` 
2. The gateway parses this into its internal schema. `reasoning_effort` hasn't been added to that schema yet.
3. The gateway re-serializes the request for the target provider. `reasoning_effort` is silently dropped.
4. The provider never sees it. Your agent gets a response with default reasoning effort. Nobody knows.

This isn't hypothetical. It happened with `reasoning_effort`, it happened with `cache_control` variants, it happens every time OpenAI or Anthropic adds a new parameter. The gateway's schema is always playing catch-up.

**But that's not even the worst part.** The response path has the same problem in reverse:

- A provider returns a 200 with an empty body (Chinese providers do this as "soft blocking")
- The gateway normalizes the response. An empty body is valid JSON — nothing to flag.
- Your agent receives an empty response, treats it as a valid model output, and continues.
- Hours of work silently derailed.

A translation gateway can't catch this because **checking the semantic content of a response body is not in its design scope.** Its job is structural translation — JSON → internal format → JSON. What the text in `content` actually says is not its problem.

---

#### The alternative: a byte-faithful passthrough router

VMR (Virtual Model Router) takes the opposite approach. It never translates. It checks which upstream speaks the same protocol as your client, rewrites the model name, and forwards everything else as-is.

This sounds simpler (and it is — one Go binary, zero dependencies), but it means VMR can do things translation gateways structurally cannot:

- **Record both layers of every request** — what your client sent AND what the upstream received. Since they differ only in the model name, the audit log is a complete forensic record.
- **Detect soft-blocking** — when a provider returns 2xx but the content is empty or replaced, VMR flags it in the audit log. Translation gateways normalize this away.
- **Replay any request** — `vmr replay` resends the exact bytes from any historical request. You can reproduce failures deterministically.
- **Strip thinking leakage** — some models (MiniMax) leak their internal reasoning chain into the output. VMR strips it at the gateway before your agent sees it.

None of these are "nice to have" for agent workloads. They're the difference between "my agent failed and I don't know why" and "my agent failed, here's exactly what the upstream sent at 3:14 AM, and I can reproduce it."

---

#### When to use what

| Use a translation gateway (LiteLLM, CCR) when... | Use VMR when... |
|---|---|
| You need to access providers with fundamentally different native protocols | All your providers speak OpenAI or Anthropic protocol |
| You want a web dashboard and team management | You want a single binary, no database, no UI |
| You're optimizing for maximum provider coverage | You're optimizing for debuggability and correctness |
| You're running a platform for multiple users | You're running your own agents |

---

#### Installation

Single binary, MIT licensed:

```bash
brew install bigfatsea/tap/vmr
# Or: https://github.com/bigfatsea/vmr/releases/latest
```

**Repo**: [github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)

---

## Article 3 (Reddit r/ClaudeAI): What happens when your Claude Code session hits a rate limit at 3 AM

**Target**: Reddit r/ClaudeAI
**Narrative**: Common pain → what happens without a router → what happens with VMR → how to set it up
**Length**: ~800 words (shorter, more action-oriented)

---

### Body

If you use Claude Code heavily, you've been here:

You kick off a long task, go to sleep, wake up to find it died at 2 AM because of a rate limit or a provider outage. Hours of context and progress, gone.

Claude Code itself has retry logic, but it's designed for transient network issues against the same endpoint. It doesn't know:
- When to switch to a different provider
- When to switch to a different API key
- That a "400 context length exceeded" means "try the endpoint with a bigger context window"
- That a "200 with empty content" (Chinese providers do this) means "something is wrong"

VMR sits between Claude Code and your providers. It handles these distinctions automatically:

- **429 rate limit** → exponential backoff, respects `Retry-After`
- **503 provider down** → exponential backoff, then background probe to check recovery
- **400 "context length exceeded"** → switch to an endpoint with higher `max_context_tokens`, don't punish the original endpoint
- **400 "content policy"** → switch providers, don't punish the healthy endpoint
- **2xx but content empty** → detected and flagged in audit (auto-failover coming)

The key design choice: **different errors have different meanings, and VMR treats them differently.** A rate limit is not the same as a dead key. A content policy rejection is not the same as a server crash.

---

#### Setup

```bash
brew install bigfatsea/tap/vmr
```

Configure your providers in one YAML file, point Claude Code at VMR:

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8800
```

Then run `vmr report` after a day or two. You'll see every attempt, every failover, every near-miss that you never noticed because VMR handled it silently.

MIT licensed. Single binary, zero dependencies.

**Repo**: [github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)
