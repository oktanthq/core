// Package httpapi is the HTTP surface of the core API.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/oktanthq/core/internal/auth"
	"github.com/oktanthq/core/internal/githubapp"
	"github.com/oktanthq/core/internal/platform"
	"github.com/oktanthq/core/internal/project"
)

// ReadyCheck reports whether the process can serve traffic.
//
// A nil check means the process is ready. Use it to fail readiness when a
// database or other dependency is down.
type ReadyCheck func(ctx context.Context) error

type gitHubAPI interface {
	InstallURL(state string) string
	CreateInstallationToken(installationID int64, repoFullName string) (githubapp.Access, error)
	ResolveCommit(token, repoFullName, ref string) (string, error)
}

// Options configures the HTTP handler.
type Options struct {
	Logger       *slog.Logger
	Version      string
	Projects     project.Store
	Platform     platform.Store
	Verifier     *auth.Verifier
	GitHub       gitHubAPI
	GitHubWebURL string
	AppOrigin    string
	StateSecret  string
	ReadyCheck   ReadyCheck
	Now          func() time.Time
}

// Server serves the core API.
type Server struct {
	logger       *slog.Logger
	version      string
	projects     project.Store
	platform     platform.Store
	verifier     *auth.Verifier
	github       gitHubAPI
	githubWebURL string
	appOrigin    string
	stateSecret  string
	readyCheck   ReadyCheck
	now          func() time.Time
	handler      http.Handler
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
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	s := &Server{
		logger:       logger,
		version:      version,
		projects:     opts.Projects,
		platform:     opts.Platform,
		verifier:     opts.Verifier,
		github:       opts.GitHub,
		githubWebURL: opts.GitHubWebURL,
		appOrigin:    opts.AppOrigin,
		stateSecret:  opts.StateSecret,
		readyCheck:   opts.ReadyCheck,
		now:          now,
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

	mux.Handle("POST /v1/orgs/{orgId}/integrations/github/connect", s.requireAuth(http.HandlerFunc(s.connectGitHub)))
	mux.HandleFunc("GET /v1/integrations/github/callback", s.githubCallback)
	mux.Handle("POST /v1/orgs/{orgId}/projects", s.requireAuth(http.HandlerFunc(s.createOrgProject)))
	mux.Handle("POST /v1/projects/{id}/analyses", s.requireAuth(http.HandlerFunc(s.createAnalysis)))

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
