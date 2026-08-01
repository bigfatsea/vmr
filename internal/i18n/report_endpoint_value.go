// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/section_endpoint_value.go (§6.6 Endpoint Value).
package i18n

// EndpointValueText is section_endpoint_value.go's text, in one language.
type EndpointValueText struct {
	Title             string
	IntroPriced       string
	IntroUnpriced     string
	BaseHeaders       [3]string                  // endpoint, successful requests, out tokens
	PricedHeaders     func(cur string) [2]string // cost/1M out, cost/successful request
	TailHeaders       [3]string                  // failed attempts, availability, wasted time
	WastedNote        string
	NoMoneyNote1      string
	NoMoneyNote2      string
	PricedCompareNote string
}

func EndpointValue(lang Lang) EndpointValueText {
	if lang == ZH {
		return EndpointValueText{
			Title:             "§6.6 端点性价比 ⭐",
			IntroPriced:       "单位产出的代价，而不只是总花费——一个单价便宜但经常失败的端点，把请求推给下一家之后的真实代价可能更高。\n\n",
			IntroUnpriced:     "单位产出的代价，而不只是总花费——未配置定价，本节只显示时间维度；配置 `-pricing` 后会补上单位成本列。\n\n",
			BaseHeaders:       [3]string{"端点", "成功请求", "out tokens"},
			PricedHeaders:     func(cur string) [2]string { return [2]string{"成本/1M out" + cur, "成本/成功请求" + cur} },
			TailHeaders:       [3]string{"失败尝试", "可用率", "失败耗时⭐"},
			WastedNote:        "> 失败耗时⭐ = 该端点**失败尝试**累计墙钟时间：请求最终由别处完成，这段时间是纯粹的延迟损耗。\n",
			NoMoneyNote1:      "> **只记时间、不折算成钱**：失败尝试拿不到 usage（vmr 只从客户端真正收到的那份响应里提取），厂商通常也不对失败请求计费——\n",
			NoMoneyNote2:      "> 给它标一个金额会是编造。这里的口径是「它让你多等了多久」，不是「它花了你多少钱」。\n",
			PricedCompareNote: "> 成本/1M out 用于横向比价（同样产出 100 万 token 谁更便宜）；成本/成功请求受各端点承接的请求形态影响，跨端点比较前先看 §5 的负载画像。\n",
		}
	}
	return EndpointValueText{
		Title:             "§6.6 Endpoint Value ⭐",
		IntroPriced:       "Cost per unit of output delivered, not just total spend — a cheap-per-request endpoint that fails often can be more expensive once you account for the retry it forces.\n\n",
		IntroUnpriced:     "Cost per unit of output delivered, not just total spend — no pricing configured, so this section only shows the time dimension; configure `-pricing` to add the unit-cost columns.\n\n",
		BaseHeaders:       [3]string{"Endpoint", "Successful Requests", "out tokens"},
		PricedHeaders:     func(cur string) [2]string { return [2]string{"Cost/1M out" + cur, "Cost/Success Req" + cur} },
		TailHeaders:       [3]string{"Failed Attempts", "Availability", "Wasted Time⭐"},
		WastedNote:        "> Wasted Time⭐ = this endpoint's **failed attempts'** cumulative wall-clock time: the request was eventually completed elsewhere, so this time is pure latency loss.\n",
		NoMoneyNote1:      "> **Time only, never converted to money**: failed attempts carry no usage (vmr only extracts it from the response the client actually received), and most providers don't bill failed requests anyway —\n",
		NoMoneyNote2:      "> putting a dollar figure on it would be fabricated. The basis here is \"how much longer did it make you wait\", not \"how much did it cost you\".\n",
		PricedCompareNote: "> Cost/1M out is for apples-to-apples comparison (which is cheaper for the same 1M tokens produced); cost/successful request is shaped by each endpoint's own request mix — check §5's workload profile before comparing across endpoints.\n",
	}
}
