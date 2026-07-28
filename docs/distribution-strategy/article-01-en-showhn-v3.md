# Show HN: VMR — record and replay every LLM API call your agent makes

> **Version**: v3 (ultra-concise, punchy)
> **Counterpart**: `article-01-en-showhn.md`
> **Difference**: Minimalist Show HN — one-line value prop, three concrete capabilities, install command. For HN readers who scan 50 Show HNs and only click on the ones they can understand in 10 seconds.
> **Target**: Hacker News (Show HN)
> **Length**: ~500 words

---

VMR is a single Go binary that sits between your AI agent and your LLM providers. It records both layers of every request — what your client sent, and what the upstream received — so you can debug failures you wouldn't otherwise know happened, and replay any request to reproduce issues.

**The key design choice**: VMR never translates your requests. It only rewrites the model name. Every other byte passes through untouched. This means (a) new provider features work immediately, (b) the audit log shows exactly what happened, and (c) you can replay any request deterministically.

---

## Three things it does that no translation gateway can

**1. Detects "soft blocks" — when a provider returns HTTP 200 with empty content**

Chinese providers sometimes flag a response with `"output_sensitive": true` and return an empty body — without setting an error status code. Your agent treats it as a valid response and keeps going. VMR flags these in the audit trail. Translation gateways normalize these private fields away and never see them.

**2. Groups requests into agent sessions → tasks → turns**

`vmr report` doesn't give you a flat list of HTTP requests. It reconstructs the conversation structure: sessions, tasks within sessions, turns within tasks. Each turn's delta is marked with a 🆕 prefix. It tells you which declared tools were never actually called.

**3. Lets you replay any historical request**

`vmr replay -ts "2026-07-24T03:14:22+08:00"` resends the exact bytes from any audit record. Useful when you need to know "what does the upstream return NOW for that same request?"

---

## Also: failover that distinguishes error types

Not all failures are the same. VMR classifies upstream errors into six classes:
- **Rate Limit (429)** → honor `Retry-After`, exponential backoff
- **Auth (401)** → long cooldown, switch keys
- **Content Policy (400/403 + moderation markers)** → switch providers WITHOUT punishing the healthy endpoint
- **Transient (5xx)** → short cooldown, exponential backoff
- **Endpoint (402/404)** → long cooldown, this key/model is dead
- **Client Error (400, truly bad request)** → return to client, don't bother switching

Recovery probes run on a dedicated background goroutine — real traffic never waits on or gets blocked by someone else's slow recovery check.

---

## Quick start

```bash
brew install bigfatsea/tap/vmr
curl -o config.yaml https://raw.githubusercontent.com/bigfatsea/vmr/main/config.example.yaml
# edit config.yaml with your API keys
vmr start -c config.yaml
export ANTHROPIC_BASE_URL=http://127.0.0.1:8800
```

Run your agent for a day, then `vmr report`. Open `reports/vmr-report.md`.

MIT. Single binary, zero dependencies. Personal tool — no SaaS, no enterprise.

**Repo**: [github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)
