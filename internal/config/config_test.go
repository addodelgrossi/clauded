package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_Precedence(t *testing.T) {
	// Arquivo YAML define addr e max_concurrency.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "clauded.yaml")
	if err := os.WriteFile(cfgPath, []byte("addr: \"1.2.3.4:1\"\nmax_concurrency: 9\ndefault_model: opus\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Env sobrescreve max_concurrency; flag sobrescreve addr.
	t.Setenv("CLAUDED_MAX_CONCURRENCY", "5")
	t.Setenv("CLAUDED_CONFIG", cfgPath)

	cfg, err := Load([]string{"--addr", "9.9.9.9:2"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "9.9.9.9:2" {
		t.Errorf("addr = %q (flag deve vencer arquivo)", cfg.Addr)
	}
	if cfg.MaxConcurrency != 5 {
		t.Errorf("max_concurrency = %d (env deve vencer arquivo)", cfg.MaxConcurrency)
	}
	if cfg.DefaultModel != "opus" {
		t.Errorf("default_model = %q (arquivo deve vencer default)", cfg.DefaultModel)
	}
}

func TestValidate(t *testing.T) {
	base := Default()
	base.APIToken = "x"
	base.OAuthToken = "y"
	if err := base.Validate(); err != nil {
		t.Errorf("config válida rejeitada: %v", err)
	}

	missing := Default()
	if err := missing.Validate(); err == nil {
		t.Errorf("esperava erro sem tokens")
	}
}

func TestEnv_RunTimeoutParse(t *testing.T) {
	t.Setenv("CLAUDED_API_TOKEN", "a")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "b")
	t.Setenv("CLAUDED_RUN_TIMEOUT", "30s")
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RunTimeout != 30*time.Second {
		t.Errorf("run_timeout = %v, quer 30s", cfg.RunTimeout)
	}
	if cfg.APIToken != "a" || cfg.OAuthToken != "b" {
		t.Errorf("segredos não carregados do env")
	}
}
