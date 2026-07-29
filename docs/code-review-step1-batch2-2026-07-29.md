# 第 1 步 —— 第二批改进执行计划（2026-07-29）

> 基于 `code-review-step1-2026-07-29.md` §7 剩余待办项，逐项对照源码核实后，
> 选出问题清晰、方案确定、低风险、高回报的 3 件事，给出可执行的 diff。

---

## 候选筛选

原始 5 项中淘汰 2 项：

| 条目 | 淘汰原因 |
|---|---|
| §1.5 BlobIndex 只记首次出现 | 第 2 步缝合后才暴露的问题，第 1 步修复属于过度设计。且修复涉及 `BlobIndex` 结构体变更，改动面大 |
| §1.4 fetchFromFile 无提前退出 | 需要改 `audit.ForEachLine` API 或新增变体，跨包改动，留给第 2 步 |

入选 3 项：

| # | 条目 | 类型 | 回报 | 风险 | 改动量 |
|---|---|---|---|---|---|
| A | §1.2 PreviewTitle 性能 | 性能修复 | 高（消除 O(N×file_size) I/O） | 零（纯批量化，语义不变） | ~30 行 |
| B | §6.3 Golden test | 回归防护 | 高（设计文档验收标准 #4） | 零（纯新增测试文件） | ~80 行 |
| C | §2.1 Ungrouped 排查入口 | 可观测性 | 低但改造成本极低 | 零（新增 flag） | ~10 行 |

---

## A. PreviewTitle 性能批量化

### 问题核实

`cmd_story.go` 的 `listJourneys` 对每个候选人调用一次 `story.PreviewTitle`：

```go
for _, l := range cands {
    title, terr := story.PreviewTitle(l, prof)  // 每次 → FetchRecords([1 loc])
    ...
}
```

`story.PreviewTitle` 内部调用 `ctxgraph.FetchRecords`。`FetchRecords` **已经支持按文件批量化**（内部先建 `byPath` map，每个文件只打开一次），但 `listJourneys` 每次只传 1 个 location，批量化被绕过。结果：168 个候选 = 168 次文件打开 + 全量顺序扫描。

`ctxgraph.Scan`（第一步）已经全扫过一次。`listJourneys`（第二步）又因为逐个调用 `PreviewTitle` 而重复全扫。对 15 天语料，`Scan` 约 57 秒，`listJourneys` 可能数倍于此。

### 方案

**思路**：不缓存到 `Manifest`/`Lineage`（避免 `ctxgraph` 结构体膨胀和 profile 耦合），而是让 `listJourneys` 在调用侧做批量化——先把所有候选根 location 收集起来，一次 `FetchRecords` 全部取回，再逐个提取标题。

需要一个新函数来从已取回的 `*audit.Record` 中提取标题（不重复做 I/O）。现有的 `PreviewTitle` 前半段是 `FetchRecords` + 查 map，后半段是从 record 中找第一条真实用户指令。把后半段抽成独立函数即可。

### 源码变更

#### `internal/story/preview.go` — 新增 `PreviewTitleFrom` + 重构 `PreviewTitle`

```diff
+// PreviewTitleFrom extracts a Journey title from an already-fetched root
+// record — the I/O-free half of PreviewTitle. Callers that batch their own
+// FetchRecords (e.g. listing many candidates) call this instead to avoid
+// opening each file once per candidate.
+func PreviewTitleFrom(rec *audit.Record, prof profile.Profile) string {
+	if rec == nil {
+		return "(无法读取)"
+	}
+	body, ok := rec.Client.Request.Body.(map[string]any)
+	if !ok {
+		return "(无标题)"
+	}
+	msgs := chatmsg.Messages(body)
+	rawMsgs, _ := body["messages"].([]any)
+	off := chatmsg.MsgOffset(body)
+	for idx, m := range msgs {
+		if m.Role != "user" {
+			continue
+		}
+		if text, ok := prof.RealUserText(m, rawMsgs, idx-off); ok {
+			return preview(text)
+		}
+	}
+	return "(无标题)"
+}
```

同时重构 `PreviewTitle` 自身调用新函数（消除重复）：

```diff
 func PreviewTitle(l *ctxgraph.Lineage, prof profile.Profile) (string, error) {
 	root := l.Manifests[0]
 	loc := ctxgraph.Loc{Path: root.Path, Line: root.Line}
 	recs, err := ctxgraph.FetchRecords([]ctxgraph.Loc{loc})
 	if err != nil {
 		return "", err
 	}
-	rec := recs[loc]
-	if rec == nil {
-		return "(无法读取)", nil
-	}
-	body, ok := rec.Client.Request.Body.(map[string]any)
-	if !ok {
-		return "(无标题)", nil
-	}
-	msgs := chatmsg.Messages(body)
-	rawMsgs, _ := body["messages"].([]any)
-	off := chatmsg.MsgOffset(body)
-	for idx, m := range msgs {
-		if m.Role != "user" {
-			continue
-		}
-		if text, ok := prof.RealUserText(m, rawMsgs, idx-off); ok {
-			return preview(text), nil
-		}
-	}
-	return "(无标题)", nil
+	return PreviewTitleFrom(recs[loc], prof), nil
 }
```

#### `cmd/vmr/cmd_story.go` — `listJourneys` 改为批量取回

```diff
 func listJourneys(cands []*ctxgraph.Lineage, g *ctxgraph.Graph, firstPath string, prof profile.Profile, includePartial bool) error {
 	excluded := len(g.Lineages) - len(cands)
 	fmt.Printf("%d candidate journey(s) (%d total lineage(s), %d single-request/scheduled excluded):\n\n", len(cands), len(g.Lineages), excluded)
+
+	// Batch-fetch every candidate's root record so each source file is
+	// opened at most once — FetchRecords groups by path internally, but
+	// only when the caller passes all locations in one call (zstd isn't
+	// seekable, so per-candidate FetchRecords = N full-file scans).
+	locs := make([]ctxgraph.Loc, len(cands))
+	for i, l := range cands {
+		locs[i] = ctxgraph.Loc{Path: l.Manifests[0].Path, Line: l.Manifests[0].Line}
+	}
+	recs, ferr := ctxgraph.FetchRecords(locs)
+	if ferr != nil {
+		return ferr
+	}
+
 	shown, skippedPartial := 0, 0
-	for _, l := range cands {
+	for i, l := range cands {
 		partial := story.IsPartialHead(l, firstPath)
 		if partial && !includePartial {
 			skippedPartial++
 			continue
 		}
-		title, terr := story.PreviewTitle(l, prof)
-		if terr != nil {
-			title = fmt.Sprintf("(读取失败: %v)", terr)
-		}
+		title := story.PreviewTitleFrom(recs[locs[i]], prof)
 		mark := ""
 		if partial {
 			mark = " [断头]"
 		}
 		first, last := l.Manifests[0], l.Manifests[len(l.Manifests)-1]
 		fmt.Printf("  %s%-6s %3d 轮  %s → %s  %s\n",
 			story.ID(l), mark, len(l.Manifests),
 			first.TS.Format("01-02 15:04"), last.TS.Format("15:04"), title)
 		shown++
 	}
 	if skippedPartial > 0 {
```

### 影响分析

- `PreviewTitle` 的签名和行为不变（仍返回 `(string, error)`），现有调用点不感知
- `listJourneys` 从 O(N×file_size) I/O 降为 O(file_count×file_size)，对典型 15 文件语料约 **10× 加速**
- `FetchRecords` 返回 error 时 `listJourneys` 直接返回 error（不再在循环内吞错误并降级为"读取失败"）——这是行为变化，但更合理：如果磁盘错误导致文件打不开，应该直接报错退出，而不是列出 168 行 "读取失败" 的标题

---

## B. Golden test

### 问题核实

当前没有任何测试验证端到端渲染输出。设计文档附录 C.3 验收标准 #4 明确要求："golden test：`testdata/` 里一份 3 请求的小语料，Markdown 输出逐字节稳定，重跑幂等"。这不仅是验收标准，也是防止渲染回归的唯一手段。

Story 包中的 `TestRenderMarkdown_BasicStructure` 只检查输出**包含**若干子串，不检查整体结构。如果某个改动意外地破坏了步骤编号、损坏了 `<details>` 配对、改变了字段排列，现有测试检测不到。

### 方案

1. 在 `internal/story/testdata/` 下创建一个小型 JSONL fixture（~4 条记录），覆盖：
   - system 消息渲染
   - 用户指令 → 任务标题
   - assistant 文本响应
   - tool_call + tool_result（配对）
   - 跨步骤事件去重（同一条 system/user 消息在步骤 2 不再出现）
   - usage 显示
   - Step 头部（时间、耗时、endpoint）

2. 创建 `golden_test.go`，构建 Journey 后用 `RenderMarkdown` 渲染，与 `testdata/golden.md` 做逐字节比较。

3. 如果输出不匹配，测试失败并打印 diff。

### Fixture 设计

```
记录1: system + user("帮我查一下 A 股新股打新收益")  → assistant "好的，我来搜索"
记录2: system + user("帮我查一下…") + assistant(tool_call: web_search) + tool(tool_result: "results")
       → assistant "搜索完成，收益如下…"
```

4 条记录，2 个步骤。覆盖：用户指令、assistant 文本、tool_call/tool_result 配对、事件去重。

### 源码变更

#### `internal/story/testdata/golden.jsonl`（新文件）

```jsonl
{"ts":"2026-07-29T10:00:00Z","dur_ms":1500,"model":"agent","protocol":"openai","stream":true,"outcome":"ok","client":{"request":{"method":"POST","path":"/v1/chat/completions","headers":{},"body":{"model":"agent","stream":true,"messages":[{"role":"system","content":"You are a helpful assistant."},{"role":"user","content":"帮我查一下 A 股新股打新收益"}]}},"response":{"status":200,"headers":{},"body":"data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"好的，我来搜索相关数据。\"}}],\"model\":\"agent\"}\n\ndata: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\",\"delta\":{}}],\"usage\":{\"prompt_tokens\":120,\"completion_tokens\":15,\"prompt_tokens_details\":{\"cached_tokens\":80}}}\n\ndata: [DONE]\n"}},"attempts":[{"endpoint":"openai:provider:agent","dur_ms":1400,"response":{"status":200}}]}}
{"ts":"2026-07-29T10:00:02Z","dur_ms":3200,"model":"agent","protocol":"openai","stream":true,"outcome":"ok","client":{"request":{"method":"POST","path":"/v1/chat/completions","headers":{},"body":{"model":"agent","stream":true,"messages":[{"role":"system","content":"You are a helpful assistant."},{"role":"user","content":"帮我查一下 A 股新股打新收益"},{"role":"assistant","content":"好的，我来搜索相关数据。","tool_calls":[{"id":"c1","function":{"name":"web_search","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c1","content":"2026年A股新股打新平均收益率为12.5%，中签率0.03%"}]}},"response":{"status":200,"headers":{},"body":"data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"根据搜索结果，2026年A股新股打新平均收益率为12.5%，中签率约0.03%。\"}}],\"model\":\"agent\"}\n\ndata: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\",\"delta\":{}}],\"usage\":{\"prompt_tokens\":280,\"completion_tokens\":30,\"prompt_tokens_details\":{\"cached_tokens\":200}}}\n\ndata: [DONE]\n"}},"attempts":[{"endpoint":"openai:provider:agent","dur_ms":3100,"response":{"status":200}}]}}
{"ts":"2026-07-29T10:00:06Z","dur_ms":5000,"model":"agent","protocol":"openai","stream":true,"outcome":"ok","client":{"request":{"method":"POST","path":"/v1/chat/completions","headers":{},"body":{"model":"agent","stream":true,"messages":[{"role":"system","content":"You are a helpful assistant."},{"role":"user","content":"帮我查一下 A 股新股打新收益"},{"role":"assistant","content":"好的，我来搜索相关数据。","tool_calls":[{"id":"c1","function":{"name":"web_search","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c1","content":"2026年A股新股打新平均收益率为12.5%，中签率0.03%"},{"role":"assistant","content":"根据搜索结果，2026年A股新股打新平均收益率为12.5%，中签率约0.03%。"},{"role":"user","content":"继续，把前10名列出来"}]}},"response":{"status":200,"headers":{},"body":"data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"好的，前10名的新股打新收益如下…\"}}],\"model\":\"agent\"}\n\ndata: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\",\"delta\":{}}],\"usage\":{\"prompt_tokens\":400,\"completion_tokens\":50,\"prompt_tokens_details\":{\"cached_tokens\":300}}}\n\ndata: [DONE]\n"}},"attempts":[{"endpoint":"openai:provider:agent","dur_ms":4900,"response":{"status":200}}]}}
```

（3 条记录：第 1 步纯文本应答，第 2 步 tool_call+tool_result，第 3 步追加用户指令 → 新 task）

#### `internal/story/golden_test.go`（新文件）

```go
package story

import (
	"os"
	"path/filepath"
	"testing"

	"vmr/internal/ctxgraph"
	"vmr/internal/story/profile"
)

func TestGoldenMarkdown(t *testing.T) {
	goldenJSONL := filepath.Join("testdata", "golden.jsonl")
	goldenMD := filepath.Join("testdata", "golden.md")

	g, err := ctxgraph.Scan([]string{goldenJSONL})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) == 0 {
		t.Fatal("no lineages found in golden fixture")
	}
	l := g.Lineages[0]
	j, err := Build(l, profile.Generic)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	got := RenderMarkdown(j)

	want, err := os.ReadFile(goldenMD)
	if err != nil {
		// First run: write the golden file so the developer can review it.
		if os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(goldenMD), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(goldenMD, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Skipf("golden file written to %s — review it, then re-run", goldenMD)
		}
		t.Fatal(err)
	}

	if got != string(want) {
		t.Errorf("golden output mismatch.\n=== got ===\n%s\n=== want ===\n%s", got, string(want))
	}
}
```

初次运行会自动生成 golden 文件并 skip，开发者 review 后第二次运行生效。

#### `internal/story/testdata/golden.md`（新文件，初次由测试自动生成后人工 review 签入）

内容略（由上述 fixture 渲染产出，逐字节稳定）。

---

## C. Ungrouped 记录排查入口

### 问题核实

`Scan` 返回 `Graph.Ungrouped`，`cmd_story.go` 打印了计数但无法深入排查。当 corpus 中出现意外的 `Ungrouped` 记录时（例如新 agent 格式导致 SessKey 推导失败），用户只能看到数字，无法定位。

### 方案

加 `--show-ungrouped` flag。开启后，在扫描汇总行之后、列出候选之前，打印前 N 条（10 条）未归组记录的 TS 和 path:line。

### 源码变更

#### `cmd/vmr/cmd_story.go` — flag 定义

```diff
 	includePartial := fs.Bool("include-partial", false, "also list/render journeys whose head looks truncated by the loaded file range (design doc §11 D1)")
+	showUngrouped := fs.Bool("show-ungrouped", false, "print the first 10 ungrouped records (no SessKey could be derived)")
```

#### `cmd/vmr/cmd_story.go` — 在扫描汇总后、候选列表前

```diff
 	fmt.Printf("%d lineage(s), %d ungrouped record(s), %d unparseable record(s)\n", len(g.Lineages), len(g.Ungrouped), g.NoBody)
+
+	if *showUngrouped && len(g.Ungrouped) > 0 {
+		limit := 10
+		if len(g.Ungrouped) < limit {
+			limit = len(g.Ungrouped)
+		}
+		fmt.Printf("\n前 %d 条未归组记录:\n", limit)
+		for i := 0; i < limit; i++ {
+			m := g.Ungrouped[i]
+			fmt.Printf("  %s  line %d  %s  model=%s  outcome=%s\n",
+				m.TS.Format("2006-01-02 15:04:05"), m.Line, m.Path, m.Model, m.Outcome)
+		}
+		fmt.Println()
+	}
+
 	firstPath := paths[0]
```

同时在 `main.go` 的 usage 中补一句：

```diff
-       vmr story [-journey id] [-include-partial] [-o dir] <audit.jsonl|glob>...   (default -o: ./reports; no -journey lists candidates)
+       vmr story [-journey id] [-include-partial] [-show-ungrouped] [-o dir] <audit.jsonl|glob>...   (default -o: ./reports; no -journey lists candidates)
```

---

## 执行顺序与依赖

三项互不依赖，可任意顺序执行。建议 A → B → C（回报从高到低）。

| 步 | 内容 | 文件 | 行数 |
|---|---|---|---|
| A | PreviewTitle 批量化 | `preview.go` + `cmd_story.go` | ~+20 / ~+10 |
| B | Golden test | `testdata/golden.jsonl` + `testdata/golden.md` + `golden_test.go` | ~+80 |
| C | Ungrouped 排查 | `cmd_story.go` + `main.go` | ~+10 / ~+1 |
