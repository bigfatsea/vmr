// Ver 2026-09-23 03:33, by Doubao Seed 2.0

// guard_corpus_scan is Agent Guard's calibration and corpus analysis tool:
// it performs a real-corpus scan against this repo's historical audit logs
// (logs/vmr-audit-*.jsonl[.zst]), and performs multi-dimensional security
// analysis to prepare data for the outbound (guard.Outbound) and inbound
// (guard.Inbound) online layers.
// Not part of the vmr binary — a calibration & analysis tool run by hand:
//
//	go run ./tools/guard_corpus_scan
//	go run ./tools/guard_corpus_scan -deep reports/guard-corpus-deep-analysis.json
//	go run ./tools/guard_corpus_scan -samples /tmp/samples.json -top 20
//
// -deep is opt-in (default empty = baseline aggregate only): the deep mode
// emits forensic previews of candidate credentials and its tool-call
// extraction covers only a slice of streaming corpora, so it must be a
// deliberate choice, not something every run sweeps in.
//
// What it does NOT do, on purpose: it never writes raw sensitive credential
// values into committed output files (-out).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/core"
	"vmr/internal/guard"
)

const maxUniqueTracked = 200000 // per-rule safety cap on distinct-value tracking memory

type ruleAgg struct {
	Rule         string `json:"rule"`
	Tier         string `json:"tier"`
	Hits         int    `json:"hits"`
	Uniq         int    `json:"uniq"`
	RecordsWith  int    `json:"records_with"`
	MaxPerRecord int    `json:"max_per_record"`
	UniqCapped   bool   `json:"uniq_capped,omitempty"`

	uniqueValues map[string]int // value -> count, capped at maxUniqueTracked
}

type corpusResult struct {
	GeneratedBy  string    `json:"generated_by"`
	RulesVersion int       `json:"rules_version"`
	FilesScanned int       `json:"files_scanned"`
	RecordsTotal int       `json:"records_total"`
	ParseErrors  int       `json:"parse_errors"`
	Rules        []ruleAgg `json:"rules"`
}

type sampleEntry struct {
	Preview string `json:"preview"`
	Length  int    `json:"length"`
	Count   int    `json:"count"`
}

type sampleFile struct {
	Note  string                   `json:"note"`
	Rules map[string][]sampleEntry `json:"rules"`
}

// Deep analysis structures
type CredentialSample struct {
	FP           string   `json:"fp"`
	Rule         string   `json:"rule"`
	Tier         string   `json:"tier"`
	Preview      string   `json:"preview"`
	Length       int      `json:"length"`
	CharsetKind  string   `json:"charset_kind"`
	IsFormable   bool     `json:"is_formable"`
	TotalHits    int      `json:"total_hits"`
	RecordsWith  int      `json:"records_with"`
	MaxPerRecord int      `json:"max_per_record"`
	Providers    []string `json:"providers"`
	Models       []string `json:"models"`
	FirstSeen    string   `json:"first_seen"`
	LastSeen     string   `json:"last_seen"`
}

type ProviderExposure struct {
	Provider string         `json:"provider"`
	ReqsWith int            `json:"reqs_with"`
	Hits     int            `json:"hits"`
	RuleHits map[string]int `json:"rule_hits"`
}

type RuneStat struct {
	Category string         `json:"category"`
	Action   string         `json:"action"`
	Total    int            `json:"total"`
	CodeMap  map[string]int `json:"codepoints"`
}

type ToolCallStat struct {
	ToolName    string `json:"tool_name"`
	Count       int    `json:"count"`
	TotalBytes  int64  `json:"total_bytes"`
	MaxArgBytes int    `json:"max_arg_bytes"`
}

type HighRiskEvent struct {
	Category string `json:"category"`
	Tool     string `json:"tool"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Time     string `json:"time"`
	Excerpt  string `json:"excerpt"`
}

type EchoEvent struct {
	Rule     string `json:"rule"`
	FP       string `json:"fp,omitempty"`
	Preview  string `json:"preview,omitempty"`
	Location string `json:"location,omitempty"`
	Tool     string `json:"tool,omitempty"`
	Provider string `json:"provider"`
	Time     string `json:"time"`
}

type DeepAnalysisResult struct {
	GeneratedAt        string             `json:"generated_at"`
	FilesScanned       int                `json:"files_scanned"`
	RecordsTotal       int                `json:"records_total"`
	ParseErrors        int                `json:"parse_errors"`
	TimeRangeStart     string             `json:"time_range_start"`
	TimeRangeEnd       string             `json:"time_range_end"`
	ProtocolDist       map[string]int     `json:"protocol_dist"`
	ModelDist          map[string]int     `json:"model_dist"`
	ProviderDist       map[string]int     `json:"provider_dist"`
	OutboundRules      []ruleAgg          `json:"outbound_rules"`
	OutboundSamples    []CredentialSample `json:"outbound_samples"`
	ProviderExposures  []ProviderExposure `json:"provider_exposures"`
	AmplificationHisto map[string]int     `json:"amplification_histo"`
	CoOccurrences      map[string]int     `json:"co_occurrences"`
	InboundRunes       []RuneStat         `json:"inbound_runes"`
	ToolCallTotals     struct {
		TotalWithTools int `json:"total_with_tools"`
		TotalCalls     int `json:"total_calls"`
		OversizeCalls  int `json:"oversize_calls_gt_256k"`
	} `json:"tool_call_totals"`
	ToolCallStats  []ToolCallStat  `json:"tool_call_stats"`
	HighRiskEvents []HighRiskEvent `json:"high_risk_events"`
	EchoEvents     []EchoEvent     `json:"echo_events"`
}

type rawRecord struct {
	TS       time.Time `json:"ts"`
	Model    string    `json:"model"`
	Protocol string    `json:"protocol"`
	Client   struct {
		Request struct {
			Body json.RawMessage `json:"body"`
		} `json:"request"`
		Response *struct {
			Body json.RawMessage `json:"body"`
		} `json:"response"`
	} `json:"client"`
	Attempts []struct {
		Endpoint string `json:"endpoint"`
	} `json:"attempts"`
}

type openAIRespMsg struct {
	Choices []struct {
		Message struct {
			Content   any `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

type anthropicRespMsg struct {
	Content []struct {
		Type  string          `json:"type"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
		Text  string          `json:"text"`
	} `json:"content"`
}

type workerState struct {
	engine       *guard.Engine
	scratch      *guard.Scratch
	recordsTotal int
	parseErrors  int
	timeMin      time.Time
	timeMax      time.Time

	protocolDist map[string]int
	modelDist    map[string]int
	providerDist map[string]int

	// Full-line stats (baseline corpus_scan.json)
	lineAggs map[string]*ruleAgg

	// Request-only stats (deep analysis)
	outRuleHits        map[string]int
	outRuleRecordsWith map[string]int
	outRuleMaxPerRec   map[string]int
	outSamples         map[string]*sampleCollector

	providerExposures  map[string]map[string]int
	providerReqsWith   map[string]int
	amplificationHisto map[string]int
	coOccurrences      map[string]int

	runesClassA map[rune]int
	runesClassB map[rune]int
	runesClassC map[rune]int

	totalWithTools int
	totalToolCalls int
	oversizeCalls  int
	toolCallStats  map[string]*toolStatAcc
	highRiskEvents []HighRiskEvent
	echoEvents     []EchoEvent
}

type sampleCollector struct {
	secret       string
	rule         string
	tier         string
	fp           string
	length       int
	totalHits    int
	recordsWith  int
	maxPerRecord int
	providers    map[string]bool
	models       map[string]bool
	firstSeen    time.Time
	lastSeen     time.Time
}

type toolStatAcc struct {
	name        string
	count       int
	totalBytes  int64
	maxArgBytes int
}

func main() {
	dir := flag.String("dir", "logs", "root directory to scan recursively for vmr-audit-*.jsonl[.zst] files")
	out := flag.String("out", "internal/guard/testdata/corpus_scan.json", "baseline aggregate output path (safe to commit)")
	deep := flag.String("deep", "", "optional deep analysis JSON output path; empty (the default) skips deep analysis entirely — when set, prefer a reports/ path (the forensic previews it carries must never sit in a docs/ dir a careless git add -A could sweep in)")
	samples := flag.String("samples", "", "optional path for a redacted, human-review-only top-N sample file")
	top := flag.Int("top", 50, "how many top-by-frequency values per rule to include in samples/deep output")
	workers := flag.Int("workers", runtime.NumCPU(), "number of worker goroutines for concurrent scanning")
	flag.Parse()

	files, err := findAuditFiles(*dir)
	if err != nil {
		log.Fatalf("find audit files under %s: %v", *dir, err)
	}
	if len(files) == 0 {
		log.Fatalf("no vmr-audit-*.jsonl[.zst] files found under %s", *dir)
	}
	fmt.Fprintf(os.Stderr, "scanning %d file(s) under %s with %d workers\n", len(files), *dir, *workers)

	// One Engine shared by every worker goroutine: the
	// rule table and its Aho-Corasick prefilter are immutable after
	// construction, so building it once here instead of once per worker
	// both amortizes the regexp compiles and exercises the exact
	// concurrent-Scan-sharing contract the online wiring will
	// depend on. Each worker still gets its own *guard.Scratch.
	sharedEngine, err := guard.NewEngine(guard.DefaultRules(), guard.RulesVersion)
	if err != nil {
		log.Fatalf("guard.NewEngine: %v", err)
	}

	fileChan := make(chan string, len(files))
	for _, f := range files {
		fileChan <- f
	}
	close(fileChan)

	var wg sync.WaitGroup
	states := make([]*workerState, *workers)
	startTime := time.Now()

	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			st := newWorkerState(sharedEngine)
			states[workerID] = st
			for filePath := range fileChan {
				processAuditFile(filePath, st, *deep != "")
			}
		}(w)
	}
	wg.Wait()
	fmt.Fprintf(os.Stderr, "scanned %d files in %v\n", len(files), time.Since(startTime))

	// Merge full-line baseline results (for corpus_scan.json)
	res := corpusResult{GeneratedBy: "tools/guard_corpus_scan", RulesVersion: guard.RulesVersion, FilesScanned: len(files)}
	mergedAggs := make(map[string]*ruleAgg)
	for _, r := range guard.DefaultRules() {
		mergedAggs[r.Name] = &ruleAgg{Rule: r.Name, Tier: r.Tier.String(), uniqueValues: map[string]int{}}
	}

	for _, st := range states {
		if st == nil {
			continue
		}
		res.RecordsTotal += st.recordsTotal
		res.ParseErrors += st.parseErrors

		for rname, a := range st.lineAggs {
			target := mergedAggs[rname]
			target.Hits += a.Hits
			target.RecordsWith += a.RecordsWith
			if a.MaxPerRecord > target.MaxPerRecord {
				target.MaxPerRecord = a.MaxPerRecord
			}
			for v, c := range a.uniqueValues {
				if len(target.uniqueValues) < maxUniqueTracked {
					target.uniqueValues[v] += c
				} else {
					target.UniqCapped = true
				}
			}
		}
	}

	for _, a := range mergedAggs {
		a.Uniq = len(a.uniqueValues)
		res.Rules = append(res.Rules, *a)
	}
	// Stable sort with rule name tie-breaker (Finding 3 fix)
	sort.Slice(res.Rules, func(i, j int) bool {
		if res.Rules[i].Hits != res.Rules[j].Hits {
			return res.Rules[i].Hits > res.Rules[j].Hits
		}
		return res.Rules[i].Rule < res.Rules[j].Rule
	})

	if err := writeJSON(*out, res); err != nil {
		log.Fatalf("write %s: %v", *out, err)
	}
	fmt.Fprintf(os.Stderr, "wrote baseline aggregate to %s\n", *out)

	// Samples export
	if *samples != "" {
		sf := sampleFile{
			Note:  "LOCAL REVIEW ONLY — redacted previews of the highest-frequency matched values per rule. Do not commit.",
			Rules: map[string][]sampleEntry{},
		}
		for _, a := range mergedAggs {
			sf.Rules[a.Rule] = topSamples(a.uniqueValues, *top)
		}
		if err := writeJSON(*samples, sf); err != nil {
			log.Fatalf("write %s: %v", *samples, err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", *samples)
	}

	// Deep analysis export
	if *deep != "" {
		deepRes := mergeDeepResults(states, len(files), *top)
		if err := writeJSON(*deep, deepRes); err != nil {
			log.Fatalf("write deep analysis %s: %v", *deep, err)
		}
		fmt.Fprintf(os.Stderr, "wrote deep analysis data to %s\n", *deep)
	}
}

func newWorkerState(eng *guard.Engine) *workerState {
	rules := eng.Rules()
	st := &workerState{
		engine:             eng,
		scratch:            guard.NewScratch(len(rules)),
		protocolDist:       make(map[string]int),
		modelDist:          make(map[string]int),
		providerDist:       make(map[string]int),
		lineAggs:           make(map[string]*ruleAgg),
		outRuleHits:        make(map[string]int),
		outRuleRecordsWith: make(map[string]int),
		outRuleMaxPerRec:   make(map[string]int),
		outSamples:         make(map[string]*sampleCollector),
		providerExposures:  make(map[string]map[string]int),
		providerReqsWith:   make(map[string]int),
		amplificationHisto: make(map[string]int),
		coOccurrences:      make(map[string]int),
		runesClassA:        make(map[rune]int),
		runesClassB:        make(map[rune]int),
		runesClassC:        make(map[rune]int),
		toolCallStats:      make(map[string]*toolStatAcc),
	}
	for _, r := range rules {
		st.lineAggs[r.Name] = &ruleAgg{Rule: r.Name, Tier: r.Tier.String(), uniqueValues: map[string]int{}}
	}
	return st
}

func processAuditFile(filePath string, st *workerState, doDeep bool) {
	f, err := audit.OpenLogFile(filePath)
	if err != nil {
		log.Printf("open %s: %v", filePath, err)
		return
	}
	defer f.Close()

	_ = audit.ForEachLine(f, audit.MaxLogLine, func(line []byte) {
		st.recordsTotal++

		// 1. Baseline: full-line scan
		findingsBuf := st.engine.Scan(line, st.scratch)
		if len(findingsBuf) > 0 {
			perRuleCount := map[string]int{}
			for _, fnd := range findingsBuf {
				a := st.lineAggs[fnd.Rule]
				a.Hits++
				perRuleCount[fnd.Rule]++
				body := string(fnd.Body(line))
				if len(a.uniqueValues) < maxUniqueTracked {
					a.uniqueValues[body]++
				} else {
					a.UniqCapped = true
				}
			}
			for rule, n := range perRuleCount {
				a := st.lineAggs[rule]
				a.RecordsWith++
				if n > a.MaxPerRecord {
					a.MaxPerRecord = n
				}
			}
		}

		if !doDeep {
			return
		}

		// 2. Deep Analysis: JSON parsed decomposition
		var rec rawRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			st.parseErrors++
			return
		}

		if st.timeMin.IsZero() || (!rec.TS.IsZero() && rec.TS.Before(st.timeMin)) {
			st.timeMin = rec.TS
		}
		if !rec.TS.IsZero() && rec.TS.After(st.timeMax) {
			st.timeMax = rec.TS
		}

		if rec.Protocol != "" {
			st.protocolDist[rec.Protocol]++
		}
		if rec.Model != "" {
			st.modelDist[rec.Model]++
		}

		provider := "unknown"
		if len(rec.Attempts) > 0 {
			ep := rec.Attempts[0].Endpoint
			if _, p, _, ok := core.SplitEndpointLabel(ep); ok && p != "" {
				provider = p
			} else {
				parts := strings.Split(ep, ":")
				if len(parts) >= 2 {
					provider = parts[1]
				}
			}
		}
		st.providerDist[provider]++

		// A. Outbound: Client.Request.Body
		var reqSecretsThisRec = make(map[string]bool)
		if len(rec.Client.Request.Body) > 0 {
			findingsBuf := st.engine.Scan(rec.Client.Request.Body, st.scratch)
			if len(findingsBuf) > 0 {
				perRuleCount := make(map[string]int)
				perRuleSet := make(map[string]bool)
				perSecretCount := make(map[string]int)

				for _, fnd := range findingsBuf {
					st.outRuleHits[fnd.Rule]++
					perRuleCount[fnd.Rule]++
					perRuleSet[fnd.Rule] = true

					secretBytes := rec.Client.Request.Body[fnd.Start:fnd.End]
					secretStr := string(secretBytes)
					reqSecretsThisRec[secretStr] = true
					perSecretCount[secretStr]++

					sc, exists := st.outSamples[secretStr]
					if !exists {
						fp := guard.Fingerprint(fnd.Rule, secretBytes)
						sc = &sampleCollector{
							secret:    secretStr,
							rule:      fnd.Rule,
							tier:      fnd.Tier.String(),
							fp:        fp,
							length:    len(secretStr),
							providers: make(map[string]bool),
							models:    make(map[string]bool),
							firstSeen: rec.TS,
							lastSeen:  rec.TS,
						}
						st.outSamples[secretStr] = sc
					}
					sc.totalHits++
					sc.providers[provider] = true
					if rec.Model != "" {
						sc.models[rec.Model] = true
					}
					if rec.TS.Before(sc.firstSeen) {
						sc.firstSeen = rec.TS
					}
					if rec.TS.After(sc.lastSeen) {
						sc.lastSeen = rec.TS
					}
				}

				for sStr, count := range perSecretCount {
					sc := st.outSamples[sStr]
					sc.recordsWith++
					if count > sc.maxPerRecord {
						sc.maxPerRecord = count
					}
				}

				for rule, count := range perRuleCount {
					st.outRuleRecordsWith[rule]++
					if count > st.outRuleMaxPerRec[rule] {
						st.outRuleMaxPerRec[rule] = count
					}
					bucket := amplificationBucket(count)
					st.amplificationHisto[bucket]++
				}

				pe, ok := st.providerExposures[provider]
				if !ok {
					pe = make(map[string]int)
					st.providerExposures[provider] = pe
				}
				for rule, count := range perRuleCount {
					pe[rule] += count
				}
				st.providerReqsWith[provider]++

				if len(perRuleSet) > 1 {
					var rlist []string
					for r := range perRuleSet {
						rlist = append(rlist, r)
					}
					sort.Strings(rlist)
					st.coOccurrences[strings.Join(rlist, "+")]++
				}
			}
		}

		// B. Inbound: Client.Response.Body
		if rec.Client.Response != nil && len(rec.Client.Response.Body) > 0 {
			respBytes := rec.Client.Response.Body

			// Echo scan
			findingsBuf := st.engine.Scan(respBytes, st.scratch)
			for _, fnd := range findingsBuf {
				secretBytes := respBytes[fnd.Start:fnd.End]
				secretStr := string(secretBytes)
				if reqSecretsThisRec[secretStr] {
					st.echoEvents = append(st.echoEvents, EchoEvent{
						Rule:     fnd.Rule,
						FP:       guard.Fingerprint(fnd.Rule, secretBytes),
						Preview:  redact(secretStr),
						Provider: provider,
						Time:     rec.TS.Format(time.RFC3339),
					})
				}
			}

			// Rune scan
			scanRunes(respBytes, st)

			// Tool call scan
			scanToolCalls(respBytes, provider, rec.Model, rec.TS, reqSecretsThisRec, st)
		}
	}, func() {
		st.parseErrors++
	})
}

func amplificationBucket(count int) string {
	switch {
	case count == 1:
		return "1"
	case count >= 2 && count <= 3:
		return "2-3"
	case count >= 4 && count <= 7:
		return "4-7"
	case count >= 8 && count <= 15:
		return "8-15"
	default:
		return "16+"
	}
}

// scanRunes buckets each rune into this report's A/B/C tier maps (kept
// per-codepoint here, unlike guard.ClassifyRunes' per-category counts,
// because a calibration report benefits from "which exact code point"
// detail a production report doesn't need) by asking guard.ClassifyRune
// for the tier -- the range checks themselves live in exactly one place
// (internal/guard), not duplicated here.
func scanRunes(b []byte, st *workerState) {
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		b = b[size:]
		switch guard.ClassifyRune(r) {
		case guard.RuneCatTags, guard.RuneCatControl:
			st.runesClassA[r]++
		case guard.RuneCatZWSP, guard.RuneCatSoftHyphen, guard.RuneCatBOM, guard.RuneCatBidi, guard.RuneCatLineSep:
			st.runesClassB[r]++
		case guard.RuneCatVarSel:
			st.runesClassC[r]++
		}
	}
}

func scanToolCalls(respBytes []byte, provider, model string, ts time.Time, reqSecrets map[string]bool, st *workerState) {
	known := make([][]byte, 0, len(reqSecrets))
	for sec := range reqSecrets {
		known = append(known, []byte(sec))
	}

	var oai openAIRespMsg
	hasTool := false
	if err := json.Unmarshal(respBytes, &oai); err == nil {
		for _, ch := range oai.Choices {
			if len(ch.Message.ToolCalls) > 0 {
				hasTool = true
				for _, tc := range ch.Message.ToolCalls {
					handleSingleToolCall(tc.Function.Name, tc.Function.Arguments, provider, model, ts, known, st)
				}
			}
		}
	}

	var ant anthropicRespMsg
	if err := json.Unmarshal(respBytes, &ant); err == nil {
		for _, blk := range ant.Content {
			if blk.Type == "tool_use" {
				hasTool = true
				handleSingleToolCall(blk.Name, string(blk.Input), provider, model, ts, known, st)
			}
		}
	}

	// A streaming response body is stored as a JSON string of raw SSE text,
	// which neither object form above can see — reassemble it through
	// chatmsg (the one shared SSE parser) before extracting tool calls.
	var sseBody string
	if err := json.Unmarshal(respBytes, &sseBody); err == nil {
		if ss := chatmsg.ReassembleSSE(sseBody); ss != nil {
			for _, tc := range ss.ToolCalls {
				hasTool = true
				handleSingleToolCall(tc.Name, tc.Args, provider, model, ts, known, st)
			}
		}
	}

	if hasTool {
		st.totalWithTools++
	}
}

// handleSingleToolCall judges one tool call via guard.InspectToolCall --
// the same tool-classification, high-risk-command, protected-path,
// and credential-echo logic internal/report's Fallback Path (guardscan.go)
// uses -- rather than a private regex library, so this calibration tool
// and `vmr analyze` can never quietly disagree about what counts as a hit.
func handleSingleToolCall(name, args, provider, model string, ts time.Time, known [][]byte, st *workerState) {
	st.totalToolCalls++
	argLen := len(args)
	if argLen > 262144 {
		st.oversizeCalls++
	}

	tsa, ok := st.toolCallStats[name]
	if !ok {
		tsa = &toolStatAcc{name: name}
		st.toolCallStats[name] = tsa
	}
	tsa.count++
	tsa.totalBytes += int64(argLen)
	if argLen > tsa.maxArgBytes {
		tsa.maxArgBytes = argLen
	}

	v := guard.InspectToolCall(name, []byte(args), known)
	if v.Echoed {
		st.echoEvents = append(st.echoEvents, EchoEvent{
			Rule:     "echo_in_tool_args",
			Location: "tool_arguments",
			Tool:     name,
			Provider: provider,
			Time:     ts.Format(time.RFC3339),
		})
	}
	if v.Hit != "" {
		st.highRiskEvents = append(st.highRiskEvents, HighRiskEvent{
			Category: v.Hit,
			Tool:     name,
			Provider: provider,
			Model:    model,
			Time:     ts.Format(time.RFC3339),
			Excerpt:  v.Excerpt,
		})
	}
}

func mergeDeepResults(states []*workerState, fileCount, top int) *DeepAnalysisResult {
	res := &DeepAnalysisResult{
		GeneratedAt:        time.Now().Format(time.RFC3339),
		FilesScanned:       fileCount,
		ProtocolDist:       make(map[string]int),
		ModelDist:          make(map[string]int),
		ProviderDist:       make(map[string]int),
		AmplificationHisto: make(map[string]int),
		CoOccurrences:      make(map[string]int),
	}

	outRuleHits := make(map[string]int)
	outRuleRecs := make(map[string]int)
	outRuleMax := make(map[string]int)
	allSamples := make(map[string]*sampleCollector)

	provExposures := make(map[string]map[string]int)
	provReqsWith := make(map[string]int)

	runesA := make(map[rune]int)
	runesB := make(map[rune]int)
	runesC := make(map[rune]int)

	toolStats := make(map[string]*toolStatAcc)
	var tMin, tMax time.Time

	for _, st := range states {
		if st == nil {
			continue
		}
		res.RecordsTotal += st.recordsTotal
		res.ParseErrors += st.parseErrors

		if tMin.IsZero() || (!st.timeMin.IsZero() && st.timeMin.Before(tMin)) {
			tMin = st.timeMin
		}
		if st.timeMax.After(tMax) {
			tMax = st.timeMax
		}

		for k, v := range st.protocolDist {
			res.ProtocolDist[k] += v
		}
		for k, v := range st.modelDist {
			res.ModelDist[k] += v
		}
		for k, v := range st.providerDist {
			res.ProviderDist[k] += v
		}

		for r, h := range st.outRuleHits {
			outRuleHits[r] += h
		}
		for r, recs := range st.outRuleRecordsWith {
			outRuleRecs[r] += recs
		}
		for r, m := range st.outRuleMaxPerRec {
			if m > outRuleMax[r] {
				outRuleMax[r] = m
			}
		}

		for k, sc := range st.outSamples {
			exist, ok := allSamples[k]
			if !ok {
				allSamples[k] = sc
			} else {
				exist.totalHits += sc.totalHits
				exist.recordsWith += sc.recordsWith
				if sc.maxPerRecord > exist.maxPerRecord {
					exist.maxPerRecord = sc.maxPerRecord
				}
				for p := range sc.providers {
					exist.providers[p] = true
				}
				for m := range sc.models {
					exist.models[m] = true
				}
				if sc.firstSeen.Before(exist.firstSeen) {
					exist.firstSeen = sc.firstSeen
				}
				if sc.lastSeen.After(exist.lastSeen) {
					exist.lastSeen = sc.lastSeen
				}
			}
		}

		for p, rmap := range st.providerExposures {
			m, ok := provExposures[p]
			if !ok {
				m = make(map[string]int)
				provExposures[p] = m
			}
			for r, c := range rmap {
				m[r] += c
			}
		}
		for p, count := range st.providerReqsWith {
			provReqsWith[p] += count
		}

		for k, v := range st.amplificationHisto {
			res.AmplificationHisto[k] += v
		}
		for k, v := range st.coOccurrences {
			res.CoOccurrences[k] += v
		}

		for r, c := range st.runesClassA {
			runesA[r] += c
		}
		for r, c := range st.runesClassB {
			runesB[r] += c
		}
		for r, c := range st.runesClassC {
			runesC[r] += c
		}

		res.ToolCallTotals.TotalWithTools += st.totalWithTools
		res.ToolCallTotals.TotalCalls += st.totalToolCalls
		res.ToolCallTotals.OversizeCalls += st.oversizeCalls

		for tname, tsa := range st.toolCallStats {
			exist, ok := toolStats[tname]
			if !ok {
				toolStats[tname] = &toolStatAcc{
					name:        tsa.name,
					count:       tsa.count,
					totalBytes:  tsa.totalBytes,
					maxArgBytes: tsa.maxArgBytes,
				}
			} else {
				exist.count += tsa.count
				exist.totalBytes += tsa.totalBytes
				if tsa.maxArgBytes > exist.maxArgBytes {
					exist.maxArgBytes = tsa.maxArgBytes
				}
			}
		}

		res.HighRiskEvents = append(res.HighRiskEvents, st.highRiskEvents...)
		res.EchoEvents = append(res.EchoEvents, st.echoEvents...)
	}

	res.TimeRangeStart = tMin.Format(time.RFC3339)
	res.TimeRangeEnd = tMax.Format(time.RFC3339)

	ruleUniqFP := make(map[string]int)
	for _, sc := range allSamples {
		ruleUniqFP[sc.rule]++
	}

	for _, r := range guard.DefaultRules() {
		res.OutboundRules = append(res.OutboundRules, ruleAgg{
			Rule:         r.Name,
			Tier:         r.Tier.String(),
			Hits:         outRuleHits[r.Name],
			Uniq:         ruleUniqFP[r.Name],
			RecordsWith:  outRuleRecs[r.Name],
			MaxPerRecord: outRuleMax[r.Name],
		})
	}
	sort.Slice(res.OutboundRules, func(i, j int) bool {
		if res.OutboundRules[i].Hits != res.OutboundRules[j].Hits {
			return res.OutboundRules[i].Hits > res.OutboundRules[j].Hits
		}
		return res.OutboundRules[i].Rule < res.OutboundRules[j].Rule
	})

	var sampleList []*sampleCollector
	for _, sc := range allSamples {
		sampleList = append(sampleList, sc)
	}
	sort.Slice(sampleList, func(i, j int) bool {
		if sampleList[i].totalHits != sampleList[j].totalHits {
			return sampleList[i].totalHits > sampleList[j].totalHits
		}
		return sampleList[i].secret < sampleList[j].secret
	})

	for i, sc := range sampleList {
		if i >= top {
			break
		}
		charset, formable := analyzeCharset(sc.rule, sc.secret)
		res.OutboundSamples = append(res.OutboundSamples, CredentialSample{
			FP:           sc.fp,
			Rule:         sc.rule,
			Tier:         sc.tier,
			Preview:      redact(sc.secret),
			Length:       sc.length,
			CharsetKind:  charset,
			IsFormable:   formable,
			TotalHits:    sc.totalHits,
			RecordsWith:  sc.recordsWith,
			MaxPerRecord: sc.maxPerRecord,
			Providers:    sortedKeys(sc.providers),
			Models:       sortedKeys(sc.models),
			FirstSeen:    sc.firstSeen.Format(time.RFC3339),
			LastSeen:     sc.lastSeen.Format(time.RFC3339),
		})
	}

	for p, rmap := range provExposures {
		totalH := 0
		for _, c := range rmap {
			totalH += c
		}
		res.ProviderExposures = append(res.ProviderExposures, ProviderExposure{
			Provider: p,
			ReqsWith: provReqsWith[p],
			Hits:     totalH,
			RuleHits: rmap,
		})
	}
	sort.Slice(res.ProviderExposures, func(i, j int) bool {
		if res.ProviderExposures[i].Hits != res.ProviderExposures[j].Hits {
			return res.ProviderExposures[i].Hits > res.ProviderExposures[j].Hits
		}
		return res.ProviderExposures[i].Provider < res.ProviderExposures[j].Provider
	})

	res.InboundRunes = []RuneStat{
		buildRuneStat("Class A (Tags, C0 ctrl, DEL)", "strip", runesA),
		buildRuneStat("Class B (Trojan Source, ZWSP, Bidi)", "strip", runesB),
		buildRuneStat("Class C (ZWJ/ZWNJ, Emoji, Bidi mark)", "keep", runesC),
	}

	for _, tsa := range toolStats {
		res.ToolCallStats = append(res.ToolCallStats, ToolCallStat{
			ToolName:    tsa.name,
			Count:       tsa.count,
			TotalBytes:  tsa.totalBytes,
			MaxArgBytes: tsa.maxArgBytes,
		})
	}
	sort.Slice(res.ToolCallStats, func(i, j int) bool {
		if res.ToolCallStats[i].Count != res.ToolCallStats[j].Count {
			return res.ToolCallStats[i].Count > res.ToolCallStats[j].Count
		}
		return res.ToolCallStats[i].ToolName < res.ToolCallStats[j].ToolName
	})

	return res
}

func buildRuneStat(cat, action string, m map[rune]int) RuneStat {
	total := 0
	cMap := make(map[string]int)
	for r, c := range m {
		total += c
		key := fmt.Sprintf("U+%04X (%q)", r, string(r))
		cMap[key] = c
	}
	return RuneStat{Category: cat, Action: action, Total: total, CodeMap: cMap}
}

func sortedKeys(m map[string]bool) []string {
	var list []string
	for k := range m {
		list = append(list, k)
	}
	sort.Strings(list)
	return list
}

func analyzeCharset(rule, secret string) (string, bool) {
	if strings.HasPrefix(secret, "AKIA") {
		return "upperAlnum", false
	}
	if strings.HasPrefix(secret, "-----BEGIN") {
		return "pemArmor", false
	}
	isHex, isBase62 := true, true
	for _, c := range secret {
		switch {
		case (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'):
		default:
			isHex = false
		}
		switch {
		case (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
		default:
			isBase62 = false
		}
	}
	if isHex {
		return "hex", true
	}
	if isBase62 {
		return "base62", true
	}
	return "base64Url", true
}

func findAuditFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if strings.HasPrefix(name, "vmr-audit-") && (strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".jsonl.zst")) {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func topSamples(values map[string]int, n int) []sampleEntry {
	type kv struct {
		v string
		c int
	}
	pairs := make([]kv, 0, len(values))
	for v, c := range values {
		pairs = append(pairs, kv{v, c})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].c != pairs[j].c {
			return pairs[i].c > pairs[j].c
		}
		return pairs[i].v < pairs[j].v
	})
	if len(pairs) > n {
		pairs = pairs[:n]
	}
	out := make([]sampleEntry, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, sampleEntry{Preview: redact(p.v), Length: len(p.v), Count: p.c})
	}
	return out
}

func redact(v string) string {
	const headLen, tailLen, minRedactable = 6, 4, 14
	if len(v) < minRedactable {
		return "(short value, not shown)"
	}
	return v[:headLen] + "..." + v[len(v)-tailLen:]
}

func writeJSON(path string, v any) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0600)
}
