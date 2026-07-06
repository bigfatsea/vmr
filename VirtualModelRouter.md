# Virtual Model Router（暂定名）

## 项目定位

Virtual Model Router（VMR）是一个**本地运行、单二进制、配置驱动**的 LLM 路由器。

它的职责只有一个：向客户端提供一个稳定的 OpenAI Compatible API，而将底层模型供应商、账号、API Key、路由策略和故障切换全部隐藏起来。

它不是 AI Gateway，不是聊天应用，也不是模型管理平台，而是一个开发工具（Developer Tool），定位类似于 `nginx`、`caddy`、`frp`、`rclone` 等 Unix 风格工具：完成一件事，并且做到极简。

---

## 问题定义

目前同时订阅多个模型供应商已经成为常态，例如 OpenAI、Anthropic、OpenRouter、DeepSeek、SiliconFlow 等。

这些供应商可能提供相同模型，也可能拥有不同额度、价格、延迟和稳定性。用户需要不断修改 Base URL、API Key 或配置文件，在不同 Provider 之间手动切换。当某个账号额度耗尽、接口异常或临时不可用时，整个调用链都会中断。

客户端真正需要连接的并不是某个 Provider，而是一个**稳定的虚拟模型（Virtual Model）**。

例如：

* `coding`
* `smart`
* `cheap`

客户端永远请求 `coding`，至于最终由哪个 Provider、哪个账号、哪个 API Key 完成请求，应该由 Router 自主决定。

因此，整个项目的核心不是 Provider Proxy，而是 **Virtual Model Routing**。

---

## 核心抽象

整个系统只保留四个核心概念。

**Virtual Model**

客户端可见的模型，也是整个系统唯一暴露出去的模型名称。

它代表一种能力，而不是某一家供应商。

例如：

* `coding`
* `reasoning`
* `cheap`

一个 Virtual Model 可以对应多个实际模型。

---

**Endpoint**

一个可调用的模型实例。

它由 Provider、模型名称、API Endpoint、API Key 等共同组成，是 Router 的最小调度单位。

例如：

* OpenAI GPT-5
* OpenRouter GPT-5
* Anthropic Claude Sonnet
* DeepSeek V3

即使 Provider 相同，不同 API Key 也属于不同 Endpoint。

---

**Strategy**

Router 的唯一职责就是根据策略选择一个 Endpoint。

策略应保持可组合，但保持简单，例如：

* Priority
* Round Robin
* Weighted
* Sticky

MVP 只实现 Priority。

---

**Health**

每个 Endpoint 都维护自己的健康状态。

请求失败、额度耗尽、429、超时等事件都会影响 Health。Router 根据 Health 自动跳过不可用节点，并定期恢复探测，而不是持续重试已经失效的 Endpoint。

---

## 功能边界

项目只负责以下事情：

* OpenAI Compatible API Proxy
* Virtual Model 映射
* Provider Routing
* Failover
* Retry
* Health Check
* Streaming Forward
* Hot Reload
* CLI 管理

除此之外，一律不做。

明确排除以下能力：

* Dashboard
* Database
* User Management
* Billing
* Analytics Platform
* Prompt Management
* Workflow
* Plugin System
* MCP Framework
* 企业级 AI Gateway 能力

保持单一职责，不向平台演化。

---

## 架构原则

整个系统坚持几个长期不会改变的原则。

首先，一切行为都由配置驱动，而不是代码驱动。任何 Provider、模型、策略的调整，都应该通过修改配置完成，而不是重新开发。

其次，Router 不理解业务，它只理解 Endpoint 的状态以及路由策略，不关心 Prompt，也不参与模型能力判断。

再次，系统应尽可能复用现有生态，而不是重复实现。例如 Provider 协议适配应优先复用官方 SDK 或成熟 Adapter，Router 只负责统一调度，不负责维护所有模型协议。

最后，Router 内部应定义一套稳定的 Canonical Request / Response，对外部协议变化保持隔离。无论 OpenAI、Anthropic 还是未来新的 Provider，都只能影响 Adapter，而不能影响 Router。

---

## 技术选型

项目采用 Go。

原因不是追求极致性能，而是追求部署体验和长期维护成本。

Go 可以直接生成单个静态可执行文件，无需 Python、Node.js、Docker 或运行时环境，天然适合作为本地后台服务。项目主要工作是 HTTP 转发、流式传输和状态调度，并不存在计算密集型场景，因此 Go 已足够满足性能需求，同时拥有更低的开发和维护成本。

整体依赖标准库，避免引入重量级框架。

---

## 开发策略

坚持 MVP。

第一阶段只支持 OpenAI Compatible API，只实现 Priority Routing、Failover、Streaming、Health、CLI 和配置热加载。

后续再逐步增加 Anthropic、Gemini 等 Adapter，以及 Round Robin、Weighted、Sticky 等策略。

所有新增能力都必须满足一个原则：**增加的是能力，而不是复杂度。**

---

## 完成标准

项目完成时，应满足以下目标：

用户安装一个可执行文件，提供一份 YAML 配置，执行一条命令即可启动。

所有 OpenAI Compatible 客户端只需要将 Base URL 指向本地 Router，即可透明获得 Provider 路由、自动故障切换和统一模型入口，而无需修改任何业务代码。

整个项目应保持单二进制、零数据库、零 Web UI、零外部基础设施依赖，并始终保持简单、可维护、可理解。
