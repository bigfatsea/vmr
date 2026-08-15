// Ver 2026-08-12 23:40, by Opus 5

// §5.5 按客户端的上游归属: one small heading + table per client, each row
// an endpoint it hit. Grouped rather than a client×endpoint matrix — see
// rows.go's ClientEndpointRow doc comment for why.
package report

import (
	"strconv"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

func renderClientEndpoint(w func(string, ...any), rep *Report2, lang i18n.Lang) {
	if len(rep.ClientEndpoints) == 0 {
		return
	}
	t := i18n.ClientEndpoint(lang)
	w("## %s\n\n", t.Title)
	w("%s", t.Intro)

	// rep.ClientEndpoints is already sorted client-major, tokens-in-desc
	// within each client (clientendpoint.go's result()) — group by
	// consecutive ClientKey rather than re-sorting.
	i := 0
	for i < len(rep.ClientEndpoints) {
		client := rep.ClientEndpoints[i].ClientKey
		j := i
		var clientTotal int64
		for j < len(rep.ClientEndpoints) && rep.ClientEndpoints[j].ClientKey == client {
			clientTotal += rep.ClientEndpoints[j].TokensIn
			j++
		}
		w("**%s**\n\n", client)
		tbl := newTable(w, t.Headers[0], t.Headers[1], t.Headers[2], t.Headers[3], t.Headers[4], t.Headers[5])
		for _, r := range rep.ClientEndpoints[i:j] {
			tbl.row(r.Endpoint, strconv.Itoa(r.Requests), fmtutil.FmtTokens(r.TokensInFresh),
				fmtutil.FmtTokens(r.TokensInCached), fmtutil.FmtTokens(r.TokensOut), pctStr64(r.TokensIn, clientTotal))
		}
		w("\n")
		i = j
	}
}
