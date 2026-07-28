# Show HN: I built an LLM router that refuses to translate your requests

> **Version**: v2 (concept-first, opinionated)
> **Counterpart**: `article-01-en-showhn.md`
> **Difference**: Opens with the controversial design choice ("refuses to translate") rather than a personal story. Provocative framing for HN readers who appreciate strong technical opinions.
> **Target**: Hacker News (Show HN)
> **Length**: ~1200 words

---

Every LLM gateway on the market today does the same thing: it translates your request into an internal format, then translates it back to whichever provider you're routing to.

VMR does the opposite. It refuses to translate anything. Here's why that matters more than you'd think.

---

## The argument

Translation gateways exist to solve a real problem: every LLM provider has a slightly different API. OpenAI's chat completions aren't Anthropic's messages aren't Google's generateContent. If your product promise is "one API to access 1000+ models," you need an intermediate representation.

But intermediate representations come with a cost that nobody talks about: **they make you blind.**

When your request passes through a translation layer, the bytes your client sent are not the bytes the upstream received. The response the upstream returned is not the bytes your client got. Both have been normalized — fields reordered, unknown parameters dropped, whitespace reformatted. For a chatbot, this doesn't matter. For an AI agent running unattended at 3 AM, where a single bad response can derail hours of work — it's the difference between being able to debug a failure and not even knowing it happened.

---

## What "refusing to translate" actually means

VMR's entire request pipeline does two things:
1. Rewrite the `"model"` field (your virtual name → the real upstream name)
2. Forward every other byte untouched

That's it. No internal schema. No normalization. No "canonical request format."

This sounds like a limitation — and it is. VMR can only route within the same protocol family (OpenAI→OpenAI, Anthropic→Anthropic). It will never support 1000+ models.

But this "limitation" is what enables everything VMR can do that translation gateways can't:

**1. New provider features work on day zero.** When OpenAI adds a `reasoning_effort` parameter tomorrow, VMR passes it through untouched. Translation gateways silently drop it until someone updates the schema. This already happened — multiple times.

**2. The audit log is a forensic record, not a summary.** Translation gateways log metadata: which model, how many tokens, how long it took. VMR logs both layers of complete bytes: what the client sent AND what the upstream received. They differ only in the model name — everything else is identical. When something goes wrong, you can see exactly what happened.

**3. Provider quirks become visible.** Chinese providers sometimes return HTTP 200 with empty content and a hidden `"output_sensitive": true` flag — a "soft block." Translation gateways normalize this away (the flag isn't in their schema). VMR sees it because it's looking at the raw bytes.

**4. You can replay any request.** `vmr replay -ts "..."` resends the exact bytes from any historical request. You can't replay what you normalized away.

---

## The trade-off, stated plainly

Translation gateways optimize for **breadth**: access the most providers with the least friction. VMR optimizes for **depth**: know exactly what happened to every request.

If you're running a platform serving multiple users with different provider needs, you want breadth. Get LiteLLM — it's the right tool for that job.

If you're running your own agents and the question you actually need answered is "what happened at 3 AM and can I reproduce it," you want depth. That's what VMR is for.

---

## What's implemented

- Dual-protocol ingress (OpenAI `/v1/chat/completions` + Anthropic `/v1/messages`), each routed within its own protocol family — never cross-translated
- Error-class-aware failover: rate limits, auth failures, content policy blocks, and transient errors each get different cooldown and retry behavior
- Two-layer audit log (client↔VMR and VMR↔upstream), one JSONL line per request, with every failover attempt and every normalization applied
- `vmr report`: agent-shaped analytics — requests grouped into sessions → tasks → turns, per-turn delta markers, tool-usage analysis, per-endpoint cost-effectiveness, time-of-day availability
- `vmr replay`: resend any historical request from the audit log
- Provider quirk fixes: MiniMax thinking-leak stripping, soft-block detection
- Single Go binary, zero runtime dependencies. Config is one YAML file, hot-reloaded.

Load-tested at 150 req/s: sub-10ms p95 routing overhead on 9/11 scenarios. ~15K lines of production code, ~15K lines of tests.

---

## Try it

```bash
brew install bigfatsea/tap/vmr
# Or: https://github.com/bigfatsea/vmr/releases/latest
```

Run your agent through VMR for a day, then `vmr report`. You'll learn things about your agent's behavior no other tool can tell you.

MIT licensed. Personal tool — no SaaS, no enterprise edition planned.

**Repo**: [github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)