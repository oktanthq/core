package platform

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryStoreProjectAndAnalysis(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	orgID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	userID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	store.SetMember(orgID, userID, RoleOwner)

	integ, err := store.UpsertGitHubInstallation(ctx, GitHubInstallInput{OrganizationID: orgID, InstallationID: "99"})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	created, err := store.CreateProject(ctx, CreateProjectInput{
		OrganizationID: orgID,
		CreatedBy:      userID,
		Name:           "Web Platform",
		IntegrationID:  integ.ID,
		Repo:           CreateRepoInput{FullName: "acme/web", DefaultBranch: "main", CloneURL: "https://github.com/acme/web.git"},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if created.Slug != "web-platform" || created.Repository == nil {
		t.Fatalf("created = %+v", created)
	}

	_, err = store.CreateProject(ctx, CreateProjectInput{
		OrganizationID: orgID,
		Name:           "Other",
		Slug:           "web-platform",
		IntegrationID:  integ.ID,
		Repo:           CreateRepoInput{FullName: "acme/other"},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("slug conflict err = %v", err)
	}

	run, err := store.CreateQueuedAnalysis(ctx, AnalysisRun{
		RepositoryID: created.Repository.ID,
		AnalysisType: "security",
		CommitSHA:    "abc",
		Trigger:      "manual",
	}, []byte(`{"type":"analysis.requested"}`))
	if err != nil {
		t.Fatalf("CreateQueuedAnalysis: %v", err)
	}
	if run.Status != StatusQueued {
		t.Fatalf("run = %+v", run)
	}
	_, err = store.CreateQueuedAnalysis(ctx, AnalysisRun{
		RepositoryID: created.Repository.ID,
		AnalysisType: "security",
		CommitSHA:    "abc",
		Trigger:      "manual",
	}, []byte(`{}`))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("dedup err = %v", err)
	}
}

func TestMemoryStoreEnqueueHookFailureDoesNotPersist(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	orgID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	store.SetMember(orgID, "u", RoleOwner)
	integ, _ := store.UpsertGitHubInstallation(ctx, GitHubInstallInput{OrganizationID: orgID, InstallationID: "1"})
	created, _ := store.CreateProject(ctx, CreateProjectInput{
		OrganizationID: orgID,
		Name:           "App",
		IntegrationID:  integ.ID,
		Repo:           CreateRepoInput{FullName: "acme/app"},
	})
	store.SetEnqueueHook(func(AnalysisRun, []byte) error { return errors.New("send failed") })
	_, err := store.CreateQueuedAnalysis(ctx, AnalysisRun{
		RepositoryID: created.Repository.ID,
		AnalysisType: "security",
		CommitSHA:    "abc",
		Trigger:      "manual",
	}, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if _, err := store.ActiveAnalysis(ctx, created.Repository.ID, "abc", "security"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("run should not persist, err = %v", err)
	}
}
