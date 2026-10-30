package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/oktanthq/core/internal/project"
)

const maxBodyBytes = 1 << 20

type projectListResponse struct {
	Projects []project.Project `json:"projects"`
}

type createProjectRequest struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

type updateProjectRequest struct {
	Name        *string `json:"name"`
	Slug        *string `json:"slug"`
	Description *string `json:"description"`
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.projects.List(r.Context())
	if err != nil {
		writeStoreError(w, s.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, projectListResponse{Projects: projects})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var body createProjectRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	created, err := s.projects.Create(r.Context(), project.CreateInput{
		Name:        body.Name,
		Slug:        body.Slug,
		Description: body.Description,
	})
	if err != nil {
		writeStoreError(w, s.logger, err)
		return
	}
	w.Header().Set("Location", "/v1/projects/"+created.ID)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validProjectID(id) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid project id")
		return
	}
	found, err := s.projects.Get(r.Context(), id)
	if err != nil {
		writeStoreError(w, s.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, found)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validProjectID(id) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid project id")
		return
	}
	var body updateProjectRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	updated, err := s.projects.Update(r.Context(), id, project.UpdateInput{
		Name:        body.Name,
		Slug:        body.Slug,
		Description: body.Description,
	})
	if err != nil {
		writeStoreError(w, s.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validProjectID(id) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid project id")
		return
	}
	if err := s.projects.Delete(r.Context(), id); err != nil {
		writeStoreError(w, s.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validProjectID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) error {
	if r.Body == nil {
		return errEmptyBody
	}
	contentType := r.Header.Get("Content-Type")
	mediaType, _, _ := strings.Cut(contentType, ";")
	if strings.TrimSpace(mediaType) != "application/json" {
		return errUnsupportedType
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return err
	}
	if decoder.More() {
		return errTrailingJSON
	}
	return nil
}

var (
	errEmptyBody       = errors.New("empty body")
	errUnsupportedType = errors.New("unsupported content type")
	errTrailingJSON    = errors.New("trailing json")
)

func writeDecodeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errUnsupportedType):
		writeError(w, http.StatusUnsupportedMediaType, "invalid_request", "content type must be application/json")
	case errors.Is(err, errEmptyBody):
		writeError(w, http.StatusBadRequest, "invalid_request", "request body is required")
	default:
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			writeError(w, http.StatusRequestEntityTooLarge, "invalid_request", "request body is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid json")
	}
}
