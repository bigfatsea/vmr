// Ver 2026-08-12 23:40, by Opus 5

// §2.5 账户（Provider）消耗与额度: rolls EndpointsAll up by upstream
// account, post-hoc — see rows.go's ProviderRow doc comment for why no new
// streaming accumulation is needed. Mirrors recextract.go's buildTools/
// buildCompactions: a pure function over already-finished buckets, called
// once after the aggregation loop completes (aggregate.go's finishBuckets).
package report

import (
	"sort"
	"strings"
)

// buildProviders rolls rep.EndpointsAll up by provider name. quotas (nil
// when config.yaml wasn't readable, or an account declares no quota) is
// looked up by provider name and copied as-is into ProviderRow.Quota.
func buildProviders(rep *Report2, quotas map[string]ProviderQuotaRef) []ProviderRow {
	byProvider := map[string]*ProviderRow{}
	models := map[string]map[string]bool{}
	durSum := map[string]int64{}
	durN := map[string]int{}

	for _, e := range rep.EndpointsAll {
		provider, model := splitEndpointProviderModelAny(e.Endpoint)
		if provider == "" {
			continue
		}
		pr := byProvider[provider]
		if pr == nil {
			pr = &ProviderRow{Provider: provider}
			byProvider[provider] = pr
			models[provider] = map[string]bool{}
		}
		if model != "" {
			models[provider][model] = true
		}

		pr.Requests += e.Requests
		pr.RequestsOK += e.RequestsOK
		pr.Attempts += e.Attempts
		pr.Failed += e.Failed
		pr.WastedMS += e.WastedMS
		for cls, n := range e.ErrorClasses {
			if pr.ErrorClasses == nil {
				pr.ErrorClasses = map[string]int{}
			}
			pr.ErrorClasses[cls] += n
		}

		pr.TokensIn += e.TokensIn
		pr.TokensInCached += e.TokensInCached
		pr.TokensInCacheWrite += e.TokensInCacheWrite
		pr.TokensInFresh += e.TokensInFresh
		pr.TokensOut += e.TokensOut
		pr.TokensKnown += e.TokensKnown

		durSum[provider] += e.DurMSSum
		durN[provider] += e.RequestsWithDur

		if e.CostEstimate != nil {
			addCost(&pr.CostEstimate, *e.CostEstimate)
		}
	}

	out := make([]ProviderRow, 0, len(byProvider))
	for provider, pr := range byProvider {
		if n := durN[provider]; n > 0 {
			pr.DurMSMean = durSum[provider] / int64(n)
		}
		if pr.Attempts > 0 {
			pr.ErrorRate = float64(pr.Failed) / float64(pr.Attempts) * 100
		}
		pr.CacheEfficiency = cacheEff(pr.TokensInCached, pr.TokensInFresh)

		ms := make([]string, 0, len(models[provider]))
		for m := range models[provider] {
			ms = append(ms, m)
		}
		sort.Strings(ms)
		pr.Models = ms

		if q, ok := quotas[provider]; ok {
			qc := q
			pr.Quota = &qc
		}
		out = append(out, *pr)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].TokensIn != out[j].TokensIn {
			return out[i].TokensIn > out[j].TokensIn
		}
		return out[i].Provider < out[j].Provider // tie-break: deterministic across runs
	})
	return out
}

// splitEndpointProviderModelAny is splitEndpointProviderModel's tolerant
// twin: it accepts both the current "protocol:provider:model" label and the
// "/"-joined format older audit logs used (see attemptUpstream, detail.go).
// Deliberately NOT a change to splitEndpointProviderModel itself — that
// function backs §2's $ estimates, and widening it would silently change
// historical reports' cost numbers for old-format logs, an out-of-scope
// behavior change (see the dev plan's risk table).
func splitEndpointProviderModelAny(endpoint string) (provider, model string) {
	if provider, model = splitEndpointProviderModel(endpoint); provider != "" {
		return provider, model
	}
	parts := strings.SplitN(endpoint, "/", 3)
	if len(parts) < 3 {
		return "", ""
	}
	return parts[1], parts[2]
}
