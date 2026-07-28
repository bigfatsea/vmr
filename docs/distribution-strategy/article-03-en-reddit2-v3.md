# Everything you wanted to know about LLM failover for Claude Code (but were afraid to ask)

> **Version**: v3 (FAQ format)
> **Counterpart**: `article-03-en-reddit2.md`
> **Difference**: Q&A structure covering common objections and questions. Educational tone — for readers who aren't sure they need a router but have questions.
> **Target**: Reddit r/ClaudeAI
> **Length**: ~1000 words

---

People ask the same questions about LLM routing for Claude Code. Here are the answers, no fluff.

---

### Q: Doesn't Claude Code already handle retries?

Claude Code retries on transient network failures against the same endpoint. It doesn't:
- Switch to a different provider when one goes down
- Distinguish "this key is dead" from "this server is temporarily overloaded"
- Know that a 400 with "content policy" means "try a different provider" while a 400 with "invalid parameter" means "stop, this request is broken"
- Detect that a 200 response with empty content is actually a silent block

A router handles these distinctions. Claude Code is an agent — it shouldn't need to be a router too.

---

### Q: Why not just use claude-code-router?

CCR is a good tool. The difference is architecture, not quality:

- **CCR translates requests** through transformer plugins. This gives you broad provider support — it can route to providers with completely different APIs. But it means your request bytes are rewritten, and the audit log stores metadata (latency, tokens, status), not the actual request/response bodies.
- **VMR passes bytes through unchanged** except for the model name. This limits provider support to same-protocol endpoints, but means the audit log contains exact request/response bytes, your requests are identical to what a direct call would send, and VMR can detect content-level anomalies (soft blocks, thinking leaks) that structural normalization hides.

If you need broad provider support and a web dashboard, use CCR. If you need deep observability and forensic audit trails for unattended agent runs, use VMR.

---

### Q: Doesn't adding a router add latency?

VMR's load test results (150 req/s): p95 overhead under 10ms on 9 of 11 tested scenarios. The only real cost is optional image downscaling, which is off by default.

For comparison, the latency difference between providers is typically hundreds of milliseconds. 10ms of routing overhead versus 500ms more latency from a slower provider — the routing overhead disappears into the noise.

---

### Q: What happens when VMR itself crashes?

VMR is a single Go binary with zero runtime dependencies. If it crashes, your agent sees a connection error — the same as if the provider was down. The OS init system (launchd on macOS, systemd on Linux) restarts it automatically when configured via `vmr.sh service install`.

---

### Q: Can I use this with Cline / Cursor / OpenClaw / etc.?

Any client that speaks OpenAI or Anthropic protocol and lets you set a custom base URL. This includes Claude Code, Cline, Roo Code, Cursor, OpenClaw, Aider, and any custom agent you've built on top of the OpenAI/Anthropic SDKs.

---

### Q: Does this support streaming?

Yes. SSE events are forwarded as they arrive. The normalizer only buffers when it detects MiniMax's inline-thinking pathology — and resumes live streaming the moment the `<｜end▁of▁thinking｜>` tag closes. For all other providers and modes, streaming is true passthrough.

---

### Q: What's the security model?

VMR runs on localhost by default. No remote management API — the admin endpoint is loopback-only (`127.0.0.1`). API keys in config.yaml support `${ENV}` expansion so you never commit secrets. Audit files are written with 0600 permissions.

There's no telemetry, no phoning home, no cloud component. It's a local tool.

---

### Q: Can I contribute? Is this an active project?

MIT licensed, open to contributions. The project is active — 80+ commits in 3 weeks, ~30K lines of Go, ~15K lines of tests. See `CONTRIBUTING.md` (coming) or just open an issue.

---

### Q: What's the roadmap?

Right now: distribution. Getting pre-built binaries, Homebrew, and CI set up so people can actually install it.

After that (P1):
- Soft-block auto-failover (non-streaming path): when a soft block is detected, switch providers instead of just flagging it
- Context-length-exceeded error classification: distinguish "your request is too long for this endpoint" from "your request is malformed" — and fail over to an endpoint with a larger context window
- Per-virtual-model token budget hard cap: prevent a runaway agent from burning through your monthly quota overnight

---

### Q: Is this going to turn into a SaaS?

No. VMR is a personal tool — single binary, local-only, MIT licensed. No SaaS, no enterprise edition, no "open core" with premium features. This isn't a business — it's a tool I built for myself and shared because others might find it useful.

---

**Install**:

```bash
brew install bigfatsea/tap/vmr
# Or: https://github.com/bigfatsea/vmr/releases/latest
```

**Repo**: [github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)
