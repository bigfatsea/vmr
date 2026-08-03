// Ver 2026-08-04, by Opus 5

// examples/sample-audit.jsonl is the hand-maintained fixture referenced from
// README.md as "what an audit record looks like" — until this test existed,
// nothing verified it still matched audit.Record's actual shape. Since
// internal/report is compile-time coupled to that shape (CLAUDE.md's stated
// invariant), a schema change could silently leave this example stale with
// no build or test failure anywhere to catch it.
package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSampleAuditJSONLDeserializes(t *testing.T) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	repoRoot := filepath.Dir(strings.TrimSpace(string(out)))
	path := filepath.Join(repoRoot, "examples", "sample-audit.jsonl")

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	lines := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		lines++
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("line %d: does not unmarshal into audit.Record: %v\n%s", lines, err, line)
		}
		if rec.Model == "" && rec.Outcome == "" {
			t.Fatalf("line %d: unmarshaled to a zero-ish Record — check the fixture is actually record-shaped, not just valid JSON", lines)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	if lines == 0 {
		t.Fatalf("%s has no non-empty lines", path)
	}
}
