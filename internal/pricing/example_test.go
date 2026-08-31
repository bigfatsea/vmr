// Ver 2026-08-31, by Opus 5

package pricing

import (
	"os"
	"testing"
)

// TestExampleSupplementFiles_LoadAndResolve keeps the repo-root
// pricing.example.yaml pair honest: they are the only documentation of the
// supplement format that a user copies VERBATIM, so a stale field name or an
// alias pointing at a key the standard table no longer has would hand every
// new user a config that fails to load. Same rationale as
// internal/config/example_config_test.go for config.example.yaml.
func TestExampleSupplementFiles_LoadAndResolve(t *testing.T) {
	std, err := LoadStandard()
	if err != nil {
		t.Fatalf("LoadStandard: %v", err)
	}
	for _, path := range []string{"../../pricing.example.yaml", "../../pricing.example.zh.yaml"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		tbl, err := ParseTableWithRates(data, nil)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if err := Merge(std, tbl).ValidateAliases(); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
}
