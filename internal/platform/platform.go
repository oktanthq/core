// Package platform persists orgs, integrations, projects, repos, and analysis runs.
package platform

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
)

const (
	maxNameLen        = 80
	maxSlugLen        = 80
	maxDescriptionLen = 500

	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"

	StatusQueued  = "queued"
	StatusRunning = "running"
)

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrInvalid   = errors.New("invalid")
	ErrForbidden = errors.New("forbidden")
)

// Error is a store error that carries a client-safe message and unwraps to
// one of the sentinel errors.
type Error struct {
	Sentinel error
	Message  string
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Sentinel }

func invalid(message string) error {
	return &Error{Sentinel: ErrInvalid, Message: message}
}

func conflict(message string) error {
	return &Error{Sentinel: ErrConflict, Message: message}
}

func notFound(message string) error {
	return &Error{Sentinel: ErrNotFound, Message: message}
}

// Integration is an org-level VCS install.
type Integration struct {
	ID                string    `json:"id"`
	OrganizationID    string    `json:"organization_id"`
	Provider          string    `json:"provider"`
	Status            string    `json:"status"`
	SecretReference   string    `json:"secret_reference,omitempty"`
	ExternalAccountID string    `json:"external_account_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Repository is a project-connected git repo.
type Repository struct {
	ID             string     `json:"id"`
	ProjectID      string     `json:"project_id"`
	IntegrationID  string     `json:"integration_id,omitempty"`
	ExternalID     string     `json:"external_id,omitempty"`
	FullName       string     `json:"full_name"`
	DefaultBranch  string     `json:"default_branch"`
	CloneURL       string     `json:"clone_url"`
	IsPrivate      bool       `json:"is_private"`
	LastSyncedAt   *time.Time `json:"last_synced_at,omitempty"`
	OrganizationID string     `json:"-"`
}

// Project is an org-scoped project with its connected repository.
type Project struct {
	ID             string      `json:"id"`
	OrganizationID string      `json:"organization_id"`
	Name           string      `json:"name"`
	Slug           string      `json:"slug"`
	Description    string      `json:"description,omitempty"`
	Status         string      `json:"status"`
	CreatedBy      string      `json:"created_by,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	Repository     *Repository `json:"repository,omitempty"`
}

// AnalysisRun is a queued or completed scan.
type AnalysisRun struct {
	ID                 string    `json:"id"`
	RepositoryID       string    `json:"repository_id"`
	AnalysisType       string    `json:"analysis_type"`
	Status             string    `json:"status"`
	CommitSHA          string    `json:"commit_sha"`
	Trigger            string    `json:"trigger"`
	RequestedAnalyzers []string  `json:"requested_analyzers"`
	CreatedAt          time.Time `json:"created_at"`
}

// CreateRepoInput is the wizard repository payload.
type CreateRepoInput struct {
	ExternalID    string
	FullName      string
	DefaultBranch string
	CloneURL      string
	IsPrivate     bool
}

// CreateProjectInput persists a project and its repo in one step.
type CreateProjectInput struct {
	OrganizationID string
	CreatedBy      string
	Name           string
	Slug           string
	Description    string
	IntegrationID  string
	Repo           CreateRepoInput
}

// GitHubInstallInput upserts the org GitHub App installation.
type GitHubInstallInput struct {
	OrganizationID string
	InstallationID string
}

// Store is the persistence seam used by the HTTP API.
type Store interface {
	Ping(ctx context.Context) error
	MemberRole(ctx context.Context, orgID, userID string) (string, error)
	UpsertGitHubInstallation(ctx context.Context, in GitHubInstallInput) (Integration, error)
	GitHubIntegration(ctx context.Context, orgID string) (Integration, error)
	CreateProject(ctx context.Context, in CreateProjectInput) (Project, error)
	ProjectByID(ctx context.Context, id string) (Project, error)
	RepositoryByID(ctx context.Context, id string) (Repository, error)
	ActiveAnalysis(ctx context.Context, repoID, sha, analysisType string) (AnalysisRun, error)
	CreateQueuedAnalysis(ctx context.Context, run AnalysisRun, message []byte) (AnalysisRun, error)
}

// RoleAtLeast reports whether role meets minRole (owner > admin > member).
func RoleAtLeast(role, minRole string) bool {
	return roleRank(role) >= roleRank(minRole)
}

func roleRank(role string) int {
	switch role {
	case RoleMember:
		return 1
	case RoleAdmin:
		return 2
	case RoleOwner:
		return 3
	default:
		return 0
	}
}

func normalizeCreate(in CreateProjectInput) (CreateProjectInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Slug = strings.TrimSpace(in.Slug)
	in.IntegrationID = strings.TrimSpace(in.IntegrationID)
	in.Repo.FullName = strings.TrimSpace(in.Repo.FullName)
	in.Repo.DefaultBranch = strings.TrimSpace(in.Repo.DefaultBranch)
	in.Repo.CloneURL = strings.TrimSpace(in.Repo.CloneURL)
	in.Repo.ExternalID = strings.TrimSpace(in.Repo.ExternalID)

	if in.OrganizationID == "" {
		return CreateProjectInput{}, invalid("organization id is required")
	}
	if in.Name == "" {
		return CreateProjectInput{}, invalid("name is required")
	}
	if len(in.Name) > maxNameLen {
		return CreateProjectInput{}, invalid("name is too long")
	}
	if len(in.Description) > maxDescriptionLen {
		return CreateProjectInput{}, invalid("description is too long")
	}
	if in.Slug == "" {
		in.Slug = slugify(in.Name)
	}
	if err := validateSlug(in.Slug); err != nil {
		return CreateProjectInput{}, err
	}
	if in.IntegrationID == "" {
		return CreateProjectInput{}, invalid("integration_id is required")
	}
	if in.Repo.FullName == "" || !strings.Contains(in.Repo.FullName, "/") {
		return CreateProjectInput{}, invalid("repo.full_name is required")
	}
	if in.Repo.DefaultBranch == "" {
		in.Repo.DefaultBranch = "main"
	}
	return in, nil
}

func validateSlug(slug string) error {
	if slug == "" {
		return invalid("slug is required")
	}
	if len(slug) > maxSlugLen {
		return invalid("slug is too long")
	}
	for i, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' && i > 0 && i < len(slug)-1:
		default:
			return invalid("slug must be lowercase letters, digits, and single hyphens")
		}
	}
	if strings.Contains(slug, "--") {
		return invalid("slug must be lowercase letters, digits, and single hyphens")
	}
	return nil
}

func slugify(name string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		if !prevHyphen && b.Len() > 0 {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func normalizeAnalysis(run AnalysisRun) (AnalysisRun, error) {
	run.RepositoryID = strings.TrimSpace(run.RepositoryID)
	run.AnalysisType = strings.TrimSpace(run.AnalysisType)
	run.CommitSHA = strings.TrimSpace(run.CommitSHA)
	run.Trigger = strings.TrimSpace(run.Trigger)
	if run.RepositoryID == "" {
		return AnalysisRun{}, invalid("repository_id is required")
	}
	switch run.AnalysisType {
	case "full", "security", "quality":
	default:
		return AnalysisRun{}, invalid("analysis_type must be full, security, or quality")
	}
	switch run.Trigger {
	case "manual", "project_created", "push", "pull_request":
	default:
		return AnalysisRun{}, invalid("trigger is invalid")
	}
	if run.CommitSHA == "" {
		return AnalysisRun{}, invalid("commit_sha is required")
	}
	if run.Status == "" {
		run.Status = StatusQueued
	}
	if run.RequestedAnalyzers == nil {
		run.RequestedAnalyzers = []string{}
	}
	return run, nil
}
