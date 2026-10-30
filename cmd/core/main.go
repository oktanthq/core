// Command core runs the Oktant core platform API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/oktanthq/core/internal/config"
	"github.com/oktanthq/core/internal/httpapi"
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

	api := httpapi.New(httpapi.Options{
		Logger:   logger,
		Version:  version,
		Projects: project.NewMemoryStore(),
	})

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
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: slogLevel}))
}
