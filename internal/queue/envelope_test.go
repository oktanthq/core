package queue

import (
	"encoding/json"
	"regexp"
	"testing"
	"time"
)

func TestNewAnalysisRequestedSchema(t *testing.T) {
	occurred := time.Date(2026, 11, 13, 19, 0, 0, 0, time.UTC)
	env := NewAnalysisRequested(
		"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
		occurred,
		"33333333-3333-4333-8333-333333333333",
		"44444444-4444-4444-8444-444444444444",
		AnalysisPayload{
			AnalysisRunID:      "55555555-5555-4555-8555-555555555555",
			OrgID:              "11111111-1111-4111-8111-111111111111",
			ProjectID:          "22222222-2222-4222-8222-222222222222",
			RepositoryID:       "66666666-6666-4666-8666-666666666666",
			Provider:           "github",
			RepoFullName:       "acme/web",
			CloneURL:           "https://github.com/acme/web.git",
			DefaultBranch:      "main",
			CommitSHA:          "abc123def4567890abc123def4567890abc123de",
			Ref:                "refs/heads/main",
			Trigger:            "manual",
			AnalysisType:       "security",
			RequestedAnalyzers: RequestedAnalyzers("security"),
			InstallationID:     123456,
			Access: Access{
				Kind:      "installation_token",
				Token:     "ghs_not-a-real-token",
				ExpiresAt: "2026-11-13T20:00:00.000Z",
			},
		},
	)

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
	if got["version"] != float64(1) {
		t.Fatalf("version = %v (%T)", got["version"], got["version"])
	}
	if got["occurred_at"] != "2026-11-13T19:00:00.000Z" {
		t.Fatalf("occurred_at = %v", got["occurred_at"])
	}
	uuidPattern := regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	for _, key := range []string{"event_id", "org_id", "project_id", "actor_user_id", "correlation_id"} {
		s, _ := got[key].(string)
		if !uuidPattern.MatchString(s) {
			t.Fatalf("%s is not a UUID: %v", key, got[key])
		}
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
	if payload["org_id"] != got["org_id"] || payload["project_id"] != got["project_id"] {
		t.Fatal("envelope org_id/project_id must match payload")
	}
	if _, ok := payload["installation_id"].(float64); !ok {
		t.Fatalf("installation_id type = %T", payload["installation_id"])
	}
	analyzers, _ := payload["requested_analyzers"].([]any)
	if len(analyzers) != 3 || analyzers[0] != "sast" || analyzers[1] != "secrets" || analyzers[2] != "dependencies" {
		t.Fatalf("requested_analyzers = %v", analyzers)
	}
	access, _ := payload["access"].(map[string]any)
	if access["kind"] != "installation_token" {
		t.Fatalf("access = %v", access)
	}
	sha, _ := payload["commit_sha"].(string)
	if matched, _ := regexp.MatchString(`(?i)^[0-9a-f]{7,40}$`, sha); !matched {
		t.Fatalf("commit_sha = %q", sha)
	}
}

func TestRequestedAnalyzers(t *testing.T) {
	for _, kind := range []string{"security", "quality", "full", "other"} {
		got := RequestedAnalyzers(kind)
		if len(got) != 3 || got[0] != "sast" || got[1] != "secrets" || got[2] != "dependencies" {
			t.Fatalf("%s = %v", kind, got)
		}
	}
}

func TestActorUserIDOmittedWhenEmpty(t *testing.T) {
	env := NewAnalysisRequested("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", time.Unix(0, 0).UTC(), "", "44444444-4444-4444-8444-444444444444", AnalysisPayload{
		OrgID: "11111111-1111-4111-8111-111111111111", ProjectID: "22222222-2222-4222-8222-222222222222",
	})
	raw, err := MarshalJSONBytes(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := got["actor_user_id"]; ok {
		t.Fatalf("actor_user_id should be omitted, got %v", got["actor_user_id"])
	}
	if got["correlation_id"] != "44444444-4444-4444-8444-444444444444" {
		t.Fatalf("correlation_id = %v", got["correlation_id"])
	}
}
