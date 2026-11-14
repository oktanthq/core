package queue

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewAnalysisRequestedSchema(t *testing.T) {
	occurred := time.Date(2026, 11, 13, 20, 0, 0, 0, time.UTC)
	env := NewAnalysisRequested("evt", occurred, "user-1", "corr-1", AnalysisPayload{
		AnalysisRunID:      "run-1",
		OrgID:              "org-1",
		ProjectID:          "proj-1",
		RepositoryID:       "repo-1",
		Provider:           "github",
		RepoFullName:       "acme/web",
		CloneURL:           "https://github.com/acme/web.git",
		DefaultBranch:      "main",
		CommitSHA:          "abc123",
		Ref:                "refs/heads/main",
		Trigger:            "manual",
		AnalysisType:       "security",
		RequestedAnalyzers: RequestedAnalyzers("security"),
		InstallationID:     123456,
		Access: Access{
			Kind:      "installation_token",
			Token:     "ghs_test",
			ExpiresAt: "2026-11-13T20:00:00Z",
		},
	})

	raw, err := MarshalJSONBytes(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"event_id", "type", "version", "occurred_at", "org_id", "project_id", "actor_user_id", "correlation_id", "payload"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing envelope key %q in %s", key, raw)
		}
	}
	if got["type"] != TypeAnalysisRequested {
		t.Fatalf("type = %v", got["type"])
	}
	payload, _ := got["payload"].(map[string]any)
	for _, key := range []string{
		"analysis_run_id", "org_id", "project_id", "repository_id", "provider",
		"repo_full_name", "clone_url", "default_branch", "commit_sha", "ref",
		"trigger", "analysis_type", "requested_analyzers", "installation_id", "access",
	} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing payload key %q", key)
		}
	}
	access, _ := payload["access"].(map[string]any)
	if access["kind"] != "installation_token" {
		t.Fatalf("access = %v", access)
	}
	if !strings.Contains(string(raw), `"token":"ghs_test"`) {
		t.Fatal("token missing from payload")
	}
}

func TestRequestedAnalyzers(t *testing.T) {
	if got := RequestedAnalyzers("security"); len(got) != 3 {
		t.Fatalf("security = %v", got)
	}
	if got := RequestedAnalyzers("nope"); got != nil {
		t.Fatalf("unknown = %v", got)
	}
}
