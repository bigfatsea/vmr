<!-- Ver 2026-08-01 14:08, by Gemini 3.6 Flash -->

# Reddit Reply: "Building AI agents gets weird once real users show up"

> **Original Reddit Post**:
> **Title**: Building AI agents gets weird once real users show up (Discussion)
> **Summary**: OP discusses the massive gap between 40-second polished demos vs real production users. Founders expect prompts to clean up dirty RAG data, expect agents to "know better" on permissions, and respond to 3-minute tool loops with "can we make the model smarter?". OP quotes: *"the actual job is becoming 20% building the agent and 80% explaining that probabilistic software doesn't become deterministic because the chat UI looks finished."*

---

### 💬 Reddit Comment Draft

Man, that line **"20% building the agent and 80% explaining that probabilistic software doesn't become deterministic because the chat UI looks finished"** belongs on a t-shirt for every AI engineer shipping to production right now.

You're definitely not around the wrong projects—you're just experiencing the painful gap between **demo-driven development** and **production state machine engineering**.

A few hard-earned takeaways from dealing with these exact production traps:

### 1. The "Make the Model Smarter" Trap
When an agent spends 3 minutes in a tool-call loop, 90% of non-technical stakeholders assume "the LLM got dumb." 
In reality, it's almost always a **state boundary failure**:
- **Tool Bloat**: Declaring 40+ tools adds 10k+ tokens of static JSON schema noise into every turn, polluting the model's attention.
- **Context Amnesia**: Mid-way context compaction/summarization silently dropped a key variable or file path, forcing the model into a retry loop.

### 2. Treat Agent Debugging Like a Flight Recorder (Not HTTP Logs)
Standard logs (`POST /v1/chat/completions 200 OK`) tell you nothing when an agent loses its mind at turn 14. 
You need to see **the raw byte stream evolution at each turn**:
- Did summarization eat a crucial constraint?
- Are declared tools actually being used, or is 80% of your schema budget being wasted?
- What was the exact **"divergence turn"** where a successful run split from a failed run?

If you want a lightweight tool to help with this, we built a local, open-source proxy called **[VMR (Virtual Model Router)](https://github.com/bigfatsea/vmr)** (single 12MB Go binary, zero DB setup). You just point your script’s `OPENAI_BASE_URL` to `http://127.0.0.1:18080/v1`:
- **`vmr story`**: Reconstructs multi-turn runs into a clean step-by-step narrative showing exact tool pairings and compaction entity loss.
- **`vmr story -compare`**: Lets you diff two agent runs side-by-side to pinpoint the exact turn where they diverged.

### 3. Hard Guardrails > Soft Prompts
For the founder expecting the agent to "know better" on permissions or 3 outdated refund policies: **prompts will never fix dirty data or bad authorization models.** Hardcode confirmation steps for state-changing actions (CRM writes, email sends) in code, not in the system prompt.

Hang in there. The shift from "magical demo" to "defensive engineering" is where real value is built.
