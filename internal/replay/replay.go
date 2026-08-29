// Ver 2026-07-30, by Sonnet 5

// Package replay implements `vmr replay`: rebuild and resend one request
// from an audit JSONL record, using the exact same adapter.BuildRequest vmr
// itself uses — so the replayed request is byte-for-byte what vmr would
// have sent, not an approximation of it.
package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"vmr/internal/adapter"
	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/ctxgraph"
	"vmr/internal/jsonscan"
	"vmr/internal/quota"
	"vmr/internal/router"
	"vmr/internal/tokenutil"
)

// Options configures one replay run. There are three ways to pick which
// record within AuditPath to replay — Req (a "basename:line" coordinate,
// as published in vmr-requests.json's "req" field or a Manifest's Req), TS
// (an exact-enough timestamp match), or Line (a raw line number, the least
// ergonomic but zero-ambiguity fallback) — and they're mutually exclusive;
// Run validates that. Print short-circuits everything below record
// selection: it prints the resolved record's raw bytes and returns,
// without requiring Provider or any of the request-building/sending
// options.
type Options struct {
	ConfigPath string
	// AuditPath is a single audit .jsonl or .jsonl.zst file — required for
	// TS/Line. For Req it's optional: "" searches the current directory
	// and config.yaml's log_dir for the coordinate's basename; a
	// directory searches just that directory; an exact file path keeps
	// the strict CanonicalPath consistency check.
	AuditPath  string
	Line       int    // 1-based; 0 = the last parsable record in the file; mutually exclusive with TS/Req
	TS         string // exact-enough match against the record's arrival timestamp (see loadRecordByTS); mutually exclusive with Line/Req
	Req        string // "basename:line" coordinate; AuditPath's own canonical basename must match; mutually exclusive with Line/TS
	Print      bool   // print the resolved record's raw JSON to stdout and return — no -provider needed, nothing sent
	Provider   string // required unless Print: name of a providers[] entry to replay against
	Model      string // override the upstream model name; "" = resolved from config
	Protocol   string // override the protocol; "" = the record's own protocol
	Stream     *bool  // nil = use the record's own stream value
	DryRun     bool   // print the request without sending it
	RecordPath string // "" = don't write a replay audit record
	MaxTime    time.Duration
}

// recordView pulls only the fields replay needs out of an audit.Record line.
// It can't reuse audit.Record directly: Message.Body is typed `any`, so
// json.Unmarshal decodes an object body into map[string]interface{} and the
// original bytes are lost — replay needs the raw bytes to resend unchanged.
// TS stays a string (not time.Time) so loadRecordByTS controls parsing
// itself instead of silently absorbing whatever encoding/json does with a
// malformed timestamp.
type recordView struct {
	TS       string `json:"ts"`
	Model    string `json:"model"`
	Protocol string `json:"protocol"`
	Stream   bool   `json:"stream"`
	Client   struct {
		Request struct {
			Headers http.Header     `json:"headers"`
			Body    json.RawMessage `json:"body"`
		} `json:"request"`
	} `json:"client"`
}

// buildReplayEndpoint resolves everything Run needs to actually send (or
// dry-run print) rv's request: the adapter for its protocol, -provider's
// config entry, and the hand-built core.Endpoint BuildRequest/chargeReplay
// need — the same resolution BuildSnapshot performs for live traffic, done
// here directly since ep never goes through BuildSnapshot itself (it's
// built from -provider/-model, not looked up from a virtual model's
// endpoint list).
func buildReplayEndpoint(cfg *config.Config, opts Options, rv *recordView) (ad adapter.Adapter, protocol string, providerCfg config.Provider, ep *core.Endpoint, err error) {
	protocol = opts.Protocol
	if protocol == "" {
		protocol = rv.Protocol
	}
	ad, ok := adapter.Get(protocol)
	if !ok {
		return nil, "", providerCfg, nil, fmt.Errorf("unknown protocol %q (available: %v)", protocol, adapter.Names())
	}
	providerCfg, ok = cfg.ProviderByName(opts.Provider)
	if !ok {
		return nil, "", providerCfg, nil, fmt.Errorf("provider %q not found in %s", opts.Provider, opts.ConfigPath)
	}
	baseURL, ok := providerCfg.BaseURL[protocol]
	if !ok {
		return nil, "", providerCfg, nil, fmt.Errorf("provider %q has no base_url for protocol %q in %s", opts.Provider, protocol, opts.ConfigPath)
	}

	model := opts.Model
	if model == "" {
		if model, err = resolveModel(cfg, protocol, rv.Model, opts.Provider); err != nil {
			return nil, "", providerCfg, nil, err
		}
	}

	ep = &core.Endpoint{
		Provider:    opts.Provider,
		AdapterType: protocol,
		BaseURL:     baseURL,
		APIKey:      providerCfg.APIKey,
		Model:       model,
		// chargeReplay needs these resolved directly, the same as above.
		Quota:       router.BuildQuotaSpecs(cfg.Providers)[opts.Provider],
		PricingRate: cfg.ResolvedPricing[opts.Provider+"\x00"+model],
	}
	ep.FullURL = ad.ResolveURL(baseURL)
	return ad, protocol, providerCfg, ep, nil
}

// Run replays one audit record end to end: load config, locate the record,
// resolve which endpoint to hit, rebuild the request via the same adapter
// path `vmr start` uses, then either print it (DryRun) or send it and print
// the upstream response to stdout.
func Run(ctx context.Context, opts Options, stdout io.Writer) error {
	if opts.Line < 0 {
		return fmt.Errorf("-line must be >= 0")
	}
	if opts.Print {
		return runPrint(opts, stdout)
	}
	if opts.Provider == "" {
		return fmt.Errorf("-provider is required")
	}
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return err
	}
	// audit.Redact below (via -record) must mask the same extra header
	// names live traffic does, or a replay-produced record would leak a
	// custom credential header the running server's config redacts.
	audit.SetExtraRedactHeaders(cfg.ExtraRedactHeaders)

	// Quota-Aware Routing: replay hits the real upstream and consumes real
	// account quota exactly like live traffic does, but until now nothing
	// charged it — see docs/VirtualModelRouter_Design_v4_Quota.md's
	// known-gap entry ② ("vmr replay 消耗真实上游额度但不计费"). This loads
	// the SAME state file `vmr start` uses, charges one successful replay
	// (via chargeReplay below) against it, and flushes once before Run
	// returns — no background flusher needed for a one-shot CLI process. A
	// missing/corrupt state file is never fatal (mirrors cmd_start.go's own
	// handling): quota is a statistics helper, replaying must not fail
	// because of it.
	qreg := quota.NewRegistry(filepath.Join(cfg.LogDir, "vmr-quota.json"))
	if err := qreg.Load(); err != nil {
		fmt.Fprintf(stdout, "WARN quota state: %v (starting from zero)\n", err)
	}
	defer qreg.Flush()
	rv, path, line, err := selectRecord(opts)
	if err != nil {
		return err
	}
	replayOf := fmt.Sprintf("%s:%d", path, line)
	if len(rv.Client.Request.Body) == 0 || rv.Client.Request.Body[0] != '{' {
		return fmt.Errorf("%s: client request body is not a JSON object; only JSON chat/messages requests can be replayed", replayOf)
	}

	ad, protocol, providerCfg, ep, err := buildReplayEndpoint(cfg, opts, rv)
	if err != nil {
		return err
	}

	stream := rv.Stream
	if opts.Stream != nil && *opts.Stream != rv.Stream {
		// -stream must change the bytes actually sent, not just local
		// bookkeeping: the upstream reads the body's own "stream" field, so
		// force it there with the same top-level byte splice RewriteModel
		// uses (adds the key when the record's body never had one). rv is
		// updated in place so everything downstream — the outbound request,
		// the -record audit line — sees the request as replayed.
		newRaw, err := jsonscan.RewriteStream(rv.Client.Request.Body, *opts.Stream)
		if err != nil {
			return fmt.Errorf("-stream: rewrite body: %w", err)
		}
		rv.Client.Request.Body = newRaw
		rv.Stream = *opts.Stream
		stream = *opts.Stream
	}

	creq := &core.CanonicalRequest{
		Model:  rv.Model, // virtual name — BuildRequest rewrites it to ep.Model, same as live traffic
		Stream: stream,
		Raw:    rv.Client.Request.Body,
		Header: replayHeaders(rv.Client.Request.Headers),
	}

	httpReq, outBody, err := ad.BuildRequest(ctx, ep, creq)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	if opts.DryRun {
		printDryRun(stdout, ep, httpReq, outBody)
		return nil
	}

	if opts.MaxTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.MaxTime)
		defer cancel()
	}
	httpReq = httpReq.WithContext(ctx)

	client := router.NewUpstreamClient(cfg, providerCfg, protocol)
	fmt.Fprintf(stdout, "-> %s %s\n", httpReq.Method, httpReq.URL)
	start := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	fmt.Fprintf(stdout, "<- %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))

	// Tee to stdout as it arrives (real streaming for SSE) while also
	// buffering it, so a non-empty -record path can still write the full
	// response without a second round trip.
	var respBuf bytes.Buffer
	if _, err := io.Copy(io.MultiWriter(stdout, &respBuf), resp.Body); err != nil {
		fmt.Fprintf(stdout, "\n(response ended early: %v)\n", err)
	}
	// Measured after the full body transfer, not just headers — matches
	// audit.Record.DurMS's "total wall time" meaning everywhere else
	// (server.go/router.go both measure through the end of the body too).
	// A response whose body trickles in after fast headers would otherwise
	// be recorded as far quicker than it actually was.
	dur := time.Since(start).Round(time.Millisecond)
	fmt.Fprintf(stdout, "(%s)\n", dur)

	// Mirrors forwardSuccess's own gate (router.go: resp.StatusCode >= 400
	// goes to handleErrorResponse instead) — a >=400 response never reaches
	// forwardSuccess on live traffic, so it must not charge here either. A
	// response that dies mid-transfer still charges: the bytes actually
	// sent were genuinely consumed (same reasoning chargeQuota's own doc
	// comment gives for the live path).
	if resp.StatusCode < 400 {
		chargeReplay(qreg, ep, rv.Client.Request.Body, respBuf.Bytes(), time.Now())
	}

	if opts.RecordPath != "" {
		if err := writeReplayRecord(opts.RecordPath, rv, ep, httpReq, outBody, resp, respBuf.Bytes(), replayOf, dur); err != nil {
			return fmt.Errorf("write -record: %w", err)
		}
	}
	return nil
}

// chargeReplay meters one successful replay's consumption against ep's
// provider quota (a no-op when ep.Quota is nil, i.e. -provider has no
// quota: configured) and hands it to router.ChargeResponse — the same
// metric-dispatch/model-multiplier/cost-pricing pipeline chargeQuota uses
// for live traffic (see that function's doc comment). It differs from
// chargeQuota only in how usage is obtained: live traffic sniffs it
// incrementally off a streaming respnorm.NormalizerStream, but replay already
// has the complete request/response bytes in hand (reqBody/respBody), so
// chatmsg.MergeUsageBytes — the exact function respnorm's own usage sniffing
// calls internally — reads it directly from the buffered bytes instead, and
// the degraded estimate comes from tokenutil.Estimate over the raw
// request/response bytes rather than from an incremental byte tally.
//
// Everything after "how usage was obtained" is router.TokenCounters, not a
// second copy of the exact-vs-degraded rule — see that function's doc comment
// for why all three call sites had to converge on one implementation.
func chargeReplay(reg *quota.Registry, ep *core.Endpoint, reqBody, respBody []byte, now time.Time) {
	if ep.Quota == nil {
		return
	}
	u := chatmsg.MergeUsageBytes(respBody, chatmsg.Usage{})
	raw, estimated := router.TokenCounters(u, u.In > 0 || u.Out > 0,
		tokenutil.Estimate(reqBody), tokenutil.Estimate(respBody))
	router.ChargeResponse(reg, ep, raw, estimated, now)
}

// statAuditPathArg classifies the raw AuditPath argument -req needs to
// treat differently from -ts/-line: "" (omitted entirely), a directory (a
// hint to search, not the file itself), or an exact file path (the
// existing strict-consistency-check behavior, unchanged). A path that
// doesn't exist yet at all is left to the eventual open call to report —
// this only distinguishes "is it a directory", so a typo'd file path
// still gets loadRecordByLine's own, more specific error.
func statAuditPathArg(raw string) (path string, isDir bool, err error) {
	if raw == "" {
		return "", false, nil
	}
	if fi, statErr := os.Stat(raw); statErr == nil && fi.IsDir() {
		return raw, true, nil
	}
	return raw, false, nil
}

// resolveReqAuditPath finds the file a -req coordinate's basename refers
// to, searching — in order — dirHint (if given, from
// a directory positional argument), the current directory (when dirHint
// is empty, i.e. the positional argument was omitted entirely), and
// config.yaml's log_dir; each directory is tried with both the bare
// basename and its .zst variant (plain-first: a live/current-day file is
// far more commonly what -print is used to inspect than an
// already-rotated one). config.Load failing is not fatal here — log_dir
// is only ever an ADDITIONAL place to look, dirHint/cwd already cover the
// common case of running this from inside the log directory itself.
func resolveReqAuditPath(basename, dirHint, configPath string) (string, error) {
	dirs := []string{dirHint}
	if dirHint == "" {
		dirs[0] = "."
	}
	if cfg, err := config.Load(configPath); err == nil && cfg.LogDir != "" {
		dirs = append(dirs, cfg.LogDir)
	}
	for _, dir := range dirs {
		for _, name := range []string{basename, basename + ".zst"} {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("-req: couldn't find %q (or its .zst variant) under %v — pass the audit file explicitly as the positional argument", basename, dirs)
}

// selectRecord dispatches to whichever of Options' three locators is set —
// Req, TS or Line — after checking that at most one is (Run's -line default
// is 0, so "not set" and "explicitly line 0" aren't distinguishable, but
// line 0 was never a valid 1-based line number anyway). It returns the
// record plus the (path, 1-based line) it was found at — the same
// coordinate regardless of which locator found it, so callers (Run's
// -record provenance, runPrint's raw re-fetch) don't need to know which
// locator was used.
func selectRecord(opts Options) (rv *recordView, path string, line int, err error) {
	set := 0
	for _, v := range []bool{opts.Req != "", opts.TS != "", opts.Line != 0} {
		if v {
			set++
		}
	}
	if set > 1 {
		return nil, "", 0, fmt.Errorf("-req, -ts and -line are mutually exclusive; pass only one")
	}

	// -req is the one locator that carries its own file identity (the
	// coordinate's basename): it alone can go
	// find its file when the positional argument is omitted or is a
	// directory, rather than requiring it spelled out. -ts/-line have no
	// such identity to search with, so they keep requiring an explicit
	// file (checked below, after this branch returns).
	if opts.Req != "" {
		basename, reqLine, perr := ctxgraph.ParseReqCoord(opts.Req)
		if perr != nil {
			return nil, "", 0, perr
		}
		auditPath, isDir, serr := statAuditPathArg(opts.AuditPath)
		if serr != nil {
			return nil, "", 0, serr
		}
		if auditPath == "" || isDir {
			resolved, rerr := resolveReqAuditPath(basename, auditPath, opts.ConfigPath)
			if rerr != nil {
				return nil, "", 0, rerr
			}
			auditPath = resolved
		} else if got := ctxgraph.CanonicalPath(auditPath); got != basename {
			return nil, "", 0, fmt.Errorf("-req %q refers to %q, but the audit file argument %q canonicalizes to %q", opts.Req, basename, auditPath, got)
		}
		rv, foundN, lerr := loadRecordByLine(auditPath, reqLine)
		if lerr != nil {
			return nil, "", 0, lerr
		}
		return rv, auditPath, foundN, nil
	}
	if opts.AuditPath == "" {
		return nil, "", 0, fmt.Errorf("an audit file argument is required")
	}
	if opts.TS != "" {
		rv, lineNo, terr := loadRecordByTS(opts.AuditPath, opts.TS)
		if terr != nil {
			return nil, "", 0, terr
		}
		return rv, opts.AuditPath, lineNo, nil
	}
	rv, lineNo, lerr := loadRecordByLine(opts.AuditPath, opts.Line)
	if lerr != nil {
		return nil, "", 0, lerr
	}
	return rv, opts.AuditPath, lineNo, nil
}

// runPrint implements Options.Print: locate the record exactly as the
// normal path would (so -req/-ts/-line all work the same way here too),
// then re-fetch and print its raw bytes rather than the partial recordView
// selectRecord decoded — a "read" consumer wants the record as it is on
// disk (every field, including response/attempts), not the subset replay
// itself needs to resend the request.
func runPrint(opts Options, stdout io.Writer) error {
	_, path, line, err := selectRecord(opts)
	if err != nil {
		return err
	}
	raw, err := audit.LineAt(path, line)
	if err != nil {
		return err
	}
	if _, err := stdout.Write(raw); err != nil {
		return err
	}
	_, err = stdout.Write([]byte("\n"))
	return err
}

// loadRecordByTS scans path for the record whose arrival timestamp matches
// ts at millisecond resolution — coarser than `vmr-requests.json`'s own
// "ts" column actually needs (whole-second precision, see aggregate.go's
// buildRequestRow), so a value copied from either that file or the raw
// audit.jsonl's full nanosecond "ts" field locates the same record.
// time.Parse(time.RFC3339, ...) accepts a fractional-second component of
// any length regardless of the layout given, so both precisions parse
// through the same call. Errors if no record
// matches, or if more than one shares that millisecond (rare, but a debug
// tool guessing which one you meant would be worse than asking you to use
// -line instead).
func loadRecordByTS(path, ts string) (*recordView, int, error) {
	want, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return nil, 0, fmt.Errorf("-ts %q: %w", ts, err)
	}
	want = want.Truncate(time.Millisecond)

	rc, err := audit.OpenLogFile(path)
	if err != nil {
		return nil, 0, err
	}
	defer rc.Close()

	var found *recordView
	foundN, matches := 0, 0
	n := 0
	scanErr := audit.ForEachLine(rc, audit.MaxLogLine, func(lb []byte) {
		n++
		var rv recordView
		if err := json.Unmarshal(lb, &rv); err != nil {
			return // malformed line: skip rather than abort the whole scan
		}
		got, err := time.Parse(time.RFC3339, rv.TS)
		if err != nil || !got.Truncate(time.Millisecond).Equal(want) {
			return
		}
		matches++
		found, foundN = &rv, n
	}, nil)
	if scanErr != nil {
		return nil, 0, scanErr
	}
	if matches == 0 {
		return nil, 0, fmt.Errorf("no record with ts=%q (millisecond match) found in %s", ts, path)
	}
	if matches > 1 {
		return nil, 0, fmt.Errorf("%d records match ts=%q within the same millisecond in %s; use -line to disambiguate", matches, ts, path)
	}
	return found, foundN, nil
}

// loadRecordByLine reads path (transparently decompressing .zst) and
// returns the record at the given 1-based line, or the last parsable record
// when line is 0 — the common "replay whatever just failed" workflow
// doesn't require counting lines first. lineNo is the actual 1-based line
// the record came from (useful for -record's ReplayOf provenance and for
// error messages).
func loadRecordByLine(path string, line int) (*recordView, int, error) {
	rc, err := audit.OpenLogFile(path)
	if err != nil {
		return nil, 0, err
	}
	defer rc.Close()

	var last *recordView
	lastN := 0
	n := 0
	scanErr := audit.ForEachLine(rc, audit.MaxLogLine, func(lb []byte) {
		n++
		if line > 0 && n != line {
			return
		}
		var rv recordView
		if err := json.Unmarshal(lb, &rv); err != nil {
			return // malformed line: skip rather than abort the whole scan
		}
		last, lastN = &rv, n
	}, nil)
	if scanErr != nil {
		return nil, 0, scanErr
	}
	if last == nil {
		if line > 0 {
			return nil, 0, fmt.Errorf("line %d not found (or not a parsable record) in %s", line, path)
		}
		return nil, 0, fmt.Errorf("no parsable records found in %s", path)
	}
	return last, lastN, nil
}

// resolveModel looks up the real upstream model name for provider under the
// virtual model the record was sent to — the same lookup config.yaml itself
// encodes (models.<virtualModel>.endpoints[].{protocol,provider,models}).
// Errors (rather than guessing) when provider/protocol match more than one
// candidate model — an EndpointGroup's Models list can legitimately hold
// several, and picking the wrong one would replay against a model the
// record was never sent to.
func resolveModel(cfg *config.Config, protocol, virtualModel, provider string) (string, error) {
	vm, ok := cfg.Models[virtualModel]
	if !ok {
		return "", fmt.Errorf("virtual model %q not found in config; pass -model to specify the upstream model explicitly", virtualModel)
	}
	var candidates []string
	for _, eg := range vm.Endpoints {
		if eg.Protocol != protocol {
			continue
		}
		for _, pn := range eg.Providers {
			if pn == provider {
				candidates = append(candidates, eg.Models...)
				break
			}
		}
	}
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("provider %q has no %s-protocol endpoint under virtual model %q; pass -model to specify the upstream model explicitly", provider, protocol, virtualModel)
	case 1:
		return candidates[0], nil
	default:
		return "", fmt.Errorf("provider %q has %d candidate models under virtual model %q (%v); pass -model to pick one", provider, len(candidates), virtualModel, candidates)
	}
}

func printDryRun(w io.Writer, ep *core.Endpoint, req *http.Request, body []byte) {
	fmt.Fprintf(w, "DRY-RUN  protocol=%s provider=%s model=%s\n", ep.AdapterType, ep.Provider, ep.Model)
	fmt.Fprintf(w, "-> %s %s\n", req.Method, req.URL)
	redacted := audit.Redact(req.Header)
	for _, k := range core.SortedKeys(redacted) {
		for _, v := range redacted[k] {
			fmt.Fprintf(w, "   %s: %s\n", k, v)
		}
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") == nil {
		fmt.Fprintln(w, pretty.String())
	} else {
		w.Write(body)
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "(%d bytes)\n", len(body))
}

// replayHeaders reconstructs the header set a live request would have
// carried. router.FilterClientHeaders alone isn't enough here: its blocklist
// and audit.Redact's separate credentialHeaders list are independently
// maintained and don't fully overlap (e.g. "Api-Key"/"X-Auth-Token" are
// masked by Redact but not blocked from forwarding on live traffic, since
// live headers are always real). The stored value for any
// audit.IsCredentialHeader entry is a masked placeholder like "***c1d4",
// never the real credential — forwarding it to a live upstream would send
// garbage where FilterClientHeaders alone wouldn't catch it, so every such
// header is stripped here too.
func replayHeaders(h http.Header) http.Header {
	out := router.FilterClientHeaders(h)
	for k := range out {
		if audit.IsCredentialHeader(k) {
			out.Del(k)
		}
	}
	return out
}

// writeReplayRecord appends one audit.Record describing this replay to
// path, independent of the main audit chain (`vmr report` never sees it
// unless the caller explicitly points a glob at it). Field layout mirrors
// what a live request produces (server.go/router.go), so a replay record
// reads correctly through the same tools (jq, vmr report, another vmr
// replay): Client.Request.Body is the pre-rewrite body actually replayed
// (virtual model name intact, exactly like live traffic — NOT ep.Model's
// rewritten outBody, which belongs on the Attempt instead); Client.Response
// is what this replay ultimately produced; the attempt's own response body
// is populated only on failure, since on success it is byte-identical to
// Client.Response.Body (same omission router.go's tryOne makes).
func writeReplayRecord(path string, rv *recordView, ep *core.Endpoint, req *http.Request, outBody []byte, resp *http.Response, respBody []byte, replayOf string, dur time.Duration) error {
	attemptResp := &audit.Message{Status: resp.StatusCode, Headers: audit.Redact(resp.Header)}
	if resp.StatusCode >= 400 {
		attemptResp.Body = audit.EncodeBody(respBody)
	}
	durMS := dur.Milliseconds()
	rec := audit.Record{
		TS:       time.Now(),
		DurMS:    durMS,
		Protocol: ep.AdapterType,
		Model:    rv.Model,
		Stream:   rv.Stream,
		Outcome:  audit.OutcomeFor(resp.StatusCode, false), // replay always has a concrete status by the time it writes a record; no cancellation concept here
		ReplayOf: replayOf,
		Client: audit.Exchange{
			Request:  audit.Message{Method: http.MethodPost, Path: router.IngressPath(ep.AdapterType), Body: audit.EncodeBody(rv.Client.Request.Body)},
			Response: &audit.Message{Status: resp.StatusCode, Headers: audit.Redact(resp.Header), Body: audit.EncodeBody(respBody)},
		},
		Attempts: []audit.Attempt{{
			Endpoint: core.EndpointLabel(ep.AdapterType, ep.Provider, ep.Model),
			Protocol: ep.AdapterType,
			Provider: ep.Provider,
			Model:    ep.Model,
			URL:      req.URL.String(),
			DurMS:    durMS,
			Request:  audit.Message{Headers: audit.Redact(req.Header), Body: audit.EncodeBody(outBody)},
			Response: attemptResp,
		}},
	}
	line, err := json.Marshal(&rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}
