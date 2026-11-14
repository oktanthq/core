package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oktanthq/core/internal/queue"
)

// Postgres is a service-role Store backed by Supabase Postgres.
type Postgres struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// NewPostgres wraps a connection pool. The pool should use a role that
// bypasses RLS (postgres or service_role).
func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool, now: func() time.Time { return time.Now().UTC() }}
}

// Ping implements Store.
func (s *Postgres) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close releases the pool.
func (s *Postgres) Close() {
	s.pool.Close()
}

// MemberRole implements Store.
func (s *Postgres) MemberRole(ctx context.Context, orgID, userID string) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx, `
		SELECT role
		FROM organization_members
		WHERE organization_id = $1 AND user_id = $2
	`, orgID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", notFound("not an organization member")
	}
	if err != nil {
		return "", err
	}
	return role, nil
}

// UpsertGitHubInstallation implements Store.
func (s *Postgres) UpsertGitHubInstallation(ctx context.Context, in GitHubInstallInput) (Integration, error) {
	if strings.TrimSpace(in.OrganizationID) == "" || strings.TrimSpace(in.InstallationID) == "" {
		return Integration{}, invalid("installation id is required")
	}
	secretRef := "github_app_installation:" + in.InstallationID
	row := s.pool.QueryRow(ctx, `
		INSERT INTO integrations (
			organization_id, provider, status, secret_reference, external_account_id
		) VALUES ($1, 'github', 'connected', $2, $3)
		ON CONFLICT (organization_id, provider)
		DO UPDATE SET
			status = 'connected',
			secret_reference = EXCLUDED.secret_reference,
			external_account_id = EXCLUDED.external_account_id,
			updated_at = now()
		RETURNING id, organization_id, provider, status, secret_reference, external_account_id, created_at, updated_at
	`, in.OrganizationID, secretRef, in.InstallationID)
	return scanIntegration(row)
}

// GitHubIntegration implements Store.
func (s *Postgres) GitHubIntegration(ctx context.Context, orgID string) (Integration, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, organization_id, provider, status, secret_reference, external_account_id, created_at, updated_at
		FROM integrations
		WHERE organization_id = $1 AND provider = 'github'
	`, orgID)
	got, err := scanIntegration(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Integration{}, notFound("github is not connected")
	}
	return got, err
}

// CreateProject implements Store.
func (s *Postgres) CreateProject(ctx context.Context, in CreateProjectInput) (Project, error) {
	normalized, err := normalizeCreate(in)
	if err != nil {
		return Project{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Project{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var integrationID string
	var integrationOrg string
	err = tx.QueryRow(ctx, `
		SELECT id, organization_id FROM integrations WHERE id = $1
	`, normalized.IntegrationID).Scan(&integrationID, &integrationOrg)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, notFound("integration not found")
	}
	if err != nil {
		return Project{}, err
	}
	if integrationOrg != normalized.OrganizationID {
		return Project{}, notFound("integration not found")
	}

	project := Project{
		ID:             uuid.NewString(),
		OrganizationID: normalized.OrganizationID,
		Name:           normalized.Name,
		Slug:           normalized.Slug,
		Description:    normalized.Description,
		Status:         "active",
		CreatedBy:      normalized.CreatedBy,
		CreatedAt:      s.now(),
		UpdatedAt:      s.now(),
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO projects (id, organization_id, name, slug, description, status, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::uuid, $8, $9)
	`, project.ID, project.OrganizationID, project.Name, project.Slug, project.Description, project.Status, project.CreatedBy, project.CreatedAt, project.UpdatedAt)
	if err != nil {
		return Project{}, mapPgError(err, "slug is already in use")
	}

	repo := Repository{
		ID:             uuid.NewString(),
		ProjectID:      project.ID,
		IntegrationID:  normalized.IntegrationID,
		ExternalID:     normalized.Repo.ExternalID,
		FullName:       normalized.Repo.FullName,
		DefaultBranch:  normalized.Repo.DefaultBranch,
		CloneURL:       normalized.Repo.CloneURL,
		IsPrivate:      normalized.Repo.IsPrivate,
		OrganizationID: normalized.OrganizationID,
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO repositories (
			id, project_id, integration_id, external_id, full_name, default_branch, clone_url, is_private
		) VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8)
	`, repo.ID, repo.ProjectID, repo.IntegrationID, repo.ExternalID, repo.FullName, repo.DefaultBranch, repo.CloneURL, repo.IsPrivate)
	if err != nil {
		return Project{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Project{}, err
	}
	project.Repository = &repo
	return project, nil
}

// ProjectByID implements Store.
func (s *Postgres) ProjectByID(ctx context.Context, id string) (Project, error) {
	var p Project
	err := s.pool.QueryRow(ctx, `
		SELECT id, organization_id, name, slug, description, status, COALESCE(created_by::text, ''), created_at, updated_at
		FROM projects
		WHERE id = $1
	`, id).Scan(&p.ID, &p.OrganizationID, &p.Name, &p.Slug, &p.Description, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, notFound("project not found")
	}
	if err != nil {
		return Project{}, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, project_id, COALESCE(integration_id::text, ''), COALESCE(external_id, ''),
		       full_name, default_branch, clone_url, is_private, last_synced_at
		FROM repositories
		WHERE project_id = $1
		ORDER BY created_at
		LIMIT 1
	`, id)
	if err != nil {
		return Project{}, err
	}
	defer rows.Close()
	if rows.Next() {
		var repo Repository
		if err := rows.Scan(&repo.ID, &repo.ProjectID, &repo.IntegrationID, &repo.ExternalID, &repo.FullName, &repo.DefaultBranch, &repo.CloneURL, &repo.IsPrivate, &repo.LastSyncedAt); err != nil {
			return Project{}, err
		}
		repo.OrganizationID = p.OrganizationID
		p.Repository = &repo
	}
	return p, rows.Err()
}

// RepositoryByID implements Store.
func (s *Postgres) RepositoryByID(ctx context.Context, id string) (Repository, error) {
	var repo Repository
	err := s.pool.QueryRow(ctx, `
		SELECT r.id, r.project_id, COALESCE(r.integration_id::text, ''), COALESCE(r.external_id, ''),
		       r.full_name, r.default_branch, r.clone_url, r.is_private, r.last_synced_at, p.organization_id
		FROM repositories r
		JOIN projects p ON p.id = r.project_id
		WHERE r.id = $1
	`, id).Scan(&repo.ID, &repo.ProjectID, &repo.IntegrationID, &repo.ExternalID, &repo.FullName, &repo.DefaultBranch, &repo.CloneURL, &repo.IsPrivate, &repo.LastSyncedAt, &repo.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Repository{}, notFound("repository not found")
	}
	return repo, err
}

// ActiveAnalysis implements Store.
func (s *Postgres) ActiveAnalysis(ctx context.Context, repoID, sha, analysisType string) (AnalysisRun, error) {
	run, err := scanAnalysis(s.pool.QueryRow(ctx, `
		SELECT id, repository_id, analysis_type, status, commit_sha, trigger, requested_analyzers, created_at
		FROM analysis_runs
		WHERE repository_id = $1 AND commit_sha = $2 AND analysis_type = $3
		  AND status IN ('queued', 'running')
		ORDER BY created_at
		LIMIT 1
	`, repoID, sha, analysisType))
	if errors.Is(err, pgx.ErrNoRows) {
		return AnalysisRun{}, notFound("analysis not found")
	}
	return run, err
}

// CreateQueuedAnalysis inserts analysis_runs and pgmq.send in one transaction.
func (s *Postgres) CreateQueuedAnalysis(ctx context.Context, run AnalysisRun, message []byte) (AnalysisRun, error) {
	normalized, err := normalizeAnalysis(run)
	if err != nil {
		return AnalysisRun{}, err
	}
	if normalized.ID == "" {
		normalized.ID = uuid.NewString()
	}
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = s.now()
	}
	err = withTx(ctx, func(ctx context.Context) (txConn, error) {
		return s.pool.Begin(ctx)
	}, func(tx txConn) error {
		return enqueueAnalysis(ctx, tx, normalized, message)
	})
	if err != nil {
		if errors.Is(err, ErrConflict) {
			existing, findErr := s.ActiveAnalysis(ctx, normalized.RepositoryID, normalized.CommitSHA, normalized.AnalysisType)
			if findErr == nil {
				return existing, ErrConflict
			}
		}
		return AnalysisRun{}, err
	}
	normalized.Status = StatusQueued
	return normalized, nil
}

type execQuerier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type txConn interface {
	execQuerier
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

func withTx(ctx context.Context, begin func(context.Context) (txConn, error), fn func(txConn) error) error {
	tx, err := begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func enqueueAnalysis(ctx context.Context, q execQuerier, run AnalysisRun, message []byte) error {
	analyzers, err := json.Marshal(run.RequestedAnalyzers)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO analysis_runs (
			id, repository_id, analysis_type, status, commit_sha, trigger, requested_analyzers, created_at
		) VALUES ($1, $2, $3, 'queued', $4, $5, $6::jsonb, $7)
	`, run.ID, run.RepositoryID, run.AnalysisType, run.CommitSHA, run.Trigger, analyzers, run.CreatedAt)
	if err != nil {
		return mapPgError(err, "analysis already queued")
	}
	var msgID int64
	if err := q.QueryRow(ctx, `SELECT pgmq.send($1::text, $2::jsonb)`, queue.NameAnalysis, message).Scan(&msgID); err != nil {
		return err
	}
	return nil
}

func scanIntegration(row pgx.Row) (Integration, error) {
	var in Integration
	var secret, external *string
	err := row.Scan(&in.ID, &in.OrganizationID, &in.Provider, &in.Status, &secret, &external, &in.CreatedAt, &in.UpdatedAt)
	if err != nil {
		return Integration{}, err
	}
	if secret != nil {
		in.SecretReference = *secret
	}
	if external != nil {
		in.ExternalAccountID = *external
	}
	return in, nil
}

func scanAnalysis(row pgx.Row) (AnalysisRun, error) {
	var run AnalysisRun
	var analyzers []byte
	err := row.Scan(&run.ID, &run.RepositoryID, &run.AnalysisType, &run.Status, &run.CommitSHA, &run.Trigger, &analyzers, &run.CreatedAt)
	if err != nil {
		return AnalysisRun{}, err
	}
	if len(analyzers) > 0 {
		_ = json.Unmarshal(analyzers, &run.RequestedAnalyzers)
	}
	if run.RequestedAnalyzers == nil {
		run.RequestedAnalyzers = []string{}
	}
	return run, nil
}

func mapPgError(err error, conflictMessage string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return conflict(conflictMessage)
	}
	return err
}
