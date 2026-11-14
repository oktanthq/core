package config

import (
	"testing"
	"time"
)

func TestFromEnvDefaults(t *testing.T) {
	cfg, err := FromEnv(nil)
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("ShutdownTimeout = %s", cfg.ShutdownTimeout)
	}
	if cfg.GitHubAPIURL != "https://api.github.com" || cfg.GitHubWebURL != "https://github.com" {
		t.Fatalf("github urls = %q %q", cfg.GitHubAPIURL, cfg.GitHubWebURL)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	cfg, err := FromEnv([]string{
		"CORE_ADDR=:9090",
		"CORE_LOG_LEVEL=WARN",
		"CORE_SHUTDOWN_TIMEOUT=2s",
		"SUPABASE_URL=https://example.supabase.co",
		"SUPABASE_JWT_SECRET=jwt-secret",
		"SUPABASE_SERVICE_ROLE_KEY=service-role",
		"DATABASE_URL=postgres://localhost/core",
		"GITHUB_APP_ID=123",
		"GITHUB_APP_SLUG=oktant",
		"GITHUB_APP_CALLBACK_URL=http://localhost:8080/v1/integrations/github/callback",
		"UNRELATED=ignored",
	})
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.Addr != ":9090" || cfg.LogLevel != "warn" || cfg.ShutdownTimeout != 2*time.Second {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.SupabaseJWTSecret != "jwt-secret" || cfg.DatabaseURL != "postgres://localhost/core" || cfg.SupabaseServiceRoleKey != "service-role" {
		t.Fatalf("supabase = %+v", cfg)
	}
	if cfg.GitHubAppCallbackURL != "http://localhost:8080/v1/integrations/github/callback" {
		t.Fatalf("callback = %q", cfg.GitHubAppCallbackURL)
	}
	if cfg.StateSecret != "jwt-secret" {
		t.Fatalf("StateSecret = %q", cfg.StateSecret)
	}
	if cfg.GitHubAppID != "123" || cfg.GitHubAppSlug != "oktant" {
		t.Fatalf("github = %+v", cfg)
	}
}

func TestFromEnvLastValueWins(t *testing.T) {
	cfg, err := FromEnv([]string{"CORE_ADDR=:1", "CORE_ADDR=:2"})
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.Addr != ":2" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
}

func TestFromEnvDatabaseURLAlias(t *testing.T) {
	cfg, err := FromEnv([]string{"SUPABASE_DB_URL=postgres://db"})
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.DatabaseURL != "postgres://db" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
}

func TestFromEnvRejectsBadValues(t *testing.T) {
	cases := [][]string{
		{"CORE_LOG_LEVEL=verbose"},
		{"CORE_SHUTDOWN_TIMEOUT=nope"},
		{"CORE_SHUTDOWN_TIMEOUT=0s"},
	}
	for _, env := range cases {
		if _, err := FromEnv(env); err == nil {
			t.Fatalf("FromEnv(%q) succeeded", env)
		}
	}
}
