# What happens when your Claude Code session hits a rate limit at 3 AM

> **Target**: Reddit r/ClaudeAI
> **Narrative**: Common pain → what happens without a router → what happens with VMR → how to set it up
> **Length**: ~800 words (shorter, action-oriented)
> **Prerequisite**: D1 complete

---

If you use Claude Code heavily, you've been here: you kick off a long task, go to sleep, wake up to find it died at 2 AM because of a rate limit or a provider outage. Hours of context and progress, gone.

Claude Code itself has retry logic, but it's designed for transient network issues against the same endpoint. It doesn't know:

- When to switch to a different provider
- When to switch to a different API key
- That a 402 "insufficient balance" error means "stop hitting this endpoint entirely"
- That a 400 "content policy" rejection means "the endpoint is fine, just switch to a different one for this request"
- That a 2xx with empty content from a Chinese provider means "this was silently blocked"

VMR sits between Claude Code and your providers. It handles these distinctions automatically.

---

## Not all errors are the same

This is the core insight behind VMR's error handling. Most retry logic treats all failures as "try again later." VMR distinguishes six error classes:

| Error Class | Example | VMR's Action |
|---|---|---|
| **Rate Limit** | 429 | Honor `Retry-After`, switch to next endpoint, exponential backoff |
| **Auth** | 401 | Long cooldown, switch — this key is dead |
| **Endpoint Issue** | 402 (insufficient balance), 404 (unknown model) | Long cooldown, switch — this endpoint is unusable |
| **Transient** | 5xx, connection timeout | Short cooldown (1s→2s→4s…max 30s), switch |
| **Content Policy** | 400/403 with moderation markers | Switch **without** punishing the endpoint — it's healthy, just doesn't like this specific request |
| **Client Error** | 400 (bad request, not content-related) | Return to client immediately — every endpoint would reject this |

The distinction between **Content Policy** and **Client Error** is the one that matters most. A content-policy rejection means "try another provider, this one flagged your request." A client error means "your request is malformed, no provider will accept it." Treating them the same way is how you get a healthy endpoint unfairly cooled down.

---

## Recovery without the risk

When an endpoint cools down, how do you know it's recovered?

The naive approach: let the next real request test it. Problem: that request might be huge (a long agent conversation), and if the endpoint is still down, it wastes time. Worse, under concurrent load, every request waiting for this endpoint queues behind that one slow probe.

VMR's default (active probe mode): when cooldown expires, VMR sends a tiny echo-nonce request (`{"messages":[{"role":"user","content":"ping"}]}`) on a dedicated background goroutine. Real traffic never waits on or gets diverted by someone else's slow recovery check. If the probe succeeds, the endpoint is marked healthy. If it fails, cooldown restarts.

---

## Session affinity: don't throw away prompt cache

One subtlety: when VMR fails over to a new endpoint, the upstream's prompt cache is cold. For multi-turn agent conversations, this means the next request has to re-send the entire context — costing more tokens.

VMR's Sticky Model keeps multi-turn conversations pinned to the same endpoint as long as it stays healthy. "Smarter routing" doesn't quietly cost you more by switching providers mid-conversation.

---

## Setup in 3 steps

```bash
# 1. Install
brew install bigfatsea/tap/vmr
# Or download binary: https://github.com/bigfatsea/vmr/releases/latest

# 2. Configure (edit config.yaml with your API keys)
curl -o config.yaml https://raw.githubusercontent.com/bigfatsea/vmr/main/config.example.yaml

# 3. Run
vmr start -c config.yaml
export ANTHROPIC_BASE_URL=http://127.0.0.1:8800
```

Then run your Claude Code agent as usual. After a day or two, run `vmr report` — you'll see every attempt, every failover, every near-miss that you never noticed because VMR handled it silently.

Single Go binary, MIT licensed, zero dependencies. Personal tool, no SaaS, no enterprise edition.

**Repo**: [github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)