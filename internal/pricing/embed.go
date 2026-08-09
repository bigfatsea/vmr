// Ver 2026-08-07, by Opus 5

package pricing

import (
	_ "embed"
	"fmt"
)

// standardGeneratedYAML is produced by tools/gen_standard_pricing from
// docs/data/model_prices_and_context_window.json (a LiteLLM-format
// price-list snapshot, MIT licensed — see that generator's own doc comment
// for the attribution this satisfies). Regenerate with:
//
//	go run ./tools/gen_standard_pricing -generated-at YYYY-MM-DD
//
//go:embed standard_price_generated.yaml
var standardGeneratedYAML []byte

// standardCuratedYAML is hand-maintained — primarily first-party domestic
// vendor rows the generated table under-covers (see
// docs/TokenPlan_Quota_Routing_Design_opus-5.md's §13 "标准表对国产第一方
// 覆盖不全" limitation). Starts empty (see
// docs/TokenPlan_Quota_P2_DevPlan_opus-5.md's S6: "standard.curated.yaml
// 起始可以是空文件") rather than with fabricated numbers — an unverified
// price is worse than an absent one, per this whole package's "missing beats
// wrong" stance. tools/gen_standard_pricing must NEVER write to this file.
//
//go:embed standard_price_curated.yaml
var standardCuratedYAML []byte

// LoadStandard parses and merges the embedded standard_price_generated.yaml
// and standard_price_curated.yaml into one Table, curated winning per-key
// conflicts (see Merge's doc comment for why that's a whole-row, not
// per-component, override). Called once by internal/config at validate()
// time — the result is cheap to hold onto (a few hundred rows) and never
// needs to be reparsed per request.
func LoadStandard() (*Table, error) {
	generated, err := ParseTable(standardGeneratedYAML)
	if err != nil {
		return nil, fmt.Errorf("embedded standard_price_generated.yaml: %w", err)
	}
	curated, err := ParseTable(standardCuratedYAML)
	if err != nil {
		return nil, fmt.Errorf("embedded standard_price_curated.yaml: %w", err)
	}
	return Merge(generated, curated), nil
}
