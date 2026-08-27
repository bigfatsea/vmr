// Ver 2026-07-30, by Sonnet 5
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const dirsTestBody = `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://example.com}, api_key: k}
models:
  m: {endpoints: [{protocol: openai-completions, providers: [p1], models: [x]}]}
`

func TestDirDefaultsResolveToPersistentHome(t *testing.T) {
	cfg, err := Parse([]byte(dirsTestBody))
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir in this environment")
	}
	if want := filepath.Join(home, ".vmr", "logs"); cfg.LogDir != want {
		t.Errorf("log_dir default: got %q, want %q", cfg.LogDir, want)
	}
	if want := filepath.Join(home, ".vmr", "image_cache"); cfg.ImageCacheDir != want {
		t.Errorf("image_cache_dir default: got %q, want %q", cfg.ImageCacheDir, want)
	}
}

func TestDirExplicitValuesUsedAsIs(t *testing.T) {
	cfg, err := Parse([]byte("log_dir: /var/log/vmr\nimage_cache_dir: /var/cache/vmr\n" + dirsTestBody))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogDir != "/var/log/vmr" {
		t.Errorf("log_dir: %q", cfg.LogDir)
	}
	if cfg.ImageCacheDir != "/var/cache/vmr" {
		t.Errorf("image_cache_dir: %q", cfg.ImageCacheDir)
	}
}

func TestDirTildeExpansion(t *testing.T) {
	cfg, err := Parse([]byte("log_dir: ~/vmr-logs\n" + dirsTestBody))
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir in this environment")
	}
	if want := filepath.Join(home, "vmr-logs"); cfg.LogDir != want {
		t.Errorf("tilde expansion: got %q, want %q", cfg.LogDir, want)
	}
	// "~user" forms and mid-path tildes stay literal.
	cfg2, err := Parse([]byte("log_dir: /data/~backup\n" + dirsTestBody))
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.LogDir != "/data/~backup" {
		t.Errorf("mid-path tilde must stay literal: %q", cfg2.LogDir)
	}
}

func TestDirEnvExpansionViaVarReference(t *testing.T) {
	// The one supported way to feed a directory from the environment:
	// reference it explicitly, same as API keys.
	t.Setenv("VMR_TEST_DIR", "/mnt/data/vmr-logs")
	cfg, err := Parse([]byte("log_dir: ${VMR_TEST_DIR}\n" + dirsTestBody))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogDir != "/mnt/data/vmr-logs" {
		t.Errorf("env-referenced log_dir: %q", cfg.LogDir)
	}
	// Unset variable expands to "" → falls back to the default chain.
	t.Setenv("VMR_TEST_DIR", "")
	cfg2, err := Parse([]byte("log_dir: ${VMR_TEST_DIR}\n" + dirsTestBody))
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.LogDir == "" || strings.Contains(cfg2.LogDir, "${") {
		t.Errorf("unset var should fall back to default, got %q", cfg2.LogDir)
	}
}
