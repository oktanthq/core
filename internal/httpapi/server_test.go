package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oktanthq/core/internal/project"
)

func TestHealthReadyAndMeta(t *testing.T) {
	handler := testHandler(t, nil)

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := request(t, handler, http.MethodGet, path, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body %s", path, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("X-Request-ID") == "" {
			t.Fatalf("%s missing request id", path)
		}
	}

	rec := request(t, handler, http.MethodGet, "/v1/meta", nil)
	var meta map[string]string
	decode(t, rec, http.StatusOK, &meta)
	if meta["service"] != "core" || meta["version"] != "test" {
		t.Fatalf("meta = %+v", meta)
	}
}

func TestReadyFailure(t *testing.T) {
	handler := testHandler(t, func(context.Context) error {
		return errors.New("db down")
	})
	rec := request(t, handler, http.MethodGet, "/readyz", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestProjectCRUD(t *testing.T) {
	handler := testHandler(t, nil)

	rec := request(t, handler, http.MethodPost, "/v1/projects", strings.NewReader(`{"name":"Web Platform","description":"site"}`))
	var created project.Project
	decode(t, rec, http.StatusCreated, &created)
	if created.Slug != "web-platform" || rec.Header().Get("Location") != "/v1/projects/"+created.ID {
		t.Fatalf("created = %+v location %q", created, rec.Header().Get("Location"))
	}

	rec = request(t, handler, http.MethodGet, "/v1/projects", nil)
	var list struct {
		Projects []project.Project `json:"projects"`
	}
	decode(t, rec, http.StatusOK, &list)
	if len(list.Projects) != 1 || list.Projects[0].ID != created.ID {
		t.Fatalf("list = %+v", list)
	}

	rec = request(t, handler, http.MethodGet, "/v1/projects/"+created.ID, nil)
	var got project.Project
	decode(t, rec, http.StatusOK, &got)
	if got.ID != created.ID {
		t.Fatalf("get = %+v", got)
	}

	rec = request(t, handler, http.MethodPatch, "/v1/projects/"+created.ID, strings.NewReader(`{"slug":"web"}`))
	var updated project.Project
	decode(t, rec, http.StatusOK, &updated)
	if updated.Slug != "web" || updated.Name != "Web Platform" {
		t.Fatalf("updated = %+v", updated)
	}

	rec = request(t, handler, http.MethodDelete, "/v1/projects/"+created.ID, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body %s", rec.Code, rec.Body.String())
	}
	rec = request(t, handler, http.MethodGet, "/v1/projects/"+created.ID, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", rec.Code)
	}
}

func TestProjectValidation(t *testing.T) {
	handler := testHandler(t, nil)

	rec := request(t, handler, http.MethodPost, "/v1/projects", strings.NewReader(`{"name":""}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty name status = %d", rec.Code)
	}

	rec = request(t, handler, http.MethodPost, "/v1/projects", strings.NewReader(`{"name":"A","extra":true}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/projects", strings.NewReader(`{"name":"A"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type status = %d", rec.Code)
	}

	request(t, handler, http.MethodPost, "/v1/projects", strings.NewReader(`{"name":"A","slug":"a"}`))
	rec = request(t, handler, http.MethodPost, "/v1/projects", strings.NewReader(`{"name":"B","slug":"a"}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d body %s", rec.Code, rec.Body.String())
	}
}

func TestClientRequestIDIsKept(t *testing.T) {
	handler := testHandler(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "abc-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-ID") != "abc-123" {
		t.Fatalf("request id = %q", rec.Header().Get("X-Request-ID"))
	}
}

func testHandler(t *testing.T, ready ReadyCheck) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(Options{
		Logger:     logger,
		Version:    "test",
		Projects:   project.NewMemoryStore(),
		ReadyCheck: ready,
	})
}

func request(t *testing.T, handler http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, status int, dest any) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d, body %s", rec.Code, status, rec.Body.String())
	}
	if err := json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(dest); err != nil {
		t.Fatalf("decode: %v body %s", err, rec.Body.String())
	}
}
