// Ver 2026-07-30, by Sonnet 5

// Package diagnose implements `vmr diagnose`: config validation, the same
// config.Check consistency scan `vmr check` runs, and — only once that scan
// comes back clean — a series of read-only checks that touch the network
// (DNS/TLS/proxy reachability, then a real minimal request per configured
// endpoint) that `vmr check` deliberately never does. A config with
// consistency issues (missing api_key, a proxy contradiction, …) skips
// straight past both network phases: there is no point dialing out for
// endpoints a human still needs to fix the declaration of first.
//
// diagnose never touches vmr's own audit log or health registry: it runs as
// a one-shot process with no access to a live `vmr start` instance's
// in-memory health state (health.Registry is per-process, never
// persisted — see internal/health), so it can't report a live instance's
// current cooldowns. For that, use `vmr status` against a running instance;
// diagnose instead annotates its own route preview with what its own
// Phase 3 connectivity test found in this run.
package diagnose

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"vmr/internal/adapter"
	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/fmtutil"
	"vmr/internal/probe"
	"vmr/internal/router"
)

// checkConcurrency bounds how many Phase 2/3 checks run at once. Diagnose is
// exactly the tool reached for when several providers are unreachable at
// the same time — run sequentially, N blackholed providers each eating a
// full timeout adds up to minutes; run with this many in flight, the total
// wait is bounded by the single slowest check, not the sum of all of them.
const checkConcurrency = 8

// Status is the outcome of one diagnostic check.
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

// Result is one line of diagnostic output.
type Result struct {
	Phase  string `json:"phase"`           // config | env | connect | route
	Group  string `json:"group,omitempty"` // route phase only: "<virtual model> [<protocol>]", groups its endpoints together
	Target string `json:"target"`          // what this result is about (provider, endpoint, or — within a route Group — just that endpoint's rank/name)
	Status Status `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Options configures one diagnose run.
type Options struct {
	ConfigPath  string
	TestRouting bool // Phase 3: send a real minimal request to every endpoint (default true; -no-test-routing clears it)
	TestTimeout time.Duration
	// Progress, if non-nil, gets one line per Phase 2/3 check as it
	// completes (plus a short header when each phase starts) — Phase 2/3
	// are the only phases that dial out and can take multiple seconds, so
	// they're the only ones worth narrating. Results still land in the
	// final Report in deterministic (not arrival) order regardless; this is
	// purely a "something is happening" signal for an interactive caller,
	// so cmdDiagnose points it at os.Stderr, keeping stdout (including
	// -json output) exactly what it was before.
	Progress io.Writer
}

// Report is the full output of one diagnose run.
type Report struct {
	Results []Result
	RanAt   time.Time // when this run finished — FormatTable's summary line uses it; not part of -json output
}

// FailCount reports how many results are StatusFail — used for the process
// exit code.
func (r *Report) FailCount() int {
	n := 0
	for _, res := range r.Results {
		if res.Status == StatusFail {
			n++
		}
	}
	return n
}

// Run executes all phases. A nil *Report means Phase 1 (config load) itself
// failed — there is nothing else to check or print.
func Run(ctx context.Context, opts Options) (*Report, error) {
	if opts.Progress != nil {
		printRule(opts.Progress, "vmr diagnose")
	}
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	// Absolute path, not whatever -c was given verbatim: "config.yaml"
	// alone doesn't say which directory it resolved from, and diagnose is
	// exactly the tool reached for when something looks misconfigured — the
	// first thing to rule out is "wrong file". Falls back to the given
	// path unresolved if Abs somehow fails (unreadable cwd); never fatal.
	configPath := opts.ConfigPath
	if abs, err := filepath.Abs(opts.ConfigPath); err == nil {
		configPath = abs
	}
	// No progress line for this one: config load is synchronous and already
	// happened above (Run would have returned by now if it failed) — by the
	// time there's anything to narrate, its result is already fully visible
	// as the first entry of the Result section below. Only Phase 2/3 dial
	// out and take long enough to be worth a separate live announcement.
	rep := &Report{}
	rep.Results = append(rep.Results, Result{
		Phase: "config", Target: configPath, Status: StatusOK,
		Detail: fmt.Sprintf("%d provider(s), %d virtual model(s)", len(cfg.Providers), len(cfg.Models)),
	})

	// Phase 1b: the same consistency scan `vmr check` runs (missing
	// api_key, a proxy contradiction that silently resolves to direct, a
	// duplicate endpoint, …) — config that's structurally valid
	// (BuildSnapshot above already succeeded) but operationally broken.
	// A SeverityError issue skips Phase 2/3 entirely: there is no point
	// dialing out to test connectivity for endpoints a human still needs
	// to fix the declaration of first — see cfg.Check's doc comment. A
	// SeverityWarning issue (e.g. a non-loopback listen with no api_keys) is
	// still reported here, but as StatusWarn, and never gates the network
	// phases — it's a risk worth surfacing, not a broken config.
	checkIssues := cfg.Check()
	for _, is := range checkIssues {
		target := "global"
		switch {
		case is.Field == "endpoint":
			target = fmt.Sprintf("model %s: %s", is.Model, is.Endpoint)
		case is.Provider != "":
			target = "provider " + is.Provider
		case is.Model != "":
			target = "model " + is.Model
		}
		status := StatusFail
		if is.Severity == config.SeverityWarning {
			status = StatusWarn
		}
		rep.Results = append(rep.Results, Result{Phase: "check", Target: target, Status: status, Detail: is.Message})
	}
	runNetworkChecks := !config.HasErrors(checkIssues)
	if !runNetworkChecks && opts.Progress != nil {
		// errCount, not len(checkIssues): a SeverityWarning issue riding
		// alongside a real error didn't cause this skip, so it shouldn't
		// inflate the count attributed to it.
		errCount := 0
		for _, is := range checkIssues {
			if is.Severity != config.SeverityWarning {
				errCount++
			}
		}
		fmt.Fprintf(opts.Progress, "Consistency check: %d issue(s) found — skipping Environment/Connectivity (real network I/O)\n", errCount)
	}

	// Phase 2: DNS/TLS/proxy/api_key per provider, up to checkConcurrency at
	// once — sequential here means N unreachable providers each eat a full
	// timeout back to back, which is exactly the scenario diagnose exists
	// to debug quickly. One check per (protocol, provider) pair: a provider
	// speaking both protocols gets checked once per declared base_url.
	type providerCheck struct {
		protocol, name string
		p              config.Provider
	}
	var providerChecks []providerCheck
	providers := append([]config.Provider(nil), cfg.Providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
	for _, p := range providers {
		for _, protocol := range core.SortedKeys(p.BaseURL) {
			providerChecks = append(providerChecks, providerCheck{protocol, p.Name, p})
		}
	}
	onResult := progressPrinter(opts.Progress)
	if runNetworkChecks {
		if opts.Progress != nil {
			fmt.Fprintf(opts.Progress, "Environment: checking %d provider(s)...\n", len(providerChecks))
		}
		rep.Results = append(rep.Results, runConcurrent(providerChecks, checkConcurrency, func(c providerCheck) Result {
			return envCheck(ctx, cfg, c.protocol, c.name, c.p)
		}, onResult)...)
	}

	// Phase 3: a real minimal request per distinct (protocol, provider,
	// model) triple referenced by any virtual model — a provider alone
	// carries no model, only an endpoint does, and the same provider can
	// back several different upstream models across virtual models. Also
	// run with up to checkConcurrency in flight, same reasoning as Phase 2.
	connResults := map[epKey]Result{}
	if opts.TestTimeout <= 0 {
		opts.TestTimeout = 15 * time.Second
	}
	if opts.TestRouting && runNetworkChecks {
		// Collect every distinct (protocol, provider, model) triple first,
		// then sort by protocol/provider/model — so the same provider's
		// endpoints land next to each other in the report instead of
		// scattered across wherever each virtual model listed them.
		// The map value is the endpoint-group's RoleMap (first endpoint-group
		// referencing a given triple wins — the same triple declared with two
		// different role_maps across virtual models is an edge case not
		// worth reconciling here): testEndpoint needs it both to apply the
		// same rewrite real traffic would get and, on failure, to word its
		// hint correctly.
		seen := map[epKey]map[string]string{}
		for _, name := range core.SortedKeys(cfg.Models) {
			for _, eg := range cfg.Models[name].Endpoints {
				for _, pn := range eg.Providers {
					for _, mn := range eg.Models {
						k := epKey{eg.Protocol, pn, mn}
						if _, ok := seen[k]; !ok {
							seen[k] = eg.RoleMap
						}
					}
				}
			}
		}
		keys := make([]epKey, 0, len(seen))
		for k := range seen {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].protocol != keys[j].protocol {
				return keys[i].protocol < keys[j].protocol
			}
			if keys[i].provider != keys[j].provider {
				return keys[i].provider < keys[j].provider
			}
			return keys[i].model < keys[j].model
		})
		endpoints := make([]*core.Endpoint, len(keys))
		for i, k := range keys {
			p, _ := cfg.ProviderByName(k.provider)
			baseURL := p.BaseURL[k.protocol]
			ad, _ := adapter.Get(k.protocol)
			ep := &core.Endpoint{Provider: k.provider, AdapterType: k.protocol, BaseURL: baseURL, APIKey: p.APIKey, Model: k.model, RoleMap: seen[k]}
			if ad != nil {
				ep.FullURL = ad.ResolveURL(baseURL)
			}
			endpoints[i] = ep
		}
		if opts.Progress != nil {
			fmt.Fprintf(opts.Progress, "Connectivity: probing %d endpoint(s) (timeout %s each)...\n", len(endpoints), opts.TestTimeout)
		}
		results := runConcurrent(endpoints, checkConcurrency, func(ep *core.Endpoint) Result {
			return testEndpoint(ctx, cfg, ep, opts.TestTimeout)
		}, onResult)
		for i, r := range results {
			connResults[keys[i]] = r
			rep.Results = append(rep.Results, r)
		}
	}

	// Phase 4: static route preview per virtual model, annotated with
	// whatever Phase 3 found for each endpoint in *this* run.
	for _, protocol := range core.SortedKeys(snap.Models) {
		for _, name := range core.SortedKeys(snap.Models[protocol]) {
			route := snap.Models[protocol][name]
			ordered := route.EffectiveOrder()
			group := fmt.Sprintf("%s [%s]", name, protocol)
			for _, ep := range ordered {
				target := fmt.Sprintf("- p=%d. %s/%s", ep.Priority, ep.Provider, ep.Model)
				status, detail := StatusOK, ""
				if r, ok := connResults[epKey{protocol, ep.Provider, ep.Model}]; ok && r.Status != StatusOK {
					status, detail = r.Status, "connectivity test: "+r.Detail
				}
				rep.Results = append(rep.Results, Result{Phase: "route", Group: group, Target: target, Status: status, Detail: detail})
			}
		}
	}

	rep.RanAt = time.Now()
	return rep, nil
}

type epKey struct{ protocol, provider, model string }

// envCheck reports whether this provider is reachable without sending an
// LLM request: DNS + TLS for a direct connection, or just the proxy's own
// reachability when this provider's traffic actually goes through one
// (router.NewUpstreamClient never dials the upstream host directly in that
// case — testing DNS/TLS against it would answer a question nothing in the
// real request path ever asks, and would misreport a proxy-only-reachable
// provider as broken). Also reports whether an api_key is present.
func envCheck(ctx context.Context, cfg *config.Config, protocol, name string, p config.Provider) Result {
	target := protocol + "/" + name
	u, err := url.Parse(p.BaseURL[protocol])
	if err != nil {
		return Result{Phase: "env", Target: target, Status: StatusFail, Detail: "invalid base_url: " + err.Error()}
	}
	var parts []string
	status := StatusOK
	fail := func(s string) { parts = append(parts, s); status = StatusFail }
	ok := func(s string) { parts = append(parts, s) }

	if mode, proxyURL := cfg.ProxySpecFor(p, protocol); mode == config.ProxyURL {
		ok("proxy:yes")
		pu, err := url.Parse(proxyURL)
		if err != nil {
			fail("proxy_reachable:FAIL")
		} else if conn, err := net.DialTimeout("tcp", pu.Host, 5*time.Second); err != nil {
			fail("proxy_reachable:FAIL")
		} else {
			conn.Close()
			ok("proxy_reachable:ok")
		}
	} else {
		ok("proxy:no")
		host := u.Hostname()
		dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, dnsErr := (&net.Resolver{}).LookupHost(dctx, host)
		cancel()
		if dnsErr != nil {
			fail("dns:FAIL")
		} else {
			ok("dns:ok")
		}
		if u.Scheme == "https" {
			port := u.Port()
			if port == "" {
				port = "443"
			}
			d := &net.Dialer{Timeout: 5 * time.Second}
			conn, err := tls.DialWithDialer(d, "tcp", net.JoinHostPort(host, port), nil)
			if err != nil {
				fail("tls:FAIL")
			} else {
				conn.Close()
				ok("tls:ok")
			}
		}
	}
	if p.APIKey == "" {
		parts = append(parts, "api_key:EMPTY")
		if status == StatusOK {
			status = StatusWarn
		}
	} else {
		ok("api_key:set")
	}
	return Result{Phase: "env", Target: target, Status: status, Detail: strings.Join(parts, " ")}
}

// testEndpoint sends one minimal real request through the exact same
// adapter.BuildRequest path vmr's own server uses, and classifies the
// response the way a user configuring vmr would want explained.
//
// An openai-protocol endpoint is probed with role "developer" instead of
// "user" — OpenAI's o1/o3-series introduced that role, and some self-
// described-OpenAI-compatible providers reject it outright (see
// config.example.yaml's role_map). There is no separate check for this: a
// provider that can't handle the role real "developer"-role clients send
// (or a missing role_map to rewrite it away first) is exactly as broken as
// any other liveness failure, so it fails the one connectivity check
// instead of a second one alongside it. Anthropic-protocol endpoints keep
// probing with plain "user" — "developer" is an OpenAI-only role, no
// Anthropic client ever sends it.
func testEndpoint(ctx context.Context, cfg *config.Config, ep *core.Endpoint, timeout time.Duration) Result {
	target := ep.AdapterType + "/" + ep.Provider + "/" + ep.Model
	ad, ok := adapter.Get(ep.AdapterType)
	if !ok {
		return Result{Phase: "connect", Target: target, Status: StatusFail, Detail: "unknown adapter " + ep.AdapterType}
	}
	var probeBody json.RawMessage
	var nonce string
	switch ep.AdapterType {
	case "openai":
		probeBody, nonce = probe.RoleCompatRequest(ep.Model, "developer")
	case "openai-responses":
		// Responses-shaped body (top-level "input", not "messages") — see
		// probe.ResponsesRequest's doc comment. No role-compat variant here:
		// unlike Chat Completions' single-vs-two-message shape ambiguity
		// (RoleCompatRequest's reason for existing), a rejected role_map
		// target for this protocol fails this one connectivity check the
		// same way any other liveness failure does, no second probe needed.
		probeBody, nonce = probe.ResponsesRequest(ep.Model)
	default:
		probeBody, nonce = probe.Request(ep.Model)
	}
	creq := &core.CanonicalRequest{Model: ep.Model, Stream: false, Raw: probeBody, Header: http.Header{}}
	req, _, err := ad.BuildRequest(ctx, ep, creq)
	if err != nil {
		return Result{Phase: "connect", Target: target, Status: StatusFail, Detail: "build request: " + err.Error()}
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req = req.WithContext(tctx)

	p, _ := cfg.ProviderByName(ep.Provider)
	client := router.NewUpstreamClient(cfg, p, ep.AdapterType)
	start := time.Now()
	resp, err := client.Do(req)
	latency := formatSeconds(time.Since(start))
	if err != nil {
		return Result{Phase: "connect", Target: target, Status: StatusFail, Detail: fmt.Sprintf("network error (%s): %v", latency, err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

	switch {
	case resp.StatusCode == http.StatusOK:
		// A 200 alone doesn't prove the model actually ran: a relay/gateway
		// hop can answer with a cached or canned response while the request
		// never really reached the model. probe.Request asked for a specific
		// nonce back; its absence downgrades to a warning (not a failure —
		// the endpoint IS reachable and DID answer, just not verifiably from
		// a fresh completion) rather than blocking `vmr diagnose` on a vendor
		// that, say, trims or paraphrases short outputs.
		if !probe.Echoed(body, nonce) {
			return Result{Phase: "connect", Target: target, Status: StatusWarn,
				Detail: fmt.Sprintf("200 OK but echo not verified — response may not be a fresh completion (%s): %s", latency, snippet(body))}
		}
		return Result{Phase: "connect", Target: target, Status: StatusOK, Detail: fmt.Sprintf("200 OK, echo verified (%s)", latency)}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return Result{Phase: "connect", Target: target, Status: StatusFail,
			Detail: fmt.Sprintf("%d auth failed — check api_key (%s)", resp.StatusCode, latency)}
	case resp.StatusCode == http.StatusNotFound:
		return Result{Phase: "connect", Target: target, Status: StatusFail,
			Detail: fmt.Sprintf("404 model not found — check model spelling (%s)", latency)}
	case resp.StatusCode == http.StatusTooManyRequests:
		return Result{Phase: "connect", Target: target, Status: StatusWarn,
			Detail: fmt.Sprintf("429 rate-limited (%s)", latency)}
	case resp.StatusCode >= 500:
		return Result{Phase: "connect", Target: target, Status: StatusFail,
			Detail: fmt.Sprintf("%d upstream error (%s)", resp.StatusCode, latency)}
	default:
		// For an openai-protocol endpoint this default case is also where a
		// rejected "developer" role lands (typically a 400) — add a role_map
		// hint precisely when that's a plausible explanation, so the fix is
		// obvious instead of just "some 400-something happened".
		hint := ""
		if ep.AdapterType == "openai" {
			if len(ep.RoleMap) == 0 {
				hint = ` — no role_map configured; if this provider rejects the "developer" role, add role_map: {developer: system}`
			} else {
				hint = fmt.Sprintf(" — role_map %v is configured; if this is a rejected \"developer\" role, check its target role name", ep.RoleMap)
			}
		}
		return Result{Phase: "connect", Target: target, Status: StatusFail,
			Detail: fmt.Sprintf("%d: %s (%s)%s", resp.StatusCode, snippet(body), latency, hint)}
	}
}

// formatSeconds renders d in seconds at millisecond precision (e.g. "0.141s",
// "1.068s") — one unit for every Connectivity/Routing latency, instead of
// Duration.String()'s default of switching to "141ms" below one second and
// "1.068s" above it. A column where some rows read "141ms" and others
// "1.068s" doesn't scan as a column; same unit throughout does. Thin local
// alias for fmtutil.FmtSeconds (3 decimals — diagnose cares about sub-10ms
// differences the live router log's 2-decimal dur= column doesn't).
func formatSeconds(d time.Duration) string {
	return fmtutil.FmtSeconds(d, 3)
}

func snippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	s = strings.Join(strings.Fields(s), " ") // collapse whitespace/newlines for a one-line detail
	if len(s) > 120 {
		// Rune-boundary cut: domestic providers answer with Chinese error
		// text, and a mid-rune byte slice would print invalid UTF-8.
		n := 120
		for n > 0 && !utf8.RuneStart(s[n]) {
			n--
		}
		s = s[:n] + "…"
	}
	return s
}

// runConcurrent runs fn over items with up to `concurrency` in flight at
// once, returning results in the same order as items regardless of which
// finishes first — each goroutine writes only to its own pre-allocated
// slot, so callers get deterministic output ordering with no locking.
// onResult, if non-nil, fires as each result lands (arrival order, i.e. not
// the deterministic order results are returned in) — the live-progress
// hook; it must be safe to call concurrently, since every in-flight
// goroutine calls it independently.
func runConcurrent[T, R any](items []T, concurrency int, fn func(T) R, onResult func(R)) []R {
	results := make([]R, len(items))
	if len(items) == 0 {
		return results
	}
	if concurrency <= 0 || concurrency > len(items) {
		concurrency = len(items)
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, item T) {
			defer wg.Done()
			defer func() { <-sem }()
			r := fn(item)
			results[i] = r
			if onResult != nil {
				onResult(r)
			}
		}(i, item)
	}
	wg.Wait()
	return results
}

// progressPrinter returns a runConcurrent onResult callback that writes one
// terse "status  target" line per completed check to w, or nil if w is nil
// (the common case: progress is opt-in). A successful check prints "Done"
// rather than "ok" — mid-run, "this one finished" is the useful signal;
// "ok" reads as a verdict, which belongs to the final Result section once
// everything is known, not to a line that might be one of many still
// in flight. Warn/fail keep their real status: those are worth flagging
// the moment they're seen, not held back for the summary. fmt.Fprintf on a
// single io.Writer from multiple goroutines needs no external locking
// because each call is one Write — os.File/bytes.Buffer-backed writers
// don't interleave partial writes from concurrent callers at this size.
func progressPrinter(w io.Writer) func(Result) {
	if w == nil {
		return nil
	}
	return func(r Result) {
		label := string(r.Status)
		if r.Status == StatusOK {
			label = "Done"
		}
		fmt.Fprintf(w, "  %-4s  %s\n", label, r.Target)
	}
}

// ruleWidth matches the "=" rule vmrBanner/logStop use around the server's
// own start/stop markers (cmd/vmr/main.go) — one consistent horizontal-rule
// convention across every place vmr prints a section title to a terminal,
// not a width invented just for diagnose.
const ruleWidth = 50

// rule is one horizontal "=" line, ruleWidth wide.
func rule() string { return strings.Repeat("=", ruleWidth) }

// printRule writes a title bracketed by rule()s — used for the two section
// headings a `vmr diagnose` run produces (the opening title and, in
// FormatTable, "Result"): plain text reads as just another log line at this
// point, easy to miss scrolling past; a rule above and below is the same
// low-tech emphasis vmrBanner already uses for "a new phase of output starts
// here", just without the full ASCII art (this fires on every run, not once
// per server process).
func printRule(w io.Writer, title string) {
	fmt.Fprintln(w, rule())
	fmt.Fprintln(w, title)
	fmt.Fprintln(w, rule())
}

// phaseTitles names each phase's section header in FormatTable's output —
// deliberately not the bare Phase string (e.g. "env"), which reads fine as
// a JSON discriminator but not as prose a human scans top to bottom.
var phaseTitles = map[string]string{
	"config":  "Config",
	"check":   "Consistency Check",
	"env":     "Environment",
	"connect": "Connectivity",
	"route":   "Routing",
}

// FormatTable renders a Report as a fixed-width, grouped listing under one
// "Result" heading: one section per phase (a blank line apart), each
// section's target/status columns aligned to that section's own longest
// entries — a global column width across every phase would pad
// "config.yaml" out to the width of the longest connectivity endpoint
// string for no benefit. The route phase additionally groups its rows
// under a "<virtual model> [<protocol>]" sub-header per Result.Group, since
// that's the one phase whose rows are naturally nested (several endpoints
// belong to one virtual model). This is the settled report, distinct from
// the live "vmr diagnose" / Environment / Connectivity narration Options.
// Progress prints while Run is still in flight (see progressPrinter) — that
// text never reaches FormatTable, and vice versa.
func FormatTable(rep *Report) string {
	var b strings.Builder
	fmt.Fprintln(&b)
	printRule(&b, "Result")
	byPhase := map[string][]Result{}
	var phases []string
	for _, r := range rep.Results {
		if _, ok := byPhase[r.Phase]; !ok {
			phases = append(phases, r.Phase)
		}
		byPhase[r.Phase] = append(byPhase[r.Phase], r)
	}
	for i, phase := range phases {
		if i > 0 {
			fmt.Fprintln(&b)
		}
		title := phaseTitles[phase]
		if title == "" {
			title = phase
		}
		fmt.Fprintln(&b, title)
		writeRows(&b, byPhase[phase])
	}
	ok, warn, fail := 0, 0, 0
	for _, r := range rep.Results {
		switch r.Status {
		case StatusOK:
			ok++
		case StatusWarn:
			warn++
		case StatusFail:
			fail++
		}
	}
	fmt.Fprintf(&b, "\nSummary: %d ok, %d warn, %d fail (%s)\n", ok, warn, fail, rep.RanAt.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05"))
	return b.String()
}

// writeRows renders one phase's rows: a fixed target-column width computed
// from this phase's own longest Target (status is always ok/warn/fail, so 4
// chars covers every value), a Group sub-header printed once whenever it
// changes, and Group-bearing rows indented one level deeper than the rest.
func writeRows(b *strings.Builder, rows []Result) {
	maxTarget := 0
	for _, r := range rows {
		if l := len(r.Target); l > maxTarget {
			maxTarget = l
		}
	}
	const maxStatus = 4 // len("fail") == len("warn")
	lastGroup := ""
	for _, r := range rows {
		indent := "  "
		if r.Group != "" {
			if r.Group != lastGroup {
				fmt.Fprintf(b, "  %s\n", r.Group)
				lastGroup = r.Group
			}
			indent = "    "
		}
		line := fmt.Sprintf("%s%-*s  %-*s", indent, maxTarget, r.Target, maxStatus, string(r.Status))
		if r.Detail != "" {
			line += "  " + r.Detail
		}
		fmt.Fprintln(b, strings.TrimRight(line, " "))
	}
}
