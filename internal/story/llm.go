// Ver 2026-07-30 21:00, by Sonnet 5

// Scoped to the -compare report: an optional, always-degradable LLM
// interpretation layer appended to a Comparison's rendered Markdown. Every
// constraint here follows one three-layer principle: the LLM only ever
// receives numbers this package already computed (Comparison/
// ComparisonExtras), never generates a new number itself, and its output is
// rendered as a clearly-labeled, separately-cached block — never mixed into
// the fact-layer sections RenderComparisonMarkdown produces.
//
// Endpoint resolution is deliberately the single simplest option: a
// manually-supplied "host:port" for an already-running VMR instance
// (LLMOptions.Addr) plus that instance's own virtual model name
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

	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// promptVersionBase is bumped whenever the -compare prompt template or its
// evidence pack's shape changes materially — part of the disk-cache key
// (the "(step_hash, model, prompt_version)" convention, adapted here to
// "(evidence pack, model, prompt_version)" since this layer operates on
// whole Journeys, not a single step). Each of the other two scenarios
// (SingleJourneyEvidencePack, DivergenceEvidencePack) carries its own
// version string in its own promptSpec — see evidencePackKind.
const promptVersionBase = "compare-llm-v2"

// promptSpec bundles one interpretation scenario's prompt template pieces:
// which system prompt to send, how to wrap the evidence pack's JSON into
// the user message, and a version tag that becomes part of the disk-cache
// key. Each evidence-pack shape supplies its own via evidencePackKind.
type promptSpec struct {
	Version                string
	System                 string
	UserPrefix, UserSuffix string
}

// evidencePackKind is implemented by every evidence-pack shape this layer
// supports — EvidencePack (-compare), SingleJourneyEvidencePack (5.9),
// DivergenceEvidencePack (6c). It's how cacheKey/buildUserPrompt/Interpret
// pick the right prompt for whichever pack type they're called with,
// entirely through Go's generic type inference — every call site
// (Interpret(ctx, opts, pack, lang)) reads identically regardless of which
// scenario pack belongs to, and the -compare call site in cmd_story.go
// needed zero changes when 5.9/6c were added.
type evidencePackKind interface {
	promptSpec(lang i18n.Lang) promptSpec
}

func (EvidencePack) promptSpec(lang i18n.Lang) promptSpec {
	t := i18n.LLM(lang)
	return promptSpec{Version: promptVersionBase, System: t.SystemPrompt, UserPrefix: t.UserPromptPrefix, UserSuffix: t.UserPromptSuffix}
}

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
	Seq int `json:"seq"`
	// Req is the Step's request-level coordinate (P2's basename:line,
	// Manifest.Req) — added in P4.3 so an LLM interpretation that cites a
	// specific Step ("at step 7 it looked up...") gives a reader or a
	// future navigation feature (P6.2) something to jump to, without the
	// evidence pack growing by more than a few bytes per entry.
	Req   string   `json:"req,omitempty"`
	Tools []string `json:"tools,omitempty"`
	Brief string   `json:"brief"`
}

// buildToolIndex condenses j's Steps into ToolIndexEntry rows, in lang (the
// "brief" field is free text the evidence pack embeds verbatim for the LLM
// to read — see this file's package comment — so it follows the same
// language as the rest of the evidence pack's surrounding prose).
func buildToolIndex(j *Journey, lang i18n.Lang) []ToolIndexEntry {
	steps := journeySteps(j)
	out := make([]ToolIndexEntry, 0, len(steps))
	noReply := i18n.LLM(lang).NoTextReply
	for _, s := range steps {
		var tools []string
		for _, tc := range s.ToolCalls {
			tools = append(tools, tc.Name)
		}
		brief := taskseg.Preview(s.RespText)
		if brief == "" {
			brief = noReply
		}
		req := ""
		if s.Manifest != nil {
			req = s.Manifest.Req
		}
		out = append(out, ToolIndexEntry{Seq: s.Seq, Req: req, Tools: tools, Brief: brief})
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
func BuildEvidencePack(jA, jB *Journey, cmp Comparison, lang i18n.Lang) EvidencePack {
	return EvidencePack{
		Comparison:  cmp,
		TaskTitlesA: journeyTaskTitles(jA),
		TaskTitlesB: journeyTaskTitles(jB),
		ToolIndexA:  buildToolIndex(jA, lang),
		ToolIndexB:  buildToolIndex(jB, lang),
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
// character count) — this is the -llm-dry-run size estimate. Deliberately
// not a token count: estimating tokens accurately would
// need a tokenizer matched to whatever model -llm-addr resolves to, which
// this package has no way to know in advance — chars/4 is close enough for
// "does this look reasonable before I spend money on it".
func (p EvidencePack) EstimateChars() int { return packChars(p) }

// packChars is EstimateChars' shared implementation — a package-level
// function rather than a generic method (Go doesn't allow type parameters
// on methods), reused by every evidence-pack shape's own EstimateChars.
func packChars(pack any) int {
	data, err := json.Marshal(pack)
	if err != nil {
		return 0
	}
	return utf8.RuneCount(data)
}

// buildUserPrompt embeds the evidence pack as pretty-printed JSON — pretty
// rather than compact on purpose: this is a debugging aid too (the exact
// prompt sent is what a human reads if they open the cache file), and the
// token-cost difference against a several-thousand-char excerpt is noise.
// The system prompt (pack.promptSpec(lang).System) instructs the model
// which language to answer in, so this wrapper text and the model's own
// reply stay in the same language as the rest of the report.
func buildUserPrompt[T evidencePackKind](pack T, lang i18n.Lang) (string, error) {
	data, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal evidence pack: %w", err)
	}
	spec := pack.promptSpec(lang)
	return spec.UserPrefix + string(data) + spec.UserSuffix, nil
}

// LLMOptions configures the interpretation layer's one HTTP call — see this
// file's package-level doc comment for why there is exactly one supported
// endpoint shape (a manually-run VMR instance, openai-completions protocol only).
type LLMOptions struct {
	Addr     string // "host:port" (or a full "http://..." base) of a running VMR instance; "" disables this layer entirely
	Model    string // that instance's virtual model name, sent verbatim as "model" — no rewriting
	APIKey   string // optional bearer token, only meaningful if that instance has api_keys configured
	CacheDir string // directory for the disk cache (0700); "" disables caching
}

// Enabled reports whether opts actually turns the interpretation layer on —
// Addr is the sole switch, there is no separate -llm bool flag.
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
func cacheKey[T evidencePackKind](model string, pack T, lang i18n.Lang) (string, error) {
	data, err := json.Marshal(pack)
	if err != nil {
		return "", err
	}
	spec := pack.promptSpec(lang)
	h := sha256.New()
	h.Write([]byte(spec.Version + "-" + lang.String()))
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
func callLLM(ctx context.Context, opts LLMOptions, systemPrompt, userPrompt string) (string, error) {
	reqBody := chatCompletionRequest{
		Model:  opts.Model,
		Stream: false,
		Messages: []chatMsgSimple{
			{Role: "system", Content: systemPrompt},
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
func Interpret[T evidencePackKind](ctx context.Context, opts LLMOptions, pack T, lang i18n.Lang) (InterpretResult, error) {
	if !opts.Enabled() {
		return InterpretResult{}, fmt.Errorf("LLM interpretation layer not enabled (no -llm-addr)")
	}

	var key string
	if opts.CacheDir != "" {
		k, err := cacheKey(opts.Model, pack, lang)
		if err == nil {
			key = k
			if data, err := os.ReadFile(cachePath(opts.CacheDir, key)); err == nil {
				return InterpretResult{Text: string(data), Cached: true}, nil
			}
		}
	}

	userPrompt, err := buildUserPrompt(pack, lang)
	if err != nil {
		return InterpretResult{}, err
	}
	text, err := callLLM(ctx, opts, pack.promptSpec(lang).System, userPrompt)
	if err != nil {
		return InterpretResult{}, err
	}

	// Both failure modes here are fail-open by design (the disk cache is an
	// optimization, not a requirement) but are kept as two
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

// atxHeading reports whether line opens with an ATX heading marker (1-6
// '#' followed by a space) and, if so, its level.
func atxHeading(line string) (level int, ok bool) {
	n := 0
	for n < len(line) && n < 6 && line[n] == '#' {
		n++
	}
	if n == 0 || n >= len(line) || line[n] != ' ' {
		return 0, false
	}
	return n, true
}

// downgradeHeadingLevels shifts every level-2-through-5 ATX heading in text
// one level deeper (## -> ###, ### -> ####, …, capped at level 6) — a
// deterministic render-layer fallback for i18n/story_llm.go's prompt
// instruction (already asks the model to use level-3 subsection headings),
// since a document's outline structure shouldn't depend on a model's
// instruction-following (architecture doc §4.2).
//
// Shifts EVERY level uniformly, not just "## " -> "### ", because a model
// that only partially follows the instruction — nesting its own
// sub-subsections correctly relative to ITS OWN top level, but forgetting
// that whole block needs to sit one level deeper under this document's own
// "## LLM Interpretation" wrapper — would otherwise have its internal
// hierarchy flattened: rewriting only "## " leaves an untouched "### "
// child at the SAME level as its (now "### ") former parent, turning
// nested sections into siblings. A uniform +1 shift preserves whatever
// hierarchy the model built internally. Level 1 is left alone (the prompt
// never asks for it, and there's no wrapper level below it to preserve
// nesting against).
//
// Skips fenced code blocks — both ``` and ~~~ fences, toggling in/out on
// each matching fence marker — the LLM's free-text response routinely
// embeds a code block, and a "## " inside one is literal content, not a
// heading.
func downgradeHeadingLevels(text string) string {
	lines := strings.Split(text, "\n")
	fence := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case fence != "" && strings.HasPrefix(trimmed, fence):
			fence = ""
			continue
		case fence != "":
			continue
		case strings.HasPrefix(trimmed, "```"):
			fence = "```"
			continue
		case strings.HasPrefix(trimmed, "~~~"):
			fence = "~~~"
			continue
		}
		if lvl, ok := atxHeading(line); ok && lvl >= 2 && lvl < 6 {
			lines[i] = "#" + line
		}
	}
	return strings.Join(lines, "\n")
}

// RenderLLMSection wraps res.Text (or, on failure, a short explanatory note)
// with the "this is interpretation, not fact" banner — always rendered as
// its own clearly separated section, never blended into the fact-layer
// sections above it. scope distinguishes this section's title from
// another LLM section in the SAME document — pass "" when this is the
// only one (renderJourney's single call); -compare's two calls (the
// overall comparison, and — only when a divergence point was found — a
// second, separately-cached call scoped to just that point) must each
// pass their own non-empty scope, or both would render under the
// byte-identical heading "## LLM 解读（模型：X）" with nothing in the
// document's own outline to tell a reader they're two different sections
// covering two different things, not one section pasted in twice.
func RenderLLMSection(opts LLMOptions, res InterpretResult, lang i18n.Lang, scope string) string {
	t := i18n.LLM(lang)
	var b strings.Builder
	b.WriteString(t.SectionTitle(opts.Model, scope))
	b.WriteString(t.SectionDisclaimer(opts.Model))
	if res.Cached {
		b.WriteString(t.CachedNote)
	}
	b.WriteString("\n")
	b.WriteString(downgradeHeadingLevels(res.Text))
	b.WriteString("\n")
	return b.String()
}
