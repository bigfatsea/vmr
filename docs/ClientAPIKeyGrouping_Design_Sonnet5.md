<!-- Ver 2026-07-15 05:00, by Sonnet 5 -->

# 按调用方（API Key）分组统计 — 方案评估与设计

> 任务性质：评估 + 系统设计 + 实现。第三版起本文档即为已落地功能的设计记录，与代码同步维护。
> 触发问题：多个 client/调用方共用一个 vmr 实例时，事后日志分析（`vmr report`）无法区分谁产生了哪些请求。
> 版本历史：
> - 第一版：`api_keys` 具名映射 + `by-key/<name>/` 子目录（含 details 的复制/软链接取舍）。
> - 第二版：吸收反馈简化——不再需要 config 里配"名字"，也不再需要 `by-key/` 子目录或对 `details/` 做任何处理；`ClientKeyTag` 固定取 key 值末 6 位。
> - 第三版：`KeyTag` 改为"先取末 6 位，若其中含横杠则只保留最后一个横杠之后的部分"，支持短于 6 位的有意义后缀，避免定长截断把可读后缀切乱。
> - 第四版（本版，已实现）：`api_key`/`api_keys` 都不配置时（纯内网、无需真实鉴权的场景），门依旧完全敞开，但客户端自己发来的 `Authorization`/`x-api-key` 值仍会被 `KeyTag` 提取打标签——不需要 vmr 侧配置任何 key 列表，调用方只要自己在值的末尾带上有意义的后缀，就能被 `vmr report` 分组。见 §六。

---

## 一、现状分析（读代码得出的事实）

### 1.1 vmr 自身的鉴权只有一把全局 Key

`internal/config/config.go:105-108`：

```go
type Config struct {
    Listen      string `yaml:"listen"`
    APIKey      string `yaml:"api_key"`
    ...
```

只有一个字符串字段，`config.example.yaml` 里的注释也说明它是"保护路由器本身"的可选项（不是上游 provider 的 key——上游那把在 `Provider.APIKey`，两者概念不同，命名却撞了，看代码时容易搞混，设计时要留意别进一步加剧这个混淆）。

鉴权发生在 `internal/server/server.go:44-57`：

```go
func (s *Server) checkAuth(r *http.Request) bool {
    key := s.rt.Snapshot().Cfg.APIKey
    if key == "" {
        return true // 未配置 = 不鉴权
    }
    got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
    if got == "" {
        got = r.Header.Get("x-api-key")
    }
    return subtle.ConstantTimeCompare([]byte(got), []byte(key)) == 1
}
```

单把 key，常数时间比较一次，返回布尔值——**没有"是哪个调用方"这个概念**，自然也没有地方可以往下传。

### 1.2 审计记录里已经有 key 的痕迹，但只是打了码，格式不适合直接当分组字段

`chatHandler`（`server.go:120-253`）在鉴权**之前**就已经开始构造 `audit.Record` 并注册了 `defer` 写盘（`server.go:122-144`）——这点很重要：**鉴权失败的请求也会被完整记入审计日志**，包括它带的（打码后的）Authorization header。打码逻辑在 `internal/audit/audit.go:198-229`：

```go
func mask(v string) string {
    ...
    if len(cred) <= 4 {
        return prefix + "***"
    }
    return prefix + "***" + cred[len(cred)-4:]
}
```

即 `Bearer ***1234` 这种形式——保留了后 4 位。这次的反馈定了调：**与其在 config 里另建一套"名字"，不如直接把这个已经存在的"key 尾巴"机制，扶正成正式的分组字段**——本质上是同一个想法的简化版：key 本身就是调用方自己配的，尾巴留什么完全由 ta 自己决定，不需要 vmr 再维护一层"名字 → key"的映射。第二版方案就是这个思路的落地（见 §三）。

### 1.3 report 管线的分层结构

`vmr report` 的产物链条（`cmd/vmr/main.go` 的 `cmdReport`，`internal/report/` 下的几个文件）：

1. `report.Build(paths, ...)` → 全量聚合出 `vmr-report.json/md`（`report.go`，按 date×protocol×model 等多个"桶"聚合，Format 字段有版本号，改结构要走版本升级流程——`report.go:19-52` 的注释记录了 1→9 的每次结构变更）。
2. `report.AnalyzeSessions(paths)` → 产出 `*SessionAnalysis`：把请求按"会话/任务"分组（`session.go`），并给每条记录算出一堆特征（`ReqInfo`，`session.go:48-121`）。
3. `report.WriteRequests(sess, path)` → `vmr-requests.jsonl`，逐条记录一行 JSON（`export.go:375-417`）。
4. `report.WriteDetails(paths, dir, sess)` → 每条记录一个 `.md` + 一个 `.json`，外加一份 `vmr-requests-index.md`（`detail.go:56-335`）。

第二版方案里，**第 4 步（details 本身）完全不用动**——这是本轮反馈里最大的简化。原因见 §二。

---

## 二、这一轮反馈带来的三处简化

用户确认了三点，直接让方案变小：

**1. details 不分组，也不用软链接。** 每条 request 对应的 `.md`/`.json` 混在同一个 `details/` 目录里没有任何问题——本来就是按时间戳命名、不同调用方的文件天然不会重名。真正需要分组的只有"汇总"性质的两个文件：`vmr-requests.jsonl`（结构化导出）和 `vmr-requests-index.md`（索引）。这直接砍掉了第一版里最复杂的部分（`by-key/` 子目录、`ForKey`/`scoped` 机制、符号链接 vs 复制的取舍）——**`detail.go` 里逐条写文件的那段代码一行都不用改**。

**2. 只有两个文件要分组，不需要建子目录，加文件名后缀即可。** 也就是：
- `vmr-requests.jsonl` → 每个调用方额外产出一份 `vmr-requests-<tag>.jsonl`，跟原文件同目录。
- `vmr-requests-index.md` → 每个调用方额外产出一份 `vmr-requests-index-<tag>.md`，同目录。

不建目录还有一个好处：这些 sibling 文件里的相对链接前缀（指向 `details/xxx.md`）跟全量索引完全一样，不需要因为"深了一层"而重新计算相对路径。

**3. 不需要 config 里配"名字"，直接用 key 本身末尾几位字符当标签。** API key 是调用方自己配的字符串，怎么起没有约束——与其在 config 里再加一层"给 key 起名"的机制（第一版的 `api_keys: {name: key}` 映射），不如直接约定："如果你想让报告里的分组标签可读，就把 key 的结尾几位起得有意义"，vmr 只负责截取。这样：
- config 里的 `api_keys` 从"具名映射"简化成一个**纯字符串列表**。
- 完全不需要名字唯一性校验、字符集校验——用户自己决定 key 怎么起，vmr 不做二次抽象。
- 尾部长度从 `mask()` 现有的 4 位加长到 **6 位**（更可读），且**约定用连字符分隔**：例如 key 以 `...-alice` 结尾，取最后 6 个字符正好是 `-alice`（连字符 + 5 个字母），分组标签天然读作"alice"。`config.example.yaml` 里加一句提示即可，不需要任何代码校验这个约定——**约定是给人看的，不是给机器强制的**。

净效果：**不再需要"名字"这个概念**，`ClientKeyName` 改叫 `ClientKeyTag`（"标签"而非"名字"，因为它是从 key 派生的，不是用户显式声明的），配置项从 map 简化成 list，`cmd/vmr/main.go` 的改动量从"加一段循环"降到"几乎不用改"（见 §3.5）。

---

## 三、设计方案（已实现）

### 3.1 Config schema：`api_keys` 是纯字符串列表

```yaml
# api_key: sk-vmr-local-xxx        # 单把 key，事后统计不区分调用方（向后兼容，行为不变）

# api_keys:                        # 多把 key，事后 `vmr report` 自动按每把 key 派生的标签分组，
#                                   # 产出 vmr-requests-<tag>.jsonl / vmr-requests-index-<tag>.md。
#                                   # 标签取法：先取 key 末 6 个字符，若这 6 个字符里有横杠，只留
#                                   # 最后一个横杠之后的部分——所以建议每把 key 都以
#                                   # "-有意义的短语"结尾（如 ...-alice、...-openclaw、...-t2），
#                                   # 后缀不足 6 位也会被完整保留，不会被硬凑成 6 位；建议后缀
#                                   # 长度 ≥3-4 位，太短容易和别的 key 撞标签。
#                                   # 每把 key 至少 16 字符（太短会导致取到的窗口就是整把 key，
#                                   # 相当于把秘钥明文写进报告和文件名——vmr 会在启动时拒绝过短的 key）。
#   - ${VMR_KEY_ALICE}
#   - ${VMR_KEY_OPENCLAW}
```

- `APIKeys []string`（yaml: `api_keys`）——纯列表，没有名字字段，比第一版的 `map[string]string` 更简单。
- **`api_key` 和 `api_keys` 可以同时配置，互不冲突**：`api_key`（如果设置）永远是一把"万能钥匙"，鉴权通过但不打标签（`ClientKeyTag == ""`）；`api_keys` 里的每一把额外鉴权通过并打上对应标签。这样存量用户完全不用动 `api_key`，只需要在需要区分调用方时追加 `api_keys` 即可——比第一版"二选一，报错强制互斥"更宽松，也更少一条校验逻辑。
- 校验规则（`config.go` 的 `validate()` 里加，只有一条新规则）：
  - `api_keys` 里每一把的长度 ≥ 16 字符——这是唯一新增的校验，专门防止"key 本身就比 6 位标签还短，标签等于整把密钥明文"这种退化情况。这个下限本来就是对 bearer token 的合理起步要求，不算额外负担。
- **热重载天然免费获得**：`Config` 是整体原子替换（`router.BuildSnapshot` + `Router.Install`，`cmd/vmr/main.go:379-400`），新字段不需要额外接入热重载逻辑。

### 3.2 `audit` 包新增一个纯函数：从 key 值本身派生标签

第三版对提取规则做了一处改进：**不再是定长截取末 6 位，而是"先截末 6 位，再在这 6 位里找横杠，横杠之前的部分再切掉"**——这样一来，有意义的后缀不需要凑够 6 位，短于 6 位的后缀（比如 `-al`、`-abcd`）也能被完整保留，不会被定长截断切乱；反过来后缀长于 6 位时，横杠本身可能已经落在 6 位窗口之外，此时找不到横杠，就直接用这 6 位原始字符（详见下面函数注释里的例子）。

`internal/audit/audit.go` 新增：

```go
// keyTagLen bounds how many trailing characters of a matched api_keys
// entry ever become its ClientKeyTag. 6 (vs. mask()'s 4) trades a couple
// more characters of exposure for a label that reads as deliberate — see
// config.example.yaml's "end your key in -something-readable" convention.
// This is independent of mask()'s redaction length: mask() protects every
// credential header generically; KeyTag only ever runs on vmr's own
// api_keys entries, whose minimum length Config.validate already enforces
// specifically so this never exposes a whole key (see keyTagLen's config-
// side counterpart, the 16-char minimum).
const keyTagLen = 6

// KeyTag derives a short, non-secret label from one api_keys entry's tail —
// the caller-facing "who sent this" identity for report grouping.
//
// Rule: take the last keyTagLen raw characters first, then, if that window
// contains a hyphen, keep only what follows the LAST hyphen inside it —
// this lets a meaningful suffix shorter than keyTagLen (e.g. "-al", 2
// chars) survive intact instead of being padded with whatever unrelated
// characters preceded it in the fixed-length window. A suffix longer than
// keyTagLen simply loses its hyphen and everything before it once the
// window no longer reaches back that far — capped, never longer.
//
// Examples (keyTagLen = 6):
//   ...-alice   → window "-alice" → tag "alice"   (5 chars, hyphen at 0)
//   ...proj-al  → window "roj-al" → tag "al"       (2 chars, hyphen at 3)
//   ...X-abcd   → window "X-abcd" → tag "abcd"     (4 chars, hyphen at 1)
//   ...-abcdefgh→ window "cdefgh" → tag "cdefgh"    (hyphen is 8 chars back, outside the window — no hyphen found, whole window kept)
//   ...9k3f7a   → window "9k3f7a" → tag "9k3f7a"    (no hyphen anywhere — whole window kept)
//
// Assumes the key is ASCII (true for every real bearer-token format). A
// key shorter than keyTagLen is used whole as the window, then the same
// hyphen rule applies — which is why config validation rejects api_keys
// entries under 16 characters: short enough for the window itself to be
// the entire secret would otherwise leak it into every report and
// filename this tag ends up in.
func KeyTag(key string) string {
    window := key
    if len(key) > keyTagLen {
        window = key[len(key)-keyTagLen:]
    }
    // i+1 < len(window) excludes a hyphen that is itself the window's last
    // character (nothing follows it) — trimming there would produce an
    // empty tag, so the whole window is kept instead.
    if i := strings.LastIndexByte(window, '-'); i >= 0 && i+1 < len(window) {
        return window[i+1:]
    }
    return window
}
```

`Record` struct 加一个字段：

```go
// ClientKeyTag identifies which api_keys entry authenticated this request
// — the last keyTagLen characters of the matched key (see KeyTag), "" when
// auth is disabled, the request matched the catch-all Config.APIKey, or
// (vmr replay) the record wasn't produced by a live authenticated request.
ClientKeyTag string `json:"client_key_tag,omitempty"`
```

不需要动 `mask()`——它是给所有 credential header 通用打码的机制，跟这里"专门给 vmr 自己的 api_keys 派生标签"是两回事，范围不同，没必要因为这个特性去动一个更宽范围、跟别的上游 provider key 打码逻辑共用的函数。

### 3.3 鉴权：循环比较，命中哪把就记哪把的标签

`server.go`：

```go
// authenticate reports whether r carries a valid credential and, if it
// matched a specific api_keys entry, that entry's KeyTag ("" for the
// catch-all Config.APIKey, or when auth is disabled entirely).
func (s *Server) authenticate(r *http.Request) (tag string, ok bool) {
    cfg := s.rt.Snapshot().Cfg
    if cfg.APIKey == "" && len(cfg.APIKeys) == 0 {
        return "", true // 未配置任何 key：不鉴权，行为与今天一致
    }
    got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
    if got == "" {
        got = r.Header.Get("x-api-key")
    }
    if cfg.APIKey != "" && subtle.ConstantTimeCompare([]byte(got), []byte(cfg.APIKey)) == 1 {
        return "", true // 万能钥匙命中：鉴权通过，不打标签
    }
    for _, key := range cfg.APIKeys {
        if subtle.ConstantTimeCompare([]byte(got), []byte(key)) == 1 {
            return audit.KeyTag(key), true
        }
    }
    return "", false
}

func (s *Server) checkAuth(r *http.Request) bool {
    _, ok := s.authenticate(r)
    return ok
}
```

> ⚠️ 上面是**第三版**的形态，为方便对照历史演进仍保留在这里，**不代表当前代码**。第四版把"提取 `got`"这一步挪到了"未配置任何 key"判断**之前**，并把该分支的返回值从 `return "", true` 改成 `return audit.KeyTag(got), true`——与 `internal/server/server.go` 一致的最终版本见 §6.3。

- 逐把常数时间比较：调用方数量是个位数到十位数级别（团队规模决定），逐把比较不构成实际的时间侧信道风险。
- `GET /v1/models`、`/admin/status` 继续用 `checkAuth`；`chatHandler` 改用 `authenticate`，把 `tag` 塞进 `audit.Record`：

```go
tag, authed := s.authenticate(r)
if rec != nil {
    rec.ClientKeyTag = tag
}
if !authed {
    writeError(...)
    return
}
```

`rec` 的构造和 `defer` 写盘早在鉴权之前就发生了（`server.go:122-144`），这个顺序不变——鉴权失败的请求依然被完整审计，只是打不上标签（`tag == ""`，因为没有任何一把命中）。

### 3.4 report/session 管线：加一个字段，`WriteRequests`/`WriteDetails` 内部各加一小段"顺手多写几个文件"的逻辑

**`session.go`**：`ReqInfo` 加一个字段，`collect()` 里赋值一行：

```go
type ReqInfo struct {
    ...
    ClientKeyTag string // "" = 未打标签（未鉴权 / 命中万能钥匙 / replay 记录）
    ...
}
```
```go
r.ClientKeyTag = rec.ClientKeyTag // collect() 里，读 rec 的地方顺手加一行
```

**`export.go` 的 `WriteRequests`**：把写文件的循环体拆成一个小函数，先按全量调一次（今天的行为），再按 `ClientKeyTag` 分组各调一次，`ClientKeyTag == ""` 的记录不参与分组（它们只出现在全量文件里）：

```go
func WriteRequests(a *SessionAnalysis, path string) (int, error) {
    if err := writeRequestRows(a.Recs, path); err != nil {
        return 0, err
    }
    byTag := map[string][]*ReqInfo{}
    for _, r := range a.Recs {
        if r.ClientKeyTag != "" {
            byTag[r.ClientKeyTag] = append(byTag[r.ClientKeyTag], r)
        }
    }
    dir := filepath.Dir(path)
    base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
    for tag, recs := range byTag {
        p := filepath.Join(dir, fmt.Sprintf("%s-%s.jsonl", base, sanitizeName(tag)))
        if err := writeRequestRows(recs, p); err != nil {
            return 0, fmt.Errorf("client key %q: %w", tag, err)
        }
    }
    return len(a.Recs), nil
}

// writeRequestRows is WriteRequests' encode loop, parameterized over which
// records to write — called once for the full set, once more per observed
// ClientKeyTag.
func writeRequestRows(recs []*ReqInfo, path string) error { /* 今天 WriteRequests 里那段 for _, r := range a.Recs { ... } 的循环体，原样搬过来 */ }
```

`recs` 沿用 `a.Recs` 的时间序（`AnalyzeSessions` 里已经 `sort.SliceStable` 过），过滤不改变顺序，所以每个按 tag 拆出来的子文件也天然是时间序——不需要重新排序。`sanitizeName` 是 `detail.go` 里已有的文件名安全字符过滤（`detail.go:479-482`），同包直接复用，不需要新写。

**`detail.go` 的 `WriteDetails`**：把"生成 `vmr-requests-index.md`"这段尾巴（今天从 `var b strings.Builder` 到写文件为止，`detail.go` 约 149-333 行）抽成一个纯函数 `renderIndex(entries []indexEntry, sess *SessionAnalysis) string`，`WriteDetails` 本身（扫文件、写 `.md`/`.json`、收集 `entries`）**一行不改**；只在原来写全量索引之后，多加一小段按 tag 过滤后再调用 `renderIndex`：

```go
// WriteDetails 原有逻辑到写完全量 vmr-requests-index.md 为止，完全不变。

byTag := map[string]bool{}
for _, e := range entries {
    if e.info != nil && e.info.ClientKeyTag != "" {
        byTag[e.info.ClientKeyTag] = true
    }
}
for tag := range byTag {
    var fEntries []indexEntry
    for _, e := range entries {
        if e.info != nil && e.info.ClientKeyTag == tag {
            fEntries = append(fEntries, e)
        }
    }
    tagIndexPath := filepath.Join(filepath.Dir(dir), fmt.Sprintf("vmr-requests-index-%s.md", sanitizeName(tag)))
    if err := os.WriteFile(tagIndexPath, []byte(renderIndex(fEntries, filterSessByTag(sess, tag))), 0o644); err != nil {
        return len(entries), fmt.Errorf("client key %q: %w", tag, err)
    }
}
return len(entries), nil
```

`filterSessByTag` 是一个新的小的、包内私有的辅助函数，只为 `renderIndex` 里"按 Chat User → Session → Task 分组"那部分渲染逻辑构造一个按 tag 过滤过的 `Sessions`/`Compactions`/`Ungrouped` 视图（**不碰 `Recs`，因为 details 文件本身不分组，`renderIndex` 也不需要靠 `Recs` 定位文件——文件已经在上面的主循环里写好了**）：

```go
func filterSessByTag(sess *SessionAnalysis, tag string) *SessionAnalysis {
    if sess == nil {
        return nil
    }
    out := &SessionAnalysis{}
    for _, s := range sess.Sessions {
        if len(s.Recs) > 0 && s.Recs[0].ClientKeyTag == tag {
            out.Sessions = append(out.Sessions, s)
        }
    }
    for _, c := range sess.Compactions {
        if c.ClientKeyTag == tag {
            out.Compactions = append(out.Compactions, c)
        }
    }
    for _, u := range sess.Ungrouped {
        if u.ClientKeyTag == tag {
            out.Ungrouped = append(out.Ungrouped, u)
        }
    }
    return out
}
```

关键点：`vmr-requests-index-<tag>.md` 和全量的 `vmr-requests-index.md` 落在**同一目录**（`filepath.Dir(dir)`），所以两者对 `details/xxx.md` 的相对链接前缀完全一样，`fileLinksCell`（`detail.go:410-413`）等渲染辅助函数不需要因为"多了一层目录"而改一个字符。这正是"不建子目录、只加文件名后缀"这条反馈带来的直接好处。

### 3.5 cmd/vmr：**不需要改**

因为分组逻辑完全内置到 `WriteRequests` 和 `WriteDetails` 内部（观测到某个非空 `ClientKeyTag` 就自动多写一对 sibling 文件），`cmd/vmr/main.go` 的 `cmdReport` 调用这两个函数的方式和今天完全一样，不需要新增循环、不需要新 flag。唯一可选的小改动是把打印的汇总行加一句"发现 N 个调用方标签"，纯提示性质，不影响主逻辑，可做可不做。

### 3.6 触发条件：全自动，零配置

维持第一版已经定下的结论——不加新 CLI flag。规则简化为：**每观测到一个非空 `ClientKeyTag`，就为它多写一对 sibling 文件；一个都没观测到（没配 `api_keys`，或全部走万能钥匙/无鉴权），sibling 文件一个都不产生，输出与今天字节级一致**。不再需要"至少 2 种"这样的阈值判断——单独配了一把具名 key 也是合理场景（比如想把"我自己手工测试的流量"和"某个具名调用方的流量"分开看），没有理由为此设阈值。

### 3.7 输出结果（最终形态）

```
reports/                          ← 或用户 -o 指定的目录（不受本次改动影响）
  vmr-report.json                  ← 全量汇总，字节级不受影响
  vmr-report.md                    ← 全量汇总，同上
  vmr-requests.jsonl               ← 全量导出，不变
  vmr-requests-index.md            ← 全量索引，不变
  vmr-requests-alice.jsonl         ← 新增：只含 tag=alice 的记录
  vmr-requests-index-alice.md      ← 新增：只含 tag=alice 的分组视图（Chat User/Session/Task）
  vmr-requests-openclaw.jsonl      ← 新增：另一个 tag
  vmr-requests-index-openclaw.md
  details/                         ← 完全不变，仍是全量、混放、按时间戳命名
    2026...ok.md / .json
```

---

## 四、改动面清单（已落地）

| 文件 | 改动性质 | 说明 |
|---|---|---|
| `internal/config/config.go` | 增量 | `Config` 加 `APIKeys []string`；新增 `minAPIKeyLen = 16` 常量；`validate()` 加一条长度检查 |
| `internal/audit/audit.go` | 增量 | 新增 `keyTagLen = 6` 常量 + `KeyTag()` 纯函数（挂在 `mask()` 旁边，两者互不影响）；`Record` 加 `ClientKeyTag` 字段 |
| `internal/server/server.go` | 小改 | `checkAuth` 拆出 `authenticate`（返回 tag + ok）；`chatHandler` 记录匹配到的 tag 到 `rec.ClientKeyTag`；第四版追加：未配置任何 key 时也对客户端自报的值调用 `KeyTag` 打标签（§六） |
| `internal/report/session.go` | 增量 | `ReqInfo` 加 `ClientKeyTag` 字段；`collect()` 加一行赋值 |
| `internal/report/export.go` | 小改 | `requestRow` 加 `ClientKey`（json: `client_key_tag`）字段；`WriteRequests` 拆出 `writeRequestRows`，本体调用一次全量 + 每个非空 tag 各调一次写 sibling |
| `internal/report/detail.go` | 小改 | `indexEntry` 类型提升到包级（供 `renderIndex` 使用）；从 `WriteDetails` 尾部抽出纯函数 `renderIndex`；新增 `filterSessByTag`；`WriteDetails` 主体（扫文件、写 `.md`/`.json`）**未改一行** |
| `cmd/vmr/main.go` | **未改** | 分组逻辑完全内置在 `WriteRequests`/`WriteDetails` 内部，调用方式不变 |
| `config.example.yaml` / `README.md` / `README.zh.md` / `docs/VirtualModelRouter_v2_Fable5.md` | 文档 | 补 `api_keys` 说明、tag 派生规则示例、与本设计文档的互链 |

相比第一版，`by-key/` 目录、`ForKey`/`ClientKeyNames`/`scoped` 这套机制、符号链接 vs 复制的取舍，全部不再需要——改动面明显更小，且 `cmd/vmr/main.go` 完全不用改。不涉及路由/failover/重试/健康检查逻辑，不涉及 `Report`/`Format` 版本号。实测改动约 90 行核心代码 + 约 260 行测试（`internal/audit/audit_test.go` 的 `TestKeyTag` 十个用例、`internal/config/config_test.go` 的两个新用例、`internal/server/server_test.go` 的 `TestRouterAuthMultiKeyTagsRequests`、`internal/report/session_test.go`/`detail_test.go` 的四个新用例），验证了每个环节的边界情况与向后兼容性（无 `api_keys` 时输出与改动前字节级一致）。

---

## 五、这一版仍然值得明确写下来的取舍（非"待拍板"，仅供知情）

1. **标签碰撞是理论上可能的，且第三版的变长规则让这个风险跟"后缀起多短"直接挂钩**：`KeyTag` 现在产出的标签长度可变（1-6 字符），后缀起得越短，碰撞空间越小——极端情况下起一个 1 字符后缀（如 `...-a`），撞车概率显著高于 6 字符定长窗口。`config.example.yaml` 里会建议后缀至少 3-4 个字符（不做代码强制，理由同 §二第 3 点：约定是给人看的）。两把 key 一旦标签撞了，它们的流量会被合并进同一个 sibling 文件、无法区分——这是"不维护额外名字映射"换来的必然代价，值得记录而不是假装不存在。
2. **16 字符下限只作用于 `api_keys`，不回溯校验存量的 `api_key`**：单把"万能钥匙"不产生任何标签，也就没有"标签等于整把密钥"的风险，因此没必要强迫存量配置也满足这个下限——新校验规则完全不影响任何现有配置文件。
3. **`vmr replay` 产生的记录 `ClientKeyTag` 恒为空**：重放的请求不经过 `authenticate`，这和它已经有独立的 `ReplayOf` 标记一样，是"这不是一次真实鉴权过的实时请求"的题中之义，不需要特殊处理。

以上三点都是设计已经吸收、不需要你再决定的细节，列出来是为了这份文档本身站得住脚，而不是遗留"没想过"的空白。§四 清单已经全部落地并通过测试（`go build ./... && go vet ./... && go test ./...` 全绿）。

---

## 六、第四版：不配置任何 key 时，客户端自报家门

### 6.1 用户提出的场景

纯私有网络（比如内网、单机、几个人共用），本来就不需要真正的访问控制——`api_key`/`api_keys` 都不打算配置。但仍然想要"按调用方分组统计"这个能力。用户的想法：既然客户端本来就要在 `Authorization`/`x-api-key` 里填一个值（多数 SDK 要求这个字段非空，哪怕 vmr 不校验），那就让每个客户端自己在这个值的末尾带上有意义的后缀，vmr 侧不用配置任何 key 列表就能按这个后缀分组。

### 6.2 可行性：成立，且改动量极小

**关键事实**：`api_key`/`api_keys` 都未配置时，`authenticate()` 今天是直接短路返回 `"", true`，**根本不读** `Authorization`/`x-api-key`——"读取但不校验"这件事，不是做不到，只是原来没做。而 `KeyTag()` 是一个纯函数，不关心输入字符串是不是配置里"真的"存在的 key——任意字符串都能提取出后缀标签。把这两点接起来，思路自然成立。

逐项检查是否有问题：

- **不增加攻击面**：这个模式下门本来就是全开的（今天已经是——任何请求都会被接受），现在只是"顺手读一下客户端发来的字符串提取标签"，不影响谁能不能进来，也不改变鉴权判定结果。
- **完全向后兼容**：不发任何凭证头的客户端（多数纯内网部署的现状）——`got` 是空字符串，`KeyTag("")` 返回空字符串，跟改动前字节级一致。
- **不需要新增 config 字段**：触发条件就是"`api_key` 和 `api_keys` 都没配置"这个已有判断，不用引入新开关，反而是用户所说的"进一步简化了 config.yaml"的体现——不配置任何东西反而解锁了这个能力。
- **不需要动 `KeyTag()`**：函数完全复用，调用方只是从"一定是配置里的某把 key"变成"客户端声称的任意字符串"。

**需要明确写下来的一点**：这个模式下发的字符串**不再有 §3.1 的 16 字符下限校验**——因为它压根不是一把需要保护的密钥，没有"泄漏密钥"这个风险维度可言了。客户端完全可以直接发 `Authorization: Bearer alice` 这种短字符串，`KeyTag` 会原样返回 `"alice"`。这不是疏漏，是这个模式下"密钥"和"标签"这两个概念直接合一了——没有校验的必要，也没有校验的对象。

### 6.3 实现

`internal/server/server.go` 的 `authenticate()`：把"提取 `got`"这一步挪到"没配任何 key"判断之前，该分支从 `return "", true` 改成 `return audit.KeyTag(got), true`：

```go
func (s *Server) authenticate(r *http.Request) (tag string, ok bool) {
	cfg := s.rt.Snapshot().Cfg
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		got = r.Header.Get("x-api-key")
	}
	if cfg.APIKey == "" && len(cfg.APIKeys) == 0 {
		return audit.KeyTag(got), true // 未配置任何 key：门依旧全开，只是顺手打个自报标签
	}
	if cfg.APIKey != "" && subtle.ConstantTimeCompare([]byte(got), []byte(cfg.APIKey)) == 1 {
		return "", true
	}
	for _, key := range cfg.APIKeys {
		if subtle.ConstantTimeCompare([]byte(got), []byte(key)) == 1 {
			return audit.KeyTag(key), true
		}
	}
	return "", false
}
```

零新增字段、零新增函数、`checkAuth`/`chatHandler`/report 侧全部不用动——`ClientKeyTag` 已经是贯穿全链路的字段，这里只是多了一个"谁来填它"的来源。改动严格局限于 `authenticate()` 内部顺序调整，`internal/server/server_test.go` 新增 `TestNoAuthConfiguredSelfDeclaredTag` 验证：自报值被正确打标签、不发任何凭证的请求仍然是空标签且 200。

### 6.4 与已配置模式的边界

明确一点，避免以后混淆：**这个"自报家门"路径只在 `api_key` 和 `api_keys` 都未配置时生效**。一旦配置了任何一把 key，行为完全回到 §三 的规则——不匹配任何已配置 key 的请求仍然是 401 + 不打标签，不会因为"顺手提取"逻辑而放行或打上一个未经校验的标签。两种模式（"有鉴权，按配置的 key 分组"和"无鉴权，按客户端自报的字符串分组"）互斥，不会叠加出奇怪的中间状态。
