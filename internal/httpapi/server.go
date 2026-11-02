// Package httpapi is the HTTP surface of the core API.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/oktanthq/core/internal/project"
)

// ReadyCheck reports whether the process can serve traffic.
//
// A nil check means the process is ready. Use it to fail readiness when a
// database or other dependency is down.
type ReadyCheck func(ctx context.Context) error

// Options configures the HTTP handler.
type Options struct {
	Logger     *slog.Logger
	Version    string
	Projects   project.Store
	ReadyCheck ReadyCheck
}

// Server serves the core API.
type Server struct {
	logger     *slog.Logger
	version    string
	projects   project.Store
	readyCheck ReadyCheck
	handler    http.Handler
}

// New builds the API handler.
//
// Projects is required. Logger defaults to slog.Default and Version defaults
// to "dev".
func New(opts Options) *Server {
	if opts.Projects == nil {
		panic("httpapi: projects store is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	version := opts.Version
	if version == "" {
		version = "dev"
	}

	s := &Server{
		logger:     logger,
		version:    version,
		projects:   opts.Projects,
		readyCheck: opts.ReadyCheck,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /v1/meta", s.meta)
	mux.HandleFunc("GET /v1/projects", s.listProjects)
	mux.HandleFunc("POST /v1/projects", s.createProject)
	mux.HandleFunc("GET /v1/projects/{id}", s.getProject)
	mux.HandleFunc("PATCH /v1/projects/{id}", s.updateProject)
	mux.HandleFunc("DELETE /v1/projects/{id}", s.deleteProject)

	s.handler = withRequestID(logger, withAccessLog(logger, withRecover(logger, mux)))
	return s
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}
