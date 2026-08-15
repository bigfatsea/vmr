// Ver 2026-08-12 23:40, by Opus 5

// §5.5 按客户端的上游归属: for each client_key_tag, which upstream
// endpoints (protocol:provider:model) it actually hit and how many tokens
// landed on each. Grouped by client, not a client×endpoint matrix — see
// docs/future-strategy/vmr_report_provider_client_cost_analysis_sonnet-5.md's
// §3.2 for why: the matrix's sparse cells add nothing a grouped table
// doesn't already answer.
//
// Must be a streaming collector (unlike provider.go's post-hoc roll-up):
// no existing bucket is keyed by (client, endpoint), so there is no
// finished result to roll up from.
package report

import "sort"

// clientEndpointCollector buffers per-(client,endpoint) token/request
// counts during Build's pass — mirrors stickyCollector's shape (sticky.go).
type clientEndpointCollector struct {
	byKey map[string]*ClientEndpointRow // "clientKey\x00endpoint" -> row
}

func newClientEndpointCollector() *clientEndpointCollector {
	return &clientEndpointCollector{byKey: map[string]*ClientEndpointRow{}}
}

func (c *clientEndpointCollector) add(rc *rec2) {
	if rc.clientKey == "" || rc.endpoint == "" {
		return
	}
	key := rc.clientKey + "\x00" + rc.endpoint
	row := c.byKey[key]
	if row == nil {
		row = &ClientEndpointRow{ClientKey: rc.clientKey, Endpoint: rc.endpoint}
		c.byKey[key] = row
	}
	row.Requests++
	if !rc.usageOK {
		return
	}
	row.TokensIn += rc.usage.In
	row.TokensInCached += rc.usage.CacheRead
	row.TokensInFresh += rc.usage.Fresh()
	row.TokensOut += rc.usage.Out
}

// clientEndpointScale reports how many distinct clients and how many rows
// §5.5 will render. §5.5 is a client×endpoint product with no Top-N cap by
// design (see the dev plan's risk table) — this is what lets a deployment
// whose section has quietly grown to hundreds of rows notice, instead of
// finding out by scrolling. Lives here rather than inline in aggregate.go's
// finishBuckets: counting this collector's own output is this collector's
// job, not the orchestration phase that calls it.
func clientEndpointScale(rows []ClientEndpointRow) (clients, rowCount int) {
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.ClientKey] = true
	}
	return len(seen), len(rows)
}

// result returns rows grouped by client, token-descending within each
// group — see ClientEndpointRow's doc comment (rows.go) for the sort/
// tie-break contract section_client_endpoint.go's grouping relies on.
func (c *clientEndpointCollector) result() []ClientEndpointRow {
	out := make([]ClientEndpointRow, 0, len(c.byKey))
	for _, row := range c.byKey {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ClientKey != out[j].ClientKey {
			return out[i].ClientKey < out[j].ClientKey
		}
		if out[i].TokensIn != out[j].TokensIn {
			return out[i].TokensIn > out[j].TokensIn
		}
		return out[i].Endpoint < out[j].Endpoint // tie-break: deterministic across runs
	})
	return out
}
