package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/oktanthq/core/internal/auth"
	"github.com/oktanthq/core/internal/githubapp"
	"github.com/oktanthq/core/internal/platform"
	"github.com/oktanthq/core/internal/queue"
)

type createAnalysisRequest struct {
	RepositoryID string `json:"repository_id"`
	CommitSHA    string `json:"commit_sha"`
	AnalysisType string `json:"analysis_type"`
}

func (s *Server) createAnalysis(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if !validUUID(projectID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid project id")
		return
	}
	if !s.requirePlatform(w) {
		return
	}
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
		return
	}

	project, err := s.platform.ProjectByID(r.Context(), projectID)
	if err != nil {
		writeStoreError(w, s.logger, err)
		return
	}
	if _, ok := s.requireMember(w, r, project.OrganizationID, platform.RoleMember); !ok {
		return
	}

	var body createAnalysisRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(w, r, &body); err != nil {
			writeDecodeError(w, err)
			return
		}
	}
	analysisType := strings.TrimSpace(body.AnalysisType)
	if analysisType == "" {
		analysisType = "security"
	}

	repo, err := s.resolveRepository(r, project, body.RepositoryID)
	if err != nil {
		writeStoreError(w, s.logger, err)
		return
	}

	run, err := s.enqueueAnalysis(r, principal.UserID, project, &repo, analysisType, strings.TrimSpace(body.CommitSHA), "manual")
	if err != nil {
		if errors.Is(err, platform.ErrConflict) {
			writeJSON(w, http.StatusAccepted, run)
			return
		}
		writeStoreError(w, s.logger, err)
		return
	}
	w.Header().Set("Location", "/v1/analyses/"+run.ID)
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) resolveRepository(r *http.Request, project platform.Project, repositoryID string) (platform.Repository, error) {
	repositoryID = strings.TrimSpace(repositoryID)
	if repositoryID != "" {
		repo, err := s.platform.RepositoryByID(r.Context(), repositoryID)
		if err != nil {
			return platform.Repository{}, err
		}
		if repo.ProjectID != project.ID {
			return platform.Repository{}, &platform.Error{Sentinel: platform.ErrNotFound, Message: "repository not found"}
		}
		return repo, nil
	}
	if project.Repository != nil {
		return *project.Repository, nil
	}
	return platform.Repository{}, &platform.Error{Sentinel: platform.ErrInvalid, Message: "repository_id is required"}
}

func (s *Server) enqueueAnalysis(r *http.Request, actorUserID string, project platform.Project, repo *platform.Repository, analysisType, commitSHA, trigger string) (platform.AnalysisRun, error) {
	if s.github == nil {
		return platform.AnalysisRun{}, &platform.Error{Sentinel: platform.ErrInvalid, Message: "github app is not configured"}
	}
	integ, err := s.platform.GitHubIntegration(r.Context(), project.OrganizationID)
	if err != nil {
		return platform.AnalysisRun{}, err
	}
	installationID, err := githubapp.ParseInstallationID(integ.ExternalAccountID)
	if err != nil {
		return platform.AnalysisRun{}, &platform.Error{Sentinel: platform.ErrInvalid, Message: "github installation is missing"}
	}

	access, err := s.github.CreateInstallationToken(installationID, repo.FullName)
	if err != nil {
		s.logger.Error("github token mint", "err", err, "request_id", RequestID(r.Context()))
		return platform.AnalysisRun{}, &platform.Error{Sentinel: platform.ErrInvalid, Message: "could not mint github access"}
	}

	ref := repo.DefaultBranch
	if ref == "" {
		ref = "main"
	}
	if commitSHA == "" {
		sha, err := s.github.ResolveCommit(access.Token, repo.FullName, ref)
		if err != nil {
			s.logger.Error("github commit", "err", err, "request_id", RequestID(r.Context()))
			return platform.AnalysisRun{}, &platform.Error{Sentinel: platform.ErrInvalid, Message: "could not resolve commit"}
		}
		commitSHA = sha
	}
	commitSHA = strings.ToLower(strings.TrimSpace(commitSHA))
	if !validCommitSHA(commitSHA) {
		return platform.AnalysisRun{}, &platform.Error{Sentinel: platform.ErrInvalid, Message: "commit_sha must be a git commit SHA"}
	}

	if existing, err := s.platform.ActiveAnalysis(r.Context(), repo.ID, commitSHA, analysisType); err == nil {
		return existing, platform.ErrConflict
	} else if !errors.Is(err, platform.ErrNotFound) {
		return platform.AnalysisRun{}, err
	}

	runID := uuid.NewString()
	eventID := uuid.NewString()
	analyzers := queue.RequestedAnalyzers(analysisType)
	cloneURL := repo.CloneURL
	if cloneURL == "" {
		cloneURL = githubapp.CloneURL(s.githubWebURL, repo.FullName)
	}
	defaultBranch := repo.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = ref
	}
	payload := queue.AnalysisPayload{
		AnalysisRunID:      runID,
		OrgID:              project.OrganizationID,
		ProjectID:          project.ID,
		RepositoryID:       repo.ID,
		Provider:           "github",
		RepoFullName:       repo.FullName,
		CloneURL:           cloneURL,
		DefaultBranch:      defaultBranch,
		CommitSHA:          commitSHA,
		Ref:                "refs/heads/" + ref,
		Trigger:            trigger,
		AnalysisType:       analysisType,
		RequestedAnalyzers: analyzers,
		InstallationID:     installationID,
		Access: queue.Access{
			Kind:      "installation_token",
			Token:     access.Token,
			ExpiresAt: queue.FormatTime(access.ExpiresAt),
		},
	}
	env := queue.NewAnalysisRequested(eventID, s.now(), uuidOrEmpty(actorUserID), correlationID(r), payload)
	raw, err := queue.MarshalJSONBytes(env)
	if err != nil {
		return platform.AnalysisRun{}, err
	}

	run, err := s.platform.CreateQueuedAnalysis(r.Context(), platform.AnalysisRun{
		ID:                 runID,
		RepositoryID:       repo.ID,
		AnalysisType:       analysisType,
		Status:             platform.StatusQueued,
		CommitSHA:          commitSHA,
		Trigger:            trigger,
		RequestedAnalyzers: analyzers,
		CreatedAt:          s.now(),
	}, raw)
	if err != nil {
		return run, err
	}
	return run, nil
}

func correlationID(r *http.Request) string {
	id := RequestID(r.Context())
	if _, err := uuid.Parse(id); err == nil {
		return id
	}
	return uuid.NewString()
}

func uuidOrEmpty(raw string) string {
	raw = strings.TrimSpace(raw)
	if _, err := uuid.Parse(raw); err != nil {
		return ""
	}
	return raw
}

func validCommitSHA(sha string) bool {
	n := len(sha)
	if n < 7 || n > 40 {
		return false
	}
	for _, r := range sha {
		if unicode.Is(unicode.ASCII_Hex_Digit, r) {
			continue
		}
		return false
	}
	return true
}
