// Ver 2026-09-15, by Opus 5

// §5.5 按客户端的上游归属 view model: one small heading + table per
// client, each row an endpoint it hit. Grouped rather than a
// client×endpoint matrix — see rows.go's ClientEndpointRow doc comment
// for why. Pairs with internal/i18n/report_client_endpoint.go.
package report

import (
	"strconv"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

func vmClientEndpointSection(rep *Report2, lang i18n.Lang) SectionVM {
	if len(rep.ClientEndpoints) == 0 {
		return SectionVM{}
	}
	t := i18n.ClientEndpoint(lang)
	sec := SectionVM{ID: "client-endpoint", Title: t.Title}
	sec.Blocks = append(sec.Blocks, ParaVM{Text: t.Intro})

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
		tbl := &TableVM{Title: "**" + client + "**", Headers: t.Headers[:]}
		for _, r := range rep.ClientEndpoints[i:j] {
			tbl.row(r.Endpoint, strconv.Itoa(r.Requests), fmtutil.FmtTokens(r.TokensInFresh),
				fmtutil.FmtTokens(r.TokensInCached), fmtutil.FmtTokens(r.TokensOut), pctStr64(r.TokensIn, clientTotal))
		}
		sec.Blocks = append(sec.Blocks, tbl)
		i = j
	}
	return sec
}
