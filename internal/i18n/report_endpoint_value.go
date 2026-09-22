// Ver 2026-09-22 02:10, by Sonnet 5

// Pairs with internal/report/viewmodel_endpoint_value.go (§6.6 Endpoint Value).
package i18n

import "fmt"

// EndpointValueText is viewmodel_endpoint_value.go's text, in one language.
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

// endpointValueRow holds report_endpoint_value.go's literal templates, one
// row per Lang (Table's own doc comment).
type endpointValueRow struct {
	title             string
	introPriced       string
	introUnpriced     string
	baseHeaders       [3]string
	pricedHeadersFmt  [2]string // cost/1M out, cost/successful request — "%s" takes the currency suffix
	tailHeaders       [3]string
	wastedNote        string
	noMoneyNote1      string
	noMoneyNote2      string
	pricedCompareNote string
}

var endpointValueRows = Table[endpointValueRow]{
	EN: {
		title:             "§6.6 Endpoint Value ⭐",
		introPriced:       "Cost per unit of output delivered, not just total spend — a cheap-per-request endpoint that fails often can be more expensive once you account for the retry it forces.\n\n",
		introUnpriced:     "Cost per unit of output delivered, not just total spend — no pricing data available, so this section only shows the time dimension; configure pricing/providers[].pricing in config.yaml to add the unit-cost columns.\n\n",
		baseHeaders:       [3]string{"Endpoint", "Successful Requests", "out tokens"},
		pricedHeadersFmt:  [2]string{"Cost/1M out%s", "Cost/Success Req%s"},
		tailHeaders:       [3]string{"Failed Attempts", "Availability", "Wasted Time⭐"},
		wastedNote:        "> Wasted Time⭐ = this endpoint's **failed attempts'** cumulative wall-clock time: the request was eventually completed elsewhere, so this time is pure latency loss.\n",
		noMoneyNote1:      "> **Time only, never converted to money**: failed attempts carry no usage (vmr only extracts it from the response the client actually received), and most providers don't bill failed requests anyway —\n",
		noMoneyNote2:      "> putting a dollar figure on it would be fabricated. The basis here is \"how much longer did it make you wait\", not \"how much did it cost you\".\n",
		pricedCompareNote: "> Cost/1M out is for apples-to-apples comparison (which is cheaper for the same 1M tokens produced); cost/successful request is shaped by each endpoint's own request mix — check §5's workload profile before comparing across endpoints.\n",
	},
	ZH: {
		title:             "§6.6 端点性价比 ⭐",
		introPriced:       "单位产出的代价，而不只是总花费——一个单价便宜但经常失败的端点，把请求推给下一家之后的真实代价可能更高。\n\n",
		introUnpriced:     "单位产出的代价，而不只是总花费——未找到可用的定价数据，本节只显示时间维度；在 config.yaml 配置 pricing/providers[].pricing 后会补上单位成本列。\n\n",
		baseHeaders:       [3]string{"端点", "成功请求", "out tokens"},
		pricedHeadersFmt:  [2]string{"成本/1M out%s", "成本/成功请求%s"},
		tailHeaders:       [3]string{"失败尝试", "可用率", "失败耗时⭐"},
		wastedNote:        "> 失败耗时⭐ = 该端点**失败尝试**累计墙钟时间：请求最终由别处完成，这段时间是纯粹的延迟损耗。\n",
		noMoneyNote1:      "> **只记时间、不折算成钱**：失败尝试拿不到 usage（vmr 只从客户端真正收到的那份响应里提取），厂商通常也不对失败请求计费——\n",
		noMoneyNote2:      "> 给它标一个金额会是编造。这里的口径是「它让你多等了多久」，不是「它花了你多少钱」。\n",
		pricedCompareNote: "> 成本/1M out 用于横向比价（同样产出 100 万 token 谁更便宜）；成本/成功请求受各端点承接的请求形态影响，跨端点比较前先看 §5 的负载画像。\n",
	},
}

func EndpointValue(lang Lang) EndpointValueText {
	r := endpointValueRows.Row(lang)
	return EndpointValueText{
		Title:         r.title,
		IntroPriced:   r.introPriced,
		IntroUnpriced: r.introUnpriced,
		BaseHeaders:   r.baseHeaders,
		PricedHeaders: func(cur string) [2]string {
			return [2]string{fmt.Sprintf(r.pricedHeadersFmt[0], cur), fmt.Sprintf(r.pricedHeadersFmt[1], cur)}
		},
		TailHeaders:       r.tailHeaders,
		WastedNote:        r.wastedNote,
		NoMoneyNote1:      r.noMoneyNote1,
		NoMoneyNote2:      r.noMoneyNote2,
		PricedCompareNote: r.pricedCompareNote,
	}
}
