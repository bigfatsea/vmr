# Show HN: VMR — a flight recorder for AI agents (single Go binary, never rewrites your requests)

> **Target**: Hacker News (Show HN), Lobsters
> **Narrative**: Personal pain → what I built → why existing tools can't do this → architecture trade-off → try it
> **Length**: ~1500 words
> **Prerequisite**: D1 must be complete (pre-built binaries + `vmr version`)

---

My Claude Code agent ran for six hours last night. It completed most of its task — then silently derailed around 3 AM. Not because of a crash. Not because of a rate limit. Because one provider returned HTTP 200 with an empty body, and the agent treated it as a valid model response and kept going.

I only found out because I had a tool that records **both layers of every request** — what the client sent to the router, and what the router sent to the upstream provider — and flags anomalies in the response content.

That tool is VMR. I built it because existing LLM gateways can't do this. Not won't. **Can't** — their architecture prevents it.

---

## The problem with translation gateways

Most LLM gateways (LiteLLM, Portkey, claude-code-router, Bifrost) are **translation gateways**. They parse your request into an internal canonical format, then re-serialize it for whichever provider you're routing to.

This is the right architecture if your product promise is "one API to access 1000+ models" — you need an intermediate representation to bridge fundamentally different APIs.

But it comes with a cost: **you lose the ability to see what actually happened.** The request your client sent is not the bytes that hit the upstream. The response the upstream returned is not the bytes your client received. Both have been through a translation layer that may have: dropped unknown parameters, reordered fields, normalized whitespace, or silently swallowed provider-specific response quirks.

For an AI agent running unattended — where a single bad response can derail hours of work — this is not a minor implementation detail. It's the difference between being able to debug a failure and being unable to even see it happened.

---

## What VMR does differently

VMR never translates. It has no internal canonical format. When a request arrives at `POST /v1/chat/completions`, VMR looks at which upstream speaks the same protocol, rewrites exactly one field (the model name), and forwards every other byte untouched.

This means:

1. **New provider features work on day one.** When OpenAI ships a new parameter tomorrow, VMR passes it through with zero changes — because it never had an opinion about which fields should exist.

2. **The audit log contains the real bytes.** Every request records both layers: client↔VMR and VMR↔upstream. Including every failover attempt and every normalization applied. This is not "we logged the metadata" — this is "we logged exactly what the upstream received and returned."

3. **You can replay any request.** `vmr replay -ts "2026-07-24T03:14:22+08:00"` resends that exact request to see what the upstream returns now. Useful both for debugging and for verifying that a fix actually fixed things.

4. **Agent-shaped reporting.** `vmr report` groups requests into sessions → tasks → turns. It marks each turn's delta with a 🆕 prefix. It tells you which declared tools are never actually called. This is not "an HTTP log viewer with an agent label slapped on" — the grouping logic understands conversation structure.

5. **Provider quirk fixes.** VMR detects and strips MiniMax's ` thinking... response` thinking-leakage tags, and flags soft-blocking responses (HTTP 200 with `"output_sensitive": true` markers) in the audit trail. These are Chinese-provider-specific behaviors that translation gateways would never see because they're normalizing the response.

---

## The five things translation gateways structurally cannot do

This is not "VMR does it better." This is "translation gateways cannot do these things because they require not translating":

| Capability | Why it requires byte-faithful passthrough |
|---|---|
| **Two-layer complete byte recording** | If you translate, the bytes you record are the translated version — not what the upstream actually received |
| **Agent semantic grouping** (sessions → tasks → turns) | Requires understanding conversation structure embedded in message bytes — which translation may alter |
| **Single-request replay** | You can only replay what you actually sent. If you translated it first, that's what you'd replay |
| **Session affinity** (preserve prompt cache) | If you rewrite requests, the upstream's prompt cache may not recognize the conversation |
| **Provider quirk detection** (thinking leakage, soft-blocking) | These require byte-level awareness of response content. Translation layers normalize this away |

Failover, request logging, and cost tracking are table stakes now — multiple tools have them. But none of them can show you the raw bytes the upstream sent back at 3 AM, or replay that exact moment.

---

## The philosophy: a flight recorder, not a control panel

VMR is not designed to manage your agents. It's designed to **record what they do** so that when something goes wrong, you can find out what happened and reproduce it.

- Single Go binary, zero dependencies, zero database
- MIT licensed, no SaaS, no enterprise edition — this is a personal tool
- Config is one YAML file, hot-reloaded in seconds
- Load-tested at 150 req/s: sub-10ms p95 routing overhead on 9/11 scenarios
- ~15K lines of production code, ~15K lines of tests, 167KB of design documentation

---

## Try it

```bash
# macOS
brew install bigfatsea/tap/vmr

# Or download the binary (macOS / Linux × amd64 / arm64)
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

## Screenshot recommendations

1. **Hero**: `vmr report` session grouping view — the tree structure with sessions/tasks/turns and 🆕 markers
2. **Middle**: A single audit record expanded — showing the two-layer body comparison and `norm` field
3. **Footer**: `vmr start` summary — showing the routing table and health status of all endpoints