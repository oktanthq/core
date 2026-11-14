package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/oktanthq/core/internal/auth"
	"github.com/oktanthq/core/internal/githubapp"
	"github.com/oktanthq/core/internal/platform"
	"github.com/oktanthq/core/internal/project"
	"github.com/oktanthq/core/internal/queue"
)

const (
	handlerSecret = "handler-test-secret"
	orgID         = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	userID        = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	otherUser     = "cccccccc-cccc-cccc-cccc-cccccccccccc"
)

type fakeGitHub struct {
	access    githubapp.Access
	sha       string
	tokenErr  error
	commitErr error
	lastRepo  string
	lastID    int64
}

func (f *fakeGitHub) InstallURL(state string) string {
	return "https://github.com/apps/oktant/installations/new?state=" + state
}

func (f *fakeGitHub) CreateInstallationToken(installationID int64, repoFullName string) (githubapp.Access, error) {
	f.lastID = installationID
	f.lastRepo = repoFullName
	if f.tokenErr != nil {
		return githubapp.Access{}, f.tokenErr
	}
	if f.access.Token == "" {
		return githubapp.Access{
			Token:     "ghs_test_token",
			ExpiresAt: time.Date(2026, 11, 13, 20, 0, 0, 0, time.UTC),
		}, nil
	}
	return f.access, nil
}

func (f *fakeGitHub) ResolveCommit(token, repoFullName, ref string) (string, error) {
	if f.commitErr != nil {
		return "", f.commitErr
	}
	_ = token
	_ = repoFullName
	_ = ref
	if f.sha == "" {
		return "abc123def456", nil
	}
	return f.sha, nil
}

func TestAuthRequiredOnNewRoutes(t *testing.T) {
	handler := scanningHandler(t, newScanEnv(t))
	for _, path := range []string{
		"/v1/orgs/" + orgID + "/integrations/github/connect",
		"/v1/orgs/" + orgID + "/projects",
		"/v1/projects/11111111-1111-1111-1111-111111111111/analyses",
	} {
		rec := request(t, handler, http.MethodPost, path, strings.NewReader(`{}`))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d body %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestConnectGitHubAndCallback(t *testing.T) {
	env := newScanEnv(t)
	handler := scanningHandler(t, env)

	rec := authedJSON(t, handler, http.MethodPost, "/v1/orgs/"+orgID+"/integrations/github/connect", `{"redirect_to":"http://localhost:3000/dashboard"}`)
	var body connectGitHubResponse
	decode(t, rec, http.StatusOK, &body)
	if !strings.Contains(body.AuthorizeURL, "installations/new?state=") {
		t.Fatalf("authorize_url = %q", body.AuthorizeURL)
	}

	state := body.AuthorizeURL[strings.Index(body.AuthorizeURL, "state=")+6:]
	rec = request(t, handler, http.MethodGet, "/v1/integrations/github/callback?installation_id=123456&state="+state, nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback status = %d body %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "github=connected") {
		t.Fatalf("location = %q", loc)
	}
	got, err := env.store.GitHubIntegration(t.Context(), orgID)
	if err != nil {
		t.Fatalf("integration: %v", err)
	}
	if got.ExternalAccountID != "123456" || got.Status != "connected" {
		t.Fatalf("integration = %+v", got)
	}

	rec = request(t, handler, http.MethodGet, "/v1/integrations/github/callback?installation_id=1&state=tampered", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("tampered status = %d", rec.Code)
	}
}

func TestConnectGitHubForbiddenForMember(t *testing.T) {
	env := newScanEnv(t)
	env.store.SetMember(orgID, userID, platform.RoleMember)
	handler := scanningHandler(t, env)
	rec := authedJSON(t, handler, http.MethodPost, "/v1/orgs/"+orgID+"/integrations/github/connect", `{"redirect_to":"/"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
}

func TestCreateOrgProjectAndAnalysis(t *testing.T) {
	env := newScanEnv(t)
	handler := scanningHandler(t, env)
	integ := seedGitHub(t, env.store)

	rec := authedJSON(t, handler, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{
		"name":"Web Platform",
		"integration_id":"`+integ.ID+`",
		"repo":{"full_name":"acme/web","default_branch":"main","is_private":true},
		"code_analysis":false
	}`)
	var created platform.Project
	decode(t, rec, http.StatusCreated, &created)
	if created.Slug != "web-platform" || created.Repository == nil || created.Repository.FullName != "acme/web" {
		t.Fatalf("created = %+v", created)
	}
	if created.OrganizationID != orgID {
		t.Fatalf("org = %s", created.OrganizationID)
	}

	rec = authedJSON(t, handler, http.MethodPost, "/v1/projects/"+created.ID+"/analyses", `{"analysis_type":"security"}`)
	var run platform.AnalysisRun
	decode(t, rec, http.StatusAccepted, &run)
	if run.Status != platform.StatusQueued || run.CommitSHA != "abc123def456" || run.AnalysisType != "security" {
		t.Fatalf("run = %+v", run)
	}
	if env.github.lastRepo != "acme/web" || env.github.lastID != 123456 {
		t.Fatalf("github calls repo=%s id=%d", env.github.lastRepo, env.github.lastID)
	}

	messages := env.store.Messages()
	if len(messages) != 1 {
		t.Fatalf("messages = %d", len(messages))
	}
	var envMsg queue.Envelope
	if err := json.Unmarshal(messages[0], &envMsg); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if envMsg.Type != queue.TypeAnalysisRequested || envMsg.Payload.Access.Token != "ghs_test_token" {
		t.Fatalf("envelope = %+v", envMsg)
	}
	if envMsg.Payload.InstallationID != 123456 || envMsg.Payload.Trigger != "manual" {
		t.Fatalf("payload = %+v", envMsg.Payload)
	}
	if envMsg.Version != 1 || envMsg.OrgID != orgID || envMsg.Payload.OrgID != orgID {
		t.Fatalf("envelope ids = %+v", envMsg)
	}
	if _, err := uuid.Parse(envMsg.EventID); err != nil {
		t.Fatalf("event_id: %v", err)
	}
	if _, err := uuid.Parse(envMsg.CorrelationID); err != nil {
		t.Fatalf("correlation_id: %v", err)
	}
	if envMsg.ActorUserID != userID {
		t.Fatalf("actor_user_id = %q", envMsg.ActorUserID)
	}
	if got := envMsg.Payload.RequestedAnalyzers; len(got) != 3 || got[0] != "sast" || got[1] != "secrets" || got[2] != "dependencies" {
		t.Fatalf("requested_analyzers = %v", got)
	}
	if envMsg.Payload.Access.Kind != "installation_token" || envMsg.Payload.Provider != "github" {
		t.Fatalf("access/provider = %+v", envMsg.Payload)
	}

	var raw map[string]any
	if err := json.Unmarshal(messages[0], &raw); err != nil {
		t.Fatalf("raw: %v", err)
	}
	if raw["version"] != float64(1) {
		t.Fatalf("version JSON type = %T %v", raw["version"], raw["version"])
	}
	payload, _ := raw["payload"].(map[string]any)
	if _, ok := payload["installation_id"].(float64); !ok {
		t.Fatalf("installation_id JSON type = %T", payload["installation_id"])
	}

	rec = authedJSON(t, handler, http.MethodPost, "/v1/projects/"+created.ID+"/analyses", `{"analysis_type":"security"}`)
	var again platform.AnalysisRun
	decode(t, rec, http.StatusAccepted, &again)
	if again.ID != run.ID {
		t.Fatalf("dedup = %+v want %+v", again, run)
	}
	if len(env.store.Messages()) != 1 {
		t.Fatalf("duplicate enqueue, messages = %d", len(env.store.Messages()))
	}
}

func TestCreateOrgProjectEnqueuesFirstScan(t *testing.T) {
	env := newScanEnv(t)
	handler := scanningHandler(t, env)
	integ := seedGitHub(t, env.store)
	rec := authedJSON(t, handler, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{
		"name":"Docs",
		"integration_id":"`+integ.ID+`",
		"repo":{"full_name":"acme/docs"}
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	if len(env.store.Messages()) != 1 {
		t.Fatalf("messages = %d", len(env.store.Messages()))
	}
	var envMsg queue.Envelope
	if err := json.Unmarshal(env.store.Messages()[0], &envMsg); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if envMsg.Payload.Trigger != "project_created" {
		t.Fatalf("trigger = %s", envMsg.Payload.Trigger)
	}
}

func TestCreateAnalysisRejectsOtherOrg(t *testing.T) {
	env := newScanEnv(t)
	handler := scanningHandler(t, env)
	integ := seedGitHub(t, env.store)
	created, err := env.store.CreateProject(t.Context(), platform.CreateProjectInput{
		OrganizationID: orgID,
		CreatedBy:      userID,
		Name:           "Web",
		IntegrationID:  integ.ID,
		Repo:           platform.CreateRepoInput{FullName: "acme/web", DefaultBranch: "main"},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	token := signUser(t, otherUser, time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+created.ID+"/analyses", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
}

func TestInvalidToken(t *testing.T) {
	handler := scanningHandler(t, newScanEnv(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/orgs/"+orgID+"/projects", strings.NewReader(`{"name":"A"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

type scanEnv struct {
	store  *platform.MemoryStore
	github *fakeGitHub
	now    time.Time
}

func newScanEnv(t *testing.T) *scanEnv {
	t.Helper()
	store := platform.NewMemoryStore()
	store.SetMember(orgID, userID, platform.RoleOwner)
	return &scanEnv{
		store:  store,
		github: &fakeGitHub{},
		now:    time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC),
	}
}

func scanningHandler(t *testing.T, env *scanEnv) http.Handler {
	t.Helper()
	verifier, err := auth.NewVerifier(auth.Options{
		JWTSecret: handlerSecret,
		Audience:  "authenticated",
		Now:       func() time.Time { return env.now },
	})
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(Options{
		Logger:       logger,
		Version:      "test",
		Projects:     project.NewMemoryStore(),
		Platform:     env.store,
		Verifier:     verifier,
		GitHub:       env.github,
		GitHubWebURL: "https://github.com",
		AppOrigin:    "http://localhost:3000",
		StateSecret:  handlerSecret,
		Now:          func() time.Time { return env.now },
	})
}

func seedGitHub(t *testing.T, store *platform.MemoryStore) platform.Integration {
	t.Helper()
	integ, err := store.UpsertGitHubInstallation(t.Context(), platform.GitHubInstallInput{
		OrganizationID: orgID,
		InstallationID: "123456",
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	return integ
}

func authedJSON(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+signUser(t, userID, time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func signUser(t *testing.T, sub string, now time.Time) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  sub,
		"role": "authenticated",
		"aud":  "authenticated",
		"exp":  now.Add(time.Hour).Unix(),
		"iat":  now.Unix(),
	})
	s, err := token.SignedString([]byte(handlerSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}
