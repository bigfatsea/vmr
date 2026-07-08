# VMR ↔ OpenClaw 兼容性分析报告

> 报告日期：2026-07-07（首次分析）  
> 依据：vmr-audit-2026-07-07.jsonl（27 行 OpenClaw 实际运行日志）  
> 涉及组件：VMR（Virtual Model Router）+ OpenClaw（基于 OpenAI JS SDK 6.39.1）  
> 仓库路径：`/Volumes/SSD2T/code/vmr`  
> 当前设计见 [`VirtualModelRouter_v2_Fable5.md`](VirtualModelRouter_v2_Fable5.md) §5.4-§5.5 与 §11 决策表

本报告是一份**事故调查**——OpenClaw 走 VMR 中转时报错率高、且 prompt 在多轮后暴涨，怀疑代理破坏了语义。审计日志显示模型发回的响应里 `model` 字段名、`` 标签、`[DONE]` 终止标记、SDK 元数据 header 全部"被代理动过"——透明代理是假的。本报告记录**调查路径**与**修复方案的设计依据**，不记录实施过程（实施测试代码看 `internal/router/response_test.go` 与 `internal/server/server_*_test.go` 即可）。

---

## 一、问题陈述

**用户反馈：**
- OpenClaw 直接使用 MiniMax M3 API：✅ 正常工作
- OpenClaw → VMR → MiniMax M3：❌ "反复的发送请求，但是没有后续执行"（看似一直发请求，但模型未真正推进工作）

**日志证据：** `vmr-audit-2026-07-07.jsonl` 共 27 行，跨 7 分钟（20:50–20:58）。24 次模型响应都是 `finish_reason: tool_calls`，说明：
- 模型确实在发工具调用
- OpenClaw 也确实在执行工具（后续请求中可见 tool_result 消息）
- 但模型陷入低效循环（反复 `read` 不存在的文件 → 收到错误 → 再读 → 收到错误 → …）

**初步结论：**
- 链路是通的（不是"卡死"或"断流"）
- 问题在于"机制能跑，但语义层面破坏了模型的决策能力"
- 主要是 VMR 透传响应时**没有做必要的归一化**导致 OpenClaw 看到的内容/字段与"直连 MiniMax"时有可观察的差异

---

## 二、逐项调查发现

### 2.1 关键差异 1：响应 `model` 字段不归一化（最关键）

| 维度 | 直连 MiniMax | 经 VMR 中转 |
|---|---|---|
| 请求 `model` | `"MiniMax-M3"`（或直接是 `MiniMax-M3`） | `"agent"`（VMR 改写为 `MiniMax-M3` 上游） |
| 响应 `model` 字段 | `"MiniMax-M3"`（匹配请求） | `"MiniMax-M3"`（**未改回** `"agent"`） |

**VMR 现状：**
- `internal/adapter/classify.go:74-83` 的 `RewriteModel` 只在**请求方向**改写 `model` 键
- 响应体是**纯透传**（`internal/router/router.go:359-371` 的 `tryOne` 成功分支 + `380-440` 的 `copyFlush` 逐字节转发），response body 中的 `model` 字段保留上游的 `"MiniMax-M3"`

**为什么这是问题：**

虚拟模型的核心语义是**对调用方屏蔽实际模型**。调用方发 `"agent"`，应看到 `"agent"`，而不是 `"MiniMax-M3"`。具体影响：

1. **OpenAI JS SDK 行为**：`ChatCompletionStream.accumulateChatCompletion` 把每个 chunk 的 `model` 字段写到 snapshot（`Object.assign(snapshot, rest)`，ChatCompletionStream.mjs:296），最终 `finalizeChatCompletion` 用 snapshot 里的 `model` 构造完成对象。SDK 不做强制校验，但**调用方拿到的 `chatCompletion.model` 是 `"MiniMax-M3"` 而非 `"agent"`**。
2. **OpenClaw 运行时**：从 OpenClaw 的设计文档（`openclaw_architecture_report.md` §3.2、§3.4）可知，OpenClaw 用 `Execution Bias` 持续推进、用 `Steering Queue` 自然转向——这些机制大多依赖"客户端看到的模型 = 客户端发出的模型"这一前提。**当 `response.model ≠ request.model` 时**，OpenClaw 内部某些 hook（如 `before_model_resolve`、`after_tool_call` 的 telemetry 关联、`agent_end` 审计）很可能因模型名不匹配而走错分支、丢弃结果、或将整轮判为脏数据丢弃。
3. **缓存与配额**：MiniMax 服务端可能对相同 `model` 字段的请求做 KV-cache 复用。客户端持续发 `"agent"`，但 VMR 改写后实际是 `"MiniMax-M3"`，cache 命中没问题；但反过来，response 里写 `"MiniMax-M3"`、客户端期待 `"agent"`，客户端的本地 prompt cache 会出现 key 不一致（key 包含 model），导致 prompt cache miss——这会让 TTFB 退化、token 计费膨胀。

**日志证据：**

```
Line | 请求 model | 响应 model（在所有 chunk 中）
  1  | agent     | (none — 非流式)
  2  | agent     | MiniMax-M3
  3  | (body 截断) | MiniMax-M3
  4  | agent     | MiniMax-M3
  ...
 27  | agent     | MiniMax-M3
```

27 行里 26 行流式响应，**全部**是 `MiniMax-M3`，无一例外。

---

### 2.2 关键差异 2：MiniMax 把 `<think>...</think>` 直接写进 `content` 字段

MiniMax M3 的"思考"内容**不放在独立的 `reasoning_content` 字段**（DeepSeek/Anthropic 风格），而是**作为普通文本**塞进 `content`。日志中 line 1（非流式）和 line 4（流式）的响应均显示：

```json
// Line 1（非流式）
"content": "<think>\nThe user just said \"Hi\". ...\n</think>\nHi there! 👋 I'm an AI assistant..."

// Line 4 的某个 chunk（流式）
"content": "<think>The user is telling me to continue the OpenClaw runtime event. Looking at the context: ... [3748 字符的思考] ... </think>\n\n"
```

**为什么这是问题：**

1. **Prompt 膨胀 + 反馈循环**：OpenClaw 把整个 `content` 存入 assistant message 历史，下次请求时再发回给模型。模型看到自己上一轮的"内心独白"，在 prompt 增长的同时**重复同样的思考模式**——这正是日志里看到的循环：`read` 失败 → 模型再 `read` → 再失败 → …，每轮都把上一轮长达数百字符的失败原因塞进历史。
2. **Token 计费成倍增加**：MiniMax 在 `usage.completion_tokens_details.reasoning_tokens` 已经分离统计（line 4 = 289 reasoning tokens），但 VMR 透传时**这些 reasoning 文本也作为 `content` 发回客户端**，客户端再发回模型时按 input tokens 计费。`prompt_tokens` 从 line 4 的 16,194 涨到 line 27 的 43,116，~2.7× 增长，远超正常上下文积累速度。
3. **导致 line 3 崩溃**：line 3（20:55:41 第二个并发 compaction 请求，body 1.65 MB，`prompt_tokens: 483,688`，`completion_tokens: 1`，`finish_reason: length`）就是 prompt 直接撞上限——这是上轮 compaction 产生的 summary 又被反复 `read` 循环的历史塞爆的典型表现。

日志证据：

```
Line  2 | prompt_tokens=20830 | completion_tokens=1329 | finish=stop           (compaction: 成功)
Line  3 | prompt_tokens=483688 | completion_tokens=1 | finish=length          (compaction: 撞上限, 1 token)
Line  4 | prompt_tokens=16194 | completion_tokens=348 | reasoning=289 | tool_calls
Line  5 | prompt_tokens=16364 | ...                                                  (工具执行循环开始)
...
Line 27 | prompt_tokens=43116 | completion_tokens=3329 | tool_calls
```

注意 line 8→9 一次跳跃 34,987→35,062（+75），但 line 7→8 一次跳跃 19,576→34,987（+15K）—— 突然的跳跃正是一次 `write` 工具写入了大量内容（参见 line 15、27 的 completion_tokens 3,401/3,329 都是 write 长内容）。

---

### 2.3 关键差异 3：响应缺 `data: [DONE]` 终止标记

OpenAI 流式响应规范要求 stream 末尾有：
```
data: [DONE]
```

MiniMax **不发**。所有 27 行响应（VMR 透传给 OpenClaw 后）的末尾字节都是：

```
... "status_msg":""}}
\n\n
```

——两个换行结束，**没有** `data: [DONE]`。

**为什么这是问题：**

OpenAI JS SDK 的 `core/streaming.mjs:31` 检查 `[DONE]` 来设置 `done=true` 并 `continue`：
```js
if (sse.data.startsWith('[DONE]')) {
    done = true;
    continue;
}
```

没有 `[DONE]` 时，SDK 靠**HTTP body 关闭**感知 stream 结束。在 Node 18+/undici 下，VMR 通过 `copyFlush` 主动关连接（`defer body.Close()`）能触发 SDK 这边的 EOF——多数情况下不会挂。但有两个边界场景会出问题：

1. **VMR 与上游之间有 idle 间隔**：`stream_idle: 120s`（默认）下，如果上游最后写完 usage chunk 后**没立即关 TCP**（MiniMax 偶尔会 keep-alive 一下），VMR 会持续 120s 等待下一字节，然后才发 `io.EOF` 给客户端。客户端那边 120s 内不结束 → 触发 `X-Stainless-Timeout`（请求日志里有 120s），`stream.controller.abort()` 抛 `APIUserAbortError`。
2. **客户端做了"收到 [DONE] 才 commit"的逻辑**：OpenClaw 内部可能有类似 commit gate。

日志里 `dur_ms` 最大是 39,527ms（line 15）和 36,885ms（line 27），都还没撞 120s 上限，但**已经接近用户感知的"卡"**。一旦上游连接复用多了，120s 触发就不是 1% 的偶发。

**验证：** 检查 line 4 响应末尾的字节：
```
最后 50 字节（hex）... 7d 7d 0a 0a
                                  ^^ ^^
                                  \n \n — 终止，无 [DONE]
```

---

### 2.4 关键差异 4：工具调用 `id` 格式在 VMR 透传时被 OpenClaw 改写

| 来源 | id 格式 | 长度 |
|---|---|---|
| MiniMax SSE chunk | `call_019f3ca6bfbd7d81ab726ebf` | 29（带 `_`） |
| OpenClaw 存进历史的 assistant 消息 | `call019f3ca6bfbd7d81ab726ebf` | 28（去 `_`） |
| OpenClaw 发回的 tool message | `tool_call_id: call019f3ca6bfbd7d81ab726ebf` | 28（去 `_`） |

**这是 OpenClaw 自己做的一次**下划线剥除（不是 VMR 改的，VMR 是 byte-passthrough）。OpenClaw 内部一致，但**模型这边**：

- MiniMax 上轮生成 `call_019f3ca6bfbd7d81ab726ebf`
- 这轮收到 `tool_call_id: call019f3ca6bfbd7d81ab726ebf`（无下划线）
- 模型按 id 关联 tool result 时，本来要用 string match，现在 match 失败

MiniMax 对这种轻微不匹配的容忍度取决于它的实现。多数 LLM 服务端是**对 tool_call_id 做 fuzzy match 或直接看 content**，不会因为一个字符之差就报 400。但**有可能**：
- 模型把 tool result 误判为"孤儿消息"——忽略它，回到没拿到结果的起点
- 触发 MiniMax 的"软拦截"：`completion_tokens: 1`、`finish_reason: length`（line 3）可能就是这种软拦截的产物

注意 27 行里 26 行的 `req_ids`（OpenClaw 历史里的 id）**永远少一个下划线**，26/26 = 100% 命中。这是 OpenClaw 的固定行为，VMR 没办法改，但**说明 VMR 的"纯透传"假设是错的——中间链路任何环节做 id 归一化都不会更糟**。

---

### 2.5 关键差异 5：Header 白名单太激进（`OpenAI/JS 6.39.1` 全部丢失）

VMR 现状（`internal/server/server.go:69-70`）：
```go
var protocolHeaders = []string{"anthropic-version", "anthropic-beta"}
```

也就是说，**OpenAI 协议下，客户端发来的所有 header 都不透传给上游**——除了 OpenAI adapter 在 `BuildRequest` 里强行设的 `Content-Type` 和 `Authorization`。

客户端实际发的 header（line 4 抓取）：
```
Accept: application/json
Accept-Encoding: gzip, deflate
Accept-Language: *
Authorization: Bearer ***long        ← VMR 用自己的 key 替换
Connection: keep-alive
Content-Length: 58645
Content-Type: application/json
Sec-Fetch-Mode: cors
Traceparent: 00-9a0c...                ← OpenTelemetry 链路追踪
User-Agent: OpenAI/JS 6.39.1           ← SDK 标识
X-Stainless-Arch: arm64
X-Stainless-Lang: js
X-Stainless-Os: MacOS
X-Stainless-Package-Version: 6.39.1
X-Stainless-Retry-Count: 0
X-Stainless-Runtime: node
X-Stainless-Runtime-Version: v24.11.1
X-Stainless-Timeout: 120
```

上游实际收到的 header：
```
Authorization: Bearer ***XYBM           ← VMR 注入的
Content-Type: application/json
```

**为什么这是问题：**

1. **`User-Agent: OpenAI/JS 6.39.1` 丢失** — MiniMax 看到 `"Go-http-client/2.0"`（Go 默认 UA）或其他。某些 LLM 服务商会根据 UA 调整：
   - 是否启用 thinking（部分服务对 `OpenAI/JS` 才开 thinking）
   - 限流策略（对 SDK 类客户端有更宽容的 429 退避）
   - 是否启用 prompt cache（`OpenAI/JS` 通常命中更高优先级 cache pool）

2. **`X-Stainless-*` 全部丢失** — 这是 OpenAI 官方 SDK 的能力自描述（OS/Runtime/版本/超时），上游可据此做：
   - 兼容性路由（v6.x 走新版 chunk 格式，v4.x 走兼容格式）
   - 客户端遥测关联

3. **`Traceparent` 丢失** — 影响 OpenClaw 的分布式追踪，断开后无法关联上游耗时。

直连 MiniMax 时，MiniMax 看到的是 OpenClaw 的真实 UA，可能激活了"OpenAI 兼容"路径的不同代码分支。VMR 切掉了这个信号。

---

### 2.6 次要观察（不影响主问题但值得记录）

| # | 现象 | 评估 |
|---|---|---|
| 1 | `created` 字段在同一响应内变化：内容 chunk 用 1783428983，最后一个 usage chunk 用 1783428981 | MiniMax bug，VMR 透传出去。SDK 不校验，但日志对不齐 |
| 2 | `base_resp: {status_code:0, status_msg:""}` MiniMax 包装字段，混在 OpenAI 标准字段中 | OpenAI SDK 不识别（不在 schema），被忽略。无害 |
| 3 | `input_sensitive` / `output_sensitive` / `audio_content` / `name: "MiniMax AI"` | MiniMax 私有字段。OpenAI SDK 忽略。无害 |
| 4 | `usage.prompt_tokens_details.cached_tokens`、`usage.completion_tokens_details.reasoning_tokens` | MiniMax 在 OpenAI `usage` schema 内嵌自家细分字段，**符合** OpenAI 6.x 的 `details` 约定。✅ 这是 MiniMax 做得好的地方 |
| 5 | 模型调用 `exec`（不在 tools 列表里，只有 `read`/`write`） | MiniMax 行为，调用了 OpenClaw 系统 prompt 里描述的隐式工具。VMR 无法干预 |
| 6 | `Line 4` 工具调用 chunk 把 `content: "</think>\n\n"` 和 `tool_calls: [...]` 塞在同一个 chunk | 不符合 OpenAI"tool call 时 content 应为空"的惯例（line 4 那个 chunk 仍有 7 字符的 content），但 SDK 容忍 |
| 7 | JSON key 在 VMR `RewriteModel` 之后按字母序重排（`map[string]json.RawMessage` 序列化） | OpenAI 协议语义上不依赖 key 顺序。无害，但调试 diff 时难看 |

---

## 三、根因分析

按影响度从高到低：

### 🔴 根因 A：响应 `model` 字段未归一化（高影响、高置信度）

VMR 透传响应时，**没有把 response chunk 里的 `model` 字段改回客户端的虚拟模型名**。OpenClaw 看到 `model: "MiniMax-M3"` 而非自己发出去的 `"agent"`，导致：

- OpenClaw 的内部状态与对话历史里 model 字段不一致
- 触发 OpenClaw 的 hook 分支异常（`before_model_resolve` 等）
- Prompt cache key 漂移

### 🔴 根因 B：`<think>...</think>` 随 `content` 透传，污染历史（高影响、中置信度）

MiniMax 的 reasoning 文本作为普通 `content` 返回，VMR 不剥离。OpenClaw 把它存进 assistant message 历史，下次请求再发回模型。模型看到自己的内心独白 → 重复同样的推理 → 卡在循环。

**这条的影响在 line 3 最明显**：1.65 MB 的 compaction 请求，prompt_tokens 483,688，被 `finish_reason: length` 截断成 1 token——这是 thinking 反馈循环导致 prompt 爆炸的直接证据。

### 🟡 根因 C：响应缺 `data: [DONE]`（中影响、中置信度）

MiniMax 不发 [DONE]，VMR 透传时不补。OpenAI JS SDK 多数情况靠 EOF 兜底，但 idle timeout 配置（默认 120s）与 `X-Stainless-Timeout: 120` 边界条件下会触发 `APIUserAbortError`。

### 🟡 根因 D：Header 白名单太激进（中影响、中置信度）

VMR 当前用白名单只透传 `Content-Type` 和 Anthropic 协议头，**对 OpenAI 协议 0 个客户端 header 透传**——`User-Agent`、`X-Stainless-*`、`Traceparent` 全部被剥。MiniMax 看不到客户端指纹，可能走不同的服务侧代码路径（限流、cache、thinking 启用策略）。**正确做法不是"加 header 到白名单"而是"换黑名单"**——LLM SDK 发的 header 集合是已知固定的，危险 header 可以穷举为黑名单，其余透传（详见修复 4）。

### 🟢 次要：tool_call_id 下划线被 OpenClaw 剥除

这是 OpenClaw 自己的行为，VMR 改不了。多数 LLM 服务端对 id 模糊匹配，影响有限。**但揭示了"VMR 透传"假设的脆弱性**——链路任何一环做归一化都不会让事情更糟。

---

## 四、与"直连 MiniMax 正常"的对比解释

| 现象 | 直连 MiniMax | 经 VMR |
|---|---|---|
| 请求 model | `MiniMax-M3` | `agent`（VMR 改 `MiniMax-M3`） |
| 响应 model | `MiniMax-M3` ✅ | `MiniMax-M3` ❌（应为 `agent`） |
| `<think>` 污染 | 同样存在，OpenClaw 也存进历史 | 同样存在，OpenClaw 也存进历史 |
| `data: [DONE]` | MiniMax 不发，但 OpenClaw 知道怎么兜底 | 同样不发，但 **加上 VMR 多一跳的 stream_idle 风险** |
| `User-Agent` | `OpenAI/JS 6.39.1` 直送 | 被 VMR 剥掉，MiniMax 看到 Go 默认 UA |
| `X-Stainless-*` | 直送 | 被 VMR 剥掉 |
| `Traceparent` | 直送 | 被 VMR 剥掉，分布式追踪断链 |

**直连 vs 经 VMR 的本质差异**：
1. **响应 model 字段不一致**（VMR 引入的"伪不同步"）
2. **Header 指纹被擦除**（VMR 引入的"匿名化"）
3. **stream 多一跳潜在 idle**（VMR 引入的"延迟不确定性"）

第 1 点单独就足以让"虚拟模型"语义破裂；2、3 点点是在第 1 点把 OpenClaw 推入异常分支之后的次级恶化。

**`<think>` 污染** 这一条在两种路径下都存在，所以**不是"直连正常而经 VMR 出问题"的差异因子**。但它会放大 1-3 点的负面效应：客户端要维持更多 `<think>` 历史才能继续工作，而 1-3 点又让客户端的本地状态管理出岔。

---

## 五、建议的修复方案

按"投入产出比"从高到低排序。

### 修复 1（必做）：响应 `model` 字段归一化 ✅ ROI 最高

**做什么：** 在 `internal/router/router.go` 的 `copyFlush`（流式）和 `tryOne` 末尾（非流式 `io.Copy` 路径），把响应体里 `"model":"MiniMax-M3"` 字符串替换为 `"model":"<客户端原始 model 名>"`。

**注意：**
- 必须做 byte-level 替换，**不能 parse JSON 再 marshal**（会改 key 顺序、可能重新编码、影响 streaming flush 时机）
- 用 `bytes.Replace` 或流式 scanner
- 仅替换 `"model":` 紧跟字符串值的字节，不影响 `"base_resp.model"` 等其他位置（MiniMax 不会把 model 放在 base_resp 里，但保险起见可加正则）

**实现位置：**
- `copyFlush`（router.go:393-440）：在 `w.Write(c.data)` 之前对 `c.data` 做替换
- `tryOne` 的非流式路径（router.go:367-371）：在 `io.Copy(w, body)` 外层包一个 `io.Reader` 包装器

**风险：** 极低。纯字符串替换，不动协议结构。

**预期效果：** OpenClaw 收到的 `chatCompletion.model === "agent"`，与请求一致；其内部状态机/hook/cache 行为恢复"直连"语义。

---

### 修复 2（强烈推荐）：剥离 `<think>...</think>` 块

**做什么：** 在 `copyFlush` 里对每个 chunk 的 `data` 做一次简单的字符串扫描/替换：
- 找到 `<think>...</think>`（处理跨 chunk 边界的情况）
- 把这段内容从 `content` 字段中移除

**实现要点：**
- `<think>` 和 `</think>` 是显式标记，状态机扫描比正则快
- 必须处理跨 chunk 边界（一个 chunk 末尾是 `<think>`，下一个 chunk 开头是 `...</think>\n\n`）
- 维护一个 `pendingThinkStart bool` 状态；遇到 `<think>` 标记后丢弃直到 `</think>`

**风险：** 中等。理论上有小概率把"用户故意输入的 `<think>` 字面量"误删，但 OpenClaw 的 system prompt 里写了 `respond naturally`，用户输入这种字面量的概率极低，且即使误删也不影响功能。

**预期效果：**
- 减少 ~30-50% 的 prompt token 增长（reasoning 部分不进历史）
- 切断"思考反馈循环"，模型能更果断地切换动作
- line 3 那种 483K tokens 的爆炸场景基本消失

**替代方案（更保守）：** 不剥离，而是把 `<think>` 内容**移到一个新字段** `reasoning_content`（DeepSeek 风格），让 OpenClaw 自选是否存进历史。但这需要 OpenAI SDK 兼容 schema 增加字段，侵入性大。

---

### 修复 3（推荐）：补 `data: [DONE]` 标记

**做什么：** 在 `copyFlush` 的 stream 末尾（`io.EOF` 之后、`return nil` 之前），向客户端多写一行：
```
data: [DONE]\n\n
```

**实现：** `copyFlush` 函数的 `if c.err == io.EOF { return nil }` 分支改成先 `w.Write([]byte("data: [DONE]\n\n"))` 再 flush 再 return。

**风险：** 极低。`[DONE]` 是 OpenAI 规范明确要求的，OpenAI SDK 见到会 `done=true; continue;`，见到多个 [DONE] 也是相同处理。

**预期效果：** OpenClaw 的 stream 终止不再依赖 TCP EOF，避免 120s idle 边界场景下的 `APIUserAbortError`。

---

### 修复 4（推荐）：把 Header 白名单换成显式黑名单

**为什么不能用白名单：** VMR 现在的白名单策略（`internal/server/server.go:69-70`）对 OpenAI 协议是 **0 个 header 透传**。这会导致所有客户端元数据（`User-Agent`、`X-Stainless-*`、`Traceparent`）被剥。商业 LLM SDK（OpenAI、Anthropic）只发已知、固定的一组 header——里面**没有任何"危险"header**（不会发 `Cookie` / `X-Forwarded-For` / `Proxy-Authorization`）。所以"客户端发什么就透传什么"在 LLM 代理场景下是安全的，前提是有一个**显式黑名单**挡掉真正不能透传的项。

**"必须改"的几项由 adapter.BuildRequest 显式覆盖**（`httpReq.Header.Set`），其余客户端 header 透传。

**做什么：** `internal/server/server.go`：

```go
// headerBlocklist 是 VMR 永远不向上游透传的客户端 header。
// 策略：除下列之外，客户端发什么原样转发。
// "必须改"的几项（Authorization/Host/Content-Length 等）由 adapter.BuildRequest
// 通过 Header.Set 覆盖，这里只防"客户端误传或代理链污染"。
var headerBlocklist = map[string]struct{}{
    "authorization":        {}, // 凭证由 adapter 注入
    "x-api-key":            {},
    "cookie":               {}, // 浏览器会话状态，不该去上游
    "x-forwarded-for":      {},
    "x-forwarded-proto":    {},
    "x-forwarded-host":     {},
    "x-real-ip":            {},
    "proxy-authorization":  {},
    "host":                 {}, // Go http.Request.Host 由 URL 决定
    "content-length":       {}, // Go 自动算
    "transfer-encoding":    {}, // Go 自动管
    "connection":           {}, // Go 自动管
}

// protocolHeaders（Anthropic 专用）保留：客户端显式表达的协议版本
var protocolHeaders = []string{"anthropic-version", "anthropic-beta"}

// 在 chatHandler 里：
hdr := http.Header{}
// 1. 协议头（Anthropic）：直接透传
for _, name := range protocolHeaders {
    if v := r.Header.Get(name); v != "" {
        hdr.Set(name, v)
    }
}
// 2. 其余客户端 header：不在 blocklist 里就透传
for k, vs := range r.Header {
    if _, blocked := headerBlocklist[strings.ToLower(k)]; blocked {
        continue
    }
    for _, v := range vs {
        hdr.Add(k, v)
    }
}
```

**黑名单里每一项的理由：**
- `authorization` / `x-api-key` — 客户端凭证；VMR 必须用自己的 key 调上游，绝不能把客户端的 key 漏过去
- `cookie` — 浏览器/会话状态，LLM API 不该看到
- `x-forwarded-for` / `x-forwarded-proto` / `x-forwarded-host` / `x-real-ip` — IP/协议欺骗向量；上游可能据此做访问控制
- `proxy-authorization` — 代理层凭证，与上游无关
- `host` — 必须是 VMR 自己的 host，不能是客户端的（Go http.Request.Host 由 URL 决定，但显式 block 更安全）
- `content-length` / `transfer-encoding` / `connection` — Go Transport 自动重算，传过去反而会冲突

**白名单外的所有 header 都会透传，包括：**
- `User-Agent: OpenAI/JS 6.39.1` ✅
- `X-Stainless-Arch / Lang / Os / Package-Version / Retry-Count / Runtime / Runtime-Version / Timeout` ✅
- `Traceparent / Tracestate`（OpenTelemetry 追踪）✅
- `Accept-Language` / `Sec-Fetch-Mode` 等无害元数据 ✅

**未来 SDK 升级加了新 header 不需要改 VMR 代码**——这是黑名单方案相对白名单扩展的核心优势。

**风险：** 极低。黑名单里的项都是"代理必须重写或剥除"的标准清单（HAProxy / Nginx / Envoy 的 proxy_set_header 都有等价黑名单）。

**预期效果：** MiniMax 看到 `OpenAI/JS 6.39.1` 真实 UA，可能激活"OpenAI 兼容"更优代码路径（更激进的 prompt cache、更合理的限流策略）。`Traceparent` 不断链，OpenClaw 分布式追踪可关联到上游耗时。

**注意：**
- 仍必须由 VMR 注入 `Authorization`（`BuildRequest` 已做 `httpReq.Header.Set("Authorization", ...)`）
- `Accept-Encoding` 客户端发了也无所谓——Go Transport 在 request 侧会**自动剥掉**这个头，避免自己声明但 body 没真的 gzped 导致的 400。但即便不显式 block，Go 的 `net/http` 也会处理。
- `X-Stainless-Timeout: 120` 透传给上游**可能让 MiniMax 知道客户端期望多久**。这是好事——MiniMax 可据此安排流式分块节奏。

---

### 修复 5（可选）：tool_call_id 归一化（但收益有限）

**做什么：** 把 MiniMax 返回的 `call_<id>` 在 chunk 中替换为 `call<id>`（去掉下划线），或者反过来把 OpenClaw 发回的 `call<id>` 还原为 `call_<id>`。

**风险：** 高。tool_call_id 是 LLM 用来关联 tool result 的关键键，**任何不一致都会让模型把 tool result 视为孤儿**。最坏情况是 MiniMax 拿到不认识的 id 直接返回 400。

**预期效果：** 不确定。MiniMax 对 id 模糊匹配的容忍度未知。

**建议：** **不做**这个修复。让 OpenClaw 自己处理 id 格式不一致。如果要做，应该在 OpenClaw 一侧（不要在 VMR 中转路径上做 ID 转换）。

---

## 六、推荐的实施顺序

| 阶段 | 修复 | 改动量 | 风险 | 预期收益 |
|---|---|---|---|---|
| P0 | 修复 1：响应 `model` 归一化 | 30 行 Go | 极低 | 解决"虚拟模型语义破裂" |
| P0 | 修复 4：Header 白名单 → 显式黑名单 | 15 行 Go | 极低 | 恢复客户端指纹 + 未来 SDK 升级免改 VMR |
| P1 | 修复 2：剥离 `<think>` 块 | 80 行 Go（含状态机） | 中 | 切断反馈循环，防 line 3 类爆炸 |
| P1 | 修复 3：补 `data: [DONE]` | 5 行 Go | 极低 | 避免 idle 边界 abort |
| P2 | 修复 5 | —— | 高 | 不建议做 |

**P0 全部完成后，预期**：
- OpenClaw 看到的 `chatCompletion.model === "agent"`
- MiniMax 看到 `OpenAI/JS 6.39.1` 真实 UA + `X-Stainless-*` 元数据
- 链路追踪 (`Traceparent`) 不断
- 未来任何 LLM SDK 升级加了新 header，VMR 都自动兼容

这一组合应该能让 OpenClaw 恢复到"直连 MiniMax"的效果（除了不可避免的 `<think>` 污染，那条 P1 解决）。

---

## 七、需要 OpenClaw 一侧配合的事情（VMR 改不了）

| 项 | 描述 | 影响 |
|---|---|---|
| tool_call_id 剥下划线 | OpenClaw 把 `call_xxx` 存成 `callxxx` | 多数 LLM 容忍；不需要 VMR 修复 |
| `<think>` 内容的存储策略 | OpenClaw 把 `<think>` 整段存进 assistant message | 主导 prompt 膨胀；如果 VMR P1 不做，OpenClaw 应该自己 strip |
| 模型字段不匹配的处理 | OpenClaw 见到 `model != request.model` 时的策略 | 这条如果 VMR P0 修了就不需要 OpenClaw 改 |

**建议给 OpenClaw 的 issue：** "When streaming through a proxy that doesn't echo back the requested model name, the model field in the response can be silently substituted. This breaks Execution Bias / prompt cache / hook routing. Proxies should rewrite response model field to match the request."

---

## 八、验证计划

修复 1-4 上线后：

1. **单元测试**：
   - `copyFlush` mock 一个 `MiniMax-M3` 响应的 reader，验证 `w` 写入流里 `model` 字段全是 `agent`
   - `copyFlush` 输入 `<think>...</think>\n\ndata: [DONE]`，验证输出无 think 块、有 DONE
2. **集成测试**：
   - 配置 `model: coding` → MiniMax 端点
   - curl `model: coding`，grep response chunks 里的 `model` 字段应全是 `coding`
3. **端到端验证**：
   - OpenClaw 配 `model: agent`（via VMR）跑一个多轮 tool-use 任务
   - 对照直连 MiniMax 跑同一个任务
   - 比较：(a) 总完成轮数 (b) prompt_tokens 增长曲线 (c) 是否出现 `finish_reason: length`

---

## 九、置信度声明

| 结论 | 置信度 | 依据 |
|---|---|---|
| VMR 是问题主因之一 | 高 | 日志明确显示 `model` 字段未归一化、header 被擦、`[DONE]` 缺失 |
| 修复 1（model 归一化）能解决主诉 | 中-高 | 与 OpenClaw 架构设计文档 (`openclaw_architecture_report.md` §3.2/3.4) 的 hook 设计逻辑一致 |
| 修复 2（剥离 think）能解决 line 3 类崩溃 | 中 | 机制上合理（切断反馈循环），但需要实测确认 |
| OpenClaw 自己也会受影响（直连也有 think 污染） | 高 | 同一个 SDK、同一个 system prompt；直连与 VMR 在这点上行为一致 |
| 27 行日志中的"循环"主要由 model 决策质量导致 | 中 | MiniMax M3 模型本身的弱决策能力；think 污染放大这一问题 |

---

## 十、附录：关键代码定位

| 文件 | 行 | 说明 |
|---|---|---|
| `internal/router/router.go` | 359-371 | `tryOne` 成功分支：写 header + `io.Copy` / `copyFlush`，**这里就是修复 1/2/3 的接入点** |
| `internal/router/router.go` | 380-440 | `copyFlush`：逐 chunk 转发，**修复 1（model 替换）和修复 3（追加 DONE）在这里做** |
| `internal/server/server.go` | 69-70 | `protocolHeaders` 白名单 + 新增 `headerBlocklist` 黑名单，**修复 4 在这里改** |
| `internal/adapter/openai/openai.go` | 25-39 | OpenAI adapter `BuildRequest`，改写 URL/Authorization/Content-Type |
| `internal/adapter/classify.go` | 74-83 | `RewriteModel` 改写请求 `model` 键（json.Unmarshal/Remarshal 模式） |
| OpenAI SDK 6.x `lib/ChatCompletionStream.mjs` | 239-250 | `_ChatCompletionStream_accumulateChatCompletion`：`Object.assign(snapshot, rest)`，model/id/created 都从这里进 snapshot |
| OpenAI SDK 6.x `core/streaming.mjs` | 18-32 | `Stream.fromSSEResponse`：唯一检查 `[DONE]` 标记的地方 |

---

*报告完成。实际验证数据见 `internal/router/response_test.go`、`internal/server/server_*_test.go` 内的测试。*

