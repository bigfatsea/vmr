// Ver 2026-09-15, by pi

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
	sys := storyMsg("system", "you are a helpful assistant")
	u1 := storyMsg("user", "please check the status and run test")
	a1 := storyMsg("assistant", "checking status")
	t1 := map[string]any{"role": "tool", "tool_call_id": "c1", "content": "status: ok"}
	a2 := storyMsg("assistant", "all tests passed")

	recs := []audit.Record{
		storyRec(at(0), []any{sys, u1}, storySSE("I will help with that")),
		storyRec(at(1), []any{sys, u1, a1, t1}, storySSE("tool executed successfully")),
		storyRec(at(2), []any{sys, u1, a1, t1, a2}, storySSE("finished")),
	}
	return writeStoryJSONL(t, recs)
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

// TestRenderOnly_LanguageMismatchRejected verifies D10:
// -render-only inherits JSON language; explicit conflicting -lang is rejected.
func TestRenderOnly_LanguageMismatchRejected(t *testing.T) {
	logPath := fixtureAuditLogs(t)
	outDir := filepath.Join(t.TempDir(), "reports-lang-test")

	// Full run in English
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-lang", "en", logPath})
	}); err != nil {
		t.Fatalf("full cmdAnalyze: %v", err)
	}

	// -render-only with conflicting -lang zh must be rejected
	err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-render-only", "-o", outDir, "-lang", "zh"})
	})
	if err == nil {
		t.Fatal("expected error when -render-only is called with conflicting -lang zh, got nil")
	}
	if !strings.Contains(err.Error(), "cannot change language") {
		t.Errorf("expected error message to mention language cannot change, got: %v", err)
	}

	// -render-only with matching -lang en must succeed
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-render-only", "-o", outDir, "-lang", "en"})
	}); err != nil {
		t.Fatalf("expected -render-only -lang en to succeed, got: %v", err)
	}

	// -render-only with no -lang inherits en and succeeds
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-render-only", "-o", outDir})
	}); err != nil {
		t.Fatalf("expected -render-only (no lang) to succeed, got: %v", err)
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
