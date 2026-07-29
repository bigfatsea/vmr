// Ver 2026-07-30 21:00, by Sonnet 5

// Step 4's 4a module, scoped to the -compare report (design doc Appendix
// C.6/G; full plan and rationale in `_tmp/plan_sonnet-5.md`): an optional,
// always-degradable LLM interpretation layer appended to a Comparison's
// rendered Markdown. Every constraint here traces back to the design doc's
// three-layer principle (§3.3/C.7): the LLM only ever receives numbers this
// package already computed (Comparison/ComparisonExtras), never generates a
// new number itself, and its output is rendered as a clearly-labeled,
// separately-cached block — never mixed into the fact-layer sections
// RenderComparisonMarkdown produces.
//
// Endpoint resolution is deliberately the single simplest option the plan
// review settled on: a manually-supplied "host:port" for an already-running
// VMR instance (LLMOptions.Addr) plus that instance's own virtual model name
// (LLMOptions.Model) — no config.yaml provider/model resolution, no
// failover, no auto-launched process. This package never depends on
// internal/router or internal/adapter for it: a plain net/http POST to
// "http://{addr}/v1/chat/completions" is the entire client.
package story

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// promptVersion is bumped whenever the prompt template or the evidence
// pack's shape changes materially — part of the disk-cache key (design doc
// §5.5's "(step_hash, model, prompt_version)" convention, adapted here to
// "(both journey ids + evidence pack, model, prompt_version)" since this
// layer operates on a pair of Journeys, not a single step).
const promptVersion = "compare-llm-v2"

// llmHTTPTimeout is deliberately generous (not the 30s a simple echo call
// would need): the evidence pack can carry a several-thousand-character
// system-prompt excerpt and a deliverable excerpt, and the model behind
// -llm-addr is whatever the user pointed it at — some (especially reasoning
// models) take a while. A slow real answer is better than a fast spurious
// timeout that throws away a paid-for call and falls back to "no LLM
// section" for no good reason.
const llmHTTPTimeout = 120 * time.Second

// ToolIndexEntry is one Step's compact, rule-generated summary — seq, which
// tools it called, and a one-line brief — fed to the LLM so it can narrate
// "what phases did this Journey go through" itself (design doc plan review:
// phase classification needs semantic judgment, which the LLM is better and
// more honest at than a maintained if/else threshold ladder). Nothing here
// is a judgment call; it's the same data journey-<id>.md already renders,
// just condensed to one line per Step instead of the full body.
type ToolIndexEntry struct {
	Seq   int      `json:"seq"`
	Tools []string `json:"tools,omitempty"`
	Brief string   `json:"brief"`
}

// buildToolIndex condenses j's Steps into ToolIndexEntry rows.
func buildToolIndex(j *Journey) []ToolIndexEntry {
	steps := journeySteps(j)
	out := make([]ToolIndexEntry, 0, len(steps))
	for _, s := range steps {
		var tools []string
		for _, tc := range s.ToolCalls {
			tools = append(tools, tc.Name)
		}
		brief := preview(s.RespText)
		if brief == "" {
			brief = "(无文本回复)"
		}
		out = append(out, ToolIndexEntry{Seq: s.Seq, Tools: tools, Brief: brief})
	}
	return out
}

// EvidencePack is the entire LLM input for the interpretation layer — the
// already-computed Comparison (Rows/Tools/Extras: every number the model is
// allowed to cite) plus each side's task-title list and per-step tool index
// (text the model is explicitly told is its own reading, not a verified
// fact). Building this from two full Journeys, not just their
// JourneySummary, is what makes the tool index and Extras possible; nothing
// here re-fetches from disk — it's all already in memory by the time
// compareJourneys calls this.
type EvidencePack struct {
	Comparison  Comparison       `json:"comparison"`
	TaskTitlesA []string         `json:"task_titles_a"`
	TaskTitlesB []string         `json:"task_titles_b"`
	ToolIndexA  []ToolIndexEntry `json:"tool_index_a"`
	ToolIndexB  []ToolIndexEntry `json:"tool_index_b"`
}

// BuildEvidencePack assembles jA/jB/cmp into the bounded evidence pack the
// LLM prompt embeds — cmp must already carry a non-nil Extras (the caller
// is expected to have called ComputeComparisonExtras first; this function
// doesn't compute it itself so callers who only want the tool index/task
// titles without paying for Extras twice can reuse an already-built cmp).
func BuildEvidencePack(jA, jB *Journey, cmp Comparison) EvidencePack {
	return EvidencePack{
		Comparison:  cmp,
		TaskTitlesA: journeyTaskTitles(jA),
		TaskTitlesB: journeyTaskTitles(jB),
		ToolIndexA:  buildToolIndex(jA),
		ToolIndexB:  buildToolIndex(jB),
	}
}

// journeyTaskTitles lists j's Tasks' titles in order — named distinctly from
// journey_test.go's own same-purpose taskTitles test helper to avoid a
// redeclaration conflict when the test binary links both files.
func journeyTaskTitles(j *Journey) []string {
	out := make([]string, len(j.Tasks))
	for i, t := range j.Tasks {
		out[i] = t.Title
	}
	return out
}

// EstimateChars returns the evidence pack's serialized JSON size in
// characters (Unicode code points, not bytes — an evidence pack full of
// Chinese task titles/excerpts would otherwise report ~3x its actual
// character count) — the -llm-dry-run size estimate (`_tmp/plan_sonnet-5.md`
// §3.4). Deliberately not a token count: estimating tokens accurately would
// need a tokenizer matched to whatever model -llm-addr resolves to, which
// this package has no way to know in advance — chars/4 is close enough for
// "does this look reasonable before I spend money on it".
func (p EvidencePack) EstimateChars() int {
	data, err := json.Marshal(p)
	if err != nil {
		return 0
	}
	return utf8.RuneCount(data)
}

// llmSystemPrompt is the fixed instruction set every call sends — the
// three-layer boundary (design doc §3.3/C.7) spelled out as constraints a
// model actually follows: numbers are off-limits to invent, prose is
// explicitly allowed to be the model's own reading as long as it says so,
// and blind spots must be named rather than glossed over.
//
// v2 (`docs/Step4a_compare_LLM解读层_差距分析与改进建议_2026-07-30_sonnet-5.md`
// §6.2 items 1/3, tier-1 adopted): two changes from v1, both prompt-only —
//  1. Point 2 now spells out the excerpt-boundary caveat explicitly, not just
//     generically: an "absence" claim ("this tool/file wasn't mentioned")
//     must be qualified as "absent from the excerpt", never asserted as
//     "doesn't exist" — the concrete failure mode a prior real run hit was
//     giving a confident, "evidence-supported"-tagged conclusion built on a
//     window that happened to cut off before the decisive evidence.
//  2. Point 3 replaces the old flat "assumption list, each tagged supported/
//     speculative" with a confidence-tiered table (high/medium/low) plus one
//     causal-chain sentence — structure, not new license to assert. The
//     tiering rule is deliberately mechanical (must point at a specific
//     evidence anchor to claim "high") so this doesn't become an invitation
//     to sound more certain than the underlying evidence supports — a human-
//     authored reference report's confident tone partly comes from asserting
//     correlation as causation, and that's a trade-off to NOT copy, not a
//     gap to close.
const llmSystemPrompt = `你是一个 Agent 任务执行对比分析助手。你会收到两个 Agent Journey（同一任务的两次不同执行）的结构化对比数据（JSON），包括已经算好的行为剖面指标、工具调用分布、端点/缓存/system prompt 规模等规则事实，以及两段有边界的原文节选（system prompt 节选、可能的最终交付物节选）和逐轮工具调用索引。

严格遵循：
1. 数字只能引用给定 JSON 里已经算好的数值，禁止编造或推算新的数字、百分比、次数。
2. 原文节选（system prompt、最终交付物、逐轮工具索引里的 brief）是未经规则解析的文本证据——你可以基于阅读理解对它们做出判断（比如"看起来加载了哪些上下文文件""大致分几个阶段"），但必须明确说明这是你的阅读理解，不是规则核实的事实。节选是从原文开头起截断的一段前缀，截断点之后的内容你看不到——如果某个判断依赖"节选里没出现"这件事（比如"某个工具/某份文件没有被提到"），必须在该判断旁边明确写"仅代表节选范围内未出现，不能排除节选之外仍存在"，不能把"节选里没看到"直接当成"确实不存在"。
3. 核心假设用一张表输出，列为：候选根因 | 直接证据 | 置信度 | 改进建议。置信度只能填"高"/"中"/"低"三档之一：能在给定 JSON 或原文节选里指认出至少一条直接支持的具体证据才标"高"；只有间接证据、需要你自己推断关联的标"中"；仅凭排除法或直觉、没有明确证据锚点的标"低"。低置信度的候选也应该列出（让读者看到"想到了但证据不足"本身就有价值），但必须诚实标低，不能为了让结论显得更确定而拔高置信度，也不能因为不确定就干脆不提。"高"或"中"置信度的候选之后可以附最多 1-2 条可执行的改进建议，"低"置信度的不需要附。表格之后另起一行，用一句话给出你认为最主要的因果链（例如"A → B → C"的箭头形式）——这一句是你的归纳，不是新增事实，不需要额外标注。
4. 必须专门列一段"VMR 看不到什么"——如宿主 Agent 自身的配置（如是否有类似 loop detection 的机制、工具白名单的来源等），这些不在给定证据里，如实说明是盲区，不要编造。
5. 不要预设某种特定的结论模板（比如"一定是某个配置文件的差异"），根因判断完全基于这次给定的实际证据。
6. 输出为 Markdown 正文，不要输出 JSON。第一行是一句话结论，然后依次是"## 候选根因"（含第 3 条要求的表格 + 一句话因果链）、"## 工作方式与阶段解读"、"## VMR 看不到什么"三个小节。`

// buildUserPrompt embeds the evidence pack as pretty-printed JSON — pretty
// rather than compact on purpose: this is a debugging aid too (the exact
// prompt sent is what a human reads if they open the cache file), and the
// token-cost difference against a several-thousand-char excerpt is noise.
func buildUserPrompt(pack EvidencePack) (string, error) {
	data, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal evidence pack: %w", err)
	}
	return "以下是两个 Journey 的结构化对比数据：\n\n```json\n" + string(data) + "\n```\n", nil
}

// LLMOptions configures the interpretation layer's one HTTP call — see this
// file's package-level doc comment for why there is exactly one supported
// endpoint shape (a manually-run VMR instance, OpenAI protocol only).
type LLMOptions struct {
	Addr     string // "host:port" (or a full "http://..." base) of a running VMR instance; "" disables this layer entirely
	Model    string // that instance's virtual model name, sent verbatim as "model" — no rewriting
	APIKey   string // optional bearer token, only meaningful if that instance has api_keys configured
	CacheDir string // directory for the disk cache (0700); "" disables caching
}

// Enabled reports whether opts actually turns the interpretation layer on —
// Addr is the sole switch (`_tmp/plan_sonnet-5.md` §4: no separate -llm
// bool flag).
func (o LLMOptions) Enabled() bool { return o.Addr != "" }

// chatURL resolves opts.Addr to the full chat-completions URL. A bare
// "host:port" (the documented, expected form) becomes "http://host:port/v1
// /chat/completions"; a value that already carries a scheme is used as
// given with only the path appended, so a caller who for some reason has
// "https://..." handy isn't forced to strip it first. A trailing "/v1" is
// also stripped before appending — someone used to typing an OpenAI-style
// base URL (host:port/v1) would otherwise get a doubled "/v1/v1/..." path
// and a confusing 404 instead of the documented "host:port" form working.
func (o LLMOptions) chatURL() string {
	base := o.Addr
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	base = strings.TrimRight(base, "/")
	base = strings.TrimSuffix(base, "/v1")
	return base + "/v1/chat/completions"
}

// cacheKey hashes everything that should invalidate a cached answer: the
// prompt template version, the model actually being asked (so switching
// -llm-model never silently reuses another model's cached prose — the
// concrete bug the plan review flagged in the alternative proposal), and
// the evidence pack's own content (so a re-run against updated Journeys, or
// a different pair of ids, never collides).
func cacheKey(model string, pack EvidencePack) (string, error) {
	data, err := json.Marshal(pack)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte(promptVersion))
	h.Write([]byte{0})
	h.Write([]byte(model))
	h.Write([]byte{0})
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func cachePath(dir, key string) string {
	return filepath.Join(dir, key+".md")
}

// chatCompletionRequest/Response are the minimal OpenAI chat-completions
// shapes this client needs — not a general-purpose client, just enough to
// send one non-streaming request and read back one message's content.
type chatCompletionRequest struct {
	Model    string          `json:"model"`
	Stream   bool            `json:"stream"`
	Messages []chatMsgSimple `json:"messages"`
}

type chatMsgSimple struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// callLLM sends one non-streaming chat-completions request to opts' address
// and returns the assistant message's content. No retry, no failover, no
// health check — a single best-effort call; the caller decides what "it
// failed" means for the rest of the report (design doc C.7: the whole layer
// degrades away, it never fails the command).
func callLLM(ctx context.Context, opts LLMOptions, userPrompt string) (string, error) {
	reqBody := chatCompletionRequest{
		Model:  opts.Model,
		Stream: false,
		Messages: []chatMsgSimple{
			{Role: "system", Content: llmSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.chatURL(), bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if opts.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+opts.APIKey)
	}

	client := &http.Client{Timeout: llmHTTPTimeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("request to %s: %w", opts.chatURL(), err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s returned %d: %s", opts.chatURL(), resp.StatusCode, truncateForError(body))
	}

	var out chatCompletionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(out.Choices) == 0 || out.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("%s returned no message content", opts.chatURL())
	}
	return out.Choices[0].Message.Content, nil
}

func truncateForError(b []byte) string {
	const max = 500
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}

// InterpretResult is Interpret's outcome — Text is "" whenever Err != nil,
// so a caller can always check Err first without inspecting Text.
type InterpretResult struct {
	Text   string
	Cached bool
}

// Interpret runs the interpretation layer for one evidence pack: check the
// disk cache, call the LLM on a miss, write the cache on success. Any
// failure (cache I/O aside — a cache miss is not a failure) is returned as
// an error and the caller is expected to treat it as "no LLM section this
// run", per design doc C.7 — this function itself never panics or retries.
func Interpret(ctx context.Context, opts LLMOptions, pack EvidencePack) (InterpretResult, error) {
	if !opts.Enabled() {
		return InterpretResult{}, fmt.Errorf("LLM interpretation layer not enabled (no -llm-addr)")
	}

	var key string
	if opts.CacheDir != "" {
		k, err := cacheKey(opts.Model, pack)
		if err == nil {
			key = k
			if data, err := os.ReadFile(cachePath(opts.CacheDir, key)); err == nil {
				return InterpretResult{Text: string(data), Cached: true}, nil
			}
		}
	}

	userPrompt, err := buildUserPrompt(pack)
	if err != nil {
		return InterpretResult{}, err
	}
	text, err := callLLM(ctx, opts, userPrompt)
	if err != nil {
		return InterpretResult{}, err
	}

	// Both failure modes here are fail-open by design (the disk cache is an
	// optimization, not a requirement — design doc §3.4) but are kept as two
	// distinct, separately-named steps rather than one collapsed `if err ==
	// nil { ... }`: a directory-creation failure (permissions, disk full) and
	// a single-file write failure are different problems, and keeping them
	// apart in the code is what makes it possible to add a diagnostic to
	// just one of them later without re-deriving which branch is which.
	if key != "" {
		mkdirErr := os.MkdirAll(opts.CacheDir, 0o700)
		if mkdirErr == nil {
			_ = os.WriteFile(cachePath(opts.CacheDir, key), []byte(text), 0o600) // write failure: also fail-open, same reasoning
		}
	}
	return InterpretResult{Text: text, Cached: false}, nil
}

// RenderLLMSection wraps res.Text (or, on failure, a short explanatory note)
// with the "this is interpretation, not fact" banner design doc §3.3
// requires — always rendered as its own clearly separated section, never
// blended into the fact-layer sections above it.
func RenderLLMSection(opts LLMOptions, res InterpretResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## LLM 解读（模型：%s）\n\n", opts.Model)
	fmt.Fprintf(&b, "> 以下内容由 `%s` 生成的解读，不是事实层，可能有误，请对照上面的证据表核实。\n", opts.Model)
	if res.Cached {
		fmt.Fprintf(&b, "> (命中缓存，未重新调用)\n")
	}
	b.WriteString("\n")
	b.WriteString(res.Text)
	b.WriteString("\n")
	return b.String()
}
