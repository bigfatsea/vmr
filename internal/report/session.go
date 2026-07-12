// Ver 2026-07-12 01:20, by Fable 5

// Session analysis: group audit records into agent sessions → tasks → turns
// and extract per-request features, all offline and rule-based (no LLM).
// Method and evidence: docs/AgentSessionGrouping_Analysis_Fable5.md.
//
// The core signal is protocol-generic — agent clients resend the whole
// conversation each turn, so the first non-system message fingerprints the
// session and the longest common prefix (LCP) against a previous request
// isolates this turn's delta. Client-specific signals (Traceparent trace_id,
// OpenClaw wrapper templates, chat_id, Claude Code metadata.user_id) are
// used when present and silently skipped when not: a request that matches
// nothing still groups by the generic rule, it just carries fewer tags.
package report

import (
	"bufio"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"vmr/internal/audit"
)

// parentWindow bounds how many recent requests per session are kept as
// parent candidates for the max-LCP match. Observed divergences (ephemeral
// tail replacement, pruning branches) always resolve within a few requests.
const parentWindow = 16

// tailPrevKeep is how many trailing message previews each request retains,
// for rendering the parent's replaced tail. Replaced tails observed in real
// logs are 1-2 messages; beyond this window only counts are reported.
const tailPrevKeep = 8

// newUserWindow guards task splitting: a real user message only counts as a
// new instruction when it sits within this many messages of the request's
// end. An in-place history edit (e.g. image pruning) can push the delta
// boundary far back and sweep old user messages into the delta; those must
// not open a task.
const newUserWindow = 8

// ReqInfo is the analysis result for one audit record: grouping coordinates
// plus rule-extracted features. Fields are best-effort — absent signals stay
// zero-valued.
type ReqInfo struct {
	// identity within the input set
	Path string
	Line int
	TS   time.Time

	Model, Protocol, Outcome string

	// grouping
	SessKey      string // metadata session id or anchor hash; "" = ungrouped
	SessionID    string // "s01"… assigned after grouping
	TaskID       string // "t01"… within the session
	TaskSeq      int    // 1-based turn number within the task
	SessSeq      int    // 1-based turn number within the session
	Parent       *ReqInfo
	DeltaStart   int  // absolute message index where this request's new part begins
	Msgs         int  // total message count (incl. leading system)
	ReplacedTail int  // parent messages beyond the common prefix (replaced/edited)
	SysChanged   bool // system prompt differs from parent's

	// features
	TraceID        string
	ChatID         string
	ToolsSig       string // "tools:<n>/<hash8>"; "" when no tools field
	ToolsDeclared  []string
	Tags           []string
	Compaction     bool
	Summarizes     string // compaction only: session id it condensed
	ContinuesTo    string // compaction only: session id continuing from its output
	NewInstruction string // preview of the real user instruction in this delta
	ToolCalls      []string
	Finish         string
	Truncated      bool
	Usage          Usage
	UsageOK        bool

	DetailFile string // deterministic detail filename (assigned in ts order)

	// Aggregates that the SessionRows / Workloads consumers need to roll up.
	MessagesKnown    int              // requests whose body parsed as a chat object
	RoleChars        map[string]int64 // per-role displayed-character totals
	Fallbacks        int              // requests that needed >1 attempt
	Images           int              // inline request images detected
	ImagesCompressed int              // subset that triggered downscaling

	// working state (analysis only, dropped from JSON)
	leadSys   int      // leading system messages (absolute offset of keys[0])
	keys      []string // per non-system message content hash
	sysKey    string
	anchor    string
	tailPrev  []string       // previews of the last tailPrevKeep messages
	realUsers map[int]string // absolute idx → preview, real user instructions
	firstText string         // first non-system message text (capped)
	respText  string         // reassembled response content (compaction linking)
	// NoReply is true when the assistant's reply was empty or just "NO_REPLY"
	// (OpenClaw's skip-on-memory-flush pattern). Such records are typically
	// retried by the client a few minutes later; the retry carries the
	// user's actual instruction and is the one that gets processed. The
	// session analyzer treats NoReply parents as NOT opening a new task
	// boundary — the next record's "new instruction" is a retry of
	// the skipped one, not a fresh user intent.
	NoReply   bool
	errClass  string // last attempt error class (filename suffix)
	realModel string // model segment of the final attempt's endpoint
	declBytes int64  // serialized size of the declared tools array
	endpoint  string // final attempt endpoint
	attempts  int
	durMS     int64
	ttftMS    int64
	stream    bool
	bytesIn   int64
	bytesOut  int64
	norm      []string
}

// SessionInfo is one grouped agent session.
type SessionInfo struct {
	ID             string
	Title          string
	ChatID         string
	ContinuedFrom  string // session id this one continues via compaction
	IsContinuation bool   // anchor is a compaction summary (link may be off-log)
	Recs           []*ReqInfo
	Tasks          []*TaskInfo
}

// TaskInfo is one user-turn burst within a session.
type TaskInfo struct {
	ID    string
	Title string
	Recs  []*ReqInfo
}

// SessionAnalysis is the whole input set analyzed.
type SessionAnalysis struct {
	Recs        []*ReqInfo // ts order
	Sessions    []*SessionInfo
	Compactions []*ReqInfo
	Ungrouped   []*ReqInfo
	byKey       map[string]*ReqInfo // "path\x00line" lookup for render pass
}

// Lookup returns the analysis for the record at path:line, nil if unknown.
func (a *SessionAnalysis) Lookup(path string, line int) *ReqInfo {
	if a == nil {
		return nil
	}
	return a.byKey[fmt.Sprintf("%s\x00%d", path, line)]
}

// AnalyzeSessions reads the audit files and produces the session grouping
// plus per-request features. Unparseable lines are skipped (Build counts
// them); records without a chat body land in Ungrouped.
func AnalyzeSessions(paths []string) (*SessionAnalysis, error) {
	a := &SessionAnalysis{byKey: map[string]*ReqInfo{}}
	for _, path := range paths {
		rc, err := openAuditFile(path)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 1<<20), 128<<20)
		line := 0
		for sc.Scan() {
			line++
			var rec audit.Record
			if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
				continue
			}
			r := collect(&rec, path, line)
			a.Recs = append(a.Recs, r)
			a.byKey[fmt.Sprintf("%s\x00%d", path, line)] = r
		}
		rc.Close()
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}

	sort.SliceStable(a.Recs, func(i, j int) bool { return a.Recs[i].TS.Before(a.Recs[j].TS) })
	assignNames(a.Recs)
	group(a)
	linkCompactions(a)
	return a, nil
}

// ---- per-record collection ----

var chatIDRe = regexp.MustCompile(`"chat_id"\s*:\s*"([^"]+)"`)

// collect extracts everything needed from one record while its parsed JSON
// is in hand; only compact metadata is retained.
func collect(rec *audit.Record, path string, line int) *ReqInfo {
	r := &ReqInfo{
		Path: path, Line: line, TS: rec.TS,
		Model: rec.Model, Protocol: rec.Protocol, Outcome: rec.Outcome,
		realUsers: map[int]string{},
	}
	if tp := rec.Client.Request.Headers.Get("Traceparent"); tp != "" {
		if parts := strings.Split(tp, "-"); len(parts) >= 2 {
			r.TraceID = parts[1]
		}
	}
	for _, at := range rec.Attempts {
		if attemptErrorClass(at) == "truncated" && rec.Outcome == "ok" {
			r.Truncated = true
		}
	}
	r.errClass = errorClass(rec)
	r.realModel = realModel(rec)
	r.endpoint = lastEndpoint(rec)
	n, compressed := countImages(rec.Images)
	r.Images, r.ImagesCompressed = n, compressed
	r.attempts = len(rec.Attempts)
	if r.attempts > 1 {
		r.Fallbacks = 1
	}
	r.durMS, r.ttftMS, r.stream = rec.DurMS, rec.TTFTMS, rec.Stream
	r.bytesIn = bodyBytes(rec.Client.Request.Body)
	if rec.Client.Response != nil {
		r.bytesOut = bodyBytes(rec.Client.Response.Body)
	}
	for i := len(rec.Attempts) - 1; i >= 0; i-- {
		if len(rec.Attempts[i].Norm) > 0 {
			r.norm = rec.Attempts[i].Norm
			break
		}
	}
	if rec.Client.Response != nil {
		r.Usage, r.UsageOK = ExtractUsage(rec.Client.Response.Body)
		if s := responseSummary(rec.Client.Response.Body); s != nil {
			r.Finish = s.Finish
			for _, tc := range s.ToolCalls {
				if tc.Name != "" {
					r.ToolCalls = append(r.ToolCalls, tc.Name)
				}
			}
			r.respText = capStr(strings.TrimSpace(s.Content), 256<<10)
			// Detect OpenClaw's "no-reply" pattern: an empty content block,
			// or content that ends with the explicit "NO_REPLY" marker
			// (memory-flush acknowledgement, or post-compaction turn).
			// Such records are sent successfully but the LLM skipped the
			// reply — the next record carrying the same user instruction
			// is a retry of THIS one, not a new task.
			if r.Finish == "stop" || r.Finish == "end_turn" {
				trimmed := strings.TrimSpace(r.respText)
				if trimmed == "" {
					r.NoReply = true
				} else if strings.HasSuffix(trimmed, "NO_REPLY") {
					r.NoReply = true
				}
			}
		}
	}

	body, ok := rec.Client.Request.Body.(map[string]any)
	if !ok {
		return r
	}
	r.ToolsDeclared = toolNames(body)
	if tools, hasTools := body["tools"]; hasTools || len(r.ToolsDeclared) > 0 {
		r.ToolsSig = toolsSig(r.ToolsDeclared)
		if raw, err := json.Marshal(tools); err == nil {
			r.declBytes = int64(len(raw))
		}
	}
	if uid, _ := nested(body, "metadata", "user_id").(string); uid != "" {
		// Claude Code embeds a session uuid: "…_session_<uuid>".
		if i := strings.Index(uid, "session_"); i >= 0 {
			r.SessKey = "meta:" + uid[i:]
		} else {
			r.SessKey = "meta:" + uid
		}
	}

	msgs := chatMessages(body) // anthropic system becomes message #0 — same shape both protocols
	r.Msgs = len(msgs)
	r.MessagesKnown = 1 // body parsed as chat object
	for role, c := range roleChars(body) {
		if r.RoleChars == nil {
			r.RoleChars = map[string]int64{}
		}
		r.RoleChars[role] += c
	}
	rawMsgs, _ := body["messages"].([]any)
	sysHash := md5.New()
	var lastUser string
	for i, m := range msgs {
		if m.Role == "system" && i == r.leadSys { // leading system block
			sysHash.Write([]byte(m.Text))
			r.leadSys++
			continue
		}
		// Hash the raw message when available (exact), else the rendered text.
		// json.Marshal sorts map keys, so the digest is deterministic.
		var raw any = m.Text
		if ri := i - msgOffset(body); ri >= 0 && ri < len(rawMsgs) {
			raw = rawMsgs[ri]
		}
		b, _ := json.Marshal(raw)
		r.keys = append(r.keys, fmt.Sprintf("%x", md5.Sum(b)))

		if r.firstText == "" {
			r.firstText = capStr(m.Text, 512<<10)
		}
		if m.Role == "user" {
			lastUser = m.Text
			if text, ok := realUserText(m, rawMsgs, i-msgOffset(body)); ok {
				r.realUsers[i] = preview(text)
			}
		}
	}
	r.sysKey = fmt.Sprintf("%x", sysHash.Sum(nil))
	r.anchor = ""
	if len(r.keys) > 0 {
		r.anchor = r.keys[0]
	}
	if r.SessKey == "" && r.anchor != "" {
		r.SessKey = "anchor:" + r.anchor
	}
	for i := max(0, len(msgs)-tailPrevKeep); i < len(msgs); i++ {
		r.tailPrev = append(r.tailPrev, msgs[i].Role+": "+preview(msgs[i].Text))
	}

	// chat_id lives in OpenClaw's "Conversation info" wrapper (scan from the end).
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" && strings.Contains(msgs[i].Text, "Conversation info (untrusted metadata)") {
			if m := chatIDRe.FindStringSubmatch(msgs[i].Text); m != nil {
				r.ChatID = m[1]
				break
			}
		}
	}

	// Compaction: summarization system prompt, or the no-tools +
	// max_completion_tokens shape (§1.6-3 triple features).
	_, hasMaxCT := body["max_completion_tokens"]
	sysText := ""
	if r.leadSys > 0 {
		sysText = msgs[0].Text
	}
	if strings.Contains(strings.ToLower(capStr(sysText, 200)), "summarization") ||
		(len(r.ToolsDeclared) == 0 && hasMaxCT && r.TraceID == "") {
		r.Compaction = true
	}

	r.Tags = templateTags(r.firstText, lastUser, r.Compaction)
	return r
}

// msgOffset is the index shift between chatMessages output and the raw
// messages array: anthropic's top-level system is prepended as message #0.
func msgOffset(body map[string]any) int {
	if _, ok := body["system"]; ok {
		return 1
	}
	return 0
}

// openClawEnvelopeRe matches OpenClaw's "Conversation info (untrusted
// metadata)" / "Sender (untrusted metadata)" JSON blocks. OpenClaw glues
// these routing headers onto the front of real inbound messages (not just
// onto pure scaffolding pings) — a message can be 90% JSON envelope and
// still carry the actual ask in its last line.
var openClawEnvelopeRe = regexp.MustCompile(`(?s)(?:Conversation info|Sender) \(untrusted metadata\):\s*` + "```" + `(?:json)?\n.*?` + "```" + `\s*`)

// stripOpenClawEnvelope removes OpenClaw's metadata-wrapper blocks, leaving
// whatever real text (if any) precedes or follows them.
func stripOpenClawEnvelope(text string) string {
	return strings.TrimSpace(openClawEnvelopeRe.ReplaceAllString(text, ""))
}

// leadingBracketRe matches OpenClaw's "[Day Mon DD HH:MM TZ]" user-typed-time
// prefix, so a message that's just a timestamp plus an (already-stripped)
// envelope — no real ask — doesn't get mistaken for one.
var leadingBracketRe = regexp.MustCompile(`^\[[^\]]*\]\s*`)

// realUserText returns a user message's real-instruction text and whether it
// counts as one at all. Transport scaffolding doesn't: OpenClaw runtime
// wrappers, tool-produced image attachments, compaction summaries, and
// anthropic messages that are purely tool_result parts. OpenClaw's metadata
// envelope (chat_id/sender JSON) is stripped rather than disqualifying the
// whole message — the real ask is often glued right behind it, and
// discarding it entirely made task titles fall back to an earlier, unrelated
// message (observed in real logs: a 06:48 launch instruction wrapped in the
// envelope was dropped, so the task title showed an unrelated "continue"
// ping from 6 minutes earlier instead). A message that's PURELY the
// envelope — nothing real left after stripping — still doesn't count.
func realUserText(m chatMessage, rawMsgs []any, rawIdx int) (string, bool) {
	head := capStr(m.Text, 200)
	if strings.HasPrefix(head, "OpenClaw runtime context") ||
		strings.HasPrefix(head, "Attached image(s) from tool result") ||
		strings.HasPrefix(head, "The conversation history before this point was compacted") {
		return "", false
	}
	text := m.Text
	if strings.Contains(head, "Conversation info (untrusted metadata)") {
		text = stripOpenClawEnvelope(text)
		if leadingBracketRe.ReplaceAllString(text, "") == "" {
			return "", false // just a timestamp bracket, nothing real left
		}
	}
	if rawIdx >= 0 && rawIdx < len(rawMsgs) {
		if rm, ok := rawMsgs[rawIdx].(map[string]any); ok {
			if parts, ok := rm["content"].([]any); ok && len(parts) > 0 {
				allToolResult := true
				for _, p := range parts {
					pm, _ := p.(map[string]any)
					if pm == nil || pm["type"] != "tool_result" {
						allToolResult = false
						break
					}
				}
				if allToolResult {
					return "", false
				}
			}
		}
	}
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	return text, true
}

// isRealUser reports whether a user message is an actual instruction rather
// than transport scaffolding. See realUserText for the classification rules.
func isRealUser(m chatMessage, rawMsgs []any, rawIdx int) bool {
	_, ok := realUserText(m, rawMsgs, rawIdx)
	return ok
}

// templateTags classifies known message shapes. Unknown shapes get no tag —
// never a wrong one.
func templateTags(firstText, lastUser string, compaction bool) []string {
	var tags []string
	if compaction {
		tags = append(tags, "compaction")
	}
	if strings.Contains(capStr(firstText, 200), "compacted into the following summary") {
		tags = append(tags, "compacted_session")
	}
	if strings.HasPrefix(firstText, "<conversation>") {
		tags = append(tags, "conversation_feed")
	}
	if strings.Contains(lastUser, "[OpenClaw heartbeat poll]") {
		tags = append(tags, "heartbeat")
	}
	if strings.Contains(lastUser, "Write a dream diary") {
		tags = append(tags, "dream_diary")
	}
	return tags
}

// toolsSig fingerprints a declared tool set: count plus name-list hash.
func toolsSig(names []string) string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	sum := md5.Sum([]byte(strings.Join(sorted, ",")))
	return fmt.Sprintf("tools:%d/%x", len(names), sum[:4])
}

// responseSummary reassembles a recorded client response body (SSE string or
// JSON object) into the model's output.
func responseSummary(body any) *streamSummary {
	switch b := body.(type) {
	case string:
		return reassembleSSE(b)
	case map[string]any:
		if s, ok := finalMessage(b); ok {
			return s
		}
	}
	return nil
}

// ---- grouping ----

// assignNames gives every record its deterministic detail filename in ts
// order, so WriteDetails and the requests export agree on links.
func assignNames(recs []*ReqInfo) {
	used := map[string]int{}
	for _, r := range recs {
		r.DetailFile = detailFileNameFromInfo(r, used)
	}
}

// group clusters records into sessions and segments tasks.
func group(a *SessionAnalysis) {
	sessions := map[string]*SessionInfo{}
	var order []*SessionInfo
	for _, r := range a.Recs {
		if r.Compaction {
			a.Compactions = append(a.Compactions, r)
			continue
		}
		if r.SessKey == "" {
			a.Ungrouped = append(a.Ungrouped, r)
			continue
		}
		s := sessions[r.SessKey]
		if s == nil {
			s = &SessionInfo{}
			sessions[r.SessKey] = s
			order = append(order, s)
		}
		attach(s, r)
	}
	for i, s := range order {
		s.ID = fmt.Sprintf("s%02d", i+1)
		for j, t := range s.Tasks {
			t.ID = fmt.Sprintf("t%02d", j+1)
		}
		for _, r := range s.Recs {
			r.SessionID = s.ID
		}
		for _, t := range s.Tasks {
			for _, r := range t.Recs {
				r.TaskID = t.ID
			}
		}
		s.Title = sessionTitle(s)
		s.ChatID = sessionChatID(s)
		s.IsContinuation = len(s.Recs) > 0 && hasTag(s.Recs[0], "compacted_session")
	}
	a.Sessions = order
}

// attach adds a record to a session: picks its parent by max LCP, derives
// the delta boundary, and opens a new task when warranted.
func attach(s *SessionInfo, r *ReqInfo) {
	lo := max(0, len(s.Recs)-parentWindow)
	bestLCP := -1
	for _, cand := range s.Recs[lo:] {
		l := lcp(cand.keys, r.keys)
		if l >= bestLCP { // ties → most recent
			bestLCP, r.Parent = l, cand
		}
	}
	newTask := r.Parent == nil
	if r.Parent != nil {
		p := r.Parent
		r.DeltaStart = r.leadSys + bestLCP
		r.ReplacedTail = len(p.keys) - bestLCP
		r.SysChanged = p.sysKey != r.sysKey
		traceChanged := r.TraceID != "" && p.TraceID != "" && r.TraceID != p.TraceID
		// If the parent record ended in NO_REPLY (the LLM skipped its
		// reply), the user's instruction in this record is a RETRY of the
		// parent's skipped instruction, not a new user intent. We treat
		// the retry as the same task to keep task boundaries aligned with
		// actual user actions (a user types once → one task; multiple
		// records may result from retries / streaming re-sends).
		newTask = traceChanged || (!p.NoReply && r.deltaHasNewInstruction())
	} else {
		r.DeltaStart = 0 // whole request is "new" for the session's first record
	}
	r.NewInstruction = r.lastInstructionInDelta()

	s.Recs = append(s.Recs, r)
	r.SessSeq = len(s.Recs)
	if newTask || len(s.Tasks) == 0 {
		s.Tasks = append(s.Tasks, &TaskInfo{Title: taskTitle(r)})
	}
	t := s.Tasks[len(s.Tasks)-1]
	t.Recs = append(t.Recs, r)
	r.TaskSeq = len(t.Recs)
}

// deltaHasNewInstruction reports whether the delta contains a real user
// instruction near the request's end (see newUserWindow).
//
// A message only counts as new if its content wasn't already present
// SOMEWHERE in the parent (not just its LCP-matched prefix). LCP diffing is
// positional: pruning or reordering earlier in the history breaks the prefix
// match at that point, so everything after it — including a real-user
// message the parent already had verbatim, just at a different offset —
// looks "new" by position alone. Without this check, a single instruction
// that survives a mid-task context prune reopens as a fresh 1-turn task
// quoting itself (observed in real logs after fixing isRealUser to see
// envelope-wrapped instructions: pruning shifted the same "OK，基于你…"
// message into the tail window a second time).
func (r *ReqInfo) deltaHasNewInstruction() bool {
	var parentKeys map[string]bool
	if r.Parent != nil {
		parentKeys = make(map[string]bool, len(r.Parent.keys))
		for _, k := range r.Parent.keys {
			parentKeys[k] = true
		}
	}
	for idx := range r.realUsers {
		if idx < r.DeltaStart || idx < r.Msgs-newUserWindow {
			continue
		}
		if ki := idx - r.leadSys; parentKeys != nil && ki >= 0 && ki < len(r.keys) && parentKeys[r.keys[ki]] {
			continue // identical content already existed in the parent — shifted, not new
		}
		return true
	}
	return false
}

// lastInstructionInDelta returns the preview of the newest real user
// instruction inside the delta; "" when this turn is a pure tool-loop step.
func (r *ReqInfo) lastInstructionInDelta() string {
	best := -1
	for idx := range r.realUsers {
		if idx >= r.DeltaStart && idx > best {
			best = idx
		}
	}
	if best < 0 {
		return ""
	}
	return r.realUsers[best]
}

func taskTitle(r *ReqInfo) string {
	if r.NewInstruction != "" {
		return r.NewInstruction
	}
	if hasTag(r, "heartbeat") {
		return "(heartbeat)"
	}
	return "(工具循环延续)"
}

func sessionTitle(s *SessionInfo) string {
	// Earliest real instruction in the session's first request — the
	// conversation's opening ask, not the latest turn.
	if len(s.Recs) > 0 {
		first := s.Recs[0]
		best := -1
		for idx := range first.realUsers {
			if best < 0 || idx < best {
				best = idx
			}
		}
		if best >= 0 {
			return first.realUsers[best]
		}
	}
	for _, r := range s.Recs {
		if r.NewInstruction != "" {
			return r.NewInstruction
		}
	}
	if len(s.Recs) > 0 && s.Recs[0].firstText != "" {
		return preview(s.Recs[0].firstText)
	}
	return "(无标题)"
}

func sessionChatID(s *SessionInfo) string {
	for _, r := range s.Recs {
		if r.ChatID != "" {
			return r.ChatID
		}
	}
	return ""
}

func hasTag(r *ReqInfo, tag string) bool {
	for _, t := range r.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// lcp is the longest common prefix length of two hash vectors.
func lcp(a, b []string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// ---- compaction linking ----

// linkCompactions ties each compaction call to the session it summarized
// (its input quotes that session's first instruction) and to the session
// continuing from it (whose anchor embeds its output). Both are exact
// substring checks — no guessing; unmatched sides stay empty.
func linkCompactions(a *SessionAnalysis) {
	for _, c := range a.Compactions {
		out := needle(c.respText)
		in := c.firstText
		var successor, predecessor *SessionInfo
		for _, s := range a.Sessions {
			if len(s.Recs) == 0 {
				continue
			}
			first := s.Recs[0]
			if out != "" && strings.Contains(first.firstText, out) &&
				!first.TS.Before(c.TS) &&
				(successor == nil || first.TS.Before(successor.Recs[0].TS)) {
				successor = s
			}
			if fn := needle(strings.TrimSpace(stripBracketPrefix(first.firstText))); fn != "" &&
				strings.Contains(in, fn) && first.TS.Before(c.TS) &&
				(predecessor == nil || first.TS.After(predecessor.Recs[0].TS)) {
				predecessor = s
			}
		}
		if successor != nil {
			c.ContinuesTo = successor.ID
		}
		if predecessor != nil {
			c.Summarizes = predecessor.ID
		}
		if successor != nil && predecessor != nil && successor != predecessor {
			successor.ContinuedFrom = predecessor.ID
		}
	}
}

// needle caps a containment probe at a length that stays cheap but is far
// beyond accidental-collision territory.
func needle(s string) string {
	s = strings.TrimSpace(s)
	return capStr(s, 200)
}

// stripBracketPrefix removes a leading "[…] " block (OpenClaw's injected
// timestamp/channel prefix) so instruction text matches across rewrites.
func stripBracketPrefix(s string) string {
	if strings.HasPrefix(s, "[") {
		if i := strings.Index(s, "] "); i >= 0 && i < 120 {
			return s[i+2:]
		}
	}
	return s
}

func capStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ---- filename (shared with detail.go) ----

// detailFileNameFromInfo mirrors detailFileName for the analysis pass, which
// no longer holds the full record. Endpoint/error class come from features
// captured in collect; both passes therefore produce identical names.
func detailFileNameFromInfo(r *ReqInfo, used map[string]int) string {
	outcome := r.Outcome
	if outcome == "error" && r.errClass != "" {
		outcome += "-" + r.errClass
	}
	base := fmt.Sprintf("%s_%s_%s_%s",
		r.TS.Format("20060102-150405.000"),
		sanitizeName(displayModelName(r.Model)), sanitizeName(r.realModel), sanitizeName(outcome))
	used[base]++
	if n := used[base]; n > 1 {
		base = fmt.Sprintf("%s-%d", base, n)
	}
	return base + ".md"
}

func displayModelName(model string) string {
	if model == "" {
		return "(rejected)"
	}
	return model
}
