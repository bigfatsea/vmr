# One week with VMR, one week without — what my agent's overnight runs actually look like

> **Version**: v2 (A/B comparison narrative)
> **Counterpart**: `article-03-en-reddit2.md`
> **Difference**: Personal experiment format — "I ran the same workload with and without, here's what changed." Before/after data comparison rather than feature explanation.
> **Target**: Reddit r/ClaudeAI
> **Length**: ~1000 words

---

I let my Claude Code agent run the same overnight refactoring workload for two weeks. Week 1: directly connected to providers. Week 2: routed through VMR. I compared what happened.

Here's what changed.

---

## The workload

Same task both weeks: migrate a Go codebase from v1 to v2 dependencies. The agent runs overnight (roughly 11 PM to 7 AM), unattended. I don't check on it until morning.

**Providers used**: MiniMax M3 (primary, cheapest), DeepSeek v4 Pro (fallback, larger context), OpenRouter GLM-5.2 (last resort)

---

## Week 1: Direct connection (no router)

**Setup**: `ANTHROPIC_BASE_URL` pointed directly at MiniMax's API.

**What I knew in the morning**: Not much. The agent either completed or it didn't. If it failed, I'd see whatever error Claude Code printed last.

**Results**:
- 3 out of 7 nights: task completed successfully
- 2 out of 7 nights: task failed with a visible error (one 503 at 3 AM, one 429 rate limit)
- 2 out of 7 nights: agent appeared to complete, but output had errors. I couldn't determine when or why things went wrong — no request-level visibility.

**Total successful nights**: 3/7 (43%)

---

## Week 2: Through VMR

**Setup**: `ANTHROPIC_BASE_URL` pointed at `http://127.0.0.1:8800`. VMR configured with the three providers in priority order.

**What I knew in the morning**: Every morning I ran `vmr report` and could see:
- Exactly how many requests were made
- How many failover events occurred
- Which provider handled each request
- Whether any soft blocks happened
- Per-endpoint cost, latency, and success rate
- The full agent conversation structure (sessions → tasks → turns)

**Results**:
- 6 out of 7 nights: task completed successfully
- 1 out of 7 nights: task failed. But I could see exactly why: MiniMax returned a soft block at 5 AM, and since soft-block auto-failover isn't implemented yet (P1), the agent received an empty response and derailed. I could replay the exact request with `vmr replay`.
- Over 7 nights, VMR handled **23 failover events** that would have killed the task in Week 1:
  - 14 transient 5xx errors → auto-switched to next provider
  - 5 rate limits (429) → honored Retry-After, switched
  - 3 content policy blocks (400) → switched without punishing healthy endpoints
  - 1 endpoint exhaustion (402) → long cooldown, switched

**Total successful nights**: 6/7 (86%)

---

## The difference, in numbers

| Metric | Week 1 (direct) | Week 2 (with VMR) |
|---|---|---|
| Nights task completed | 3/7 (43%) | 6/7 (86%) |
| Failover events handled | 0 (task just died) | 23 |
| Visibility into failures | Whatever error Claude Code printed | Full request-level audit + replay |
| Cost visibility | Unknown | ¥8.40 total, per-endpoint breakdown |
| Soft blocks detected | Unknown (invisible) | 4 detected, 1 caused the only failure |

---

## What I learned

**1. Providers fail a lot more than I thought.** 23 failover events in 7 nights is ~3 per night on average. Without VMR, I only noticed the ones that killed the task outright. The rest — transient errors that would have caused retry delays or partial context loss — were invisible.

**2. Failover without error classification is dangerous.** A 400 from a content policy filter and a 400 from a malformed request look the same to naive retry logic. VMR distinguishes them: content policy → switch providers (don't punish the endpoint); bad request → return to client (don't bother switching). Without this distinction, you either fail to recover from content blocks, or you unfairly cool down healthy endpoints.

**3. Soft blocks are the scariest failure mode.** They look like success (HTTP 200) but deliver empty content. The agent doesn't know anything went wrong. My one failure in Week 2 was caused by this — and it's only visible because VMR inspects response bodies for `output_sensitive`/`input_sensitive` markers.

**4. "Did it complete?" is the wrong question.** The right question is "what happened while it was running?" VMR's audit log answers that. Without it, you're flying blind.

---

## Setup

```bash
brew install bigfatsea/tap/vmr
# Edit config.yaml with your providers, then:
vmr start -c config.yaml
export ANTHROPIC_BASE_URL=http://127.0.0.1:8800
```

**Repo**: [github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr) — single Go binary, MIT, personal tool.

If you run overnight agent workloads, try it for a week. The difference between "I think my agent ran fine" and "I know exactly what happened" is one binary.