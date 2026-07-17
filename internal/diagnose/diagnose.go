// Ver 2026-07-16 21:00, by Fable 5

// Package diagnose implements `vmr diagnose`: config validation plus a
// series of read-only checks (DNS/TLS/proxy reachability, then a real
// minimal request per configured endpoint) that `vmr check` deliberately
// doesn't do — vmr check is a static preview, diagnose actually dials out.
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
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"vmr/internal/adapter"
	"vmr/internal/config"
	"vmr/internal/core"
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
	Phase  string `json:"phase"`  // config | env | connect | route
	Target string `json:"target"` // what this result is about (provider, endpoint, or route entry)
	Status Status `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Options configures one diagnose run.
type Options struct {
	ConfigPath  string
	TestRouting bool // Phase 3: send a real minimal request to every endpoint (default true; -no-test-routing clears it)
	TestTimeout time.Duration
}

// Report is the full output of one diagnose run.
type Report struct {
	Results []Result
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
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	rep := &Report{}
	rep.Results = append(rep.Results, Result{
		Phase: "config", Target: opts.ConfigPath, Status: StatusOK,
		Detail: fmt.Sprintf("%d provider(s), %d model(s)", countNested(cfg.Providers), countNested(cfg.Models)),
	})

	// Phase 2: DNS/TLS/proxy/api_key per provider, up to checkConcurrency at
	// once — sequential here means N unreachable providers each eat a full
	// timeout back to back, which is exactly the scenario diagnose exists
	// to debug quickly.
	type providerCheck struct {
		protocol, name string
		p              config.Provider
	}
	var providerChecks []providerCheck
	for _, protocol := range sortedKeys(cfg.Providers) {
		for _, name := range sortedKeys(cfg.Providers[protocol]) {
			providerChecks = append(providerChecks, providerCheck{protocol, name, cfg.Providers[protocol][name]})
		}
	}
	rep.Results = append(rep.Results, runConcurrent(providerChecks, checkConcurrency, func(c providerCheck) Result {
		return envCheck(ctx, cfg, c.protocol, c.name, c.p)
	})...)

	// Phase 3: a real minimal request per distinct (protocol, provider,
	// model) triple referenced by any virtual model — a provider alone
	// carries no model, only an endpoint does, and the same provider can
	// back several different upstream models across virtual models. Also
	// run with up to checkConcurrency in flight, same reasoning as Phase 2.
	connResults := map[epKey]Result{}
	if opts.TestTimeout <= 0 {
		opts.TestTimeout = 10 * time.Second
	}
	if opts.TestRouting {
		seen := map[epKey]bool{}
		var keys []epKey
		var endpoints []*core.Endpoint
		for _, protocol := range sortedKeys(cfg.Models) {
			for _, name := range sortedKeys(cfg.Models[protocol]) {
				for _, ec := range cfg.Models[protocol][name].Endpoints {
					k := epKey{protocol, ec.Provider, ec.Model}
					if seen[k] {
						continue
					}
					seen[k] = true
					p := cfg.Providers[protocol][ec.Provider]
					keys = append(keys, k)
					endpoints = append(endpoints, &core.Endpoint{Provider: ec.Provider, AdapterType: protocol, BaseURL: p.BaseURL, APIKey: p.APIKey, Model: ec.Model})
				}
			}
		}
		results := runConcurrent(endpoints, checkConcurrency, func(ep *core.Endpoint) Result {
			return testEndpoint(ctx, cfg, ep, opts.TestTimeout)
		})
		for i, r := range results {
			connResults[keys[i]] = r
			rep.Results = append(rep.Results, r)
		}
	}

	// Phase 4: static route preview per virtual model, annotated with
	// whatever Phase 3 found for each endpoint in *this* run.
	for _, protocol := range sortedKeys(snap.Models) {
		for _, name := range sortedKeys(snap.Models[protocol]) {
			route := snap.Models[protocol][name]
			ordered := route.EffectiveOrder()
			for i, ep := range ordered {
				target := fmt.Sprintf("%s [%s] %d. %s/%s", name, protocol, i+1, ep.Provider, ep.Model)
				status, detail := StatusOK, ""
				if r, ok := connResults[epKey{protocol, ep.Provider, ep.Model}]; ok && r.Status != StatusOK {
					status, detail = r.Status, "connectivity test: "+r.Detail
				}
				rep.Results = append(rep.Results, Result{Phase: "route", Target: target, Status: status, Detail: detail})
			}
		}
	}

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
	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return Result{Phase: "env", Target: target, Status: StatusFail, Detail: "invalid base_url: " + err.Error()}
	}
	var parts []string
	status := StatusOK
	fail := func(s string) { parts = append(parts, s); status = StatusFail }
	ok := func(s string) { parts = append(parts, s) }

	if mode, proxyURL := cfg.ProxySpecFor(p); mode == config.ProxyURL {
		pu, err := url.Parse(proxyURL)
		if err != nil {
			fail("proxy:FAIL")
		} else if conn, err := net.DialTimeout("tcp", pu.Host, 5*time.Second); err != nil {
			fail("proxy:FAIL")
		} else {
			conn.Close()
			ok("proxy:ok")
		}
	} else {
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
func testEndpoint(ctx context.Context, cfg *config.Config, ep *core.Endpoint, timeout time.Duration) Result {
	target := ep.AdapterType + "/" + ep.Provider + "/" + ep.Model
	ad, ok := adapter.Get(ep.AdapterType)
	if !ok {
		return Result{Phase: "connect", Target: target, Status: StatusFail, Detail: "unknown adapter " + ep.AdapterType}
	}
	creq := &core.CanonicalRequest{Model: ep.Model, Stream: false, Raw: minimalBody(ep.Model), Header: http.Header{}}
	req, _, err := ad.BuildRequest(ctx, ep, creq)
	if err != nil {
		return Result{Phase: "connect", Target: target, Status: StatusFail, Detail: "build request: " + err.Error()}
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req = req.WithContext(tctx)

	client := router.NewUpstreamClient(cfg, cfg.Providers[ep.AdapterType][ep.Provider])
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Round(time.Millisecond)
	if err != nil {
		return Result{Phase: "connect", Target: target, Status: StatusFail, Detail: fmt.Sprintf("network error (%s): %v", latency, err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

	switch {
	case resp.StatusCode == http.StatusOK:
		return Result{Phase: "connect", Target: target, Status: StatusOK, Detail: fmt.Sprintf("200 OK (%s)", latency)}
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
		return Result{Phase: "connect", Target: target, Status: StatusFail,
			Detail: fmt.Sprintf("%d: %s (%s)", resp.StatusCode, snippet(body), latency)}
	}
}

// minimalBody is a one-token request valid on both supported protocols
// (model/messages/max_tokens are recognized by every OpenAI- and
// Anthropic-compatible provider vmr targets).
func minimalBody(model string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
	})
	return b
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
func runConcurrent[T, R any](items []T, concurrency int, fn func(T) R) []R {
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
			results[i] = fn(item)
		}(i, item)
	}
	wg.Wait()
	return results
}

func countNested[V any](m map[string]map[string]V) int {
	n := 0
	for _, byName := range m {
		n += len(byName)
	}
	return n
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// FormatTable renders a Report as simple line-based text, matching the
// existing `vmr check`/`vmr status` style (no box-drawing tables).
func FormatTable(rep *Report) string {
	var b strings.Builder
	byPhase := map[string][]Result{}
	var phases []string
	for _, r := range rep.Results {
		if _, ok := byPhase[r.Phase]; !ok {
			phases = append(phases, r.Phase)
		}
		byPhase[r.Phase] = append(byPhase[r.Phase], r)
	}
	for _, phase := range phases {
		for _, r := range byPhase[phase] {
			line := fmt.Sprintf("[%s] %s: %s", phase, r.Target, r.Status)
			if r.Detail != "" {
				line += " — " + r.Detail
			}
			fmt.Fprintln(&b, line)
		}
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
	fmt.Fprintf(&b, "\nSummary: %d ok, %d warn, %d fail\n", ok, warn, fail)
	return b.String()
}
