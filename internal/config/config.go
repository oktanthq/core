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
	defaultGitHubAPIURL    = "https://api.github.com"
	defaultGitHubWebURL    = "https://github.com"
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

	// SupabaseURL is the project URL, used to check JWT issuer when set.
	SupabaseURL string
	// SupabaseJWTSecret verifies HS256 access tokens from Supabase Auth.
	SupabaseJWTSecret string
	// SupabaseServiceRoleKey is the service-role API key. It is never sent
	// on a user request path; keep it for service-role clients.
	SupabaseServiceRoleKey string
	// DatabaseURL is the Postgres connection string (service role / postgres).
	// SUPABASE_DB_URL is accepted as an alias.
	DatabaseURL string

	// GitHubAppID is the numeric GitHub App id.
	GitHubAppID string
	// GitHubAppPrivateKey is the App PKCS#1 or PKCS#8 PEM. Never log it.
	GitHubAppPrivateKey string
	// GitHubAppSlug is used to build the install URL.
	GitHubAppSlug string
	// GitHubAppCallbackURL is the public URL GitHub redirects to after install.
	GitHubAppCallbackURL string
	// GitHubAPIURL defaults to https://api.github.com.
	GitHubAPIURL string
	// GitHubWebURL defaults to https://github.com.
	GitHubWebURL string

	// AppOrigin is the web app origin allowed in GitHub callback redirects.
	AppOrigin string
	// StateSecret signs GitHub App connect state. Falls back to the JWT secret.
	StateSecret string
}

// FromEnv parses configuration from environ entries in KEY=VALUE form.
//
// Missing keys use defaults. A blank value is treated as unset.
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
		GitHubAPIURL:    defaultGitHubAPIURL,
		GitHubWebURL:    defaultGitHubWebURL,
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

	cfg.SupabaseURL = strings.TrimSpace(values["SUPABASE_URL"])
	cfg.SupabaseJWTSecret = firstNonEmpty(values, "SUPABASE_JWT_SECRET", "SUPABASE_ANON_JWT_SECRET")
	cfg.SupabaseServiceRoleKey = strings.TrimSpace(values["SUPABASE_SERVICE_ROLE_KEY"])
	cfg.DatabaseURL = firstNonEmpty(values, "DATABASE_URL", "SUPABASE_DB_URL")

	cfg.GitHubAppID = strings.TrimSpace(values["GITHUB_APP_ID"])
	cfg.GitHubAppPrivateKey = decodePrivateKey(values["GITHUB_APP_PRIVATE_KEY"])
	cfg.GitHubAppSlug = strings.TrimSpace(values["GITHUB_APP_SLUG"])
	cfg.GitHubAppCallbackURL = strings.TrimSpace(values["GITHUB_APP_CALLBACK_URL"])
	if api := strings.TrimSpace(values["GITHUB_API_URL"]); api != "" {
		cfg.GitHubAPIURL = strings.TrimRight(api, "/")
	}
	if web := strings.TrimSpace(values["GITHUB_WEB_URL"]); web != "" {
		cfg.GitHubWebURL = strings.TrimRight(web, "/")
	}

	cfg.AppOrigin = strings.TrimRight(strings.TrimSpace(values["CORE_APP_ORIGIN"]), "/")
	cfg.StateSecret = firstNonEmpty(values, "CORE_STATE_SECRET", "SUPABASE_JWT_SECRET")

	return cfg, nil
}

func firstNonEmpty(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(values[key]); v != "" {
			return v
		}
	}
	return ""
}

func decodePrivateKey(raw string) string {
	return strings.TrimSpace(raw)
}
