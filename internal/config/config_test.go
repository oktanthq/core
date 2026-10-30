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
}

func TestFromEnvOverrides(t *testing.T) {
	cfg, err := FromEnv([]string{
		"CORE_ADDR=:9090",
		"CORE_LOG_LEVEL=WARN",
		"CORE_SHUTDOWN_TIMEOUT=2s",
		"UNRELATED=ignored",
	})
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.Addr != ":9090" || cfg.LogLevel != "warn" || cfg.ShutdownTimeout != 2*time.Second {
		t.Fatalf("cfg = %+v", cfg)
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
