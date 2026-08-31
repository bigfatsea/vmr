// Ver 2026-08-28, by Sonnet 5

// Pairs with internal/story/render_html.go + render_html_dashboard.go — the
// single-page HTML journey dashboard (`vmr analyze -journey <id> -html`).
// Fixed UI strings only; every number and every piece of conversation
// content comes from the Journey itself (or, under -redact, a length
// placeholder or a bare count).
package i18n

import "fmt"

// StoryHTMLText is the journey dashboard's chrome, in one language.
type StoryHTMLText struct {
	DocTitle       func(id string) string
	Subtitle       func(tasks, steps int, from, to string) string
	PartialBanner  string
	BreakBanner    func(kind string) string
	RedactedBanner string
	GeneratedNote  string
	Empty          string
	RedactedText   func(runes int) string

	// Incident-report front page: recorder bar + PROBABLE CAUSE verdict
	// panel + point-of-no-return strip.
	RecorderBar          string
	VerdictProbableCause string
	VerdictStamp         func(level string) string // level ∈ critical|warning|clean
	VerdictClean         string
	VerdictRedacted      func(code string, step int) string
	DamageLine           func(steps int, dur, tokens string) string
	DamageCost           func(amount string) string
	PointOfNoReturnHead  func(step int, ts string) string
	PONRCompaction       func(before, after string, dropped int) string
	PONRRetry            string
	PONRContract         string
	NoPointOfNoReturn    string
	// DiffusePointOfNoReturn replaces NoPointOfNoReturn when the verdict is
	// critical but no single structural signal (compaction / contract /
	// retry loop) marks the turn — a behavioral failure (loop, goal drift,
	// oscillation) has no one decisive step, and a "no turning point" line
	// would contradict the CRITICAL stamp.
	DiffusePointOfNoReturn string

	// Verdict (first screen). OutcomeDeliverable is a prefix — the renderer
	// appends the tool name as <code> so it reads as code, not backticks.
	OutcomeLabel       string
	OutcomeDeliverable func(step int) string
	OutcomeTermination func(finish string) string
	OutcomeUnknown     string

	// Section headings + left-rail labels
	SectionStructure string
	SectionMetrics   string
	SectionFindings  string
	TaskLabel        func(n int) string
	StepLabel        func(n int) string

	// Structure timeline
	StructNoSteps   string
	MoreTools       func(n int) string // "+3" overflow chip
	Attempts        func(n int) string
	FailoverBadge   string
	SysChanged      string
	EditLabel       func(kind string) string
	StitchLabel     func(kind string, score, confidence string) string
	CompactionShort func(before, after int64) string
	EntityCounts    func(swallowed, survived int) string // redact-mode compaction summary
	Swallowed       string
	Survived        string
	Instruction     string
	StepSaid        string
	NoReply         string

	// Metrics
	MetricNetTime       string
	MetricModelTime     string
	MetricAgentTime     string
	MetricTokensIn      string
	MetricTokensCached  string
	MetricTokensOut     string
	MetricToolCalls     string
	MetricCompactions   string
	MetricDupActionRate string
	MetricOutputRepeat  string
	MetricErrorRecovery string
	MetricPlanExecRatio string
	MetricModelSwitches string
	MetricContextUtil   string
	SparkContextTitle   string
	SparkContextCaption func(firstTok, lastTok int64) string

	// Findings
	FindingSourceRule    string
	FindingSourceLLM     string
	FindingConfidence    func(level string) string
	FindingAtStep        func(seq int) string
	FindingRelatedSteps  func(seqs string) string
	FindingEvidenceLabel string
	FindingActionLabel   string
	NoFindings           string
	RedactedFindingsNote string
	// AnthropicCoverageNote is the protocol-blind-spot disclosure the
	// Markdown Findings section already carries (spine.AnthropicOnlyCoverageNote)
	// — shown here too so the shareable dashboard is not more confident than
	// the .md it mirrors (问题 10). codes is the affected detector/metric list.
	AnthropicCoverageNote func(codes string) string
}

// StoryHTML returns the dashboard chrome text for lang.
func StoryHTML(lang Lang) StoryHTMLText {
	if lang == ZH {
		return StoryHTMLText{
			DocTitle: func(id string) string { return "Journey " + id },
			Subtitle: func(t, s int, from, to string) string {
				return fmt.Sprintf("%d 个任务 · %d 个步骤 · %s – %s", t, s, from, to)
			},
			PartialBanner: "此 Journey 的开头被所加载的文件范围截断，展示的是可见部分。",
			BreakBanner: func(k string) string {
				return "此 Journey 的开头是一个未解析的断裂（" + k + "）——找不到更早的前驱。"
			},
			RedactedBanner: "脱敏模式：结构、指标、角色、token 数保留；对话正文替换为长度占位，逐步详单链接与 Findings 文本已隐去。",
			GeneratedNote:  "由 vmr analyze 生成 · 单文件自包含 · 零外部依赖",
			Empty:          "（空）",
			RedactedText:   func(n int) string { return fmt.Sprintf("‹text: %d chars›", n) },

			RecorderBar:          "VMR · 飞行记录仪",
			VerdictProbableCause: "主因判定",
			VerdictStamp: func(level string) string {
				switch level {
				case "critical":
					return "严重"
				case "warning":
					return "警告"
				default:
					return "正常"
				}
			},
			VerdictClean: "未触发任何规则检测器（不等于运行无问题）。",
			VerdictRedacted: func(code string, step int) string {
				return fmt.Sprintf("检测器 %s @ 步骤 %d（正文已脱敏）", code, step)
			},
			DamageLine: func(steps int, dur, tokens string) string {
				return fmt.Sprintf("%d 步 · %s · 处理 %s token", steps, dur, tokens)
			},
			DamageCost: func(amount string) string { return " · 约 " + amount },
			PointOfNoReturnHead: func(step int, ts string) string {
				if ts == "" {
					return fmt.Sprintf("不可逆转折点 → 步骤 %d", step)
				}
				return fmt.Sprintf("不可逆转折点 → 步骤 %d @ %s", step, ts)
			},
			PONRCompaction: func(before, after string, dropped int) string {
				if before == "" {
					if dropped > 0 {
						return fmt.Sprintf("一次上下文压缩丢弃了 %d 个具名约束", dropped)
					}
					return "一次上下文压缩丢掉了关键指令"
				}
				s := fmt.Sprintf("上下文从 %s 压到 %s token", before, after)
				if dropped > 0 {
					s += fmt.Sprintf(" · %d 个具名约束被丢弃", dropped)
				}
				return s
			},
			PONRRetry:              "对失败的调用原样重试，未调整参数",
			PONRContract:           "历史被重建、大幅缩小——被丢弃的部分已不可见",
			NoPointOfNoReturn:      "未检测到不可逆的转折点（不代表运行无问题）。",
			DiffusePointOfNoReturn: "没有单一的不可逆转折点——失控是渐进的，而非某一步骤突变，详见下方 Findings。",

			OutcomeLabel:       "结局",
			OutcomeDeliverable: func(step int) string { return fmt.Sprintf("Step %d 产出最终文件，工具", step) },
			OutcomeTermination: func(finish string) string { return "终止：finish=" + finish },
			OutcomeUnknown:     "终止方式未知",

			SectionStructure: "结构",
			SectionMetrics:   "指标",
			SectionFindings:  "疑似问题",
			TaskLabel:        func(n int) string { return fmt.Sprintf("任务 %d", n) },
			StepLabel:        func(n int) string { return fmt.Sprintf("步骤 %d", n) },

			StructNoSteps: "（无步骤）",
			MoreTools:     func(n int) string { return fmt.Sprintf("+%d", n) },
			Attempts:      func(n int) string { return fmt.Sprintf("%d 次尝试", n) },
			FailoverBadge: "failover",
			SysChanged:    "系统提示词变化",
			EditLabel:     func(k string) string { return "上下文编辑：" + k },
			StitchLabel: func(k, sc, cf string) string {
				return fmt.Sprintf("缝合 %s（覆盖率 %s，置信度 %s）", k, sc, cf)
			},
			CompactionShort: func(b, a int64) string { return fmt.Sprintf("压缩 %d→%d tok", b, a) },
			EntityCounts:    func(sw, su int) string { return fmt.Sprintf("压缩后：%d 个实体消失、%d 个保留", sw, su) },
			Swallowed:       "压缩后不再出现",
			Survived:        "压缩后仍保留",
			Instruction:     "指令",
			StepSaid:        "本步模型说了什么",
			NoReply:         "本轮模型按约定未回复",

			MetricNetTime:       "净工作时长",
			MetricModelTime:     "模型时长",
			MetricAgentTime:     "Agent 执行时长",
			MetricTokensIn:      "输入 token",
			MetricTokensCached:  "其中缓存命中",
			MetricTokensOut:     "输出 token",
			MetricToolCalls:     "工具调用",
			MetricCompactions:   "压缩次数",
			MetricDupActionRate: "重复动作率",
			MetricOutputRepeat:  "输出重复率",
			MetricErrorRecovery: "错误恢复次数",
			MetricPlanExecRatio: "纯规划轮占比",
			MetricModelSwitches: "模型切换次数",
			MetricContextUtil:   "上下文利用率",
			SparkContextTitle:   "每步上下文 token",
			SparkContextCaption: func(f, l int64) string { return fmt.Sprintf("%d → %d tok", f, l) },

			FindingSourceRule:    "规则",
			FindingSourceLLM:     "AI 推测",
			FindingConfidence:    func(l string) string { return "置信度 " + l },
			FindingAtStep:        func(seq int) string { return fmt.Sprintf("Step %d", seq) },
			FindingRelatedSteps:  func(seqs string) string { return "相关 Step：" + seqs },
			FindingEvidenceLabel: "证据",
			FindingActionLabel:   "建议",
			NoFindings:           "未命中任何行为指标。",
			RedactedFindingsNote: "脱敏模式：仅列出 Finding 代码与 Step 锚点，叙述文本与证据片段已隐去。",
			AnthropicCoverageNote: func(codes string) string {
				return "⚠️ 本 journey 全部请求均为非 Anthropic Messages 协议：以下规则信号依赖仅该协议才会填充的字段，未触发不代表已检查（" + codes + "）。"
			},
		}
	}
	return StoryHTMLText{
		DocTitle: func(id string) string { return "Journey " + id },
		Subtitle: func(t, s int, from, to string) string {
			return fmt.Sprintf("%d tasks · %d steps · %s – %s", t, s, from, to)
		},
		PartialBanner: "This journey's beginning is truncated by the loaded file range; only the visible part is shown.",
		BreakBanner: func(k string) string {
			return "This journey opens on an unresolved break (" + k + ") — no earlier predecessor was found."
		},
		RedactedBanner: "Redacted: structure, metrics, roles and token counts kept; conversation bodies are length placeholders, per-step detail links and finding text are removed.",
		GeneratedNote:  "Generated by vmr analyze · single self-contained file · zero external dependencies",
		Empty:          "(empty)",
		RedactedText:   func(n int) string { return fmt.Sprintf("‹text: %d chars›", n) },

		RecorderBar:          "VMR · FLIGHT RECORDER",
		VerdictProbableCause: "PROBABLE CAUSE",
		VerdictStamp: func(level string) string {
			switch level {
			case "critical":
				return "CRITICAL"
			case "warning":
				return "WARNING"
			default:
				return "NOMINAL"
			}
		},
		VerdictClean: "No behavior detector fired (does not imply the run was problem-free).",
		VerdictRedacted: func(code string, step int) string {
			return fmt.Sprintf("detector %s at Step %d (text redacted)", code, step)
		},
		DamageLine: func(steps int, dur, tokens string) string {
			return fmt.Sprintf("%d steps · %s · %s tokens processed", steps, dur, tokens)
		},
		DamageCost: func(amount string) string { return " · ≈ " + amount },
		PointOfNoReturnHead: func(step int, ts string) string {
			if ts == "" {
				return fmt.Sprintf("POINT OF NO RETURN → Step %d", step)
			}
			return fmt.Sprintf("POINT OF NO RETURN → Step %d @ %s", step, ts)
		},
		PONRCompaction: func(before, after string, dropped int) string {
			if before == "" {
				if dropped > 0 {
					return fmt.Sprintf("a context compaction dropped %d named constraints", dropped)
				}
				return "a context compaction dropped critical instructions"
			}
			s := fmt.Sprintf("context compacted %s → %s tokens", before, after)
			if dropped > 0 {
				s += fmt.Sprintf(" · %d named constraints dropped", dropped)
			}
			return s
		},
		PONRRetry:              "retried a failing call without changing the arguments",
		PONRContract:           "history rebuilt much smaller — whatever was in the discarded tail is gone",
		NoPointOfNoReturn:      "No unrecoverable turning point detected (does not imply the run was problem-free).",
		DiffusePointOfNoReturn: "No single point of no return — the run degraded gradually rather than at one decisive step. See the findings below.",

		OutcomeLabel:       "Outcome",
		OutcomeDeliverable: func(step int) string { return fmt.Sprintf("Final file written at Step %d via", step) },
		OutcomeTermination: func(finish string) string { return "Ended: finish=" + finish },
		OutcomeUnknown:     "Termination mode unknown",

		SectionStructure: "Structure",
		SectionMetrics:   "Metrics",
		SectionFindings:  "Suspected issues",
		TaskLabel:        func(n int) string { return fmt.Sprintf("Task %d", n) },
		StepLabel:        func(n int) string { return fmt.Sprintf("Step %d", n) },

		StructNoSteps:   "(no steps)",
		MoreTools:       func(n int) string { return fmt.Sprintf("+%d", n) },
		Attempts:        func(n int) string { return fmt.Sprintf("%d attempts", n) },
		FailoverBadge:   "failover",
		SysChanged:      "system prompt changed",
		EditLabel:       func(k string) string { return "context edit: " + k },
		StitchLabel:     func(k, sc, cf string) string { return fmt.Sprintf("stitch %s (coverage %s, confidence %s)", k, sc, cf) },
		CompactionShort: func(b, a int64) string { return fmt.Sprintf("compaction %d→%d tok", b, a) },
		EntityCounts: func(sw, su int) string {
			return fmt.Sprintf("after compaction: %d entities dropped, %d survived", sw, su)
		},
		Swallowed:   "dropped after compaction",
		Survived:    "survived compaction",
		Instruction: "Instruction",
		StepSaid:    "What the model said this step",
		NoReply:     "The model deliberately did not reply this turn",

		MetricNetTime:       "Net working time",
		MetricModelTime:     "Model time",
		MetricAgentTime:     "Agent exec time",
		MetricTokensIn:      "Input tokens",
		MetricTokensCached:  "  of which cached",
		MetricTokensOut:     "Output tokens",
		MetricToolCalls:     "Tool calls",
		MetricCompactions:   "Compactions",
		MetricDupActionRate: "Duplicate-action rate",
		MetricOutputRepeat:  "Output repetition",
		MetricErrorRecovery: "Error recoveries",
		MetricPlanExecRatio: "Plan-only turn share",
		MetricModelSwitches: "Model switches",
		MetricContextUtil:   "Context utilization",
		SparkContextTitle:   "Context tokens per step",
		SparkContextCaption: func(f, l int64) string { return fmt.Sprintf("%d → %d tok", f, l) },

		FindingSourceRule:    "rule",
		FindingSourceLLM:     "AI inferred",
		FindingConfidence:    func(l string) string { return "confidence " + l },
		FindingAtStep:        func(seq int) string { return fmt.Sprintf("Step %d", seq) },
		FindingRelatedSteps:  func(seqs string) string { return "related Steps: " + seqs },
		FindingEvidenceLabel: "evidence",
		FindingActionLabel:   "action",
		NoFindings:           "No behavior indicators fired.",
		RedactedFindingsNote: "Redacted: finding codes and Step anchors only; narrative text and evidence excerpts removed.",
		AnthropicCoverageNote: func(codes string) string {
			return "⚠️ Every request in this journey used a non-Anthropic-Messages protocol: the rule signals below read a field only that protocol populates, so one not firing does not mean it was checked (" + codes + ")."
		},
	}
}
