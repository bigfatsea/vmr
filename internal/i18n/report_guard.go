// Ver 2026-09-22 02:30, by Sonnet 5

// Pairs with internal/report/viewmodel_guard.go (Agent Guard's M2 offline
// bidirectional forensic audit section --
// the Agent Guard spec).
package i18n

import "fmt"

// GuardText is viewmodel_guard.go's text, in one language.
type GuardText struct {
	Title          string
	Intro          func(recordsScanned, recordsWithHits int) string
	SourceLine     func(stamped, fallbackScanned int) string
	ScanFailedLine func(failed int) string
	TableHeaders   [6]string // rule, tier, unique credentials, total hits, records affected, max/record
	Tier1Label     string
	Tier2Label     string
	Tier2Note      string
	Amplification  string

	ProvidersLabel       string
	ProviderTableHeaders [4]string // provider, total hits, unique credentials, records affected

	InboundLabel         string
	InboundIntro         func(toolCallsInspected int) string
	RuneTableHeaders     [2]string // category, count
	RuneCategoryLabel    func(category string) string
	SanitizedRunesLabel  string
	ToolRiskTableHeaders [4]string // category, cwe, count, example tools
	EchoLine             func(toolEcho, textEcho int) string
}

// guardRow holds report_guard.go's literal templates, one row per Lang
// (Table's own doc comment). EchoLine's "toolEcho == 0" branch and
// RuneCategoryLabel's "found in map" lookup are the two conditionals here
// — both conditions are identical in both languages, so that logic is
// written once in Guard below.
type guardRow struct {
	title             string
	introFmt          string
	sourceLineFmt     string
	scanFailedLineFmt string
	tableHeaders      [6]string
	tier1Label        string
	tier2Label        string
	tier2Note         string
	amplification     string

	providersLabel       string
	providerTableHeaders [4]string

	inboundLabel         string
	inboundIntroFmt      string
	runeTableHeaders     [2]string
	runeCategoryLabels   map[string]string
	sanitizedRunesLabel  string
	toolRiskTableHeaders [4]string

	echoPrefix     string
	echoZeroBranch string
	echoNonzeroFmt string
	echoSuffixFmt  string
}

var guardRows = Table[guardRow]{
	EN: {
		title:             "§ Agent Guard Offline Bidirectional Forensic Audit",
		introFmt:          "This section summarizes credential-shaped hits Agent Guard found across %d covered requests, %d of which matched at least one rule. Data prefers the Authoritative Fast Path (`audit.Record.Guard`, written at request time by online guarding) and falls back to an offline on-the-spot scan when that is absent (ADR-12) -- this section is offline presentation only, never an online interception or rewrite decision.\n\n",
		sourceLineFmt:     "Of these, %d came from the online-guard Authoritative Path and %d from the offline Fallback Path.\n\n",
		scanFailedLineFmt: "> ⚠️ **%d record(s) failed offline fallback scanning and were skipped.**\n\n",
		tableHeaders:      [6]string{"Rule", "Tier", "Unique Credentials", "Total Hits", "Records Affected", "Max/Record"},
		tier1Label:        "Tier 1 (high confidence)",
		tier2Label:        "Tier 2 (audit-only, human review)",
		tier2Note:         "> **Tier 2 rules carry known false-positive patterns** (e.g. the generic `sk-` prefix matches some English word substrings) -- audit/triage only, never a basis for any online rewrite/block decision.\n\n",
		amplification:     "> **Max/Record measures a context-amplification factor** (how many times the same credential appears within one request's context, e.g. a long session resending its full history) -- not a leak count. The same credential appearing N times is usually one leak amplified N times, not N independent leaks.\n\n",

		providersLabel:       "Provider Exposure Attribution",
		providerTableHeaders: [4]string{"Provider", "Total Hits", "Unique Credentials", "Records Affected"},

		inboundLabel:     "Inbound Forensics",
		inboundIntroFmt:  "The following comes from an offline scan of what the client actually received (`Client.Response.Body`) -- it reports what already happened, never what was blocked. %d tool call(s) were inspected in total.\n\n",
		runeTableHeaders: [2]string{"Category", "Count"},
		runeCategoryLabels: map[string]string{
			"tags": "Tags block (tier A, always stripped online)", "control": "Control characters (tier A, always stripped online)",
			"zwsp": "Zero-width space (tier B)", "soft_hyphen": "Soft hyphen (tier B)",
			"bom": "Mid-string BOM (tier B)", "bidi": "Bidi override/isolate (tier B, Trojan Source carrier)",
			"line_sep": "Line/paragraph separator (tier B)", "varsel": "Variation selector/ZWJ/bidi mark (tier C, flagged only, never deleted)",
		},
		sanitizedRunesLabel:  "**Invisible characters already stripped online** -- the counts below come from Agent Guard's online sanitizer actually removing characters (ADR-15's one remaining online inbound intervention). This table counts what never reached the client at all because it was stripped first; where a table above also lists what survived to the final response, the two are complementary, never overlapping.\n\n",
		toolRiskTableHeaders: [4]string{"Risk Category", "CWE", "Count", "Example Tools"},

		echoPrefix:     "Credential-echo cross-reference (ADR-6): ",
		echoZeroBranch: "0 inside tool-call arguments (the real-corpus baseline; nonzero would be worth a human look -- it may indicate an upstream echoing back a real credential from the request itself)",
		echoNonzeroFmt: "**%d** inside tool-call arguments -- nonzero, recommend immediate human review",
		echoSuffixFmt:  "; %d inside plain assistant text (normal conversation, e.g. restating a config value -- not an attack signal).\n\n",
	},
	ZH: {
		title:             "§ Agent Guard 离线双向取证审计",
		introFmt:          "本节统计 Agent Guard 在 %d 条已覆盖请求中检出的凭据形状命中，其中 %d 条请求命中至少一条规则。数据优先取自在线护栏盖章的 `audit.Record.Guard`，尚未盖章时由离线现场补扫提供（ADR-12）——本节只做离线呈现，不做任何在线拦截或改写。\n\n",
		sourceLineFmt:     "其中 %d 条来自在线护栏盖章（权威路径），%d 条来自离线现场补扫（兼容路径）。\n\n",
		scanFailedLineFmt: "> ⚠️ **离线补扫有 %d 条记录处理异常已跳过，请检查日志。**\n\n",
		tableHeaders:      [6]string{"规则", "级别", "唯一凭据数", "总命中次数", "涉及请求数", "单请求最大重复度"},
		tier1Label:        "Tier 1（高置信度）",
		tier2Label:        "Tier 2（仅供人工复核）",
		tier2Note:         "> **Tier 2 规则含已知误报模式**（例如泛化的 `sk-` 前缀会命中部分英文单词子串），仅供人工复核，永不作为在线改写/阻断的依据。\n\n",
		amplification:     "> **单请求最大重复度**衡量的是「同一凭据在一次请求的上下文里出现了多少次」（长会话把历史全文重发导致的放大），不是「泄露了多少次」——同一凭据出现 N 次通常是一次泄露被放大 N 次，而非 N 次独立泄露。\n\n",

		providersLabel:       "Provider 暴露面归因",
		providerTableHeaders: [4]string{"Provider", "总命中次数", "唯一凭据数", "涉及请求数"},

		inboundLabel:     "入向取证",
		inboundIntroFmt:  "以下数据来自对客户端实收响应体（`Client.Response.Body`）的离线扫描——报告的是**已经发生的事**，不是被拦截的事。共审查了 %d 次工具调用。\n\n",
		runeTableHeaders: [2]string{"类别", "计数"},
		runeCategoryLabels: map[string]string{
			"tags": "Tags 区块（A 档，必删）", "control": "控制字符（A 档，必删）",
			"zwsp": "零宽空格 ZWSP（B 档）", "soft_hyphen": "软连字符（B 档）",
			"bom": "中间位 BOM（B 档）", "bidi": "双向控制符（B 档，Trojan Source 载体）",
			"line_sep": "行/段分隔符（B 档）", "varsel": "变体选择符/ZWJ/双向标记（C 档，仅标记不删）",
		},
		sanitizedRunesLabel:  "**在线已过滤的隐写字符**——以下计数来自 Agent Guard 在线净化器实际剥除的字符（ADR-15 唯一保留的入向在线干预）。本表统计的是净化前存在、已被提前剥除、本不会到达客户端的字符；若上方还有一张列出最终留在响应里的字符的表格，两者互补而非重叠。\n\n",
		toolRiskTableHeaders: [4]string{"风险类别", "CWE", "次数", "示例工具"},

		echoPrefix:     "凭据回显交叉比对（ADR-6）：命令类工具实参内 ",
		echoZeroBranch: "0 次（真实语料基线，非零值值得人工复核，可能是上游原样回显了请求内携带的真实凭据）",
		echoNonzeroFmt: "**%d 次**——非零，建议立即人工复核",
		echoSuffixFmt:  "；助手纯文本内容内 %d 次（正常对话场景，如复述配置项，不作为攻击信号）。\n\n",
	},
}

func Guard(lang Lang) GuardText {
	r := guardRows.Row(lang)
	return GuardText{
		Title: r.title,
		Intro: func(recordsScanned, recordsWithHits int) string {
			return fmt.Sprintf(r.introFmt, recordsScanned, recordsWithHits)
		},
		SourceLine: func(stamped, fallbackScanned int) string {
			return fmt.Sprintf(r.sourceLineFmt, stamped, fallbackScanned)
		},
		ScanFailedLine: func(failed int) string { return fmt.Sprintf(r.scanFailedLineFmt, failed) },
		TableHeaders:   r.tableHeaders,
		Tier1Label:     r.tier1Label,
		Tier2Label:     r.tier2Label,
		Tier2Note:      r.tier2Note,
		Amplification:  r.amplification,

		ProvidersLabel:       r.providersLabel,
		ProviderTableHeaders: r.providerTableHeaders,

		InboundLabel:     r.inboundLabel,
		InboundIntro:     func(toolCallsInspected int) string { return fmt.Sprintf(r.inboundIntroFmt, toolCallsInspected) },
		RuneTableHeaders: r.runeTableHeaders,
		RuneCategoryLabel: func(category string) string {
			if l, ok := r.runeCategoryLabels[category]; ok {
				return l
			}
			return category
		},
		SanitizedRunesLabel:  r.sanitizedRunesLabel,
		ToolRiskTableHeaders: r.toolRiskTableHeaders,
		EchoLine: func(toolEcho, textEcho int) string {
			line := r.echoPrefix
			if toolEcho == 0 {
				line += r.echoZeroBranch
			} else {
				line += fmt.Sprintf(r.echoNonzeroFmt, toolEcho)
			}
			line += fmt.Sprintf(r.echoSuffixFmt, textEcho)
			return line
		},
	}
}
