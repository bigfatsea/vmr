// Ver 2026-09-16, by Sonnet 5

// Pairs with internal/report/viewmodel_guard.go (Agent Guard's M2 offline
// bidirectional forensic audit section --
// the Agent Guard spec).
package i18n

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

func Guard(lang Lang) GuardText {
	if lang == ZH {
		return GuardText{
			Title: "§ Agent Guard 离线双向取证审计",
			Intro: func(recordsScanned, recordsWithHits int) string {
				return "本节统计 Agent Guard 在 " + itoa64(int64(recordsScanned)) + " 条已覆盖请求中检出的凭据形状命中，其中 " +
					itoa64(int64(recordsWithHits)) + " 条请求命中至少一条规则。数据优先取自在线护栏盖章的 `audit.Record.Guard`，尚未盖章时由离线现场补扫提供（ADR-12）——本节只做离线呈现，不做任何在线拦截或改写。\n\n"
			},
			SourceLine: func(stamped, fallbackScanned int) string {
				return "其中 " + itoa64(int64(stamped)) + " 条来自在线护栏盖章（权威路径），" +
					itoa64(int64(fallbackScanned)) + " 条来自离线现场补扫（兼容路径）。\n\n"
			},
			ScanFailedLine: func(failed int) string {
				return "> ⚠️ **离线补扫有 " + itoa64(int64(failed)) + " 条记录处理异常已跳过，请检查日志。**\n\n"
			},
			TableHeaders:         [6]string{"规则", "级别", "唯一凭据数", "总命中次数", "涉及请求数", "单请求最大重复度"},
			Tier1Label:           "Tier 1（高置信度）",
			Tier2Label:           "Tier 2（仅供人工复核）",
			Tier2Note:            "> **Tier 2 规则含已知误报模式**（例如泛化的 `sk-` 前缀会命中部分英文单词子串），仅供人工复核，永不作为在线改写/阻断的依据。\n\n",
			Amplification:        "> **单请求最大重复度**衡量的是「同一凭据在一次请求的上下文里出现了多少次」（长会话把历史全文重发导致的放大），不是「泄露了多少次」——同一凭据出现 N 次通常是一次泄露被放大 N 次，而非 N 次独立泄露。\n\n",
			ProvidersLabel:       "Provider 暴露面归因",
			ProviderTableHeaders: [4]string{"Provider", "总命中次数", "唯一凭据数", "涉及请求数"},
			InboundLabel:         "入向取证",
			InboundIntro: func(toolCallsInspected int) string {
				return "以下数据来自对客户端实收响应体（`Client.Response.Body`）的离线扫描——报告的是**已经发生的事**，不是被拦截的事。共审查了 " + itoa64(int64(toolCallsInspected)) + " 次工具调用。\n\n"
			},
			RuneTableHeaders: [2]string{"类别", "计数"},
			RuneCategoryLabel: func(category string) string {
				labels := map[string]string{
					"tags": "Tags 区块（A 档，必删）", "control": "控制字符（A 档，必删）",
					"zwsp": "零宽空格 ZWSP（B 档）", "soft_hyphen": "软连字符（B 档）",
					"bom": "中间位 BOM（B 档）", "bidi": "双向控制符（B 档，Trojan Source 载体）",
					"line_sep": "行/段分隔符（B 档）", "varsel": "变体选择符/ZWJ/双向标记（C 档，仅标记不删）",
				}
				if l, ok := labels[category]; ok {
					return l
				}
				return category
			},
			SanitizedRunesLabel:  "**在线已过滤的隐写字符**——以下计数来自 Agent Guard 在线净化器实际剥除的字符（ADR-15 唯一保留的入向在线干预）。本表统计的是净化前存在、已被提前剥除、本不会到达客户端的字符；若上方还有一张列出最终留在响应里的字符的表格，两者互补而非重叠。\n\n",
			ToolRiskTableHeaders: [4]string{"风险类别", "CWE", "次数", "示例工具"},
			EchoLine: func(toolEcho, textEcho int) string {
				line := "凭据回显交叉比对（ADR-6）：命令类工具实参内 "
				if toolEcho == 0 {
					line += "0 次（真实语料基线，非零值值得人工复核，可能是上游原样回显了请求内携带的真实凭据）"
				} else {
					line += "**" + itoa64(int64(toolEcho)) + " 次**——非零，建议立即人工复核"
				}
				line += "；助手纯文本内容内 " + itoa64(int64(textEcho)) + " 次（正常对话场景，如复述配置项，不作为攻击信号）。\n\n"
				return line
			},
		}
	}
	return GuardText{
		Title: "§ Agent Guard Offline Bidirectional Forensic Audit",
		Intro: func(recordsScanned, recordsWithHits int) string {
			return "This section summarizes credential-shaped hits Agent Guard found across " + itoa64(int64(recordsScanned)) +
				" covered requests, " + itoa64(int64(recordsWithHits)) + " of which matched at least one rule. Data prefers the Authoritative Fast Path (`audit.Record.Guard`, written at request time by online guarding) and falls back to an offline on-the-spot scan when that is absent (ADR-12) -- this section is offline presentation only, never an online interception or rewrite decision.\n\n"
		},
		SourceLine: func(stamped, fallbackScanned int) string {
			return "Of these, " + itoa64(int64(stamped)) + " came from the online-guard Authoritative Path and " +
				itoa64(int64(fallbackScanned)) + " from the offline Fallback Path.\n\n"
		},
		ScanFailedLine: func(failed int) string {
			return "> ⚠️ **" + itoa64(int64(failed)) + " record(s) failed offline fallback scanning and were skipped.**\n\n"
		},
		TableHeaders:         [6]string{"Rule", "Tier", "Unique Credentials", "Total Hits", "Records Affected", "Max/Record"},
		Tier1Label:           "Tier 1 (high confidence)",
		Tier2Label:           "Tier 2 (audit-only, human review)",
		Tier2Note:            "> **Tier 2 rules carry known false-positive patterns** (e.g. the generic `sk-` prefix matches some English word substrings) -- audit/triage only, never a basis for any online rewrite/block decision.\n\n",
		Amplification:        "> **Max/Record measures a context-amplification factor** (how many times the same credential appears within one request's context, e.g. a long session resending its full history) -- not a leak count. The same credential appearing N times is usually one leak amplified N times, not N independent leaks.\n\n",
		ProvidersLabel:       "Provider Exposure Attribution",
		ProviderTableHeaders: [4]string{"Provider", "Total Hits", "Unique Credentials", "Records Affected"},
		InboundLabel:         "Inbound Forensics",
		InboundIntro: func(toolCallsInspected int) string {
			return "The following comes from an offline scan of what the client actually received (`Client.Response.Body`) -- it reports what already happened, never what was blocked. " +
				itoa64(int64(toolCallsInspected)) + " tool call(s) were inspected in total.\n\n"
		},
		RuneTableHeaders: [2]string{"Category", "Count"},
		RuneCategoryLabel: func(category string) string {
			labels := map[string]string{
				"tags": "Tags block (tier A, always stripped online)", "control": "Control characters (tier A, always stripped online)",
				"zwsp": "Zero-width space (tier B)", "soft_hyphen": "Soft hyphen (tier B)",
				"bom": "Mid-string BOM (tier B)", "bidi": "Bidi override/isolate (tier B, Trojan Source carrier)",
				"line_sep": "Line/paragraph separator (tier B)", "varsel": "Variation selector/ZWJ/bidi mark (tier C, flagged only, never deleted)",
			}
			if l, ok := labels[category]; ok {
				return l
			}
			return category
		},
		SanitizedRunesLabel:  "**Invisible characters already stripped online** -- the counts below come from Agent Guard's online sanitizer actually removing characters (ADR-15's one remaining online inbound intervention). This table counts what never reached the client at all because it was stripped first; where a table above also lists what survived to the final response, the two are complementary, never overlapping.\n\n",
		ToolRiskTableHeaders: [4]string{"Risk Category", "CWE", "Count", "Example Tools"},
		EchoLine: func(toolEcho, textEcho int) string {
			line := "Credential-echo cross-reference (ADR-6): "
			if toolEcho == 0 {
				line += "0 inside tool-call arguments (the real-corpus baseline; nonzero would be worth a human look -- it may indicate an upstream echoing back a real credential from the request itself)"
			} else {
				line += "**" + itoa64(int64(toolEcho)) + "** inside tool-call arguments -- nonzero, recommend immediate human review"
			}
			line += "; " + itoa64(int64(textEcho)) + " inside plain assistant text (normal conversation, e.g. restating a config value -- not an attack signal).\n\n"
			return line
		},
	}
}
