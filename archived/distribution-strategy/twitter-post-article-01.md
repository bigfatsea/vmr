# Twitter/X 发帖文案（Article 01）

---

## 版本 A：故事型（推荐首发）

我的 Agent 跑了一整夜，47 轮交互，中间不知不觉掉进了一个死循环——对着同一个文件读了 18 次。

控制台日志上全是 "200 OK"，完全看不出来问题在哪。直到我用 VMR 跑了一次 `vmr story`——它把整个任务还原成了可读的叙事流，每一步的上下文增量、工具调用和 Compaction 信息损失都清清楚楚。

原来"黑盒"不是真的黑，你只是缺了合适的工具。

VMR：为 AI Agent 而生的运行黑匣子与透明路由
🔗 github.com/bigfatsea/vmr
📦 brew install bigfatsea/tap/vmr

#Agent #LLM #开源 #Go #AI


## 版本 B：功能型（适合置顶/固定帖）

给 Agent 装一个黑匣子 🎙️

VMR 能做什么：
✅ 两层完整字节审计——每一层收发的原始字节都在
✅ 自动还原 Agent 任务叙事——50 轮交互，每一步清清楚楚
✅ 物理级请求回放——把任意一条历史记录原样重发复现
✅ 零代码侵入——改个 base_url 就行，不需 SDK 埋点
✅ Go 单二进制 12MB——brew install 即装即用

开源 | github.com/bigfatsea/vmr

#AI #Agent #开源工具


## 版本 C：短帖型（碎片时间传播）

你以为你的 Agent 在正常工作，其实它可能在第 23 轮就开始死循环了——只是你不知道。

VMR —— 给 Agent 装个黑匣子，看清每一步。

brew install bigfatsea/tap/vmr
github.com/bigfatsea/vmr

#开源 #Agent #LLM
