// Ver 2026-07-30 00:10, by Sonnet 5

// The behavior profile — nine rule-derived, framework-comparable metrics
// computed over a built Journey. Purely a function of data already sitting
// on Journey/Step/Event — no re-fetching, no I/O, no LLM calls (the profile
// layer is rule-derived; the LLM layer above it is optional and not built
// yet). Two Journeys' Metrics diff directly; that diff is Step 4's 4d
// module's entire deliverable.
package story

import (
	"sort"
	"strings"
	"time"

	"vmr/internal/chatmsg"
	"vmr/internal/core"
	"vmr/internal/i18n"
)

// Metrics is one Journey's behavior profile (the design doc's nine-indicator
// behavior-profile section).
type Metrics struct {
	// F10 time three-way decomposition: every inter-step gap
	// is classified by whether the step it precedes is HumanInitiated.
	ModelMS      int64 `json:"model_ms"`       // sum of every Step's Rec.DurMS
	AgentExecMS  int64 `json:"agent_exec_ms"`  // gap time before a non-HumanInitiated step
	HumanIdleMS  int64 `json:"human_idle_ms"`  // gap time before a HumanInitiated step (the Journey's own first step has no preceding gap)
	NetWorkingMS int64 `json:"net_working_ms"` // ModelMS + AgentExecMS — human idle excluded

	// ModelToToolRatio is ModelMS / AgentExecMS — is the bottleneck in
	// reasoning or in execution. 0 when AgentExecMS is 0 (no agent-side gap
	// observed anywhere in this Journey).
	ModelToToolRatio float64 `json:"model_to_tool_ratio"`

	ToolCallCount int            `json:"tool_call_count"`
	ToolCallDist  []ToolCallStat `json:"tool_call_distribution"` // sorted by Count desc, Name asc on ties

	// DuplicateActionRate is the fraction of tool calls whose (name, args)
	// pair repeats an earlier call in the same Journey — "is it spinning its
	// wheels".
	DuplicateActionRate float64 `json:"duplicate_action_rate"`

	// ErrorRecoveryCount counts Steps that both (a) received an
	// is_error-marked tool_result since the previous Step and (b) went on to
	// issue their own tool call anyway — a rough proxy for "the agent tried
	// to recover from a failure". Only
	// Anthropic's explicit is_error content-block field is detected;
	// OpenAI's protocol has no equivalent standard marker, so this
	// undercounts on OpenAI-only corpora — a documented limitation, not a
	// bug (matches the "宁可粗糙也不猜语义" rule this whole layer follows).
	ErrorRecoveryCount int `json:"error_recovery_count"`

	// PlanExecRatio is the fraction of Steps whose response carried no tool
	// call — pure "thinking out loud" turns versus turns that took action.
	PlanExecRatio float64 `json:"plan_exec_ratio"`

	// ContextCurve is one point per Step: the estimated token composition of
	// that Step's OWN full request body, split by role — "上下文构成演化".
	ContextCurve []ContextPoint `json:"context_composition_curve"`

	// ContextUtilization is the S-D-scenario indicator (the design doc's
	// blind-spot table): of every non-system Event's estimated
	// tokens, what fraction mentioned an entity (file path, URL — the same
	// rough regex the compaction step's CompactionInfo already uses) that some LATER Event
	// went on to mention again. Low value = a lot of what entered the
	// context was never referred to again. Scoped to ALL non-system Events
	// rather than only tool-role ones: chatmsg flattens a whole message's
	// content parts into one Text blob, so an Anthropic tool_result can't be
	// cleanly isolated from a user message it shares content with — the
	// broader scope is the honest approximation, not tool_result-only.
	ContextUtilization float64 `json:"context_utilization"`

	// CompactionCount/CompactionLossTokens aggregate every stitch
	// boundary's already-computed CompactionInfo — the design doc's
	// information-loss summary, rolled up to one Journey-level number.
	CompactionCount      int   `json:"compaction_count"`
	CompactionLossTokens int64 `json:"compaction_loss_tokens"`
}

// ToolCallStat is one tool name's share of a Journey's tool calls.
type ToolCallStat struct {
	Name       string  `json:"name"`
	Count      int     `json:"count"`
	TokenShare float64 `json:"token_share"` // this tool's Args tokens / total tool-call Args tokens across the Journey
}

// ContextPoint is one Step's request-body token composition by role.
type ContextPoint struct {
	Seq             int   `json:"seq"`
	SystemTokens    int64 `json:"system_tokens"`
	UserTokens      int64 `json:"user_tokens"`
	AssistantTokens int64 `json:"assistant_tokens"`
	ToolTokens      int64 `json:"tool_tokens"`
}

// ComputeMetrics derives j's behavior profile.
func ComputeMetrics(j *Journey) Metrics {
	steps := journeySteps(j)

	var m Metrics
	m.ModelMS, m.AgentExecMS, m.HumanIdleMS = computeTimeSplit(steps)
	m.NetWorkingMS = m.ModelMS + m.AgentExecMS
	if m.AgentExecMS > 0 {
		m.ModelToToolRatio = float64(m.ModelMS) / float64(m.AgentExecMS)
	}

	m.ToolCallDist, m.ToolCallCount = toolCallDistribution(steps)
	m.DuplicateActionRate = duplicateActionRate(steps)
	m.ErrorRecoveryCount = errorRecoveryCount(steps)
	m.PlanExecRatio = planExecRatio(steps)
	m.ContextCurve = contextCurve(steps)
	m.ContextUtilization = contextUtilization(j)
	m.CompactionCount, m.CompactionLossTokens = compactionTotals(steps)
	return m
}

// journeySteps flattens j's Tasks into one Seq-ordered Step slice — the same
// shape journey_test.go's allSteps test helper builds, duplicated here
// because production code can't depend on a _test.go file.
func journeySteps(j *Journey) []*Step {
	var out []*Step
	for _, t := range j.Tasks {
		out = append(out, t.Steps...)
	}
	return out
}

// computeTimeSplit applies the design doc's gap-classification rule to every
// consecutive Step pair: the wall-clock gap between one Step's response
// landing and the next Step's request arriving is human idle iff the next
// Step is HumanInitiated, else agent-side execution (the agent kept working
// locally — tool execution, planning — without a fresh instruction). A
// non-positive gap (clock skew, or overlapping/retried requests) contributes
// nothing rather than going negative and corrupting the totals.
func computeTimeSplit(steps []*Step) (modelMS, agentMS, idleMS int64) {
	for i, s := range steps {
		modelMS += s.Rec.DurMS
		if i == 0 {
			continue
		}
		prev := steps[i-1]
		prevEnd := prev.Manifest.TS.Add(time.Duration(prev.Rec.DurMS) * time.Millisecond)
		gap := s.Manifest.TS.Sub(prevEnd).Milliseconds()
		if gap <= 0 {
			continue
		}
		if s.HumanInitiated {
			idleMS += gap
		} else {
			agentMS += gap
		}
	}
	return
}

// toolCallDistribution tallies every Step's ToolCalls by name, with each
// tool's Args-token share of the Journey's total tool-call Args tokens.
func toolCallDistribution(steps []*Step) ([]ToolCallStat, int) {
	counts := map[string]int{}
	tokens := map[string]int64{}
	var names []string
	total := 0
	var totalTokens int64
	for _, s := range steps {
		for _, tc := range s.ToolCalls {
			if counts[tc.Name] == 0 {
				names = append(names, tc.Name)
			}
			counts[tc.Name]++
			tk := core.EstimateTextTokens([]byte(tc.Args))
			tokens[tc.Name] += tk
			totalTokens += tk
			total++
		}
	}
	sort.Strings(names)
	out := make([]ToolCallStat, 0, len(names))
	for _, name := range names {
		var share float64
		if totalTokens > 0 {
			share = float64(tokens[name]) / float64(totalTokens)
		}
		out = append(out, ToolCallStat{Name: name, Count: counts[name], TokenShare: share})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out, total
}

// toolCallKey is the exact-repeat identity of a tool call: same tool, same
// serialized arguments. Shared by toolCallRepeats and findings.go's own
// grouping so both agree on what "the same call" means — two independent
// definitions here would let a Step get tagged 🔄 by one and skipped by the
// other for what a reader would see as the identical situation.
func toolCallKey(tc chatmsg.ToolCall) string { return tc.Name + "\x00" + tc.Args }

// ToolCallOccurrence is one tool call's repeat status within a Journey —
// the shared basis for duplicateActionRate (aggregate rate), the
// exact_repeat_tool_call Finding (findings.go, Step-level location), and the
// 🔄 retry tag/symbol render_spine.go's Step-role labeling and tool-call
// timeline both use. All three read this one slice instead of each
// re-deciding "is this a repeat" on its own.
type ToolCallOccurrence struct {
	StepSeq      int
	Name         string
	IsRepeat     bool // this (name, args) pair already occurred earlier in the Journey
	FirstSeenSeq int  // valid when IsRepeat: the Step where this pair first appeared
}

// toolCallRepeats walks steps in order and marks each tool call as a repeat
// (of an earlier, exact — same name, same serialized args — call) or not.
func toolCallRepeats(steps []*Step) []ToolCallOccurrence {
	seen := map[string]int{} // toolCallKey -> first-seen StepSeq
	var out []ToolCallOccurrence
	for _, s := range steps {
		for _, tc := range s.ToolCalls {
			key := toolCallKey(tc)
			if first, ok := seen[key]; ok {
				out = append(out, ToolCallOccurrence{StepSeq: s.Seq, Name: tc.Name, IsRepeat: true, FirstSeenSeq: first})
			} else {
				seen[key] = s.Seq
				out = append(out, ToolCallOccurrence{StepSeq: s.Seq, Name: tc.Name})
			}
		}
	}
	return out
}

// duplicateActionRate is the fraction of tool calls whose exact (name, args)
// pair already occurred earlier in the same Journey — built on
// toolCallRepeats rather than its own scan, so the Journey-level rate and
// the Step-level 🔄 tagging can never disagree on what counts as a repeat.
func duplicateActionRate(steps []*Step) float64 {
	occ := toolCallRepeats(steps)
	if len(occ) == 0 {
		return 0
	}
	dup := 0
	for _, o := range occ {
		if o.IsRepeat {
			dup++
		}
	}
	return float64(dup) / float64(len(occ))
}

// isErrorMarker is the literal text chatmsg.RenderPart embeds for an
// Anthropic tool_result content block whose is_error field is true.
const isErrorMarker = "❌ is_error"

// errorRecoveryCount counts Steps that both received an error-marked
// tool_result among their NewEvents and went on to issue a tool call of
// their own — see ErrorRecoveryCount's doc comment for the OpenAI caveat.
func errorRecoveryCount(steps []*Step) int {
	n := 0
	for _, s := range steps {
		if len(s.ToolCalls) == 0 {
			continue
		}
		for _, ev := range s.NewEvents {
			if strings.Contains(ev.Msg.Text, isErrorMarker) {
				n++
				break
			}
		}
	}
	return n
}

// planExecRatio is the fraction of Steps whose response carried no tool
// call.
func planExecRatio(steps []*Step) float64 {
	if len(steps) == 0 {
		return 0
	}
	plan := 0
	for _, s := range steps {
		if len(s.ToolCalls) == 0 {
			plan++
		}
	}
	return float64(plan) / float64(len(steps))
}

// contextCurve re-derives each Step's own full request body's role/token
// composition — not persisted on Step itself (would duplicate data already
// reachable via Rec), recomputed here the same way buildCompactionInfo
// re-reads a predecessor's body on demand.
func contextCurve(steps []*Step) []ContextPoint {
	out := make([]ContextPoint, 0, len(steps))
	for _, s := range steps {
		body, _ := s.Rec.Client.Request.Body.(map[string]any)
		p := ContextPoint{Seq: s.Seq}
		for _, msg := range chatmsg.Messages(body) {
			tk := core.EstimateTextTokens([]byte(msg.Text))
			switch msg.Role {
			case "system":
				p.SystemTokens += tk
			case "assistant":
				p.AssistantTokens += tk
			case "tool":
				p.ToolTokens += tk
			default: // "user" and any non-standard role (chatmsg.Messages' "?" fallback)
				p.UserTokens += tk
			}
		}
		out = append(out, p)
	}
	return out
}

// contextUtilization implements ContextUtilization's doc comment: for each
// non-system Event, does any entity extracted from it (extractEntities, the
// same extraction the compaction step already uses) reappear in a LATER
// Event's text. j.Events is already in first-appearance order, so "later"
// is simply a higher slice index.
//
// O(N·E) via a single lastSeen[entity]->max index pass, not the naive
// O(N²·E) "for each Event, rescan every later Event and rebuild a map" —
// an entity reappears after i iff its last (i.e. highest-index) occurrence
// anywhere in the Journey is > i, so one forward pass recording the max
// index per entity answers every Event's "does this reappear later"
// question in O(len(entities)) instead of O(N). A 1,000-step Journey (a
// real long agent task) turns ~3×10⁷ map inserts into ~a few thousand.
func contextUtilization(j *Journey) float64 {
	type scanned struct {
		tokens   int64
		entities []string
	}
	info := make([]scanned, len(j.Events))
	lastSeen := make(map[string]int) // entity -> highest Event index it appears at
	for i, ev := range j.Events {
		if ev.Msg.Role == "system" {
			continue
		}
		entities := extractEntities(ev.Msg.Text)
		info[i] = scanned{tokens: core.EstimateTextTokens([]byte(ev.Msg.Text)), entities: entities}
		for _, e := range entities {
			lastSeen[e] = i
		}
	}

	var total, referenced int64
	for i, ev := range j.Events {
		if ev.Msg.Role == "system" || info[i].tokens == 0 {
			continue
		}
		total += info[i].tokens
		for _, e := range info[i].entities {
			if lastSeen[e] > i {
				referenced += info[i].tokens
				break
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(referenced) / float64(total)
}

// compactionTotals rolls every stitch boundary's already-computed
// CompactionInfo up to one Journey-level count and loss total.
func compactionTotals(steps []*Step) (count int, lossTokens int64) {
	for _, s := range steps {
		if s.Compaction == nil {
			continue
		}
		count++
		if loss := s.Compaction.TokensBefore - s.Compaction.TokensAfter; loss > 0 {
			lossTokens += loss
		}
	}
	return
}

// JourneySummary is journey-<id>.json's shape (design doc: "输出同时落
// journey-<id>.json，供第 4 步的对比模块直接消费") — a Journey's identity
// plus its Metrics profile and rule-derived Findings, so Phase 4d's
// comparison module can diff two Journeys without re-parsing Markdown.
type JourneySummary struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Metrics  Metrics   `json:"metrics"`
	Findings []Finding `json:"findings,omitempty"`
}

// Summarize builds j's JourneySummary, computing Metrics and Findings
// fresh. Findings are always computed with i18n.EN — journey-<id>.json is
// always-English, same convention report.Report2/vmr-report.json already
// uses for its own Finding field; a target-language copy is a separate
// ComputeFindings(j, lang) call the Markdown renderer's caller makes (see
// cmd/vmr/cmd_story.go's writeJourneyFile).
func Summarize(j *Journey) JourneySummary {
	return JourneySummary{
		ID: j.ID, Title: j.Title, From: j.From, To: j.To,
		Metrics: ComputeMetrics(j), Findings: ComputeFindings(j, i18n.EN),
	}
}
