// Ver 2026-08-12 23:40, by Opus 5
package report

import (
	"testing"

	"vmr/internal/chatmsg"
)

func TestClientEndpointCollectorGroupsAndSorts(t *testing.T) {
	c := newClientEndpointCollector()
	c.add(&rec2{clientKey: "agent-a", endpoint: "openai:p1:m1", usageOK: true,
		usage: chatmsg.Usage{In: 100, Out: 10}})
	c.add(&rec2{clientKey: "agent-a", endpoint: "openai:p2:m2", usageOK: true,
		usage: chatmsg.Usage{In: 500, Out: 50}})
	c.add(&rec2{clientKey: "agent-a", endpoint: "openai:p1:m1", usageOK: true,
		usage: chatmsg.Usage{In: 100, Out: 10}})
	c.add(&rec2{clientKey: "agent-b", endpoint: "openai:p1:m1", usageOK: true,
		usage: chatmsg.Usage{In: 50, Out: 5}})
	rows := c.result()
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (2 for agent-a, 1 for agent-b)", len(rows))
	}
	// agent-a sorts before agent-b (client-major); within agent-a, p2:m2 (600
	// in) sorts before p1:m1 (200 in, aggregated across the two adds).
	if rows[0].ClientKey != "agent-a" || rows[0].Endpoint != "openai:p2:m2" {
		t.Errorf("rows[0] = %+v, want agent-a/openai:p2:m2 first (higher tokens)", rows[0])
	}
	if rows[1].ClientKey != "agent-a" || rows[1].Endpoint != "openai:p1:m1" {
		t.Errorf("rows[1] = %+v", rows[1])
	}
	if rows[1].Requests != 2 || rows[1].TokensIn != 200 {
		t.Errorf("rows[1] aggregation = %+v, want Requests=2 TokensIn=200", rows[1])
	}
	if rows[2].ClientKey != "agent-b" {
		t.Errorf("rows[2] = %+v, want agent-b", rows[2])
	}
}

// The key must be the full endpoint label, not the model name — the whole
// reason this feature exists: config.mba.yaml can put the same model under
// two different provider accounts, and only the endpoint label preserves
// that distinction.
func TestClientEndpointCollectorKeysByFullEndpointNotModel(t *testing.T) {
	c := newClientEndpointCollector()
	c.add(&rec2{clientKey: "agent-a", endpoint: "openai:volcengine:deepseek-v4-flash", usageOK: true, usage: chatmsg.Usage{In: 10}})
	c.add(&rec2{clientKey: "agent-a", endpoint: "openai:volcengine2:deepseek-v4-flash", usageOK: true, usage: chatmsg.Usage{In: 20}})
	rows := c.result()
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (same model, two distinct accounts must not merge)", len(rows))
	}
}

func TestClientEndpointCollectorSkipsEmptyKeys(t *testing.T) {
	c := newClientEndpointCollector()
	c.add(&rec2{clientKey: "", endpoint: "openai:p1:m1", usageOK: true, usage: chatmsg.Usage{In: 10}})
	c.add(&rec2{clientKey: "agent-a", endpoint: "", usageOK: true, usage: chatmsg.Usage{In: 10}})
	if rows := c.result(); len(rows) != 0 {
		t.Errorf("rows = %+v, want empty (both records missing a grouping key)", rows)
	}
}

// A record with no usable usage still counts as a request against that
// (client, endpoint) pair — it just contributes no tokens.
func TestClientEndpointCollectorUsageNotOKCountsRequestOnly(t *testing.T) {
	c := newClientEndpointCollector()
	c.add(&rec2{clientKey: "agent-a", endpoint: "openai:p1:m1", usageOK: false})
	rows := c.result()
	if len(rows) != 1 || rows[0].Requests != 1 || rows[0].TokensIn != 0 {
		t.Errorf("rows = %+v, want 1 request, 0 tokens", rows)
	}
}

func TestClientEndpointCollectorDeterministicTieBreak(t *testing.T) {
	c := newClientEndpointCollector()
	c.add(&rec2{clientKey: "agent-a", endpoint: "openai:zeta:m", usageOK: true, usage: chatmsg.Usage{In: 100}})
	c.add(&rec2{clientKey: "agent-a", endpoint: "openai:alpha:m", usageOK: true, usage: chatmsg.Usage{In: 100}})
	rows := c.result()
	if len(rows) != 2 || rows[0].Endpoint != "openai:alpha:m" || rows[1].Endpoint != "openai:zeta:m" {
		t.Errorf("order = %v, want alpha before zeta on a token tie", rows)
	}
}
