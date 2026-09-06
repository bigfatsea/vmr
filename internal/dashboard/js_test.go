// Ver 2026-09-06, by Claude
package dashboard

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestJS_PureFunctionsAndFixture executes pure JavaScript functions against
// testdata/fmt_cases.json using Node.js (§5.6). Skips cleanly if node is not found.
func TestJS_PureFunctionsAndFixture(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node executable not found, skipping JS assertions")
	}

	commonJSPath, err := filepath.Abs("assets/common.js")
	if err != nil {
		t.Fatalf("resolve common.js path: %v", err)
	}
	fixturePath, err := filepath.Abs("testdata/fmt_cases.json")
	if err != nil {
		t.Fatalf("resolve fmt_cases.json path: %v", err)
	}

	testScript := `
const fs = require('fs');
const { versionBehavior, FmtTokens, FmtBytes, FmtPercent, FmtCurrency, FmtCost } = require(` + "'" + commonJSPath + "'" + `);
const fixture = JSON.parse(fs.readFileSync(` + "'" + fixturePath + "'" + `, 'utf8'));

const fns = { FmtTokens, FmtBytes, FmtPercent, FmtCurrency, FmtCost };

let failed = 0;
for (const tc of fixture.cases) {
  const fn = fns[tc.fn];
  if (!fn) {
    console.error('Unknown fn:', tc.fn);
    failed++;
    continue;
  }
  const got = fn(tc.input);
  if (got !== tc.want) {
    console.error('Mismatch for ' + tc.fn + '(' + tc.input + '): got "' + got + '", want "' + tc.want + '"');
    failed++;
  }
}

const vbCases = [
  { exp: 11, act: 11, want: 'ok' },
  { exp: 11, act: '11', want: 'ok' },
  { exp: 11, act: 10, want: 'banner' },
  { exp: 11, act: 12, want: 'banner' },
  { exp: 11, act: null, want: 'missing' },
  { exp: 11, act: undefined, want: 'missing' },
  { exp: 11, act: '', want: 'missing' },
];

for (const c of vbCases) {
  const got = versionBehavior(c.exp, c.act);
  if (got !== c.want) {
    console.error('versionBehavior mismatch:', c, got);
    failed++;
  }
}

if (failed > 0) {
  process.exit(1);
}
`
	cmd := exec.Command(nodePath, "-e", testScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node test script failed: %v\nOutput:\n%s", err, string(out))
	}
}
