// Ver 2026-09-22 00:10, by Sonnet 5

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/report"
)

func hashFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fixtureAuditLogs(t *testing.T) string {
	t.Helper()
	at := func(m int) time.Time { return time.Date(2026, 8, 20, 10, m, 0, 0, time.UTC) }
	sys := journeyMsg("system", "you are a helpful assistant")
	u1 := journeyMsg("user", "please check the status and run test")
	a1 := journeyMsg("assistant", "checking status")
	t1 := map[string]any{"role": "tool", "tool_call_id": "c1", "content": "status: ok"}
	a2 := journeyMsg("assistant", "all tests passed")

	recs := []audit.Record{
		journeyRec(at(0), []any{sys, u1}, journeySSE("I will help with that")),
		journeyRec(at(1), []any{sys, u1, a1, t1}, journeySSE("tool executed successfully")),
		journeyRec(at(2), []any{sys, u1, a1, t1, a2}, journeySSE("finished")),
	}
	return writeJourneyJSONL(t, recs)
}

// TestRenderOnly_ByteEquivalenceWithFullRun proves §9's guard:
// -render-only outputs are byte-for-byte identical to the full run outputs
// across all resident human-readable Markdown products (D11).
func TestRenderOnly_ByteEquivalenceWithFullRun(t *testing.T) {
	for _, lang := range []string{"en", "zh"} {
		t.Run("lang="+lang, func(t *testing.T) {
			logPath := fixtureAuditLogs(t)
			outDir := filepath.Join(t.TempDir(), "reports-"+lang)

			// 1. Full analyze run
			if err := captureStdoutErr(t, func() error {
				return cmdAnalyze([]string{"-o", outDir, "-lang", lang, logPath})
			}); err != nil {
				t.Fatalf("full cmdAnalyze failed: %v", err)
			}

			// Collect all .md files and their hashes
			fullHashes := make(map[string]string)
			fullBytes := make(map[string][]byte)
			err := filepath.Walk(outDir, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return err
				}
				if strings.HasSuffix(path, ".md") {
					rel, _ := filepath.Rel(outDir, path)
					fullHashes[rel] = hashFile(t, path)
					data, _ := os.ReadFile(path)
					fullBytes[rel] = data
				}
				return nil
			})
			if err != nil {
				t.Fatalf("walk full run outDir: %v", err)
			}

			// Sanity check: must have produced vmr-report.md and journeys/index.md
			if _, ok := fullHashes["vmr-report.md"]; !ok {
				t.Fatal("full run missing vmr-report.md")
			}
			if _, ok := fullHashes[filepath.Join("journeys", "index.md")]; !ok {
				t.Fatal("full run missing journeys/index.md")
			}

			// Details directory must NOT have been materialized in default suite
			detailsDir := filepath.Join(outDir, "requests", "details")
			if detailDirHasFiles(detailsDir) {
				t.Fatal("default suite should not have materialized requests/details/*.md")
			}

			// 2. Run -render-only on the same output root
			if err := captureStdoutErr(t, func() error {
				return cmdAnalyze([]string{"-render-only", "-o", outDir})
			}); err != nil {
				t.Fatalf("-render-only cmdAnalyze failed: %v", err)
			}

			// Verify all .md files are byte-for-byte identical
			for rel, wantHash := range fullHashes {
				path := filepath.Join(outDir, rel)
				gotHash := hashFile(t, path)
				if gotHash != wantHash {
					gotBytes, _ := os.ReadFile(path)
					t.Errorf("file %s mismatch between full run and -render-only:\n=== WANT ===\n%s\n=== GOT ===\n%s",
						rel, string(fullBytes[rel]), string(gotBytes))
				}
			}

			// Details directory must STILL not be materialized
			if detailDirHasFiles(detailsDir) {
				t.Fatal("-render-only must never materialize requests/details/*.md")
			}
		})
	}
}

// TestRenderOnlyChangesLanguage verifies R1's headline consequence
// (codebase-weight-analysis doc §7): -render-only can now switch language,
// because the JSON data product it redraws from is language-invariant —
// changing language is pure re-render, never re-aggregation. This test used
// to pin the opposite (a conflicting -lang was rejected, D10) — reversed,
// not deleted, so the fact that this was a deliberate policy correction
// (not an accidental regression) stays visible in history.
func TestRenderOnlyChangesLanguage(t *testing.T) {
	logPath := fixtureAuditLogs(t)
	outDir := filepath.Join(t.TempDir(), "reports-lang-test")

	// Full run in English.
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-lang", "en", logPath})
	}); err != nil {
		t.Fatalf("full cmdAnalyze: %v", err)
	}
	mdPath := filepath.Join(outDir, "vmr-report.md")
	enMD, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read vmr-report.md: %v", err)
	}
	if !strings.Contains(string(enMD), "# VMR Usage Report") {
		t.Fatalf("expected English report, got:\n%s", enMD)
	}

	// -render-only -lang zh must switch language WITHOUT re-aggregating —
	// pin that by touching the audit log source out from under it: if this
	// path re-read it, the run would either fail or reflect the tamper.
	if err := os.WriteFile(logPath, []byte("not valid jsonl"), 0o600); err != nil {
		t.Fatalf("tamper source log: %v", err)
	}
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-render-only", "-o", outDir, "-lang", "zh"})
	}); err != nil {
		t.Fatalf("expected -render-only -lang zh to succeed (pure re-render, no re-aggregation): %v", err)
	}
	zhMD, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read vmr-report.md after -render-only -lang zh: %v", err)
	}
	if !strings.Contains(string(zhMD), "VMR 用量报告") {
		t.Fatalf("expected -render-only -lang zh to switch vmr-report.md to Chinese, got:\n%s", zhMD)
	}

	// manifest.json's Lang field tracks the last render, not "this
	// snapshot's language" (R1) — it must now read zh.
	m, err := report.ReadManifest(outDir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m.Lang != "zh" {
		t.Errorf("manifest.Lang = %q, want %q after -render-only -lang zh", m.Lang, "zh")
	}

	// -render-only with no -lang inherits the last-rendered language (zh)
	// and succeeds.
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-render-only", "-o", outDir})
	}); err != nil {
		t.Fatalf("expected -render-only (no lang) to succeed, got: %v", err)
	}
	inheritedMD, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(inheritedMD), "VMR 用量报告") {
		t.Errorf("expected -render-only with no -lang to inherit zh, got:\n%s", inheritedMD)
	}
}

// TestRenderOnly_CorruptedManifestRejected verifies §3.4:
// if any slice sha256 recorded in manifest.json does not match disk, -render-only rejects.
func TestRenderOnly_CorruptedManifestRejected(t *testing.T) {
	logPath := fixtureAuditLogs(t)
	outDir := filepath.Join(t.TempDir(), "reports-corrupt-test")

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, logPath})
	}); err != nil {
		t.Fatalf("full cmdAnalyze: %v", err)
	}

	// Corrupt macro/summary.json
	summaryPath := filepath.Join(outDir, "macro", "summary.json")
	if err := os.WriteFile(summaryPath, []byte(`{"corrupted": true}`), 0o600); err != nil {
		t.Fatalf("corrupt summary: %v", err)
	}

	err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-render-only", "-o", outDir})
	})
	if err == nil {
		t.Fatal("expected error on corrupted slice, got nil")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Errorf("expected sha256 mismatch in error, got: %v", err)
	}
}

// TestRenderOnly_MissingManifestRejected verifies §3.4:
// -render-only without manifest.json is rejected.
func TestRenderOnly_MissingManifestRejected(t *testing.T) {
	outDir := t.TempDir()
	err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-render-only", "-o", outDir})
	})
	if err == nil {
		t.Fatal("expected error on missing manifest.json, got nil")
	}
}

// TestRenderOnly_MutuallyExclusiveFlags verifies CLI validation:
// log aggregation flags are rejected with -render-only.
func TestRenderOnly_MutuallyExclusiveFlags(t *testing.T) {
	outDir := t.TempDir()
	badCases := [][]string{
		{"-render-only", "-journey", "j-123"},
		{"-render-only", "-compare", "a,b"},
		{"-render-only", "-benchmark"},
		{"-render-only", "-render-all"},
		{"-render-only", "-macro-only"},
		{"-render-only", "-list-only"},
		{"-render-only", "-journey-only"},
		{"-render-only", "-details"},
	}
	for _, args := range badCases {
		fullArgs := append([]string{"-o", outDir}, args...)
		err := cmdAnalyze(fullArgs)
		if err == nil {
			t.Errorf("expected error for args %v, got nil", args)
		}
		if !strings.Contains(err.Error(), "mutually exclusive") {
			t.Errorf("expected 'mutually exclusive' error for %v, got: %v", args, err)
		}
	}
}

// TestRenderOnly_MacroOnlyByteEquivalence verifies that a -macro-only run
// followed by -render-only produces byte-identical vmr-report.md and requests/failed.md.
func TestRenderOnly_MacroOnlyByteEquivalence(t *testing.T) {
	logPath := fixtureAuditLogs(t)
	outDir := filepath.Join(t.TempDir(), "reports-macro-only")

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-macro-only", "-o", outDir, logPath})
	}); err != nil {
		t.Fatalf("macro-only analyze: %v", err)
	}

	reportMDPath := filepath.Join(outDir, "vmr-report.md")
	wantReportHash := hashFile(t, reportMDPath)

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-render-only", "-o", outDir})
	}); err != nil {
		t.Fatalf("-render-only after macro-only: %v", err)
	}

	gotReportHash := hashFile(t, reportMDPath)
	if gotReportHash != wantReportHash {
		t.Errorf("vmr-report.md hash mismatch after -render-only")
	}
}

// TestRenderOnly_BenchmarkByteEquivalence verifies that a -benchmark run
// followed by -render-only produces byte-identical benchmarks.md.
func TestRenderOnly_BenchmarkByteEquivalence(t *testing.T) {
	logPath := fixtureAuditLogs(t)
	outDir := filepath.Join(t.TempDir(), "reports-benchmark")

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-benchmark", "-o", outDir, logPath})
	}); err != nil {
		t.Fatalf("benchmark analyze: %v", err)
	}

	benchMDPath := filepath.Join(outDir, "journeys", "benchmarks.md")
	wantBenchHash := hashFile(t, benchMDPath)

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-render-only", "-o", outDir})
	}); err != nil {
		t.Fatalf("-render-only after benchmark: %v", err)
	}

	gotBenchHash := hashFile(t, benchMDPath)
	if gotBenchHash != wantBenchHash {
		t.Errorf("benchmarks.md hash mismatch after -render-only")
	}
}
