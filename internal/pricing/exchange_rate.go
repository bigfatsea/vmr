// Ver 2026-09-06, by Sonnet 5

package pricing

import (
	_ "embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// standardExchangeRateYAML is a hand-maintained "1 USD = X <code>" default
// table — see standard_exchange_rate.yaml's own doc comment for why this
// exists and its precision expectations.
//
//go:embed standard_exchange_rate.yaml
var standardExchangeRateYAML []byte

type exchangeRateFile struct {
	GeneratedAt string             `yaml:"generated_at"`
	Rates       map[string]float64 `yaml:"rates"`
}

// LoadDefaultExchangeRate parses the embedded default exchange-rate table —
// used both to build EffectiveExchangeRate's fallback layer and by `vmr
// check` to label which currencies came from that built-in default (as
// opposed to the user's own top-level exchange_rate: block) alongside its
// generation date.
func LoadDefaultExchangeRate() (rates map[string]float64, generatedAt string, err error) {
	var ef exchangeRateFile
	dec := yaml.NewDecoder(strings.NewReader(string(standardExchangeRateYAML)))
	dec.KnownFields(true)
	if err := dec.Decode(&ef); err != nil {
		return nil, "", fmt.Errorf("embedded standard_exchange_rate.yaml: %w", err)
	}
	return ef.Rates, ef.GeneratedAt, nil
}

// EffectiveExchangeRate merges userRates over the embedded default table —
// a user-declared code always wins on a matching key (lookup order: user
// config -> built-in default -> load-time error). The returned map is what
// every FactorBetween call in this package's callers
// (internal/config's provider currency conversion, cmd/vmr/cmd_report.go's
// display-currency factor) should pass as their rates argument — a
// currency absent from BOTH layers stays absent, so FactorBetween still
// fails loudly rather than guessing.
func EffectiveExchangeRate(userRates map[string]float64) (map[string]float64, error) {
	def, _, err := LoadDefaultExchangeRate()
	if err != nil {
		return nil, err
	}
	merged := make(map[string]float64, len(def)+len(userRates))
	for k, v := range def {
		merged[strings.ToUpper(strings.TrimSpace(k))] = v
	}
	for k, v := range userRates {
		merged[strings.ToUpper(strings.TrimSpace(k))] = v
	}
	return merged, nil
}
