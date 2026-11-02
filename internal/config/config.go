// Package config loads process settings for the core API from the environment.
package config

import (
	"fmt"
	"strings"
	"time"
)

const (
	defaultAddr            = ":8080"
	defaultLogLevel        = "info"
	defaultShutdownTimeout = 10 * time.Second
)

// Config is the runtime configuration for the core API process.
type Config struct {
	// Addr is the HTTP listen address, such as ":8080".
	Addr string
	// LogLevel is one of debug, info, warn, or error.
	LogLevel string
	// ShutdownTimeout is how long the process waits for in-flight requests
	// after SIGINT or SIGTERM.
	ShutdownTimeout time.Duration
}

// FromEnv parses configuration from environ entries in KEY=VALUE form.
//
// Missing keys use defaults. CORE_ADDR, CORE_LOG_LEVEL, and
// CORE_SHUTDOWN_TIMEOUT are recognized. A blank value is treated as unset.
// Later duplicate keys win.
func FromEnv(environ []string) (Config, error) {
	values := map[string]string{}
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		values[key] = value
	}

	cfg := Config{
		Addr:            defaultAddr,
		LogLevel:        defaultLogLevel,
		ShutdownTimeout: defaultShutdownTimeout,
	}

	if addr := strings.TrimSpace(values["CORE_ADDR"]); addr != "" {
		cfg.Addr = addr
	}

	if level := strings.TrimSpace(values["CORE_LOG_LEVEL"]); level != "" {
		level = strings.ToLower(level)
		switch level {
		case "debug", "info", "warn", "error":
			cfg.LogLevel = level
		default:
			return Config{}, fmt.Errorf("CORE_LOG_LEVEL must be debug, info, warn, or error")
		}
	}

	if raw := strings.TrimSpace(values["CORE_SHUTDOWN_TIMEOUT"]); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("CORE_SHUTDOWN_TIMEOUT: %w", err)
		}
		if timeout <= 0 {
			return Config{}, fmt.Errorf("CORE_SHUTDOWN_TIMEOUT must be greater than zero")
		}
		cfg.ShutdownTimeout = timeout
	}

	return cfg, nil
}
