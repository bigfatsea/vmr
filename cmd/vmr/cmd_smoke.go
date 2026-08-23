// Ver 2026-08-23, by Gemini

// cmdSmoke implements `vmr smoke`: fire a real minimal request at every
// configured (virtual model × provider × target model) combination through
// a RUNNING vmr instance — not directly at the upstreams like `vmr
// diagnose` does. Each request goes through the live router, so it
// exercises the full online path (auth, health, conditions, quota
// metering, audit recording) and, crucially, WARMS the quota buckets: a
// per-model Limit's bucket only exists once a request has charged it (see
// internal/quota's lazy allocation), which is why /status shows empty rows
// for models nobody has called yet. `vmr smoke` is the "poke every backend
// once" tool that makes those rows appear and proves end-to-end
// reachability in one pass.
//
// Pinned routing: each smoke request carries X-VMR-Provider /
// X-VMR-Target-Model so it lands on the exact backend it's reporting on
// rather than whatever priority/order routing would pick for that virtual
// model — see internal/router/pin.go. This is also what makes `-provider`
// / `-target-model` useful: filter the run to one provider and re-smoke
// just it after a fix, without touching the routing config.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/router"
)

// smokeTarget is one (virtual model, protocol, provider, target model)
// combination to poke.
type smokeTarget struct {
	model    string // virtual model name, sent as the request's "model"
	protocol string // which ingress protocol this endpoint speaks
	provider string // pinned via X-VMR-Provider
	target   string // pinned via X-VMR-Target-Model
}

// smokeResult is the outcome of one smoke request, in a shape that renders
// both as a table row and as one -json element.
type smokeResult struct {
	Model     string `json:"model"`
	Protocol  string `json:"protocol"`
	Provider  string `json:"provider"`
	Target    string `json:"target_model"`
	OK        bool   `json:"ok"`
	Status    int    `json:"status"`             // HTTP status from vmr (0 = transport failure)
	Endpoint  string `json:"endpoint,omitempty"` // X-VMR-Endpoint header, when vmr answered
	LatencyMS int64  `json:"latency_ms"`
	Detail    string `json:"detail,omitempty"`
}

// smokeRequestBody returns a minimal-but-valid request body for the given
// ingress protocol. These are the byte shapes vmr's own integration tests
// use (server/responses_test.go, anthropic_concurrency_test.go) — kept
// inline because each protocol's mandatory fields differ and there's no
// shared body builder outside the adapters.
func smokeRequestBody(protocol, model string) []byte {
	switch protocol {
	case "anthropic":
		return []byte(`{"model":"` + model + `","max_tokens":4,"messages":[{"role":"user","content":"hi"}]}`)
	case "openai-responses":
		return []byte(`{"model":"` + model + `","input":"hi","max_output_tokens":4}`)
	default: // openai
		// max_tokens: 4 covers upstreams with a strict minimum bound (e.g.
		// Bai/DeepSeek rejects max_tokens <= 2 with a 400 invalid_request_error).
		return []byte(`{"model":"` + model + `","messages":[{"role":"user","content":"hi"}],"max_tokens":4}`)
	}
}

// smokeTargets enumerates every distinct (model, protocol, provider,
// target) combination in config. It follows the same expansion
// router.BuildSnapshot does (outer loop over Models, inner loop over each
// EndpointGroup's Providers × Models) so the smoke set is exactly the set
// real routing could reach. FallbackEndpoints are intentionally excluded:
// smoke reports on what a virtual model declares as its own backends, not
// the shared catch-all tier. Dedups identical combos that would otherwise
// repeat (the same provider+target listed twice for one model). Deterministic
// order: model names sorted, then config order within a model.
func smokeTargets(cfg *config.Config) []smokeTarget {
	var out []smokeTarget
	seen := map[string]bool{}
	for _, name := range core.SortedKeys(cfg.Models) {
		m := cfg.Models[name]
		for _, eg := range m.Endpoints {
			for _, prov := range eg.Providers {
				for _, target := range eg.Models {
					key := name + "\x00" + eg.Protocol + "\x00" + prov + "\x00" + target
					if seen[key] {
						continue
					}
					seen[key] = true
					out = append(out, smokeTarget{model: name, protocol: eg.Protocol, provider: prov, target: target})
				}
			}
		}
	}
	return out
}

// runSmoke sends one minimal request through vmr and reports the result.
func runSmoke(client *http.Client, addr string, apiKey string, tgt smokeTarget) smokeResult {
	body := smokeRequestBody(tgt.protocol, tgt.model)
	url := "http://" + addr + router.IngressPath(tgt.protocol)
	start := time.Now()
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return smokeResult{Model: tgt.model, Protocol: tgt.protocol, Provider: tgt.provider, Target: tgt.target,
			LatencyMS: time.Since(start).Milliseconds(), Detail: "build request: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	// Pin headers steer this request to the exact backend being reported on.
	req.Header.Set(router.PinProviderHeader, tgt.provider)
	req.Header.Set(router.PinModelHeader, tgt.target)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return smokeResult{Model: tgt.model, Protocol: tgt.protocol, Provider: tgt.provider, Target: tgt.target,
			LatencyMS: latency, Detail: "request failed: " + err.Error()}
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	res := smokeResult{Model: tgt.model, Protocol: tgt.protocol, Provider: tgt.provider, Target: tgt.target,
		Status: resp.StatusCode, Endpoint: resp.Header.Get("X-VMR-Endpoint"), LatencyMS: latency,
		OK: resp.StatusCode >= 200 && resp.StatusCode < 300}
	if !res.OK {
		res.Detail = firstLine(string(b))
	}
	return res
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160] + "..."
	}
	return strings.TrimSpace(s)
}

func cmdSmoke(args []string) error {
	fs := flag.NewFlagSet("smoke", flag.ExitOnError)
	path := fs.String("c", "config.yaml", "path to config file")
	addr := fs.String("addr", "", "host:port of the running vmr (default: config's listen)")
	keyFlag := fs.String("key", "", "API key to authenticate with (default: first of config's api_keys)")
	timeout := fs.Duration("timeout", 30*time.Second, "per-request timeout")
	parallel := fs.Int("parallel", 4, "number of smoke requests to run concurrently")
	providerFilter := fs.String("provider", "", "only smoke endpoints of this provider")
	targetFilter := fs.String("target-model", "", "only smoke this upstream target model")
	modelFilter := fs.String("model", "", "only smoke this virtual model")
	jsonOut := fs.Bool("json", false, "print results as a JSON array instead of the table")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	target := *addr
	apiKey := *keyFlag
	if target == "" {
		target = cfg.Listen
	}
	if apiKey == "" && len(cfg.APIKeys) > 0 {
		apiKey = cfg.APIKeys[0]
	}

	targets := smokeTargets(cfg)
	filtered := targets[:0]
	for _, t := range targets {
		if *providerFilter != "" && t.provider != *providerFilter {
			continue
		}
		if *targetFilter != "" && t.target != *targetFilter {
			continue
		}
		if *modelFilter != "" && t.model != *modelFilter {
			continue
		}
		filtered = append(filtered, t)
	}
	if len(filtered) == 0 {
		return fmt.Errorf("no smoke targets match the given filters")
	}

	client := &http.Client{Timeout: *timeout, Transport: &http.Transport{}} // bare Transport: never route through a proxy

	if *parallel < 1 {
		*parallel = 1 // a 0-capacity semaphore would deadlock the worker pool
	}

	// Bounded worker pool over filtered, then reassemble in the original
	// deterministic order so the table/JSON order never depends on
	// completion timing. smoke is the tool reached for when several
	// providers are down at once — sequential would multiply N timeouts
	// into minutes, while -parallel bounds the total wait by the slowest
	// batch instead of the sum.
	results := make([]smokeResult, len(filtered))
	var wg sync.WaitGroup
	sem := make(chan struct{}, *parallel)
	for i, tgt := range filtered {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, tgt smokeTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = runSmoke(client, dialHost(target), apiKey, tgt)
		}(i, tgt)
	}
	wg.Wait()

	ok, fail := 0, 0
	for _, r := range results {
		if r.OK {
			ok++
		} else {
			fail++
		}
	}
	if *jsonOut {
		data, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		printSmokeTable(results)
		fmt.Printf("smoke: %d/%d ok\n", ok, len(results))
	}
	// Exit contract: any failed smoke is a non-zero exit. In -json mode the
	// array is the complete stdout (a parser must not skip past a trailing
	// summary); the error here goes to stderr, so the JSON stream stays
	// clean either way.
	if fail > 0 {
		return fmt.Errorf("%d/%d smoke check(s) failed", fail, len(results))
	}
	return nil
}

func printSmokeTable(results []smokeResult) {
	tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tPROTOCOL\tPROVIDER\tTARGET MODEL\tSTATUS\tLATENCY\tENDPOINT")
	for _, r := range results {
		status := fmt.Sprintf("%d", r.Status)
		if r.Status == 0 {
			status = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Model, r.Protocol, r.Provider, r.Target, status, fmt.Sprintf("%dms", r.LatencyMS), r.Endpoint)
		if !r.OK && r.Detail != "" {
			fmt.Fprintf(tw, "\t\t\t\t\t\t  ! %s\n", r.Detail)
		}
	}
	tw.Flush()
}
