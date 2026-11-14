package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// MemoryStore is a process-local Store used in tests.
type MemoryStore struct {
	mu           sync.Mutex
	now          func() time.Time
	roles        map[string]string
	integrations map[string]Integration
	projects     map[string]Project
	repos        map[string]Repository
	analyses     map[string]AnalysisRun
	messages     [][]byte
	enqueueHook  func(run AnalysisRun, message []byte) error
	slugIndex    map[string]string
}

// NewMemoryStore returns an empty memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		now:          func() time.Time { return time.Now().UTC() },
		roles:        map[string]string{},
		integrations: map[string]Integration{},
		projects:     map[string]Project{},
		repos:        map[string]Repository{},
		analyses:     map[string]AnalysisRun{},
		slugIndex:    map[string]string{},
	}
}

// SetMember records a membership for tests.
func (s *MemoryStore) SetMember(orgID, userID, role string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roles[orgID+"/"+userID] = role
}

// SetEnqueueHook injects enqueue behavior for tests.
func (s *MemoryStore) SetEnqueueHook(fn func(run AnalysisRun, message []byte) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enqueueHook = fn
}

// Messages returns queued payloads.
func (s *MemoryStore) Messages() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.messages))
	copy(out, s.messages)
	return out
}

// Ping implements Store.
func (s *MemoryStore) Ping(context.Context) error { return nil }

// MemberRole implements Store.
func (s *MemoryStore) MemberRole(_ context.Context, orgID, userID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	role, ok := s.roles[orgID+"/"+userID]
	if !ok {
		return "", notFound("not an organization member")
	}
	return role, nil
}

// UpsertGitHubInstallation implements Store.
func (s *MemoryStore) UpsertGitHubInstallation(_ context.Context, in GitHubInstallInput) (Integration, error) {
	if strings.TrimSpace(in.OrganizationID) == "" || strings.TrimSpace(in.InstallationID) == "" {
		return Integration{}, invalid("installation id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	current, ok := s.integrations[in.OrganizationID]
	if !ok {
		id, err := newUUID()
		if err != nil {
			return Integration{}, err
		}
		current = Integration{
			ID:             id,
			OrganizationID: in.OrganizationID,
			Provider:       "github",
			CreatedAt:      now,
		}
	}
	current.Status = "connected"
	current.ExternalAccountID = in.InstallationID
	current.SecretReference = "github_app_installation:" + in.InstallationID
	current.UpdatedAt = now
	s.integrations[in.OrganizationID] = current
	return current, nil
}

// GitHubIntegration implements Store.
func (s *MemoryStore) GitHubIntegration(_ context.Context, orgID string) (Integration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	got, ok := s.integrations[orgID]
	if !ok || got.Status != "connected" {
		return Integration{}, notFound("github is not connected")
	}
	return got, nil
}

// CreateProject implements Store.
func (s *MemoryStore) CreateProject(_ context.Context, in CreateProjectInput) (Project, error) {
	normalized, err := normalizeCreate(in)
	if err != nil {
		return Project{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.integrations[normalized.OrganizationID]; !ok {
		return Project{}, notFound("integration not found")
	}
	integ := s.integrations[normalized.OrganizationID]
	if integ.ID != normalized.IntegrationID {
		return Project{}, notFound("integration not found")
	}
	slugKey := normalized.OrganizationID + "/" + normalized.Slug
	if _, taken := s.slugIndex[slugKey]; taken {
		return Project{}, conflict("slug is already in use")
	}
	projectID, err := newUUID()
	if err != nil {
		return Project{}, err
	}
	repoID, err := newUUID()
	if err != nil {
		return Project{}, err
	}
	now := s.now()
	repo := Repository{
		ID:             repoID,
		ProjectID:      projectID,
		IntegrationID:  normalized.IntegrationID,
		ExternalID:     normalized.Repo.ExternalID,
		FullName:       normalized.Repo.FullName,
		DefaultBranch:  normalized.Repo.DefaultBranch,
		CloneURL:       normalized.Repo.CloneURL,
		IsPrivate:      normalized.Repo.IsPrivate,
		OrganizationID: normalized.OrganizationID,
	}
	project := Project{
		ID:             projectID,
		OrganizationID: normalized.OrganizationID,
		Name:           normalized.Name,
		Slug:           normalized.Slug,
		Description:    normalized.Description,
		Status:         "active",
		CreatedBy:      normalized.CreatedBy,
		CreatedAt:      now,
		UpdatedAt:      now,
		Repository:     &repo,
	}
	s.projects[projectID] = project
	s.repos[repoID] = repo
	s.slugIndex[slugKey] = projectID
	return project, nil
}

// ProjectByID implements Store.
func (s *MemoryStore) ProjectByID(_ context.Context, id string) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	got, ok := s.projects[id]
	if !ok {
		return Project{}, notFound("project not found")
	}
	return got, nil
}

// RepositoryByID implements Store.
func (s *MemoryStore) RepositoryByID(_ context.Context, id string) (Repository, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	got, ok := s.repos[id]
	if !ok {
		return Repository{}, notFound("repository not found")
	}
	return got, nil
}

// ActiveAnalysis implements Store.
func (s *MemoryStore) ActiveAnalysis(_ context.Context, repoID, sha, analysisType string) (AnalysisRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, run := range s.analyses {
		if run.RepositoryID == repoID && run.CommitSHA == sha && run.AnalysisType == analysisType {
			if run.Status == StatusQueued || run.Status == StatusRunning {
				return run, nil
			}
		}
	}
	return AnalysisRun{}, notFound("analysis not found")
}

// CreateQueuedAnalysis implements Store.
func (s *MemoryStore) CreateQueuedAnalysis(_ context.Context, run AnalysisRun, message []byte) (AnalysisRun, error) {
	normalized, err := normalizeAnalysis(run)
	if err != nil {
		return AnalysisRun{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.repos[normalized.RepositoryID]; !ok {
		return AnalysisRun{}, notFound("repository not found")
	}
	for _, existing := range s.analyses {
		if existing.RepositoryID == normalized.RepositoryID && existing.CommitSHA == normalized.CommitSHA && existing.AnalysisType == normalized.AnalysisType {
			if existing.Status == StatusQueued || existing.Status == StatusRunning {
				return AnalysisRun{}, conflict("analysis already queued")
			}
		}
	}
	if s.enqueueHook != nil {
		if err := s.enqueueHook(normalized, message); err != nil {
			return AnalysisRun{}, err
		}
	}
	if normalized.ID == "" {
		id, err := newUUID()
		if err != nil {
			return AnalysisRun{}, err
		}
		normalized.ID = id
	}
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = s.now()
	}
	normalized.Status = StatusQueued
	s.analyses[normalized.ID] = normalized
	copied := append([]byte(nil), message...)
	s.messages = append(s.messages, copied)
	return normalized, nil
}

func newUUID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	hexed := hex.EncodeToString(buf[:])
	return hexed[0:8] + "-" + hexed[8:12] + "-" + hexed[12:16] + "-" + hexed[16:20] + "-" + hexed[20:], nil
}
