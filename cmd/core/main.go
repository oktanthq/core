// Command core runs the Oktant core platform API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oktanthq/core/internal/auth"
	"github.com/oktanthq/core/internal/config"
	"github.com/oktanthq/core/internal/githubapp"
	"github.com/oktanthq/core/internal/httpapi"
	"github.com/oktanthq/core/internal/platform"
	"github.com/oktanthq/core/internal/project"
)

// version is overridden with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cfg, err := config.FromEnv(os.Environ())
	if err != nil {
		slog.Error("configuration", "err", err)
		os.Exit(1)
	}

	logger := newLogger(os.Stderr, cfg.LogLevel)
	slog.SetDefault(logger)

	opts := httpapi.Options{
		Logger:       logger,
		Version:      version,
		Projects:     project.NewMemoryStore(),
		GitHubWebURL: cfg.GitHubWebURL,
		AppOrigin:    cfg.AppOrigin,
		StateSecret:  cfg.StateSecret,
	}

	if cfg.SupabaseJWTSecret != "" {
		verifier, err := auth.NewVerifier(auth.Options{
			JWTSecret: cfg.SupabaseJWTSecret,
			Issuer:    auth.IssuerFromURL(cfg.SupabaseURL),
		})
		if err != nil {
			logger.Error("auth", "err", err)
			os.Exit(1)
		}
		opts.Verifier = verifier
	}

	if cfg.DatabaseURL != "" {
		pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
		if err != nil {
			logger.Error("database", "err", err)
			os.Exit(1)
		}
		defer pool.Close()
		store := platform.NewPostgres(pool)
		opts.Platform = store
		opts.ReadyCheck = store.Ping
	}

	if cfg.GitHubAppID != "" && cfg.GitHubAppPrivateKey != "" {
		gh, err := githubapp.New(githubapp.Options{
			AppID:      cfg.GitHubAppID,
			PrivateKey: cfg.GitHubAppPrivateKey,
			APIURL:     cfg.GitHubAPIURL,
			WebURL:     cfg.GitHubWebURL,
			Slug:       cfg.GitHubAppSlug,
		})
		if err != nil {
			logger.Error("github app", "err", err)
			os.Exit(1)
		}
		opts.GitHub = gh
	}

	api := httpapi.New(opts)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr, "version", version)
		errCh <- server.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server", "err", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown", "err", err)
			os.Exit(1)
		}
	}
}

func newLogger(out *os.File, level string) *slog.Logger {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{
		Level:       slogLevel,
		ReplaceAttr: redactAttr,
	}))
}

func redactAttr(_ []string, a slog.Attr) slog.Attr {
	key := strings.ToLower(a.Key)
	switch {
	case key == "token", key == "authorization", key == "secret",
		strings.Contains(key, "private_key"), strings.Contains(key, "access_token"):
		return slog.String(a.Key, "[redacted]")
	default:
		return a
	}
}
