// Ver 2026-08-12 23:40, by Opus 5

// Pairs with internal/report/section_client_endpoint.go (§5.5 Per-Client
// Upstream Attribution).
package i18n

// ClientEndpointText is section_client_endpoint.go's text, in one language.
type ClientEndpointText struct {
	Title   string
	Intro   string
	Headers [6]string // endpoint, requests, fresh, cached, out, % of client's tokens
}

func ClientEndpoint(lang Lang) ClientEndpointText {
	if lang == ZH {
		return ClientEndpointText{
			Title:   "§5.5 按客户端的上游归属",
			Intro:   "每个客户端命中了哪些上游端点、各自拿走了多少 token——回答\"这个 Agent 的流量到底落到哪几个账户/模型上了\"。\n\n",
			Headers: [6]string{"端点", "请求", "fresh", "cached", "out", "占该客户端 token 的比例"},
		}
	}
	return ClientEndpointText{
		Title:   "§5.5 Per-Client Upstream Attribution",
		Intro:   "Which upstream endpoints each client actually hit, and how many tokens landed on each — answers \"where does this agent's traffic actually land\".\n\n",
		Headers: [6]string{"Endpoint", "Requests", "fresh", "cached", "out", "% of client's tokens"},
	}
}
