// Ver 2026-08-07, by Opus 5

package router

import (
	"testing"

	"vmr/internal/config"
	"vmr/internal/core"

	_ "vmr/internal/adapter/openai"
)

const quotaSnapYAML = `
listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai: https://example.com}
    api_key: k1
    quota:
      limits:
        - {metric: requests, every: 1mo, since: 2026-08-01, amount: 90000}
  - name: p2
    base_url: {openai: https://example2.com}
    api_key: k2
models:
  m1:
    endpoints:
      - protocol: openai
        providers: [p1]
        models: [model-a, model-b]
      - protocol: openai
        providers: [p2]
        models: [model-c]
`

func TestBuildSnapshot_QuotaSpecAttachedAndSharedPerProvider(t *testing.T) {
	cfg, err := config.Parse([]byte(quotaSnapYAML))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	route := snap.Models["openai"]["m1"]
	var p1Endpoints, p2Endpoints []*core.Endpoint
	for _, ep := range route.Endpoints {
		switch ep.Provider {
		case "p1":
			p1Endpoints = append(p1Endpoints, ep)
		case "p2":
			p2Endpoints = append(p2Endpoints, ep)
		}
	}
	if len(p1Endpoints) != 2 {
		t.Fatalf("expected 2 p1 endpoints (model-a, model-b), got %d", len(p1Endpoints))
	}
	for _, ep := range p1Endpoints {
		if ep.Quota == nil {
			t.Fatalf("p1 endpoint %s: Quota is nil, want the configured spec", ep.Name())
		}
		if len(ep.Quota.Limits) != 1 || ep.Quota.Limits[0].Metric != core.MetricRequests || ep.Quota.Limits[0].Amount != 90000 {
			t.Fatalf("p1 endpoint %s: Quota = %+v", ep.Name(), ep.Quota)
		}
	}
	// Same provider, different endpoints (model-a vs model-b): must be the
	// SAME pointer, not two independently-built equal structs — quota is an
	// account property (see core.Endpoint.Quota's doc comment), and
	// scoring/reordering assume identity is meaningful.
	if p1Endpoints[0].Quota != p1Endpoints[1].Quota {
		t.Errorf("p1's two endpoints have different *core.QuotaSpec pointers, want the same one shared")
	}
	for _, ep := range p2Endpoints {
		if ep.Quota != nil {
			t.Errorf("p2 endpoint %s: Quota = %+v, want nil (no quota: configured)", ep.Name(), ep.Quota)
		}
	}
}

func TestBuildSnapshot_NoQuotaAnywhere_AllEndpointsNil(t *testing.T) {
	yaml := `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: https://example.com}, api_key: k1}
models:
  m1:
    endpoints: [{protocol: openai, providers: [p1], models: [m]}]
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range snap.Models["openai"]["m1"].Endpoints {
		if ep.Quota != nil {
			t.Errorf("endpoint %s: Quota = %+v, want nil", ep.Name(), ep.Quota)
		}
	}
}
