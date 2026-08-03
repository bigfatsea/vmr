<!-- Ver 2026-07-31 21:40, by Gemini 3.6 Flash -->

# 中转商响应异常？我用 VMR 的两层审计与 Replay 抓到了铁证

> **TL;DR**：在使用第三方 API 中转商或云服务商时，你是否遇到过这些扯皮场景：**怀疑中转商偷偷用便宜模型替换了昂贵模型（偷换模型）、或者中转商在网关层丢弃了某个关键 JSON 字段导致 Agent 报错？** 向客服提起工单时，对方往往一句“我们系统日志正常”就把你打发了。本文将分享如何利用 VMR（Virtual Model Router）的**客户端/上游两层完整字节审计**与 **`vmr replay` 物理重发功能**，抓取不可篡改的底层证据并成功维权。

---

## 0. 痛点：API 中转生态里的“罗生门”

在 AI 开发圈，使用第三方中转 API 或聚合服务是很多个人的选择。

但由于中转商质量参差不齐，开发者经常陷入无法举证的“罗生门”：
1. **“掺假/偷换模型”**：你付费请求的是昂贵的 `Claude-3.5-Sonnet`，但模型返回的代码质量极差，甚至偶尔吐出某国产小模型的专有默认拒绝语。你怀疑被降级成了便宜模型，但苦于没有请求证据；
2. **“中转网关字段打扰”**：中转商在中间做转译时，默默剥离掉了 OpenAI 协议里的 `tools` 或 `reasoning_content` 字段，导致上层 Agent 抛出空指针，客服却坚持说是你的客户端发错了请求；
3. **“偶发性网络丢包/断流”**：流式 SSE 输出走到一半断了，中转商却按全量生成的 Token 扣了你的余额。

传统的 HTTP 抓包工具（如 Charles 或 Wireshark）只能抓取本地发出的数据，配置繁琐，且无法离线持久化；而常规的 LLM Gateway 只保留了解析后的元数据（如 `model="gpt-4"`），**无法证明上游服务器实际发回了什么字节**。

---

## 1. 底牌：VMR 的两层完整字节审计 (Two-Layer Audit)

为了从根本上解决信任与取证问题，VMR（Virtual Model Router）从第一天起就确立了 **Byte-faithful（字节保真）** 架构。

在 VMR 运行过程中，它会在本地 `~/.vmr/logs/` 目录下生成追加型的 `.jsonl` 审计日志。对于每一次 API 调用，VMR 都会无死角地记录**两层物理字节**：

```
[客户端 Agent]
      │
      ▼  (Layer 1: client.request.body & client.response.body)
┌───────────┐
│ VMR 节点  │  ───► 本地 JSONL 审计落盘 (不可篡改的物理快照)
└───────────┘
      │
      ▼  (Layer 2: attempts[i].upstream_request & upstream_response)
[第三方中转商 / 上游 API]
```

### 两层审计记录了什么？
- **Layer 1 (客户端侧)**：你的 Agent 实际上向 VMR 发送了什么原始字节、VMR 最终给 Agent 返回了什么字节；
- **Layer 2 (上游 Attempt 侧)**：VMR 加上供应商 Key、替换了 `model` 名字后，发给中转商的**真实 HTTP 请求体**，以及中转商服务器返回的**原始 HTTP 响应体（包含 Headers 与原始 SSE 字符串）**。

两层记录天然构成了因果链条：你可以轻松证明“我的客户端发出了字段 X，但中转商发回的原始 HTTP 字节中缺失了字段 X”。

---

## 2. 物理级重现：用 `vmr replay` 重放抓现行

有了字节记录，如果中转商客服辩称“那是你当时网络不好，现在我们系统很稳定”，你怎么证明当时确实是他们出了问题？

VMR 提供了物理级的单条请求重发工具：**`vmr replay`**。

### 步骤 1：从审计日志中定位问题行
首先使用 `vmr report` 或在日志文件中搜索特定时刻的异常响应，找到对应的日志行号（如第 `1452` 行）：

```bash
vmr replay -line 1452 --dry-run
```

在 `--dry-run` 模式下，VMR 不会真的发出请求，而是将当时发给中转商的原始 Header、URL 和请求 Body 完全打印出来：

```markdown
================================================================================
          VMR Replay 物理重现预览 (Line 1452)
================================================================================
[Target Endpoint]: https://api.middleman.com/v1/chat/completions
[Resolved Model]: deepseek-v3
[Original Timestamp]: 2026-07-31 14:23:05 CST

[Reconstructed HTTP Headers]:
  Authorization: Bearer sk-mid-xxxxxx
  Content-Type: application/json
  X-VMR-Client-Tag: openclaw_prod

[Reconstructed Body Splice]:
  {"model":"deepseek-chat","messages":[{"role":"user","content":"..."}],"stream":true}
```

VMR 使用与生产路由**完全相同的请求构建代码**来复原环境，确保请求体中的模型名、格式、空格与原始请求 100% 一致。

---

### 步骤 2：原样重发并录制新响应

确认预览无误后，执行真正重发，并加上 `--record` 选项保存对比结果：

```bash
vmr replay -line 1452 --record
```

VMR 会重新向中转商拨号，将发给上游的原始字节重新推送过去。

如果中转商真的存在偶发性转译 Bug，你可以在控制台上看到新旧响应的直接对比：

```markdown
[原始历史响应 (Line 1452)]:
  HTTP 200 OK
  Body: data: {"id":"chat-1","choices":[{"delta":{"content":"Thinking Process:\n1..."}}]}
  ⚠️ (异常: 中转商未解析思维链，将草稿作为正文返回)

[Replay 现场重发响应]:
  HTTP 200 OK
  Body: data: {"id":"chat-2","choices":[{"delta":{"content":"Hello! How can I help?"}}]}
  ✅ (现状: 中转商修复了 Bug，证实 14:23 时确实是中转网关故障)
```

---

## 3. 维权实战：如何拿 VMR 证据向中转商工单举证？

有了 VMR 提供的完整证据链，向客服或中转商提起工单时，你的话术和证据将无懈可击：

### 📁 举证材料清单：
1. **原始 JSONL 审计片段**：提供 Layer 2 的 `attempts[0].response.body`，展示中转商返回原始字节中的错误代码；
2. **`vmr replay` 执行日志**：展示通过物理重发复现的故障现场；
3. **HTTP Header 中的 `X-VMR-Failover`**：证明 VMR 当时是因为中转商抛出异常才被迫切换备用节点的。

### 💬 维权工单模板：
> **关于 07-31 14:23 请求出现格式剥离异常的工单说明**
>
> 贵方客服您好：
> 我方于 2026-07-31 14:23:05 调用的 `chat/completions` 接口（ Request ID: `req-98127`）出现响应格式异常。
>
> 我方已通过本地 VMR 透明代理保留了**物理级两层字节审计**：
> - 我方发送给贵方网关的原始 Body（见附件 `req_client.json`）中，明确包含了 `"tools"` 参数；
> - 贵方网关返回给我的原始 HTTP 响应（见附件 `req_upstream_raw.json`）中，直接抛出了未处理的中间件 500 报错。
>
> 我方已使用 `vmr replay` 命令复原了当时的物理请求，并在本地成功复现了该故障。请贵方核对网关侧 14:23 的日志并退还异常扣除的余额。

在如此详尽的底层字节证据面前，绝大多数中转商都会迅速承认网关故障并退还额度。

---

## 4. 总结

在复杂的 API 中转和云服务生态里，**口说无凭，字节才是硬道理。**

VMR 的两层字节审计与 Replay 功能，就像给你的每一个 API 请求都盖上了带时间戳的公章。它不仅能帮助你在本地调试 Agent，更是你在面对不稳定中转商时，最强有力的维权武器。

- **GitHub 开源地址**：[github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)
- **macOS 安装**：`brew install bigfatsea/tap/vmr`
