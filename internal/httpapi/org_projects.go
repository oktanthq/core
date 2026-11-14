package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/oktanthq/core/internal/githubapp"
	"github.com/oktanthq/core/internal/platform"
)

type createOrgProjectRequest struct {
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	Description   string `json:"description"`
	IntegrationID string `json:"integration_id"`
	CodeAnalysis  *bool  `json:"code_analysis"`
	Repo          struct {
		ExternalID    string `json:"external_id"`
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
		Branch        string `json:"branch"`
		CloneURL      string `json:"clone_url"`
		IsPrivate     bool   `json:"is_private"`
	} `json:"repo"`
}

func (s *Server) createOrgProject(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("orgId")
	if !validUUID(orgID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid organization id")
		return
	}
	principal, ok := s.requireMember(w, r, orgID, platform.RoleMember)
	if !ok {
		return
	}

	var body createOrgProjectRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}

	branch := strings.TrimSpace(body.Repo.DefaultBranch)
	if branch == "" {
		branch = strings.TrimSpace(body.Repo.Branch)
	}
	cloneURL := strings.TrimSpace(body.Repo.CloneURL)
	if cloneURL == "" && body.Repo.FullName != "" {
		web := "https://github.com"
		if s.githubWebURL != "" {
			web = s.githubWebURL
		}
		cloneURL = githubapp.CloneURL(web, body.Repo.FullName)
	}

	created, err := s.platform.CreateProject(r.Context(), platform.CreateProjectInput{
		OrganizationID: orgID,
		CreatedBy:      principal.UserID,
		Name:           body.Name,
		Slug:           body.Slug,
		Description:    body.Description,
		IntegrationID:  body.IntegrationID,
		Repo: platform.CreateRepoInput{
			ExternalID:    body.Repo.ExternalID,
			FullName:      body.Repo.FullName,
			DefaultBranch: branch,
			CloneURL:      cloneURL,
			IsPrivate:     body.Repo.IsPrivate,
		},
	})
	if err != nil {
		writeStoreError(w, s.logger, err)
		return
	}

	codeAnalysis := true
	if body.CodeAnalysis != nil {
		codeAnalysis = *body.CodeAnalysis
	}
	if codeAnalysis && created.Repository != nil {
		if _, err := s.enqueueAnalysis(r, principal.UserID, created, created.Repository, "security", "", "project_created"); err != nil && !errors.Is(err, platform.ErrConflict) {
			s.logger.Error("first analysis", "err", err, "request_id", RequestID(r.Context()), "project_id", created.ID)
		}
	}

	w.Header().Set("Location", "/v1/projects/"+created.ID)
	writeJSON(w, http.StatusCreated, created)
}
